package delivery

// V1.0 收敛 B2-3：统计导出测试（自 internal/handlers statistics_export_
// handler_test.go、hscov_seam_test.go、cvh_gaps_test.go 统计部分迁移）。
// seam 错误分支驱动本包私有的 newExportCSVWriter / hookExportExcelize*
// 注入点。

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	analyticscontract "servify/apps/server/internal/modules/analytics/contract"
	satisfactiondelivery "servify/apps/server/internal/modules/satisfaction/delivery"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/xuri/excelize/v2"
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

func exportTestRouter(analytics *stubAnalyticsHandlerService, reader SatisfactionStatsReader) *gin.Engine {
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

// seamExportCSV 让 CSV 导出写入必然失败的 writer（驱动行写入/Flush 错误）。
func seamExportCSV(t *testing.T) {
	t.Helper()
	old := newExportCSVWriter
	newExportCSVWriter = func(io.Writer) *csv.Writer {
		return csv.NewWriter(failingExportWriter{})
	}
	t.Cleanup(func() { newExportCSVWriter = old })
}

// failingExportWriter 对 Write 一律失败，模拟底层 writer 错误。
type failingExportWriter struct{}

func (failingExportWriter) Write(p []byte) (int, error) { return 0, errors.New("boom: writer failed") }

// setExportSeam 替换一个包级 seam 并在测试结束后还原。
func setExportSeam[T any](t *testing.T, slot *T, value T) {
	t.Helper()
	old := *slot
	*slot = value
	t.Cleanup(func() { *slot = old })
}

func TestExportStatisticsTimeRangeCSV(t *testing.T) {
	analytics := &stubAnalyticsHandlerService{
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
	analytics := &stubAnalyticsHandlerService{
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
	analytics := &stubAnalyticsHandlerService{
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
	r := exportTestRouter(&stubAnalyticsHandlerService{}, reader)

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
	r := exportTestRouter(&stubAnalyticsHandlerService{}, &stubSatisfactionStatsReader{})

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

// --- 长尾分支（原 cvh_gaps_test.go 统计部分）---

func TestStatisticsExportBuildRowsErrors(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cases := []struct {
		name      string
		analytics *stubAnalyticsHandlerService
		want      int
	}{
		{"time_range", &stubAnalyticsHandlerService{timeRangeErr: errors.New("boom")}, http.StatusInternalServerError},
		{"agent_performance", &stubAnalyticsHandlerService{agentPerfErr: errors.New("boom")}, http.StatusInternalServerError},
		{"ticket_category", &stubAnalyticsHandlerService{categoryErr: errors.New("boom")}, http.StatusInternalServerError},
		{"customer_source", &stubAnalyticsHandlerService{sourceErr: errors.New("boom")}, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := exportTestRouter(tc.analytics, &stubSatisfactionStatsReader{})
			w := doGet(r, "/statistics/export?type="+tc.name+"&from=2026-01-01&to=2026-01-02")
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), "Failed to export statistics")
			assert.Contains(t, w.Body.String(), "boom")
		})
	}
}

func TestStatisticsExportTicketPriority(t *testing.T) {
	analytics := &stubAnalyticsHandlerService{category: []analyticscontract.CategoryStats{{Category: "billing", Count: 7}}}
	r := exportTestRouter(analytics, &stubSatisfactionStatsReader{})
	w := doGet(r, "/statistics/export?type=ticket_priority&from=2026-01-01&to=2026-01-02")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "billing")
}

func TestStatisticsExportSatisfactionGaps(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	t.Run("satisfaction service unavailable", func(t *testing.T) {
		r := exportTestRouter(&stubAnalyticsHandlerService{}, nil)
		w := doGet(r, "/statistics/export?type=satisfaction&from=2026-01-01&to=2026-01-02")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "satisfaction service unavailable")
	})

	t.Run("satisfaction reader error", func(t *testing.T) {
		reader := &stubSatisfactionStatsReader{err: errors.New("boom")}
		r := exportTestRouter(&stubAnalyticsHandlerService{}, reader)
		w := doGet(r, "/statistics/export?type=satisfaction&from=2026-01-01&to=2026-01-02")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to export statistics")
	})
}

