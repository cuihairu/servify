//go:build integration
// +build integration

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	automationapp "servify/apps/server/internal/modules/automation/application"
	automationdelivery "servify/apps/server/internal/modules/automation/delivery"
)

func newTestDBForAutomations(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := uniqueMemDSN("file:automations_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.Ticket{}, &models.TicketComment{}, &models.AutomationTrigger{}, &models.AutomationRun{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestAutomationHandler_BatchRun_And_RunsList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTestDBForAutomations(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	svc := automationdelivery.NewHandlerService(db)
	h := NewAutomationHandler(svc)

	// seed ticket
	now := time.Now()
	ticket := &models.Ticket{
		Title:       "t1",
		Description: "hello",
		Priority:    "normal",
		Status:      "open",
		Category:    "general",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}

	// seed trigger: if ticket.priority == normal => set priority high + add_tag urgent
	trig, err := svc.CreateTrigger(context.Background(), &automationapp.TriggerRequest{
		Name:  "p-up",
		Event: "ticket_updated",
		Conditions: []automationapp.TriggerCondition{
			{Field: "ticket.priority", Op: "eq", Value: "normal"},
		},
		Actions: []automationapp.TriggerAction{
			{Type: "set_priority", Params: map[string]interface{}{"priority": "high"}},
			{Type: "add_tag", Params: map[string]interface{}{"tag": "urgent"}},
		},
	})
	if err != nil || trig == nil {
		t.Fatalf("create trigger: %v", err)
	}

	r := gin.New()
	api := r.Group("/api")
	RegisterAutomationRoutes(api, h)

	// dry-run
	w1 := httptest.NewRecorder()
	body1, _ := json.Marshal(map[string]interface{}{
		"event":      "ticket_updated",
		"ticket_ids": []uint{ticket.ID},
		"dry_run":    true,
	})
	req1, _ := http.NewRequest(http.MethodPost, "/api/automations/run", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("dry-run status=%d body=%s", w1.Code, w1.Body.String())
	}
	var dryResp automationapp.BatchRunResponse
	if err := json.Unmarshal(w1.Body.Bytes(), &dryResp); err != nil {
		t.Fatalf("unmarshal dry: %v body=%s", err, w1.Body.String())
	}
	if dryResp.DryRun != true || dryResp.Matches == 0 {
		t.Fatalf("unexpected dry resp: %#v", dryResp)
	}
	var afterDry models.Ticket
	if err := db.First(&afterDry, ticket.ID).Error; err != nil {
		t.Fatalf("load ticket: %v", err)
	}
	if afterDry.Priority != "normal" {
		t.Fatalf("dry-run should not change priority, got %s", afterDry.Priority)
	}

	// real run
	w2 := httptest.NewRecorder()
	body2, _ := json.Marshal(map[string]interface{}{
		"event":      "ticket_updated",
		"ticket_ids": []uint{ticket.ID},
		"dry_run":    false,
	})
	req2, _ := http.NewRequest(http.MethodPost, "/api/automations/run", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", w2.Code, w2.Body.String())
	}
	var after models.Ticket
	if err := db.First(&after, ticket.ID).Error; err != nil {
		t.Fatalf("load ticket: %v", err)
	}
	if after.Priority != "high" {
		t.Fatalf("expected priority=high got %s", after.Priority)
	}
	if after.Tags == "" || !strings.Contains(after.Tags, "urgent") {
		t.Fatalf("expected tag urgent, got %q", after.Tags)
	}

	// list runs
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodGet, "/api/automations/runs?page=1&page_size=10", nil)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("runs list status=%d body=%s", w3.Code, w3.Body.String())
	}
	var page PaginatedResponse
	if err := json.Unmarshal(w3.Body.Bytes(), &page); err != nil {
		t.Fatalf("unmarshal page: %v body=%s", err, w3.Body.String())
	}
	if page.Total < 1 {
		t.Fatalf("expected at least 1 run, got %d", page.Total)
	}
}
