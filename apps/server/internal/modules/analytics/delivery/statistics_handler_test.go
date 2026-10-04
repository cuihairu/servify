package delivery

// V1.0 收敛 B2-3：statistics handler 单元测试（自 internal/handlers
// statistics_session_transfer_unit_test.go 统计部分迁移）。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	analyticscontract "servify/apps/server/internal/modules/analytics/contract"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// stubAnalyticsHandlerService 单元测试用 HandlerService 假实现。
type stubAnalyticsHandlerService struct {
	dashboard    *analyticscontract.DashboardStats
	timeRange    []analyticscontract.TimeRangeStats
	agentPerf    []analyticscontract.AgentPerformanceStats
	category     []analyticscontract.CategoryStats
	remoteAssist *analyticscontract.RemoteAssistTicketStats
	dashboardErr error
	timeRangeErr error
	agentPerfErr error
	categoryErr  error
	sourceErr    error
	remoteErr    error
	updateErr    error
}

func (s *stubAnalyticsHandlerService) GetDashboardStats(ctx context.Context) (*analyticscontract.DashboardStats, error) {
	if s.dashboardErr != nil {
		return nil, s.dashboardErr
	}
	return s.dashboard, nil
}

func (s *stubAnalyticsHandlerService) GetTimeRangeStats(ctx context.Context, startDate, endDate time.Time) ([]analyticscontract.TimeRangeStats, error) {
	if s.timeRangeErr != nil {
		return nil, s.timeRangeErr
	}
	return s.timeRange, nil
}

func (s *stubAnalyticsHandlerService) GetAgentPerformanceStats(ctx context.Context, startDate, endDate time.Time, limit int) ([]analyticscontract.AgentPerformanceStats, error) {
	if s.agentPerfErr != nil {
		return nil, s.agentPerfErr
	}
	return s.agentPerf, nil
}

func (s *stubAnalyticsHandlerService) GetTicketCategoryStats(ctx context.Context, startDate, endDate time.Time) ([]analyticscontract.CategoryStats, error) {
	if s.categoryErr != nil {
		return nil, s.categoryErr
	}
	return s.category, nil
}

func (s *stubAnalyticsHandlerService) GetTicketPriorityStats(ctx context.Context, startDate, endDate time.Time) ([]analyticscontract.CategoryStats, error) {
	if s.categoryErr != nil {
		return nil, s.categoryErr
	}
	return s.category, nil
}

func (s *stubAnalyticsHandlerService) GetCustomerSourceStats(ctx context.Context) ([]analyticscontract.CategoryStats, error) {
	if s.sourceErr != nil {
		return nil, s.sourceErr
	}
	return s.category, nil
}

func (s *stubAnalyticsHandlerService) GetRemoteAssistTicketStats(ctx context.Context) (*analyticscontract.RemoteAssistTicketStats, error) {
	if s.remoteErr != nil {
		return nil, s.remoteErr
	}
	return s.remoteAssist, nil
}

func (s *stubAnalyticsHandlerService) UpdateDailyStats(ctx context.Context, date time.Time) error {
	return s.updateErr
}

func newStatisticsUnitRouter(svc *stubAnalyticsHandlerService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	h := NewStatisticsHandler(svc, logger)
	r := gin.New()
	RegisterStatisticsRoutes(&r.RouterGroup, h, NewStatisticsExportHandler(svc, &stubSatisfactionStatsReader{}, logger))
	return r
}

