package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
)

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