func TestStatisticsBuildRowsUnsupportedType(t *testing.T) {
	h := NewStatisticsExportHandler(nil, nil, nil)
	_, _, err := h.buildRows(context.Background(), "bogus-type", time.Now(), time.Now())
	assert.ErrorContains(t, err, `unsupported export type "bogus-type"`)
}

// --- seam 错误分支（原 hscov_seam_test.go 统计部分）---

func TestStatisticsExportCSVWriteErrors(t *testing.T) {
	t.Run("csv row write error surfaces 500", func(t *testing.T) {
		seamExportCSV(t)
		analytics := &stubAnalyticsHandlerService{timeRange: []analyticscontract.TimeRangeStats{
			{Date: strings.Repeat("D", 6000), Tickets: 1},
		}}
		r := exportTestRouter(analytics, &stubSatisfactionStatsReader{})
		w := doGet(r, "/statistics/export?type=time_range&from=2026-04-08&to=2026-04-09&format=csv")
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Failed to render export") {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("csv flush error surfaces 500", func(t *testing.T) {
		seamExportCSV(t)
		r := exportTestRouter(&stubAnalyticsHandlerService{}, &stubSatisfactionStatsReader{})
		w := doGet(r, "/statistics/export?type=time_range&from=2026-04-08&to=2026-04-09&format=csv")
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Failed to render export") {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})
}

// TestStatisticsExportRenderErrorLogs：render 错误且配置了 logger 时
// 记录错误日志（Errorf 分支）后仍返回 500。
func TestStatisticsExportRenderErrorLogs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	seamExportCSV(t)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	r := gin.New()
	RegisterStatisticsRoutes(&r.RouterGroup, NewStatisticsHandler(&stubAnalyticsHandlerService{}, nil),
		NewStatisticsExportHandler(&stubAnalyticsHandlerService{}, &stubSatisfactionStatsReader{}, logger))
	w := doGet(r, "/statistics/export?type=time_range&from=2026-04-08&to=2026-04-09&format=csv")
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Failed to render export") {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestStatisticsExportXLSXErrors(t *testing.T) {
	analytics := &stubAnalyticsHandlerService{agentPerf: []analyticscontract.AgentPerformanceStats{{AgentName: "a"}}}
	path := "/statistics/export?type=agent_performance&format=xlsx"

	t.Run("header SetSheetRow error", func(t *testing.T) {
		setExportSeam(t, &hookExportExcelizeSetSheetRow, func(*excelize.File, string, string, interface{}) error {
			return errors.New("boom: set sheet row")
		})
		r := exportTestRouter(analytics, &stubSatisfactionStatsReader{})
		w := doGet(r, path)
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Failed to render export") {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("cell name error", func(t *testing.T) {
		setExportSeam(t, &hookExportExcelizeCellName, func(int, int, ...bool) (string, error) {
			return "", errors.New("boom: cell name")
		})
		r := exportTestRouter(analytics, &stubSatisfactionStatsReader{})
		w := doGet(r, path)
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Failed to render export") {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("row SetSheetRow error", func(t *testing.T) {
		calls := 0
		setExportSeam(t, &hookExportExcelizeSetSheetRow, func(f *excelize.File, sheet, cell string, v interface{}) error {
			calls++ // 第一次是表头（放行），第二次是数据行（报错）
			if calls > 1 {
				return errors.New("boom: set row")
			}
			return f.SetSheetRow(sheet, cell, v)
		})
		r := exportTestRouter(analytics, &stubSatisfactionStatsReader{})
		w := doGet(r, path)
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Failed to render export") {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("file write error", func(t *testing.T) {
		setExportSeam(t, &hookExportExcelizeWrite, func(*excelize.File, io.Writer) error {
			return errors.New("boom: write file")
		})
		r := exportTestRouter(analytics, &stubSatisfactionStatsReader{})
		w := doGet(r, path)
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Failed to render export") {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})
}
