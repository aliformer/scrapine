package threads

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gosom/scrapemate"
	"github.com/mxschmitt/playwright-go"

	"github.com/gosom/google-maps-scraper/cookiepool"
	"github.com/gosom/google-maps-scraper/deduper"
	"github.com/gosom/google-maps-scraper/exiter"
)

type ThreadsJobOptions func(*ThreadsJob)

type ThreadsJob struct {
	scrapemate.Job

	Username    string
	Query       string
	MaxDepth    int
	TargetCount int
	Deduper     deduper.Deduper
	ExitMonitor exiter.Exiter
	Cookie      string
}

func NewThreadsJob(id, username string, maxDepth int, opts ...ThreadsJobOptions) *ThreadsJob {
	if id == "" {
		id = uuid.New().String()
	}

	const (
		maxRetries = 3
		prio       = scrapemate.PriorityLow
	)

	job := ThreadsJob{
		Job: scrapemate.Job{
			ID:     id,
			Method: http.MethodGet,
			Headers: map[string]string{
				"User-Agent":                "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
				"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
				"Accept-Language":           "en-US,en;q=0.9",
				"Sec-Ch-Ua":                 `"Chromium";v="124", "Google Chrome";v="124", "Not-A.Brand";v="99"`,
				"Sec-Ch-Ua-Mobile":          "?0",
				"Sec-Ch-Ua-Platform":        `"Windows"`,
				"Sec-Fetch-Dest":            "document",
				"Sec-Fetch-Mode":            "navigate",
				"Sec-Fetch-Site":            "none",
				"Sec-Fetch-User":            "?1",
				"Upgrade-Insecure-Requests": "1",
			},
			MaxRetries: maxRetries,
			Priority:   prio,
			CheckResponse: func(resp *scrapemate.Response) bool {
				return resp.StatusCode >= 200 && resp.StatusCode < 500
			},
		},
		Username: username,
		MaxDepth: maxDepth,
		Cookie:   cookiepool.GetDefaultPool().GetCookie("threads"),
	}

	for _, opt := range opts {
		opt(&job)
	}

	if job.TargetCount <= 0 {
		job.TargetCount = 100
	}

	var targetURL string
	switch {
	case job.Username != "" && job.Query != "":
		cleanUser := strings.TrimPrefix(job.Username, "@")
		targetURL = fmt.Sprintf("https://www.threads.net/search?q=%s", url.QueryEscape(fmt.Sprintf("%s %s", cleanUser, job.Query)))
	case job.Username != "":
		targetURL = fmt.Sprintf("https://www.threads.net/@%s", strings.TrimPrefix(job.Username, "@"))
	case job.Query != "":
		targetURL = fmt.Sprintf("https://www.threads.net/search?q=%s", url.QueryEscape(job.Query))
	default:
		targetURL = "https://www.threads.net/"
	}
	job.URL = targetURL

	if job.Cookie != "" {
		job.Headers["Cookie"] = job.Cookie
	}

	return &job
}

func WithTargetCount(count int) ThreadsJobOptions {
	return func(j *ThreadsJob) {
		j.TargetCount = count
	}
}

func WithQuery(query string) ThreadsJobOptions {
	return func(j *ThreadsJob) {
		j.Query = query
	}
}

func WithDeduper(d deduper.Deduper) ThreadsJobOptions {
	return func(j *ThreadsJob) {
		j.Deduper = d
	}
}

func WithExitMonitor(e exiter.Exiter) ThreadsJobOptions {
	return func(j *ThreadsJob) {
		j.ExitMonitor = e
	}
}

func WithCookie(c string) ThreadsJobOptions {
	return func(j *ThreadsJob) {
		if c != "" {
			j.Cookie = c
		}
	}
}

func (j *ThreadsJob) UseInResults() bool {
	return true
}

func (j *ThreadsJob) ProcessOnFetchError() bool {
	return false
}

