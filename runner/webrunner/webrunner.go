package webrunner

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gosom/google-maps-scraper/cookiepool"
	"github.com/gosom/google-maps-scraper/deduper"
	"github.com/gosom/google-maps-scraper/exiter"
	"github.com/gosom/google-maps-scraper/facebook"
	"github.com/gosom/google-maps-scraper/runner"
	"github.com/gosom/google-maps-scraper/threads"
	"github.com/gosom/google-maps-scraper/tiktok"
	"github.com/gosom/google-maps-scraper/tlmt"
	"github.com/gosom/google-maps-scraper/twitter"
	"github.com/gosom/google-maps-scraper/web"
	"github.com/gosom/google-maps-scraper/web/sqlite"
	"github.com/gosom/scrapemate"
	"github.com/gosom/scrapemate/adapters/writers/csvwriter"
	"github.com/gosom/scrapemate/scrapemateapp"
	"golang.org/x/sync/errgroup"
)

type webrunner struct {
	srv       *web.Server
	svc       *web.Service
	cfg       *runner.Config
	setupMate func(context.Context, io.Writer, *web.Job) (mateRunner, error)
}

type mateRunner interface {
	Start(context.Context, ...scrapemate.IJob) error
	Close() error
}

func New(cfg *runner.Config) (runner.Runner, error) {
	if cfg.DataFolder == "" {
		return nil, fmt.Errorf("data folder is required")
	}

	if err := os.MkdirAll(cfg.DataFolder, os.ModePerm); err != nil {
		return nil, err
	}

	const dbfname = "jobs.db"

	dbpath := filepath.Join(cfg.DataFolder, dbfname)

	repo, err := sqlite.New(dbpath)
	if err != nil {
		return nil, err
	}

	svc := web.NewService(repo, cfg.DataFolder)

	srv, err := web.New(svc, cfg.Addr)
	if err != nil {
		return nil, err
	}

	ans := webrunner{
		srv:       srv,
		svc:       svc,
		cfg:       cfg,
		setupMate: defaultSetupMate(cfg),
	}

	return &ans, nil
}

func (w *webrunner) Run(ctx context.Context) error {
	egroup, ctx := errgroup.WithContext(ctx)

	egroup.Go(func() error {
		return w.work(ctx)
	})

	egroup.Go(func() error {
		return w.srv.Start(ctx)
	})

	return egroup.Wait()
}

func (w *webrunner) Close(context.Context) error {
	return nil
}

func (w *webrunner) work(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			jobs, err := w.svc.SelectPending(ctx)
			if err != nil {
				return err
			}

			for i := range jobs {
				select {
				case <-ctx.Done():
					return nil
				default:
					t0 := time.Now().UTC()
					if err := w.scrapeJob(ctx, &jobs[i]); err != nil {
						params := map[string]any{
							"job_count": len(jobs[i].Data.Keywords),
							"duration":  time.Now().UTC().Sub(t0).String(),
							"error":     err.Error(),
						}

						evt := tlmt.NewEvent("web_runner", params)

						_ = runner.Telemetry().Send(ctx, evt)

						log.Printf("error scraping job %s: %v", jobs[i].ID, err)
					} else {
						params := map[string]any{
							"job_count": len(jobs[i].Data.Keywords),
							"duration":  time.Now().UTC().Sub(t0).String(),
						}

						_ = runner.Telemetry().Send(ctx, tlmt.NewEvent("web_runner", params))

						log.Printf("job %s scraped successfully", jobs[i].ID)
					}
				}
			}
		}
	}
}

