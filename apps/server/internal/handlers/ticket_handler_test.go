//go:build integration
// +build integration

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	ticketcontract "servify/apps/server/internal/modules/ticket/contract"
	ticketdelivery "servify/apps/server/internal/modules/ticket/delivery"
	auditplatform "servify/apps/server/internal/platform/audit"
)

type ticketAuditRecorder struct {
	entries []auditplatform.Entry
}

type stubTicketHandlerService struct {
	updateErr  error
	assignErr  error
	commentErr error
	closeErr   error
}

func (r *ticketAuditRecorder) Record(_ context.Context, entry auditplatform.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}

func (s stubTicketHandlerService) CreateTicket(ctx context.Context, req *ticketcontract.CreateTicketRequest) (*models.Ticket, error) {
	return nil, errors.New("not implemented")
}

func (s stubTicketHandlerService) GetTicketByID(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	return nil, errors.New("record not found")
}

func (s stubTicketHandlerService) UpdateTicket(ctx context.Context, ticketID uint, req *ticketcontract.UpdateTicketRequest, userID uint) (*models.Ticket, error) {
	return nil, s.updateErr
}

func (s stubTicketHandlerService) ListTickets(ctx context.Context, req *ticketcontract.ListTicketRequest) ([]models.Ticket, int64, error) {
	return nil, 0, errors.New("not implemented")
}

func (s stubTicketHandlerService) ListTicketCustomFields(ctx context.Context, activeOnly bool) ([]models.CustomField, error) {
	return nil, errors.New("not implemented")
}

func (s stubTicketHandlerService) AssignTicket(ctx context.Context, ticketID uint, agentID uint, assignerID uint) error {
	return s.assignErr
}

func (s stubTicketHandlerService) AddComment(ctx context.Context, ticketID uint, userID uint, content string, commentType string) (*models.TicketComment, error) {
	return nil, s.commentErr
}

func (s stubTicketHandlerService) CloseTicket(ctx context.Context, ticketID uint, userID uint, reason string) error {
	return s.closeErr
}

func (s stubTicketHandlerService) GetTicketStats(ctx context.Context, agentID *uint) (*ticketcontract.TicketStats, error) {
	return &ticketcontract.TicketStats{}, nil
}

func (s stubTicketHandlerService) BulkUpdateTickets(ctx context.Context, req *ticketcontract.BulkUpdateTicketRequest, userID uint) (*ticketcontract.BulkUpdateResult, error) {
	return nil, errors.New("not implemented")
}

func (s stubTicketHandlerService) GetRelatedConversations(ctx context.Context, ticketID uint) ([]models.Session, error) {
	return nil, nil
}

