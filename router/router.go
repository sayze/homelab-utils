// Package router builds a chi router with the standard homelab middleware
// and a GET /health route already mounted. Register service routes on the
// returned router and pass it to an http.Server as its Handler.
//
// Every request gets an ID, one JSON log line via the logger package once
// it completes, and a 500 plus an error log line if its handler panics.
package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/sayze/homelab-utils/logger"
)

// HealthPath is the route that answers Nomad/Consul health checks.
const HealthPath = "/health"

// New returns a chi router with the standard middleware and HealthPath.
func New() chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(requestLogger)
	r.Use(recoverer)

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
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		defer func() {
			status := ww.Status()
			if status == 0 {
				// Handler wrote nothing; net/http sends an implicit 200.
				status = http.StatusOK
			}
			logger.Info("request",
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
func recoverer(next http.Handler) http.Handler {
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
			logger.Error("handler panicked",
				"panic", fmt.Sprint(rec),
				"stack", string(debug.Stack()),
				"request_id", middleware.GetReqID(r.Context()),
			)
			w.WriteHeader(http.StatusInternalServerError)
		}()

		next.ServeHTTP(w, r)
	})
}
