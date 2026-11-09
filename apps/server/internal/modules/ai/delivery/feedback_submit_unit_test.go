package delivery

// SubmitFeedback HTTP 面单元覆盖（同面集成版在 feedback_handler_test.go，
// 带 integration tag 不进默认门禁）：agent 主体成功落库、访客主体无用户名
// 时以会话标识留痕、未知答案 400。sqlite 内存库自足，无 build tag。

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func newSubmitFeedbackFixture(t *testing.T, principalKind, sessionID, username string) (*gin.Engine, func(query, session string) uint) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := newAnswerRecordingDB(t)
	handler := NewAnswerFeedbackHandler(newExtraFeedbackService(db))
	router := gin.New()
	router.Use(extraPrincipalStub(principalKind, sessionID, username))
	RegisterAnswerFeedbackRoutes(router.Group("/api/v1"), handler)
	store := NewGormAnswerStore(db)
	return router, func(query, session string) uint {
		resp := &AIResponse{Content: "7 天无理由", Confidence: 0.9, Strategy: "weknora"}
		RecordResponse(context.Background(), store, query, session, resp)
		return resp.AnswerID
	}
}

func extraPost(t *testing.T, router *gin.Engine, path string, body map[string]interface{}) (int, []byte) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func TestSubmitFeedbackAgentPersistsAndAcks(t *testing.T) {
	router, seed := newSubmitFeedbackFixture(t, "agent", "", "agent-alice")
	answerID := seed("退款政策是什么", "conv-1")

	code, body := extraPost(t, router, "/api/v1/ai/feedback", map[string]interface{}{
		"answer_id": answerID, "helpful": true, "comment": "很有用",
	})
	if code != http.StatusOK {
		t.Fatalf("agent feedback status=%d body=%s want 200", code, body)
	}
	var ack map[string]interface{}
	if err := json.Unmarshal(body, &ack); err != nil {
		t.Fatalf("unmarshal ack: %v body=%s", err, body)
	}
	if ack["id"] == nil || ack["answer_id"] == nil {
		t.Fatalf("ack = %s", body)
	}
}

func TestSubmitFeedbackVisitorFallsBackToSessionIdentity(t *testing.T) {
	router, seed := newSubmitFeedbackFixture(t, "end_user", "conv-9", "")
	answerID := seed("怎么查快递", "conv-9")

	code, body := extraPost(t, router, "/api/v1/ai/feedback", map[string]interface{}{
		"answer_id": answerID, "helpful": true,
	})
	if code != http.StatusOK {
		t.Fatalf("visitor feedback status=%d body=%s want 200", code, body)
	}
}

func TestSubmitFeedbackUnknownAnswerReturns400(t *testing.T) {
	router, seed := newSubmitFeedbackFixture(t, "agent", "", "agent-alice")
	answerID := seed("退款政策是什么", "conv-1")

	if code, body := extraPost(t, router, "/api/v1/ai/feedback", map[string]interface{}{
		"answer_id": answerID + 99999, "helpful": false,
	}); code != http.StatusBadRequest {
		t.Fatalf("unknown answer status=%d body=%s want 400", code, body)
	}
}
