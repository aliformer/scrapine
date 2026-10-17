package tiktok

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/gosom/scrapemate"

	"github.com/gosom/google-maps-scraper/internal/jsonparse"
)

var (
	tiktokVideoIDRegex = regexp.MustCompile(`/video/(\d+)`)
	metricKMBRegex     = regexp.MustCompile(`(?i)([\d.]+)\s*([KMB])?`)
)

func parseTikTokResponse(ctx context.Context, resp *scrapemate.Response, job *TikTokJob) (any, []scrapemate.IJob, error) {
	var videos []*Video
	var nextJobs []scrapemate.IJob

	if resp.Error != nil {
		return videos, nextJobs, resp.Error
	}

	// 1. Intercepted API responses
	if resp.Meta != nil {
		if captured, ok := resp.Meta["api_videos"].([]map[string]any); ok && len(captured) > 0 {
			videos = extractVideosFromAPI(ctx, captured, job)
			if len(videos) > 0 {
				return videos, nextJobs, nil
			}
		}
	}

	doc, _ := resp.Document.(*goquery.Document)
	if doc == nil && len(resp.Body) > 0 {
		doc, _ = goquery.NewDocumentFromReader(strings.NewReader(string(resp.Body)))
	}

	if doc != nil {
		// 2. Try __UNIVERSAL_DATA_FOR_REHYDRATION__ (modern TikTok)
		if data, found := jsonparse.FindScriptJSONByID(doc, "__UNIVERSAL_DATA_FOR_REHYDRATION__"); found {
			videos = extractFromUniversalData(ctx, data, job)
			if len(videos) > 0 {
				return videos, nextJobs, nil
			}
		}

		// 3. Fallback: try SIGI_STATE (older TikTok)
		videos = extractFromSigiState(ctx, doc, job)
		if len(videos) > 0 {
			return videos, nextJobs, nil
		}

		// 4. DOM Fallback
		videos = extractVideosFromDOM(ctx, doc, job)
		if len(videos) > 0 {
			return videos, nextJobs, nil
		}
	}

	return videos, nextJobs, nil
}

func extractVideosFromAPI(ctx context.Context, responses []map[string]any, job *TikTokJob) []*Video {
	var videos []*Video
	seen := make(map[string]bool)

	for _, respMap := range responses {
		var items []any
		if list := jsonparse.GetSlice(respMap, "itemList"); len(list) > 0 {
			items = list
		} else if list := jsonparse.GetSlice(respMap, "item_list"); len(list) > 0 {
			items = list
		} else if list := jsonparse.GetSlice(respMap, "data"); len(list) > 0 {
			items = list
		} else if dataMap := jsonparse.GetMap(respMap, "data"); dataMap != nil {
			if list := jsonparse.GetSlice(dataMap, "item_list"); len(list) > 0 {
				items = list
			} else if list := jsonparse.GetSlice(dataMap, "itemList"); len(list) > 0 {
				items = list
			}
		}

		for _, item := range items {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}

			if innerItem := jsonparse.GetMap(itemMap, "item"); innerItem != nil {
				itemMap = innerItem
			}

			video := extractVideoFromItem(itemMap, nil, job)
			if video != nil && video.ID != "" && !seen[video.ID] {
				if job.Deduper != nil && !job.Deduper.AddIfNotExists(ctx, video.ID) {
					continue
				}
				seen[video.ID] = true
				videos = append(videos, video)
			}
		}
	}

	return videos
}

func parseMetricCount(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	s = strings.ReplaceAll(s, ",", "")
	matches := metricKMBRegex.FindStringSubmatch(s)
	if len(matches) < 2 {
		return 0
	}
	val, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0
	}
	multiplier := 1.0
	if len(matches) >= 3 {
		switch strings.ToUpper(matches[2]) {
		case "K":
			multiplier = 1_000
		case "M":
			multiplier = 1_000_000
		case "B":
			multiplier = 1_000_000_000
		}
	}
	return int(val * multiplier)
}

