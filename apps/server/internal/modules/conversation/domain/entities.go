package domain

import "time"

type ConversationStatus string

const (
	ConversationStatusActive       ConversationStatus = "active"
	ConversationStatusWaitingHuman ConversationStatus = "waiting_human"
	ConversationStatusTransferred  ConversationStatus = "transferred"
	ConversationStatusClosed       ConversationStatus = "closed"
)

type ParticipantRole string

const (
	ParticipantRoleCustomer ParticipantRole = "customer"
	ParticipantRoleAgent    ParticipantRole = "agent"
	ParticipantRoleAI       ParticipantRole = "ai"
	ParticipantRoleSystem   ParticipantRole = "system"
)

type MessageKind string

const (
	MessageKindText   MessageKind = "text"
	MessageKindSystem MessageKind = "system"
)

type ChannelBinding struct {
	Channel     string
	ExternalID  string
	SessionID   string
	WorkspaceID string
	Protocol    string
	ProtocolRef string
}

type Participant struct {
	ID          string
	UserID      *uint
	Role        ParticipantRole
	DisplayName string
}

type Conversation struct {
	ID            string
	CustomerID    *uint
	Status        ConversationStatus
	Subject       string
	Channel       ChannelBinding
	Participants  []Participant
	StartedAt     time.Time
	LastMessageAt *time.Time
	EndedAt       *time.Time
}

type ConversationMessage struct {
	ID             string
	ConversationID string
	Sender         ParticipantRole
	Kind           MessageKind
	Content        string
	Metadata       map[string]string
	CreatedAt      time.Time
}

// ConversationEvent 是会话服务过程的事件流水（Service Timeline，V1.0
// 收敛 B1，docs/v1-convergence-plan.md §3.1）：由事件总线上的
// conversation.* / routing.* / ticket.* 事件投影写入，只读消费，
// 不进入业务写路径。ActorType 取值：system | customer | agent | ai | routing。
type ConversationEvent struct {
	ID             uint
	ConversationID string
	EventType      string
	ActorType      string
	ActorID        string
	Summary        string
	// Payload 为事件原文的 JSON 存档，仅供排障/回放，Timeline 展示走
	// EventType + Summary。
	Payload    string
	OccurredAt time.Time
}
