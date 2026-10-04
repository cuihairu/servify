package handlers

// 本文件通过 seams.go 的包级 seam 驱动 CSV / XLSX 导出与 OIDC 随机数
// 路径的错误分支，以及满意度列表的合法日期参数。

import (
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"servify/apps/server/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func httptestGetHSCOV(r *gin.Engine, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	r.ServeHTTP(w, req)
	return w
}

// hscovFailingWriter 对 Write 一律失败，模拟底层 writer 错误。
type hscovFailingWriter struct{}

func (hscovFailingWriter) Write(p []byte) (int, error) { return 0, errors.New("boom: writer failed") }

// hscovSetSeam 替换一个包级变量并在测试结束后还原。
func hscovSetSeam[T any](t *testing.T, slot *T, value T) {
	t.Helper()
	old := *slot
	*slot = value
	t.Cleanup(func() { *slot = old })
}

// hscovFailingCSVSeam 让 CSV 导出忽略真实目标，写入必然失败的 writer。
func hscovFailingCSVSeam(t *testing.T) {
	t.Helper()
	hscovSetSeam(t, &newCSVWriter, func(io.Writer) *csv.Writer {
		return csv.NewWriter(hscovFailingWriter{})
	})
}

// --- audit ExportCSV：行写入错误 + Flush 错误 ---

func TestHSCOVAuditExportCSVWriteErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	bigItem := models.AuditLog{ID: 1, Action: "a", ResourceType: "t", ResourceID: "r",
		PrincipalKind: "agent", Success: true, StatusCode: 200, UserAgent: strings.Repeat("u", 6000)}

	t.Run("row write error surfaces 500", func(t *testing.T) {
		hscovFailingCSVSeam(t)
		svc := &stubAuditQueryService{items: []models.AuditLog{bigItem}, total: 1}
		r := gin.New()
		r.GET("/audit/logs/export", NewAuditHandler(svc).ExportCSV)
		w := httptestGetHSCOV(r, "/audit/logs/export")
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Failed to write csv") {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("flush error surfaces 500 via writer.Error", func(t *testing.T) {
		hscovFailingCSVSeam(t)
		svc := &stubAuditQueryService{items: []models.AuditLog{{ID: 2, Action: "small"}}, total: 1}
		r := gin.New()
		r.GET("/audit/logs/export", NewAuditHandler(svc).ExportCSV)
		w := httptestGetHSCOV(r, "/audit/logs/export")
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Failed to write csv") {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})
}

// --- tickets ExportTicketsCSV：行写入错误 + Flush 错误 ---

func TestHSCOVTicketsExportCSVWriteErrors(t *testing.T) {
	bigTicket := *dxcExportTicketFixture()
	bigTicket.Title = strings.Repeat("T", 6000)

	t.Run("row write error surfaces 500", func(t *testing.T) {
		hscovFailingCSVSeam(t)
		r := dxcExportRouter(dxcExportSvc(nil, []models.Ticket{bigTicket}))
		w := dxcDo(r, http.MethodGet, "/tickets/export", "")
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Failed to write csv") {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("flush error surfaces 500 via writer.Error", func(t *testing.T) {
		hscovFailingCSVSeam(t)
		r := dxcExportRouter(dxcExportSvc(nil, []models.Ticket{*dxcExportTicketFixture()}))
		w := dxcDo(r, http.MethodGet, "/tickets/export", "")
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Failed to write csv") {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})
}

// --- OIDC Start：state/nonce 随机数失败 ---

func TestHSCOVOIDCStartRandFailures(t *testing.T) {
	// failPlan[i] 控制 i-1 次调用是否失败：Start 依次取 state、nonce。
	failPlan := []bool{true, false}
	call := 0
	hscovSetSeam(t, &oidcRandRead, func(b []byte) (int, error) {
		shouldFail := failPlan[min(call, len(failPlan)-1)]
		call++
		if shouldFail {
			return 0, errors.New("boom: rand read")
		}
		for i := range b {
			b[i] = byte(call)
		}
		return len(b), nil
	})
	h, _ := newOIDCHandler(t, nil)

	t.Run("state rand failure", func(t *testing.T) {
		call, failPlan[0], failPlan[1] = 0, true, false
		w := performOIDC(t, h, "/start", nil)
		if w.Code != http.StatusFound || !strings.Contains(w.Header().Get("Location"), "error=oidc_failed") {
			t.Fatalf("code=%d location=%q", w.Code, w.Header().Get("Location"))
		}
	})

	t.Run("nonce rand failure", func(t *testing.T) {
		call, failPlan[0], failPlan[1] = 0, false, true
		w := performOIDC(t, h, "/start", nil)
		if w.Code != http.StatusFound || !strings.Contains(w.Header().Get("Location"), "error=oidc_failed") {
			t.Fatalf("code=%d location=%q", w.Code, w.Header().Get("Location"))
		}
	})
}

// --- satisfaction ListSatisfactions：日期参数格式互斥（绑定 RFC3339 vs 手工解析 YYYY-MM-DD）---

func TestHSCOVSatisfactionListDateFormatMutualExclusion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	r := gin.New()
	RegisterSatisfactionRoutes(&r.RouterGroup, NewSatisfactionHandler(&unitSatisfactionService{
		sats: []models.CustomerSatisfaction{{ID: 1}}, total: 1,
	}, logger))

	t.Run("YYYY-MM-DD rejected by gin binding before handler", func(t *testing.T) {
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions?date_from=2026-01-01", "")
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "Invalid query parameters") {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("RFC3339 binds but is rejected by handler date guard", func(t *testing.T) {
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions?date_from=2026-01-01T00:00:00Z&date_to=2026-01-31T00:00:00Z", "")
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "Invalid date_from format") {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("RFC3339 date_to rejected by handler date guard", func(t *testing.T) {
		w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions?date_to=2026-01-31T00:00:00Z", "")
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "Invalid date_to format") {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})
}
