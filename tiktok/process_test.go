package tiktok

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/gosom/scrapemate"
)

func TestParseTikTokResponse(t *testing.T) {
	// JSON embedded in script tag, matching TikTok's actual format
	html := `<html><body>
<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">
{
  "__DEFAULT_SCOPE__": {
    "webapp.user-detail": {
      "userInfo": {
        "user": {
          "id": "user123",
          "uniqueId": "ttuser",
          "nickname": "TikTok User"
        }
      },
      "itemList": [{
        "id": "video123",
        "desc": "Cool TikTok video",
        "createTime": 1696694400,
        "author": {
          "id": "user123",
          "uniqueId": "ttuser",
          "nickname": "TikTok User",
          "avatarLarger": "https://example.com/avatar.jpg"
        },
        "stats": {
          "playCount": 10000,
          "diggCount": 500,
          "commentCount": 50,
          "shareCount": 25
        },
        "video": {
          "playAddr": "https://example.com/video.mp4"
        }
      }]
    }
  }
}
</script>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := &scrapemate.Response{
		Document: doc,
	}

	job := NewTikTokJob("test-1", "ttuser", "", 1)
	res, _, err := parseTikTokResponse(context.Background(), resp, job)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	videos, ok := res.([]*Video)
	if !ok || len(videos) == 0 {
		t.Fatalf("expected videos, got %v", res)
	}

	v := videos[0]
	if v.Description != "Cool TikTok video" {
		t.Errorf("expected description 'Cool TikTok video', got %q", v.Description)
	}
	if v.ID != "video123" {
		t.Errorf("expected ID 'video123', got %q", v.ID)
	}
	if v.Author.Username != "ttuser" {
		t.Errorf("expected username 'ttuser', got %q", v.Author.Username)
	}
	if v.Metrics.Views != 10000 {
		t.Errorf("expected 10000 views, got %d", v.Metrics.Views)
	}
	if v.Metrics.Likes != 500 {
		t.Errorf("expected 500 likes, got %d", v.Metrics.Likes)
	}
}

func TestParseTikTokResponse_APIIntercepted(t *testing.T) {
	resp := &scrapemate.Response{
		Meta: map[string]any{
			"api_videos": []map[string]any{
				{
					"itemList": []any{
						map[string]any{
							"id":   "api_video_100",
							"desc": "Intercepted API Video",
							"author": map[string]any{
								"uniqueId": "apicreator",
							},
							"stats": map[string]any{
								"playCount": float64(25000),
								"diggCount": float64(1200),
							},
						},
					},
				},
			},
		},
	}

	job := NewTikTokJob("test-api", "apicreator", "", 1)
	res, _, err := parseTikTokResponse(context.Background(), resp, job)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	videos, ok := res.([]*Video)
	if !ok || len(videos) != 1 {
		t.Fatalf("expected 1 video, got %v", res)
	}

	if videos[0].ID != "api_video_100" {
		t.Errorf("expected ID api_video_100, got %q", videos[0].ID)
	}
	if videos[0].Metrics.Views != 25000 {
		t.Errorf("expected 25000 views, got %d", videos[0].Metrics.Views)
	}
}

func TestParseTikTokResponse_DOMFallback(t *testing.T) {
	html := `<html><body>
<div data-e2e="user-post-item">
  <a href="https://www.tiktok.com/@domcreator/video/7123456789012345678" title="DOM Fallback Video Title">
    <div data-e2e="user-post-item-desc">DOM Fallback Video Description</div>
    <strong data-e2e="video-views">15.5K</strong>
  </a>
</div>
</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp := &scrapemate.Response{
		Document: doc,
		Body:     []byte(html),
	}

	job := NewTikTokJob("test-dom", "domcreator", "", 1)
	res, _, err := parseTikTokResponse(context.Background(), resp, job)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	videos, ok := res.([]*Video)
	if !ok || len(videos) != 1 {
		t.Fatalf("expected 1 video, got %v", res)
	}

	if videos[0].ID != "7123456789012345678" {
		t.Errorf("expected ID 7123456789012345678, got %q", videos[0].ID)
	}
	if videos[0].Metrics.Views != 15500 {
		t.Errorf("expected 15500 views, got %d", videos[0].Metrics.Views)
	}
	if videos[0].Description != "DOM Fallback Video Description" {
		t.Errorf("expected 'DOM Fallback Video Description', got %q", videos[0].Description)
	}
}
