# Infinite Scroll, Target Item Count, and Parallel Scraping

## Context
Twitter and social scrapers currently default to scroll depth 1, returning ~26 initial items without dynamic scrolling. Users need scrapers to dynamically infinite scroll until reaching a configured target count (e.g., 100 tweets) and support spawning scraping jobs across queries/accounts in parallel while preventing cross-job and in-job duplicates. This plan implements dynamic count-based infinite scrolling, seed job fan-out for multiple targets/keywords in `webrunner`, shared `deduper.Deduper` integration, and frontend/API target count controls.

## Approach

### 1. Extend Data Contracts & API for Target Count (`MaxResults`)
- Target: `web/job.go`:
  - Add `MaxResults int `json:"max_results,omitempty"`` to `JobData` struct.
  - In `JobData.Validate()`:
    For social types (`"twitter"`, `"facebook"`, `"tiktok"`, `"threads"`):
    - If `d.MaxResults <= 0`:
      - If `d.Depth <= 1`: default `d.MaxResults = 100`.
      - If `d.Depth > 1`: default `d.MaxResults = d.Depth * 20`.
- Target: `web/web.go`:
  - Add `MaxResults int `json:"max_results,omitempty"`` to `apiScrapeRequest` struct.
  - In `apiScrape()`: map `MaxResults: req.MaxResults` into `newJob.Data`.
- Target: `web/frontend/src/types/index.ts`:
  - Add `max_results?: number` to `JobData` and `CreateJobRequest`.
- Target: `web/frontend/src/pages/Twitter.tsx`:
  - Add `maxResults` state initialized to 100: `const [maxResults, setMaxResults] = useState(100)`.
  - Add UI input for `maxResults` (label: "Target Tweet Count", type: `number`, min: 1, max: 1000).
  - Include `max_results: maxResults` in `CreateJobRequest` payload inside `handleSubmit`.
  - Update `username` and `query` input helper text: "Comma- or newline-separated values run in parallel."

### 2. Multi-Target Parallel Seed Job Spawning
- Target: `runner/webrunner/webrunner.go`:
  - Implement helper:
    ```go
    type socialTarget struct {
        Username string
        Query    string
    }

    func parseSocialTargets(username, query string, keywords []string) []socialTarget {
        var raw []socialTarget
        if len(keywords) > 0 {
            for _, kw := range keywords {
                kw = strings.TrimSpace(kw)
                if kw != "" {
                    raw = append(raw, socialTarget{Query: kw})
                }
            }
        } else if strings.ContainsAny(username, ",\n") {
            for _, u := range strings.FieldsFunc(username, func(r rune) bool { return r == ',' || r == '\n' }) {
                u = strings.TrimSpace(u)
                if u != "" {
                    raw = append(raw, socialTarget{Username: u})
                }
            }
        } else if strings.ContainsAny(query, ",\n") {
            for _, q := range strings.FieldsFunc(query, func(r rune) bool { return r == ',' || r == '\n' }) {
                q = strings.TrimSpace(q)
                if q != "" {
                    raw = append(raw, socialTarget{Query: q})
                }
            }
        } else {
            raw = append(raw, socialTarget{Username: strings.TrimSpace(username), Query: strings.TrimSpace(query)})
        }

        seen := make(map[string]bool)
        var targets []socialTarget
        for _, t := range raw {
            key := t.Username + "|" + t.Query
            if !seen[key] && key != "|" {
                seen[key] = true
                targets = append(targets, t)
            }
        }
        return targets
    }
    ```
  - In `w.scrapeJob(ctx, job)` under `switch jobType`:
    - For `"twitter"`:
      - Call `targets := parseSocialTargets(job.Data.Username, job.Data.Query, job.Data.Keywords)`.
      - Compute per-seed count: `targetPerSeed := job.Data.MaxResults / len(targets)`. If `targetPerSeed < 20`, clamp to `20`.
      - For each target `t` at index `i`, append to `seedJobs`:
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
    - Apply identical target fan-out with shared `dedup` and `exitMonitor` for `"threads"`, `"facebook"`, and `"tiktok"`.