func newTestDBForTickets(t *testing.T) *gorm.DB {
	t.Helper()

	// Use shared in-memory DB; ticket orchestration may spawn goroutines that
	// use a different connection.
	dsn := uniqueMemDSN("file:ticket_handler_" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()) + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	// Ticket read paths preload these associations; keep schema in sync.
	if err := db.AutoMigrate(
		&models.User{},
		&models.Agent{},
		&models.Session{},
		&models.Ticket{},
		&models.CustomField{},
		&models.TicketCustomFieldValue{},
		&models.TicketStatus{},
		&models.TicketComment{},
		&models.TicketFile{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	return db
}

func TestTicketHandler_Create_Get_List_Assign(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForTickets(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Seed a customer and an agent user; ticket creation validates customer existence.
	if err := db.Create(&models.User{ID: 1, Username: "c1", Name: "c1", Email: "c1@example.com", Role: "customer"}).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if err := db.Create(&models.User{ID: 2, Username: "a1", Name: "a1", Email: "a1@example.com", Role: "agent"}).Error; err != nil {
		t.Fatalf("seed agent user: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: 2, Status: "online", MaxConcurrent: 5, CurrentLoad: 0}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := db.Create(&models.User{ID: 3, Username: "a2", Name: "a2", Email: "a2@example.com", Role: "agent"}).Error; err != nil {
		t.Fatalf("seed agent user 2: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: 3, Status: "online", MaxConcurrent: 5, CurrentLoad: 0}).Error; err != nil {
		t.Fatalf("seed agent 2: %v", err)
	}

	h := NewTicketHandler(ticketdelivery.NewHandlerServiceWithDependencies(ticketdelivery.HandlerAssemblyDependencies{
		DB:     db,
		Logger: logger,
	}), logger)

	r := gin.New()
	r.POST("/api/tickets", h.CreateTicket)
	r.POST("/api/tickets/bulk", h.BulkUpdateTickets)
	r.GET("/api/tickets", h.ListTickets)
	r.GET("/api/tickets/:id", h.GetTicket)
	r.POST("/api/tickets/:id/assign", h.AssignTicket)

	// Create ticket
	createBody := map[string]any{
		"title":       "hello",
		"description": "desc",
		"customer_id": 1,
		"priority":    "normal",
		"category":    "general",
	}
	b, _ := json.Marshal(createBody)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/tickets", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var created models.Ticket
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created: %v body=%s", err, w.Body.String())
	}
	if created.ID == 0 {
		t.Fatalf("expected created ticket id")
	}

	// Get ticket
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/api/tickets/"+toStr(created.ID), nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", w2.Code, w2.Body.String())
	}

	// List tickets (no filters)
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodGet, "/api/tickets?page=1&page_size=10", nil)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", w3.Code, w3.Body.String())
	}

	// Assign ticket to agent (agent_id here is user_id per service logic).
	assignBody := map[string]any{"agent_id": 2}
	b2, _ := json.Marshal(assignBody)
	w4 := httptest.NewRecorder()
	req4, _ := http.NewRequest(http.MethodPost, "/api/tickets/"+toStr(created.ID)+"/assign", bytes.NewReader(b2))
	req4.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("assign status=%d body=%s", w4.Code, w4.Body.String())
	}

	// Re-assign (transfer) to another agent; should decrement old load and increment new load.
	assignBody2 := map[string]any{"agent_id": 3}
	bTransfer, _ := json.Marshal(assignBody2)
	w4b := httptest.NewRecorder()
	req4b, _ := http.NewRequest(http.MethodPost, "/api/tickets/"+toStr(created.ID)+"/assign", bytes.NewReader(bTransfer))
	req4b.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w4b, req4b)
	if w4b.Code != http.StatusOK {
		t.Fatalf("transfer assign status=%d body=%s", w4b.Code, w4b.Body.String())
	}

	var agent2 models.Agent
	if err := db.Where("user_id = ?", 2).First(&agent2).Error; err != nil {
		t.Fatalf("load agent 2: %v", err)
	}
	var agent3 models.Agent
	if err := db.Where("user_id = ?", 3).First(&agent3).Error; err != nil {
		t.Fatalf("load agent 3: %v", err)
	}
	if agent2.CurrentLoad != 0 {
		t.Fatalf("expected agent 2 load decremented to 0, got %d", agent2.CurrentLoad)
	}
	if agent3.CurrentLoad != 1 {
		t.Fatalf("expected agent 3 load incremented to 1, got %d", agent3.CurrentLoad)
	}

	// Bulk update: add tags + set status
	bulkBody := map[string]any{
		"ticket_ids":  []uint{created.ID},
		"status":      "resolved",
		"add_tags":    []string{"vip", "urgent"},
		"remove_tags": []string{"non-existent"},
	}
	b3, _ := json.Marshal(bulkBody)
	w5 := httptest.NewRecorder()
	req5, _ := http.NewRequest(http.MethodPost, "/api/tickets/bulk", bytes.NewReader(b3))
	req5.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w5, req5)
	if w5.Code != http.StatusOK {
		t.Fatalf("bulk status=%d body=%s", w5.Code, w5.Body.String())
	}

	var updated models.Ticket
	if err := db.First(&updated, created.ID).Error; err != nil {
		t.Fatalf("load ticket after bulk: %v", err)
	}
	if updated.Status != "resolved" {
		t.Fatalf("expected status resolved, got %q", updated.Status)
	}
	if updated.Tags == "" {
		t.Fatalf("expected tags to be set after bulk update")
	}

	// Bulk unassign
	bulkUnassign := map[string]any{
		"ticket_ids":     []uint{created.ID},
		"unassign_agent": true,
		"remove_tags":    []string{"vip"},
		"add_tags":       []string{"after-unassign"},
	}
	b4, _ := json.Marshal(bulkUnassign)
	w6 := httptest.NewRecorder()
	req6, _ := http.NewRequest(http.MethodPost, "/api/tickets/bulk", bytes.NewReader(b4))
	req6.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w6, req6)
	if w6.Code != http.StatusOK {
		t.Fatalf("bulk unassign status=%d body=%s", w6.Code, w6.Body.String())
	}
	var after models.Ticket
	if err := db.First(&after, created.ID).Error; err != nil {
		t.Fatalf("load ticket after bulk unassign: %v", err)
	}
	if after.AgentID != nil {
		t.Fatalf("expected agent_id to be nil after unassign")
	}
}

