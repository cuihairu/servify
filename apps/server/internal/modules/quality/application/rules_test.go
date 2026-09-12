package application

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

func ruleMsg(sender, content string, at time.Time) models.Message {
	return models.Message{Sender: sender, Content: content, CreatedAt: at}
}

func TestBannedWordsRule(t *testing.T) {
	base := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	rule := &BannedWordsRule{words: lowerAll([]string{"Refund"})}

	t.Run("staff 消息命中且大小写不敏感", func(t *testing.T) {
		vs := rule.Evaluate(context.Background(), RuleInput{Messages: []models.Message{
			ruleMsg("agent", "我们提供 REFUND保证 服务", base),
		}})
		if len(vs) != 1 {
			t.Fatalf("want 1 violation, got %d", len(vs))
		}
		v := vs[0]
		if v.Rule != "banned_words" || v.Severity != SeverityHigh || len(v.MessageIDs) != 1 {
			t.Fatalf("unexpected violation: %+v", v)
		}
		if v.Snippet == "" {
			t.Fatal("snippet must be captured")
		}
	})

	t.Run("user 消息不扫描", func(t *testing.T) {
		vs := rule.Evaluate(context.Background(), RuleInput{Messages: []models.Message{
			ruleMsg("user", "refund refund", base),
		}})
		if len(vs) != 0 {
			t.Fatalf("user messages must not be scanned: %+v", vs)
		}
	})

	t.Run("同一条消息只记一次", func(t *testing.T) {
		rule := &BannedWordsRule{words: []string{"a", "b"}}
		vs := rule.Evaluate(context.Background(), RuleInput{Messages: []models.Message{
			ruleMsg("ai", "a b 都命中", base),
		}})
		if len(vs) != 1 {
			t.Fatalf("want 1 violation per message, got %d", len(vs))
		}
	})

	t.Run("空词表不产出", func(t *testing.T) {
		if vs := (&BannedWordsRule{}).Evaluate(context.Background(), RuleInput{Messages: []models.Message{
			ruleMsg("agent", "anything", base),
		}}); len(vs) != 0 {
			t.Fatalf("empty wordlist must yield nothing: %+v", vs)
		}
	})
}

func TestResponseTimeoutRule(t *testing.T) {
	base := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)

	t.Run("超时记 medium", func(t *testing.T) {
		rule := &ResponseTimeoutRule{threshold: 60}
		vs := rule.Evaluate(context.Background(), RuleInput{Messages: []models.Message{
			ruleMsg("user", "在吗", base),
			ruleMsg("agent", "在的", base.Add(2*time.Minute)),
		}})
		if len(vs) != 1 || vs[0].Severity != SeverityMedium {
			t.Fatalf("unexpected violations: %+v", vs)
		}
		if len(vs[0].MessageIDs) != 2 {
			t.Fatalf("must reference both messages: %+v", vs[0])
		}
	})

	t.Run("阈值内不记", func(t *testing.T) {
		rule := &ResponseTimeoutRule{threshold: 120}
		vs := rule.Evaluate(context.Background(), RuleInput{Messages: []models.Message{
			ruleMsg("user", "在吗", base),
			ruleMsg("ai", "在的", base.Add(time.Minute)),
		}})
		if len(vs) != 0 {
			t.Fatalf("within threshold must pass: %+v", vs)
		}
	})

	t.Run("无后续回复不评", func(t *testing.T) {
		rule := &ResponseTimeoutRule{threshold: 1}
		vs := rule.Evaluate(context.Background(), RuleInput{Messages: []models.Message{
			ruleMsg("user", "在吗", base),
		}})
		if len(vs) != 0 {
			t.Fatalf("dangling user message must not be judged: %+v", vs)
		}
	})

	t.Run("阈值关闭不评", func(t *testing.T) {
		rule := &ResponseTimeoutRule{threshold: 0}
		vs := rule.Evaluate(context.Background(), RuleInput{Messages: []models.Message{
			ruleMsg("user", "在吗", base),
			ruleMsg("agent", "在的", base.Add(time.Hour)),
		}})
		if len(vs) != 0 {
			t.Fatalf("disabled rule must pass: %+v", vs)
		}
	})
}

func TestFirstResponseRule(t *testing.T) {
	base := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	session := &models.Session{ID: "s1", StartedAt: base}

	t.Run("超时记 high", func(t *testing.T) {
		rule := &FirstResponseRule{threshold: 60}
		vs := rule.Evaluate(context.Background(), RuleInput{Session: session, Messages: []models.Message{
			ruleMsg("user", "你好", base.Add(10*time.Second)),
			ruleMsg("agent", "您好", base.Add(3*time.Minute)),
		}})
		if len(vs) != 1 || vs[0].Severity != SeverityHigh {
			t.Fatalf("unexpected violations: %+v", vs)
		}
	})

	t.Run("从首条 user 消息起算晚于开始时间", func(t *testing.T) {
		rule := &FirstResponseRule{threshold: 60}
		vs := rule.Evaluate(context.Background(), RuleInput{Session: session, Messages: []models.Message{
			ruleMsg("agent", "您好", base.Add(30*time.Second)), // 开场白不算首响起点
			ruleMsg("user", "你好", base.Add(10*time.Minute)),
			ruleMsg("agent", "在的", base.Add(10*time.Minute+30*time.Second)),
		}})
		if len(vs) != 0 {
			t.Fatalf("gap from first user message is within threshold: %+v", vs)
		}
	})

	t.Run("无任何 staff 回复不评", func(t *testing.T) {
		rule := &FirstResponseRule{threshold: 1}
		vs := rule.Evaluate(context.Background(), RuleInput{Session: session, Messages: []models.Message{
			ruleMsg("user", "你好", base),
		}})
		if len(vs) != 0 {
			t.Fatalf("no-reply session must not be judged: %+v", vs)
		}
	})
}

type panicRule struct{}

func (panicRule) Name() string { return "boom" }

func (panicRule) Evaluate(context.Context, RuleInput) []Violation { panic("boom") }

type okRule struct{ hit *bool }

func (okRule) Name() string { return "ok" }

func (r okRule) Evaluate(context.Context, RuleInput) []Violation {
	*r.hit = true
	return nil
}

func TestRuleEnginePanicIsolated(t *testing.T) {
	hit := false
	engine := &RuleEngine{rules: []Rule{panicRule{}, okRule{hit: &hit}}, onPanic: func(string, any) {}}
	vs := engine.Run(context.Background(), RuleInput{})
	if len(vs) != 0 {
		t.Fatalf("panic rule must degrade to nothing: %+v", vs)
	}
	if !hit {
		t.Fatal("rules after the panicking one must still run")
	}
}

func TestLowerAllNormalizes(t *testing.T) {
	got := lowerAll([]string{"  BAD ", "", "Word"})
	if got[0] != "bad" || got[2] != "word" {
		t.Fatalf("unexpected normalization: %q", got)
	}
}

func TestSnippetAroundBounds(t *testing.T) {
	if s := snippetAround("short", "hort"); s != "short" {
		t.Fatalf("short content must not slice out of range: %q", s)
	}
}
