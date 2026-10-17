package tiktok

import (
	"strings"
	"testing"
)

func TestNewTikTokJob_URLs(t *testing.T) {
	tests := []struct {
		name     string
		username string
		query    string
		wantURL  string
	}{
		{
			name:     "only username",
			username: "charlidamelio",
			query:    "",
			wantURL:  "https://www.tiktok.com/@charlidamelio",
		},
		{
			name:     "username with @ prefix",
			username: "@charlidamelio",
			query:    "",
			wantURL:  "https://www.tiktok.com/@charlidamelio",
		},
		{
			name:     "only query",
			username: "",
			query:    "golang programming",
			wantURL:  "https://www.tiktok.com/search?q=golang+programming",
		},
		{
			name:     "both username and query",
			username: "khaby.lame",
			query:    "comedy",
			wantURL:  "https://www.tiktok.com/search?q=khaby.lame+comedy",
		},
		{
			name:     "neither username nor query",
			username: "",
			query:    "",
			wantURL:  "https://www.tiktok.com/explore",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := NewTikTokJob("test-id", tt.username, tt.query, 1)
			if job.URL != tt.wantURL {
				t.Errorf("got URL %q, want %q", job.URL, tt.wantURL)
			}
		})
	}
}

func TestTikTokJob_Options(t *testing.T) {
	cookieVal := "sessionid=tiktok_secret_123; ttwid=wide_456"
	job := NewTikTokJob("job-1", "alice", "", 2, WithCookie(cookieVal))

	if job.Cookie != cookieVal {
		t.Errorf("got Cookie %q, want %q", job.Cookie, cookieVal)
	}
	if !strings.Contains(job.Headers["Cookie"], "sessionid=tiktok_secret_123") {
		t.Errorf("expected Cookie header to contain sessionid, got %q", job.Headers["Cookie"])
	}
}