func TestTicketHandler_CustomFields_Create_Filter_Export(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForTickets(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.Create(&models.User{ID: 1, Username: "c1", Name: "c1", Email: "c1@example.com", Role: "customer"}).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	now := time.Now()
	if err := db.Create(&models.CustomField{
		Resource:    "ticket",
		Key:         "company_size",
		Name:        "Company Size",
		Type:        "select",
		Required:    true,
		Active:      true,
		OptionsJSON: `["small","large"]`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("seed custom field: %v", err)
	}

	h := NewTicketHandler(ticketdelivery.NewHandlerServiceWithDependencies(ticketdelivery.HandlerAssemblyDependencies{
		DB:     db,
		Logger: logger,
	}), logger)

	r := gin.New()
	r.POST("/api/tickets", h.CreateTicket)
	r.GET("/api/tickets", h.ListTickets)
	r.GET("/api/tickets/export", h.ExportTicketsCSV)

	// create ticket with custom field
	body := map[string]any{
		"title":       "T1",
		"customer_id": 1,
		"priority":    "normal",
		"custom_fields": map[string]any{
			"company_size": "large",
		},
	}
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/tickets", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}

	// list with cf filter
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/api/tickets?cf.company_size=large&page=1&page_size=10", nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", w2.Code, w2.Body.String())
	}

	// export with cf filter
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodGet, "/api/tickets/export?cf.company_size=large&limit=10", nil)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", w3.Code, w3.Body.String())
	}
	csvText := w3.Body.String()
	if !strings.Contains(csvText, "cf.company_size") {
		t.Fatalf("expected csv header to include custom field, got: %s", csvText)
	}
	if !strings.Contains(csvText, ",large") {
		t.Fatalf("expected csv to include custom field value, got: %s", csvText)
	}
}

