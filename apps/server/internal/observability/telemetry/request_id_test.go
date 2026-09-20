package telemetry

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestRequestIDMiddleware_GeneratesID(t *testing.T) {
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/test", func(c *gin.Context) {
		id := c.GetString(FieldRequestID)
		if id == "" {
			t.Fatal("expected request ID to be set in gin context")
		}
		c.String(http.StatusOK, id)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Response should contain X-Request-ID header
	respID := w.Header().Get("X-Request-ID")
	if respID == "" {
		t.Fatal("expected X-Request-ID header in response")
	}

	// Body should match header
	if w.Body.String() != respID {
		t.Fatalf("body %q != header %q", w.Body.String(), respID)
	}
}

func TestRequestIDMiddleware_PreservesExistingID(t *testing.T) {
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Request-ID", "existing-id-123")
	r.ServeHTTP(w, req)

	respID := w.Header().Get("X-Request-ID")
	if respID != "existing-id-123" {
		t.Fatalf("expected existing-id-123, got %q", respID)
	}
}

func TestRequestIDMiddleware_UUIDFormat(t *testing.T) {
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	respID := w.Header().Get("X-Request-ID")
	if len(respID) != 36 {
		t.Fatalf("expected UUID format (36 chars), got %d chars: %q", len(respID), respID)
	}
}
