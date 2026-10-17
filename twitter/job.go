package twitter

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
	"github.com/gosom/google-maps-scraper/internal/jsonparse"
)

type TwitterJobOptions func(*TwitterJob)

type TwitterJob struct {
	scrapemate.Job

	Username    string
	Query       string
	MaxDepth    int
	TargetCount int
	Deduper     deduper.Deduper
	ExitMonitor exiter.Exiter
	Cookie      string
}

func NewTwitterJob(id, username, query string, maxDepth int, opts ...TwitterJobOptions) *TwitterJob {
	if id == "" {
		id = uuid.New().String()
	}

	var targetURL string
	switch {
	case username != "" && query != "":
		cleanUser := strings.TrimPrefix(username, "@")
		rawQuery := fmt.Sprintf("(from:%s) %s", cleanUser, query)
		targetURL = fmt.Sprintf("https://x.com/search?q=%s&f=live", url.QueryEscape(rawQuery))
	case username != "":
		targetURL = fmt.Sprintf("https://x.com/%s", strings.TrimPrefix(username, "@"))
	case query != "":
		targetURL = fmt.Sprintf("https://x.com/search?q=%s&f=live", url.QueryEscape(query))
	default:
		targetURL = "https://x.com/home"
	}

	const (
		maxRetries = 3
		prio       = scrapemate.PriorityLow
	)

	job := TwitterJob{
		Job: scrapemate.Job{
			ID:     id,
			Method: http.MethodGet,
			URL:    targetURL,
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
		Query:    query,
		MaxDepth: maxDepth,
		Cookie:   cookiepool.GetDefaultPool().GetCookie("twitter"),
	}

	for _, opt := range opts {
		opt(&job)
	}

	if job.TargetCount <= 0 {
		job.TargetCount = 100
	}

	if job.Cookie != "" {
		job.Headers["Cookie"] = job.Cookie
	}

	return &job
}

func WithTargetCount(count int) TwitterJobOptions {
	return func(j *TwitterJob) {
		j.TargetCount = count
	}
}

func WithDeduper(d deduper.Deduper) TwitterJobOptions {
	return func(j *TwitterJob) {
		j.Deduper = d
	}
}

func WithExitMonitor(e exiter.Exiter) TwitterJobOptions {
	return func(j *TwitterJob) {
		j.ExitMonitor = e
	}
}

func WithCookie(c string) TwitterJobOptions {
	return func(j *TwitterJob) {
		if c != "" {
			j.Cookie = c
		}
	}
}

func (j *TwitterJob) UseInResults() bool {
	return true
}

func (j *TwitterJob) ProcessOnFetchError() bool {
	return false
}

func (j *TwitterJob) BrowserActions(ctx context.Context, page scrapemate.BrowserPage) scrapemate.Response {
	var resp scrapemate.Response

	pwPage, _ := page.Unwrap().(playwright.Page)

	injectCookies(page, pwPage, j.Cookie)

	cleanup, getResponses, getTweetCount := attachGraphQLInterceptor(pwPage)
	defer cleanup()

	pageResponse, err := page.Goto(j.GetFullURL(), scrapemate.WaitUntilDOMContentLoaded)
	if err != nil {
		resp.Error = err

		return resp
	}

	currentURL := page.URL()
	if strings.Contains(currentURL, "/i/flow/login") || strings.Contains(currentURL, "/login") {
		resp.Error = fmt.Errorf("authentication required: redirected to %s", currentURL)

		return resp
	}

	if pageResponse != nil {
		if pageResponse.StatusCode == 401 || pageResponse.StatusCode == 403 {
			resp.Error = fmt.Errorf("authentication failed: HTTP %d", pageResponse.StatusCode)

			return resp
		}

		if pageResponse.StatusCode == 429 {
			resp.Error = fmt.Errorf("rate limited by X (Twitter): HTTP 429")

			return resp
		}
	}

	err = page.WaitForSelector(`article[data-testid="tweet"], div[data-testid="cellInnerDiv"]`, 5*time.Second)
	if err != nil {
		page.WaitForTimeout(2 * time.Second)
	}

	maxDepth := j.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 1
	}

	if scrollErr := scrollDown(ctx, page, maxDepth, j.TargetCount, getTweetCount); scrollErr != nil {
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

		resp.Meta["graphql_tweets"] = captured
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

			isHTTPOnly := name == "auth_token"
			pwCookies = append(pwCookies, playwright.OptionalCookie{
				Name:     name,
				Value:    val,
				Domain:   playwright.String(".x.com"),
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
			_, _ = page.Eval(fmt.Sprintf(`document.cookie = %q`, p+"; domain=.x.com; path=/; SameSite=Lax"))
		}
	}
}

func attachGraphQLInterceptor(pwPage playwright.Page) (func(), func() []map[string]any, func() int) {
	if pwPage == nil {
		return func() {}, func() []map[string]any { return nil }, func() int { return 0 }
	}

	var (
		mu        sync.Mutex
		responses []map[string]any
		seenIDs   = make(map[string]bool)
	)

	handler := func(pResponse playwright.Response) {
		reqURL := pResponse.URL()
		if !strings.Contains(reqURL, "/i/api/graphql/") {
			return
		}

		if pResponse.Status() < 200 || pResponse.Status() >= 300 {
			return
		}

		var data map[string]any
		if err := pResponse.JSON(&data); err == nil && data != nil {
			mu.Lock()
			responses = append(responses, data)

			typed := jsonparse.FindByTypename(data, []string{
				"Tweet",
				"TweetWithVisibilityResults",
				"TweetTombstone",
			}, 200)
			for _, obj := range typed {
				if jsonparse.GetString(obj, "__typename") == "TweetWithVisibilityResults" {
					if tw := jsonparse.GetMap(obj, "tweet"); tw != nil {
						obj = tw
					}
				}
				id := jsonparse.GetString(obj, "rest_id")
				if id == "" {
					if leg := jsonparse.GetMap(obj, "legacy"); leg != nil {
						id = jsonparse.GetString(leg, "id_str")
					}
				}
				if id != "" {
					seenIDs[id] = true
				}
			}

			resultObjects := jsonparse.FindByKey(data, "tweet_results", 200)
			for _, ro := range resultObjects {
				tr := jsonparse.GetMap(ro, "tweet_results")
				if tr != nil {
					if res := jsonparse.GetMap(tr, "result"); res != nil {
						id := jsonparse.GetString(res, "rest_id")
						if id == "" {
							if leg := jsonparse.GetMap(res, "legacy"); leg != nil {
								id = jsonparse.GetString(leg, "id_str")
							}
						}
						if id != "" {
							seenIDs[id] = true
						}
					}
				}
			}
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

	getTweetCount := func() int {
		mu.Lock()
		defer mu.Unlock()

		return len(seenIDs)
	}

	return cleanup, getResponses, getTweetCount
}

func scrollDown(ctx context.Context, page scrapemate.BrowserPage, maxDepth int, targetCount int, getCount func() int) error {
	stuckCount := 0
	lastHeight := 0
	lastCount := 0

	limit := maxDepth
	if limit <= 1 {
		limit = 50
	}

	if targetCount <= 0 {
		targetCount = 100
	}

	for range limit {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		count := 0
		if getCount != nil {
			count = getCount()
		}
		if count == 0 {
			val, _ := page.Eval(`() => document.querySelectorAll('article[data-testid="tweet"]').length`)
			switch v := val.(type) {
			case int:
				count = v
			case float64:
				count = int(v)
			}
		}

		if count >= targetCount {
			return nil
		}

		_, _ = page.Eval(`() => window.scrollTo(0, document.body.scrollHeight)`)
		page.WaitForTimeout(1500 * time.Millisecond)

		heightVal, evalErr := page.Eval(`() => document.body.scrollHeight`)
		if evalErr == nil {
			var currentHeight int
			switch v := heightVal.(type) {
			case int:
				currentHeight = v
			case float64:
				currentHeight = int(v)
			}

			if currentHeight > 0 && currentHeight == lastHeight && count == lastCount {
				stuckCount++
				if stuckCount >= 2 {
					// Jiggle scroll slightly to trigger intersection observer
					_, _ = page.Eval(`() => {
						window.scrollBy(0, -300);
						setTimeout(() => window.scrollTo(0, document.body.scrollHeight), 100);
					}`)
					page.WaitForTimeout(1000 * time.Millisecond)
				}
				if stuckCount >= 4 {
					break
				}
			} else {
				stuckCount = 0
				lastHeight = currentHeight
				lastCount = count
			}
		}
	}

	return nil
}

func (j *TwitterJob) Process(ctx context.Context, resp *scrapemate.Response) (any, []scrapemate.IJob, error) {
	return parseTwitterResponse(ctx, resp, j)
}
