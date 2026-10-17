package twitter

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gosom/scrapemate"

	"github.com/gosom/google-maps-scraper/internal/jsonparse"
)

var statusIDRegex = regexp.MustCompile(`/status/(\d+)`)

func parseTwitterResponse(ctx context.Context, resp *scrapemate.Response, job *TwitterJob) (any, []scrapemate.IJob, error) {
	var tweets []*Tweet
	var nextJobs []scrapemate.IJob

	if resp.Error != nil {
		return tweets, nextJobs, resp.Error
	}

	// 1. Process GraphQL responses intercepted during browser actions
	if resp.Meta != nil {
		if rawData, ok := resp.Meta["graphql_tweets"]; ok {
			if objects, ok := rawData.([]map[string]any); ok && len(objects) > 0 {
				tweets = extractTweetsFromGraphQL(ctx, objects, job)
				if len(tweets) > 0 {
					return tweets, nextJobs, nil
				}
			}
		}
	}

	doc, ok := resp.Document.(*goquery.Document)
	if !ok {
		return tweets, nextJobs, nil
	}

	// 2. Try __NEXT_DATA__ first (legacy/fallback)
	if data, found := jsonparse.FindScriptJSONByID(doc, "__NEXT_DATA__"); found {
		tweets = extractTweetsFromNextData(ctx, data, job)
		if len(tweets) > 0 {
			return tweets, nextJobs, nil
		}
	}

	// 3. Search all script tags for tweet data (SSR fallback)
	tweets = extractTweetsFromScripts(ctx, doc, job)
	if len(tweets) > 0 {
		return tweets, nextJobs, nil
	}

	// 4. Fallback: extract tweets from rendered DOM
	tweets = extractTweetsFromDOM(ctx, doc, job)

	return tweets, nextJobs, nil
}

func extractTweetsFromGraphQL(ctx context.Context, responses []map[string]any, job *TwitterJob) []*Tweet {
	var allObjects []map[string]any

	for _, respMap := range responses {
		typed := jsonparse.FindByTypename(respMap, []string{
			"Tweet",
			"TweetWithVisibilityResults",
			"TweetTombstone",
		}, 200)
		allObjects = append(allObjects, typed...)

		resultObjects := jsonparse.FindByKey(respMap, "tweet_results", 200)
		for _, ro := range resultObjects {
			tr := jsonparse.GetMap(ro, "tweet_results")
			if tr != nil {
				if res := jsonparse.GetMap(tr, "result"); res != nil {
					allObjects = append(allObjects, res)
				}
			}
		}
	}

	return processTweetObjects(ctx, allObjects, job)
}

func extractTweetsFromNextData(ctx context.Context, data map[string]any, job *TwitterJob) []*Tweet {
	// Find objects with __typename "Tweet" or "TweetWithVisibilityResults"
	tweetObjects := jsonparse.FindByTypename(data, []string{
		"Tweet",
		"TweetWithVisibilityResults",
		"TweetTombstone",
	}, 50)

	return processTweetObjects(ctx, tweetObjects, job)
}

func extractTweetsFromScripts(ctx context.Context, doc *goquery.Document, job *TwitterJob) []*Tweet {
	var tweets []*Tweet

	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		text := s.Text()

		// Look for tweet_results or TweetDetail markers
		if !strings.Contains(text, "tweet_results") && !strings.Contains(text, "__typename") {
			return
		}

		// Try to find JSON in the script
		// Twitter often uses: window.__INITIAL_STATE__ = {...}
		jsonStr := extractJSONFromScript(text)
		if jsonStr == "" {
			return
		}

		var data map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
			return
		}

		tweetObjects := jsonparse.FindByTypename(data, []string{
			"Tweet",
			"TweetWithVisibilityResults",
		}, 50)

		tweets = append(tweets, processTweetObjects(ctx, tweetObjects, job)...)
	})

	return tweets
}

