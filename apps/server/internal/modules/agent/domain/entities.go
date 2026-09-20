package domain

import "time"

type PresenceStatus string

const (
	PresenceStatusOffline PresenceStatus = "offline"
	PresenceStatusOnline  PresenceStatus = "online"
	PresenceStatusBusy    PresenceStatus = "busy"
	PresenceStatusAway    PresenceStatus = "away"
)

type AgentProfile struct {
	UserID              uint
	Username            string
	Name                string
	Department          string
	Skills              []string
	MaxChatConcurrency  int
	MaxVoiceConcurrency int
	CurrentChatLoad     int
	CurrentVoiceLoad    int
	Rating              float64
	AvgResponseTime     int
	TotalTickets        int
}

type AgentPresence struct {
	UserID       uint
	Status       PresenceStatus
	LastActivity time.Time
	ConnectedAt  time.Time
}

type AgentLoad struct {
	UserID              uint
	MaxChatConcurrency  int
	MaxVoiceConcurrency int
	CurrentChatLoad     int
	CurrentVoiceLoad    int
}
