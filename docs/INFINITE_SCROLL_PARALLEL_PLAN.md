# Infinite Scroll, Target Item Count, and Parallel Scraping

## Context
Twitter and social scrapers currently default to a scroll depth of 1, resulting in only ~26 initial tweets being extracted without dynamic scrolling. Users need scrapers to dynamically infinite scroll until reaching a configured target count (e.g., 100 tweets) and support spawning scraping jobs across queries/accounts in parallel while preventing cross-job and in-job duplicates. This plan implements dynamic count-based infinite scrolling, seed job fan-out for multiple targets/keywords in `webrunner`, shared `deduper.Deduper` integration, and frontend/API target count controls.

## Approach

### 1. Extend Data Contracts & API (`web/job.go`, `web/web.go`, `web/frontend/`)
- In `web/job.go`:
  - Add `MaxResults int `json:"max_results,omitempty"`` to `JobData`.
  - In `JobData.Validate()`: if `MaxResults <= 0`, default social jobs (`twitter`, `facebook`, `tiktok`, `threads`) to 100 if `Depth <= 1`; if `Depth > 1`, default `MaxResults = Depth * 20`.
- In `web/web.go`:
  - Add `MaxResults int `json:"max_results,omitempty"`` to `apiScrapeRequest`.
  - Map `newJob.Data.MaxResults = req.MaxResults` in `apiScrape()`.
- In `web/frontend/src/types/index.ts`:
  - Add `max_results?: number` to `JobData` and `CreateJobRequest`.
- In `web/frontend/src/pages/Twitter.tsx`:
  - Add `maxResults` state (default 100).
  - Add form input for Target Tweet Count with tooltip and helper text.
  - Update `username` and `query` input helper text to document that comma- or newline-separated values spawn parallel scrape tasks.
  - Include `max_results: maxResults` in `CreateJobRequest` payload.

### 2. Multi-Target Parallel Seed Job Spawning (`runner/webrunner/webrunner.go`)
- Create helper `parseSocialTargets(username, query string, keywords []string) []struct{ Username, Query string }`:
  - If `len(keywords) > 0`: convert each non-empty keyword into a target entry (`Query: kw`).
  - If `username` contains commas or newlines: split into individual usernames.
  - If `query` contains commas or newlines: split into individual queries.
  - Deduplicate identical targets; fallback to `(username, query)` if single.
- In `w.scrapeJob(ctx, job)` under `switch jobType`:
  - For `twitter`:
    - Call `targets := parseSocialTargets(job.Data.Username, job.Data.Query, job.Data.Keywords)`.
    - Calculate per-seed target count: `targetPerSeed := job.Data.MaxResults / len(targets)` (minimum 20, fallback `job.Data.MaxResults`).
    - For each target at index `i`, append to `seedJobs`:
      ```go
      twitter.NewTwitterJob(
          fmt.Sprintf("%s-%d", job.ID, i),
          t.Username,
          t.Query,
          job.Data.Depth,
          twitter.WithDeduper(dedup),
          twitter.WithExitMonitor(exitMonitor),
          twitter.WithCookie(job.Data.Cookie),
          twitter.WithTargetCount(targetPerSeed),
      )
      ```
  - Replicate the target expansion pattern for `threads`, `facebook`, and `tiktok` seed job creation.
  - Because `mateRunner` is instantiated with `scrapemateapp.WithConcurrency(cfg.Concurrency)`, all seed jobs in `seedJobs` execute concurrently in parallel worker goroutines.
  - All seed jobs share the single `dedup := deduper.New()` instance, ensuring cross-job deduplication.

### 3. Dynamic Infinite Scroll with Target Count & Early Exit (`twitter/job.go`)
- Add `TargetCount int` field to `TwitterJob` struct.
- Add option `WithTargetCount(n int) TwitterJobOptions` to set `j.TargetCount`. Default `j.TargetCount = 100` if `<= 0`.
- Update `attachGraphQLInterceptor(pwPage)`:
  - Add thread-safe unique tweet counter `getTweetCount() int` tracking intercepted unique `rest_id` and `legacy.id_str` values in real-time.
