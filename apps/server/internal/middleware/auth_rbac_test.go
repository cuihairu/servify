package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"servify/apps/server/internal/config"

	"github.com/gin-gonic/gin"
)

func createTestHS256JWT(t *testing.T, payload map[string]interface{}, secret string) string {
	t.Helper()

	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	enc := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	h := enc(headerJSON)
	p := enc(payloadJSON)
	signing := h + "." + p

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signing))
	sig := enc(mac.Sum(nil))
	return signing + "." + sig
}

func TestAuthMiddleware_RBACRoleExpansion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "test-secret"
	cfg := &config.Config{
		JWT: config.JWTConfig{Secret: secret},
		Security: config.SecurityConfig{
			RBAC: config.RBACConfig{
				Enabled: true,
				Roles: map[string][]string{
					"viewer": {"tickets.read"},
				},
			},
		},
	}

	now := time.Now()
	token := createTestHS256JWT(t, map[string]interface{}{
		"user_id": 1,
		"roles":   []string{"viewer"},
		"iat":     now.Unix(),
		"exp":     now.Add(10 * time.Minute).Unix(),
	}, secret)

	r := gin.New()
	r.Use(AuthMiddleware(cfg, nil))
	r.Use(RequireResourcePermission("tickets"))
	r.GET("/tickets", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	r.POST("/tickets", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	// GET allowed via role->permission mapping
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/tickets", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET expected 200 got %d body=%s", w.Code, w.Body.String())
	}

	// POST denied (viewer lacks tickets.write)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodPost, "/tickets", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("POST expected 403 got %d body=%s", w2.Code, w2.Body.String())
	}
}
