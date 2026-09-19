package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
)

func TestAIHandler_Status_And_Query(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := aidelivery.NewAIService("", "")
	base.InitializeKnowledgeBase()
	h := NewAIHandler(aidelivery.NewHandlerServiceAdapter(base))

	r := gin.New()
	r.GET("/api/v1/ai/status", h.GetStatus)
	r.POST("/api/v1/ai/query", h.ProcessQuery)

	// status
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/ai/status", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status code: %d", w.Code)
	}

	// query
	payload := map[string]string{"query": "你好", "session_id": "s1"}
	buf, _ := json.Marshal(payload)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/api/v1/ai/query", bytes.NewReader(buf))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("query status code: %d, body=%s", w2.Code, w2.Body.String())
	}
}
