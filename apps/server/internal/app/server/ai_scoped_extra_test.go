package server

// 覆盖补充（B3-1b 首答持久化旁路）：REST 成功应答触发 RecordResponse、
// 流式透传实例报错原样上抛（WS hub 据此回退非流式路径）。

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/config"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"

	"github.com/sirupsen/logrus"
)

func TestScopedAIHandlerProcessQueryRecordsAnswer(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Knowledge.Provider = "pgvector"
	// pgvector 直通 startup 实例：应答成功且结果为 *AIResponse 时走
	// RecordResponse 旁路（answerStore 未注入时旁路自灭，不阻塞主链路）。
	handler := &scopedAIHandlerService{cfg: cfg, logger: logrus.New(), startup: stubRuntimeFallback{}}
	result, err := handler.ProcessQuery(context.Background(), "hello", "session-rec")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	resp, ok := result.(*aidelivery.AIResponse)
	if !ok || resp.Content != "fallback" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

// errStreamRuntimeFallback 具备流式能力但每次报错，驱动透传错误分支。
type errStreamRuntimeFallback struct{ stubRuntimeFallback }

func (errStreamRuntimeFallback) ProcessQueryStream(context.Context, string, string) (<-chan aidelivery.AIStreamEvent, error) {
	return nil, errors.New("stream down")
}

func TestScopedAIRuntimeProcessQueryStreamErrorPropagates(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Knowledge.Provider = "pgvector"
	svc := &scopedAIRuntimeService{cfg: cfg, fallback: errStreamRuntimeFallback{}}
	_, err := svc.ProcessQueryStream(context.Background(), "q", "s")
	if err == nil || err.Error() != "stream down" {
		t.Fatalf("stream error must propagate, got %v", err)
	}
}
