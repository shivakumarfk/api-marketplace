package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func TestRequestIDAndBearer(t *testing.T) {
	r := gin.New()
	r.Use(Recovery(slog.New(slog.NewTextHandler(io.Discard, nil))), RequestID())
	r.GET("/open", func(c *gin.Context) { c.String(200, GetRequestID(c)) })
	r.GET("/admin", BearerToken("s3cret"), func(c *gin.Context) { c.String(200, "ok") })
	r.GET("/panic", func(c *gin.Context) { panic("boom") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/open", nil)
	req.Header.Set("X-Request-Id", "client-chosen")
	r.ServeHTTP(w, req)
	if id := w.Header().Get("X-Request-Id"); !strings.HasPrefix(id, "req_") || w.Body.String() != id {
		t.Fatalf("request id %q body %q", id, w.Body.String())
	}

	for tok, want := range map[string]int{"": 401, "Bearer nope": 401, "Bearer s3cret": 200} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/admin", nil)
		if tok != "" {
			req.Header.Set("Authorization", tok)
		}
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Errorf("%q: got %d", tok, w.Code)
		}
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/panic", nil))
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), `"INTERNAL"`) {
		t.Fatalf("panic: %d %s", w.Code, w.Body.String())
	}
}

func TestEmptyTokenNeverMatches(t *testing.T) {
	r := gin.New()
	r.GET("/admin", BearerToken(""), func(c *gin.Context) { c.String(200, "ok") })
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("Authorization", "Bearer ")
	r.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("an unset admin token must lock the admin API, got %d", w.Code)
	}
}
