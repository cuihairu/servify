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
	customerdelivery "servify/apps/server/internal/modules/customer/delivery"
	auditplatform "servify/apps/server/internal/platform/audit"
)

func newTestDBForCustomers(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(uniqueMemDSN("file:customer_handler")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	// CustomerService.GetCustomerByID preloads Sessions and Tickets.
	if err := db.AutoMigrate(
		&models.User{},
		&models.Customer{},
		&models.Session{},
		&models.Ticket{},
		&models.Message{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	return db
}

func TestCustomerHandler_Create_Get_Update_List(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForCustomers(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	svc := customerdelivery.NewHandlerService(db)
	h := NewCustomerHandler(svc, logger)

	r := gin.New()
	r.POST("/api/customers", h.CreateCustomer)
	r.GET("/api/customers", h.ListCustomers)
	r.GET("/api/customers/:id", h.GetCustomer)
	r.PUT("/api/customers/:id", h.UpdateCustomer)
	r.POST("/api/customers/:id/revoke-tokens", h.RevokeCustomerTokens)

	// Create
	createBody := map[string]any{
		"username": "c2",
		"email":    "c2@example.com",
		"name":     "c2 name",
		"company":  "acme",
	}
	b, _ := json.Marshal(createBody)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/customers", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var created models.User
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created: %v body=%s", err, w.Body.String())
	}
	if created.ID == 0 {
		t.Fatalf("expected created customer id")
	}

	// Get
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/api/customers/"+toStr(created.ID), nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", w2.Code, w2.Body.String())
	}

	// Update
	newName := "c2 name updated"
	updateBody := map[string]any{"name": newName}
	bu, _ := json.Marshal(updateBody)
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodPut, "/api/customers/"+toStr(created.ID), bytes.NewReader(bu))
	req3.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", w3.Code, w3.Body.String())
	}

	// List (no search/ILIKE filters to keep sqlite compatibility)
	w4 := httptest.NewRecorder()
	req4, _ := http.NewRequest(http.MethodGet, "/api/customers?page=1&page_size=10", nil)
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", w4.Code, w4.Body.String())
	}

	// Revoke tokens
	w5 := httptest.NewRecorder()
	req5, _ := http.NewRequest(http.MethodPost, "/api/customers/"+toStr(created.ID)+"/revoke-tokens", nil)
	r.ServeHTTP(w5, req5)
	if w5.Code != http.StatusOK {
		t.Fatalf("revoke tokens status=%d body=%s", w5.Code, w5.Body.String())
	}
}

