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

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/services"
)

func newAgentHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := uniqueMemDSN("file:agent_handler_extended_" + name + "")
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
		&models.Session{},
		&models.Ticket{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	return db
}

func TestAgentHandler_AssignSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newAgentHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// 创建用户和客服
	user := &models.User{
		ID:       10,
		Username: "agent10",
		Email:    "agent10@example.com",
		Name:     "Agent Ten",
		Role:     "agent",
		Status:   "active",
	}
	db.Create(user)

	agent := &models.Agent{
		UserID:          10,
		Department:      "support",
		Status:          "online",
		MaxConcurrent:   5,
		CurrentLoad:     0,
		AvgResponseTime: 300,
	}
	db.Create(agent)

	svc := services.NewAgentService(db, logger)
	h := NewAgentHandler(svc, logger)

	// 让 agent 上线，使其进入 onlineAgents 缓存
	if err := svc.AgentGoOnline(context.Background(), 10); err != nil {
		t.Fatalf("failed to set agent online: %v", err)
	}

	// 创建测试会话
	session := &models.Session{
		ID:     "session123",
		Status: "waiting",
		UserID: 1,
	}
	db.Create(session)

	r := gin.New()
	r.POST("/api/agents/:id/assign-session", h.AssignSession)

	tests := []struct {
		name       string
		agentID    string
		body       map[string]string
		wantStatus int
	}{
		{
			name:    "valid session assignment",
			agentID: "10",
			body: map[string]string{
				"session_id": "session123",
			},
			wantStatus: http.StatusOK,
		},
		{
			name:    "invalid agent id",
			agentID: "invalid",
			body: map[string]string{
				"session_id": "session123",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "missing session_id",
			agentID: "10",
			body: map[string]string{
				"other_field": "value",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "non-existent agent",
			agentID: "9999",
			body: map[string]string{
				"session_id": "session123",
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:    "non-existent session",
			agentID: "10",
			body: map[string]string{
				"session_id": "missing-session",
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/agents/"+tt.agentID+"/assign-session", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("AssignSession() status = %d, want %d, body=%s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestAgentHandler_ReleaseSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newAgentHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// 创建用户和客服
	user := &models.User{
		ID:       11,
		Username: "agent11",
		Email:    "agent11@example.com",
		Name:     "Agent Eleven",
		Role:     "agent",
		Status:   "active",
	}
	db.Create(user)

	agent := &models.Agent{
		UserID:          11,
		Department:      "support",
		Status:          "busy",
		MaxConcurrent:   5,
		CurrentLoad:     1,
		AvgResponseTime: 300,
	}
	db.Create(agent)

	svc := services.NewAgentService(db, logger)
	h := NewAgentHandler(svc, logger)

	session := &models.Session{
		ID:      "session456",
		Status:  "active",
		UserID:  1,
		AgentID: uintPtr(11),
	}
	db.Create(session)

	r := gin.New()
	r.POST("/api/agents/:id/release-session", h.ReleaseSession)

	tests := []struct {
		name       string
		agentID    string
		body       map[string]string
		wantStatus int
	}{
		{
			name:    "valid session release",
			agentID: "11",
			body: map[string]string{
				"session_id": "session456",
			},
			wantStatus: http.StatusOK,
		},
		{
			name:    "invalid agent id",
			agentID: "invalid",
			body: map[string]string{
				"session_id": "session456",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "missing session_id",
			agentID: "11",
			body: map[string]string{
				"other_field": "value",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "session not assigned to agent",
			agentID: "11",
			body: map[string]string{
				"session_id": "missing-session",
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/agents/"+tt.agentID+"/release-session", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("ReleaseSession() status = %d, want %d, body=%s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestAgentHandler_GetAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newAgentHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// 创建测试数据
	user := &models.User{
		ID:       20,
		Username: "agent20",
		Email:    "agent20@example.com",
		Name:     "Agent Twenty",
		Role:     "agent",
		Status:   "active",
	}
	db.Create(user)

	agent := &models.Agent{
		UserID:          20,
		Department:      "support",
		Status:          "online",
		MaxConcurrent:   5,
		CurrentLoad:     2,
		AvgResponseTime: 250,
	}
	db.Create(agent)

	svc := services.NewAgentService(db, logger)
	h := NewAgentHandler(svc, logger)

	r := gin.New()
	r.GET("/api/agents/:id", h.GetAgent)

	tests := []struct {
		name       string
		agentID    string
		wantStatus int
	}{
		{
			name:       "existing agent",
			agentID:    "20",
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid agent id",
			agentID:    "invalid",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "non-existent agent",
			agentID:    "9999",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/api/agents/"+tt.agentID, nil)
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("GetAgent() status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				var resp map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to parse response: %v", err)
				}

				if resp["user_id"] != float64(20) {
					t.Errorf("expected user_id 20, got %v", resp["user_id"])
				}
			}
		})
	}
}

func TestAgentHandler_GetAgentStats(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newAgentHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// 创建客服数据
	user := &models.User{
		ID:       30,
		Username: "agent30",
		Email:    "agent30@example.com",
		Name:     "Agent Thirty",
		Role:     "agent",
		Status:   "active",
	}
	db.Create(user)

	agent := &models.Agent{
		UserID:          30,
		Department:      "support",
		Status:          "online",
		MaxConcurrent:   5,
		CurrentLoad:     3,
		AvgResponseTime: 200,
	}
	db.Create(agent)

	svc := services.NewAgentService(db, logger)
	h := NewAgentHandler(svc, logger)

	r := gin.New()
	r.GET("/api/agents/stats", h.GetAgentStats)

	tests := []struct {
		name       string
		url        string
		wantStatus int
	}{
		{
			name:       "existing agent stats",
			url:        "/api/agents/stats?agent_id=30",
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid agent id",
			url:        "/api/agents/stats?agent_id=invalid",
			wantStatus: http.StatusOK, // 无效ID会被忽略，返回所有统计
		},
		{
			name:       "all agents stats (no filter)",
			url:        "/api/agents/stats",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", tt.url, nil)
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("GetAgentStats() status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				var resp map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to parse response: %v", err)
				}

				// GetAgentStats 返回直接的统计数据，不是嵌套在 data 中
				if len(resp) == 0 {
					t.Error("expected stats in response")
				}
			}
		})
	}
}

func TestAgentHandler_FindAvailableAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newAgentHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// 创建在线客服
	for i := 1; i <= 3; i++ {
		agentNum := []string{"1", "2", "3"}[i-1]
		user := &models.User{
			ID:       uint(i),
			Username: "agent" + agentNum,
			Email:    "agent" + agentNum + "@example.com",
			Name:     "Agent " + agentNum,
			Role:     "agent",
			Status:   "active",
		}
		db.Create(user)

		agent := &models.Agent{
			UserID:          uint(i),
			Department:      "support",
			Status:          "online",
			MaxConcurrent:   5,
			CurrentLoad:     i % 3,
			AvgResponseTime: 100 * i,
		}
		db.Create(agent)
	}

	svc := services.NewAgentService(db, logger)
	h := NewAgentHandler(svc, logger)

	// 让所有 agents 上线
	for i := 1; i <= 3; i++ {
		if err := svc.AgentGoOnline(context.Background(), uint(i)); err != nil {
			t.Fatalf("failed to set agent %d online: %v", i, err)
		}
	}

	r := gin.New()
	r.GET("/api/agents/available", h.FindAvailableAgent)

	tests := []struct {
		name       string
		department string
		wantStatus int
	}{
		{
			name:       "find available agent",
			department: "support",
			wantStatus: http.StatusOK,
		},
		{
			name:       "find without department filter",
			department: "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "find in non-existent department",
			department: "engineering",
			wantStatus: http.StatusOK, // 由于有其他可用的 agent，会返回它们
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			url := "/api/agents/available"
			if tt.department != "" {
				url += "?department=" + tt.department
			}
			req, _ := http.NewRequest("GET", url, nil)
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("FindAvailableAgent() status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				var resp map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to parse response: %v", err)
				}

				// FindAvailableAgent 直接返回 agent 对象，不是嵌套在 data 中
				if resp["user_id"] == nil {
					t.Error("expected user_id in response")
				}
			}
		})
	}
}