func extractJSONFromScript(text string) string {
	// Try common patterns
	patterns := []struct {
		prefix string
		suffix string
	}{
		{"window.__INITIAL_STATE__=", ";"},
		{"window.__INITIAL_STATE__ = ", ";"},
		{`"data":`, ""},
	}

	for _, p := range patterns {
		if idx := strings.Index(text, p.prefix); idx >= 0 {
			start := idx + len(p.prefix)
			remaining := text[start:]

			// Find matching brace
			depth := 0
			end := -1
			for i, c := range remaining {
				if c == '{' {
					depth++
				} else if c == '}' {
					depth--
					if depth == 0 {
						end = i + 1
						break
					}
				}
			}

			if end > 0 {
				return remaining[:end]
			}
		}
	}

	// If whole content looks like JSON
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "{") && strings.HasSuffix(text, "}") {
		return text
	}

	return ""
}

func processTweetObjects(ctx context.Context, objects []map[string]any, job *TwitterJob) []*Tweet {
	var tweets []*Tweet
	seen := make(map[string]bool)

	for _, obj := range objects {
		tweet := extractTweet(obj, job)
		if tweet == nil || tweet.ID == "" {
			continue
		}
		if seen[tweet.ID] {
			continue
		}
		if job != nil && job.Deduper != nil && !job.Deduper.AddIfNotExists(ctx, tweet.ID) {
			continue
		}
		seen[tweet.ID] = true
		tweets = append(tweets, tweet)
	}

	return tweets
}

