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
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	routingapp "servify/apps/server/internal/modules/routing/application"
	routingdelivery "servify/apps/server/internal/modules/routing/delivery"
	routinginfra "servify/apps/server/internal/modules/routing/infra"
	ticketdelivery "servify/apps/server/internal/modules/ticket/delivery"
	"servify/apps/server/internal/platform/eventbus"
	"servify/apps/server/internal/services"
)

type stubAIForTransferHandler struct{}

func (s stubAIForTransferHandler) ProcessQuery(ctx context.Context, query string, sessionID string) (*services.AIResponse, error) {
	return &services.AIResponse{Content: "ok", Confidence: 1, Source: "ai"}, nil
}
func (s stubAIForTransferHandler) ShouldTransferToHuman(query string, sessionHistory []models.Message) bool {
	return false
}
func (s stubAIForTransferHandler) GetSessionSummary(messages []models.Message) (string, error) {
	return "summary", nil
}
func (s stubAIForTransferHandler) InitializeKnowledgeBase() {}
func (s stubAIForTransferHandler) GetStatus(ctx context.Context) map[string]interface{} {
	return map[string]interface{}{"type": "stub"}
}

func newTestDBForSessionTransferHandler(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := uniqueMemDSN("file:session_transfer_handler_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&models.User{},
		&models.Agent{},
		&models.Session{},
		&models.Message{},
		&models.TransferRecord{},
		&models.WaitingRecord{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func newRoutingTransferHandlerService(db *gorm.DB, logger *logrus.Logger, ai stubAIForTransferHandler, agentSvc *agentdelivery.HandlerServiceAdapter) routingdelivery.HandlerService {
	bus := eventbus.NewInMemoryBus()
	routingSvc := routingapp.NewService(routinginfra.NewGormRepository(db), bus)
	return routingdelivery.NewHandlerService(routingdelivery.HandlerDependencies{
		DB:           db,
		Logger:       logger,
		AI:           ai,
		Agents:       agentSvc,
		Routing:      routingdelivery.NewSessionTransferAdapter(routingSvc, bus),
		Tickets:      ticketdelivery.NewRuntimeAdapter(bus),
		Conversation: conversationdelivery.NewRuntimeAdapter(db, bus),
		AgentLoad:    agentdelivery.NewTransferRuntimeAdapter(),
	})
}

func TestSessionTransferHandler_ListWaiting_And_Cancel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForSessionTransferHandler(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.Create(&models.User{ID: 1, Username: "u1", Name: "u1", Email: "u1@example.com", Role: "customer"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	now := time.Now()
	if err := db.Create(&models.Session{ID: "s1", UserID: 1, Status: "active", Platform: "web", StartedAt: now, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := db.Create(&models.WaitingRecord{SessionID: "s1", Reason: "r", Priority: "normal", Status: "waiting", QueuedAt: now, CreatedAt: now}).Error; err != nil {
		t.Fatalf("seed waiting: %v", err)
	}

	agentSvc := newTestAgentService(db, logger)
	transferSvc := newRoutingTransferHandlerService(db, logger, stubAIForTransferHandler{}, agentSvc)
	h := NewSessionTransferHandler(transferSvc, logger)

	r := gin.New()
	api := r.Group("/api")
	RegisterSessionTransferRoutes(api, h)

	// list waiting
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/session-transfer/waiting?status=waiting&limit=10", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list waiting status=%d body=%s", w.Code, w.Body.String())
	}
	var listResp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list: %v body=%s", err, w.Body.String())
	}
	if int(listResp["count"].(float64)) != 1 {
		t.Fatalf("expected count=1 got %v", listResp["count"])
	}

	// cancel waiting
	w2 := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"session_id": "s1", "reason": "no_need"})
	req2, _ := http.NewRequest(http.MethodPost, "/api/session-transfer/cancel", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", w2.Code, w2.Body.String())
	}

	var wr models.WaitingRecord
	if err := db.Where("session_id = ?", "s1").First(&wr).Error; err != nil {
		t.Fatalf("load waiting record: %v", err)
	}
	if wr.Status != "cancelled" {
		t.Fatalf("expected cancelled got %s", wr.Status)
	}
}

func TestSessionTransferHandler_TransferToHuman(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForSessionTransferHandler(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.Create(&models.User{ID: 1, Username: "u1", Name: "u1", Email: "u1@example.com", Role: "customer"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	now := time.Now()
	if err := db.Create(&models.Session{ID: "s1", UserID: 1, Status: "active", Platform: "web", StartedAt: now, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	agentSvc := newTestAgentService(db, logger)
	transferSvc := newRoutingTransferHandlerService(db, logger, stubAIForTransferHandler{}, agentSvc)
	h := NewSessionTransferHandler(transferSvc, logger)

	r := gin.New()
	r.POST("/session-transfer/to-human", h.TransferToHuman)

	payload := map[string]string{
		"session_id": "s1",
		"reason":     "customer_request",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/session-transfer/to-human", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestSessionTransferHandler_GetTransferHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForSessionTransferHandler(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.Create(&models.User{ID: 1, Username: "u1", Name: "u1", Email: "u1@example.com", Role: "customer"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	now := time.Now()
	if err := db.Create(&models.Session{ID: "s1", UserID: 1, Status: "active", Platform: "web", StartedAt: now, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	agentSvc := newTestAgentService(db, logger)
	transferSvc := newRoutingTransferHandlerService(db, logger, stubAIForTransferHandler{}, agentSvc)
	h := NewSessionTransferHandler(transferSvc, logger)

	r := gin.New()
	r.GET("/session-transfer/history/:session_id", h.GetTransferHistory)

	req := httptest.NewRequest("GET", "/session-transfer/history/s1", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
}

func TestSessionTransferHandler_ListRecentTransferHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForSessionTransferHandler(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.Create(&models.User{ID: 1, Username: "u1", Name: "u1", Email: "u1@example.com", Role: "customer"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	now := time.Now()
	if err := db.Create(&models.Session{ID: "s1", UserID: 1, Status: "active", Platform: "web", StartedAt: now, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := db.Create(&models.TransferRecord{
		SessionID:     "s1",
		Reason:        "manual_transfer",
		TransferredAt: now,
		CreatedAt:     now,
	}).Error; err != nil {
		t.Fatalf("seed transfer record: %v", err)
	}

	agentSvc := newTestAgentService(db, logger)
	transferSvc := newRoutingTransferHandlerService(db, logger, stubAIForTransferHandler{}, agentSvc)
	h := NewSessionTransferHandler(transferSvc, logger)

	r := gin.New()
	r.GET("/session-transfer/history", h.ListRecentTransferHistory)

	req := httptest.NewRequest("GET", "/session-transfer/history?limit=10", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Count int                     `json:"count"`
		Data  []models.TransferRecord `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v body=%s", err, w.Body.String())
	}
	if resp.Count < 1 || len(resp.Data) < 1 {
		t.Fatalf("expected at least one record, got %+v", resp)
	}
	if resp.Data[0].SessionID != "s1" {
		t.Fatalf("unexpected session id: %+v", resp.Data[0])
	}
}

func TestSessionTransferHandler_ProcessWaitingQueue(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForSessionTransferHandler(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.Create(&models.User{ID: 1, Username: "u1", Name: "u1", Email: "u1@example.com", Role: "customer"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.User{ID: 2, Username: "agent1", Name: "Agent", Email: "agent@example.com", Role: "agent"}).Error; err != nil {
		t.Fatalf("seed agent user: %v", err)
	}
	if err := db.Create(&models.Agent{ID: 1, UserID: 2, Status: "online", MaxConcurrent: 5, CurrentLoad: 0}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	now := time.Now()
	if err := db.Create(&models.Session{ID: "s1", UserID: 1, Status: "active", Platform: "web", StartedAt: now, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := db.Create(&models.WaitingRecord{SessionID: "s1", Reason: "test", Priority: "normal", Status: "waiting", QueuedAt: now, CreatedAt: now}).Error; err != nil {
		t.Fatalf("seed waiting: %v", err)
	}

	agentSvc := newTestAgentService(db, logger)
	transferSvc := newRoutingTransferHandlerService(db, logger, stubAIForTransferHandler{}, agentSvc)
	h := NewSessionTransferHandler(transferSvc, logger)

	r := gin.New()
	r.POST("/session-transfer/process-queue", h.ProcessWaitingQueue)

	req := httptest.NewRequest("POST", "/session-transfer/process-queue", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestSessionTransferHandler_CheckAutoTransfer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForSessionTransferHandler(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.Create(&models.User{ID: 1, Username: "u1", Name: "u1", Email: "u1@example.com", Role: "customer"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	now := time.Now()
	if err := db.Create(&models.Session{ID: "s1", UserID: 1, Status: "active", Platform: "web", StartedAt: now, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	agentSvc := newTestAgentService(db, logger)
	transferSvc := newRoutingTransferHandlerService(db, logger, stubAIForTransferHandler{}, agentSvc)
	h := NewSessionTransferHandler(transferSvc, logger)

	r := gin.New()
	r.POST("/session-transfer/check-auto-transfer", h.CheckAutoTransfer)

	payload := map[string]interface{}{
		"session_id": "s1",
		"messages": []map[string]string{
			{"content": "I need help", "sender": "customer"},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/session-transfer/check-auto-transfer", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
}
