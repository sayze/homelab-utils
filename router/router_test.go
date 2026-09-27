package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// entry is one call recorded by fakeLogger.
type entry struct {
	level, msg string
	args       map[string]any
}

type fakeLogger struct{ entries []entry }

func (f *fakeLogger) record(level, msg string, args []any) {
	m := map[string]any{}
	for i := 0; i+1 < len(args); i += 2 {
		m[args[i].(string)] = args[i+1]
	}
	f.entries = append(f.entries, entry{level, msg, m})
}

func (f *fakeLogger) Info(msg string, args ...any)  { f.record("INFO", msg, args) }
func (f *fakeLogger) Warn(msg string, args ...any)  { f.record("WARN", msg, args) }
func (f *fakeLogger) Error(msg string, args ...any) { f.record("ERROR", msg, args) }

func serve(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestNew_Routes(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string // JSON-compared when non-empty
	}{
		{"health check", HealthPath, http.StatusOK, `{"status": "ok"}`},
		{"root not routed", "/", http.StatusNotFound, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(New(), tt.path)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody != "" {
				assert.JSONEq(t, tt.wantBody, rec.Body.String())
			}
		})
	}
}

func TestNew_ServiceRoutesGetMiddleware(t *testing.T) {
	r := New()
	var reqID string
	r.Get("/thing", func(w http.ResponseWriter, r *http.Request) {
		reqID = middleware.GetReqID(r.Context())
		w.WriteHeader(http.StatusTeapot)
	})

	rec := serve(r, "/thing")

	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.NotEmpty(t, reqID, "request ID should be set")
}

func TestNew_PanicBecomes500(t *testing.T) {
	r := New()
	r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })

	rec := serve(r, "/boom")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestNew_AbortHandlerPanicPropagates(t *testing.T) {
	r := New()
	r.Get("/abort", func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })

	assert.PanicsWithValue(t, http.ErrAbortHandler, func() { serve(r, "/abort") })
}

func TestWithLogger_RequestLine(t *testing.T) {
	log := &fakeLogger{}
	rec := serve(New(WithLogger(log)), HealthPath)

	require.Len(t, log.entries, 1)
	got := log.entries[0]
	assert.Equal(t, "INFO", got.level)
	assert.Equal(t, "request", got.msg)
	assert.Equal(t, http.MethodGet, got.args["method"])
	assert.Equal(t, HealthPath, got.args["path"])
	assert.Equal(t, rec.Code, got.args["status"])
	assert.NotEmpty(t, got.args["request_id"])
}

func TestWithLogger_PanicLine(t *testing.T) {
	log := &fakeLogger{}
	r := New(WithLogger(log))
	r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })

	serve(r, "/boom")

	require.Len(t, log.entries, 2)
	assert.Equal(t, "ERROR", log.entries[0].level)
	assert.Equal(t, "handler panicked", log.entries[0].msg)
	assert.Equal(t, "boom", log.entries[0].args["panic"])
	assert.Equal(t, http.StatusInternalServerError, log.entries[1].args["status"])
}

func TestWithLogger_NilKeepsDefault(t *testing.T) {
	assert.NotPanics(t, func() { serve(New(WithLogger(nil)), HealthPath) })
}