func (j *ThreadsJob) BrowserActions(ctx context.Context, page scrapemate.BrowserPage) scrapemate.Response {
	var resp scrapemate.Response

	pwPage, _ := page.Unwrap().(playwright.Page)

	injectCookies(page, pwPage, j.Cookie)

	cleanup, getResponses := attachGraphQLInterceptor(pwPage)
	defer cleanup()

	pageResponse, err := page.Goto(j.GetFullURL(), scrapemate.WaitUntilDOMContentLoaded)
	if err != nil {
		resp.Error = err
		return resp
	}

	currentURL := page.URL()
	if strings.Contains(currentURL, "/login") {
		resp.Error = fmt.Errorf("authentication required: redirected to %s", currentURL)
		return resp
	}

	if pageResponse != nil {
		if pageResponse.StatusCode == 401 || pageResponse.StatusCode == 403 {
			resp.Error = fmt.Errorf("authentication failed: HTTP %d", pageResponse.StatusCode)
			return resp
		}

		if pageResponse.StatusCode == 429 {
			resp.Error = fmt.Errorf("rate limited by Threads: HTTP 429")
			return resp
		}
	}

	// Wait for content or JSON data, fall back to timeout
	err = page.WaitForSelector(`div[data-pressable-container], div[data-testid*="post"], script[type="application/json"]`, 5*time.Second)
	if err != nil {
		page.WaitForTimeout(2 * time.Second)
	}

	maxDepth := j.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 1
	}

	if scrollErr := scrollDown(ctx, page, maxDepth); scrollErr != nil {
		resp.Error = scrollErr
		return resp
	}

	if pageResponse != nil {
		resp.URL = pageResponse.URL
		resp.StatusCode = pageResponse.StatusCode
		resp.Headers = pageResponse.Headers
	}

	if resp.URL == "" {
		resp.URL = page.URL()
	}

	captured := getResponses()
	if len(captured) > 0 {
		if resp.Meta == nil {
			resp.Meta = make(map[string]any)
		}
		resp.Meta["graphql_threads"] = captured
	}

	body, err := page.Content()
	if err != nil {
		resp.Error = err
		return resp
	}

	resp.Body = []byte(body)
	return resp
}

func injectCookies(page scrapemate.BrowserPage, pwPage playwright.Page, cookieStr string) {
	if cookieStr == "" {
		return
	}

	if pwPage != nil && pwPage.Context() != nil {
		var pwCookies []playwright.OptionalCookie

		for _, p := range strings.Split(cookieStr, ";") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}

			name, val, ok := strings.Cut(p, "=")
			if !ok {
				continue
			}

			name = strings.TrimSpace(name)
			val = strings.TrimSpace(val)

			if name == "" {
				continue
			}

			isHTTPOnly := name == "sessionid" || name == "csrftoken" || name == "mid" || name == "ds_user_id"
			pwCookies = append(pwCookies, playwright.OptionalCookie{
				Name:     name,
				Value:    val,
				Domain:   playwright.String(".threads.net"),
				Path:     playwright.String("/"),
				Secure:   playwright.Bool(true),
				HttpOnly: playwright.Bool(isHTTPOnly),
			})
		}

		if len(pwCookies) > 0 {
			_ = pwPage.Context().AddCookies(pwCookies)
		}

		return
	}

	for _, p := range strings.Split(cookieStr, ";") {
		p = strings.TrimSpace(p)
		if p != "" {
			_, _ = page.Eval(fmt.Sprintf(`document.cookie = %q`, p+"; domain=.threads.net; path=/; SameSite=Lax"))
		}
	}
}

func attachGraphQLInterceptor(pwPage playwright.Page) (func(), func() []map[string]any) {
	if pwPage == nil {
		return func() {}, func() []map[string]any { return nil }
	}

	var (
		mu        sync.Mutex
		responses []map[string]any
	)

	handler := func(pResponse playwright.Response) {
		reqURL := pResponse.URL()
		if !strings.Contains(reqURL, "/api/graphql") && !strings.Contains(reqURL, "/graphql/query") {
			return
		}

		if pResponse.Status() < 200 || pResponse.Status() >= 300 {
			return
		}

		var data map[string]any
		if err := pResponse.JSON(&data); err == nil && data != nil {
			mu.Lock()
			responses = append(responses, data)
			mu.Unlock()
		}
	}

	pwPage.OnResponse(handler)

	cleanup := func() {
		pwPage.RemoveListener("response", handler)
	}

	getResponses := func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()

		cp := make([]map[string]any, len(responses))
		copy(cp, responses)

		return cp
	}

	return cleanup, getResponses
}

func scrollDown(ctx context.Context, page scrapemate.BrowserPage, maxDepth int) error {
	stuckCount := 0
	lastHeight := 0

	for i := 1; i < maxDepth; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, _ = page.Eval(`() => window.scrollTo(0, document.body.scrollHeight)`)
		page.WaitForTimeout(2000 * time.Millisecond)

		heightVal, evalErr := page.Eval(`() => document.body.scrollHeight`)
		if evalErr == nil {
			var currentHeight int
			switch v := heightVal.(type) {
			case int:
				currentHeight = v
			case float64:
				currentHeight = int(v)
			}

			if currentHeight > 0 && currentHeight == lastHeight {
				stuckCount++
				if stuckCount >= 3 {
					break
				}

				// Jiggle scroll slightly to trigger intersection observer
				_, _ = page.Eval(`() => {
					window.scrollBy(0, -200);
					setTimeout(() => window.scrollTo(0, document.body.scrollHeight), 100);
				}`)
				page.WaitForTimeout(1000 * time.Millisecond)
			} else {
				stuckCount = 0
				lastHeight = currentHeight
			}
		}
	}

	return nil
}

func (j *ThreadsJob) Process(ctx context.Context, resp *scrapemate.Response) (any, []scrapemate.IJob, error) {
	// ponytail: basic DOM scraper boilerplate. Upgrade to mobile API signature extraction.
	return parseThreadsResponse(ctx, resp, j)
}
