//go:build integration
// +build integration

package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
	"servify/apps/server/internal/models"
	workspaceapp "servify/apps/server/internal/modules/workspace/application"
	workspacedelivery "servify/apps/server/internal/modules/workspace/delivery"
	workspaceinfra "servify/apps/server/internal/modules/workspace/infra"
)

func newWorkspaceHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := t.Name()
	dsn := uniqueMemDSN("file:workspace_handler_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Session{},
		&models.Agent{},
		&models.User{},
		&models.Customer{},
		&models.Ticket{},
	); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func TestWorkspaceHandler_GetOverview_EmptyDB(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newWorkspaceHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	agentSvc := newTestAgentService(db, logger)
	workspaceSvc := workspacedelivery.NewHandlerServiceAdapter(workspaceapp.NewService(workspaceinfra.NewGormRepository(db), agentSvc))
	handler := NewWorkspaceHandler(workspaceSvc)

	router := gin.New()
	router.GET("/omni/workspace", handler.GetOverview)

	req := httptest.NewRequest("GET", "/omni/workspace", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "total_active_sessions")
	assert.Contains(t, w.Body.String(), "online_agents")
}

func TestWorkspaceHandler_GetOverview_WithLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newWorkspaceHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	agentSvc := newTestAgentService(db, logger)
	workspaceSvc := workspacedelivery.NewHandlerServiceAdapter(workspaceapp.NewService(workspaceinfra.NewGormRepository(db), agentSvc))
	handler := NewWorkspaceHandler(workspaceSvc)

	router := gin.New()
	router.GET("/omni/workspace", handler.GetOverview)

	req := httptest.NewRequest("GET", "/omni/workspace?limit=20", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestWorkspaceHandler_GetOverview_InvalidLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newWorkspaceHandlerTestDB(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	agentSvc := newTestAgentService(db, logger)
	workspaceSvc := workspacedelivery.NewHandlerServiceAdapter(workspaceapp.NewService(workspaceinfra.NewGormRepository(db), agentSvc))
	handler := NewWorkspaceHandler(workspaceSvc)

	router := gin.New()
	router.GET("/omni/workspace", handler.GetOverview)

	req := httptest.NewRequest("GET", "/omni/workspace?limit=invalid", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Invalid limit should default to 10 and still return OK
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNewWorkspaceHandler(t *testing.T) {
	db := newWorkspaceHandlerTestDB(t)
	logger := logrus.New()
	agentSvc := newTestAgentService(db, logger)
	workspaceSvc := workspacedelivery.NewHandlerServiceAdapter(workspaceapp.NewService(workspaceinfra.NewGormRepository(db), agentSvc))
	handler := NewWorkspaceHandler(workspaceSvc)

	assert.NotNil(t, handler)
	assert.Equal(t, workspaceSvc, handler.service)
}

func TestRegisterWorkspaceRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newWorkspaceHandlerTestDB(t)
	logger := logrus.New()
	agentSvc := newTestAgentService(db, logger)
	workspaceSvc := workspacedelivery.NewHandlerServiceAdapter(workspaceapp.NewService(workspaceinfra.NewGormRepository(db), agentSvc))
	handler := NewWorkspaceHandler(workspaceSvc)

	router := gin.New()
	group := router.Group("/api")
	RegisterWorkspaceRoutes(group, handler)

	routes := router.Routes()

	// Verify route is registered
	routePaths := make(map[string]bool)
	for _, route := range routes {
		routePaths[route.Path] = true
	}

	assert.True(t, routePaths["/api/omni/workspace"], "GET /omni/workspace should be registered")
}
