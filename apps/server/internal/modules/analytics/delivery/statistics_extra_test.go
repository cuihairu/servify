package delivery

// 覆盖补充：parseOptionalRange 双参数齐全的解析分支（非法 end_date → 400、
// 双合法日期放行）与导出 buildRows 服务报错 → 500（带日志路径）。

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	analyticscontract "servify/apps/server/internal/modules/analytics/contract"
)

func TestParseOptionalRangeRejectsInvalidEndDate(t *testing.T) {
	r := newStatisticsUnitRouter(&stubAnalyticsHandlerService{})
	w := doGet(r, "/statistics/ticket-category?start_date=2026-01-01&end_date=not-a-date")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Invalid end_date format") {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestParseOptionalRangeAcceptsBothDates(t *testing.T) {
	svc := &stubAnalyticsHandlerService{category: []analyticscontract.CategoryStats{
		{Category: "咨询", Count: 3},
	}}
	r := newStatisticsUnitRouter(svc)
	// start_date 与 end_date 双合法：走解析成功的放行分支并透传给服务层。
	w := doGet(r, "/statistics/ticket-priority?start_date=2026-01-01&end_date=2026-01-31")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "咨询") {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestExportBuildRowsServiceErrorReturns500(t *testing.T) {
	svc := &stubAnalyticsHandlerService{timeRangeErr: errors.New("stats down")}
	r := newStatisticsUnitRouter(svc)
	w := doGet(r, "/statistics/export?type=time_range&from=2026-04-08&to=2026-04-09&format=csv")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Failed to export statistics") {
		t.Fatalf("body = %s", w.Body.String())
	}
}