func (w *webrunner) scrapeJob(ctx context.Context, job *web.Job) error {
	job.Status = web.StatusWorking

	err := w.svc.Update(ctx, job)
	if err != nil {
		return err
	}

	jobType := job.Data.Type
	if jobType == "" {
		jobType = job.Type
	}

	outpath := filepath.Join(w.cfg.DataFolder, job.ID+".csv")

	outfile, err := os.Create(outpath)
	if err != nil {
		return err
	}

	defer func() {
		_ = outfile.Close()
	}()

	setupMate := w.setupMate
	if setupMate == nil {
		setupMate = defaultSetupMate(w.cfg)
	}

	mate, err := setupMate(ctx, outfile, job)
	if err != nil {
		job.Status = web.StatusFailed

		err2 := w.svc.Update(ctx, job)
		if err2 != nil {
			log.Printf("failed to update job status: %v", err2)
		}

		return err
	}

	defer mate.Close()

	dedup := deduper.New()
	exitMonitor := exiter.New()

	var seedJobs []scrapemate.IJob

	maxResults := job.Data.MaxResults
	if maxResults <= 0 {
		if job.Data.Depth <= 1 {
			maxResults = 100
		} else {
			maxResults = job.Data.Depth * 20
		}
	}

	switch jobType {
	case "twitter":
		targets := parseSocialTargets(job.Data.Username, job.Data.Query, job.Data.Keywords)
		if len(targets) == 0 {
			targets = []socialTarget{{Username: job.Data.Username, Query: job.Data.Query}}
		}
		accounts := resolveAccountCookies(job.Data.Cookie, "twitter")
		if len(targets) == 1 && len(accounts) > 1 {
			single := targets[0]
			targets = make([]socialTarget, len(accounts))
			for i := range accounts {
				targets[i] = single
			}
		}
		targetPerSeed := maxResults / len(targets)
		if targetPerSeed < 20 {
			targetPerSeed = 20
		}
		for i, t := range targets {
			var cookie string
			if len(accounts) > 0 {
				cookie = accounts[i%len(accounts)]
			}
			seedJobs = append(seedJobs, twitter.NewTwitterJob(
				fmt.Sprintf("%s-%d", job.ID, i),
				t.Username,
				t.Query,
				job.Data.Depth,
				twitter.WithDeduper(dedup),
				twitter.WithExitMonitor(exitMonitor),
				twitter.WithCookie(cookie),
				twitter.WithTargetCount(targetPerSeed),
			))
		}
	case "threads":
		targets := parseSocialTargets(job.Data.Username, job.Data.Query, job.Data.Keywords)
		if len(targets) == 0 {
			targets = []socialTarget{{Username: job.Data.Username, Query: job.Data.Query}}
		}
		accounts := resolveAccountCookies(job.Data.Cookie, "threads")
		if len(targets) == 1 && len(accounts) > 1 {
			single := targets[0]
			targets = make([]socialTarget, len(accounts))
			for i := range accounts {
				targets[i] = single
			}
		}
		targetPerSeed := maxResults / len(targets)
		if targetPerSeed < 20 {
			targetPerSeed = 20
		}
		for i, t := range targets {
			var cookie string
			if len(accounts) > 0 {
				cookie = accounts[i%len(accounts)]
			}
			seedJobs = append(seedJobs, threads.NewThreadsJob(
				fmt.Sprintf("%s-%d", job.ID, i),
				t.Username,
				job.Data.Depth,
				threads.WithQuery(t.Query),
				threads.WithDeduper(dedup),
				threads.WithExitMonitor(exitMonitor),
				threads.WithCookie(cookie),
				threads.WithTargetCount(targetPerSeed),
			))
		}
	case "facebook":
		targets := parseSocialTargets(job.Data.Username, job.Data.Query, job.Data.Keywords)
		if len(targets) == 0 {
			targets = []socialTarget{{Username: job.Data.Username, Query: job.Data.Query}}
		}
		accounts := resolveAccountCookies(job.Data.Cookie, "facebook")
		if len(targets) == 1 && len(accounts) > 1 {
			single := targets[0]
			targets = make([]socialTarget, len(accounts))
			for i := range accounts {
				targets[i] = single
			}
		}
		targetPerSeed := maxResults / len(targets)
		if targetPerSeed < 20 {
			targetPerSeed = 20
		}
		for i, t := range targets {
			var cookie string
			if len(accounts) > 0 {
				cookie = accounts[i%len(accounts)]
			}
			seedJobs = append(seedJobs, facebook.NewFacebookJob(
				fmt.Sprintf("%s-%d", job.ID, i),
				t.Username,
				t.Query,
				job.Data.Depth,
				facebook.WithDeduper(dedup),
				facebook.WithExitMonitor(exitMonitor),
				facebook.WithCookie(cookie),
				facebook.WithTargetCount(targetPerSeed),
			))
		}
	case "tiktok":
		targets := parseSocialTargets(job.Data.Username, job.Data.Query, job.Data.Keywords)
		if len(targets) == 0 {
			targets = []socialTarget{{Username: job.Data.Username, Query: job.Data.Query}}
		}
		accounts := resolveAccountCookies(job.Data.Cookie, "tiktok")
		if len(targets) == 1 && len(accounts) > 1 {
			single := targets[0]
			targets = make([]socialTarget, len(accounts))
			for i := range accounts {
				targets[i] = single
			}
		}
		targetPerSeed := maxResults / len(targets)
		if targetPerSeed < 20 {
			targetPerSeed = 20
		}
		for i, t := range targets {
			var cookie string
			if len(accounts) > 0 {
				cookie = accounts[i%len(accounts)]
			}
			seedJobs = append(seedJobs, tiktok.NewTikTokJob(
				fmt.Sprintf("%s-%d", job.ID, i),
				t.Username,
				t.Query,
				job.Data.Depth,
				tiktok.WithDeduper(dedup),
				tiktok.WithExitMonitor(exitMonitor),
				tiktok.WithCookie(cookie),
				tiktok.WithTargetCount(targetPerSeed),
			))
		}
	default:
		if len(job.Data.Keywords) == 0 {
			job.Status = web.StatusFailed
			return w.svc.Update(ctx, job)
		}

		var coords string
		if job.Data.Lat != "" && job.Data.Lon != "" {
			coords = job.Data.Lat + "," + job.Data.Lon
		}

		var err error
		seedJobs, err = runner.CreateSeedJobs(
			job.Data.FastMode,
			job.Data.Lang,
			strings.NewReader(strings.Join(job.Data.Keywords, "\n")),
			job.Data.Depth,
			job.Data.Email,
			coords,
			job.Data.Zoom,
			func() float64 {
				if job.Data.Radius <= 0 {
					return 10000 // 10 km
				}

				return float64(job.Data.Radius)
			}(),
			dedup,
			exitMonitor,
			w.cfg.ExtraReviews || job.Data.ExtraReviews,
		)
		if err != nil {
			err2 := w.svc.Update(ctx, job)
			if err2 != nil {
				log.Printf("failed to update job status: %v", err2)
			}

			return err
		}
	}

	if len(seedJobs) > 0 {
		exitMonitor.SetSeedCount(len(seedJobs))

		allowedSeconds := max(60, len(seedJobs)*10*job.Data.Depth/50+120)

		if job.Data.MaxTime > 0 {
			if job.Data.MaxTime.Seconds() < 180 {
				allowedSeconds = 180
			} else {
				allowedSeconds = int(job.Data.MaxTime.Seconds())
			}
		}

		log.Printf("running job %s with %d seed jobs and %d allowed seconds", job.ID, len(seedJobs), allowedSeconds)

		mateCtx, cancel := context.WithTimeout(ctx, time.Duration(allowedSeconds)*time.Second)
		defer cancel()

		exitMonitor.SetCancelFunc(cancel)

		go exitMonitor.Run(mateCtx)

		err = mate.Start(mateCtx, seedJobs...)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			cancel()

			err2 := w.svc.Update(ctx, job)
			if err2 != nil {
				log.Printf("failed to update job status: %v", err2)
			}

			return err
		}

		cancel()
	}

	job.Status = web.StatusOK

	return w.svc.Update(ctx, job)
}

