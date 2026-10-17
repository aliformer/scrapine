package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

//go:embed static/dist static/spec static/redoc.html
var static embed.FS

type Server struct {
	srv *http.Server
	svc *Service
}

func New(svc *Service, addr string) (*Server, error) {
	ans := Server{
		svc: svc,
		srv: &http.Server{
			Addr:              addr,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       60 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
	}

	distFS, err := fs.Sub(static, "static/dist")
	if err != nil {
		return nil, err
	}

	staticFS, err := fs.Sub(static, "static")
	if err != nil {
		return nil, err
	}

	distServer := http.FileServer(http.FS(distFS))
	staticServer := http.FileServer(http.FS(staticFS))

	mux := http.NewServeMux()

	mux.Handle("/static/", http.StripPrefix("/static/", staticServer))
	mux.HandleFunc("/api/docs", ans.redocHandler)

	mux.HandleFunc("/api/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			ans.apiScrape(w, r)
		case http.MethodGet:
			ans.apiGetJobs(w, r)
		default:
			ans := apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			}
			renderJSON(w, http.StatusMethodNotAllowed, ans)
		}
	})

	mux.HandleFunc("/api/v1/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)

		switch r.Method {
		case http.MethodGet:
			ans.apiGetJob(w, r)
		case http.MethodDelete:
			ans.apiDeleteJob(w, r)
		default:
			ans := apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			}
			renderJSON(w, http.StatusMethodNotAllowed, ans)
		}
	})

	mux.HandleFunc("/api/v1/jobs/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)

		if r.Method != http.MethodGet {
			ans := apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			}
			renderJSON(w, http.StatusMethodNotAllowed, ans)
			return
		}

		ans.download(w, r)
	})

	mux.HandleFunc("/api/v1/jobs/{id}/places", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)

		if r.Method != http.MethodGet {
			ans := apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			}
			renderJSON(w, http.StatusMethodNotAllowed, ans)
			return
		}

		ans.apiGetJobPlaces(w, r)
	})

	mux.HandleFunc("/api/v1/jobs/{id}/tweets", func(w http.ResponseWriter, r *http.Request) {
		r = requestWithID(r)

		if r.Method != http.MethodGet {
			ans := apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			}
			renderJSON(w, http.StatusMethodNotAllowed, ans)
			return
		}

		ans.apiGetJobTweets(w, r)
	})

	mux.HandleFunc("/api/v1/auth/cookies/{platform}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			renderJSON(w, http.StatusMethodNotAllowed, apiError{
				Code:    http.StatusMethodNotAllowed,
				Message: "Method not allowed",
			})
			return
		}

		ans.apiSetAuthCookies(w, r)
	})

	// SPA fallback handler
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if f, err := distFS.Open(path); err == nil {
				_ = f.Close()
				distServer.ServeHTTP(w, r)
				return
			}
		}

		indexFile, err := distFS.Open("index.html")
		if err != nil {
			http.Error(w, "frontend not found", http.StatusNotFound)
			return
		}
		defer indexFile.Close()

		stat, err := indexFile.Stat()
		if err != nil {
			http.Error(w, "frontend not found", http.StatusNotFound)
			return
		}

		if rs, ok := indexFile.(io.ReadSeeker); ok {
			http.ServeContent(w, r, "index.html", stat.ModTime(), rs)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.Copy(w, indexFile)
	})

	ans.srv.Handler = securityHeaders(mux)

	return &ans, nil
}

func (s *Server) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		err := s.srv.Shutdown(context.Background())
		if err != nil {
			log.Println(err)
			return
		}
		log.Println("server stopped")
	}()

	fmt.Fprintf(os.Stderr, "visit http://localhost%s\n", s.srv.Addr)

	err := s.srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

type ctxKey string

const idCtxKey ctxKey = "id"

func requestWithID(r *http.Request) *http.Request {
	id := r.PathValue("id")
	if id == "" {
		id = r.URL.Query().Get("id")
	}

	parsed, err := uuid.Parse(id)
	if err == nil {
		r = r.WithContext(context.WithValue(r.Context(), idCtxKey, parsed))
	}

	return r
}

