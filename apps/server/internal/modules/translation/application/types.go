package application

import (
	"errors"
	"fmt"
)

// 翻译应用层错误（delivery 侧映射 HTTP 状态；provider 故障原样上抛由
// handler 按 502/504 处理）。
var (
	ErrTranslationTextRequired   = errors.New("translation text is required")
	ErrTranslationTextTooLong    = fmt.Errorf("translation text exceeds %d characters", MaxTextRunes)
	ErrTranslationTargetRequired = errors.New("target language is required")
	ErrTranslationLangInvalid    = errors.New("language must be a BCP-47 style tag such as zh-CN or en")
	ErrTranslationUnavailable    = errors.New("translation is not configured")
	ErrTranslationEmptyOutput    = errors.New("translation model returned empty content")
)

// 输入上限：单条聊天消息翻译的合理上界（约 4000 字符），超出按 400 拒绝——
// 长文本应走批处理/文档翻译（后续阶段），不该占实时链路预算。
const MaxTextRunes = 4000

// SourceLangAuto 表示源语言由模型自动检测（TranslateCommand.SourceLang 缺省值）。
const SourceLangAuto = "auto"

// RuntimeParams 与首答/Copilot 同源的出站模型参数（装配层从 llmfactory 注入）。
type RuntimeParams struct {
	Model       string
	Temperature float64
	MaxTokens   int
	TimeoutMs   int
}

// MaxContextRunes 逐句翻译的上下文尾窗上限（设计文档 §2.2：携带上一句
// 原文+译文尾窗 ≤ 200 字符，控制时延与费用；超窗服务端截尾）。
const MaxContextRunes = 200

// TranslateCommand 单条文本翻译指令。SourceLang 缺省 auto（自动检测）；
// Context 为可选对话上文（语音逐句链路防割裂，Phase 2 刀二b），仅参与
// 提示词、不参与输出。
type TranslateCommand struct {
	Text       string
	SourceLang string
	TargetLang string
	Context    string
}

// TranslateResult 翻译产出；Text 为可直接展示的译文。
type TranslateResult struct {
	Text       string `json:"text"`
	SourceLang string `json:"source_lang"`
	TargetLang string `json:"target_lang"`
}
