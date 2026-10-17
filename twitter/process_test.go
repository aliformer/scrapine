package twitter

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/gosom/scrapemate"

	"github.com/gosom/google-maps-scraper/deduper"
)

func TestParseTwitterResponse(t *testing.T) {
	// JSON embedded in script tag, matching Twitter's actual format
	html := `<html><body>
<script id="__NEXT_DATA__" type="application/json">
{
  "props": {
    "pageProps": {
      "timeline": {
        "instructions": [{
          "entries": [{
            "content": {
              "itemContent": {
                "tweet_results": {
                  "result": {
                    "__typename": "Tweet",
                    "rest_id": "123456789",
                    "core": {
                      "user_results": {
                        "result": {
                          "rest_id": "987654321",
                          "legacy": {
                            "screen_name": "TwitterUser",
                            "name": "Twitter User"
                          }
                        }
                      }
                    },
                    "legacy": {
                      "id_str": "123456789",
                      "full_text": "Hello World Tweet",
                      "created_at": "Mon Oct 07 12:00:00 +0000 2024",
                      "favorite_count": 42,
                      "retweet_count": 10,
                      "reply_count": 5
                    },
                    "views": {
                      "count": "1000"
                    }
                  }
                }
              }
            }
          }]
        }]
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

	job := NewTwitterJob("test-1", "TwitterUser", "", 1)
	res, _, err := parseTwitterResponse(context.Background(), resp, job)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	tweets, ok := res.([]*Tweet)
	if !ok || len(tweets) == 0 {
		t.Fatalf("expected tweets, got %v", res)
	}

	tw := tweets[0]
	if tw.Text != "Hello World Tweet" {
		t.Errorf("expected text 'Hello World Tweet', got %q", tw.Text)
	}
	if tw.ID != "123456789" {
		t.Errorf("expected ID '123456789', got %q", tw.ID)
	}
	if tw.Author.Username != "TwitterUser" {
		t.Errorf("expected username 'TwitterUser', got %q", tw.Author.Username)
	}
	if tw.Metrics.Likes != 42 {
		t.Errorf("expected 42 likes, got %d", tw.Metrics.Likes)
	}
}

func TestParseTwitterResponse_GraphQLIntercepted(t *testing.T) {
	// Simulated intercepted GraphQL payload from X.com
	payload := map[string]any{
		"data": map[string]any{
			"user": map[string]any{
				"result": map[string]any{
					"timeline_v2": map[string]any{
						"timeline": map[string]any{
							"instructions": []any{
								map[string]any{
									"type": "TimelineAddEntries",
									"entries": []any{
										map[string]any{
											"entryId": "tweet-1122334455",
											"content": map[string]any{
												"itemContent": map[string]any{
													"tweet_results": map[string]any{
														"result": map[string]any{
															"__typename": "Tweet",
															"rest_id":    "1122334455",
															"core": map[string]any{
																"user_results": map[string]any{
																	"result": map[string]any{
																		"rest_id": "998877",
																		"legacy": map[string]any{
																			"screen_name": "graphql_expert",
																			"name":        "GraphQL Expert",
																			"verified":    true, // boolean verified
																		},
																	},
																},
															},
															"legacy": map[string]any{
																"id_str":         "1122334455",
																"full_text":      "GraphQL Intercepted tweet works!",
																"created_at":     "Tue Oct 15 14:00:00 +0000 2024",
																"favorite_count": 88,
																"retweet_count":  22,
																"reply_count":    9,
															},
															"views": map[string]any{
																"count": 5432,
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	resp := &scrapemate.Response{
		Meta: map[string]any{
			"graphql_tweets": []map[string]any{payload},
		},
	}

	job := NewTwitterJob("test-graphql", "graphql_expert", "", 1)
	res, _, err := parseTwitterResponse(context.Background(), resp, job)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	tweets, ok := res.([]*Tweet)
	if !ok || len(tweets) != 1 {
		t.Fatalf("expected 1 tweet, got %v", res)
	}

	tw := tweets[0]
	if tw.ID != "1122334455" {
		t.Errorf("expected ID '1122334455', got %q", tw.ID)
	}
	if tw.Text != "GraphQL Intercepted tweet works!" {
		t.Errorf("expected text 'GraphQL Intercepted tweet works!', got %q", tw.Text)
	}
	if tw.Author.Username != "graphql_expert" {
		t.Errorf("expected username 'graphql_expert', got %q", tw.Author.Username)
	}
	if !tw.Author.Verified {
		t.Errorf("expected author to be verified (boolean verified: true)")
	}
	if tw.Metrics.Likes != 88 {
		t.Errorf("expected 88 likes, got %d", tw.Metrics.Likes)
	}
	if tw.Metrics.Views != 5432 {
		t.Errorf("expected 5432 views, got %d", tw.Metrics.Views)
	}
}

func TestParseTwitterResponse_DOMFallback(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
  <div id="react-root">
    <section>
      <article data-testid="tweet">
        <div data-testid="User-Name">
          <span>Official Twitter</span>
          <a href="/TwitterUser"><span>@TwitterUser</span></a>
          <svg data-testid="icon-verified"></svg>
        </div>
        <a href="/TwitterUser/status/9876543210123" role="link">
          <time datetime="2024-10-15T10:00:00Z">Oct 15</time>
        </a>
        <div data-testid="tweetText">DOM rendered tweet test content</div>
        <button data-testid="reply">12</button>
        <button data-testid="retweet">34</button>
        <button data-testid="like">1.5K</button>
        <div data-testid="bookmark">7</div>
        <a href="/TwitterUser/status/9876543210123/analytics">10K</a>
        <img src="https://pbs.twimg.com/media/test_image.jpg" />
      </article>
    </section>
  </div>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("unexpected goquery error: %v", err)
	}

	resp := &scrapemate.Response{
		Document: doc,
	}

	job := NewTwitterJob("test-dom", "TwitterUser", "", 1)
	res, _, err := parseTwitterResponse(context.Background(), resp, job)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	tweets, ok := res.([]*Tweet)
	if !ok || len(tweets) != 1 {
		t.Fatalf("expected 1 tweet, got %v", res)
	}

	tw := tweets[0]
	if tw.ID != "9876543210123" {
		t.Errorf("expected ID '9876543210123', got %q", tw.ID)
	}
	if tw.Text != "DOM rendered tweet test content" {
		t.Errorf("expected text 'DOM rendered tweet test content', got %q", tw.Text)
	}
	if tw.Author.Username != "TwitterUser" {
		t.Errorf("expected username 'TwitterUser', got %q", tw.Author.Username)
	}
	if tw.Author.Name != "Official Twitter" {
		t.Errorf("expected name 'Official Twitter', got %q", tw.Author.Name)
	}
	if !tw.Author.Verified {
		t.Errorf("expected author to be verified")
	}
	if tw.Metrics.Likes != 1500 {
		t.Errorf("expected 1500 likes, got %d", tw.Metrics.Likes)
	}
	if tw.Metrics.Replies != 12 {
		t.Errorf("expected 12 replies, got %d", tw.Metrics.Replies)
	}
	if tw.Metrics.Retweets != 34 {
		t.Errorf("expected 34 retweets, got %d", tw.Metrics.Retweets)
	}
	if tw.Metrics.Bookmarks != 7 {
		t.Errorf("expected 7 bookmarks, got %d", tw.Metrics.Bookmarks)
	}
	if tw.Metrics.Views != 10000 {
		t.Errorf("expected 10000 views, got %d", tw.Metrics.Views)
	}
	if len(tw.Media) != 1 || tw.Media[0] != "https://pbs.twimg.com/media/test_image.jpg" {
		t.Errorf("expected media list with 1 item, got %v", tw.Media)
	}
}

func TestParseTwitterResponse_RawSSRHTML(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
  <article class="flex flex-col gap-1">
    <div class="flex items-center">
      <a href="/elonmusk"><span>@elonmusk</span></a>
      <svg data-icon="icon-verified-premium-color"></svg>
    </div>
    <a href="/elonmusk/status/2110549767063507044">
      <time datetime="2026-10-15T09:23:02Z">Oct 15</time>
    </a>
    <div dir="auto">
      <span>The new Tesla Roadster demo</span>
    </div>
  </article>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("unexpected goquery error: %v", err)
	}

	resp := &scrapemate.Response{Document: doc}
	job := NewTwitterJob("test-ssr", "elonmusk", "", 1)
	res, _, err := parseTwitterResponse(context.Background(), resp, job)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	tweets, ok := res.([]*Tweet)
	if !ok || len(tweets) != 1 {
		t.Fatalf("expected 1 tweet, got %v", res)
	}

	tw := tweets[0]
	if tw.ID != "2110549767063507044" {
		t.Errorf("expected ID '2110549767063507044', got %q", tw.ID)
	}
	if tw.Author.Username != "elonmusk" {
		t.Errorf("expected username 'elonmusk', got %q", tw.Author.Username)
	}
	if tw.Text != "The new Tesla Roadster demo" {
		t.Errorf("expected text 'The new Tesla Roadster demo', got %q", tw.Text)
	}
	if !tw.Author.Verified {
		t.Errorf("expected author to be verified")
	}
}

func TestParseTwitterResponse_ModernGraphQLSchema(t *testing.T) {
	// Modern X schema where user profile is in user_results.result.core
	payload := map[string]any{
		"data": map[string]any{
			"search_by_raw_query": map[string]any{
				"search_timeline": map[string]any{
					"timeline": map[string]any{
						"instructions": []any{
							map[string]any{
								"entries": []any{
									map[string]any{
										"content": map[string]any{
											"itemContent": map[string]any{
												"tweet_results": map[string]any{
													"result": map[string]any{
														"__typename": "Tweet",
														"rest_id":    "2110670745256821135",
														"core": map[string]any{
															"user_results": map[string]any{
																"result": map[string]any{
																	"__typename": "User",
																	"rest_id":    "1616071410207846400",
																	"core": map[string]any{
																		"name":        "Test Creator",
																		"screen_name": "test_creator",
																	},
																	"avatar": map[string]any{
																		"image_url": "https://pbs.twimg.com/avatar.jpg",
																	},
																	"relationship_counts": map[string]any{
																		"followers": 1500,
																		"following": 500,
																	},
																	"verification": map[string]any{
																		"verified": true,
																	},
																},
															},
														},
														"legacy": map[string]any{
															"full_text":      "Modern tweet body text",
															"created_at":     "Thu Oct 15 14:00:00 +0000 2026",
															"favorite_count": 50,
															"retweet_count":  10,
															"reply_count":    5,
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	resp := &scrapemate.Response{
		Meta: map[string]any{
			"graphql_tweets": []map[string]any{payload},
		},
	}

	job := NewTwitterJob("test-modern", "", "#AI", 1)
	res, _, err := parseTwitterResponse(context.Background(), resp, job)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	tweets, ok := res.([]*Tweet)
	if !ok || len(tweets) != 1 {
		t.Fatalf("expected 1 tweet, got %v", res)
	}

	tw := tweets[0]
	if tw.Author.Username != "test_creator" {
		t.Errorf("expected username 'test_creator', got %q", tw.Author.Username)
	}
	if tw.Author.Name != "Test Creator" {
		t.Errorf("expected name 'Test Creator', got %q", tw.Author.Name)
	}
	if tw.Author.FollowersCount != 1500 {
		t.Errorf("expected 1500 followers, got %d", tw.Author.FollowersCount)
	}
	if !tw.Author.Verified {
		t.Errorf("expected author to be verified")
	}
	if tw.URL != "https://x.com/test_creator/status/2110670745256821135" {
		t.Errorf("expected URL 'https://x.com/test_creator/status/2110670745256821135', got %q", tw.URL)
	}
}

func TestProcessTweetObjects_Deduper(t *testing.T) {
	t.Parallel()

	dedup := deduper.New()
	job1 := NewTwitterJob("job-1", "alice", "", 1, WithDeduper(dedup))
	job2 := NewTwitterJob("job-2", "bob", "", 1, WithDeduper(dedup))

	mockTweetObj := func(id, text string) map[string]any {
		return map[string]any{
			"__typename": "Tweet",
			"rest_id":    id,
			"legacy": map[string]any{
				"id_str":    id,
				"full_text": text,
			},
			"core": map[string]any{
				"user_results": map[string]any{
					"result": map[string]any{
						"rest_id": "u1",
						"legacy": map[string]any{
							"screen_name": "user1",
						},
					},
				},
			},
		}
	}

	objects := []map[string]any{
		mockTweetObj("tweet-100", "first tweet"),
		mockTweetObj("tweet-101", "second tweet"),
		mockTweetObj("tweet-100", "duplicate first tweet"),
	}

	res1 := processTweetObjects(context.Background(), objects, job1)
	if len(res1) != 2 {
		t.Fatalf("job 1: expected 2 tweets, got %d", len(res1))
	}
	if res1[0].ID != "tweet-100" || res1[1].ID != "tweet-101" {
		t.Errorf("job 1: unexpected IDs: %s, %s", res1[0].ID, res1[1].ID)
	}

	res2 := processTweetObjects(context.Background(), objects, job2)
	if len(res2) != 0 {
		t.Fatalf("job 2: expected 0 tweets due to cross-job deduplication, got %d", len(res2))
	}

	newObjects := []map[string]any{
		mockTweetObj("tweet-102", "third tweet"),
	}
	res3 := processTweetObjects(context.Background(), newObjects, job2)
	if len(res3) != 1 || res3[0].ID != "tweet-102" {
		t.Fatalf("job 2: expected 1 new tweet, got %v", res3)
	}
}
