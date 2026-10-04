package handlers

// 覆盖率补齐（V1.0 收敛验收口径：-tags integration 覆盖率 100%）：
// 1) customer_handler 数据主体 PII 导出/删除两端点的全部分支；
// 2) knowledge_doc 来源登记/索引任务的剩余错误分支与 limit 兜底；
// 3) session_transfer 评分审计读口（含空 session_id 兜底）；
// 4) conversation_workspace 时间线只读面的未注入 503 与委托；
// 5) seams.go excelize 三个默认 hook 的函数体（真实 excelize 路径）。

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	customerapplication "servify/apps/server/internal/modules/customer/application"
	customerdelivery "servify/apps/server/internal/modules/customer/delivery"
	knowledgedelivery "servify/apps/server/internal/modules/knowledge/delivery"
	routingapplication "servify/apps/server/internal/modules/routing/application"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/xuri/excelize/v2"
)

// ---- customer：数据主体 PII 导出 / 删除 ----

type extraCustomerBoundaryService struct {
	*unitCustomerService
	export    *customerapplication.CustomerDataExport
	exportErr error
	erase     *customerapplication.CustomerDataEraseResult
	eraseErr  error
}

func (s *extraCustomerBoundaryService) ExportCustomerData(ctx context.Context, customerID uint) (*customerapplication.CustomerDataExport, error) {
	if s.exportErr != nil {
		return nil, s.exportErr
	}
	return s.export, nil
}

func (s *extraCustomerBoundaryService) EraseCustomerData(ctx context.Context, customerID uint) (*customerapplication.CustomerDataEraseResult, error) {
	if s.eraseErr != nil {
		return nil, s.eraseErr
	}
	return s.erase, nil
}

func TestExtraCustomerDataBoundaryEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	newRouter := func(svc customerdelivery.HandlerService) *gin.Engine {
		r := gin.New()
		RegisterCustomerRoutes(&r.RouterGroup, NewCustomerHandler(svc, logger))
		return r
	}
	do := func(r *gin.Engine, method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}

	svc := &extraCustomerBoundaryService{}
	r := newRouter(svc)

	// 非法/零 id → 400（parseCustomerIDParam）。
	if w := do(r, http.MethodGet, "/customers/abc/export"); w.Code != http.StatusBadRequest {
		t.Fatalf("export bad id expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if w := do(r, http.MethodPost, "/customers/0/erase-data"); w.Code != http.StatusBadRequest {
		t.Fatalf("erase zero id expected 400, got %d body=%s", w.Code, w.Body.String())
	}

	// 404：customerdelivery.ErrCustomerNotFound。
	svc.exportErr = customerdelivery.ErrCustomerNotFound
	if w := do(r, http.MethodGet, "/customers/9/export"); w.Code != http.StatusNotFound {
		t.Fatalf("export not-found expected 404, got %d body=%s", w.Code, w.Body.String())
	}
	svc.exportErr = nil

	// 500：其他错误。
	svc.eraseErr = errors.New("boom")
	w := do(r, http.MethodPost, "/customers/9/erase-data")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("erase internal expected 500, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("Failed to erase customer data")) {
		t.Fatalf("unexpected erase error body: %s", w.Body.String())
	}
	svc.eraseErr = nil

	// export 成功：Content-Disposition 头。
	svc.export = &customerapplication.CustomerDataExport{
		ExportedAt: time.Now(),
		Customer:   &models.Customer{},
	}
	w = do(r, http.MethodGet, "/customers/9/export")
	if w.Code != http.StatusOK {
		t.Fatalf("export expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	wantDisposition := "attachment; filename=customer-9-data-export.json"
	if got := w.Header().Get("Content-Disposition"); got != wantDisposition {
		t.Fatalf("Content-Disposition = %q want %q", got, wantDisposition)
	}

	// erase 成功：message 包装。
	svc.erase = &customerapplication.CustomerDataEraseResult{CustomerID: 9, MessagesHit: 3}
	w = do(r, http.MethodPost, "/customers/9/erase-data")
	if w.Code != http.StatusOK {
		t.Fatalf("erase expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("Customer data erased successfully")) {
		t.Fatalf("unexpected erase body: %s", w.Body.String())
	}
}

// ---- knowledge：来源登记 / 索引任务剩余分支 ----

type extraKnowledgeSourcesService struct {
	*unitKnowledgeService
	source          *models.KnowledgeSource
	listSourcesErr  error
	createSourceErr error
	deleteSourceErr error
	jobs            []knowledgedelivery.IndexJobDTO
	jobsErr         error
	indexErr        error
	retryErr        error
}

func (s *extraKnowledgeSourcesService) ListSources(ctx context.Context, sourceType string) ([]models.KnowledgeSource, error) {
	if s.listSourcesErr != nil {
		return nil, s.listSourcesErr
	}
	return []models.KnowledgeSource{*s.source}, nil
}

func (s *extraKnowledgeSourcesService) CreateSource(ctx context.Context, req *knowledgedelivery.KnowledgeSourceCreateRequest) (*models.KnowledgeSource, error) {
	if s.createSourceErr != nil {
		return nil, s.createSourceErr
	}
	return s.source, nil
}

func (s *extraKnowledgeSourcesService) DeleteSource(ctx context.Context, id uint) error {
	return s.deleteSourceErr
}

func (s *extraKnowledgeSourcesService) ListIndexJobs(ctx context.Context, documentID string, limit int) ([]knowledgedelivery.IndexJobDTO, error) {
	if s.jobsErr != nil {
		return nil, s.jobsErr
	}
	return s.jobs, nil
}

func (s *extraKnowledgeSourcesService) IndexDocument(ctx context.Context, documentID string) (*knowledgedelivery.IndexJobResult, error) {
	if s.indexErr != nil {
		return nil, s.indexErr
	}
	return &knowledgedelivery.IndexJobResult{JobID: "j1", Status: "done"}, nil
}

func (s *extraKnowledgeSourcesService) RetryIndexJob(ctx context.Context, jobID string) (*knowledgedelivery.IndexJobResult, error) {
	if s.retryErr != nil {
		return nil, s.retryErr
	}
	return &knowledgedelivery.IndexJobResult{JobID: jobID, Status: "done"}, nil
}

func TestExtraKnowledgeSourcesAndIndexJobsBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &extraKnowledgeSourcesService{
		source: &models.KnowledgeSource{Name: "src", Type: "markdown"},
		jobs:   []knowledgedelivery.IndexJobDTO{{ID: "j1"}},
	}
	r := gin.New()
	RegisterKnowledgeDocRoutes(r.Group("/api"), NewKnowledgeDocHandler(svc))
	do := func(method, path string, body io.Reader) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, body)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		r.ServeHTTP(w, req)
		return w
	}

	// ListSources 错误 → 400。
	svc.listSourcesErr = errors.New("boom")
	if w := do(http.MethodGet, "/api/knowledge-docs/sources?type=markdown", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("list sources error expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	svc.listSourcesErr = nil

	// CreateSource：非法 JSON → 400；成功 → 201。
	if w := do(http.MethodPost, "/api/knowledge-docs/sources", bytes.NewReader([]byte("{bad"))); w.Code != http.StatusBadRequest {
		t.Fatalf("create source bad json expected 400, got %d", w.Code)
	}
	body := bytes.NewReader([]byte(`{"name":"src","type":"markdown"}`))
	if w := do(http.MethodPost, "/api/knowledge-docs/sources", body); w.Code != http.StatusCreated {
		t.Fatalf("create source expected 201, got %d body=%s", w.Code, w.Body.String())
	}

	// DeleteSource：非法 id → 400；成功 → 200。
	if w := do(http.MethodDelete, "/api/knowledge-docs/sources/abc", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("delete source bad id expected 400, got %d", w.Code)
	}
	if w := do(http.MethodDelete, "/api/knowledge-docs/sources/1", nil); w.Code != http.StatusOK {
		t.Fatalf("delete source expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	// ListIndexJobs：非法 limit 兜底 20 → 200；服务错误 → 400。
	if w := do(http.MethodGet, "/api/knowledge-docs/7/index-jobs?limit=abc", nil); w.Code != http.StatusOK {
		t.Fatalf("index jobs bad limit expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	svc.jobsErr = errors.New("boom")
	if w := do(http.MethodGet, "/api/knowledge-docs/7/index-jobs?limit=5", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("index jobs error expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	svc.jobsErr = nil

	// IndexDocument 错误 → 400。
	svc.indexErr = errors.New("boom")
	if w := do(http.MethodPost, "/api/knowledge-docs/7/index-jobs", bytes.NewReader([]byte("{}"))); w.Code != http.StatusBadRequest {
		t.Fatalf("index document error expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	svc.indexErr = nil

	// RetryIndexJob 错误 → 400。
	svc.retryErr = errors.New("boom")
	if w := do(http.MethodPost, "/api/knowledge-docs/index-jobs/j1/retry", bytes.NewReader([]byte("{}"))); w.Code != http.StatusBadRequest {
		t.Fatalf("retry index job error expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	svc.retryErr = nil
}

// ---- session transfer：评分审计读口 ----

type extraTransferScoringService struct {
	*unitTransferService
	items []routingapplication.RoutingAssignmentDTO
	err   error
}

func (s *extraTransferScoringService) ListRoutingAssignments(ctx context.Context, sessionID string, limit int) ([]routingapplication.RoutingAssignmentDTO, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.items, nil
}

func TestExtraRoutingAssignmentScoring(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	svc := &extraTransferScoringService{}
	h := NewSessionTransferHandler(svc, logger)

	// 空 session_id → 400（路由参数缺失时经 CreateTestContext 驱动）。
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/session-transfer/scoring", nil)
	h.GetRoutingAssignmentScoring(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing session id expected 400, got %d body=%s", w.Code, w.Body.String())
	}

	r := gin.New()
	RegisterSessionTransferRoutes(&r.RouterGroup, h)
	do := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}

	// 成功：limit=7 合法解析，返回条目与 count。
	svc.items = []routingapplication.RoutingAssignmentDTO{{
		SessionID:  "s1",
		ToAgentID:  7,
		TotalScore: 0.8,
		Factors:    map[string]float64{"load": 0.8},
		Reasons:    []string{"least_load"},
		Strategy:   "least_load",
		AssignedAt: time.Now(),
	}}
	w = do("/session-transfer/scoring/s1?limit=7")
	if w.Code != http.StatusOK {
		t.Fatalf("scoring expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"count":1`)) {
		t.Fatalf("expected count=1 in body: %s", w.Body.String())
	}

	// 服务错误 → 500。
	svc.err = errors.New("boom")
	w = do("/session-transfer/scoring/s1")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("scoring error expected 500, got %d body=%s", w.Code, w.Body.String())
	}
	svc.err = nil
}

// ---- conversation workspace：时间线只读面 ----

type extraTimelineReaderStub struct{ called bool }

func (s *extraTimelineReaderStub) HandleListTimeline(c *gin.Context) {
	s.called = true
	c.JSON(http.StatusOK, gin.H{"data": []int{}})
}

func TestExtraConversationTimelineFallbackAndDelegate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewConversationWorkspaceHandler(nil, nil, nil)
	r := gin.New()
	r.GET("/omni/sessions/:id/timeline", h.GetTimeline)

	// 未注入 timeline → 503 兜底。
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/omni/sessions/s9/timeline", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("timeline fallback expected 503, got %d body=%s", w.Code, w.Body.String())
	}

	// WithTimeline 返回自身并委托注入的只读面。
	stub := &extraTimelineReaderStub{}
	if got := h.WithTimeline(stub); got != h {
		t.Fatalf("WithTimeline should return the handler itself")
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/omni/sessions/s9/timeline", nil))
	if w.Code != http.StatusOK || !stub.called {
		t.Fatalf("timeline delegate expected 200 and stub hit, got %d called=%v body=%s", w.Code, stub.called, w.Body.String())
	}
}

// ---- seams：excelize 默认 hook 的真实函数体 ----

func TestExtraExcelizeDefaultSeams(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := hookExcelizeSetSheetRow(f, "Sheet1", "A1", &[]interface{}{"v"}); err != nil {
		t.Fatalf("default SetSheetRow seam: %v", err)
	}
	cell, err := hookExcelizeCellName(2, 3)
	if err != nil {
		t.Fatalf("default CoordinatesToCellName seam: %v", err)
	}
	if cell != "B3" {
		t.Fatalf("CoordinatesToCellName(2,3) = %q want B3", cell)
	}
	var buf bytes.Buffer
	if err := hookExcelizeWrite(f, &buf); err != nil {
		t.Fatalf("default Write seam: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected xlsx bytes from default Write seam")
	}
}
