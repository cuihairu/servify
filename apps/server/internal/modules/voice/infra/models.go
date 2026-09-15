package infra

import "time"

// 本文件是 voice 模块自有的 GORM 持久化模型。历史上位于 internal/models，
// P1-7 边界收口迁入模块（legacy 侧通过 internal/models 的类型别名过渡引用）。

// VoiceCall 语音通话记录
type VoiceCall struct {
	ID              string     `gorm:"primaryKey" json:"id"`
	SessionID       string     `gorm:"index" json:"session_id"`
	Status          string     `gorm:"default:'started'" json:"status"` // started, answered, held, ended, transferred
	StartedAt       time.Time  `json:"started_at"`
	AnsweredAt      *time.Time `json:"answered_at,omitempty"`
	HeldAt          *time.Time `json:"held_at,omitempty"`
	ResumedAt       *time.Time `json:"resumed_at,omitempty"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	TransferToAgent *uint      `json:"transfer_to_agent,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// VoiceRecording 语音录音记录
type VoiceRecording struct {
	ID         string    `gorm:"primaryKey" json:"id"`
	CallID     string    `gorm:"index" json:"call_id"`
	Provider   string    `json:"provider"`
	Status     string    `gorm:"default:'recording'" json:"status"` // recording, stopped
	StorageURI string    `json:"storage_uri"`
	StartedAt  time.Time `json:"started_at"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// VoiceTranscript 语音转写记录
type VoiceTranscript struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	CallID    string    `gorm:"index" json:"call_id"`
	Content   string    `gorm:"type:text" json:"content"`
	Language  string    `json:"language"`
	Finalized bool      `gorm:"default:false" json:"finalized"`
	CreatedAt time.Time `json:"created_at"`
}
