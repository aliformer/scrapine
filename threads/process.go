package threads

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
	threadsPostIDRegex = regexp.MustCompile(`(?:/post/|/t/)([A-Za-z0-9_-]+)`)
	metricKMBRegexTh   = regexp.MustCompile(`(?i)([\d.]+)\s*([KMB])?`)
)

func parseThreadsResponse(ctx context.Context, resp *scrapemate.Response, job *ThreadsJob) (any, []scrapemate.IJob, error) {
	var posts []*Post
	var nextJobs []scrapemate.IJob

	if resp.Error != nil {
		return posts, nextJobs, resp.Error
	}

	// 1. Intercepted GraphQL responses
	if resp.Meta != nil {
		if captured, ok := resp.Meta["graphql_threads"].([]map[string]any); ok && len(captured) > 0 {
			posts = extractPostsFromGraphQL(ctx, captured, job)
			if len(posts) > 0 {
				return posts, nextJobs, nil
			}
		}
	}

	doc, _ := resp.Document.(*goquery.Document)
	if doc == nil && len(resp.Body) > 0 {
		doc, _ = goquery.NewDocumentFromReader(strings.NewReader(string(resp.Body)))
	}

	if doc != nil {
		// 2. Embedded scripts in HTML
		posts = extractPostsFromScripts(ctx, doc, job)
		if len(posts) > 0 {
			return posts, nextJobs, nil
		}

		// 3. DOM Fallback
		posts = extractPostsFromDOM(ctx, doc, job)
		if len(posts) > 0 {
			return posts, nextJobs, nil
		}
	}

	return posts, nextJobs, nil
}

func extractPostsFromGraphQL(ctx context.Context, responses []map[string]any, job *ThreadsJob) []*Post {
	var posts []*Post
	seen := make(map[string]bool)

	for _, respMap := range responses {
		threadItems := jsonparse.FindByTypename(respMap, []string{
			"XDTThreadItem",
			"XDTThread",
			"Thread",
		}, 50)

		for _, item := range threadItems {
			post := extractPostFromThreadItem(item, job)
			if post != nil && post.ID != "" && !seen[post.ID] {
				if job.Deduper != nil && !job.Deduper.AddIfNotExists(ctx, post.ID) {
					continue
				}
				seen[post.ID] = true
				posts = append(posts, post)
			}
		}

		postObjects := jsonparse.FindByKey(respMap, "thread_items", 30)
		for _, obj := range postObjects {
			items := jsonparse.GetSlice(obj, "thread_items")
			for _, ti := range items {
				if tiMap, ok := ti.(map[string]any); ok {
					post := extractPostFromThreadItem(tiMap, job)
					if post != nil && post.ID != "" && !seen[post.ID] {
						if job.Deduper != nil && !job.Deduper.AddIfNotExists(ctx, post.ID) {
							continue
						}
						seen[post.ID] = true
						posts = append(posts, post)
					}
				}
			}
		}
	}

	return posts
}

func parseMetricCount(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	s = strings.ReplaceAll(s, ",", "")
	matches := metricKMBRegexTh.FindStringSubmatch(s)
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

func extractPostsFromDOM(ctx context.Context, doc *goquery.Document, job *ThreadsJob) []*Post {
	var posts []*Post
	seen := make(map[string]bool)

	pageAuthor := job.Username

	doc.Find(`div[data-pressable-container], div[data-testid*="post"]`).Each(func(i int, s *goquery.Selection) {
		var postURL, postID string
		s.Find(`a[href*="/post/"], a[href*="/t/"]`).Each(func(_ int, link *goquery.Selection) {
			if postID != "" {
				return
			}
			href, _ := link.Attr("href")
			if m := threadsPostIDRegex.FindStringSubmatch(href); len(m) >= 2 {
				postID = m[1]
				postURL = href
			}
		})

		if postID == "" {
			return
		}
		if seen[postID] {
			return
		}
		if job.Deduper != nil && !job.Deduper.AddIfNotExists(ctx, postID) {
			return
		}
		seen[postID] = true

		if !strings.HasPrefix(postURL, "http") {
			postURL = "https://www.threads.net" + postURL
		}

		authorName := pageAuthor
		if authorLink := s.Find(`a[href^="/@"]`).First(); authorLink.Length() > 0 {
			href, _ := authorLink.Attr("href")
			u := strings.TrimPrefix(href, "/@")
			if idx := strings.Index(u, "/"); idx >= 0 {
				u = u[:idx]
			}
			if u != "" {
				authorName = u
			}
		}

		text := ""
		if msgEl := s.Find(`div[dir="auto"], span[dir="auto"]`).First(); msgEl.Length() > 0 {
			text = strings.TrimSpace(msgEl.Text())
		}

		var metrics Metrics
		s.Find(`div[role="button"], button`).Each(func(_ int, btn *goquery.Selection) {
			txt := strings.TrimSpace(btn.Text())
			aria, _ := btn.Attr("aria-label")
			combined := strings.ToLower(txt + " " + aria)
			switch {
			case strings.Contains(combined, "like"):
				metrics.Likes = parseMetricCount(txt)
			case strings.Contains(combined, "repl"):
				metrics.Replies = parseMetricCount(txt)
			case strings.Contains(combined, "repost"):
				metrics.Reposts = parseMetricCount(txt)
			}
		})

		post := &Post{
			ID:  postID,
			URL: postURL,
			Author: Profile{
				Username: authorName,
			},
			Text:    text,
			Metrics: metrics,
		}
		posts = append(posts, post)
	})

	return posts
}

func extractPostsFromScripts(ctx context.Context, doc *goquery.Document, job *ThreadsJob) []*Post {
	var posts []*Post

	doc.Find(`script[type="application/json"]`).Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if text == "" {
			return
		}

		var data map[string]any
		if err := json.Unmarshal([]byte(text), &data); err != nil {
			return
		}

		// Look for XDTThreadItem or Thread objects
		threadItems := jsonparse.FindByTypename(data, []string{
			"XDTThreadItem",
			"XDTThread",
			"Thread",
		}, 50)

		for _, item := range threadItems {
			post := extractPostFromThreadItem(item, job)
			if post != nil {
				posts = append(posts, post)
			}
		}

		// Also look for posts directly (different structure)
		postObjects := jsonparse.FindByKey(data, "thread_items", 30)
		for _, obj := range postObjects {
			threadItems := jsonparse.GetSlice(obj, "thread_items")
			for _, ti := range threadItems {
				if tiMap, ok := ti.(map[string]any); ok {
					post := extractPostFromThreadItem(tiMap, job)
					if post != nil {
						posts = append(posts, post)
					}
				}
			}
		}
	})

	// Deduplicate by ID
	seen := make(map[string]bool)
	var unique []*Post
	for _, p := range posts {
		if p.ID != "" && !seen[p.ID] {
			if job.Deduper != nil && !job.Deduper.AddIfNotExists(ctx, p.ID) {
				continue
			}
			seen[p.ID] = true
			unique = append(unique, p)
		}
	}

	return unique
}

