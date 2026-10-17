# Modernize TikTok, Facebook, and Threads Scrapers

## Context
Following the Twitter scraper overhaul, the remaining social scrapers (`tiktok/`, `facebook/`, `threads/`) suffer from the same architectural flaws: broken cookie injection using `document.cookie` on `robots.txt` (which fails silently for HttpOnly session cookies), missing network response interception for client-side API/GraphQL calls, completely unhandled scroll depth (`MaxDepth` is ignored), missing auth/checkpoint wall detection, missing DOM fallback extraction when script tags are absent, and boolean verification parsing bugs. This plan updates all three scrapers to use Playwright native cookie injection, network interception, infinite scrolling, dual-source extraction (API/GraphQL + DOM fallback), and unified query support.

## Approach

### 1. TikTok Scraper Overhaul (`tiktok/job.go`, `tiktok/process.go`)
- **Target URL Construction (`tiktok/job.go`)**:
  - `username != "" && query != ""`: `https://www.tiktok.com/search?q=%s` with URL-escaped `username + " " + query`
  - `username != ""`: `https://www.tiktok.com/@%s` (stripping leading `@`)
  - `query != ""`: `https://www.tiktok.com/search?q=%s`
  - default: `https://www.tiktok.com/explore`
- **Native Cookie Injection (`tiktok/job.go`)**:
  - Check `pwPage, _ := page.Unwrap().(playwright.Page)`.
  - Parse semicolon-delimited cookie string into key-value pairs.
  - Inject cookies into `pwPage.Context().AddCookies(...)` with domain `.tiktok.com`, `Path: "/"`, `Secure: true`, and `HttpOnly: true` for `sessionid`, `tt_chain_token`, `ttwid`, `msToken`.
  - Fall back to `document.cookie` evaluation with `domain=.tiktok.com; path=/; SameSite=Lax` if `pwPage` is nil.
- **API Network Interception (`tiktok/job.go`)**:
  - Register `pwPage.OnResponse` listener filtering for URL substrings `/api/item_list/`, `/api/post/item_list/`, `/api/search/item/full/`, `/api/search/general/full/`, `/api/recommend/item_list/`.
  - Capture HTTP 200 responses, parse JSON body, and store into slice.
  - Pass captured slices into `resp.Meta["api_videos"]`.
- **Auth Wall / Captcha Detection (`tiktok/job.go`)**:
  - Inspect `page.URL()`: if URL contains `/login`, `passport/web/`, or `verify`, set `resp.Error = fmt.Errorf("authentication or verification required: redirected to %s", currentURL)`.
  - Detect HTTP 401/403/429 status codes on `pageResponse` and return descriptive errors.
- **Infinite Scrolling for Depth (`tiktok/job.go`)**:
  - Add `scrollDown(ctx, page, maxDepth)`: loop from 1 to `maxDepth`, scroll to `document.body.scrollHeight`, wait 2s, check scroll height with stuck detection (3 consecutive identical heights terminates), and jiggle scroll (`window.scrollBy(0, -200)` then scroll down).
- **Dual-Source Parsing (`tiktok/process.go`)**:
  - In `parseTikTokResponse`:
    1. First check `resp.Meta["api_videos"]`: extract items from `itemList`, `item_list`, or `data` using existing `extractVideoFromItem(itemMap, nil, job)`.
    2. Check embedded scripts: `__UNIVERSAL_DATA_FOR_REHYDRATION__` and `SIGI_STATE`.
    3. DOM Fallback: implement `extractVideosFromDOM(doc *goquery.Document, job *TikTokJob) []*Video`:
       - Match video elements: `div[data-e2e="user-post-item"]`, `div[data-e2e="search_top-item"]`, `div[data-e2e="search-card-like-container"]`, `a[href*="/video/"]`.
       - Video ID: extract numeric ID from `/video/(\d+)` regex on link `href`.
       - Video URL: `https://www.tiktok.com/@` + username + `/video/` + id.
       - Description: text from `div[data-e2e="user-post-item-desc"]`, `div[data-e2e="search-card-video-caption"]`, or `a[title]`.
       - Author: username/name from profile header `h1[data-e2e="user-title"]` or card username `span[data-e2e="search-card-user-unique-id"]`.
       - Views: parse metric count from `strong[data-e2e="video-views"]` or `span[data-e2e="search-card-like-count"]`.
       - Deduplicate by ID before returning.

