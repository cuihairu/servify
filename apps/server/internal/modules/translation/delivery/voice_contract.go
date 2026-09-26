package delivery

import (
	"context"
	"errors"

	translationapp "servify/apps/server/internal/modules/translation/application"
)

// 语音翻译通道契约（Phase 2 刀二b-2，docs/realtime-translation-design.md
// §1.2 分发半边）：平台 voice hub 按连接消费 StartAudioStream，管线产出经
// VoiceStreamSink 回流广播。类型别名让平台层只依赖 delivery 契约面，
// 不反向依赖模块应用层。

// 说话方角色（voice 通道握手 speaker 参数；决定偏好读向：visitor 说话
// 消费 agent 读向——对方坐席读什么语言，反之亦然）。
const (
	VoiceSpeakerVisitor = "visitor"
	VoiceSpeakerAgent   = "agent"
)

// ErrVoiceChannelInactive 会话未设置目标语言偏好：语音通道对该会话不启用
// （hub 侧发 voice-error{disabled} 后收线，客户端可引导先设偏好）。
var ErrVoiceChannelInactive = errors.New("translation voice channel is not enabled for this session")

// VoiceAudioStream 一条语音上行流：喂原始音频分片（pcm16 24kHz 单声道，
// 与 ASR provider 协议面对齐）+ 收尾。Feed 返回错误即会话破损（上游断流/
// 会话收尾），调用方应停止喂入并收线。
type VoiceAudioStream interface {
	Feed(chunk []byte) error
	Close() error
}

// VoiceStreamSink 管线产出回流面（translationapp.VoiceSink 的契约别名）。
type VoiceStreamSink = translationapp.VoiceSink

// VoiceCaption 字幕产出载荷（translationapp.VoiceCaption 的契约别名，
// 平台层经 delivery 消费，不反向依赖应用层）。
type VoiceCaption = translationapp.VoiceCaption

// VoiceStreamStarter 语音翻译通道门面（平台 voice hub 注入点）：为会话的
// 某说话方建立"ASR 流式识别 → 分句 → 逐句翻译 → TTS"管线。未配置（AI
// 面缺失）返回 ErrTranslationUnavailable；会话无偏好返回
// ErrVoiceChannelInactive；ASR 建联失败原样上抛。
type VoiceStreamStarter interface {
	StartAudioStream(ctx context.Context, sessionID, speaker string, sink VoiceStreamSink) (VoiceAudioStream, error)
}
