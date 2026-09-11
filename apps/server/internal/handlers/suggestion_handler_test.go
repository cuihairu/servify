//go:build integration
// +build integration

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	suggestiondelivery "servify/apps/server/internal/modules/suggestion/delivery"
)

func newTestDBForSuggestions(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := uniqueMemDSN("file:suggestions_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.Ticket{}, &models.KnowledgeDoc{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestSuggestionHandler_Suggest_ReturnsIntentAndRecommendations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTestDBForSuggestions(t)

	now := time.Now()
	seedTickets := []models.Ticket{
		{Title: "无法登录账号", Description: "登录失败提示 error 500", Category: "technical", Priority: "high", Status: "open", CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)},
		{Title: "退款申请", Description: "想要退款，账单有问题", Category: "billing", Priority: "normal", Status: "open", CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-1 * time.Hour)},
		{Title: "一般咨询", Description: "请问你们支持哪些平台？", Category: "general", Priority: "low", Status: "open", CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour)},
	}
	for i := range seedTickets {
		if err := db.Create(&seedTickets[i]).Error; err != nil {
			t.Fatalf("seed ticket: %v", err)
		}
	}
	seedDocs := []models.KnowledgeDoc{
		{Title: "登录问题排查", Content: "如果无法登录，请检查账号与网络。", Category: "faq", Tags: "login,error", CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now.Add(-24 * time.Hour)},
		{Title: "退款政策", Content: "退款流程与账单说明。", Category: "billing", Tags: "refund,invoice", CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-48 * time.Hour)},
	}
	for i := range seedDocs {
		if err := db.Create(&seedDocs[i]).Error; err != nil {
			t.Fatalf("seed doc: %v", err)
		}
	}

	svc := suggestiondelivery.NewHandlerService(db)
	h := NewSuggestionHandler(svc)

	r := gin.New()
	api := r.Group("/api")
	RegisterSuggestionRoutes(api, h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/assist/suggest?query=无法登录+error&limit=3&doc_limit=3", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if resp["success"] != true {
		t.Fatalf("expected success=true got %v", resp["success"])
	}
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object got %T", resp["data"])
	}
	intent, ok := data["intent"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected intent object got %T", data["intent"])
	}
	if intent["label"] != "technical" && intent["label"] != "general" {
		t.Fatalf("unexpected intent label=%v", intent["label"])
	}

	tickets, ok := data["similar_tickets"].([]interface{})
	if !ok || len(tickets) == 0 {
		t.Fatalf("expected similar_tickets non-empty got %T len=%d", data["similar_tickets"], len(tickets))
	}
	docs, ok := data["knowledge_docs"].([]interface{})
	if !ok || len(docs) == 0 {
		t.Fatalf("expected knowledge_docs non-empty got %T len=%d", data["knowledge_docs"], len(docs))
	}
}
