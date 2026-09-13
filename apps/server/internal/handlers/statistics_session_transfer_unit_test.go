package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	analyticscontract "servify/apps/server/internal/modules/analytics/contract"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func newStatisticsUnitRouter(svc *unitAnalyticsService) *gin.Engine {
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
		r := newStatisticsUnitRouter(&unitAnalyticsService{dashboard: &analyticscontract.DashboardStats{}})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/statistics/dashboard", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("error", func(t *testing.T) {
		r := newStatisticsUnitRouter(&unitAnalyticsService{dashboardErr: errors.New("boom")})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/statistics/dashboard", nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestStatisticsHandlerUnitTimeRange(t *testing.T) {
	ok := &unitAnalyticsService{timeRange: []analyticscontract.TimeRangeStats{{}}}
	cases := []struct {
		name string
		svc  *unitAnalyticsService
		path string
		want int
	}{
		{"success", ok, "/statistics/time-range?start_date=2026-01-01&end_date=2026-02-01", http.StatusOK},
		{"missing params", &unitAnalyticsService{}, "/statistics/time-range", http.StatusBadRequest},
		{"missing end", &unitAnalyticsService{}, "/statistics/time-range?start_date=2026-01-01", http.StatusBadRequest},
		{"bad start", &unitAnalyticsService{}, "/statistics/time-range?start_date=zz&end_date=2026-02-01", http.StatusBadRequest},
		{"bad end", &unitAnalyticsService{}, "/statistics/time-range?start_date=2026-01-01&end_date=zz", http.StatusBadRequest},
		{"end before start", &unitAnalyticsService{}, "/statistics/time-range?start_date=2026-03-01&end_date=2026-02-01", http.StatusBadRequest},
		{"error", &unitAnalyticsService{timeRangeErr: errors.New("boom")}, "/statistics/time-range?start_date=2026-01-01&end_date=2026-02-01", http.StatusInternalServerError},
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
	ok := &unitAnalyticsService{agentPerf: []analyticscontract.AgentPerformanceStats{{}}}
	cases := []struct {
		name string
		svc  *unitAnalyticsService
		path string
		want int
	}{
		{"success", ok, "/statistics/agent-performance?start_date=2026-01-01&end_date=2026-02-01&limit=3", http.StatusOK},
		{"bad limit falls back", ok, "/statistics/agent-performance?start_date=2026-01-01&end_date=2026-02-01&limit=zz", http.StatusOK},
		{"missing params", &unitAnalyticsService{}, "/statistics/agent-performance", http.StatusBadRequest},
		{"bad start", &unitAnalyticsService{}, "/statistics/agent-performance?start_date=zz&end_date=2026-02-01", http.StatusBadRequest},
		{"bad end", &unitAnalyticsService{}, "/statistics/agent-performance?start_date=2026-01-01&end_date=zz", http.StatusBadRequest},
		{"error", &unitAnalyticsService{agentPerfErr: errors.New("boom")}, "/statistics/agent-performance?start_date=2026-01-01&end_date=2026-02-01", http.StatusInternalServerError},
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
	ok := &unitAnalyticsService{category: []analyticscontract.CategoryStats{{}}}
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
		t.Run("only start defaults "+base, func(t *testing.T) {
			r := newStatisticsUnitRouter(ok)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, base+"?start_date=2026-01-01", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d", w.Code)
			}
		})
		t.Run("bad start "+base, func(t *testing.T) {
			r := newStatisticsUnitRouter(&unitAnalyticsService{})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, base+"?start_date=zz&end_date=2026-02-01", nil))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d", w.Code)
			}
		})
		t.Run("bad end "+base, func(t *testing.T) {
			r := newStatisticsUnitRouter(&unitAnalyticsService{})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, base+"?start_date=2026-01-01&end_date=zz", nil))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d", w.Code)
			}
		})
		t.Run("error "+base, func(t *testing.T) {
			r := newStatisticsUnitRouter(&unitAnalyticsService{categoryErr: errors.New("boom")})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, base+"?start_date=2026-01-01&end_date=2026-02-01", nil))
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d", w.Code)
			}
		})
	}
}

