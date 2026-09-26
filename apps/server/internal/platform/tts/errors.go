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
	// ErrUpstream provider 上游失败（HTTP 非 2xx / 网络失败）的包装面：
	// 逐句预算熔断（设计文档 §3.2）按它识别"该句合成失败→降级"分支。
	ErrUpstream = errors.New("tts: upstream error")
)