func defaultSetupMate(cfg *runner.Config) func(context.Context, io.Writer, *web.Job) (mateRunner, error) {
	return func(_ context.Context, writer io.Writer, job *web.Job) (mateRunner, error) {
		concurrency := cfg.Concurrency
		if concurrency < 20 {
			concurrency = 20
		}

		opts := []func(*scrapemateapp.Config) error{
			scrapemateapp.WithConcurrency(concurrency),
			scrapemateapp.WithExitOnInactivity(time.Minute * 3),
		}

		isSocial := job.Data.Type == "twitter" || job.Data.Type == "threads" || job.Data.Type == "facebook" || job.Data.Type == "tiktok"
		if isSocial && job.Data.FastMode {
			// Social fast mode: direct HTTP fetcher without browser
		} else if !job.Data.FastMode {
			// Normal mode (Google Maps & Social): uses Playwright browser to execute BrowserActions
			opts = append(opts,
				scrapemateapp.WithJS(scrapemateapp.DisableImages()),
			)
		} else {
			// Fast mode for Google Maps: uses HTTP stealth client with Tile/RPC API
			opts = append(opts,
				scrapemateapp.WithStealth("firefox"),
			)
		}

		opts = runner.AppendBrowserCapacityOptions(opts, cfg)

		hasProxy := false

		if len(cfg.Proxies) > 0 {
			opts = append(opts, scrapemateapp.WithProxies(cfg.Proxies))
			hasProxy = true
		} else if len(job.Data.Proxies) > 0 {
			opts = append(opts,
				scrapemateapp.WithProxies(job.Data.Proxies),
			)
			hasProxy = true
		}

		if !cfg.DisablePageReuse {
			opts = append(opts,
				scrapemateapp.WithPageReuseLimit(2),
				scrapemateapp.WithBrowserReuseLimit(200),
			)
		}

		log.Printf("job %s has proxy: %v", job.ID, hasProxy)

		csvWriter := csvwriter.NewCsvWriter(csv.NewWriter(writer))

		writers := []scrapemate.ResultWriter{csvWriter}

		matecfg, err := scrapemateapp.NewConfig(
			writers,
			opts...,
		)
		if err != nil {
			return nil, err
		}

		return scrapemateapp.NewScrapeMateApp(matecfg)
	}
}

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
		u := strings.TrimSpace(username)
		q := strings.TrimSpace(query)
		if u != "" || q != "" {
			raw = append(raw, socialTarget{Username: u, Query: q})
		}
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

func resolveAccountCookies(rawCookie, platform string) []string {
	var list []string
	if rawCookie != "" {
		for _, l := range strings.Split(rawCookie, "\n") {
			l = strings.TrimSpace(l)
			if l != "" && !strings.HasPrefix(l, "#") {
				list = append(list, l)
			}
		}
	}
	if len(list) == 0 {
		list = cookiepool.GetDefaultPool().GetAllCookies(platform)
	}
	return list
}
