package delivery

import (
	"context"
	"errors"
	"testing"

	platformauth "servify/apps/server/internal/platform/auth"
	"servify/apps/server/internal/platform/llm"

	"github.com/sirupsen/logrus"
)

// stubQuotaMeter 配额计量 inline mock：可注入失败，记录累加流水。
type stubQuotaMeter struct {
	usage   int64
	err     error
	records []int64
}

func (m *stubQuotaMeter) Usage(context.Context, string) (int64, error) { return m.usage, m.err }

func (m *stubQuotaMeter) Add(_ context.Context, _ string, amount int64) (int64, error) {
	if m.err != nil {
		return 0, m.err
	}
	m.usage += amount
	m.records = append(m.records, amount)
	return m.usage, nil
}

// stubNext 被装饰的翻译门面 inline mock：回执固定译文，可注入 usage。
type stubNext struct {
	usage      *llm.TokenUsage
	batchCalls int
}

func (s *stubNext) Translate(_ context.Context, cmd TranslateCommand) (TranslateResult, error) {
	return TranslateResult{Text: "ok", TargetLang: cmd.TargetLang, TokenUsage: s.usage}, nil
}

func (s *stubNext) BatchTranslate(ctx context.Context, cmd BatchTranslateCommand) (BatchTranslateResult, error) {
	s.batchCalls++
	if len(cmd.Texts) == 0 {
		return BatchTranslateResult{Texts: []string{}, TargetLang: cmd.TargetLang}, nil
	}
	_, _ = s.Translate(ctx, TranslateCommand{Text: cmd.Texts[0], TargetLang: cmd.TargetLang})
	return BatchTranslateResult{Texts: []string{"ok"}, TargetLang: cmd.TargetLang}, nil
}