- Update `scrollDown(ctx context.Context, page scrapemate.BrowserPage, targetCount int, getCount func() int) error`:
  - Maximum scroll loop safety limit: 50 scrolls or `ctx.Done()`.
  - In each iteration:
    - Execute `window.scrollTo(0, document.body.scrollHeight)`.
    - Wait for response/render (1.5s delay).
    - Check count: `count := getCount()`. If GraphQL count is 0, check DOM count via `() => document.querySelectorAll('article[data-testid="tweet"]').length`.
    - If `count >= targetCount`: terminate scroll loop immediately (early exit).
    - If `currentHeight == lastHeight` and count did not increase:
      - Increment `stuckCount`.
      - If `stuckCount >= 2`: perform scroll jiggle (`window.scrollBy(0, -300)` then scroll to bottom).
      - If `stuckCount >= 4`: break loop (end of feed or rate limit reached).
      - If height or count increased: reset `stuckCount = 0`.
- In `TwitterJob.BrowserActions()`:
  - Pass `j.TargetCount` and the counter callback into `scrollDown`.

### 4. Deduplication via `Deduper` (`twitter/process.go`)
- In `twitter/process.go` inside `processTweetObjects(objects []map[string]any, job *TwitterJob)`:
  - Update deduplication loop:
    ```go
    for _, obj := range objects {
        tweet := extractTweet(obj, job)
        if tweet == nil || tweet.ID == "" {
            continue
        }
        if seen[tweet.ID] {
            continue
        }
        if job.Deduper != nil && !job.Deduper.AddIfNotExists(ctx, tweet.ID) {
            continue
        }
        seen[tweet.ID] = true
        tweets = append(tweets, tweet)
    }
    ```
- In `extractTweetsFromDOM`:
  - Check `job.Deduper.AddIfNotExists(ctx, tweet.ID)` before appending to prevent duplicate DOM tweets from overlapping scroll frames.
- Replicate `job.Deduper.AddIfNotExists(ctx, item.ID)` check in `tiktok/process.go`, `facebook/process.go`, and `threads/process.go`.

## Critical files & anchors
1. `runner/webrunner/webrunner.go:190-230`: `scrapeJob()` where social seed jobs are created; anchor for multi-target splitting and parallel fan-out.
2. `twitter/job.go:160-350`: `BrowserActions()`, `attachGraphQLInterceptor()`, and `scrollDown()`; anchor for dynamic count-based infinite scroll.
3. `twitter/process.go:160-205`: `processTweetObjects()`; anchor for `job.Deduper.AddIfNotExists(ctx, tweet.ID)`.
4. `web/job.go:50-95`: `JobData` struct and `Validate()`; anchor for `MaxResults` field and default calculation.
5. `web/frontend/src/pages/Twitter.tsx:25-95`: Twitter frontend form and submission; anchor for target count UI input.

## Verification
- Unit test in `twitter/job_test.go`:
  - Verify `WithTargetCount(150)` sets `TargetCount = 150`.
  - Verify `NewTwitterJob` defaults `TargetCount` to 100 when passed `<= 0`.
- Unit test in `twitter/process_test.go`:
  - Feed mock GraphQL objects containing duplicate tweets and verify `job.Deduper.AddIfNotExists` discards duplicates.
  - Instantiate two `TwitterJob` instances sharing one `deduper.New()`; verify second job discards tweet IDs processed by first job.
- Unit test in `runner/webrunner/webrunner_test.go`:
  - Test `parseSocialTargets` with comma-separated usernames (`alice, bob, charlie`) generates 3 distinct seed jobs.
  - Test `parseSocialTargets` with keywords slice generates separate seed jobs per keyword.
- Execute test suite:
  - `go test -v ./twitter/... ./runner/webrunner/... ./web/...`
  - `make vet`
  - `make test`

## Assumptions & contingencies
- Assumption: If user specifies a single query without commas, parallelization across date windows is not inferred unless explicitly configured; infinite scroll handles single-query target counts. If user requests single-query chunking in future, date-window partitioning (`since: / until:`) can be added.
- Fallback: If Twitter's GraphQL network response format changes or is blocked by anti-bot, `scrollDown` falls back to counting DOM article elements `article[data-testid="tweet"]` to track progress toward `TargetCount`.