func getIDFromRequest(r *http.Request) (uuid.UUID, bool) {
	id, ok := r.Context().Value(idCtxKey).(uuid.UUID)
	return id, ok
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, ok := getIDFromRequest(r)
	if !ok {
		http.Error(w, "Invalid ID", http.StatusUnprocessableEntity)
		return
	}

	filePath, err := s.svc.GetCSV(ctx, id.String())
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	fileName := filepath.Base(filePath)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	w.Header().Set("Content-Type", "text/csv")

	_, err = io.Copy(w, file)
	if err != nil {
		http.Error(w, "Failed to send file", http.StatusInternalServerError)
		return
	}
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type apiScrapeRequest struct {
	Name         string        `json:"name"`
	Type         string        `json:"type,omitempty"`
	Username     string        `json:"username,omitempty"`
	Query        string        `json:"query,omitempty"`
	Keywords     []string      `json:"keywords"`
	Lang         string        `json:"lang"`
	Zoom         int           `json:"zoom"`
	Lat          string        `json:"lat"`
	Lon          string        `json:"lon"`
	FastMode     bool          `json:"fast_mode"`
	Radius       int           `json:"radius"`
	Depth        int           `json:"depth"`
	MaxResults   int           `json:"max_results,omitempty"`
	Email        bool          `json:"email"`
	ExtraReviews bool          `json:"extra_reviews"`
	MaxTime      time.Duration `json:"max_time"`
	Proxies      []string      `json:"proxies"`
	Cookie       string        `json:"cookie,omitempty"`
}

type apiScrapeResponse struct {
	ID string `json:"id"`
}

func (s *Server) redocHandler(w http.ResponseWriter, _ *http.Request) {
	redocFile, err := static.Open("static/redoc.html")
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html><html><head><title>API Documentation</title><meta charset="utf-8"/><meta name="viewport" content="width=device-width, initial-scale=1"><link href="https://fonts.googleapis.com/css?family=Montserrat:300,400,700|Roboto:300,400,700" rel="stylesheet"></head><body><redoc spec-url="/static/spec/spec.yaml"></redoc><script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script></body></html>`)
		return
	}
	defer redocFile.Close()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.Copy(w, redocFile)
}

func (s *Server) apiScrape(w http.ResponseWriter, r *http.Request) {
	var req apiScrapeRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		ans := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusUnprocessableEntity, ans)
		return
	}

	jobType := strings.ToLower(strings.TrimSpace(req.Type))
	if jobType == "" {
		jobType = "gmaps"
	}

	newJob := Job{
		ID:     uuid.New().String(),
		Name:   req.Name,
		Date:   time.Now().UTC(),
		Status: StatusPending,
		Type:   jobType,
		Data: JobData{
			Type:         jobType,
			Username:     req.Username,
			Query:        req.Query,
			Keywords:     req.Keywords,
			Lang:         req.Lang,
			Zoom:         req.Zoom,
			Lat:          req.Lat,
			Lon:          req.Lon,
			FastMode:     req.FastMode,
			Radius:       req.Radius,
			Depth:        req.Depth,
			MaxResults:   req.MaxResults,
			Email:        req.Email,
			ExtraReviews: req.ExtraReviews,
			MaxTime:      req.MaxTime,
			Proxies:      req.Proxies,
			Cookie:       req.Cookie,
		},
	}

	if jobType == "gmaps" {
		newJob.Data.MaxTime *= time.Second
	}

	err = newJob.Validate()
	if err != nil {
		ans := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusUnprocessableEntity, ans)
		return
	}

	err = s.svc.Create(r.Context(), &newJob)
	if err != nil {
		ans := apiError{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusInternalServerError, ans)
		return
	}

	ans := apiScrapeResponse{
		ID: newJob.ID,
	}

	renderJSON(w, http.StatusCreated, ans)
}

func (s *Server) apiGetJobs(w http.ResponseWriter, r *http.Request) {
	jobType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))

	var (
		jobs []Job
		err  error
	)

	if jobType != "" {
		jobs, err = s.svc.AllWithType(r.Context(), jobType)
	} else {
		jobs, err = s.svc.All(r.Context())
	}

	if err != nil {
		apiError := apiError{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusInternalServerError, apiError)
		return
	}

	if jobs == nil {
		jobs = []Job{}
	}

	renderJSON(w, http.StatusOK, jobs)
}

func (s *Server) apiGetJob(w http.ResponseWriter, r *http.Request) {
	id, ok := getIDFromRequest(r)
	if !ok {
		apiError := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: "Invalid ID",
		}

		renderJSON(w, http.StatusUnprocessableEntity, apiError)
		return
	}

	job, err := s.svc.Get(r.Context(), id.String())
	if err != nil {
		apiError := apiError{
			Code:    http.StatusNotFound,
			Message: http.StatusText(http.StatusNotFound),
		}

		renderJSON(w, http.StatusNotFound, apiError)
		return
	}

	renderJSON(w, http.StatusOK, job)
}

func (s *Server) apiGetJobPlaces(w http.ResponseWriter, r *http.Request) {
	id, ok := getIDFromRequest(r)
	if !ok {
		apiError := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: "Invalid ID",
		}
		renderJSON(w, http.StatusUnprocessableEntity, apiError)
		return
	}

	places, err := s.svc.GetPlaces(r.Context(), id.String())
	if err != nil {
		if !errors.Is(err, ErrPlacesNotFound) {
			log.Printf("api view job %s: %v", id, err)
		}
		places = []Place{}
	}

	renderJSON(w, http.StatusOK, places)
}

func (s *Server) apiGetJobTweets(w http.ResponseWriter, r *http.Request) {
	id, ok := getIDFromRequest(r)
	if !ok {
		apiError := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: "Invalid ID",
		}
		renderJSON(w, http.StatusUnprocessableEntity, apiError)
		return
	}

	tweets, err := s.svc.GetTweets(r.Context(), id.String())
	if err != nil {
		if !errors.Is(err, ErrTweetsNotFound) {
			log.Printf("api view job tweets %s: %v", id, err)
		}
		tweets = []TweetDTO{}
	}

	renderJSON(w, http.StatusOK, tweets)
}

func (s *Server) apiDeleteJob(w http.ResponseWriter, r *http.Request) {
	id, ok := getIDFromRequest(r)
	if !ok {
		apiError := apiError{
			Code:    http.StatusUnprocessableEntity,
			Message: "Invalid ID",
		}

		renderJSON(w, http.StatusUnprocessableEntity, apiError)
		return
	}

	err := s.svc.Delete(r.Context(), id.String())
	if err != nil {
		apiError := apiError{
			Code:    http.StatusInternalServerError,
			Message: err.Error(),
		}

		renderJSON(w, http.StatusInternalServerError, apiError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

type apiSetCookiesRequest struct {
	Cookies string `json:"cookies"`
}

func (s *Server) apiSetAuthCookies(w http.ResponseWriter, r *http.Request) {
	platform := r.PathValue("platform")

	validPlatforms := map[string]bool{
		"twitter":  true,
		"tiktok":   true,
		"threads":  true,
		"facebook": true,
	}

	if !validPlatforms[platform] {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: "Invalid platform. Supported: twitter, tiktok, threads, facebook",
		})
		return
	}

	var req apiSetCookiesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: "Invalid request body",
		})
		return
	}

	if strings.TrimSpace(req.Cookies) == "" {
		renderJSON(w, http.StatusBadRequest, apiError{
			Code:    http.StatusBadRequest,
			Message: "Cookies cannot be empty",
		})
		return
	}

	// Save cookies to cookies/{platform}.txt
	cookieDir := "cookies"
	if err := os.MkdirAll(cookieDir, 0755); err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{
			Code:    http.StatusInternalServerError,
			Message: "Failed to create cookies directory",
		})
		return
	}

	cookiePath := filepath.Join(cookieDir, platform+".txt")
	if err := os.WriteFile(cookiePath, []byte(strings.TrimSpace(req.Cookies)), 0600); err != nil {
		renderJSON(w, http.StatusInternalServerError, apiError{
			Code:    http.StatusInternalServerError,
			Message: "Failed to save cookies",
		})
		return
	}

	renderJSON(w, http.StatusOK, map[string]string{
		"status":   "ok",
		"platform": platform,
	})
}

func renderJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' cdn.redoc.ly cdn.tailwindcss.com cdnjs.cloudflare.com 'unsafe-inline' 'unsafe-eval'; "+
				"worker-src 'self' blob:; "+
				"style-src 'self' 'unsafe-inline' fonts.googleapis.com cdnjs.cloudflare.com; "+
				"img-src 'self' data: cdn.redoc.ly cdnjs.cloudflare.com *.tile.openstreetmap.org; "+
				"font-src 'self' fonts.gstatic.com; "+
				"connect-src 'self'")

		next.ServeHTTP(w, r)
	})
}