func extractVideosFromDOM(ctx context.Context, doc *goquery.Document, job *TikTokJob) []*Video {
	var videos []*Video
	seen := make(map[string]bool)

	pageAuthor := job.Username
	if h1 := doc.Find(`h1[data-e2e="user-title"], h2[data-e2e="user-subtitle"]`).First(); h1.Length() > 0 {
		txt := strings.TrimSpace(h1.Text())
		if txt != "" {
			pageAuthor = strings.TrimPrefix(txt, "@")
		}
	}

	doc.Find(`div[data-e2e="user-post-item"], div[data-e2e="search_top-item"], div[data-e2e="search-card-like-container"], div[data-e2e="challenge-item"]`).Each(func(_ int, s *goquery.Selection) {
		link := s.Find(`a[href*="/video/"]`).First()
		if link.Length() == 0 {
			return
		}
		href, _ := link.Attr("href")
		m := tiktokVideoIDRegex.FindStringSubmatch(href)
		if len(m) < 2 {
			return
		}
		id := m[1]
		if seen[id] {
			return
		}
		if job.Deduper != nil && !job.Deduper.AddIfNotExists(ctx, id) {
			return
		}
		seen[id] = true

		authorName := pageAuthor
		if cardUser := s.Find(`span[data-e2e="search-card-user-unique-id"], a[data-e2e="search-card-user-link"]`).First(); cardUser.Length() > 0 {
			if u := strings.TrimSpace(cardUser.Text()); u != "" {
				authorName = strings.TrimPrefix(u, "@")
			}
		}

		desc := strings.TrimSpace(s.Find(`div[data-e2e="user-post-item-desc"], div[data-e2e="search-card-video-caption"]`).Text())
		if desc == "" {
			desc, _ = link.Attr("title")
		}

		viewsStr := strings.TrimSpace(s.Find(`strong[data-e2e="video-views"], span[data-e2e="search-card-like-count"]`).Text())
		views := parseMetricCount(viewsStr)

		videoURL := href
		if !strings.HasPrefix(videoURL, "http") {
			videoURL = "https://www.tiktok.com" + videoURL
		}

		v := &Video{
			ID:  id,
			URL: videoURL,
			Author: Profile{
				Username: authorName,
			},
			Description: desc,
			Metrics: Metrics{
				Views: views,
			},
		}
		videos = append(videos, v)
	})

	if len(videos) == 0 {
		doc.Find(`a[href*="/video/"]`).Each(func(_ int, s *goquery.Selection) {
			href, _ := s.Attr("href")
			m := tiktokVideoIDRegex.FindStringSubmatch(href)
			if len(m) < 2 {
				return
			}
			id := m[1]
			if seen[id] {
				return
			}
			if job.Deduper != nil && !job.Deduper.AddIfNotExists(ctx, id) {
				return
			}
			seen[id] = true

			videoURL := href
			if !strings.HasPrefix(videoURL, "http") {
				videoURL = "https://www.tiktok.com" + videoURL
			}

			videos = append(videos, &Video{
				ID:  id,
				URL: videoURL,
				Author: Profile{
					Username: pageAuthor,
				},
				Description: strings.TrimSpace(s.Text()),
			})
		})
	}

	return videos
}