### 3. Dynamic Infinite Scroll with Real-Time Counter & Early Exit
- Target: `twitter/job.go`:
  - Add `TargetCount int` field to `TwitterJob` struct.
  - Add option:
    ```go
    func WithTargetCount(count int) TwitterJobOptions {
        return func(j *TwitterJob) {
            j.TargetCount = count
        }
    }
    ```
  - In `NewTwitterJob`: default `job.TargetCount = 100` if `job.TargetCount <= 0`.
  - Update `attachGraphQLInterceptor(pwPage playwright.Page)` return signature:
    `(cleanup func(), getResponses func() []map[string]any, getTweetCount func() int)`.
    - In response handler: parse tweet IDs (`rest_id`, `legacy.id_str`) from intercepted `/i/api/graphql/` payloads into `seenIDs map[string]bool` under `mu.Lock()`.
    - Implement `getTweetCount`: returns `len(seenIDs)` under `mu.Lock()`.
  - Update `scrollDown(ctx context.Context, page scrapemate.BrowserPage, maxDepth int, targetCount int, getCount func() int) error`:
    - Loop ceiling: `maxDepth` if `maxDepth > 1`, else 50 iterations.
    - Each iteration:
      - Check `ctx.Done()`, return `ctx.Err()`.
      - Scroll bottom: `page.Eval("() => window.scrollTo(0, document.body.scrollHeight)")`.
      - Wait 1500ms for response and render.
      - Check count: `count := getCount()`. If `count == 0`, fallback to DOM count `() => document.querySelectorAll('article[data-testid="tweet"]').length`.
      - If `count >= targetCount`: return `nil` immediately (early exit).
      - Check scroll height: if `currentHeight == lastHeight` and count didn't increase:
        - `stuckCount++`.
        - If `stuckCount >= 2`: perform scroll jiggle (`window.scrollBy(0, -300)` followed by scroll to bottom).
        - If `stuckCount >= 4`: break loop (end of feed or rate limited).
        - If height or count increased: reset `stuckCount = 0`, update `lastHeight`.
  - In `TwitterJob.BrowserActions()`:
    - Pass `j.TargetCount` and `getTweetCount` callback into `scrollDown`.

### 4. Cross-Job and In-Job Deduplication via `Deduper`
- Target: `twitter/process.go`:
  - Update `processTweetObjects(ctx context.Context, objects []map[string]any, job *TwitterJob) []*Tweet`:
    - In the loop:
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
  - Update `extractTweetsFromGraphQL`, `extractTweetsFromNextData`, and `extractTweetsFromScripts` to pass `ctx` through to `processTweetObjects`.
  - In `extractTweetsFromDOM(ctx context.Context, doc *goquery.Document, job *TwitterJob)`:
    - Check `job.Deduper == nil || job.Deduper.AddIfNotExists(ctx, tweet.ID)` before appending to `tweets`.
- Targets: `tiktok/process.go`, `facebook/process.go`, `threads/process.go`:
  - Add `if job.Deduper != nil && !job.Deduper.AddIfNotExists(ctx, item.ID) { continue }` to extraction deduplication loops.

## Critical files & anchors
1. `runner/webrunner/webrunner.go:190-250`: `scrapeJob()` social seed generation and `parseSocialTargets`.
2. `twitter/job.go:180-350`: `attachGraphQLInterceptor`, `scrollDown`, and `BrowserActions`.
3. `twitter/process.go:160-220`: `processTweetObjects` deduplication via `job.Deduper.AddIfNotExists`.
4. `web/job.go:50-95`: `JobData` struct and `Validate()` with `MaxResults`.
5. `web/frontend/src/pages/Twitter.tsx:80-180`: Target tweet count input and multi-target hints.

## Verification
- Unit test in `twitter/job_test.go`:
  - `TestTwitterJob_TargetCount`: verify `WithTargetCount(150)` sets `TargetCount = 150`, and `NewTwitterJob` defaults to 100 when non-positive.
- Unit test in `twitter/process_test.go`:
  - `TestProcessTweetObjects_Deduper`: feed duplicate tweets across two `TwitterJob` instances sharing one `deduper.New()`; verify second job filters out existing tweets.
- Unit test in `runner/webrunner/webrunner_test.go`:
  - `TestParseSocialTargets`: verify comma-separated usernames, newline-separated queries, and keyword slices produce expected `socialTarget` slices with duplicates pruned.
- Full test and static analysis run:
  - `go test -v ./twitter/... ./runner/webrunner/... ./web/...`
  - `make vet`
  - `make test`

## Assumptions & contingencies
- Multi-target input delimiter: comma or newline separated. If queries contain commas inside quotes, standard comma split is used unless explicit quoted syntax is requested.
- Fallback for dynamic scroll count: if GraphQL response structure changes or is blocked by auth walls, `scrollDown` automatically falls back to counting DOM `article[data-testid="tweet"]` nodes.
