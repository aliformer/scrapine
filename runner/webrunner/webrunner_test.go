//nolint:testpackage // This test needs unexported hooks to avoid running a browser.
package webrunner

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/gosom/google-maps-scraper/runner"
	"github.com/gosom/google-maps-scraper/web"
	"github.com/gosom/scrapemate"
)

func TestScrapeJobMarksOKBeforeClosingMate(t *testing.T) {
	t.Parallel()

	repo := &memoryJobRepo{}
	svc := web.NewService(repo, t.TempDir())
	job := web.Job{
		ID:     "job-1",
		Name:   "coffee",
		Date:   time.Now().UTC(),
		Status: web.StatusPending,
		Data: web.JobData{
			Keywords: []string{"coffee"},
			Lang:     "en",
			Zoom:     15,
			Lat:      "37.7749",
			Lon:      "-122.4194",
			FastMode: true,
			Radius:   1000,
			Depth:    10,
			MaxTime:  time.Minute,
		},
	}

	if err := svc.Create(context.Background(), &job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	w := &webrunner{
		svc: svc,
		cfg: &runner.Config{DataFolder: t.TempDir(), Concurrency: 1},
		setupMate: func(_ context.Context, _ io.Writer, _ *web.Job) (mateRunner, error) {
			return fakeMate{
				onClose: func() {
					got, err := svc.Get(context.Background(), job.ID)
					if err != nil {
						t.Fatalf("get job during close: %v", err)
					}
					if got.Status != web.StatusOK {
						t.Fatalf("status during close = %q, want %q", got.Status, web.StatusOK)
					}
				},
			}, nil
		},
	}

	if err := w.scrapeJob(context.Background(), &job); err != nil {
		t.Fatalf("scrape job: %v", err)
	}
}

type fakeMate struct {
	onClose func()
}

func (m fakeMate) Start(context.Context, ...scrapemate.IJob) error {
	return nil
}

func (m fakeMate) Close() error {
	if m.onClose != nil {
		m.onClose()
	}

	return nil
}

type memoryJobRepo struct {
	jobs map[string]web.Job
}

func (r *memoryJobRepo) Get(_ context.Context, id string) (web.Job, error) {
	return r.jobs[id], nil
}

func (r *memoryJobRepo) Create(_ context.Context, job *web.Job) error {
	if r.jobs == nil {
		r.jobs = make(map[string]web.Job)
	}

	r.jobs[job.ID] = *job

	return nil
}

func (r *memoryJobRepo) Delete(_ context.Context, id string) error {
	delete(r.jobs, id)
	return nil
}

func (r *memoryJobRepo) Select(_ context.Context, params web.SelectParams) ([]web.Job, error) {
	jobs := make([]web.Job, 0, len(r.jobs))

	for id := range r.jobs {
		job := r.jobs[id]
		if params.Status == "" || job.Status == params.Status {
			jobs = append(jobs, job)
		}
	}

	return jobs, nil
}

func (r *memoryJobRepo) Update(_ context.Context, job *web.Job) error {
	r.jobs[job.ID] = *job
	return nil
}

func TestParseSocialTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		query    string
		keywords []string
		want     []socialTarget
	}{
		{
			name:     "single user",
			username: "alice",
			query:    "",
			keywords: nil,
			want: []socialTarget{
				{Username: "alice", Query: ""},
			},
		},
		{
			name:     "comma separated users with duplicates and spaces",
			username: "alice, bob, alice , charlie",
			query:    "",
			keywords: nil,
			want: []socialTarget{
				{Username: "alice", Query: ""},
				{Username: "bob", Query: ""},
				{Username: "charlie", Query: ""},
			},
		},
		{
			name:     "newline separated queries",
			username: "",
			query:    "golang\npython\ngolang\nrust",
			keywords: nil,
			want: []socialTarget{
				{Query: "golang"},
				{Query: "python"},
				{Query: "rust"},
			},
		},
		{
			name:     "keywords slice takes priority",
			username: "alice, bob",
			query:    "golang",
			keywords: []string{"tag1", "tag2", "tag1"},
			want: []socialTarget{
				{Query: "tag1"},
				{Query: "tag2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSocialTargets(tt.username, tt.query, tt.keywords)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d targets, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("at index %d: got %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestResolveAccountCookies(t *testing.T) {
	t.Parallel()

	raw := "auth_token=tok1; ct0=cs1\n# comment\nauth_token=tok2; ct0=cs2\n\nauth_token=tok3; ct0=cs3"
	cookies := resolveAccountCookies(raw, "twitter")
	if len(cookies) != 3 {
		t.Fatalf("expected 3 cookies, got %d", len(cookies))
	}
	if cookies[0] != "auth_token=tok1; ct0=cs1" || cookies[1] != "auth_token=tok2; ct0=cs2" || cookies[2] != "auth_token=tok3; ct0=cs3" {
		t.Errorf("unexpected cookies parsed: %v", cookies)
	}
}
