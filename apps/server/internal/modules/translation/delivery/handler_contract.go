// Package delivery 定义翻译模块对 HTTP 处理层的契约：handlers 只依赖
// 本包（与 assist/suggestion 等模块同口径），application 类型与哨兵错误
// 在此再导出。
package delivery

import (
	"context"

	translationapp "servify/apps/server/internal/modules/translation/application"
)

// 应用层类型再导出（handler 侧唯一入口）。
type (
	// TranslateCommand 单条文本翻译指令。
	TranslateCommand = translationapp.TranslateCommand
	// TranslateResult 翻译产出。
	TranslateResult = translationapp.TranslateResult
	// BatchTranslateCommand 批量（多段）翻译指令（Phase 1 收尾，历史消息标注）。
	BatchTranslateCommand = translationapp.BatchTranslateCommand
	// BatchTranslateResult 批量翻译产出（与输入同序等长）。
	BatchTranslateResult = translationapp.BatchTranslateResult
	// RuntimeParams 出站模型参数（装配层注入）。
	RuntimeParams = translationapp.RuntimeParams
)

// 应用层哨兵错误再导出（handler 依此映射 4xx/5xx）。
var (
	ErrTranslationTextRequired   = translationapp.ErrTranslationTextRequired
	ErrTranslationTextTooLong    = translationapp.ErrTranslationTextTooLong
	ErrTranslationTargetRequired = translationapp.ErrTranslationTargetRequired
	ErrTranslationLangInvalid    = translationapp.ErrTranslationLangInvalid
	ErrTranslationUnavailable    = translationapp.ErrTranslationUnavailable
	ErrTranslationEmptyOutput    = translationapp.ErrTranslationEmptyOutput
	ErrTranslationBatchTooLarge  = translationapp.ErrTranslationBatchTooLarge
)

// HandlerService 翻译服务契约（管理面坐席 / 访客面 SDK 共用）。
type HandlerService interface {
	// Translate 翻译单条聊天文本；参数问题返回哨兵错误，provider 故障
	// 原样上抛（handler 映射 502/504）。
	Translate(ctx context.Context, cmd TranslateCommand) (TranslateResult, error)
	// BatchTranslate 批量翻译多条文本（历史消息标注用）：单次 LLM 调用
	// 分段协议，失配自动退回逐条；结果与输入同序等长，失败段为空串。
	BatchTranslate(ctx context.Context, cmd BatchTranslateCommand) (BatchTranslateResult, error)
}
