package application

import "time"

type StartCallCommand struct {
	CallID       string
	SessionID    string
	ConnectionID string
}

type AnswerCallCommand struct {
	CallID string
}

type HoldCallCommand struct {
	CallID string
}

type ResumeCallCommand struct {
	CallID string
}

type EndCallCommand struct {
	CallID string
}

type TransferCallCommand struct {
	CallID    string
	ToAgentID uint
}

type CallDTO struct {
	ID              string     `json:"id"`
	SessionID       string     `json:"session_id"`
	Status          string     `json:"status"`
	StartedAt       time.Time  `json:"started_at"`
	AnsweredAt      *time.Time `json:"answered_at,omitempty"`
	HeldAt          *time.Time `json:"held_at,omitempty"`
	ResumedAt       *time.Time `json:"resumed_at,omitempty"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	TransferToAgent *uint      `json:"transfer_to_agent,omitempty"`
}

type RecordingDTO struct {
	ID         string    `json:"id"`
	CallID     string    `json:"call_id"`
	Provider   string    `json:"provider,omitempty"`
	Status     string    `json:"status"`
	StorageURI string    `json:"storage_uri,omitempty"`
	StartedAt  time.Time `json:"started_at"`
}

// CompleteRecordingCommand 在 provider 侧已完成录制时落地终态:
// hosted vendor(如 Twilio)通过回调推送录音 URL,不走 Start/Stop 指令方向。
type CompleteRecordingCommand struct {
	RecordingID string
	CallID      string
	Provider    string
	StorageURI  string
}

type TranscriptDTO struct {
	CallID     string    `json:"call_id"`
	Content    string    `json:"content"`
	Language   string    `json:"language"`
	Finalized  bool      `json:"finalized"`
	AppendedAt time.Time `json:"appended_at"`
}
