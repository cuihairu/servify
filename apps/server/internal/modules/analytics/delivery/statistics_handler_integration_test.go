//go:build integration
// +build integration

package delivery

// V1.0 收敛 B2-3：statistics handler 集成测试（自 internal/handlers
// statistics_handler_test.go 迁移，sqlite 真库打底）。

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	analyticsapp "servify/apps/server/internal/modules/analytics/application"
	analyticsinfra "servify/apps/server/internal/modules/analytics/infra"
)

var statisticsMemDBSeq atomic.Uint32

// uniqueStatisticsMemDSN 给命名内存库 DSN 追加全局唯一序号，
// 避免 -count 重跑或 -run 反复执行命中同一命名库。
func uniqueStatisticsMemDSN(base string) string {
	return base + "_" + strconv.FormatUint(uint64(statisticsMemDBSeq.Add(1)), 10) + "?mode=memory&cache=shared"
}

func newTestDBForStatistics(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(uniqueStatisticsMemDSN("file:statistics_handler")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	// StatisticsService touches these tables.
	if err := db.AutoMigrate(
		&models.User{},
		&models.Agent{},
		&models.Ticket{},
		&models.Session{},
		&models.Message{},
		&models.DailyStats{},
		&models.Customer{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	return db
}

// newStatisticsHandlerService 用 module adapter 构造 handler 依赖。
func newStatisticsHandlerService(db *gorm.DB, logger *logrus.Logger) *HandlerServiceAdapter {
	return NewHandlerServiceAdapter(analyticsapp.NewService(analyticsinfra.NewGormRepository(db)))
}

func newStatisticsIntegrationRouter(t *testing.T, db *gorm.DB) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	svc := newStatisticsHandlerService(db, logger)
	r := gin.New()
	RegisterStatisticsRoutes(&r.RouterGroup, NewStatisticsHandler(svc, logger), NewStatisticsExportHandler(svc, nil, logger))
	return r
}

func TestStatisticsHandler_Dashboard_And_TimeRange(t *testing.T) {
	r := newStatisticsIntegrationRouter(t, newTestDBForStatistics(t))

	// Dashboard should succeed even with empty DB.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/statistics/dashboard", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard status=%d body=%s", w.Code, w.Body.String())
	}

	// Missing params should fail fast.
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/statistics/time-range", nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("time-range missing params status=%d body=%s", w2.Code, w2.Body.String())
	}

	// Valid params should return 200.
	today := time.Now().Format("2006-01-02")
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodGet, "/statistics/time-range?start_date="+today+"&end_date="+today, nil)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("time-range ok status=%d body=%s", w3.Code, w3.Body.String())
	}
}

func TestStatisticsHandler_GetAgentPerformanceStats_SQLiteError(t *testing.T) {
	r := newStatisticsIntegrationRouter(t, newTestDBForStatistics(t))

	// SQLite doesn't support PostgreSQL's EXTRACT function, so this returns empty array
	today := time.Now().Format("2006-01-02")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/statistics/agent-performance?start_date="+today+"&end_date="+today, nil)
	r.ServeHTTP(w, req)

	// Should return 200 with empty array (graceful degradation for SQLite)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 with empty array (SQLite graceful degradation), got %d, body: %s", w.Code, w.Body.String())
	}
	// Verify response is an empty array
	var response []interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(response) != 0 {
		t.Fatalf("expected empty array, got %d items", len(response))
	}
}

func TestStatisticsHandler_GetTicketCategoryStats(t *testing.T) {
	r := newStatisticsIntegrationRouter(t, newTestDBForStatistics(t))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/statistics/ticket-category", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestStatisticsHandler_GetTicketPriorityStats(t *testing.T) {
	r := newStatisticsIntegrationRouter(t, newTestDBForStatistics(t))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/statistics/ticket-priority", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestStatisticsHandler_GetCustomerSourceStats(t *testing.T) {
	r := newStatisticsIntegrationRouter(t, newTestDBForStatistics(t))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/statistics/customer-source", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestStatisticsHandler_GetRemoteAssistTicketStats(t *testing.T) {
	db := newTestDBForStatistics(t)

	now := time.Now()
	if err := db.Create(&models.Ticket{Title: "ra-open", CustomerID: 1, Status: "open", Source: "remote_assist", Tags: "remote_assist", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed open remote assist ticket: %v", err)
	}
	closedAt := now.Add(2 * time.Hour)
	if err := db.Create(&models.Ticket{Title: "ra-resolved", CustomerID: 1, Status: "resolved", Source: "remote_assist", Tags: "remote_assist", CreatedAt: now, UpdatedAt: now, ClosedAt: &closedAt}).Error; err != nil {
		t.Fatalf("seed resolved remote assist ticket: %v", err)
	}
	if err := db.Create(&models.Ticket{Title: "other", CustomerID: 1, Status: "closed", Source: "web", Tags: "normal", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed non remote assist ticket: %v", err)
	}

	r := newStatisticsIntegrationRouter(t, db)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/statistics/remote-assist-tickets", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var got struct {
		Total         int64   `json:"total"`
		Open          int64   `json:"open"`
		Resolved      int64   `json:"resolved"`
		Closed        int64   `json:"closed"`
		ResolvedRate  float64 `json:"resolved_rate"`
		ClosedRate    float64 `json:"closed_rate"`
		AvgCloseHours float64 `json:"avg_close_hours"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal stats: %v body=%s", err, w.Body.String())
	}
	if got.Total != 2 || got.Open != 1 || got.Resolved != 1 || got.Closed != 0 {
		t.Fatalf("unexpected remote assist stats: %+v", got)
	}
	if got.ResolvedRate != 0.5 || got.ClosedRate != 0 {
		t.Fatalf("unexpected remote assist rates: %+v", got)
	}
	if math.Abs(got.AvgCloseHours-2) > 0.001 {
		t.Fatalf("unexpected avg close hours: %+v", got)
	}
}