func extractTweet(obj map[string]any, job *TwitterJob) *Tweet {
	// Handle TweetWithVisibilityResults wrapper
	if jsonparse.GetString(obj, "__typename") == "TweetWithVisibilityResults" {
		obj = jsonparse.GetMap(obj, "tweet")
		if obj == nil {
			return nil
		}
	}

	// Get legacy data (contains most tweet info)
	legacy := jsonparse.GetMap(obj, "legacy")
	if legacy == nil {
		return nil
	}

	// Tweet ID
	id := jsonparse.GetString(obj, "rest_id")
	if id == "" {
		id = jsonparse.GetString(legacy, "id_str")
	}
	if id == "" {
		return nil
	}

	// Author info
	core := jsonparse.GetMap(obj, "core")
	userResults := jsonparse.GetMap(core, "user_results")
	userResult := jsonparse.GetMap(userResults, "result")
	if jsonparse.GetString(userResult, "__typename") == "UserWithVisibilityResults" {
		if u := jsonparse.GetMap(userResult, "user"); u != nil {
			userResult = u
		}
	}

	userCore := jsonparse.GetMap(userResult, "core")
	userLegacy := jsonparse.GetMap(userResult, "legacy")

	authorID := jsonparse.GetString(userResult, "rest_id")
	if authorID == "" {
		authorID = jsonparse.GetString(userResult, "id")
	}

	username := jsonparse.GetString(userCore, "screen_name")
	if username == "" {
		username = jsonparse.GetString(userLegacy, "screen_name")
	}
	if username == "" {
		username = jsonparse.GetString(userResult, "screen_name")
	}

	name := jsonparse.GetString(userCore, "name")
	if name == "" {
		name = jsonparse.GetString(userLegacy, "name")
	}
	if name == "" {
		name = jsonparse.GetString(userResult, "name")
	}

	bio := jsonparse.GetString(userResult, "profile_bio", "description")
	if bio == "" {
		bio = jsonparse.GetString(userLegacy, "description")
	}

	avatarURL := jsonparse.GetString(userResult, "avatar", "image_url")
	if avatarURL == "" {
		avatarURL = jsonparse.GetString(userLegacy, "profile_image_url_https")
	}

	bannerURL := jsonparse.GetString(userResult, "banner", "image_url")
	if bannerURL == "" {
		bannerURL = jsonparse.GetString(userLegacy, "profile_banner_url")
	}

	followersCount := jsonparse.GetInt(userResult, "relationship_counts", "followers")
	if followersCount == 0 {
		followersCount = jsonparse.GetInt(userLegacy, "followers_count")
	}

	followingCount := jsonparse.GetInt(userResult, "relationship_counts", "following")
	if followingCount == 0 {
		followingCount = jsonparse.GetInt(userLegacy, "friends_count")
	}

	tweetsCount := jsonparse.GetInt(userResult, "tweet_counts", "tweets")
	if tweetsCount == 0 {
		tweetsCount = jsonparse.GetInt(userLegacy, "statuses_count")
	}

	verified := jsonparse.GetBool(userResult, "verification", "verified") ||
		jsonparse.GetBool(userResult, "is_blue_verified") ||
		jsonparse.GetBool(userLegacy, "verified")

	if username == "" && job != nil {
		username = job.Username
	}
	if username == "" {
		username = "i"
	}

	author := Profile{
		ID:             authorID,
		Username:       username,
		Name:           name,
		Bio:            bio,
		AvatarURL:      avatarURL,
		BannerURL:      bannerURL,
		FollowersCount: followersCount,
		FollowingCount: followingCount,
		TweetsCount:    tweetsCount,
		Verified:       verified,
	}

	// Metrics
	metrics := Metrics{
		Likes:     jsonparse.GetInt(legacy, "favorite_count"),
		Retweets:  jsonparse.GetInt(legacy, "retweet_count"),
		Replies:   jsonparse.GetInt(legacy, "reply_count"),
		Bookmarks: jsonparse.GetInt(legacy, "bookmark_count"),
	}

	// Views from separate object
	views := jsonparse.GetMap(obj, "views")
	if views != nil {
		metrics.Views = jsonparse.GetInt(views, "count")
	}

	// Media URLs
	var media []string
	extMedia := jsonparse.GetMap(legacy, "extended_entities")
	if extMedia != nil {
		mediaList := jsonparse.GetSlice(extMedia, "media")
		for _, m := range mediaList {
			if mMap, ok := m.(map[string]any); ok {
				url := jsonparse.GetString(mMap, "media_url_https")
				if url != "" {
					media = append(media, url)
				}
			}
		}
	}

	// Tweet URL
	var tweetURL string
	if author.Username != "" && author.Username != "i" {
		tweetURL = "https://x.com/" + author.Username + "/status/" + id
	} else {
		tweetURL = "https://x.com/i/status/" + id
	}

	// Detect reply/retweet
	isReply := jsonparse.GetString(legacy, "in_reply_to_status_id_str") != ""
	isRetweet := strings.HasPrefix(jsonparse.GetString(legacy, "full_text"), "RT @")

	// Full text, with note_tweet expansion for long posts
	fullText := jsonparse.GetString(legacy, "full_text")
	if noteTweet := jsonparse.GetMap(obj, "note_tweet"); noteTweet != nil {
		if ntRes := jsonparse.GetMap(noteTweet, "note_tweet_results", "result"); ntRes != nil {
			if fullNote := jsonparse.GetString(ntRes, "text"); fullNote != "" {
				fullText = fullNote
			}
		}
	}
	if fullText == "" {
		fullText = jsonparse.GetString(obj, "text")
	}

	return &Tweet{
		ID:        id,
		URL:       tweetURL,
		Author:    author,
		Text:      fullText,
		CreatedAt: jsonparse.GetTime(legacy, "created_at"),
		Media:     media,
		Metrics:   metrics,
		IsReply:   isReply,
		IsRetweet: isRetweet,
	}
}

