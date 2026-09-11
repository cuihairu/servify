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
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/services"
)

func newSatisfactionHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := t.Name()
	dsn := uniqueMemDSN("file:satisfaction_handler_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.CustomerSatisfaction{},
		&models.SatisfactionSurvey{},
		&models.Ticket{},
		&models.User{},
		&models.Agent{},
		&models.Customer{},
	); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func TestSatisfactionHandler_CreateSatisfaction_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.POST("/satisfactions", handler.CreateSatisfaction)

	// Create ticket and users
	now := time.Now()
	customerUser := &models.User{ID: 1, Username: "customer", Name: "Customer", Email: "customer@example.com", Role: "customer"}
	customer := &models.Customer{UserID: 1, Company: "TestCo"}
	agentUser := &models.User{ID: 2, Username: "agent", Name: "Agent", Email: "agent@example.com", Role: "agent"}
	agent := &models.Agent{UserID: 2, Status: "online", MaxConcurrent: 5, CurrentLoad: 0}
	ticket := &models.Ticket{ID: 1, CustomerID: 1, AgentID: &agent.ID, Status: "closed", CreatedAt: now, UpdatedAt: now}

	db.Create(customerUser)
	db.Create(customer)
	db.Create(agentUser)
	db.Create(agent)
	db.Create(ticket)

	payload := map[string]interface{}{
		"ticket_id":   1,
		"customer_id": 1,
		"agent_id":    2,
		"rating":      5,
		"comment":     "Great service!",
		"category":    "quality",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/satisfactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestSatisfactionHandler_CreateSatisfaction_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.POST("/satisfactions", handler.CreateSatisfaction)

	req := httptest.NewRequest("POST", "/satisfactions", bytes.NewReader([]byte("{invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSatisfactionHandler_GetSatisfaction_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.GET("/satisfactions/:id", handler.GetSatisfaction)

	req := httptest.NewRequest("GET", "/satisfactions/999", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSatisfactionHandler_GetSatisfaction_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.GET("/satisfactions/:id", handler.GetSatisfaction)

	req := httptest.NewRequest("GET", "/satisfactions/invalid", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSatisfactionHandler_ListSatisfactions_Empty(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.GET("/satisfactions", handler.ListSatisfactions)

	req := httptest.NewRequest("GET", "/satisfactions", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, float64(0), response["total"])
}

func TestSatisfactionHandler_ListSatisfactions_InvalidDate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.GET("/satisfactions", handler.ListSatisfactions)

	req := httptest.NewRequest("GET", "/satisfactions?date_from=invalid-date", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSatisfactionHandler_ListSurveys_Empty(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.GET("/satisfactions/surveys", handler.ListSurveys)

	req := httptest.NewRequest("GET", "/satisfactions/surveys", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSatisfactionHandler_ResendSurvey_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.POST("/satisfactions/surveys/:id/resend", handler.ResendSurvey)

	req := httptest.NewRequest("POST", "/satisfactions/surveys/999/resend", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSatisfactionHandler_ResendSurvey_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.POST("/satisfactions/surveys/:id/resend", handler.ResendSurvey)

	req := httptest.NewRequest("POST", "/satisfactions/surveys/invalid/resend", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSatisfactionHandler_GetSatisfactionByTicket_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.GET("/tickets/:ticket_id/satisfaction", handler.GetSatisfactionByTicket)

	req := httptest.NewRequest("GET", "/tickets/999/satisfaction", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestSatisfactionHandler_GetSatisfactionByTicket_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.GET("/tickets/:ticket_id/satisfaction", handler.GetSatisfactionByTicket)

	req := httptest.NewRequest("GET", "/tickets/invalid/satisfaction", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSatisfactionHandler_GetSatisfactionStats_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.GET("/satisfactions/stats", handler.GetSatisfactionStats)

	req := httptest.NewRequest("GET", "/satisfactions/stats", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSatisfactionHandler_GetSatisfactionStats_InvalidDate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.GET("/satisfactions/stats", handler.GetSatisfactionStats)

	req := httptest.NewRequest("GET", "/satisfactions/stats?date_from=invalid", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSatisfactionHandler_UpdateSatisfaction_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.PUT("/satisfactions/:id", handler.UpdateSatisfaction)

	payload := map[string]string{"comment": "Updated comment"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("PUT", "/satisfactions/999", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSatisfactionHandler_UpdateSatisfaction_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.PUT("/satisfactions/:id", handler.UpdateSatisfaction)

	payload := map[string]string{"comment": "Updated comment"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("PUT", "/satisfactions/invalid", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSatisfactionHandler_DeleteSatisfaction_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.DELETE("/satisfactions/:id", handler.DeleteSatisfaction)

	req := httptest.NewRequest("DELETE", "/satisfactions/999", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestSatisfactionHandler_DeleteSatisfaction_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := services.NewSatisfactionService(db, logger)
	handler := NewSatisfactionHandler(svc, logger)

	router := gin.New()
	router.DELETE("/satisfactions/:id", handler.DeleteSatisfaction)

	req := httptest.NewRequest("DELETE", "/satisfactions/invalid", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNewSatisfactionHandler(t *testing.T) {
	db := newSatisfactionHandlerTestDB(t)
	logger := logrus.New()
	svc := services.NewSatisfactionService(db, logger)

	handler := NewSatisfactionHandler(svc, logger)

	assert.NotNil(t, handler)
	assert.Equal(t, svc, handler.satisfactionService)
	assert.Equal(t, logger, handler.logger)
}