func TestStatisticsHandlerUnitSourceRemoteDaily(t *testing.T) {
	t.Run("source success", func(t *testing.T) {
		r := newStatisticsUnitRouter(&unitAnalyticsService{})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/statistics/customer-source", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("source error", func(t *testing.T) {
		r := newStatisticsUnitRouter(&unitAnalyticsService{sourceErr: errors.New("boom")})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/statistics/customer-source", nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("remote success", func(t *testing.T) {
		r := newStatisticsUnitRouter(&unitAnalyticsService{remoteAssist: &analyticscontract.RemoteAssistTicketStats{}})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/statistics/remote-assist-tickets", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("remote error", func(t *testing.T) {
		r := newStatisticsUnitRouter(&unitAnalyticsService{remoteErr: errors.New("boom")})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/statistics/remote-assist-tickets", nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("daily default date", func(t *testing.T) {
		r := newStatisticsUnitRouter(&unitAnalyticsService{})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/statistics/update-daily", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("daily explicit date", func(t *testing.T) {
		r := newStatisticsUnitRouter(&unitAnalyticsService{})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/statistics/update-daily?date=2026-05-01", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("daily bad date", func(t *testing.T) {
		r := newStatisticsUnitRouter(&unitAnalyticsService{})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/statistics/update-daily?date=zz", nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("daily error", func(t *testing.T) {
		r := newStatisticsUnitRouter(&unitAnalyticsService{updateErr: errors.New("boom")})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/statistics/update-daily", nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestSessionTransferHandlerUnit(t *testing.T) {
	newRouter := func(svc *unitTransferService, withUser bool) *gin.Engine {
		gin.SetMode(gin.TestMode)
		logger := logrus.New()
		logger.SetLevel(logrus.ErrorLevel)
		h := NewSessionTransferHandler(svc, logger)
		r := gin.New()
		if withUser {
			r.Use(func(c *gin.Context) {
				c.Set("user_id", uint(4))
				c.Next()
			})
		}
		RegisterSessionTransferRoutes(&r.RouterGroup, h)
		return r
	}
	request := func(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	result := transferResultFixture()

	t.Run("to human success", func(t *testing.T) {
		svc := &unitTransferService{transfer: result}
		w := request(newRouter(svc, false), http.MethodPost, "/session-transfer/to-human", `{"session_id":"s1"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("to human bad body", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{}, false), http.MethodPost, "/session-transfer/to-human", `{`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("to human error", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{humanErr: errors.New("boom")}, false), http.MethodPost, "/session-transfer/to-human", `{"session_id":"s1"}`)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("to agent success", func(t *testing.T) {
		svc := &unitTransferService{transfer: result}
		w := request(newRouter(svc, false), http.MethodPost, "/session-transfer/to-agent", `{"session_id":"s1","target_agent_id":3}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("to agent bad body", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{}, false), http.MethodPost, "/session-transfer/to-agent", `{"session_id":"s1"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("to agent error", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{agentErr: errors.New("boom")}, false), http.MethodPost, "/session-transfer/to-agent", `{"session_id":"s1","target_agent_id":3}`)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("history success", func(t *testing.T) {
		svc := &unitTransferService{history: transferHistoryFixture()}
		w := request(newRouter(svc, false), http.MethodGet, "/session-transfer/history/s1", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("history error", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{historyErr: errors.New("boom")}, false), http.MethodGet, "/session-transfer/history/s1", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("history empty session id via direct context", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		logger := logrus.New()
		logger.SetLevel(logrus.ErrorLevel)
		h := NewSessionTransferHandler(&unitTransferService{}, logger)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/history", nil)
		h.GetTransferHistory(c)
		if c.Writer.Status() != http.StatusBadRequest {
			t.Fatalf("status = %d", c.Writer.Status())
		}
	})
	t.Run("recent history success and limit variants", func(t *testing.T) {
		svc := &unitTransferService{history: transferHistoryFixture()}
		r := newRouter(svc, false)
		for _, q := range []string{"?limit=5", "?limit=zz", ""} {
			w := request(r, http.MethodGet, "/session-transfer/history"+q, "")
			if w.Code != http.StatusOK {
				t.Fatalf("query %s status = %d", q, w.Code)
			}
		}
	})
	t.Run("recent history error", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{listErr: errors.New("boom")}, false), http.MethodGet, "/session-transfer/history", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("waiting success variants", func(t *testing.T) {
		svc := &unitTransferService{}
		r := newRouter(svc, false)
		for _, q := range []string{"?status=cancelled&limit=7", "?limit=zz", ""} {
			w := request(r, http.MethodGet, "/session-transfer/waiting"+q, "")
			if w.Code != http.StatusOK {
				t.Fatalf("query %s status = %d", q, w.Code)
			}
		}
	})
	t.Run("waiting error", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{waitingErr: errors.New("boom")}, false), http.MethodGet, "/session-transfer/waiting", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("cancel without user", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{}, false), http.MethodPost, "/session-transfer/cancel", `{"session_id":"s1"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("cancel with user", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{}, true), http.MethodPost, "/session-transfer/cancel", `{"session_id":"s1","reason":"r"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("cancel bad body", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{}, true), http.MethodPost, "/session-transfer/cancel", `{`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("cancel error", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{cancelErr: errors.New("boom")}, true), http.MethodPost, "/session-transfer/cancel", `{"session_id":"s1"}`)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("process queue success", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{}, false), http.MethodPost, "/session-transfer/process-queue", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("process queue error", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{processErr: errors.New("boom")}, false), http.MethodPost, "/session-transfer/process-queue", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("check auto transfer true and false", func(t *testing.T) {
		body := `{"session_id":"s1","messages":[{"content":"hello","sender":"customer"}]}`
		w := request(newRouter(&unitTransferService{shouldTran: true}, false), http.MethodPost, "/session-transfer/check-auto", body)
		if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"should_transfer":true`)) {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
		w = request(newRouter(&unitTransferService{shouldTran: false}, false), http.MethodPost, "/session-transfer/check-auto", body)
		if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"should_transfer":false`)) {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("check auto bad body", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{}, false), http.MethodPost, "/session-transfer/check-auto", `{"session_id":"s1"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
}