func extractTweetsFromDOM(ctx context.Context, doc *goquery.Document, job *TwitterJob) []*Tweet {
	var tweets []*Tweet
	seen := make(map[string]bool)

	doc.Find("article").Each(func(_ int, s *goquery.Selection) {
		var (
			tweetID  string
			tweetURL string
			username string
		)

		s.Find(`a[href*="/status/"]`).Each(func(_ int, a *goquery.Selection) {
			if tweetID != "" {
				return
			}

			href, exists := a.Attr("href")
			if !exists {
				return
			}

			matches := statusIDRegex.FindStringSubmatch(href)
			if len(matches) > 1 {
				tweetID = matches[1]
				if strings.HasPrefix(href, "http") {
					tweetURL = href
				} else {
					tweetURL = "https://x.com" + href
				}

				parts := strings.Split(strings.Trim(href, "/"), "/")
				if len(parts) >= 3 && parts[1] == "status" && parts[0] != "i" {
					username = parts[0]
				}
			}
		})

		if tweetID == "" || seen[tweetID] {
			return
		}

		if job != nil && job.Deduper != nil && !job.Deduper.AddIfNotExists(ctx, tweetID) {
			return
		}

		seen[tweetID] = true

		userElem := s.Find(`div[data-testid="User-Name"]`)
		name := ""
		if userElem.Length() > 0 {
			nameElem := userElem.Find("span").First()
			name = strings.TrimSpace(nameElem.Text())
			userElem.Find(`a[href*="/"]`).Each(func(_ int, a *goquery.Selection) {
				href, _ := a.Attr("href")
				clean := strings.Trim(href, "/")
				if clean != "" && !strings.Contains(clean, "/") && clean != "i" && username == "" {
					username = clean
				}
			})
		}

		if username == "" || username == "i" {
			s.Find(`a[href^="/"]`).Each(func(_ int, a *goquery.Selection) {
				if username != "" && username != "i" {
					return
				}

				href, _ := a.Attr("href")
				clean := strings.Trim(href, "/")
				if clean != "" && !strings.Contains(clean, "/") && clean != "i" &&
					clean != "home" && clean != "explore" && clean != "notifications" && clean != "messages" {
					username = clean
				}
			})
		}

		if name == "" {
			name = strings.TrimSpace(s.Find(`a[href^="/"] span`).First().Text())
		}

		if username == "" && job != nil {
			username = job.Username
		}

		if username == "" {
			username = "i"
		}

		if username != "i" && strings.Contains(tweetURL, "/i/status/") {
			tweetURL = "https://x.com/" + username + "/status/" + tweetID
		}

		text := strings.TrimSpace(s.Find(`div[data-testid="tweetText"]`).Text())
		if text == "" {
			text = strings.TrimSpace(s.Find(`div[dir="auto"]`).First().Text())
		}

		var createdAt time.Time
		if timeElem := s.Find("time"); timeElem.Length() > 0 {
			if dt, exists := timeElem.Attr("datetime"); exists {
				if t, err := time.Parse(time.RFC3339, dt); err == nil {
					createdAt = t.UTC()
				} else if t, err := time.Parse(time.RFC3339Nano, dt); err == nil {
					createdAt = t.UTC()
				}
			}
		}

		metrics := Metrics{
			Replies:  parseMetricCount(s.Find(`[data-testid="reply"]`).Text()),
			Retweets: parseMetricCount(s.Find(`[data-testid="retweet"]`).Text()),
			Likes:    parseMetricCount(s.Find(`[data-testid="like"]`).Text()),
		}
		if bm := s.Find(`[data-testid="bookmark"]`); bm.Length() > 0 {
			metrics.Bookmarks = parseMetricCount(bm.Text())
		}
		if views := s.Find(`a[href*="/analytics"]`); views.Length() > 0 {
			metrics.Views = parseMetricCount(views.Text())
		}

		var media []string
		s.Find(`img[src*="pbs.twimg.com/media/"]`).Each(func(_ int, img *goquery.Selection) {
			if src, exists := img.Attr("src"); exists {
				media = append(media, src)
			}
		})

		isRetweet := strings.Contains(s.Find(`[data-testid="socialContext"]`).Text(), "Retweeted") ||
			strings.HasPrefix(text, "RT @")
		isReply := s.Find(`div:contains("Replying to")`).Length() > 0
		verified := s.Find(`svg[data-testid="icon-verified"]`).Length() > 0 ||
			s.Find(`svg[data-icon="icon-verified-premium-color"]`).Length() > 0

		author := Profile{
			Username: username,
			Name:     name,
			Verified: verified,
		}

		tweets = append(tweets, &Tweet{
			ID:        tweetID,
			URL:       tweetURL,
			Author:    author,
			Text:      text,
			CreatedAt: createdAt,
			Media:     media,
			Metrics:   metrics,
			IsReply:   isReply,
			IsRetweet: isRetweet,
		})
	})

	return tweets
}

func parseMetricCount(s string) int {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	if s == "" {
		return 0
	}
	multiplier := 1.0
	lower := strings.ToLower(s)
	if strings.HasSuffix(lower, "k") {
		multiplier = 1000
		s = s[:len(s)-1]
	} else if strings.HasSuffix(lower, "m") {
		multiplier = 1000000
		s = s[:len(s)-1]
	}
	val, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return int(val * multiplier)
}
