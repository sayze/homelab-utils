// Package router builds a chi router with the standard homelab middleware
// and a GET /health route already mounted. Register service routes on the
// returned router and pass it to an http.Server as its Handler.
//
// Every request gets an ID, one log line once it completes, and a 500 plus
// an error log line if its handler panics. Logs go to the Logger passed to
// WithLogger, or slog.Default() (which logger.Init sets) otherwise.
package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// HealthPath is the route that answers Nomad/Consul health checks.
const HealthPath = "/health"

// Logger is what the router logs through. *slog.Logger satisfies it.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// Option configures New.
type Option func(*config)

type config struct {
	log Logger
}

// logger returns the configured Logger, or slog.Default() at call time so
// a logger.Init after New still takes effect.
func (c config) logger() Logger {
	if c.log != nil {
		return c.log
	}
	return slog.Default()
}

// WithLogger sends the router's log lines to l instead of slog.Default().
// A nil l is ignored.
func WithLogger(l Logger) Option {
	return func(c *config) {
		if l != nil {
			c.log = l
		}
	}
}

// New returns a chi router with the standard middleware and HealthPath.
func New(opts ...Option) chi.Router {
	var c config
	for _, opt := range opts {
		opt(&c)
	}

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(c.requestLogger)
	r.Use(c.recoverer)

	r.Get(HealthPath, handleHealth)

	return r
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// requestLogger logs one JSON line per request once it completes. It
// replaces chi's middleware.Logger, which writes plain text.
func (c config) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		defer func() {
			status := ww.Status()
			if status == 0 {
				// Handler wrote nothing; net/http sends an implicit 200.
				status = http.StatusOK
			}
			c.logger().Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
				"remote_addr", r.RemoteAddr,
			)
		}()

		next.ServeHTTP(ww, r)
	})
}

// recoverer turns a handler panic into a 500 and a JSON error log line. It
// replaces chi's middleware.Recoverer, which prints a plain-text stack
// trace straight to stderr.
func (c config) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				// net/http's own signal to abort the response; let it through.
				panic(rec)
			}
			c.logger().Error("handler panicked",
				"panic", fmt.Sprint(rec),
				"stack", string(debug.Stack()),
				"request_id", middleware.GetReqID(r.Context()),
			)
			w.WriteHeader(http.StatusInternalServerError)
		}()

		next.ServeHTTP(w, r)
	})
}