func TestCustomerHandler_GetCustomerActivity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForCustomers(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	svc := customerdelivery.NewHandlerService(db)
	h := NewCustomerHandler(svc, logger)

	r := gin.New()
	r.GET("/api/customers/:id/activity", h.GetCustomerActivity)

	// Create customer
	user := &models.User{Username: "test", Email: "test@example.com", Name: "Test", Role: "customer"}
	db.Create(user)
	customer := &models.Customer{UserID: user.ID, Company: "TestCo"}
	db.Create(customer)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/customers/1/activity", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestCustomerHandler_AddCustomerNote(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForCustomers(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	svc := customerdelivery.NewHandlerService(db)
	h := NewCustomerHandler(svc, logger)

	r := gin.New()
	// 添加中间件来设置user_id
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(1))
		c.Next()
	})
	r.POST("/api/customers/:id/notes", h.AddCustomerNote)

	// Create customer
	user := &models.User{Username: "test2", Email: "test2@example.com", Name: "Test", Role: "customer"}
	db.Create(user)
	customer := &models.Customer{UserID: user.ID, Company: "TestCo"}
	db.Create(customer)

	payload := map[string]string{"note": "Test note"}
	body, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/customers/1/notes", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestCustomerHandler_UpdateCustomerTags(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForCustomers(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	svc := customerdelivery.NewHandlerService(db)
	h := NewCustomerHandler(svc, logger)

	r := gin.New()
	r.PUT("/api/customers/:id/tags", h.UpdateCustomerTags)

	// Create customer
	user := &models.User{Username: "test3", Email: "test3@example.com", Name: "Test", Role: "customer"}
	db.Create(user)
	customer := &models.Customer{UserID: user.ID, Company: "TestCo"}
	db.Create(customer)

	payload := map[string]interface{}{"tags": []string{"vip", "enterprise"}}
	body, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/customers/1/tags", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestCustomerHandler_GetCustomerStats(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForCustomers(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	svc := customerdelivery.NewHandlerService(db)
	h := NewCustomerHandler(svc, logger)

	r := gin.New()
	r.GET("/api/customers/stats", h.GetCustomerStats)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/customers/stats", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestCustomerHandler_AuditSnapshotsForWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newTestDBForCustomers(t)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	svc := customerdelivery.NewHandlerService(db)
	h := NewCustomerHandler(svc, logger)
	recorder := &ticketAuditRecorder{}

	user := &models.User{ID: 10, Username: "cust-audit", Email: "cust-audit@example.com", Name: "Cust Audit", Role: "customer", TokenVersion: 1}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	customer := &models.Customer{UserID: user.ID, Company: "OldCo", Tags: "old", Notes: "old-note"}
	if err := db.Create(customer).Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uint(99))
		c.Set("principal_kind", "admin")
		c.Next()
	})
	r.Use(auditplatform.Middleware(recorder))
	r.PUT("/api/customers/:id", h.UpdateCustomer)
	r.POST("/api/customers/:id/revoke-tokens", h.RevokeCustomerTokens)
	r.POST("/api/customers/:id/notes", h.AddCustomerNote)
	r.PUT("/api/customers/:id/tags", h.UpdateCustomerTags)

	updateBody, _ := json.Marshal(map[string]any{"name": "Cust Audit Updated", "company": "NewCo"})
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodPut, "/api/customers/10", bytes.NewReader(updateBody))
	req1.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", w1.Code, w1.Body.String())
	}

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodPost, "/api/customers/10/revoke-tokens", nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", w2.Code, w2.Body.String())
	}

	noteBody, _ := json.Marshal(map[string]any{"note": "fresh note"})
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodPost, "/api/customers/10/notes", bytes.NewReader(noteBody))
	req3.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("note status=%d body=%s", w3.Code, w3.Body.String())
	}

	tagsBody, _ := json.Marshal(map[string]any{"tags": []string{"vip", "enterprise"}})
	w4 := httptest.NewRecorder()
	req4, _ := http.NewRequest(http.MethodPut, "/api/customers/10/tags", bytes.NewReader(tagsBody))
	req4.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("tags status=%d body=%s", w4.Code, w4.Body.String())
	}

	if len(recorder.entries) != 4 {
		t.Fatalf("expected 4 audit entries got %d", len(recorder.entries))
	}
	for _, entry := range recorder.entries {
		if entry.BeforeJSON == "" || entry.AfterJSON == "" {
			t.Fatalf("expected before/after snapshot for %s, got before=%q after=%q", entry.Action, entry.BeforeJSON, entry.AfterJSON)
		}
	}
	if !strings.Contains(recorder.entries[0].BeforeJSON, "Cust Audit") || !strings.Contains(recorder.entries[0].AfterJSON, "Cust Audit Updated") {
		t.Fatalf("unexpected update snapshots: before=%s after=%s", recorder.entries[0].BeforeJSON, recorder.entries[0].AfterJSON)
	}
	if !strings.Contains(recorder.entries[1].BeforeJSON, `"token_version":1`) || !strings.Contains(recorder.entries[1].AfterJSON, `"token_version":2`) {
		t.Fatalf("unexpected revoke snapshots: before=%s after=%s", recorder.entries[1].BeforeJSON, recorder.entries[1].AfterJSON)
	}
	if !strings.Contains(recorder.entries[3].AfterJSON, "vip") || !strings.Contains(recorder.entries[3].AfterJSON, "enterprise") {
		t.Fatalf("unexpected tags snapshot: %s", recorder.entries[3].AfterJSON)
	}
}
