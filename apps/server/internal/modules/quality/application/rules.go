package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/models"
)

// 严重度（列表筛选与展示共用）。
const (
	SeverityLow    = "low"
	SeverityMedium = "medium"
	SeverityHigh   = "high"
)

// RuleConfig 规则参数（来自 config.QualityConfig.Rules）。
type RuleConfig struct {
	BannedWords                 []string
	ResponseTimeoutSeconds      int
	FirstResponseTimeoutSeconds int
}

// RuleInput 规则评估输入。
type RuleInput struct {
	Session  *models.Session
	Messages []models.Message // created_at ASC 全量
	Config   RuleConfig
}

// Violation 单条违规。
type Violation struct {
	Rule       string `json:"rule"`
	Severity   string `json:"severity"` // low|medium|high
	MessageIDs []uint `json:"message_ids"`
	Snippet    string `json:"snippet"`
	Detail     string `json:"detail"`
}

// Rule 单条质检规则。
type Rule interface {
	Name() string
	Evaluate(ctx context.Context, in RuleInput) []Violation
}

// RuleEngine 顺序执行全部规则；单规则 panic 降级为日志不中断扫描。
type RuleEngine struct {
	rules   []Rule
	onPanic func(rule string, r any)
}

// NewRuleEngine 组装默认三条规则。
func NewRuleEngine(cfg RuleConfig, onPanic func(rule string, r any)) *RuleEngine {
	return &RuleEngine{
		rules: []Rule{
			&BannedWordsRule{words: lowerAll(cfg.BannedWords)},
			&ResponseTimeoutRule{threshold: cfg.ResponseTimeoutSeconds},
			&FirstResponseRule{threshold: cfg.FirstResponseTimeoutSeconds},
		},
		onPanic: onPanic,
	}
}

// Run 执行全部规则并合并违规。
func (e *RuleEngine) Run(ctx context.Context, in RuleInput) []Violation {
	var all []Violation
	for _, rule := range e.rules {
		vs := safeEvaluate(rule, ctx, in, e.onPanic)
		all = append(all, vs...)
	}
	return all
}

func safeEvaluate(rule Rule, ctx context.Context, in RuleInput, onPanic func(string, any)) (vs []Violation) {
	defer func() {
		if r := recover(); r != nil {
			if onPanic != nil {
				onPanic(rule.Name(), r)
			}
			vs = nil
		}
	}()
	return rule.Evaluate(ctx, in)
}

// ---------- banned_words：坐席/AI 消息命中违禁词 ----------

type BannedWordsRule struct{ words []string }

func (r *BannedWordsRule) Name() string { return "banned_words" }

func (r *BannedWordsRule) Evaluate(_ context.Context, in RuleInput) []Violation {
	if len(r.words) == 0 {
		return nil
	}
	var out []Violation
	for i := range in.Messages {
		msg := &in.Messages[i]
		if !isStaffMessage(msg.Sender) {
			continue
		}
		lower := strings.ToLower(msg.Content)
		for _, w := range r.words {
			if w != "" && strings.Contains(lower, w) {
				out = append(out, Violation{
					Rule:       r.Name(),
					Severity:   SeverityHigh,
					MessageIDs: []uint{msg.ID},
					Snippet:    snippetAround(lower, w),
					Detail:     fmt.Sprintf("命中违禁词 %q", w),
				})
				break // 同一条消息只记一次
			}
		}
	}
	return out
}

// ---------- response_timeout：客户消息到下一条坐席/AI 回复的超时 ----------

type ResponseTimeoutRule struct{ threshold int }

func (r *ResponseTimeoutRule) Name() string { return "response_timeout" }

func (r *ResponseTimeoutRule) Evaluate(_ context.Context, in RuleInput) []Violation {
	if r.threshold <= 0 {
		return nil
	}
	msgs := in.Messages
	var out []Violation
	for i := range msgs {
		if msgs[i].Sender != "user" {
			continue
		}
		if reply, ok := nextStaffReply(msgs, i); ok {
			gap := reply.CreatedAt.Sub(msgs[i].CreatedAt)
			if gap > time.Duration(r.threshold)*time.Second {
				out = append(out, Violation{
					Rule:       r.Name(),
					Severity:   SeverityMedium,
					MessageIDs: []uint{msgs[i].ID, reply.ID},
					Detail:     fmt.Sprintf("响应等待 %s 超过阈值 %ds", gap.Round(time.Second), r.threshold),
				})
			}
		}
	}
	return out
}

// ---------- first_response：会话开始（或首条客户消息）到首次回复的超时 ----------

type FirstResponseRule struct{ threshold int }

func (r *FirstResponseRule) Name() string { return "first_response" }

func (r *FirstResponseRule) Evaluate(_ context.Context, in RuleInput) []Violation {
	if r.threshold <= 0 || in.Session == nil {
		return nil
	}
	replyIdx := -1
	for i := range in.Messages {
		if isStaffMessage(in.Messages[i].Sender) {
			replyIdx = i
			break
		}
	}
	if replyIdx < 0 {
		return nil // 会话内无任何坐席/AI 回复，不评
	}
	start := in.Session.StartedAt
	for i := range in.Messages {
		if in.Messages[i].Sender == "user" {
			if in.Messages[i].CreatedAt.After(start) {
				start = in.Messages[i].CreatedAt
			}
			break
		}
	}
	gap := in.Messages[replyIdx].CreatedAt.Sub(start)
	if gap > time.Duration(r.threshold)*time.Second {
		return []Violation{{
			Rule:       r.Name(),
			Severity:   SeverityHigh,
			MessageIDs: []uint{in.Messages[replyIdx].ID},
			Detail:     fmt.Sprintf("首次响应耗时 %s 超过阈值 %ds", gap.Round(time.Second), r.threshold),
		}}
	}
	return nil
}

// ---------- helpers ----------

func isStaffMessage(sender string) bool { return sender == "agent" || sender == "ai" }

// nextStaffReply 找 from 之后第一条坐席/AI 消息。
func nextStaffReply(msgs []models.Message, from int) (*models.Message, bool) {
	for i := from + 1; i < len(msgs); i++ {
		if isStaffMessage(msgs[i].Sender) {
			return &msgs[i], true
		}
	}
	return nil, false
}

func lowerAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, w := range in {
		out = append(out, strings.ToLower(strings.TrimSpace(w)))
	}
	return out
}

// snippetAround 截取命中词前后各 20 字符的上下文。
func snippetAround(lower, word string) string {
	idx := strings.Index(lower, word)
	if idx < 0 {
		return ""
	}
	start := idx - 20
	if start < 0 {
		start = 0
	}
	end := idx + len(word) + 20
	if end > len(lower) {
		end = len(lower)
	}
	return lower[start:end]
}
