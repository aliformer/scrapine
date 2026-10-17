package facebook

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/gosom/scrapemate"
)

func TestParseFacebookResponse(t *testing.T) {
	// JSON embedded in script tag, matching Facebook's actual format
	html := `<html><body>
<script type="application/json">
{
  "data": {
    "node": {
      "__typename": "Story",
      "id": "post123",
      "url": "https://www.facebook.com/post123",
      "creation_time": 1696694400,
      "actors": [{
        "id": "user456",
        "name": "AuthorName",
        "username": "fbuser"
      }],
      "message": {
        "text": "Sample FB post"
      },
      "feedback": {
        "reactors": {
          "count": 50
        },
        "comment_count": {
          "total_count": 10
        },
        "share_count": {
          "count": 5
        }
      }
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

	job := NewFacebookJob("test-1", "fbuser", "", 1)
	res, _, err := parseFacebookResponse(context.Background(), resp, job)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	posts, ok := res.([]*Post)
	if !ok || len(posts) == 0 {
		t.Fatalf("expected posts, got %v", res)
	}

	p := posts[0]
	if p.Text != "Sample FB post" {
		t.Errorf("expected text 'Sample FB post', got %q", p.Text)
	}
	if p.ID != "post123" {
		t.Errorf("expected ID 'post123', got %q", p.ID)
	}
	if p.Author.Name != "AuthorName" {
		t.Errorf("expected author name 'AuthorName', got %q", p.Author.Name)
	}
	if p.Metrics.Likes != 50 {
		t.Errorf("expected 50 likes, got %d", p.Metrics.Likes)
	}
}

func TestParseFacebookResponse_GraphQLIntercepted(t *testing.T) {
	resp := &scrapemate.Response{
		Meta: map[string]any{
			"graphql_posts": []map[string]any{
				{
					"data": map[string]any{
						"node": map[string]any{
							"__typename": "Story",
							"id":         "fb_graphql_999",
							"url":        "https://www.facebook.com/fb_graphql_999",
							"actors": []any{
								map[string]any{
									"name":     "GraphQL Author",
									"username": "gqluser",
								},
							},
							"message": map[string]any{
								"text": "Intercepted FB GraphQL Post",
							},
							"feedback": map[string]any{
								"reactors": map[string]any{
									"count": float64(120),
								},
								"comment_count": map[string]any{
									"total_count": float64(30),
								},
							},
						},
					},
				},
			},
		},
	}

	job := NewFacebookJob("test-gql", "gqluser", "", 1)
	res, _, err := parseFacebookResponse(context.Background(), resp, job)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	posts, ok := res.([]*Post)
	if !ok || len(posts) != 1 {
		t.Fatalf("expected 1 post, got %v", res)
	}

	if posts[0].ID != "fb_graphql_999" {
		t.Errorf("expected ID fb_graphql_999, got %q", posts[0].ID)
	}
	if posts[0].Metrics.Likes != 120 {
		t.Errorf("expected 120 likes, got %d", posts[0].Metrics.Likes)
	}
	if posts[0].Metrics.Comments != 30 {
		t.Errorf("expected 30 comments, got %d", posts[0].Metrics.Comments)
	}
	if posts[0].Text != "Intercepted FB GraphQL Post" {
		t.Errorf("expected text 'Intercepted FB GraphQL Post', got %q", posts[0].Text)
	}
}

func TestParseFacebookResponse_DOMFallback(t *testing.T) {
	html := `<html><body>
<div role="article">
  <h2><a role="link" href="/fbcreator"><span>FB Creator Name</span></a></h2>
  <a role="link" href="https://www.facebook.com/posts/987654321098">Permalink</a>
  <div dir="auto">DOM Fallback Facebook Post Text Content</div>
  <span aria-label="1.2K reactions">1.2K</span>
  <span aria-label="350 comments">350</span>
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

	job := NewFacebookJob("test-dom", "fbcreator", "", 1)
	res, _, err := parseFacebookResponse(context.Background(), resp, job)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	posts, ok := res.([]*Post)
	if !ok || len(posts) != 1 {
		t.Fatalf("expected 1 post, got %v", res)
	}

	if posts[0].ID != "987654321098" {
		t.Errorf("expected ID 987654321098, got %q", posts[0].ID)
	}
	if posts[0].Author.Name != "FB Creator Name" {
		t.Errorf("expected author 'FB Creator Name', got %q", posts[0].Author.Name)
	}
	if posts[0].Text != "DOM Fallback Facebook Post Text Content" {
		t.Errorf("expected post text, got %q", posts[0].Text)
	}
	if posts[0].Metrics.Likes != 1200 {
		t.Errorf("expected 1200 likes, got %d", posts[0].Metrics.Likes)
	}
	if posts[0].Metrics.Comments != 350 {
		t.Errorf("expected 350 comments, got %d", posts[0].Metrics.Comments)
	}
}
