package facebook

import (
	"strings"
	"testing"
)

func TestNewFacebookJob_URLs(t *testing.T) {
	tests := []struct {
		name     string
		username string
		query    string
		wantURL  string
	}{
		{
			name:     "only username",
			username: "zuck",
			query:    "",
			wantURL:  "https://www.facebook.com/zuck",
		},
		{
			name:     "username with @ prefix",
			username: "@zuck",
			query:    "",
			wantURL:  "https://www.facebook.com/zuck",
		},
		{
			name:     "only query",
			username: "",
			query:    "open source software",
			wantURL:  "https://www.facebook.com/search/top?q=open+source+software",
		},
		{
			name:     "both username and query",
			username: "meta",
			query:    "llama 3",
			wantURL:  "https://www.facebook.com/meta/search/?q=llama+3",
		},
		{
			name:     "neither username nor query",
			username: "",
			query:    "",
			wantURL:  "https://www.facebook.com/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := NewFacebookJob("test-id", tt.username, tt.query, 1)
			if job.URL != tt.wantURL {
				t.Errorf("got URL %q, want %q", job.URL, tt.wantURL)
			}
		})
	}
}

func TestFacebookJob_Options(t *testing.T) {
	cookieVal := "c_user=123456789; xs=secret_token_abc"
	job := NewFacebookJob("job-1", "alice", "", 2, WithCookie(cookieVal))

	if job.Cookie != cookieVal {
		t.Errorf("got Cookie %q, want %q", job.Cookie, cookieVal)
	}
	if !strings.Contains(job.Headers["Cookie"], "c_user=123456789") {
		t.Errorf("expected Cookie header to contain c_user, got %q", job.Headers["Cookie"])
	}
}
