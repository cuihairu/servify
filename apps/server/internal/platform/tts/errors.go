package tts

import "errors"

// 契约级哨兵错误：调用方用 errors.Is 分支，provider 实现可包装或直接返回。
var (
	// ErrNotConfigured provider 未配置（factory 对空 provider 的统一出口，
	// 装配层据此跳过语音链路接线而不是启动失败）。
	ErrNotConfigured = errors.New("tts: provider not configured")
	// ErrInvalidRequest 请求不合法（空文本等）；provider 实现应在前置校验
	// 时返回本哨兵的包装错误。
	ErrInvalidRequest = errors.New("tts: invalid request")
)
