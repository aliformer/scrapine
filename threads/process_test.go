package threads

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/gosom/scrapemate"
)

func TestParseThreadsResponse(t *testing.T) {
	// JSON embedded in script tag, matching Threads' actual format
	html := `<html><body>
<script type="application/json">
{
  "data": {
    "edges": [{
      "node": {
        "__typename": "XDTThreadItem",
        "thread_items": [{
          "post": {
            "pk": "post123",
            "code": "ABC123",
            "user": {
              "pk": "user456",
              "username": "ThreadsUser",
              "full_name": "Threads User",
              "is_verified": "false"
            },
            "caption": {
              "text": "Hello Threads"
            },
            "taken_at": 1696694400,
            "like_count": 100,
            "text_post_app_info": {
              "direct_reply_count": 5,
              "share_info": {
                "repost_count": 10,
                "quote_count": 3
              }
            }
          }
        }]
      }
    }]
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

	job := NewThreadsJob("test-1", "ThreadsUser", 1)
	res, _, err := parseThreadsResponse(context.Background(), resp, job)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	posts, ok := res.([]*Post)
	if !ok || len(posts) == 0 {
		t.Fatalf("expected posts, got %v", res)
	}

	p := posts[0]
	if p.Text != "Hello Threads" {
		t.Errorf("expected text 'Hello Threads', got %q", p.Text)
	}
	if p.ID != "post123" {
		t.Errorf("expected ID 'post123', got %q", p.ID)
	}
	if p.Author.Username != "ThreadsUser" {
		t.Errorf("expected username 'ThreadsUser', got %q", p.Author.Username)
	}
	if p.Metrics.Likes != 100 {
		t.Errorf("expected 100 likes, got %d", p.Metrics.Likes)
	}
}

func TestParseThreadsResponse_GraphQLIntercepted(t *testing.T) {
	resp := &scrapemate.Response{
		Meta: map[string]any{
			"graphql_threads": []map[string]any{
				{
					"data": map[string]any{
						"node": map[string]any{
							"__typename": "XDTThreadItem",
							"post": map[string]any{
								"pk":   "th_graphql_555",
								"code": "XYZ987",
								"user": map[string]any{
									"username":    "gqlthreadsuser",
									"is_verified": true,
								},
								"caption": map[string]any{
									"text": "Intercepted Threads Post",
								},
								"like_count": float64(450),
								"text_post_app_info": map[string]any{
									"direct_reply_count": float64(15),
									"share_info": map[string]any{
										"repost_count": float64(8),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	job := NewThreadsJob("test-th-gql", "gqlthreadsuser", 1)
	res, _, err := parseThreadsResponse(context.Background(), resp, job)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	posts, ok := res.([]*Post)
	if !ok || len(posts) != 1 {
		t.Fatalf("expected 1 post, got %v", res)
	}

	if posts[0].ID != "th_graphql_555" {
		t.Errorf("expected ID th_graphql_555, got %q", posts[0].ID)
	}
	if !posts[0].Author.Verified {
		t.Errorf("expected Author.Verified = true, got false")
	}
	if posts[0].Metrics.Likes != 450 {
		t.Errorf("expected 450 likes, got %d", posts[0].Metrics.Likes)
	}
	if posts[0].Metrics.Replies != 15 {
		t.Errorf("expected 15 replies, got %d", posts[0].Metrics.Replies)
	}
	if posts[0].Text != "Intercepted Threads Post" {
		t.Errorf("expected text 'Intercepted Threads Post', got %q", posts[0].Text)
	}
}

func TestParseThreadsResponse_DOMFallback(t *testing.T) {
	html := `<html><body>
<div data-pressable-container="true">
  <a href="/@domthreadsuser">@domthreadsuser</a>
  <a href="https://www.threads.net/@domthreadsuser/post/C4XyZ123">Post Link</a>
  <div dir="auto">DOM Fallback Threads Message Text</div>
  <button aria-label="Like">250</button>
  <button aria-label="Reply">42</button>
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

	job := NewThreadsJob("test-dom", "domthreadsuser", 1)
	res, _, err := parseThreadsResponse(context.Background(), resp, job)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	posts, ok := res.([]*Post)
	if !ok || len(posts) != 1 {
		t.Fatalf("expected 1 post, got %v", res)
	}

	if posts[0].ID != "C4XyZ123" {
		t.Errorf("expected ID C4XyZ123, got %q", posts[0].ID)
	}
	if posts[0].Author.Username != "domthreadsuser" {
		t.Errorf("expected author 'domthreadsuser', got %q", posts[0].Author.Username)
	}
	if posts[0].Text != "DOM Fallback Threads Message Text" {
		t.Errorf("expected text, got %q", posts[0].Text)
	}
	if posts[0].Metrics.Likes != 250 {
		t.Errorf("expected 250 likes, got %d", posts[0].Metrics.Likes)
	}
	if posts[0].Metrics.Replies != 42 {
		t.Errorf("expected 42 replies, got %d", posts[0].Metrics.Replies)
	}
}
