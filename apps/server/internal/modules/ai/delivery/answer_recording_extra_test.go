package delivery

// 覆盖首答记录路径与反馈/检索分析 HTTP 面的防御分支：nil db 装配、来源序列
// 化与落库失败（记录是旁路，静默不阻塞）、流式 nil store 透传、反馈端点非法
// JSON、分析端点参数回落与聚合查询失败。sqlite 内存库自足，无 build tag。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aiapp "servify/apps/server/internal/modules/ai/application"
	aidomain "servify/apps/server/internal/modules/ai/domain"
	aiinfra "servify/apps/server/internal/modules/ai/infra"
	"servify/apps/server/pkg/weknora"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestNewGormAnswerStoreNilDBReturnsNil(t *testing.T) {
	if store := NewGormAnswerStore(nil); store != nil {
		t.Fatalf("nil db must yield nil store, got %T", store)
	}
}

// TestRecordResponseMarshalFailureIsSilent 经 seam 注入序列化失败：落库是旁
// 路观测，RecordAnswer 错误上抛、RecordResponse 记日志后静默返回，不回填
// answer_id 也不落行。
func TestRecordResponseMarshalFailureIsSilent(t *testing.T) {
	db := newAnswerRecordingDB(t)
	store := NewGormAnswerStore(db)
	orig := marshalSources
	marshalSources = func([]aidomain.SourceSnapshot) (string, error) {
		return "", errors.New("marshal boom")
	}
	defer func() { marshalSources = orig }()

	resp := &AIResponse{
		Content: "7 天无理由退款",
		Sources: []weknora.SearchResult{{DocumentID: "d1", Title: "退款政策"}},
	}
	RecordResponse(context.Background(), store, "q", "s", resp)
	if resp.AnswerID != 0 {
		t.Fatalf("failed recording must not stamp id, got %d", resp.AnswerID)
	}
	var total int64
	if err := db.Model(&aidomain.AIAnswer{}).Count(&total).Error; err != nil {
		t.Fatalf("count answers: %v", err)
	}
	if total != 0 {
		t.Fatalf("failed recording must not persist rows, got %d", total)
	}
}

// TestRecordAnswerCreateFailure 关闭底库驱动 repo.Create 失败：RecordAnswer
// 错误上抛，RecordResponse 静默不回填。
func TestRecordAnswerCreateFailure(t *testing.T) {
	db := newAnswerRecordingDB(t)
	store := NewGormAnswerStore(db)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	if _, err := store.RecordAnswer(context.Background(), AnswerRecord{SessionID: "s"}); err == nil {
		t.Fatal("expected create failure after db close")
	}

	resp := &AIResponse{Content: "x"}
	RecordResponse(context.Background(), store, "q", "s", resp)
	if resp.AnswerID != 0 {
		t.Fatalf("failed recording must not stamp id, got %d", resp.AnswerID)
	}
}

func TestRecordingStreamChanNilStorePassthrough(t *testing.T) {
	in := make(chan AIStreamEvent, 1)
	in <- AIStreamEvent{ContentDelta: "增量"}
	out := RecordingStreamChan(context.Background(), nil, "q", "s", in)
	event, ok := <-out
	if !ok || event.ContentDelta != "增量" || event.Done {
		t.Fatalf("nil store must pass events through unchanged, got %+v (ok=%v)", event, ok)
	}
}

// --- 反馈 / 检索分析 HTTP 面（无 tag 自足装配） ---

// extraPrincipalStub 模拟认证中间件写入的主体上下文（与 integration 侧
// principalStubMiddleware 同款 key，独立命名避免双 tag 合编冲突）。
func extraPrincipalStub(principalKind, sessionID, username string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("principal_kind", principalKind)
		c.Set("session_id", sessionID)
		c.Set("username", username)
		c.Next()
	}
}

func newExtraFeedbackRouter(t *testing.T, db *gorm.DB) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler := NewAnswerFeedbackHandler(newExtraFeedbackService(db))
	router := gin.New()
	router.Use(extraPrincipalStub("agent", "", "agent-alice"))
	RegisterAnswerFeedbackRoutes(router.Group("/api/v1"), handler)
	RegisterAIRetrievalAnalyticsRoutes(router.Group("/api/v1"), handler)
	return router
}

// newExtraFeedbackService 以真实仓储装配反馈应用服务（聚合读口同对象实现）。
func newExtraFeedbackService(db *gorm.DB) *aiapp.AnswerFeedbackService {
	return aiapp.NewAnswerFeedbackService(
		aiinfra.NewGormAnswerRepository(db),
		aiinfra.NewGormAnswerFeedbackRepository(db),
	)
}

func extraGet(t *testing.T, router *gin.Engine, path string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func TestSubmitFeedbackInvalidJSONReturns400(t *testing.T) {
	router := newExtraFeedbackRouter(t, newAnswerRecordingDB(t))
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/feedback", bytes.NewReader([]byte("{not-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid json status=%d body=%s want 400", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid request") {
		t.Fatalf("body = %s", w.Body.String())
	}
}

// TestRetrievalAnalyticsInvalidParamsFallBack 非法 days/limit 回落默认窗口
// （7/10）而不是报错。
func TestRetrievalAnalyticsInvalidParamsFallBack(t *testing.T) {
	router := newExtraFeedbackRouter(t, newAnswerRecordingDB(t))
	code, body := extraGet(t, router, "/api/v1/ai/retrieval-analytics?days=abc&limit=xyz")
	if code != http.StatusOK {
		t.Fatalf("analytics status=%d body=%s", code, body)
	}
	var out struct {
		WindowDays int `json:"window_days"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, body)
	}
	if out.WindowDays != 7 {
		t.Fatalf("window_days = %d want default 7", out.WindowDays)
	}
}

// TestRetrievalAnalyticsServiceErrorReturns500 关闭底库使聚合查询失败：
// 读口报 500 而不是降级空聚合。
func TestRetrievalAnalyticsServiceErrorReturns500(t *testing.T) {
	db := newAnswerRecordingDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	router := newExtraFeedbackRouter(t, db)
	code, body := extraGet(t, router, "/api/v1/ai/retrieval-analytics?days=7&limit=10")
	if code != http.StatusInternalServerError {
		t.Fatalf("analytics status=%d body=%s want 500", code, body)
	}
}