### 2. Facebook Scraper Overhaul (`facebook/job.go`, `facebook/process.go`)
- **Target URL Construction (`facebook/job.go`)**:
  - `username != "" && query != ""`: `https://www.facebook.com/%s/search/?q=%s` with URL-escaped query
  - `username != ""`: `https://www.facebook.com/%s` (stripping leading `@`)
  - `query != ""`: `https://www.facebook.com/search/top?q=%s`
  - default: `https://www.facebook.com/`
- **Native Cookie Injection (`facebook/job.go`)**:
  - Inject cookies into `pwPage.Context().AddCookies(...)` with domain `.facebook.com`, `Path: "/"`, `Secure: true`, and `HttpOnly: true` for `c_user`, `xs`, `datr`, `sb`, `fr`.
  - Fall back to `document.cookie` if `pwPage` is nil.
- **GraphQL Network Interception (`facebook/job.go`)**:
  - Register `pwPage.OnResponse` listener filtering for URL substring `/api/graphql/`.
  - Capture HTTP 200 responses, parse JSON body, and store into slice.
  - Pass captured slices into `resp.Meta["graphql_posts"]`.
- **Auth Wall / Checkpoint Detection (`facebook/job.go`)**:
  - Inspect `page.URL()`: if URL contains `/login`, `checkpoint`, or `recover`, set `resp.Error = fmt.Errorf("authentication required: redirected to %s", currentURL)`.
  - Detect HTTP 401/403/429 status codes on `pageResponse`.
- **Infinite Scrolling for Depth (`facebook/job.go`)**:
  - Add `scrollDown(ctx, page, maxDepth)` with height checks, stuck detection, and jiggle.
- **Dual-Source Parsing (`facebook/process.go`)**:
  - In `parseFacebookResponse`:
    1. First check `resp.Meta["graphql_posts"]`: use `jsonparse.FindByTypename(obj, []string{"Story", "Post", "UserPost", "PagePost", "CometFeedUnit"}, 50)` and pass matching objects to existing `extractPostFromStory(obj, job)`.
    2. Check embedded scripts using existing `extractPostsFromScripts`.
    3. DOM Fallback: implement `extractPostsFromDOM(doc *goquery.Document, job *FacebookJob) []*Post`:
       - Match post elements: `div[role="article"]`, `div[data-pagelet*="FeedUnit"]`.
       - Post ID & URL: extract from `a[href*="/posts/"]`, `a[href*="/permalink/"]`, `a[href*="/videos/"]`, `a[role="link"][href*="facebook.com"]`.
       - Author: text from `h2, h3, h4, strong a, a[role="link"] span`.
       - Message Text: text from `div[dir="auto"], div[data-ad-preview="message"]`.
       - Metrics: extract counts from aria-labels (`div[aria-label*="reaction"]`, `div[aria-label*="comment"]`, `div[aria-label*="share"]`).
       - Deduplicate by ID before returning.

### 3. Threads Scraper Overhaul (`threads/job.go`, `threads/process.go`)
- **Query Support & URL Construction (`threads/job.go`)**:
  - Add `Query string` field to `ThreadsJob`.
  - Add `WithQuery(query string) ThreadsJobOptions` functional option.
  - Target URL routing:
    - `username != "" && query != ""`: `https://www.threads.net/search?q=%s` with URL-escaped `username + " " + query`
    - `username != ""`: `https://www.threads.net/@%s` (stripping leading `@`)
    - `query != ""`: `https://www.threads.net/search?q=%s`
    - default: `https://www.threads.net/`
- **Native Cookie Injection (`threads/job.go`)**:
  - Inject cookies into `pwPage.Context().AddCookies(...)` with domain `.threads.net`, `Path: "/"`, `Secure: true`, and `HttpOnly: true` for `sessionid`, `csrftoken`, `mid`, `ds_user_id`.
  - Fall back to `document.cookie` if `pwPage` is nil.
- **GraphQL Network Interception (`threads/job.go`)**:
  - Register `pwPage.OnResponse` listener filtering for `/api/graphql` or `/graphql/query`.
  - Capture HTTP 200 responses, parse JSON body, and store into slice.
  - Pass captured slices into `resp.Meta["graphql_threads"]`.
- **Auth Wall Detection (`threads/job.go`)**:
  - Inspect `page.URL()`: if URL contains `/login`, set `resp.Error = fmt.Errorf("authentication required: redirected to %s", currentURL)`.
  - Detect HTTP 401/403/429 status codes on `pageResponse`.
