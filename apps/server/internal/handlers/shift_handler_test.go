//go:build integration
// +build integration

package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	shiftapp "servify/apps/server/internal/modules/shift/application"
	shiftdelivery "servify/apps/server/internal/modules/shift/delivery"
	shiftinfra "servify/apps/server/internal/modules/shift/infra"
)

func newShiftHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := t.Name()
	dsn := uniqueMemDSN("file:shift_handler_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.ShiftSchedule{},
		&models.Agent{},
		&models.User{},
	); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func TestShiftHandler_CreateShift_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newShiftHandlerTestDB(t)
	svc := shiftdelivery.NewHandlerServiceAdapter(shiftapp.NewService(shiftinfra.NewGormRepository(db)))
	handler := NewShiftHandler(svc)

	router := gin.New()
	router.POST("/shifts", handler.CreateShift)

	// Create agent
	now := time.Now()
	user := &models.User{ID: 1, Username: "agent1", Name: "Agent", Email: "agent@example.com", Role: "agent"}
	agent := &models.Agent{ID: 1, UserID: 1, Status: "online", MaxConcurrent: 5, CurrentLoad: 0}
	db.Create(user)
	db.Create(agent)

	startTime := now.Add(1 * time.Hour)
	endTime := now.Add(9 * time.Hour)

	payload := map[string]interface{}{
		"agent_id":   1,
		"start_time": startTime.Format(time.RFC3339),
		"end_time":   endTime.Format(time.RFC3339),
		"shift_type": "regular",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/shifts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestShiftHandler_CreateShift_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newShiftHandlerTestDB(t)
	svc := shiftdelivery.NewHandlerServiceAdapter(shiftapp.NewService(shiftinfra.NewGormRepository(db)))
	handler := NewShiftHandler(svc)

	router := gin.New()
	router.POST("/shifts", handler.CreateShift)

	req := httptest.NewRequest("POST", "/shifts", bytes.NewReader([]byte("{invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestShiftHandler_ListShifts_Empty(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newShiftHandlerTestDB(t)
	svc := shiftdelivery.NewHandlerServiceAdapter(shiftapp.NewService(shiftinfra.NewGormRepository(db)))
	handler := NewShiftHandler(svc)

	router := gin.New()
	router.GET("/shifts", handler.ListShifts)

	req := httptest.NewRequest("GET", "/shifts", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, float64(0), response["total"])
}

func TestShiftHandler_UpdateShift_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newShiftHandlerTestDB(t)
	svc := shiftdelivery.NewHandlerServiceAdapter(shiftapp.NewService(shiftinfra.NewGormRepository(db)))
	handler := NewShiftHandler(svc)

	router := gin.New()
	router.PUT("/shifts/:id", handler.UpdateShift)

	payload := map[string]string{"notes": "Updated notes"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("PUT", "/shifts/999", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestShiftHandler_UpdateShift_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newShiftHandlerTestDB(t)
	svc := shiftdelivery.NewHandlerServiceAdapter(shiftapp.NewService(shiftinfra.NewGormRepository(db)))
	handler := NewShiftHandler(svc)

	router := gin.New()
	router.PUT("/shifts/:id", handler.UpdateShift)

	payload := map[string]string{"notes": "Updated notes"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("PUT", "/shifts/invalid", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestShiftHandler_DeleteShift_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newShiftHandlerTestDB(t)
	svc := shiftdelivery.NewHandlerServiceAdapter(shiftapp.NewService(shiftinfra.NewGormRepository(db)))
	handler := NewShiftHandler(svc)

	router := gin.New()
	router.DELETE("/shifts/:id", handler.DeleteShift)

	req := httptest.NewRequest("DELETE", "/shifts/999", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestShiftHandler_DeleteShift_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newShiftHandlerTestDB(t)
	svc := shiftdelivery.NewHandlerServiceAdapter(shiftapp.NewService(shiftinfra.NewGormRepository(db)))
	handler := NewShiftHandler(svc)

	router := gin.New()
	router.DELETE("/shifts/:id", handler.DeleteShift)

	req := httptest.NewRequest("DELETE", "/shifts/invalid", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestShiftHandler_GetShiftStats_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newShiftHandlerTestDB(t)
	svc := shiftdelivery.NewHandlerServiceAdapter(shiftapp.NewService(shiftinfra.NewGormRepository(db)))
	handler := NewShiftHandler(svc)

	router := gin.New()
	router.GET("/shifts/stats", handler.GetShiftStats)

	req := httptest.NewRequest("GET", "/shifts/stats", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNewShiftHandler(t *testing.T) {
	db := newShiftHandlerTestDB(t)
	svc := shiftdelivery.NewHandlerServiceAdapter(shiftapp.NewService(shiftinfra.NewGormRepository(db)))

	handler := NewShiftHandler(svc)

	assert.NotNil(t, handler)
	assert.Equal(t, svc, handler.shiftService)
}
