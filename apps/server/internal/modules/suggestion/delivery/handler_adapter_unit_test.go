package delivery_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	suggestionapp "servify/apps/server/internal/modules/suggestion/application"
	suggestioncontract "servify/apps/server/internal/modules/suggestion/contract"
	suggestiondelivery "servify/apps/server/internal/modules/suggestion/delivery"
	suggestioninfra "servify/apps/server/internal/modules/suggestion/infra"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// 无标签单测：覆盖 handler_adapter.go 的构造器与 Suggest 直通路径
// （integration 标签下的端到端测试由 handler_service_integration_test.go 负责）。
func newSuggestionUnitDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.ReplaceAll(t.Name(), "/", "_")
	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:suggestion_unit_"+name)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.Ticket{}, &models.KnowledgeDoc{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	return db
}

func TestSuggestionHandlerAdapterUnitConstruction(t *testing.T) {
	adapter := suggestiondelivery.NewHandlerService(newSuggestionUnitDB(t))
	if adapter == nil {
		t.Fatal("expected adapter instance")
	}
	var _ suggestiondelivery.HandlerService = adapter

	if suggestiondelivery.NewHandlerServiceAdapter(suggestionapp.NewService(suggestioninfra.NewGormRepository(newSuggestionUnitDB(t)))) == nil {
		t.Fatal("expected adapter instance from service")
	}
}

func TestSuggestionHandlerAdapterUnitSuggestPassthrough(t *testing.T) {
	db := newSuggestionUnitDB(t)
	base := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	if err := db.Create(&models.Ticket{
		ID: 5, Title: "printer broken", Description: "cannot print", CustomerID: 7,
		Status: "open", Category: "hardware", Priority: "high",
		TenantID: "t1", CreatedAt: base, UpdatedAt: base,
	}).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}

	adapter := suggestiondelivery.NewHandlerService(db)
	resp, err := adapter.Suggest(context.Background(), &suggestioncontract.SuggestionRequest{Query: "printer"})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if resp.Query != "printer" {
		t.Fatalf("unexpected query echo: %+v", resp)
	}
	if len(resp.SimilarTickets) != 1 || resp.SimilarTickets[0].ID != 5 {
		t.Fatalf("expected ticket suggestion, got %+v", resp.SimilarTickets)
	}
	if resp.Meta == nil || resp.Meta["tokens"] == nil {
		t.Fatalf("expected meta tokens, got %+v", resp.Meta)
	}

	// nil 请求走 application 层默认值路径
	resp, err = adapter.Suggest(context.Background(), nil)
	if err != nil {
		t.Fatalf("Suggest(nil) error = %v", err)
	}
	if resp == nil || resp.Query != "" {
		t.Fatalf("expected empty default response, got %+v", resp)
	}
}

func TestSuggestionHandlerAdapterUnitQuestionPassthrough(t *testing.T) {
	adapter := suggestiondelivery.NewHandlerServiceAdapter(
		suggestionapp.NewService(suggestioninfra.NewGormRepository(newSuggestionUnitDB(t))))
	ctx := context.Background()

	if _, err := adapter.InitialQuestions(ctx, &suggestioncontract.InitialQuestionsRequest{Limit: 1}); err != nil {
		t.Fatalf("InitialQuestions passthrough: %v", err)
	}
	if _, err := adapter.NextQuestions(ctx, &suggestioncontract.NextQuestionsRequest{Query: "refund"}); err != nil {
		t.Fatalf("NextQuestions passthrough: %v", err)
	}
}
