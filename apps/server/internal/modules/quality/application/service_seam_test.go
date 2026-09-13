package application

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// panicCaptureHook 捕获 logrus 日志条目，断言规则 panic 回调内容。
type panicCaptureHook struct {
	entries []*logrus.Entry
}

func (h *panicCaptureHook) Fire(e *logrus.Entry) error {
	h.entries = append(h.entries, e)
	return nil
}

func (h *panicCaptureHook) Levels() []logrus.Level { return logrus.AllLevels }

func TestNewQualityServiceLogsRulePanicViaEngineSeam(t *testing.T) {
	hook := &panicCaptureHook{}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	logger.AddHook(hook)

	orig := newRuleEngine
	newRuleEngine = func(cfg RuleConfig, onPanic func(string, any)) *RuleEngine {
		// 构造期触发一次 panic 回调，覆盖 NewQualityService 注入的日志闭包。
		onPanic("panicky_rule", errors.New("boom"))
		return NewRuleEngine(cfg, onPanic)
	}
	defer func() { newRuleEngine = orig }()

	svc := NewQualityService(nil, nil, ServiceConfig{}, logger)
	if svc == nil || svc.engine == nil {
		t.Fatalf("expected constructed service with rule engine")
	}
	if len(hook.entries) != 1 {
		t.Fatalf("expected exactly 1 panic log entry, got %d", len(hook.entries))
	}
	entry := hook.entries[0]
	if entry.Level != logrus.ErrorLevel {
		t.Fatalf("expected error level entry, got %s", entry.Level)
	}
	if !strings.Contains(entry.Message, "quality rule panic") || !strings.Contains(entry.Message, "boom") {
		t.Fatalf("unexpected log message: %q", entry.Message)
	}
	if entry.Data["rule"] != "panicky_rule" {
		t.Fatalf("expected rule field panicky_rule, got %v", entry.Data["rule"])
	}
}

func TestScoreAndPersistMarshalDimensionsFailure(t *testing.T) {
	orig := marshalDimensions
	marshalDimensions = func(v interface{}) ([]byte, error) { return nil, errors.New("boom") }
	defer func() { marshalDimensions = orig }()

	// repo 置 nil：marshal 失败必须在触达仓储之前返回。
	svc := &QualityService{scorer: goodScorer()}
	err := svc.scoreAndPersist(context.Background(), "sess-1", nil, 0, time.Now())
	if err == nil || !strings.Contains(err.Error(), "marshal dimensions") {
		t.Fatalf("expected marshal dimensions error, got %v", err)
	}
}
