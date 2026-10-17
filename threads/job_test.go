package threads

import (
	"strings"
	"testing"
)

func TestNewThreadsJob_URLs(t *testing.T) {
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
			wantURL:  "https://www.threads.net/@zuck",
		},
		{
			name:     "username with @ prefix",
			username: "@zuck",
			query:    "",
			wantURL:  "https://www.threads.net/@zuck",
		},
		{
			name:     "only query",
			username: "",
			query:    "golang updates",
			wantURL:  "https://www.threads.net/search?q=golang+updates",
		},
		{
			name:     "both username and query",
			username: "mosseri",
			query:    "creators",
			wantURL:  "https://www.threads.net/search?q=mosseri+creators",
		},
		{
			name:     "neither username nor query",
			username: "",
			query:    "",
			wantURL:  "https://www.threads.net/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []ThreadsJobOptions{}
			if tt.query != "" {
				opts = append(opts, WithQuery(tt.query))
			}
			job := NewThreadsJob("test-id", tt.username, 1, opts...)
			if job.URL != tt.wantURL {
				t.Errorf("got URL %q, want %q", job.URL, tt.wantURL)
			}
		})
	}
}

func TestThreadsJob_Options(t *testing.T) {
	cookieVal := "sessionid=threads_secret_123; csrftoken=csrf_abc"
	job := NewThreadsJob("job-1", "alice", 2, WithCookie(cookieVal), WithQuery("test search"))

	if job.Cookie != cookieVal {
		t.Errorf("got Cookie %q, want %q", job.Cookie, cookieVal)
	}
	if job.Query != "test search" {
		t.Errorf("got Query %q, want 'test search'", job.Query)
	}
	if !strings.Contains(job.Headers["Cookie"], "sessionid=threads_secret_123") {
		t.Errorf("expected Cookie header to contain sessionid, got %q", job.Headers["Cookie"])
	}
}
