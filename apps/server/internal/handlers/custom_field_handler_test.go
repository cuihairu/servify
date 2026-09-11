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
	"servify/apps/server/internal/services"
)

func newCustomFieldHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := t.Name()
	dsn := uniqueMemDSN("file:custom_field_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.CustomField{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func TestCustomFieldHandler_List_Empty(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newCustomFieldHandlerTestDB(t)
	svc := services.NewCustomFieldService(db)
	handler := NewCustomFieldHandler(svc)

	router := gin.New()
	router.GET("/custom-fields", handler.List)

	req := httptest.NewRequest("GET", "/custom-fields", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response []models.CustomField
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Empty(t, response)
}

func TestCustomFieldHandler_Create_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newCustomFieldHandlerTestDB(t)
	svc := services.NewCustomFieldService(db)
	handler := NewCustomFieldHandler(svc)

	router := gin.New()
	router.POST("/custom-fields", handler.Create)

	payload := map[string]interface{}{
		"key":      "test_field",
		"name":     "Test Field",
		"type":     "string",
		"required": true,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/custom-fields", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestCustomFieldHandler_Create_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newCustomFieldHandlerTestDB(t)
	svc := services.NewCustomFieldService(db)
	handler := NewCustomFieldHandler(svc)

	router := gin.New()
	router.POST("/custom-fields", handler.Create)

	req := httptest.NewRequest("POST", "/custom-fields", bytes.NewReader([]byte("{invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCustomFieldHandler_Get_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newCustomFieldHandlerTestDB(t)
	svc := services.NewCustomFieldService(db)
	handler := NewCustomFieldHandler(svc)

	router := gin.New()
	router.GET("/custom-fields/:id", handler.Get)

	req := httptest.NewRequest("GET", "/custom-fields/1", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCustomFieldHandler_Update_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newCustomFieldHandlerTestDB(t)
	svc := services.NewCustomFieldService(db)
	handler := NewCustomFieldHandler(svc)

	router := gin.New()
	router.PUT("/custom-fields/:id", handler.Update)

	payload := map[string]string{"name": "updated"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("PUT", "/custom-fields/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCustomFieldHandler_Delete_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newCustomFieldHandlerTestDB(t)
	svc := services.NewCustomFieldService(db)
	handler := NewCustomFieldHandler(svc)

	router := gin.New()
	router.DELETE("/custom-fields/:id", handler.Delete)

	req := httptest.NewRequest("DELETE", "/custom-fields/1", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestNewCustomFieldHandler(t *testing.T) {
	db := newCustomFieldHandlerTestDB(t)
	svc := services.NewCustomFieldService(db)

	handler := NewCustomFieldHandler(svc)

	assert.NotNil(t, handler)
	assert.Equal(t, svc, handler.service)
}
