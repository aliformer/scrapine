# Fix Twitter Scraper Extraction, Authentication, and Scrolling Logic

## Context
Auditing `twitter/` reveals fundamental logic flaws causing Twitter scrapes to return 0 tweets and produce empty CSVs. The parser in `twitter/process.go` searches `<script id="__NEXT_DATA__">` and script tags, which do not exist on modern X.com client-side SPAs where data is loaded via GraphQL endpoints (`/i/api/graphql/...`) and rendered into DOM elements. Authentication in `twitter/job.go` attempts to inject `auth_token` via `document.cookie` on `robots.txt` which fails for HttpOnly cookies, scroll depth is completely unhandled, and boolean verification parsing is broken.

## Approach

### 1. Robust Cookie Injection & Context Setup (`twitter/job.go`)
- Replace the `document.cookie` eval on `robots.txt` in `BrowserActions` with native Playwright cookie injection via `page.Unwrap()`.
- Extract `playwright.Page` from `page.Unwrap().(playwright.Page)` and access its browser context via `page.Context().AddCookies(...)`.
- Split `j.Cookie` by `;`, parse key-value pairs (`auth_token`, `ct0`, etc.), and set cookies with `Domain: ".x.com"`, `Path: "/"`, `Secure: true`, `HttpOnly: true` (for `auth_token`).
- If `page.Unwrap()` is not a `playwright.Page`, fall back to evaluating `document.cookie` with explicit `domain=.x.com; path=/; SameSite=Lax`.

### 2. GraphQL Network Interception in `BrowserActions` (`twitter/job.go`)
- Before navigating to `j.GetFullURL()`, register a response listener on `page` using `scrapemate.RequestHookProvider` or Playwright `page.OnResponse`.
- Filter responses for Twitter GraphQL URLs containing `/i/api/graphql/` (specifically operations like `UserTweets`, `SearchTimeline`, `TweetDetail`, `HomeTimeline`).
- When a matching response arrives with status 200, capture response body bytes and store the parsed JSON map into a shared slice in `j` or pass via `resp.Meta["graphql_tweets"]`.
- Add detection for auth redirects / walls: if `page.URL()` contains `/i/flow/login` or status is 401/403/429, populate `resp.Error` with descriptive errors (`ErrAuthRequired`, `ErrRateLimited`).

### 3. Infinite Scrolling for Depth (`twitter/job.go`)
- Support `j.MaxDepth` (defaulting to 1 if <= 0):
  - Execute a loop up to `j.MaxDepth` iterations:
    - Scroll the window downwards using `page.Eval("() => window.scrollBy(0, 1000)")`.
    - Wait 1.5–2 seconds between scrolls using `page.WaitForTimeout(1500 * time.Millisecond)` to allow incremental GraphQL requests and DOM rendering.
    - Break early if `ctx.Err() != nil` or if tweet count stops increasing for consecutive iterations.

### 4. DOM Fallback Extraction & Dual-Source Processing (`twitter/process.go`)
- Update `parseTwitterResponse`:
  - Check `resp.Meta["graphql_tweets"]` first: pass intercepted JSON objects directly to `processTweetObjects` (reusing existing GraphQL walker logic).
  - If intercepted GraphQL data is absent, fall back to parsing rendered DOM from `doc.Find("article[data-testid='tweet']")`:
    - Tweet ID & URL: extract from `a[role="link"][href*="/status/"]`.
    - Author username & display name: extract from `div[data-testid="User-Name"]`.
    - Tweet text: extract from `div[data-testid="tweetText"]`.
    - Timestamp: extract from `time` element attribute `datetime` (RFC3339).
    - Metrics (replies, retweets, likes, views): extract from buttons with `data-testid="reply"`, `data-testid="retweet"`, `data-testid="like"`, and view elements.
- Fix profile verification bug in `extractTweet`:
  - Replace `jsonparse.GetString(userLegacy, "verified") == "true"` with boolean type check: `val, ok := userLegacy["verified"].(bool); verified = ok && val` or string fallback.

### 5. URL Construction Support for Combined User & Query (`twitter/job.go`)
- Update `NewTwitterJob` target URL routing:
  - If both `username != ""` and `query != ""`: build `https://x.com/search?q=(from:%s) %s&f=live` (properly URL-escaped).
  - If only `username != ""`: `https://x.com/%s`.
  - If only `query != ""`: `https://x.com/search?q=%s&f=live`.
  - Default: `https://x.com/home`.

### 6. Realistic Integration & Unit Tests (`twitter/process_test.go`)
- Update `TestParseTwitterResponse`:
  - Test GraphQL intercepted payload structure via `resp.Meta["graphql_tweets"]`.
  - Test realistic DOM fallback with `<article data-testid="tweet">` HTML nodes.
  - Verify boolean `verified: true` parses correctly into `author.Verified == true`.
  - Verify `(from:username)` query URL formatting in `job_test.go`.

## Critical Files & Anchors
- `twitter/job.go:40-55`: Target URL generation for username and query combinations.
- `twitter/job.go:120-160`: `BrowserActions` cookie injection, response interception, and scrolling.
- `twitter/process.go:15-38`: `parseTwitterResponse` GraphQL meta handling and DOM extraction fallback.
- `twitter/process.go:175-200`: `extractTweet` boolean verification bug fix.
- `twitter/process_test.go:10-70`: Test fixture updates for DOM and GraphQL parsing.

## Verification
1. `go test -v ./twitter/...`: verify all unit tests pass (including GraphQL response parsing, DOM fallback parsing, and URL formatting).
2. `go vet ./twitter/...` and `make lint`: verify clean linting and formatting.
3. Smoke run with throwaway script `cmd/test-twitter/main.go` using a sample offline HTML/JSON fixture simulating Playwright capture to confirm CSV output produces valid row data without `-fallback` placeholder IDs.

## Assumptions & Contingencies
- If `page.Unwrap()` does not expose `playwright.Page` in certain test runners, `BrowserActions` falls back to `page.Eval` cookie injection and DOM scraping.
- If Twitter alters GraphQL schema field names, DOM fallback ensures resilient extraction of core tweet fields (ID, author, text, timestamp, metrics).
