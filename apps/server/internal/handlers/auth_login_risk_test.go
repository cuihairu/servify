package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"servify/apps/server/internal/models"
	authdelivery "servify/apps/server/internal/modules/auth/delivery"
)

// TestAuthHandlerLoginBlockedByRisk：风险策略拦截映射为 403 且不回显判定
// 依据；审计留痕由 auth 面 audit 中间件（含失败）负责。
func TestAuthHandlerLoginBlockedByRisk(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &stubAuthService{loginErr: authdelivery.ErrLoginBlockedByRisk}
	handler := NewAuthHandler(svc)
	r := gin.New()
	r.POST("/api/v1/auth/login", handler.Login)

	body, _ := json.Marshal(map[string]string{"username": "risk-user", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("风险策略拦截")) {
		t.Fatalf("expected generic risk rejection message, got %s", w.Body.String())
	}
	// 不泄漏判定依据（来源标签/情报细节不得出现在响应里）。
	if bytes.Contains(w.Body.Bytes(), []byte("hosting")) {
		t.Fatalf("risk rationale must not leak, got %s", w.Body.String())
	}
}

// TestAuthHandlerLoginDirectOutcome：Login 正常返回时 handler 透传会话
// 结果（风险执行不改变成功响应形状）。
func TestAuthHandlerLoginDirectOutcome(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &stubAuthService{loginOutcome: &authdelivery.LoginOutcome{
		Result: &authdelivery.AuthResult{
			Token:            "access-1",
			ExpiresIn:        3600,
			RefreshToken:     "refresh-1",
			RefreshExpiresIn: 86400,
			User:             &models.User{ID: 7, Username: "demo", Role: "customer", Status: "active"},
		},
	}}
	handler := NewAuthHandler(svc)
	r := gin.New()
	r.POST("/api/v1/auth/login", handler.Login)

	body, _ := json.Marshal(map[string]string{"username": "demo", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.10:1234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"token":"access-1"`)) {
		t.Fatalf("expected session token in response: %s", w.Body.String())
	}
}

// TestHTTPSessionIPIntelligenceLoginNetworkLabel：适配 authdelivery.LoginRiskIntel
// 的方法——nil receiver 返回空串（安全标签，永不拦截），正常路径透传
// 情报标签。
func TestHTTPSessionIPIntelligenceLoginNetworkLabel(t *testing.T) {
	var provider *HTTPSessionIPIntelligence
	if got := provider.LoginNetworkLabel(context.Background(), "203.0.113.1"); got != "" {
		t.Fatalf("nil provider label = %q, want empty", got)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"network_label":"hosting"}`))
	}))
	defer srv.Close()

	provider = NewHTTPSessionIPIntelligence(srv.URL+"/lookup/{ip}", "", "", time.Second)
	if got := provider.LoginNetworkLabel(context.Background(), "203.0.113.1"); got != "hosting" {
		t.Fatalf("label = %q, want hosting", got)
	}
}
