//go:build integration
// +build integration

package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	appintegrationapp "servify/apps/server/internal/modules/app_integration/application"
	appintegrationdelivery "servify/apps/server/internal/modules/app_integration/delivery"
	appintegrationinfra "servify/apps/server/internal/modules/app_integration/infra"
)

func newAppMarketHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := t.Name()
	dsn := uniqueMemDSN("file:app_market_handler_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.AppIntegration{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func TestAppMarketHandler_ListIntegrations_Empty(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newAppMarketHandlerTestDB(t)
	svc := appintegrationdelivery.NewHandlerServiceAdapter(appintegrationapp.NewService(appintegrationinfra.NewGormRepository(db)))
	handler := NewAppMarketHandler(svc)

	router := gin.New()
	router.GET("/apps/integrations", handler.ListIntegrations)

	req := httptest.NewRequest("GET", "/apps/integrations", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, float64(0), response["total"])
}

func TestAppMarketHandler_CreateIntegration_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newAppMarketHandlerTestDB(t)
	svc := appintegrationdelivery.NewHandlerServiceAdapter(appintegrationapp.NewService(appintegrationinfra.NewGormRepository(db)))
	handler := NewAppMarketHandler(svc)

	router := gin.New()
	router.POST("/apps/integrations", handler.CreateIntegration)

	payload := map[string]interface{}{
		"name":       "slack",
		"category":   "communication",
		"iframe_url": "https://example.com/app",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/apps/integrations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestAppMarketHandler_CreateIntegration_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newAppMarketHandlerTestDB(t)
	svc := appintegrationdelivery.NewHandlerServiceAdapter(appintegrationapp.NewService(appintegrationinfra.NewGormRepository(db)))
	handler := NewAppMarketHandler(svc)

	router := gin.New()
	router.POST("/apps/integrations", handler.CreateIntegration)

	req := httptest.NewRequest("POST", "/apps/integrations", bytes.NewReader([]byte("{invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAppMarketHandler_CreateIntegration_Conflict(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newAppMarketHandlerTestDB(t)
	svc := appintegrationdelivery.NewHandlerServiceAdapter(appintegrationapp.NewService(appintegrationinfra.NewGormRepository(db)))
	handler := NewAppMarketHandler(svc)

	existing := &models.AppIntegration{Name: "slack", Slug: "slack", IFrameURL: "https://example.com/app"}
	if err := db.Create(existing).Error; err != nil {
		t.Fatalf("seed integration: %v", err)
	}

	router := gin.New()
	router.POST("/apps/integrations", handler.CreateIntegration)

	payload := map[string]interface{}{
		"name":       "Slack",
		"slug":       "slack",
		"iframe_url": "https://example.com/another",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/apps/integrations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestAppMarketHandler_UpdateIntegration_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newAppMarketHandlerTestDB(t)
	svc := appintegrationdelivery.NewHandlerServiceAdapter(appintegrationapp.NewService(appintegrationinfra.NewGormRepository(db)))
	handler := NewAppMarketHandler(svc)

	router := gin.New()
	router.PUT("/apps/integrations/:id", handler.UpdateIntegration)

	payload := map[string]string{"name": "Updated"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("PUT", "/apps/integrations/999", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAppMarketHandler_UpdateIntegration_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newAppMarketHandlerTestDB(t)
	svc := appintegrationdelivery.NewHandlerServiceAdapter(appintegrationapp.NewService(appintegrationinfra.NewGormRepository(db)))
	handler := NewAppMarketHandler(svc)

	router := gin.New()
	router.PUT("/apps/integrations/:id", handler.UpdateIntegration)

	payload := map[string]string{"name": "Updated"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("PUT", "/apps/integrations/invalid", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAppMarketHandler_DeleteIntegration_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newAppMarketHandlerTestDB(t)
	svc := appintegrationdelivery.NewHandlerServiceAdapter(appintegrationapp.NewService(appintegrationinfra.NewGormRepository(db)))
	handler := NewAppMarketHandler(svc)

	router := gin.New()
	router.DELETE("/apps/integrations/:id", handler.DeleteIntegration)

	req := httptest.NewRequest("DELETE", "/apps/integrations/999", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAppMarketHandler_DeleteIntegration_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newAppMarketHandlerTestDB(t)
	svc := appintegrationdelivery.NewHandlerServiceAdapter(appintegrationapp.NewService(appintegrationinfra.NewGormRepository(db)))
	handler := NewAppMarketHandler(svc)

	router := gin.New()
	router.DELETE("/apps/integrations/:id", handler.DeleteIntegration)

	req := httptest.NewRequest("DELETE", "/apps/integrations/invalid", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNewAppMarketHandler(t *testing.T) {
	db := newAppMarketHandlerTestDB(t)
	svc := appintegrationdelivery.NewHandlerServiceAdapter(appintegrationapp.NewService(appintegrationinfra.NewGormRepository(db)))

	handler := NewAppMarketHandler(svc)

	assert.NotNil(t, handler)
	assert.Equal(t, svc, handler.service)
}

func TestRegisterAppIntegrationRoutes_NilHandler(t *testing.T) {
	router := gin.New()
	group := router.Group("/api")

	// Should not panic with nil handler
	RegisterAppIntegrationRoutes(group, nil)

	// Should not panic with nil group
	RegisterAppIntegrationRoutes(nil, nil)
}