func extractFromUniversalData(ctx context.Context, data map[string]any, job *TikTokJob) []*Video {
	var videos []*Video

	// Path: __DEFAULT_SCOPE__ -> webapp.user-detail -> userInfo for profile
	// Path: __DEFAULT_SCOPE__ -> webapp.video-detail -> itemInfo -> itemStruct for single video
	defaultScope := jsonparse.GetMap(data, "__DEFAULT_SCOPE__")
	if defaultScope == nil {
		return videos
	}

	// Try user profile path (list of videos)
	userDetail := jsonparse.GetMap(defaultScope, "webapp.user-detail")
	if userDetail != nil {
		// User info
		userInfo := jsonparse.GetMap(userDetail, "userInfo")
		user := jsonparse.GetMap(userInfo, "user")

		// Item list (videos)
		itemList := jsonparse.GetSlice(userDetail, "itemList")
		for _, item := range itemList {
			if itemMap, ok := item.(map[string]any); ok {
				video := extractVideoFromItem(itemMap, user, job)
				if video != nil && video.ID != "" {
					if job.Deduper != nil && !job.Deduper.AddIfNotExists(ctx, video.ID) {
						continue
					}
					videos = append(videos, video)
				}
			}
		}
	}

	// Try single video path
	videoDetail := jsonparse.GetMap(defaultScope, "webapp.video-detail")
	if videoDetail != nil {
		itemInfo := jsonparse.GetMap(videoDetail, "itemInfo")
		itemStruct := jsonparse.GetMap(itemInfo, "itemStruct")
		if itemStruct != nil {
			video := extractVideoFromItem(itemStruct, nil, job)
			if video != nil && video.ID != "" {
				if job.Deduper == nil || job.Deduper.AddIfNotExists(ctx, video.ID) {
					videos = append(videos, video)
				}
			}
		}
	}

	return videos
}

func extractVideoFromItem(item map[string]any, defaultUser map[string]any, job *TikTokJob) *Video {
	id := jsonparse.GetString(item, "id")
	if id == "" {
		return nil
	}

	// Author info - prefer from item, fall back to default
	author := jsonparse.GetMap(item, "author")
	if author == nil {
		author = defaultUser
	}

	var profile Profile
	if author != nil {
		profile = Profile{
			ID:        jsonparse.GetString(author, "id"),
			Username:  jsonparse.GetString(author, "uniqueId"),
			Name:      jsonparse.GetString(author, "nickname"),
			AvatarURL: jsonparse.GetString(author, "avatarLarger"),
		}
	} else {
		profile = Profile{Username: job.Username}
	}

	// Stats
	stats := jsonparse.GetMap(item, "stats")
	var metrics Metrics
	if stats != nil {
		metrics = Metrics{
			Views:    jsonparse.GetInt(stats, "playCount"),
			Likes:    jsonparse.GetInt(stats, "diggCount"),
			Comments: jsonparse.GetInt(stats, "commentCount"),
			Shares:   jsonparse.GetInt(stats, "shareCount"),
		}
	}

	// Video URL
	videoURL := jsonparse.GetString(item, "video", "playAddr")
	if videoURL == "" {
		// Construct from ID
		videoURL = "https://www.tiktok.com/@" + profile.Username + "/video/" + id
	}

	return &Video{
		ID:          id,
		URL:         videoURL,
		Author:      profile,
		Description: jsonparse.GetString(item, "desc"),
		CreatedAt:   jsonparse.GetTime(item, "createTime"),
		Metrics:     metrics,
	}
}

func extractFromSigiState(ctx context.Context, doc *goquery.Document, job *TikTokJob) []*Video {
	var videos []*Video

	// Find SIGI_STATE in script tags
	re := regexp.MustCompile(`SIGI_STATE["\s]*=\s*(\{.+?\});`)

	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		text := s.Text()
		if !strings.Contains(text, "SIGI_STATE") {
			return
		}

		matches := re.FindStringSubmatch(text)
		if len(matches) < 2 {
			return
		}

		var data map[string]any
		if err := json.Unmarshal([]byte(matches[1]), &data); err != nil {
			return
		}

		// Look for ItemModule which contains video data
		itemModule := jsonparse.GetMap(data, "ItemModule")
		if itemModule != nil {
			for _, v := range itemModule {
				if itemMap, ok := v.(map[string]any); ok {
					video := extractVideoFromItem(itemMap, nil, job)
					if video != nil && video.ID != "" {
						if job.Deduper != nil && !job.Deduper.AddIfNotExists(ctx, video.ID) {
							continue
						}
						videos = append(videos, video)
					}
				}
			}
		}
	})

	return videos
}