- **Infinite Scrolling for Depth (`threads/job.go`)**:
  - Add `scrollDown(ctx, page, maxDepth)` with height checks, stuck detection, and jiggle.
- **Dual-Source Parsing & Verified Fix (`threads/process.go`)**:
  - Fix boolean verification bug at `threads/process.go:118`: replace `jsonparse.GetString(user, "is_verified") == "true"` with `jsonparse.GetBool(user, "is_verified")`.
  - In `parseThreadsResponse`:
    1. First check `resp.Meta["graphql_threads"]`: use `jsonparse.FindByTypename` for `XDTThreadItem`, `XDTThread`, `Thread` and `jsonparse.FindByKey` for `thread_items`, passing items to `extractPostFromThreadItem`.
    2. Check embedded scripts using existing `extractPostsFromScripts`.
    3. DOM Fallback: implement `extractPostsFromDOM(doc *goquery.Document, job *ThreadsJob) []*Post`:
       - Match thread elements: `div[data-pressable-container]`, `div[data-testid*="post"]`.
       - Post ID & URL: extract from `a[href*="/post/"]` or `a[href*="/t/"]`.
       - Author: text from user link `a[href^="/@"]`.
       - Text: text from `div[dir="auto"]`.
       - Metrics: parse likes and replies from button text / SVG aria-labels.
       - Deduplicate by ID before returning.

### 4. Runner & Validation Alignment (`runner/webrunner/webrunner.go`, `web/job.go`)
- In `runner/webrunner/webrunner.go:204-209`: pass `threads.WithQuery(job.Data.Query)` to `threads.NewThreadsJob`.
- In `web/job.go:91-94`: update Threads validation to allow either `Username` or `Query` (`if strings.TrimSpace(d.Username) == "" && strings.TrimSpace(d.Query) == "" { return errors.New("username or query is required for Threads job") }`).

### 5. Shared Metric Helper
- Extract or replicate clean string-to-int count parser (`parseMetricCount(s string) int`) handling `K`, `M`, `B` suffixes and commas across DOM extraction logic.

## Critical Files & Anchors
- `tiktok/job.go:30-170`: Target URL generation, native cookie injection, network interception, scrollDown, and error checks.
- `tiktok/process.go:15-50`: `parseTikTokResponse` handling API meta, scripts, and DOM fallback extraction.
- `facebook/job.go:30-170`: Target URL generation, native cookie injection, GraphQL interception, scrollDown, and error checks.
- `facebook/process.go:15-50`: `parseFacebookResponse` handling GraphQL meta, scripts, and DOM fallback extraction.
- `threads/job.go:30-170`: `WithQuery` option, target URL generation, cookie injection, GraphQL interception, and scrollDown.
- `threads/process.go:15-50, 115-125`: `parseThreadsResponse` GraphQL meta, DOM fallback, and `is_verified` boolean fix.

## Verification
1. `go test -v ./tiktok/...`: verify `TestParseTikTokResponse` passes for embedded scripts, add test cases for `resp.Meta["api_videos"]` payload and DOM fallback parsing.
2. `go test -v ./facebook/...`: verify `TestParseFacebookResponse` passes for embedded scripts, add test cases for `resp.Meta["graphql_posts"]` payload and DOM fallback parsing.
3. `go test -v ./threads/...`: verify `TestParseThreadsResponse` passes for embedded scripts, add test cases for `resp.Meta["graphql_threads"]` payload, DOM fallback parsing, and boolean `is_verified` extraction.
4. Add `tiktok/job_test.go`, `facebook/job_test.go`, and `threads/job_test.go` verifying URL generation for username, query, both, and defaults.
5. Run `go test -v ./facebook/... ./tiktok/... ./threads/... ./twitter/... ./runner/... ./web/...` to verify all suites pass without regressions.
6. Run `make vet` and `make lint` to confirm clean formatting and zero static analysis warnings.

## Assumptions & Contingencies
- If `page.Unwrap()` is not a `playwright.Page` (e.g. during certain mock or non-browser tests), all three jobs fall back to `page.Eval` cookie injection and DOM scraping without crashing.
- If social platforms obfuscate or compress GraphQL/API network responses, the layered extraction falls back automatically from intercepted JSON to embedded script JSON, and finally to rendered DOM element parsing.
- Query support on Threads falls back to search URL `https://www.threads.net/search?q=%s` when query is provided. If only username is provided, it accesses `https://www.threads.net/@%s`.