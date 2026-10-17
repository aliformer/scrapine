package facebook

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
	fbPostIDRegex    = regexp.MustCompile(`(?:/posts/|/permalink/|/videos/|fbid=|story_fbid=)(\d+)`)
	metricKMBRegexFB = regexp.MustCompile(`(?i)([\d.]+)\s*([KMB])?`)
)

func parseFacebookResponse(ctx context.Context, resp *scrapemate.Response, job *FacebookJob) (any, []scrapemate.IJob, error) {
	var posts []*Post
	var nextJobs []scrapemate.IJob

	if resp.Error != nil {
		return posts, nextJobs, resp.Error
	}

	// 1. Intercepted GraphQL responses
	if resp.Meta != nil {
		if captured, ok := resp.Meta["graphql_posts"].([]map[string]any); ok && len(captured) > 0 {
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

func extractPostsFromGraphQL(ctx context.Context, responses []map[string]any, job *FacebookJob) []*Post {
	var posts []*Post
	seen := make(map[string]bool)

	for _, respMap := range responses {
		storyObjects := jsonparse.FindByTypename(respMap, []string{
			"Story",
			"Post",
			"UserPost",
			"PagePost",
			"CometFeedUnit",
		}, 50)

		for _, obj := range storyObjects {
			post := extractPostFromStory(obj, job)
			if post != nil && post.ID != "" && !seen[post.ID] {
				if job.Deduper != nil && !job.Deduper.AddIfNotExists(ctx, post.ID) {
					continue
				}
				seen[post.ID] = true
				posts = append(posts, post)
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
	matches := metricKMBRegexFB.FindStringSubmatch(s)
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

func extractPostsFromDOM(ctx context.Context, doc *goquery.Document, job *FacebookJob) []*Post {
	var posts []*Post
	seen := make(map[string]bool)

	pageAuthor := job.Username

	doc.Find(`div[role="article"], div[data-pagelet*="FeedUnit"]`).Each(func(i int, s *goquery.Selection) {
		var postURL, postID string
		s.Find(`a[href*="/posts/"], a[href*="/permalink/"], a[href*="/videos/"], a[href*="fbid="], a[role="link"][href*="facebook.com"]`).Each(func(_ int, link *goquery.Selection) {
			if postID != "" {
				return
			}
			href, _ := link.Attr("href")
			if m := fbPostIDRegex.FindStringSubmatch(href); len(m) >= 2 {
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
			postURL = "https://www.facebook.com" + postURL
		}

		authorName := pageAuthor
		if headerLink := s.Find(`h2 a, h3 a, h4 a, strong a, a[role="link"] span[dir="auto"]`).First(); headerLink.Length() > 0 {
			if name := strings.TrimSpace(headerLink.Text()); name != "" {
				authorName = name
			}
		}

		text := ""
		if msgEl := s.Find(`div[dir="auto"], div[data-ad-preview="message"]`).First(); msgEl.Length() > 0 {
			text = strings.TrimSpace(msgEl.Text())
		}

		var metrics Metrics
		s.Find(`span[aria-label], div[aria-label]`).Each(func(_ int, el *goquery.Selection) {
			label, _ := el.Attr("aria-label")
			lower := strings.ToLower(label)
			switch {
			case strings.Contains(lower, "reaction") || strings.Contains(lower, "like"):
				metrics.Likes = parseMetricCount(label)
			case strings.Contains(lower, "comment"):
				metrics.Comments = parseMetricCount(label)
			case strings.Contains(lower, "share"):
				metrics.Shares = parseMetricCount(label)
			}
		})

		post := &Post{
			ID:  postID,
			URL: postURL,
			Author: Profile{
				Username: authorName,
				Name:     authorName,
			},
			Text:    text,
			Metrics: metrics,
		}
		posts = append(posts, post)
	})

	return posts
}

func extractPostsFromScripts(ctx context.Context, doc *goquery.Document, job *FacebookJob) []*Post {
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

		// Look for Story or Post objects (Facebook's internal types)
		storyObjects := jsonparse.FindByTypename(data, []string{
			"Story",
			"Post",
			"UserPost",
			"PagePost",
		}, 50)

		for _, obj := range storyObjects {
			post := extractPostFromStory(obj, job)
			if post != nil {
				posts = append(posts, post)
			}
		}
	})

	// Also search in regular script tags for embedded JSON
	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		text := s.Text()
		if !strings.Contains(text, `"__typename"`) {
			return
		}

		// Try to extract JSON from script content
		jsonStr := extractJSONFromScript(text)
		if jsonStr == "" {
			return
		}

		var data map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
			return
		}

		storyObjects := jsonparse.FindByTypename(data, []string{
			"Story",
			"Post",
			"UserPost",
			"PagePost",
		}, 50)

		for _, obj := range storyObjects {
			post := extractPostFromStory(obj, job)
			if post != nil {
				posts = append(posts, post)
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

func extractJSONFromScript(text string) string {
	// Find JSON object boundaries
	text = strings.TrimSpace(text)

	// Look for assignment patterns
	patterns := []string{
		"require(\"ServerJS\").handle(",
		"bigPipe.onPageletArrive(",
		"new (require(\"ServerJS\"))",
	}

	for _, p := range patterns {
		if idx := strings.Index(text, p); idx >= 0 {
			start := idx + len(p)
			remaining := text[start:]

			// Find the JSON object
			braceIdx := strings.Index(remaining, "{")
			if braceIdx >= 0 {
				remaining = remaining[braceIdx:]
				if end := findMatchingBrace(remaining); end > 0 {
					return remaining[:end]
				}
			}
		}
	}

	// Direct JSON
	if strings.HasPrefix(text, "{") {
		if end := findMatchingBrace(text); end > 0 {
			return text[:end]
		}
	}

	return ""
}

func findMatchingBrace(s string) int {
	depth := 0
	for i, c := range s {
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func extractPostFromStory(obj map[string]any, job *FacebookJob) *Post {
	// ID
	id := jsonparse.GetString(obj, "id")
	if id == "" {
		id = jsonparse.GetString(obj, "post_id")
	}
	if id == "" {
		return nil
	}

	// Author from actors array or comet_sections
	var author Profile

	// Try actors array first
	actors := jsonparse.GetSlice(obj, "actors")
	if len(actors) > 0 {
		if actorMap, ok := actors[0].(map[string]any); ok {
			author = Profile{
				ID:        jsonparse.GetString(actorMap, "id"),
				Name:      jsonparse.GetString(actorMap, "name"),
				Username:  jsonparse.GetString(actorMap, "username"),
				URL:       jsonparse.GetString(actorMap, "url"),
				AvatarURL: jsonparse.GetString(actorMap, "profile_picture", "uri"),
			}
		}
	}

	// Try owner
	if author.ID == "" {
		owner := jsonparse.GetMap(obj, "owner")
		if owner != nil {
			author = Profile{
				ID:        jsonparse.GetString(owner, "id"),
				Name:      jsonparse.GetString(owner, "name"),
				Username:  jsonparse.GetString(owner, "username"),
				URL:       jsonparse.GetString(owner, "url"),
				AvatarURL: jsonparse.GetString(owner, "profile_picture", "uri"),
			}
		}
	}

	if author.ID == "" {
		author = Profile{Username: job.Username}
	}

	// Text content
	text := ""

	// Try message object
	message := jsonparse.GetMap(obj, "message")
	if message != nil {
		text = jsonparse.GetString(message, "text")
	}

	// Try comet_sections path
	if text == "" {
		cometSections := jsonparse.GetMap(obj, "comet_sections")
		if cometSections != nil {
			content := jsonparse.GetMap(cometSections, "content")
			story := jsonparse.GetMap(content, "story")
			cometMsg := jsonparse.GetMap(story, "comet_sections", "message")
			if cometMsg != nil {
				msgStory := jsonparse.GetMap(cometMsg, "story")
				msgObj := jsonparse.GetMap(msgStory, "message")
				text = jsonparse.GetString(msgObj, "text")
			}
		}
	}

	// Metrics from feedback object
	var metrics Metrics
	feedback := jsonparse.GetMap(obj, "feedback")
	if feedback != nil {
		// Reactions/likes
		reactors := jsonparse.GetMap(feedback, "reactors")
		if reactors != nil {
			metrics.Likes = jsonparse.GetInt(reactors, "count")
		}
		if metrics.Likes == 0 {
			metrics.Likes = jsonparse.GetInt(feedback, "reaction_count", "count")
		}

		// Comments
		commentCount := jsonparse.GetMap(feedback, "comment_count")
		if commentCount != nil {
			metrics.Comments = jsonparse.GetInt(commentCount, "total_count")
		}

		// Shares
		shareCount := jsonparse.GetMap(feedback, "share_count")
		if shareCount != nil {
			metrics.Shares = jsonparse.GetInt(shareCount, "count")
		}
	}

	// Media URLs
	var media []string
	attachments := jsonparse.GetSlice(obj, "attachments")
	for _, att := range attachments {
		if attMap, ok := att.(map[string]any); ok {
			mediaObj := jsonparse.GetMap(attMap, "media")
			if mediaObj != nil {
				// Image
				image := jsonparse.GetMap(mediaObj, "image")
				if image != nil {
					if uri := jsonparse.GetString(image, "uri"); uri != "" {
						media = append(media, uri)
					}
				}
				// Photo image
				if uri := jsonparse.GetString(mediaObj, "photo_image", "uri"); uri != "" {
					media = append(media, uri)
				}
			}
		}
	}

	// Post URL
	postURL := jsonparse.GetString(obj, "url")
	if postURL == "" {
		postURL = "https://www.facebook.com/" + id
	}

	// Timestamp
	createdAt := jsonparse.GetTime(obj, "creation_time")
	if createdAt.IsZero() {
		createdAt = jsonparse.GetTime(obj, "created_time")
	}

	return &Post{
		ID:        id,
		URL:       postURL,
		Author:    author,
		Text:      text,
		CreatedAt: createdAt,
		Media:     media,
		Metrics:   metrics,
	}
}
