//go:build integration
// +build integration

package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestAgentHandler_CreateAgent_BadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 创建测试数据库
	db := newAgentHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := newTestAgentService(db, logger)
	h := NewAgentHandler(svc, logger)

	r := gin.New()
	r.POST("/api/agents", h.CreateAgent)

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "missing user_id",
			body:       `{"department":"support"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json",
			body:       `{invalid}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "non-existent user",
			body:       `{"user_id":9999}`,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/agents", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("CreateAgent() status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}
