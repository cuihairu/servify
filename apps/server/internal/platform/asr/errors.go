package asr

import "errors"

// 契约级哨兵错误：调用方用 errors.Is 分支，provider 实现可包装或直接返回。
var (
	// ErrNotConfigured provider 未配置（factory 对空 provider 的统一出口，
	// 装配层据此跳过语音链路接线而不是启动失败）。
	ErrNotConfigured = errors.New("asr: provider not configured")
	// ErrSessionClosed 会话已关闭后继续 FeedAudio。
	ErrSessionClosed = errors.New("asr: session closed")
)