func TestParseQuotaUnit(t *testing.T) {
	for _, ok := range []string{"requests", "tokens"} {
		if _, err := ParseQuotaUnit(ok); err != nil {
			t.Fatalf("ParseQuotaUnit(%q) unexpected error: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "chars", "REQUESTS"} {
		if _, err := ParseQuotaUnit(bad); err == nil {
			t.Fatalf("ParseQuotaUnit(%q) expected error", bad)
		}
	}
}

func TestNewQuotaGateNilSafety(t *testing.T) {
	next := &stubNext{}
	if NewQuotaGate(nil, &stubQuotaMeter{}, 10, QuotaUnitRequests, nil) != nil {
		t.Fatal("nil next must return nil (no gate)")
	}
	if NewQuotaGate(next, nil, 10, QuotaUnitRequests, nil) != next {
		t.Fatal("nil meter must return bare next")
	}
	// logger 为 nil：回退标准 logger，正常过门。
	gate := NewQuotaGate(next, &stubQuotaMeter{}, 10, QuotaUnitRequests, nil)
	if _, err := gate.Translate(tenantCtx(), TranslateCommand{Text: "hi"}); err != nil {
		t.Fatalf("nil logger gate: %v", err)
	}
}

func tenantCtx() context.Context {
	return platformauth.ContextWithScope(context.Background(), "tenant-1", "")
}

func TestGateRequestsUnit(t *testing.T) {
	next := &stubNext{}
	meter := &stubQuotaMeter{}
	gate := NewQuotaGate(next, meter, 2, QuotaUnitRequests, logrus.StandardLogger())
	ctx := tenantCtx()

	for i := 0; i < 2; i++ {
		if _, err := gate.Translate(ctx, TranslateCommand{Text: "hi"}); err != nil {
			t.Fatalf("call #%d within quota: %v", i+1, err)
		}
	}
	if _, err := gate.Translate(ctx, TranslateCommand{Text: "hi"}); !errors.Is(err, ErrTranslationQuotaExceeded) {
		t.Fatalf("call #3 expected ErrTranslationQuotaExceeded, got %v", err)
	}
	// 被拒调用也占窗：累计 3 > limit 2。
	if meter.usage != 3 {
		t.Fatalf("meter usage = %d, want 3 (rejected call counts)", meter.usage)
	}
	// 无租户静默跳过。
	if _, err := gate.Translate(context.Background(), TranslateCommand{Text: "hi"}); err != nil {
		t.Fatalf("anonymous call must skip gate: %v", err)
	}
	// 批量面同口径：窗已耗尽同样被拒；新窗口（新计量器）下放行且一次调用 +1。
	if _, err := gate.BatchTranslate(ctx, BatchTranslateCommand{Texts: []string{"a"}, TargetLang: "en"}); !errors.Is(err, ErrTranslationQuotaExceeded) {
		t.Fatalf("batch over quota expected ErrTranslationQuotaExceeded, got %v", err)
	}
	freshGate := NewQuotaGate(next, &stubQuotaMeter{}, 1, QuotaUnitRequests, logrus.StandardLogger())
	if _, err := freshGate.BatchTranslate(ctx, BatchTranslateCommand{Texts: []string{"a"}, TargetLang: "en"}); err != nil {
		t.Fatalf("batch within fresh quota: %v", err)
	}
	if _, err := freshGate.BatchTranslate(ctx, BatchTranslateCommand{Texts: []string{"a"}, TargetLang: "en"}); !errors.Is(err, ErrTranslationQuotaExceeded) {
		t.Fatalf("second batch in window of 1 expected rejection, got %v", err)
	}
}

func TestGateRequestsUnitMeterErrorFailOpen(t *testing.T) {
	next := &stubNext{}
	meter := &stubQuotaMeter{err: errors.New("redis down")}
	gate := NewQuotaGate(next, meter, 0, QuotaUnitRequests, logrus.StandardLogger())
	res, err := gate.Translate(tenantCtx(), TranslateCommand{Text: "hi"})
	if err != nil || res.Text != "ok" {
		t.Fatalf("meter failure must fail-open, got (%+v, %v)", res, err)
	}
	if _, err := gate.BatchTranslate(tenantCtx(), BatchTranslateCommand{Texts: []string{"a"}, TargetLang: "en"}); err != nil {
		t.Fatalf("request-count batch meter failure must fail-open: %v", err)
	}
}

func TestGateTokensUnit(t *testing.T) {
	next := &stubNext{}
	meter := &stubQuotaMeter{usage: 90}
	gate := NewQuotaGate(next, meter, 100, QuotaUnitTokens, logrus.StandardLogger())
	ctx := tenantCtx()

	// 下一调用回执 20 tokens：90+20 未越限，放行并记账。
	next.usage = &llm.TokenUsage{TotalTokens: 20}
	if _, err := gate.Translate(ctx, TranslateCommand{Text: "hi"}); err != nil {
		t.Fatalf("call within token quota: %v", err)
	}
	if meter.usage != 110 || len(meter.records) != 1 || meter.records[0] != 20 {
		t.Fatalf("meter usage = %d records = %v, want 110/[20]", meter.usage, meter.records)
	}

	// 已用量 110 ≥ limit 100：后续调用被拒，且不消耗计量。
	if _, err := gate.Translate(ctx, TranslateCommand{Text: "hi"}); !errors.Is(err, ErrTranslationQuotaExceeded) {
		t.Fatalf("call over token quota expected ErrTranslationQuotaExceeded, got %v", err)
	}
	if meter.usage != 110 {
		t.Fatalf("rejected call must not consume meter, usage = %d", meter.usage)
	}

	// 回执无 usage 面（provider 未回传）：放行但不记账。
	meter.usage = 0
	next.usage = nil
	if _, err := gate.Translate(ctx, TranslateCommand{Text: "hi"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(meter.records) != 1 {
		t.Fatalf("usage-less call must not record, records = %v", meter.records)
	}

	// tokens 配额下批量产出无 usage 面，必须在调用 provider 前拒绝。
	meter.usage = 100
	if _, err := gate.BatchTranslate(ctx, BatchTranslateCommand{Texts: []string{"a"}, TargetLang: "en"}); !errors.Is(err, ErrTranslationQuotaUsageUnavailable) {
		t.Fatalf("unmeterable token batch expected ErrTranslationQuotaUsageUnavailable, got %v", err)
	}
	meter.usage = 0
	before := len(meter.records)
	if _, err := gate.BatchTranslate(ctx, BatchTranslateCommand{Texts: []string{"a"}, TargetLang: "en"}); !errors.Is(err, ErrTranslationQuotaUsageUnavailable) {
		t.Fatalf("unmeterable token batch expected ErrTranslationQuotaUsageUnavailable, got %v", err)
	}
	if len(meter.records) != before {
		t.Fatalf("batch must not record tokens (no usage surface), records = %v", meter.records)
	}
	if next.batchCalls != 0 {
		t.Fatalf("unmeterable batch reached provider %d times", next.batchCalls)
	}
	if _, err := gate.BatchTranslate(ctx, BatchTranslateCommand{Texts: []string{}, TargetLang: "en"}); err != nil {
		t.Fatalf("empty token batch should pass without usage: %v", err)
	}
	if next.batchCalls != 1 {
		t.Fatalf("empty batch should pass through once, calls = %d", next.batchCalls)
	}
}

func TestGateTokensUnitMeterErrorFailOpen(t *testing.T) {
	next := &stubNext{}
	meter := &stubQuotaMeter{err: errors.New("redis down")}
	gate := NewQuotaGate(next, meter, 0, QuotaUnitTokens, logrus.StandardLogger())
	if _, err := gate.Translate(tenantCtx(), TranslateCommand{Text: "hi"}); err != nil {
		t.Fatalf("meter failure must fail-open: %v", err)
	}
	if _, err := gate.BatchTranslate(tenantCtx(), BatchTranslateCommand{Texts: []string{"a"}, TargetLang: "en"}); !errors.Is(err, ErrTranslationQuotaUsageUnavailable) {
		t.Fatalf("unmeterable token batch must be rejected: %v", err)
	}
}

func TestGateZeroTotalTokensNotRecorded(t *testing.T) {
	next := &stubNext{}
	meter := &stubQuotaMeter{}
	gate := NewQuotaGate(next, meter, 100, QuotaUnitTokens, logrus.StandardLogger())
	next.usage = &llm.TokenUsage{TotalTokens: 0}
	if _, err := gate.Translate(tenantCtx(), TranslateCommand{Text: "hi"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(meter.records) != 0 {
		t.Fatalf("zero usage must not record, records = %v", meter.records)
	}
}

// failNext 翻译失败注入：翻译主链路报错（token 记账不得发生在失败面）。
type failNext struct{ stubNext }

func (s *failNext) Translate(_ context.Context, cmd TranslateCommand) (TranslateResult, error) {
	return TranslateResult{}, errors.New("provider boom")
}

// recordFailMeter Usage 正常、Add 失败：事后记账失败的 fail-open 面。
type recordFailMeter struct {
	stubQuotaMeter
}

func (m *recordFailMeter) Add(_ context.Context, _ string, _ int64) (int64, error) {
	return 0, errors.New("redis down on write")
}

func TestGateTokensTranslateFailureSkipsRecord(t *testing.T) {
	meter := &stubQuotaMeter{}
	gate := NewQuotaGate(&failNext{}, meter, 100, QuotaUnitTokens, logrus.StandardLogger())
	if _, err := gate.Translate(tenantCtx(), TranslateCommand{Text: "hi"}); err == nil {
		t.Fatal("translation failure must propagate")
	}
	if len(meter.records) != 0 {
		t.Fatalf("failed translation must not record, records = %v", meter.records)
	}
}

func TestGateTokensRecordFailureFailOpen(t *testing.T) {
	next := &stubNext{usage: &llm.TokenUsage{TotalTokens: 5}}
	gate := NewQuotaGate(next, &recordFailMeter{}, 100, QuotaUnitTokens, logrus.StandardLogger())
	res, err := gate.Translate(tenantCtx(), TranslateCommand{Text: "hi"})
	if err != nil || res.Text != "ok" {
		t.Fatalf("record failure must fail-open, got (%+v, %v)", res, err)
	}
}

func TestGateBatchAnonymousSkipsGate(t *testing.T) {
	next := &stubNext{}
	meter := &stubQuotaMeter{}
	gate := NewQuotaGate(next, meter, 0, QuotaUnitRequests, logrus.StandardLogger())
	res, err := gate.BatchTranslate(context.Background(), BatchTranslateCommand{Texts: []string{"a"}, TargetLang: "en"})
	if err != nil || res.Texts[0] != "ok" {
		t.Fatalf("anonymous batch must skip gate, got (%+v, %v)", res, err)
	}
	if meter.usage != 0 {
		t.Fatalf("anonymous batch must not consume meter, usage = %d", meter.usage)
	}
}

// TestNewQuotaGateWithLimitSourceNilSafety 解析源为 nil 时等价静态限额
// （无 configscope provider 的部署形态），nil next / nil meter 不装饰。
func TestNewQuotaGateWithLimitSourceNilSafety(t *testing.T) {
	next := &stubNext{}
	if NewQuotaGateWithLimitSource(nil, &stubQuotaMeter{}, 10, QuotaUnitRequests, nil, nil) != nil {
		t.Fatal("nil next must return nil (no gate)")
	}
	if got := NewQuotaGateWithLimitSource(next, nil, 10, QuotaUnitRequests, nil, nil); got != next {
		t.Fatalf("nil meter must return bare next, got %v", got)
	}
	// 已用量 50 < 静态限额 100：放行（覆盖解析器缺失不改判）。
	gate := NewQuotaGateWithLimitSource(next, &stubQuotaMeter{usage: 50}, 100, QuotaUnitTokens, nil, logrus.StandardLogger())
	if _, err := gate.Translate(tenantCtx(), TranslateCommand{Text: "hi"}); err != nil {
		t.Fatalf("nil source must fall back to static limit: %v", err)
	}
	if _, err := gate.BatchTranslate(tenantCtx(), BatchTranslateCommand{Texts: []string{"a"}, TargetLang: "en"}); !errors.Is(err, ErrTranslationQuotaUsageUnavailable) {
		t.Fatalf("nil source must not allow unmeterable token batch: %v", err)
	}
}

// TestGateLimitSourceOverride 租户覆盖限额按 ctx 解析生效（requests / tokens
// 两单位、Translate / BatchTranslate 两面），解析返回 <=0 回落静态限额。
func TestGateLimitSourceOverride(t *testing.T) {
	ctx := tenantCtx()
	// requests：静态 10、覆盖 1 → 第 2 次调用被拒（静态限额下会放行）。
	meter := &stubQuotaMeter{}
	gate := NewQuotaGateWithLimitSource(&stubNext{}, meter, 10, QuotaUnitRequests,
		func(context.Context) int64 { return 1 }, logrus.StandardLogger())
	if _, err := gate.Translate(ctx, TranslateCommand{Text: "hi"}); err != nil {
		t.Fatalf("first call within override quota: %v", err)
	}
	if _, err := gate.Translate(ctx, TranslateCommand{Text: "hi"}); !errors.Is(err, ErrTranslationQuotaExceeded) {
		t.Fatalf("second call expected ErrTranslationQuotaExceeded, got %v", err)
	}
	if _, err := gate.BatchTranslate(ctx, BatchTranslateCommand{Texts: []string{"a"}, TargetLang: "en"}); !errors.Is(err, ErrTranslationQuotaExceeded) {
		t.Fatalf("batch expected ErrTranslationQuotaExceeded, got %v", err)
	}

	// tokens：已用量 6 ≥ 覆盖 5 → 被拒且不记账。
	meter2 := &stubQuotaMeter{usage: 6}
	gate2 := NewQuotaGateWithLimitSource(&stubNext{}, meter2, 100, QuotaUnitTokens,
		func(context.Context) int64 { return 5 }, logrus.StandardLogger())
	if _, err := gate2.Translate(ctx, TranslateCommand{Text: "hi"}); !errors.Is(err, ErrTranslationQuotaExceeded) {
		t.Fatalf("tokens call expected ErrTranslationQuotaExceeded, got %v", err)
	}
	if meter2.records != nil {
		t.Fatalf("rejected call must not consume meter, records = %v", meter2.records)
	}
	if _, err := gate2.BatchTranslate(ctx, BatchTranslateCommand{Texts: []string{"a"}, TargetLang: "en"}); !errors.Is(err, ErrTranslationQuotaUsageUnavailable) {
		t.Fatalf("unmeterable tokens batch expected ErrTranslationQuotaUsageUnavailable, got %v", err)
	}

	// 解析返回 0（覆盖未设置）：回落静态 100 → 放行。
	gate3 := NewQuotaGateWithLimitSource(&stubNext{}, meter2, 100, QuotaUnitTokens,
		func(context.Context) int64 { return 0 }, logrus.StandardLogger())
	if _, err := gate3.Translate(ctx, TranslateCommand{Text: "hi"}); err != nil {
		t.Fatalf("unset override must fall back to static limit: %v", err)
	}
	if _, err := gate3.BatchTranslate(ctx, BatchTranslateCommand{Texts: []string{"a"}, TargetLang: "en"}); !errors.Is(err, ErrTranslationQuotaUsageUnavailable) {
		t.Fatalf("unset override must not allow unmeterable token batch: %v", err)
	}
}
