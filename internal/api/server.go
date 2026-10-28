// Package api is foundry's HTTP surface: the JSON API under /api/v1, the event stream, and the
// embedded web UI. Handlers read from the store and act through a Controller.
package api

import (
	"crypto/subtle"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/bobbyrc/foundry/internal/events"
	"github.com/bobbyrc/foundry/internal/store"
)

// Options configures NewServer.
type Options struct {
	Store      store.Store
	Bus        events.Bus
	Controller Controller
	// Token is the bearer token every route except health requires.
	Token   string
	Version string
	// Web is the UI to serve. Nil serves the build embedded from webdist (see DefaultWeb).
	Web    fs.FS
	Logger *slog.Logger
}

type server struct {
	opts Options
	log  *slog.Logger
	web  fs.FS
}

// NewServer returns the handler for every foundry HTTP route.
func NewServer(o Options) http.Handler {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	web := o.Web
	if web == nil {
		web = DefaultWeb()
	}
	s := &server{opts: o, log: o.Logger, web: web}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(s.requestLog)
	r.Route("/api/v1", func(r chi.Router) {
		r.NotFound(notFound)
		r.MethodNotAllowed(methodNotAllowed)
		r.Get("/health", s.health)
		r.Group(func(r chi.Router) {
			r.Use(s.auth)
			s.apiRoutes(r)
		})
	})
	// Any other /api/ path is a JSON 404 rather than the web UI.
	r.HandleFunc("/api/*", notFound)
	r.Handle("/*", http.HandlerFunc(s.serveWeb))
	return r
}

func (s *server) apiRoutes(r chi.Router) {
	r.Get("/events", s.events)
	r.Get("/events/history", s.eventHistory)
	r.Get("/stats", s.stats)

	r.Get("/control", s.getControl)
	r.Post("/control/pause", s.setPaused(true))
	r.Post("/control/resume", s.setPaused(false))

	r.Get("/projects", s.listProjects)
	r.Post("/projects/{name}/pause", s.setProjectPaused(true))
	r.Post("/projects/{name}/resume", s.setProjectPaused(false))
	r.Get("/agents", s.listAgents)

	r.Get("/tasks", s.listTasks)
	r.Post("/tasks", s.createTask)
	r.Get("/tasks/{id}", s.getTask)
	r.Post("/tasks/{id}/cancel", s.cancelTask)
	r.Post("/tasks/{id}/retry", s.retryTask)

	r.Get("/runs", s.listRuns)
	r.Get("/runs/{id}", s.getRun)
	r.Get("/runs/{id}/logs", s.runLogs)
	r.Post("/runs/{id}/cancel", s.cancelRun)

	r.Get("/approvals", s.listApprovals)
	r.Post("/approvals/{id}/decide", s.decideApproval)

	r.Get("/deployments", s.listDeployments)
	r.Get("/signals", s.listSignals)
	r.Get("/friction", s.listFriction)

	r.Get("/loops", s.listLoops)
	r.Post("/loops/{name}/run", s.runLoop)

	r.Get("/config", s.getConfig)
	r.Post("/config/reload", s.reloadConfig)

	r.Get("/conversations", s.listConversations)
	r.Post("/conversations", s.createConversation)
	r.Get("/conversations/{id}", s.getConversation)
	r.Post("/conversations/{id}/messages", s.sendMessage)
}

// auth requires the bearer token. GET /events also accepts ?token=, because browsers'
// EventSource cannot set headers. The health route is mounted outside this middleware.
func (s *server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := ""
		if h := r.Header.Get("Authorization"); len(h) > len("Bearer ") && strings.EqualFold(h[:len("Bearer ")], "Bearer ") {
			tok = h[len("Bearer "):]
		}
		if tok == "" && r.URL.Path == "/api/v1/events" {
			tok = r.URL.Query().Get("token")
		}
		// An empty configured token must never match; ConstantTimeCompare alone would
		// treat two empty values as equal.
		if tok == "" || s.opts.Token == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(s.opts.Token)) != 1 {
			writeErrorMsg(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestLog writes one line per request. Event streams and health checks log at debug level
// so they do not drown the log.
func (s *server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		level := slog.LevelInfo
		if r.URL.Path == "/api/v1/events" || r.URL.Path == "/api/v1/health" {
			level = slog.LevelDebug
		}
		s.log.Log(r.Context(), level, "http request",
			"method", r.Method, "path", r.URL.Path, "status", status, "duration", time.Since(start))
	})
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": s.opts.Version})
}

// fail writes the error response for err.
func (s *server) fail(w http.ResponseWriter, err error) {
	writeError(w, s.log, err)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	writeErrorMsg(w, http.StatusNotFound, "not found")
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeErrorMsg(w, http.StatusMethodNotAllowed, "method not allowed")
}
