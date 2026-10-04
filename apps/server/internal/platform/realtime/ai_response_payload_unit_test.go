package realtime

import (
	"testing"

	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	"servify/apps/server/pkg/weknora"
)

// TestAIResponsePayloadBasics stub / legacy 服务只填基础三字段：附加键
// 全部省略，ai-response 帧形状与历史一致（向后兼容）。
func TestAIResponsePayloadBasics(t *testing.T) {
	payload := aiResponsePayload(&aidelivery.AIResponse{Content: "hi", Confidence: 0.6, Source: "ai"})
	if len(payload) != 3 {
		t.Fatalf("expected exactly 3 base keys, got %v", payload)
	}
	if payload["content"] != "hi" || payload["confidence"] != 0.6 || payload["source"] != "ai" {
		t.Fatalf("unexpected payload: %v", payload)
	}
}

// TestAIResponsePayloadEnhancedExtras 编排路径的附加输出按零值省略透出：
// 引用来源、产生方式、置信不足建议（SDK 据此渲染引用与"转人工"提示）。
func TestAIResponsePayloadEnhancedExtras(t *testing.T) {
	resp := &aidelivery.AIResponse{
		Content:       "answer",
		Confidence:    0.91,
		Source:        "ai",
		Sources:       []weknora.SearchResult{{DocumentID: "doc-1", Title: "Billing"}},
		Strategy:      "weknora",
		NextAction:    "handoff",
		HandoffReason: "low_confidence",
	}
	payload := aiResponsePayload(resp)
	if len(payload) != 7 {
		t.Fatalf("expected 7 keys with extras, got %v", payload)
	}
	sources, ok := payload["sources"].([]weknora.SearchResult)
	if !ok || len(sources) != 1 || sources[0].DocumentID != "doc-1" {
		t.Fatalf("expected sources passthrough, got %v", payload["sources"])
	}
	if payload["strategy"] != "weknora" || payload["next_action"] != "handoff" || payload["handoff_reason"] != "low_confidence" {
		t.Fatalf("unexpected extras: %v", payload)
	}
}

// TestAIResponsePayloadAnswerID 首答持久化 ID（B3-1b §5.3）非零时透出：
// 访客 widget 凭它调 POST /api/v1/ai/feedback 完成反馈闭环。
func TestAIResponsePayloadAnswerID(t *testing.T) {
	resp := &aidelivery.AIResponse{Content: "hi", AnswerID: 42}
	payload := aiResponsePayload(resp)
	if payload["answer_id"] != resp.AnswerID {
		t.Fatalf("expected answer_id passthrough, got %v", payload["answer_id"])
	}
}