func extractPostFromThreadItem(item map[string]any, job *ThreadsJob) *Post {
	// Try to get post data from nested "post" key or directly
	postData := jsonparse.GetMap(item, "post")
	if postData == nil {
		postData = item
	}

	// ID can be pk or id
	id := jsonparse.GetString(postData, "pk")
	if id == "" {
		id = jsonparse.GetString(postData, "id")
	}
	if id == "" {
		return nil
	}

	// Author info
	user := jsonparse.GetMap(postData, "user")
	var author Profile
	if user != nil {
		author = Profile{
			ID:             jsonparse.GetString(user, "pk"),
			Username:       jsonparse.GetString(user, "username"),
			Name:           jsonparse.GetString(user, "full_name"),
			Bio:            jsonparse.GetString(user, "biography"),
			AvatarURL:      jsonparse.GetString(user, "profile_pic_url"),
			FollowersCount: jsonparse.GetInt(user, "follower_count"),
			Verified:       jsonparse.GetBool(user, "is_verified"),
		}
	} else {
		author = Profile{Username: job.Username}
	}

	// Text content
	text := ""
	caption := jsonparse.GetMap(postData, "caption")
	if caption != nil {
		text = jsonparse.GetString(caption, "text")
	}
	if text == "" {
		text = jsonparse.GetString(postData, "text")
	}

	// Metrics
	metrics := Metrics{
		Likes: jsonparse.GetInt(postData, "like_count"),
	}

	// Reply/repost counts from text_post_app_info
	textPostInfo := jsonparse.GetMap(postData, "text_post_app_info")
	if textPostInfo != nil {
		metrics.Replies = jsonparse.GetInt(textPostInfo, "direct_reply_count")

		shareInfo := jsonparse.GetMap(textPostInfo, "share_info")
		if shareInfo != nil {
			metrics.Reposts = jsonparse.GetInt(shareInfo, "repost_count")
			metrics.Shares = jsonparse.GetInt(shareInfo, "quote_count")
		}
	}

	// Media URLs
	var media []string

	// Single image
	imageVersions := jsonparse.GetMap(postData, "image_versions2")
	if imageVersions != nil {
		candidates := jsonparse.GetSlice(imageVersions, "candidates")
		if len(candidates) > 0 {
			if first, ok := candidates[0].(map[string]any); ok {
				if url := jsonparse.GetString(first, "url"); url != "" {
					media = append(media, url)
				}
			}
		}
	}

	// Carousel media
	carouselMedia := jsonparse.GetSlice(postData, "carousel_media")
	for _, cm := range carouselMedia {
		if cmMap, ok := cm.(map[string]any); ok {
			iv := jsonparse.GetMap(cmMap, "image_versions2")
			if iv != nil {
				candidates := jsonparse.GetSlice(iv, "candidates")
				if len(candidates) > 0 {
					if first, ok := candidates[0].(map[string]any); ok {
						if url := jsonparse.GetString(first, "url"); url != "" {
							media = append(media, url)
						}
					}
				}
			}
		}
	}

	// Post URL
	code := jsonparse.GetString(postData, "code")
	postURL := "https://www.threads.net/@" + author.Username + "/post/" + code
	if code == "" {
		postURL = "https://www.threads.net/@" + author.Username
	}

	return &Post{
		ID:        id,
		URL:       postURL,
		Author:    author,
		Text:      text,
		CreatedAt: jsonparse.GetTime(postData, "taken_at"),
		Media:     media,
		Metrics:   metrics,
	}
}
