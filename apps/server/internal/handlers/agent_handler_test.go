//go:build integration
// +build integration

package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	agentapp "servify/apps/server/internal/modules/agent/application"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	agentinfra "servify/apps/server/internal/modules/agent/infra"
	auditplatform "servify/apps/server/internal/platform/audit"
)

// newTestAgentService 构造经 module delivery adapter 的测试用 agent 服务
// （in-memory registry，装配语义与 agent module 单实例一致）。
func newTestAgentService(db *gorm.DB, logger *logrus.Logger) *agentdelivery.HandlerServiceAdapter {
	return agentdelivery.NewHandlerServiceAdapter(
		agentapp.NewService(agentinfra.NewGormRepository(db), agentinfra.NewInMemoryRegistry()),
		logger,
	)
}

func newTestDBForAgents(t *testing.T) *gorm.DB {
	t.Helper()

	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := uniqueMemDSN("file:agent_handler_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.User{},
		&models.Agent{},
		&models.Ticket{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	return db
}

func TestAgentHandler_Create_Online_Status_Offline(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForAgents(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Seed a base user to be promoted to agent.
	if err := db.Create(&models.User{
		ID:       10,
		Username: "agent10",
		Email:    "agent10@example.com",
		Name:     "agent10",
		Role:     "customer",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	svc := newTestAgentService(db, logger)
	h := NewAgentHandler(svc, logger)

	r := gin.New()
	r.POST("/api/agents", h.CreateAgent)
	r.GET("/api/agents", h.ListAgents)
	r.POST("/api/agents/:id/online", h.AgentGoOnline)
	r.PUT("/api/agents/:id/status", h.UpdateAgentStatus)
	r.GET("/api/agents/online", h.GetOnlineAgents)
	r.POST("/api/agents/:id/offline", h.AgentGoOffline)
	r.POST("/api/agents/:id/revoke-tokens", h.RevokeAgentTokens)

	// Create agent for user 10
	createBody := map[string]any{"user_id": 10, "department": "support", "skills": "billing,tech", "max_concurrent": 3}
	b, _ := json.Marshal(createBody)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/agents", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}

	// List agents (should include newly created agent)
	wList := httptest.NewRecorder()
	reqList, _ := http.NewRequest(http.MethodGet, "/api/agents", nil)
	r.ServeHTTP(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", wList.Code, wList.Body.String())
	}

	// Go online
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodPost, "/api/agents/10/online", nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("online status=%d body=%s", w2.Code, w2.Body.String())
	}

	// Update status
	updateBody := map[string]any{"status": "busy"}
	bu, _ := json.Marshal(updateBody)
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodPut, "/api/agents/10/status", bytes.NewReader(bu))
	req3.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("status update status=%d body=%s", w3.Code, w3.Body.String())
	}

	// Online list
	w4 := httptest.NewRecorder()
	req4, _ := http.NewRequest(http.MethodGet, "/api/agents/online", nil)
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("online list status=%d body=%s", w4.Code, w4.Body.String())
	}

	// Go offline
	w5 := httptest.NewRecorder()
	req5, _ := http.NewRequest(http.MethodPost, "/api/agents/10/offline", nil)
	r.ServeHTTP(w5, req5)
	if w5.Code != http.StatusOK {
		t.Fatalf("offline status=%d body=%s", w5.Code, w5.Body.String())
	}

	// Revoke tokens
	w6 := httptest.NewRecorder()
	req6, _ := http.NewRequest(http.MethodPost, "/api/agents/10/revoke-tokens", nil)
	r.ServeHTTP(w6, req6)
	if w6.Code != http.StatusOK {
		t.Fatalf("revoke tokens status=%d body=%s", w6.Code, w6.Body.String())
	}
}

func TestAgentHandler_AuditSnapshotsForStatusChanges(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForAgents(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	if err := db.Create(&models.User{
		ID:       20,
		Username: "agent20",
		Email:    "agent20@example.com",
		Name:     "agent20",
		Role:     "agent",
		Status:   "active",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.Agent{
		UserID:        20,
		Status:        "offline",
		MaxConcurrent: 3,
	}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	svc := newTestAgentService(db, logger)
	h := NewAgentHandler(svc, logger)
	recorder := &ticketAuditRecorder{}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(99))
		c.Set("principal_kind", "admin")
		c.Next()
	})
	r.Use(auditplatform.Middleware(recorder))
	r.POST("/api/agents/:id/online", h.AgentGoOnline)
	r.PUT("/api/agents/:id/status", h.UpdateAgentStatus)
	r.POST("/api/agents/:id/offline", h.AgentGoOffline)

	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodPost, "/api/agents/20/online", nil)
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("online status=%d body=%s", w1.Code, w1.Body.String())
	}

	updateBody := map[string]any{"status": "busy"}
	bu, _ := json.Marshal(updateBody)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodPut, "/api/agents/20/status", bytes.NewReader(bu))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("status update=%d body=%s", w2.Code, w2.Body.String())
	}

	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodPost, "/api/agents/20/offline", nil)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("offline status=%d body=%s", w3.Code, w3.Body.String())
	}

	if len(recorder.entries) != 3 {
		t.Fatalf("expected 3 audit entries got %d", len(recorder.entries))
	}
	for _, entry := range recorder.entries {
		if entry.BeforeJSON == "" || entry.AfterJSON == "" {
			t.Fatalf("expected before/after snapshot for %s, got before=%q after=%q", entry.Action, entry.BeforeJSON, entry.AfterJSON)
		}
	}
	if !strings.Contains(recorder.entries[0].BeforeJSON, `"status":"offline"`) || !strings.Contains(recorder.entries[0].AfterJSON, `"status":"online"`) {
		t.Fatalf("unexpected online snapshots: before=%s after=%s", recorder.entries[0].BeforeJSON, recorder.entries[0].AfterJSON)
	}
	if !strings.Contains(recorder.entries[1].AfterJSON, `"status":"busy"`) {
		t.Fatalf("unexpected update snapshot: %s", recorder.entries[1].AfterJSON)
	}
	if !strings.Contains(recorder.entries[2].AfterJSON, `"status":"offline"`) {
		t.Fatalf("unexpected offline snapshot: %s", recorder.entries[2].AfterJSON)
	}
}

func TestAgentHandler_CreateAgent_NotFoundAndNilLoggerSafe(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForAgents(t)
	svc := newTestAgentService(db, nil)
	h := NewAgentHandler(svc, nil)

	r := gin.New()
	r.POST("/api/agents", h.CreateAgent)

	body, _ := json.Marshal(map[string]any{"user_id": 9999})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/agents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
