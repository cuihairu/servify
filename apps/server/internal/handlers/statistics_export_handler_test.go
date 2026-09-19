package handlers

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	analyticscontract "servify/apps/server/internal/modules/analytics/contract"
	satisfactiondelivery "servify/apps/server/internal/modules/satisfaction/delivery"

	"github.com/gin-gonic/gin"
)

type stubSatisfactionStatsReader struct {
	called bool
	stats  *satisfactiondelivery.SatisfactionStatsResponse
	err    error
}

func (s *stubSatisfactionStatsReader) GetSatisfactionStats(ctx context.Context, from, to *time.Time) (*satisfactiondelivery.SatisfactionStatsResponse, error) {
	s.called = true
	if s.err != nil {
		return nil, s.err
	}
	if s.stats != nil {
		return s.stats, nil
	}
	return &satisfactiondelivery.SatisfactionStatsResponse{
		TotalRatings:  2,
		AverageRating: 4.5,
		TrendData: []satisfactiondelivery.SatisfactionTrend{
			{Date: "2026-04-08", Count: 1, AverageRating: 4},
			{Date: "2026-04-09", Count: 1, AverageRating: 5},
		},
	}, nil
}

func exportTestRouter(analytics *unitAnalyticsService, reader SatisfactionStatsReader) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// RegisterStatisticsRoutes 自带 /statistics 前缀
	RegisterStatisticsRoutes(&r.RouterGroup, NewStatisticsHandler(analytics, nil), NewStatisticsExportHandler(analytics, reader, nil))
	return r
}

func doGet(r *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	return w
}

func parseCSV(t *testing.T, body []byte) ([][]string, error) {
	t.Helper()
	// 去掉 BOM
	if len(body) >= 3 && bytes.Equal(body[:3], []byte("\xEF\xBB\xBF")) {
		body = body[3:]
	}
	r := csv.NewReader(bytes.NewReader(body))
	return r.ReadAll()
}

func TestExportStatisticsTimeRangeCSV(t *testing.T) {
	analytics := &unitAnalyticsService{
		timeRange: []analyticscontract.TimeRangeStats{
			{Date: "2026-04-08", Tickets: 5, Sessions: 3, Messages: 20, ResolvedTickets: 2, AvgResponseTime: 12.5, CustomerSatisfaction: 4.25},
			{Date: "2026-04-09"},
		},
	}
	r := exportTestRouter(analytics, &stubSatisfactionStatsReader{})

	w := doGet(r, "/statistics/export?type=time_range&from=2026-04-08&to=2026-04-09&format=csv")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if disp := w.Header().Get("Content-Disposition"); !strings.Contains(disp, "servify-time_range-") || !strings.HasSuffix(disp, ".csv") {
		t.Fatalf("unexpected Content-Disposition %q", disp)
	}
	records, err := parseCSV(t, w.Body.Bytes())
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("csv rows = %d want 3", len(records))
	}
	wantHeader := []string{"日期", "工单数", "会话数", "消息数", "解决工单数", "平均响应时间(秒)", "客户满意度"}
	for i, cell := range wantHeader {
		if records[0][i] != cell {
			t.Fatalf("header[%d] = %q want %q", i, records[0][i], cell)
		}
	}
	if records[1][0] != "2026-04-08" || records[1][1] != "5" || records[1][5] != "12.50" {
		t.Fatalf("row1 unexpected: %v", records[1])
	}
	// 零填充日也要有行
	if records[2][0] != "2026-04-09" || records[2][1] != "0" {
		t.Fatalf("row2 unexpected: %v", records[2])
	}
}

func TestExportStatisticsXLSX(t *testing.T) {
	analytics := &unitAnalyticsService{
		agentPerf: []analyticscontract.AgentPerformanceStats{
			{AgentID: 7, AgentName: "坐席甲", Department: "客服部", TotalTickets: 10, ResolvedTickets: 8, Rating: 4.6},
		},
	}
	r := exportTestRouter(analytics, &stubSatisfactionStatsReader{})

	w := doGet(r, "/statistics/export?type=agent_performance&from=2026-04-08&to=2026-04-09&format=xlsx")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "spreadsheetml") {
		t.Fatalf("unexpected Content-Type %q", ct)
	}
	// xlsx 是 zip 容器，PK 魔数即可与 CSV 错误响应区分
	if !bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) {
		t.Fatalf("expected xlsx zip payload, got %x", w.Body.Bytes()[:4])
	}
	if disp := w.Header().Get("Content-Disposition"); !strings.HasSuffix(disp, ".xlsx") {
		t.Fatalf("unexpected Content-Disposition %q", disp)
	}
}

func TestExportStatisticsCategoryAndSource(t *testing.T) {
	analytics := &unitAnalyticsService{
		category: []analyticscontract.CategoryStats{{Category: "billing", Count: 12}},
	}
	r := exportTestRouter(analytics, &stubSatisfactionStatsReader{})

	w := doGet(r, "/statistics/export?type=ticket_category&from=2026-04-08&to=2026-04-08")
	records, err := parseCSV(t, w.Body.Bytes())
	if err != nil || len(records) != 2 || records[0][0] != "分类" || records[1][0] != "billing" {
		t.Fatalf("category export unexpected: %v %v", records, err)
	}

	w = doGet(r, "/statistics/export?type=customer_source")
	records, err = parseCSV(t, w.Body.Bytes())
	if err != nil || len(records) != 2 || records[0][0] != "客户来源" {
		t.Fatalf("source export unexpected: %v %v", records, err)
	}
}

func TestExportStatisticsSatisfaction(t *testing.T) {
	reader := &stubSatisfactionStatsReader{}
	r := exportTestRouter(&unitAnalyticsService{}, reader)

	w := doGet(r, "/statistics/export?type=satisfaction&from=2026-04-08&to=2026-04-09")
	records, err := parseCSV(t, w.Body.Bytes())
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if records[0][0] != "日期" || records[1][2] != "4.00" || records[2][2] != "5.00" {
		t.Fatalf("satisfaction rows unexpected: %v", records)
	}
	if !reader.called {
		t.Fatalf("expected satisfaction reader to be called")
	}
}

func TestExportStatisticsValidation(t *testing.T) {
	r := exportTestRouter(&unitAnalyticsService{}, &stubSatisfactionStatsReader{})

	cases := []struct {
		name string
		url  string
	}{
		{"unknown type", "/statistics/export?type=nope"},
		{"bad format", "/statistics/export?type=time_range&format=pdf"},
		{"bad from", "/statistics/export?type=time_range&from=04/08/2026"},
		{"bad to", "/statistics/export?type=time_range&to=yesterday"},
		{"inverted range", "/statistics/export?type=time_range&from=2026-04-09&to=2026-04-08"},
		{"too long range", "/statistics/export?type=time_range&from=2024-01-01&to=2026-04-08"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doGet(r, tc.url)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var payload map[string]string
			_ = json.Unmarshal(w.Body.Bytes(), &payload)
			if payload["error"] == "" {
				t.Fatalf("expected error field in body: %s", w.Body.String())
			}
		})
	}
}

func TestParseExportRangeDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest(http.MethodGet, "/export", nil)

	from, to, err := parseExportRange(c, "time_range")
	if err != nil {
		t.Fatalf("default range error = %v", err)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if to != today {
		t.Fatalf("default to = %v want %v", to, today)
	}
	if days := int(to.Sub(from).Hours()/24) + 1; days != 30 {
		t.Fatalf("default range = %d days want 30", days)
	}
}
