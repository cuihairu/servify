package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireResourcePermission_ReadWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("permissions", []string{"tickets.read"})
		c.Next()
	})
	r.Use(RequireResourcePermission("tickets"))
	r.GET("/tickets", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	r.POST("/tickets", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	// GET allowed with tickets.read
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/tickets", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET expected 200 got %d", w.Code)
	}

	// POST forbidden without tickets.write
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodPost, "/tickets", nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("POST expected 403 got %d", w2.Code)
	}
}
