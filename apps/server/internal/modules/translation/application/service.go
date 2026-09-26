// Package application 提供聊天文本实时翻译的应用服务：单条消息粒度、
// 无状态 LLM 封装（Phase 0，见 docs/realtime-translation-design.md）。
package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"servify/apps/server/internal/platform/llm"
)

// 语言标签校验：BCP-47 常用子集（2-3 位主语言 + 可选 - 扩展段）或 auto。
var langTagPattern = regexp.MustCompile(`^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{1,8})*$`)

const translationSystemPrompt = "你是客服会话的实时翻译引擎。把用户给出的内容翻译成目标语言，" +
	"要求：只输出译文本身，不加任何解释、前缀、引号或代码块；" +
	"保留原文的换行、列表与占位符（如 {name}、%s、URL、邮箱）不翻译；" +
	"语气与礼貌程度与原文一致；原文已是目标语言时原样返回。"

// Service 聊天文本翻译服务。与首答/Copilot 共用 LLMProvider 与出站参数，
// 不持有任何会话状态；llm 为 nil 时 Translate 返回 ErrTranslationUnavailable
// （部署形态未配置 AI，端点降级 503 而非启动失败）。
type Service struct {
	llm    llm.LLMProvider
	params RuntimeParams
}

// NewService 构造翻译服务。
func NewService(llm llm.LLMProvider, params RuntimeParams) *Service {
	return &Service{llm: llm, params: params}
}

// Translate 翻译单条文本：参数校验 → 提示词组装 → 单次 LLM 调用。
// provider 错误原样上抛（含 %w 包装），由 handler 映射 502/504。
func (s *Service) Translate(ctx context.Context, cmd TranslateCommand) (TranslateResult, error) {
	if s == nil || s.llm == nil {
		return TranslateResult{}, ErrTranslationUnavailable
	}
	text := strings.TrimSpace(cmd.Text)
	if text == "" {
		return TranslateResult{}, ErrTranslationTextRequired
	}
	if utf8.RuneCountInString(text) > MaxTextRunes {
		return TranslateResult{}, ErrTranslationTextTooLong
	}
	target := normalizeLang(cmd.TargetLang)
	if target == "" {
		return TranslateResult{}, ErrTranslationTargetRequired
	}
	if !langTagPattern.MatchString(target) {
		return TranslateResult{}, fmt.Errorf("%w: %q", ErrTranslationLangInvalid, cmd.TargetLang)
	}
	source := normalizeLang(cmd.SourceLang)
	if source == "" {
		source = SourceLangAuto
	}
	if source != SourceLangAuto && !langTagPattern.MatchString(source) {
		return TranslateResult{}, fmt.Errorf("%w: %q", ErrTranslationLangInvalid, cmd.SourceLang)
	}

	resp, err := s.llm.Chat(ctx, llm.ChatRequest{
		Model:       s.params.Model,
		Messages:    buildTranslationMessages(text, source, target, cmd.Context),
		Temperature: s.params.Temperature,
		MaxTokens:   s.params.MaxTokens,
		Options:     llm.RequestOptions{TimeoutMs: s.params.TimeoutMs},
	})
	if err != nil {
		return TranslateResult{}, fmt.Errorf("translation chat: %w", err)
	}
	translated := strings.TrimSpace(resp.Content)
	if translated == "" {
		return TranslateResult{}, ErrTranslationEmptyOutput
	}
	return TranslateResult{Text: translated, SourceLang: source, TargetLang: target}, nil
}

// buildTranslationMessages 组装翻译提示词：system 定角色与纪律，
// user 携带语言指令 + 可选上文（语音逐句链路防割裂，超窗截尾）+
// 待译文本（分隔符防注入混排）。
func buildTranslationMessages(text, source, target, context string) []llm.ChatMessage {
	var instruction string
	if source == SourceLangAuto {
		instruction = fmt.Sprintf("请把下面的内容翻译成 %s。", target)
	} else {
		instruction = fmt.Sprintf("请把下面的内容从 %s 翻译成 %s。", source, target)
	}
	user := instruction + "\n待译内容：\n" + text
	if trimmed := strings.TrimSpace(context); trimmed != "" {
		runes := []rune(trimmed)
		if len(runes) > MaxContextRunes {
			// 尾窗：只保留最近 200 字符（语义连续性在近处）。
			trimmed = string(runes[len(runes)-MaxContextRunes:])
		}
		user = instruction + "\n对话上文（仅帮助理解衔接，不要翻译或输出它）：\n" +
			trimmed + "\n待译内容：\n" + text
	}
	return []llm.ChatMessage{
		{Role: "system", Content: translationSystemPrompt},
		{Role: "user", Content: user},
	}
}

// normalizeLang 去空白；语言标签统一小写（zh-CN → zh-cn）便于比对与回显一致。
func normalizeLang(lang string) string {
	return strings.ToLower(strings.TrimSpace(lang))
}
