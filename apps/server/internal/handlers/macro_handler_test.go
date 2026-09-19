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
	macroapp "servify/apps/server/internal/modules/macro/application"
	macrodelivery "servify/apps/server/internal/modules/macro/delivery"
	macroinfra "servify/apps/server/internal/modules/macro/infra"
)

func newMacroHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := t.Name()
	dsn := uniqueMemDSN("file:macro_handler_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Macro{}, &models.Ticket{}, &models.TicketComment{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func TestMacroHandler_List_Empty(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newMacroHandlerTestDB(t)
	svc := macrodelivery.NewHandlerServiceAdapter(macroapp.NewService(macroinfra.NewGormRepository(db)))
	handler := NewMacroHandler(svc)

	router := gin.New()
	router.GET("/api/macros", handler.List)

	req := httptest.NewRequest("GET", "/api/macros", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response []models.Macro
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Empty(t, response)
}

func TestMacroHandler_List_WithData(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newMacroHandlerTestDB(t)
	svc := macrodelivery.NewHandlerServiceAdapter(macroapp.NewService(macroinfra.NewGormRepository(db)))
	handler := NewMacroHandler(svc)

	// Create test macro
	macro := &models.Macro{
		Name:        "Test Macro",
		Description: "Test Description",
		Content:     "Test Content",
		Language:    "zh",
	}
	db.Create(macro)

	router := gin.New()
	router.GET("/api/macros", handler.List)

	req := httptest.NewRequest("GET", "/api/macros", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response []models.Macro
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Len(t, response, 1)
}

func TestMacroHandler_Create_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newMacroHandlerTestDB(t)
	svc := macrodelivery.NewHandlerServiceAdapter(macroapp.NewService(macroinfra.NewGormRepository(db)))
	handler := NewMacroHandler(svc)

	router := gin.New()
	router.POST("/api/macros", handler.Create)

	payload := map[string]interface{}{
		"name":        "New Macro",
		"description": "Test Description",
		"content":     "Test Content",
		"language":    "zh",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/macros", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestMacroHandler_Create_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newMacroHandlerTestDB(t)
	svc := macrodelivery.NewHandlerServiceAdapter(macroapp.NewService(macroinfra.NewGormRepository(db)))
	handler := NewMacroHandler(svc)

	router := gin.New()
	router.POST("/api/macros", handler.Create)

	req := httptest.NewRequest("POST", "/api/macros", bytes.NewReader([]byte("{invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMacroHandler_Update_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newMacroHandlerTestDB(t)
	svc := macrodelivery.NewHandlerServiceAdapter(macroapp.NewService(macroinfra.NewGormRepository(db)))
	handler := NewMacroHandler(svc)

	// Create test macro
	macro := &models.Macro{
		Name:        "Test Macro",
		Description: "Test Description",
		Content:     "Test Content",
		Language:    "zh",
	}
	db.Create(macro)

	router := gin.New()
	router.PUT("/api/macros/:id", handler.Update)

	payload := map[string]interface{}{
		"description": "Updated Description",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("PUT", "/api/macros/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMacroHandler_Update_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newMacroHandlerTestDB(t)
	svc := macrodelivery.NewHandlerServiceAdapter(macroapp.NewService(macroinfra.NewGormRepository(db)))
	handler := NewMacroHandler(svc)

	router := gin.New()
	router.PUT("/api/macros/:id", handler.Update)

	req := httptest.NewRequest("PUT", "/api/macros/invalid", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMacroHandler_Update_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newMacroHandlerTestDB(t)
	svc := macrodelivery.NewHandlerServiceAdapter(macroapp.NewService(macroinfra.NewGormRepository(db)))
	handler := NewMacroHandler(svc)

	router := gin.New()
	router.PUT("/api/macros/:id", handler.Update)

	body := bytes.NewReader([]byte(`{"description":"Updated"}`))
	req := httptest.NewRequest("PUT", "/api/macros/999", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestMacroHandler_Delete_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newMacroHandlerTestDB(t)
	svc := macrodelivery.NewHandlerServiceAdapter(macroapp.NewService(macroinfra.NewGormRepository(db)))
	handler := NewMacroHandler(svc)

	// Create test macro
	macro := &models.Macro{
		Name:        "Test Macro",
		Description: "Test Description",
		Content:     "Test Content",
		Language:    "zh",
	}
	db.Create(macro)

	router := gin.New()
	router.DELETE("/api/macros/:id", handler.Delete)

	req := httptest.NewRequest("DELETE", "/api/macros/1", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "deleted", response["message"])
}

func TestMacroHandler_Delete_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newMacroHandlerTestDB(t)
	svc := macrodelivery.NewHandlerServiceAdapter(macroapp.NewService(macroinfra.NewGormRepository(db)))
	handler := NewMacroHandler(svc)

	router := gin.New()
	router.DELETE("/api/macros/:id", handler.Delete)

	req := httptest.NewRequest("DELETE", "/api/macros/999", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestMacroHandler_Apply_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newMacroHandlerTestDB(t)
	svc := macrodelivery.NewHandlerServiceAdapter(macroapp.NewService(macroinfra.NewGormRepository(db)))
	handler := NewMacroHandler(svc)

	// Create test macro and ticket
	macro := &models.Macro{
		Name:        "Test Macro",
		Description: "Test Description",
		Content:     "Test Content",
		Language:    "zh",
	}
	db.Create(macro)

	ticket := &models.Ticket{
		Title:       "Test Ticket",
		Description: "Test",
		Status:      "open",
		CustomerID:  1,
	}
	db.Create(ticket)

	router := gin.New()
	router.POST("/api/macros/:id/apply", handler.Apply)

	payload := map[string]interface{}{
		"ticket_id": 1,
		"user_id":   1,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/api/macros/1/apply", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMacroHandler_Apply_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newMacroHandlerTestDB(t)
	svc := macrodelivery.NewHandlerServiceAdapter(macroapp.NewService(macroinfra.NewGormRepository(db)))
	handler := NewMacroHandler(svc)

	router := gin.New()
	router.POST("/api/macros/:id/apply", handler.Apply)

	req := httptest.NewRequest("POST", "/api/macros/invalid/apply", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMacroHandler_Apply_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newMacroHandlerTestDB(t)
	svc := macrodelivery.NewHandlerServiceAdapter(macroapp.NewService(macroinfra.NewGormRepository(db)))
	handler := NewMacroHandler(svc)

	router := gin.New()
	router.POST("/api/macros/:id/apply", handler.Apply)

	body := bytes.NewReader([]byte(`{"ticket_id":1,"user_id":1}`))
	req := httptest.NewRequest("POST", "/api/macros/999/apply", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestNewMacroHandler(t *testing.T) {
	db := newMacroHandlerTestDB(t)
	svc := macrodelivery.NewHandlerServiceAdapter(macroapp.NewService(macroinfra.NewGormRepository(db)))

	handler := NewMacroHandler(svc)

	assert.NotNil(t, handler)
	assert.Equal(t, svc, handler.service)
}