func TestTicketHandler_GetAndListExposeTicketViewFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForTickets(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	now := time.Now()
	if err := db.Create(&models.User{ID: 1, Username: "customer1", Name: "客户一", Email: "c1@example.com", Role: "customer"}).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	if err := db.Create(&models.User{ID: 2, Username: "agent1", Name: "客服一", Email: "a1@example.com", Role: "agent"}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := db.Create(&models.CustomField{
		Resource:  "ticket",
		Key:       "remote_assist",
		Name:      "Remote Assist",
		Type:      "string",
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed custom field: %v", err)
	}

	sessionID := "sess-ra-1"
	ticket := &models.Ticket{
		Title:       "远程协助跟进 - 客户一",
		Description: "desc",
		CustomerID:  1,
		AgentID:     ptrUintTicket(2),
		SessionID:   &sessionID,
		Status:      "open",
		Priority:    "high",
		Category:    "remote-assist",
		Source:      "remote_assist",
		Tags:        "remote_assist,followup",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	var field models.CustomField
	if err := db.Where("key = ?", "remote_assist").First(&field).Error; err != nil {
		t.Fatalf("load custom field: %v", err)
	}
	if err := db.Create(&models.TicketCustomFieldValue{
		TicketID:      ticket.ID,
		CustomFieldID: field.ID,
		Value:         `{"session_id":"sess-ra-1"}`,
		CreatedAt:     now,
		UpdatedAt:     now,
	}).Error; err != nil {
		t.Fatalf("seed custom field value: %v", err)
	}

	h := NewTicketHandler(ticketdelivery.NewHandlerServiceWithDependencies(ticketdelivery.HandlerAssemblyDependencies{
		DB:     db,
		Logger: logger,
	}), logger)

	r := gin.New()
	r.GET("/api/tickets", h.ListTickets)
	r.GET("/api/tickets/:id", h.GetTicket)

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/api/tickets/"+toStr(ticket.ID), nil)
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", w1.Code, w1.Body.String())
	}
	var got struct {
		CustomerName string                 `json:"customer_name"`
		AgentName    string                 `json:"agent_name"`
		Source       string                 `json:"source"`
		SessionID    string                 `json:"session_id"`
		Tags         string                 `json:"tags"`
		TagList      []string               `json:"tag_list"`
		CustomFields map[string]interface{} `json:"custom_fields"`
	}
	if err := json.Unmarshal(w1.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal get: %v body=%s", err, w1.Body.String())
	}
	if got.CustomerName != "客户一" || got.AgentName != "客服一" {
		t.Fatalf("expected customer/agent names, got %+v", got)
	}
	if got.Source != "remote_assist" || got.SessionID != "sess-ra-1" {
		t.Fatalf("expected source/session in response, got %+v", got)
	}
	if got.Tags != "remote_assist,followup" || len(got.TagList) != 2 {
		t.Fatalf("expected tags and tag_list in response, got %+v", got)
	}
	if got.CustomFields["remote_assist"] != `{"session_id":"sess-ra-1"}` {
		t.Fatalf("expected custom_fields.remote_assist, got %+v", got.CustomFields)
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/tickets?page=1&page_size=10", nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", w2.Code, w2.Body.String())
	}
	var listResp struct {
		Data []struct {
			Source       string                 `json:"source"`
			TagList      []string               `json:"tag_list"`
			CustomFields map[string]interface{} `json:"custom_fields"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list: %v body=%s", err, w2.Body.String())
	}
	if len(listResp.Data) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(listResp.Data))
	}
	if listResp.Data[0].Source != "remote_assist" || len(listResp.Data[0].TagList) != 2 {
		t.Fatalf("expected source/tag_list in list response, got %+v", listResp.Data[0])
	}

	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/api/tickets?page=1&page_size=10&source=remote_assist&tag=followup", nil)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("list filtered status=%d body=%s", w3.Code, w3.Body.String())
	}
	var filteredResp struct {
		Data []struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w3.Body.Bytes(), &filteredResp); err != nil {
		t.Fatalf("unmarshal filtered list: %v body=%s", err, w3.Body.String())
	}
	if len(filteredResp.Data) != 1 || filteredResp.Data[0].ID != ticket.ID {
		t.Fatalf("expected filtered response to contain remote assist ticket, got %+v", filteredResp.Data)
	}
}

func TestTicketHandler_AuditSnapshotsForUpdateAssignClose(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForTickets(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.Create(&models.User{ID: 1, Username: "c1", Name: "c1", Email: "c1@example.com", Role: "customer"}).Error; err != nil {
		t.Fatalf("seed customer user: %v", err)
	}
	if err := db.Create(&models.User{ID: 2, Username: "a1", Name: "a1", Email: "a1@example.com", Role: "agent"}).Error; err != nil {
		t.Fatalf("seed agent user 1: %v", err)
	}
	if err := db.Create(&models.User{ID: 3, Username: "a2", Name: "a2", Email: "a2@example.com", Role: "agent"}).Error; err != nil {
		t.Fatalf("seed agent user 2: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: 2, Status: "online", MaxConcurrent: 5}).Error; err != nil {
		t.Fatalf("seed agent 1: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: 3, Status: "online", MaxConcurrent: 5}).Error; err != nil {
		t.Fatalf("seed agent 2: %v", err)
	}
	ticket := &models.Ticket{
		Title:      "before-title",
		CustomerID: 1,
		Status:     "open",
		Priority:   "normal",
		Category:   "general",
	}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}

	h := NewTicketHandler(ticketdelivery.NewHandlerServiceWithDependencies(ticketdelivery.HandlerAssemblyDependencies{
		DB:     db,
		Logger: logger,
	}), logger)

	recorder := &ticketAuditRecorder{}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(99))
		c.Set("principal_kind", "agent")
		c.Next()
	})
	r.Use(auditplatform.Middleware(recorder))
	r.PUT("/api/tickets/:id", h.UpdateTicket)
	r.POST("/api/tickets/:id/assign", h.AssignTicket)
	r.POST("/api/tickets/:id/close", h.CloseTicket)

	updateBody, _ := json.Marshal(map[string]any{"title": "after-title"})
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPut, "/api/tickets/"+toStr(ticket.ID), bytes.NewReader(updateBody))
	req1.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", w1.Code, w1.Body.String())
	}
	var updatedTicket models.Ticket
	if err := db.First(&updatedTicket, ticket.ID).Error; err != nil {
		t.Fatalf("load updated ticket: %v", err)
	}
	if updatedTicket.Title != "after-title" {
		t.Fatalf("updated title=%q want after-title", updatedTicket.Title)
	}

	assignBody, _ := json.Marshal(map[string]any{"agent_id": 2})
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/tickets/"+toStr(ticket.ID)+"/assign", bytes.NewReader(assignBody))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("assign status=%d body=%s", w2.Code, w2.Body.String())
	}

	closeBody, _ := json.Marshal(map[string]any{"reason": "done"})
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/api/tickets/"+toStr(ticket.ID)+"/close", bytes.NewReader(closeBody))
	req3.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("close status=%d body=%s", w3.Code, w3.Body.String())
	}
	var closedTicket models.Ticket
	if err := db.First(&closedTicket, ticket.ID).Error; err != nil {
		t.Fatalf("load closed ticket: %v", err)
	}
	if closedTicket.Status != "closed" {
		t.Fatalf("closed status=%q want closed", closedTicket.Status)
	}
	if closedTicket.ClosedAt == nil || closedTicket.ClosedAt.IsZero() {
		t.Fatalf("expected closed_at to be set")
	}

	if len(recorder.entries) != 3 {
		t.Fatalf("expected 3 audit entries got %d", len(recorder.entries))
	}
	for _, entry := range recorder.entries {
		if entry.BeforeJSON == "" || entry.AfterJSON == "" {
			t.Fatalf("expected before/after snapshot for action %s, got before=%q after=%q", entry.Action, entry.BeforeJSON, entry.AfterJSON)
		}
	}
	if !strings.Contains(recorder.entries[0].BeforeJSON, "before-title") || !strings.Contains(recorder.entries[0].AfterJSON, "after-title") {
		t.Fatalf("unexpected update audit snapshots: before=%s after=%s", recorder.entries[0].BeforeJSON, recorder.entries[0].AfterJSON)
	}
	if !strings.Contains(recorder.entries[1].AfterJSON, "\"agent_id\":2") {
		t.Fatalf("unexpected assign audit after snapshot: %s", recorder.entries[1].AfterJSON)
	}
	if !strings.Contains(recorder.entries[2].AfterJSON, "\"status\":\"closed\"") {
		t.Fatalf("unexpected close audit after snapshot: %s", recorder.entries[2].AfterJSON)
	}
}

func toStr(v uint) string {
	// uint->string without fmt to keep the test dependency surface small.
	if v == 0 {
		return "0"
	}
	var b [32]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

func ptrUintTicket(v uint) *uint {
	return &v
}

func TestTicketHandler_GetRelatedConversations(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForTickets(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Seed customer
	if err := db.Create(&models.User{ID: 1, Username: "c1", Name: "c1", Email: "c1@example.com", Role: "customer"}).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	h := NewTicketHandler(ticketdelivery.NewHandlerServiceWithDependencies(ticketdelivery.HandlerAssemblyDependencies{
		DB:     db,
		Logger: logger,
	}), logger)

	r := gin.New()
	r.GET("/api/tickets/:id/conversations", h.GetRelatedConversations)

	// Create a ticket
	createBody := map[string]any{
		"title":       "linked ticket",
		"customer_id": 1,
		"priority":    "normal",
	}
	b, _ := json.Marshal(createBody)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/tickets", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")

	r2 := gin.New()
	r2.POST("/api/tickets", h.CreateTicket)
	r2.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var created models.Ticket
	json.Unmarshal(w.Body.Bytes(), &created)

	// Create sessions linked to ticket
	if err := db.Create(&models.Session{
		ID:       "sess-linked-1",
		TicketID: &created.ID,
		UserID:   1,
		Platform: "web",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := db.Create(&models.Session{
		ID:       "sess-linked-2",
		TicketID: &created.ID,
		UserID:   1,
		Platform: "wechat",
		Status:   "closed",
	}).Error; err != nil {
		t.Fatalf("seed session 2: %v", err)
	}

	// Fetch related conversations
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/api/tickets/"+toStr(created.ID)+"/conversations", nil)
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("get conversations status=%d body=%s", w2.Code, w2.Body.String())
	}

	var resp struct {
		Data []models.Session `json:"data"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 linked sessions, got %d", len(resp.Data))
	}
}

func TestTicketHandler_GetRelatedConversations_NoTicket(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForTickets(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	h := NewTicketHandler(ticketdelivery.NewHandlerServiceWithDependencies(ticketdelivery.HandlerAssemblyDependencies{
		DB:     db,
		Logger: logger,
	}), logger)

	r := gin.New()
	r.GET("/api/tickets/:id/conversations", h.GetRelatedConversations)

	// Non-existent ticket returns empty list (sessions query returns 0)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/tickets/99999/conversations", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []models.Session `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Data) != 0 {
		t.Fatalf("expected 0 sessions for non-existent ticket, got %d", len(resp.Data))
	}
}

func TestTicketHandler_AddComment(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForTickets(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.Create(&models.User{ID: 1, Username: "c1", Name: "c1", Email: "c1@example.com", Role: "customer"}).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	ticket := &models.Ticket{
		Title:      "comment-ticket",
		CustomerID: 1,
		Status:     "open",
		Priority:   "normal",
		Category:   "general",
	}
	if err := db.Create(ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}

	h := NewTicketHandler(ticketdelivery.NewHandlerServiceWithDependencies(ticketdelivery.HandlerAssemblyDependencies{
		DB:     db,
		Logger: logger,
	}), logger)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(99))
		c.Next()
	})
	r.POST("/api/tickets/:id/comments", h.AddComment)

	body, _ := json.Marshal(map[string]any{
		"content": "follow-up note",
		"type":    "internal",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/tickets/"+toStr(ticket.ID)+"/comments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("add comment status=%d body=%s", w.Code, w.Body.String())
	}

	var got models.TicketComment
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal comment: %v body=%s", err, w.Body.String())
	}
	if got.TicketID != ticket.ID {
		t.Fatalf("comment ticket_id=%d want %d", got.TicketID, ticket.ID)
	}
	if got.UserID != 99 {
		t.Fatalf("comment user_id=%d want 99", got.UserID)
	}
	if got.Content != "follow-up note" {
		t.Fatalf("comment content=%q want follow-up note", got.Content)
	}

	var persisted models.TicketComment
	if err := db.First(&persisted, got.ID).Error; err != nil {
		t.Fatalf("load persisted comment: %v", err)
	}
	if persisted.Content != "follow-up note" {
		t.Fatalf("persisted content=%q want follow-up note", persisted.Content)
	}
}

func TestTicketErrorToStatusCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{name: "not found", err: errors.New("ticket not found"), want: http.StatusNotFound},
		{name: "validation", err: errors.New("agent_id is required"), want: http.StatusBadRequest},
		{name: "conflict", err: errors.New("agent not available"), want: http.StatusConflict},
		{name: "unknown", err: errors.New("database exploded"), want: http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ticketErrorToStatusCode(tc.err); got != tc.want {
				t.Fatalf("ticketErrorToStatusCode(%q) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestTicketHandler_ErrorStatusMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewTicketHandler(stubTicketHandlerService{
		updateErr:  errors.New("ticket not found"),
		assignErr:  errors.New("agent not available"),
		commentErr: errors.New("content is required"),
		closeErr:   errors.New("status transition not allowed"),
	}, logrus.New())

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", uint(1))
		c.Next()
	})
	router.PUT("/api/tickets/:id", handler.UpdateTicket)
	router.POST("/api/tickets/:id/assign", handler.AssignTicket)
	router.POST("/api/tickets/:id/comments", handler.AddComment)
	router.POST("/api/tickets/:id/close", handler.CloseTicket)

	updateBody := bytes.NewBufferString(`{"title":"updated"}`)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/tickets/12", updateBody)
	updateReq.Header.Set("Content-Type", "application/json")
	updateResp := httptest.NewRecorder()
	router.ServeHTTP(updateResp, updateReq)
	if updateResp.Code != http.StatusNotFound {
		t.Fatalf("update status=%d body=%s", updateResp.Code, updateResp.Body.String())
	}

	assignReq := httptest.NewRequest(http.MethodPost, "/api/tickets/12/assign", bytes.NewBufferString(`{"agent_id":2}`))
	assignReq.Header.Set("Content-Type", "application/json")
	assignResp := httptest.NewRecorder()
	router.ServeHTTP(assignResp, assignReq)
	if assignResp.Code != http.StatusConflict {
		t.Fatalf("assign status=%d body=%s", assignResp.Code, assignResp.Body.String())
	}

	commentReq := httptest.NewRequest(http.MethodPost, "/api/tickets/12/comments", bytes.NewBufferString(`{"content":"x"}`))
	commentReq.Header.Set("Content-Type", "application/json")
	commentResp := httptest.NewRecorder()
	router.ServeHTTP(commentResp, commentReq)
	if commentResp.Code != http.StatusBadRequest {
		t.Fatalf("comment status=%d body=%s", commentResp.Code, commentResp.Body.String())
	}

	closeReq := httptest.NewRequest(http.MethodPost, "/api/tickets/12/close", bytes.NewBufferString(`{"reason":"done"}`))
	closeReq.Header.Set("Content-Type", "application/json")
	closeResp := httptest.NewRecorder()
	router.ServeHTTP(closeResp, closeReq)
	if closeResp.Code != http.StatusConflict {
		t.Fatalf("close status=%d body=%s", closeResp.Code, closeResp.Body.String())
	}
}
