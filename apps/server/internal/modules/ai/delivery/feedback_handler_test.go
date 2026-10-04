//go:build integration
// +build integration

package delivery

// V1.0 收敛 B3-1b：反馈与检索分析 HTTP 面的集成行为——落库、访客会话绑定、
// 聚合读口。主体上下文（principal_kind/session_id/username）由认证中间件
// 写入 gin context，测试用桩中间件模拟同款 key。

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	aiapp "servify/apps/server/internal/modules/ai/application"
	aiinfra "servify/apps/server/internal/modules/ai/infra"

	"github.com/gin-gonic/gin"
)

// principalStubMiddleware 模拟认证中间件写入的主体上下文。
func principalStubMiddleware(principalKind, sessionID, username string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("principal_kind", principalKind)
		c.Set("session_id", sessionID)
		c.Set("username", username)
		c.Next()
	}
}

type feedbackFixture struct {
	agentRouter   *gin.Engine
	visitorRouter *gin.Engine // end_user 主体，token 绑定 conv-9
	store         AnswerStore
}

func newFeedbackFixture(t *testing.T) *feedbackFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := newAnswerRecordingDB(t)
	service := aiapp.NewAnswerFeedbackService(
		aiinfra.NewGormAnswerRepository(db),
		aiinfra.NewGormAnswerFeedbackRepository(db),
	)
	handler := NewAnswerFeedbackHandler(service)

	agentRouter := gin.New()
	agentRouter.Use(principalStubMiddleware("agent", "", "agent-alice"))
	RegisterAnswerFeedbackRoutes(agentRouter.Group("/api/v1"), handler)
	RegisterAIRetrievalAnalyticsRoutes(agentRouter.Group("/api/v1"), handler)

	visitorRouter := gin.New()
	visitorRouter.Use(principalStubMiddleware("end_user", "conv-9", ""))
	RegisterAnswerFeedbackRoutes(visitorRouter.Group("/api/v1"), handler)

	return &feedbackFixture{agentRouter: agentRouter, visitorRouter: visitorRouter, store: NewGormAnswerStore(db)}
}

// seedAnswer 经记录路径落一条答案并回填 answer_id。
func (f *feedbackFixture) seedAnswer(query, session string) uint {
	resp := &AIResponse{Content: "7 天无理由", Confidence: 0.9, Strategy: "weknora"}
	RecordResponse(context.Background(), f.store, query, session, resp)
	return resp.AnswerID
}

func (f *feedbackFixture) post(r *gin.Engine, path string, body map[string]interface{}) (int, []byte) {
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func (f *feedbackFixture) get(r *gin.Engine, path string) (int, []byte) {
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func TestFeedbackEndpointAgentSubmission(t *testing.T) {
	f := newFeedbackFixture(t)
	answerID := f.seedAnswer("退款政策是什么", "conv-1")

	code, body := f.post(f.agentRouter, "/api/v1/ai/feedback", map[string]interface{}{
		"answer_id": answerID, "helpful": true, "comment": "很有用",
	})
	if code != http.StatusOK {
		t.Fatalf("agent feedback status=%d body=%s", code, body)
	}
	var ack map[string]interface{}
	_ = json.Unmarshal(body, &ack)
	if ack["id"] == nil || ack["answer_id"] == nil {
		t.Fatalf("ack = %s", body)
	}

	// 不存在的答案 → 400。
	if code, _ = f.post(f.agentRouter, "/api/v1/ai/feedback", map[string]interface{}{"answer_id": 99999}); code != http.StatusBadRequest {
		t.Fatalf("unknown answer status=%d want 400", code)
	}
}

func TestFeedbackEndpointVisitorSessionBinding(t *testing.T) {
	f := newFeedbackFixture(t)
	own := f.seedAnswer("怎么查快递", "conv-9")
	other := f.seedAnswer("退款政策是什么", "conv-1")

	// 自己会话内的答案 → 放行。
	if code, body := f.post(f.visitorRouter, "/api/v1/ai/feedback", map[string]interface{}{
		"answer_id": own, "helpful": true,
	}); code != http.StatusOK {
		t.Fatalf("visitor own-session feedback status=%d body=%s", code, body)
	}
	// 跨会话答案 → 拒绝（绑定校验）。
	if code, _ := f.post(f.visitorRouter, "/api/v1/ai/feedback", map[string]interface{}{
		"answer_id": other, "helpful": false,
	}); code != http.StatusBadRequest {
		t.Fatalf("cross-session feedback status=%d want 400", code)
	}
}

func TestRetrievalAnalyticsEndpoint(t *testing.T) {
	f := newFeedbackFixture(t)
	// 两问一答 + 一条零命中问题 + 一条反馈。
	f.seedAnswer("退款政策是什么", "conv-1")
	lowID := f.seedAnswer("怎么开发票", "conv-2")
	_ = lowID
	f.post(f.agentRouter, "/api/v1/ai/feedback", map[string]interface{}{"answer_id": lowID, "helpful": false})

	code, body := f.get(f.agentRouter, "/api/v1/ai/retrieval-analytics?days=7&limit=10")
	if code != http.StatusOK {
		t.Fatalf("analytics status=%d body=%s", code, body)
	}
	var out struct {
		WindowDays      int     `json:"window_days"`
		TotalAnswers    int64   `json:"total_answers"`
		NoHitAnswers    int64   `json:"no_hit_answers"`
		AvgConfidence   float64 `json:"avg_confidence"`
		NotHelpfulCount int64   `json:"not_helpful_count"`
		NoHitQuestions  []struct {
			Query string `json:"query"`
		} `json:"no_hit_questions"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, body)
	}
	if out.WindowDays != 7 || out.TotalAnswers != 2 || out.NoHitAnswers != 2 {
		t.Fatalf("totals = %+v", out)
	}
	if out.AvgConfidence < 0.89 || out.AvgConfidence > 0.91 {
		t.Fatalf("avg confidence = %f want ~0.9", out.AvgConfidence)
	}
	if out.NotHelpfulCount != 1 {
		t.Fatalf("not helpful = %d want 1", out.NotHelpfulCount)
	}
	if len(out.NoHitQuestions) != 2 {
		t.Fatalf("no hit questions = %+v", out.NoHitQuestions)
	}
}
