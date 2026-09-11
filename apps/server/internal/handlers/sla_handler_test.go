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
	"servify/apps/server/internal/services"
)

func newTestDBForSLA(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:sla_handler")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(&models.SLAConfig{}, &models.SLAViolation{}, &models.Ticket{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestSLAHandler_Create_And_List(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForSLA(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	svc := services.NewSLAService(db, logger)
	h := NewSLAHandler(svc, nil)

	r := gin.New()
	r.POST("/api/sla/configs", h.CreateSLAConfig)
	r.GET("/api/sla/configs/:id", h.GetSLAConfig)
	r.GET("/api/sla/configs", h.ListSLAConfigs)
	r.PUT("/api/sla/configs/:id", h.UpdateSLAConfig)
	r.DELETE("/api/sla/configs/:id", h.DeleteSLAConfig)

	// Create config
	body := map[string]any{
		"name":                "default-normal",
		"priority":            "normal",
		"first_response_time": 30,
		"resolution_time":     240,
		"escalation_time":     60,
	}
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/sla/configs", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var created models.SLAConfig
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create: %v body=%s", err, w.Body.String())
	}
	if created.ID == 0 {
		t.Fatalf("expected created config id")
	}

	// List configs
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/api/sla/configs?page=1&page_size=10", nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", w2.Code, w2.Body.String())
	}

	// Get config
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodGet, "/api/sla/configs/"+toStr(created.ID), nil)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", w3.Code, w3.Body.String())
	}

	// Update config (name only)
	updateBody := map[string]any{"name": "default-normal-updated"}
	bu, _ := json.Marshal(updateBody)
	w4 := httptest.NewRecorder()
	req4, _ := http.NewRequest(http.MethodPut, "/api/sla/configs/"+toStr(created.ID), bytes.NewReader(bu))
	req4.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", w4.Code, w4.Body.String())
	}

	// Delete config
	w5 := httptest.NewRecorder()
	req5, _ := http.NewRequest(http.MethodDelete, "/api/sla/configs/"+toStr(created.ID), nil)
	r.ServeHTTP(w5, req5)
	if w5.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", w5.Code, w5.Body.String())
	}
}

func TestSLAHandler_GetSLAConfigByPriority_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForSLA(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	svc := services.NewSLAService(db, logger)
	h := NewSLAHandler(svc, nil)

	r := gin.New()
	r.GET("/api/sla/configs/priority/:priority", h.GetSLAConfigByPriority)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/sla/configs/priority/normal", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestSLAHandler_GetSLAConfigByPriority_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForSLA(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	svc := services.NewSLAService(db, logger)
	h := NewSLAHandler(svc, nil)

	r := gin.New()
	r.GET("/api/sla/configs/priority/:priority", h.GetSLAConfigByPriority)

	// Create a config first
	config := &models.SLAConfig{
		Name:              "test-normal",
		Priority:          "normal",
		FirstResponseTime: 30,
		ResolutionTime:    240,
		EscalationTime:    60,
	}
	db.Create(config)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/sla/configs/priority/normal", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestSLAHandler_ListSLAViolations_Empty(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForSLA(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	svc := services.NewSLAService(db, logger)
	h := NewSLAHandler(svc, nil)

	r := gin.New()
	r.GET("/api/sla/violations", h.ListSLAViolations)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/sla/violations?page=1&page_size=10", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestSLAHandler_ResolveSLAViolation_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForSLA(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	svc := services.NewSLAService(db, logger)
	h := NewSLAHandler(svc, nil)

	r := gin.New()
	r.POST("/api/sla/violations/:id/resolve", h.ResolveSLAViolation)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/sla/violations/999/resolve", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestSLAHandler_ResolveSLAViolation_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForSLA(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	svc := services.NewSLAService(db, logger)
	h := NewSLAHandler(svc, nil)

	r := gin.New()
	r.POST("/api/sla/violations/:id/resolve", h.ResolveSLAViolation)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/sla/violations/invalid/resolve", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestSLAHandler_GetSLAStats(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForSLA(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	svc := services.NewSLAService(db, logger)
	h := NewSLAHandler(svc, nil)

	r := gin.New()
	r.GET("/api/sla/stats", h.GetSLAStats)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/sla/stats", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestSLAHandler_CreateSLAConfig_InvalidTimeLogic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForSLA(t)
	svc := services.NewSLAService(db, logrus.New())
	h := NewSLAHandler(svc, nil)

	r := gin.New()
	r.POST("/api/sla/configs", h.CreateSLAConfig)

	body := map[string]any{
		"name":                "bad-config",
		"priority":            "normal",
		"first_response_time": 60,
		"resolution_time":     30,
		"escalation_time":     20,
	}
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/sla/configs", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "SLA配置参数无效") {
		t.Fatalf("expected invalid config message, got %s", w.Body.String())
	}
}

func TestSLAHandler_UpdateSLAConfig_Conflict(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForSLA(t)
	svc := services.NewSLAService(db, logrus.New())
	h := NewSLAHandler(svc, nil)

	now := models.SLAConfig{
		Name:              "normal-default",
		Priority:          "normal",
		FirstResponseTime: 30,
		ResolutionTime:    240,
		EscalationTime:    60,
	}
	other := models.SLAConfig{
		Name:              "high-default",
		Priority:          "high",
		FirstResponseTime: 15,
		ResolutionTime:    120,
		EscalationTime:    30,
	}
	if err := db.Create(&now).Error; err != nil {
		t.Fatalf("create config 1: %v", err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatalf("create config 2: %v", err)
	}

	r := gin.New()
	r.PUT("/api/sla/configs/:id", h.UpdateSLAConfig)

	body := map[string]any{
		"priority": "high",
	}
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/sla/configs/"+toStr(now.ID), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
}