func TestStatisticsHandlerUnitDashboard(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := newStatisticsUnitRouter(&stubAnalyticsHandlerService{dashboard: &analyticscontract.DashboardStats{}})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/statistics/dashboard", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("error", func(t *testing.T) {
		r := newStatisticsUnitRouter(&stubAnalyticsHandlerService{dashboardErr: errors.New("boom")})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/statistics/dashboard", nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestStatisticsHandlerUnitTimeRange(t *testing.T) {
	ok := &stubAnalyticsHandlerService{timeRange: []analyticscontract.TimeRangeStats{{}}}
	cases := []struct {
		name string
		svc  *stubAnalyticsHandlerService
		path string
		want int
	}{
		{"success", ok, "/statistics/time-range?start_date=2026-01-01&end_date=2026-02-01", http.StatusOK},
		{"missing params", &stubAnalyticsHandlerService{}, "/statistics/time-range", http.StatusBadRequest},
		{"missing end", &stubAnalyticsHandlerService{}, "/statistics/time-range?start_date=2026-01-01", http.StatusBadRequest},
		{"bad start", &stubAnalyticsHandlerService{}, "/statistics/time-range?start_date=zz&end_date=2026-02-01", http.StatusBadRequest},
		{"bad end", &stubAnalyticsHandlerService{}, "/statistics/time-range?start_date=2026-01-01&end_date=zz", http.StatusBadRequest},
		{"end before start", &stubAnalyticsHandlerService{}, "/statistics/time-range?start_date=2026-03-01&end_date=2026-02-01", http.StatusBadRequest},
		{"error", &stubAnalyticsHandlerService{timeRangeErr: errors.New("boom")}, "/statistics/time-range?start_date=2026-01-01&end_date=2026-02-01", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newStatisticsUnitRouter(tc.svc)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestStatisticsHandlerUnitAgentPerformance(t *testing.T) {
	ok := &stubAnalyticsHandlerService{agentPerf: []analyticscontract.AgentPerformanceStats{{}}}
	cases := []struct {
		name string
		svc  *stubAnalyticsHandlerService
		path string
		want int
	}{
		{"success", ok, "/statistics/agent-performance?start_date=2026-01-01&end_date=2026-02-01&limit=3", http.StatusOK},
		{"bad limit falls back", ok, "/statistics/agent-performance?start_date=2026-01-01&end_date=2026-02-01&limit=zz", http.StatusOK},
		{"missing params", &stubAnalyticsHandlerService{}, "/statistics/agent-performance", http.StatusBadRequest},
		{"bad start", &stubAnalyticsHandlerService{}, "/statistics/agent-performance?start_date=zz&end_date=2026-02-01", http.StatusBadRequest},
		{"bad end", &stubAnalyticsHandlerService{}, "/statistics/agent-performance?start_date=2026-01-01&end_date=zz", http.StatusBadRequest},
		{"error", &stubAnalyticsHandlerService{agentPerfErr: errors.New("boom")}, "/statistics/agent-performance?start_date=2026-01-01&end_date=2026-02-01", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newStatisticsUnitRouter(tc.svc)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestStatisticsHandlerUnitCategoryPriority(t *testing.T) {
	ok := &stubAnalyticsHandlerService{category: []analyticscontract.CategoryStats{{}}}
	paths := []string{"/statistics/ticket-category", "/statistics/ticket-priority"}
	for _, base := range paths {
		t.Run("default range "+base, func(t *testing.T) {
			r := newStatisticsUnitRouter(ok)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, base, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d", w.Code)
			}
		})
		t.Run("explicit range "+base, func(t *testing.T) {
			r := newStatisticsUnitRouter(ok)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, base+"?start_date=2026-01-01&end_date=2026-02-01", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d", w.Code)
			}
		})
		t.Run("bad range "+base, func(t *testing.T) {
			r := newStatisticsUnitRouter(ok)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, base+"?start_date=zz&end_date=2026-02-01", nil))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d", w.Code)
			}
		})
		t.Run("error "+base, func(t *testing.T) {
			r := newStatisticsUnitRouter(&stubAnalyticsHandlerService{categoryErr: errors.New("boom")})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, base, nil))
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d", w.Code)
			}
		})
	}
}

func TestStatisticsHandlerUnitSourceRemoteDaily(t *testing.T) {
	t.Run("source success", func(t *testing.T) {
		r := newStatisticsUnitRouter(&stubAnalyticsHandlerService{category: []analyticscontract.CategoryStats{{}}})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/statistics/customer-source", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("source error", func(t *testing.T) {
		r := newStatisticsUnitRouter(&stubAnalyticsHandlerService{sourceErr: errors.New("boom")})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/statistics/customer-source", nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("remote assist success", func(t *testing.T) {
		r := newStatisticsUnitRouter(&stubAnalyticsHandlerService{remoteAssist: &analyticscontract.RemoteAssistTicketStats{}})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/statistics/remote-assist-tickets", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("remote assist error", func(t *testing.T) {
		r := newStatisticsUnitRouter(&stubAnalyticsHandlerService{remoteErr: errors.New("boom")})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/statistics/remote-assist-tickets", nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("update daily success", func(t *testing.T) {
		r := newStatisticsUnitRouter(&stubAnalyticsHandlerService{})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/statistics/update-daily?date=2026-04-08", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("update daily bad date", func(t *testing.T) {
		r := newStatisticsUnitRouter(&stubAnalyticsHandlerService{})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/statistics/update-daily?date=zz", nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("update daily error", func(t *testing.T) {
		r := newStatisticsUnitRouter(&stubAnalyticsHandlerService{updateErr: errors.New("boom")})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/statistics/update-daily", nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}