func TestAgentHandler_ListAgents_Extended(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newAgentHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// 创建多个客服
	for i := 1; i <= 5; i++ {
		agentNum := []string{"1", "2", "3", "4", "5"}[i-1]
		user := &models.User{
			ID:       uint(i),
			Username: "listagent" + agentNum,
			Email:    "listagent" + agentNum + "@example.com",
			Name:     "List Agent " + agentNum,
			Role:     "agent",
			Status:   "active",
		}
		db.Create(user)

		dept := "support"
		if i%2 != 0 {
			dept = "sales"
		}
		status := "online"
		if i%3 == 0 {
			status = "offline"
		}
		agent := &models.Agent{
			UserID:          uint(i),
			Department:      dept,
			Status:          status,
			MaxConcurrent:   5,
			CurrentLoad:     i,
			AvgResponseTime: 100 * i,
		}
		db.Create(agent)
	}

	svc := services.NewAgentService(db, logger)
	h := NewAgentHandler(svc, logger)

	r := gin.New()
	r.GET("/api/agents", h.ListAgents)

	tests := []struct {
		name       string
		query      string
		wantStatus int
		minAgents  int
	}{
		{
			name:       "list all agents",
			query:      "",
			wantStatus: http.StatusOK,
			minAgents:  5,
		},
		{
			name:       "filter by department",
			query:      "?department=support",
			wantStatus: http.StatusOK,
			minAgents:  2,
		},
		{
			name:       "filter by status",
			query:      "?status=online",
			wantStatus: http.StatusOK,
			minAgents:  3,
		},
		{
			name:       "pagination",
			query:      "?page=1&page_size=3",
			wantStatus: http.StatusOK,
			minAgents:  3,
		},
		{
			name:       "search",
			query:      "?search=Agent",
			wantStatus: http.StatusOK,
			minAgents:  5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/api/agents"+tt.query, nil)
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("ListAgents() status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				var resp map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to parse response: %v", err)
				}

				// ListAgents 返回 {"data": [...]}，其中 data 直接是 agents 数组
				agents, ok := resp["data"].([]interface{})
				if !ok {
					t.Fatalf("expected data to be array, got %T: %+v", resp["data"], resp)
				}

				if len(agents) < tt.minAgents {
					t.Errorf("expected at least %d agents, got %d", tt.minAgents, len(agents))
				}
			}
		})
	}
}
