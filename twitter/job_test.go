package twitter

import (
	"strings"
	"testing"
)

func TestNewTwitterJob_URLs(t *testing.T) {
	tests := []struct {
		name     string
		username string
		query    string
		wantURL  string
	}{
		{
			name:     "only username",
			username: "jack",
			query:    "",
			wantURL:  "https://x.com/jack",
		},
		{
			name:     "username with @ prefix",
			username: "@jack",
			query:    "",
			wantURL:  "https://x.com/jack",
		},
		{
			name:     "only query",
			username: "",
			query:    "golang news",
			wantURL:  "https://x.com/search?q=golang+news&f=live",
		},
		{
			name:     "both username and query",
			username: "jack",
			query:    "bitcoin",
			wantURL:  "https://x.com/search?q=%28from%3Ajack%29+bitcoin&f=live",
		},
		{
			name:     "neither username nor query",
			username: "",
			query:    "",
			wantURL:  "https://x.com/home",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := NewTwitterJob("test-id", tt.username, tt.query, 1)
			if job.URL != tt.wantURL {
				t.Errorf("got URL %q, want %q", job.URL, tt.wantURL)
			}
		})
	}
}

func TestTwitterJob_Options(t *testing.T) {
	cookieVal := "auth_token=secret123; ct0=csrf456"
	job := NewTwitterJob("job-1", "alice", "", 2, WithCookie(cookieVal))

	if job.Cookie != cookieVal {
		t.Errorf("got Cookie %q, want %q", job.Cookie, cookieVal)
	}
	if !strings.Contains(job.Headers["Cookie"], "auth_token=secret123") {
		t.Errorf("expected Cookie header to contain auth_token, got %q", job.Headers["Cookie"])
	}
}

func TestTwitterJob_TargetCount(t *testing.T) {
	t.Parallel()

	// Default when non-positive
	jobDefault := NewTwitterJob("job-1", "alice", "", 1)
	if jobDefault.TargetCount != 100 {
		t.Errorf("got default TargetCount %d, want 100", jobDefault.TargetCount)
	}

	// Explicit TargetCount
	jobCustom := NewTwitterJob("job-2", "bob", "", 1, WithTargetCount(150))
	if jobCustom.TargetCount != 150 {
		t.Errorf("got custom TargetCount %d, want 150", jobCustom.TargetCount)
	}
}
