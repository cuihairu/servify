package models

import (
	"gorm.io/gorm"
	"time"
)

// 用户模型
type User struct {
	ID              uint           `gorm:"primaryKey" json:"id"`
	Username        string         `gorm:"unique;not null" json:"username"`
	Email           string         `gorm:"unique;not null" json:"email"`
	Password        string         `gorm:"not null" json:"-"` // bcrypt hash, never exposed via JSON
	Name            string         `json:"name"`
	Phone           string         `json:"phone"`
	Avatar          string         `json:"avatar"`
	Role            string         `gorm:"default:'customer'" json:"role"` // customer, agent, admin
	Status          string         `gorm:"default:'active'" json:"status"` // active, inactive, banned
	LastLogin       *time.Time     `json:"last_login"`
	TokenValidAfter *time.Time     `json:"token_valid_after,omitempty"`
	TokenVersion    int            `gorm:"default:0" json:"token_version"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`

	// 关联关系
	Sessions     []Session         `gorm:"foreignKey:UserID" json:"sessions,omitempty"`
	Tickets      []Ticket          `gorm:"foreignKey:CustomerID" json:"tickets,omitempty"`
	AuthSessions []UserAuthSession `gorm:"foreignKey:UserID" json:"auth_sessions,omitempty"`
}

// UserAuthSession tracks login/refresh session state for JWT issuance and targeted revocation.
type UserAuthSession struct {
	ID                string         `gorm:"primaryKey;size:64" json:"id"`
	UserID            uint           `gorm:"index;not null" json:"user_id"`
	Status            string         `gorm:"default:'active';index" json:"status"` // active, revoked
	TokenVersion      int            `gorm:"default:0" json:"token_version"`
	DeviceFingerprint string         `gorm:"size:128;index" json:"device_fingerprint"`
	UserAgent         string         `gorm:"size:512" json:"user_agent"`
	ClientIP          string         `gorm:"size:128" json:"client_ip"`
	LastSeenAt        *time.Time     `json:"last_seen_at,omitempty"`
	LastRefreshedAt   *time.Time     `json:"last_refreshed_at,omitempty"`
	RevokedAt         *time.Time     `json:"revoked_at,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// RevokedToken tracks explicitly denylisted JWTs by their unique token id (jti).
type RevokedToken struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	JTI       string         `gorm:"size:128;uniqueIndex;not null" json:"jti"`
	UserID    uint           `gorm:"index" json:"user_id"`
	SessionID string         `gorm:"index" json:"session_id"`
	TokenUse  string         `gorm:"index" json:"token_use"` // access, refresh
	Reason    string         `gorm:"type:text" json:"reason"`
	ExpiresAt *time.Time     `gorm:"index" json:"expires_at,omitempty"`
	RevokedAt time.Time      `gorm:"index;not null" json:"revoked_at"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// 客户信息扩展
type Customer struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	TenantID    string         `gorm:"index:idx_customers_scope" json:"tenant_id"`
	WorkspaceID string         `gorm:"index:idx_customers_scope" json:"workspace_id"`
	UserID      uint           `gorm:"index" json:"user_id"`
	Company     string         `json:"company"`
	Industry    string         `json:"industry"`
	Source      string         `json:"source"` // web, referral, marketing
	Tags        string         `json:"tags"`   // 标签，逗号分隔
	Notes       string         `gorm:"type:text" json:"notes"`
	Priority    string         `gorm:"default:'normal'" json:"priority"` // low, normal, high, urgent
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// 客服代理
type Agent struct {
	ID              uint           `gorm:"primaryKey" json:"id"`
	TenantID        string         `gorm:"index:idx_agents_scope" json:"tenant_id"`
	WorkspaceID     string         `gorm:"index:idx_agents_scope" json:"workspace_id"`
	UserID          uint           `gorm:"index" json:"user_id"`
	Department      string         `json:"department"`
	Skills          string         `json:"skills"`                             // 技能标签，逗号分隔
	Status          string         `gorm:"default:'offline'" json:"status"`    // online, offline, busy
	MaxConcurrent   int            `gorm:"default:5" json:"max_concurrent"`    // 最大并发工单数
	CurrentLoad     int            `gorm:"default:0" json:"current_load"`      // 当前工单数
	Rating          float64        `gorm:"default:5.0" json:"rating"`          // 评分
	TotalTickets    int            `gorm:"default:0" json:"total_tickets"`     // 总处理工单数
	AvgResponseTime int            `gorm:"default:0" json:"avg_response_time"` // 平均响应时间(秒)
	LastActivityAt  *time.Time     `json:"last_activity_at,omitempty"`         // 最后活动时间(持久化，替代内存registry)
	ConnectedAt     *time.Time     `json:"connected_at,omitempty"`             // 上线时间(持久化，替代内存registry)
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`

	User    User     `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Tickets []Ticket `gorm:"foreignKey:AgentID" json:"tickets,omitempty"`
}

// 工单模型
type Ticket struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	TenantID    string         `gorm:"index:idx_tickets_scope" json:"tenant_id"`
	WorkspaceID string         `gorm:"index:idx_tickets_scope" json:"workspace_id"`
	Title       string         `gorm:"not null" json:"title"`
	Description string         `gorm:"type:text" json:"description"`
	CustomerID  uint           `gorm:"index" json:"customer_id"`
	AgentID     *uint          `gorm:"index" json:"agent_id"`
	SessionID   *string        `gorm:"index" json:"session_id"`
	Category    string         `json:"category"`                         // technical, billing, general, complaint
	Priority    string         `gorm:"default:'normal'" json:"priority"` // low, normal, high, urgent
	Status      string         `gorm:"default:'open'" json:"status"`     // open, assigned, in_progress, resolved, closed
	Source      string         `json:"source"`                           // web, email, phone, chat
	Tags        string         `json:"tags"`                             // 标签，逗号分隔
	DueDate     *time.Time     `json:"due_date"`
	ResolvedAt  *time.Time     `json:"resolved_at"`
	ClosedAt    *time.Time     `json:"closed_at"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`

	// 关联关系
	Customer          User                     `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	Agent             *User                    `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
	Session           *Session                 `gorm:"foreignKey:SessionID" json:"session,omitempty"`
	Comments          []TicketComment          `gorm:"foreignKey:TicketID" json:"comments,omitempty"`
	Attachments       []TicketFile             `gorm:"foreignKey:TicketID" json:"attachments,omitempty"`
	StatusHistory     []TicketStatus           `gorm:"foreignKey:TicketID" json:"status_history,omitempty"`
	CustomFieldValues []TicketCustomFieldValue `gorm:"foreignKey:TicketID" json:"custom_field_values,omitempty"`
}

// CustomField 自定义字段配置（用于动态表单 / 查询 / 导出）
type CustomField struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	TenantID       string         `gorm:"index" json:"tenant_id"`
	WorkspaceID    string         `gorm:"index" json:"workspace_id"`
	Resource       string         `gorm:"default:'ticket';index" json:"resource"` // ticket
	Key            string         `gorm:"unique;not null" json:"key"`             // stable identifier (slug)
	Name           string         `gorm:"not null" json:"name"`
	Type           string         `gorm:"not null" json:"type"` // string, number, boolean, date, select, multiselect
	Required       bool           `gorm:"default:false" json:"required"`
	Active         bool           `gorm:"default:true" json:"active"`
	OptionsJSON    string         `gorm:"type:text" json:"options_json,omitempty"`    // JSON array (for select/multiselect)
	ValidationJSON string         `gorm:"type:text" json:"validation_json,omitempty"` // JSON object (min/max/regex/etc)
	ShowWhenJSON   string         `gorm:"type:text" json:"show_when_json,omitempty"`  // JSON condition expression
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

// TicketCustomFieldValue 工单自定义字段值
type TicketCustomFieldValue struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	TicketID      uint      `gorm:"index;uniqueIndex:uniq_ticket_field" json:"ticket_id"`
	CustomFieldID uint      `gorm:"index;uniqueIndex:uniq_ticket_field" json:"custom_field_id"`
	Value         string    `gorm:"type:text" json:"value"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	CustomField CustomField `gorm:"foreignKey:CustomFieldID" json:"custom_field,omitempty"`
}

// 工单评论
type TicketComment struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TicketID  uint      `gorm:"index" json:"ticket_id"`
	UserID    uint      `gorm:"index" json:"user_id"`
	Content   string    `gorm:"type:text;not null" json:"content"`
	Type      string    `gorm:"default:'comment'" json:"type"` // comment, internal_note, system
	CreatedAt time.Time `json:"created_at"`

	Ticket Ticket `gorm:"foreignKey:TicketID" json:"ticket,omitempty"`
	User   User   `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// 工单附件
type TicketFile struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TicketID  uint      `gorm:"index" json:"ticket_id"`
	UserID    uint      `gorm:"index" json:"user_id"`
	FileName  string    `gorm:"not null" json:"file_name"`
	FilePath  string    `gorm:"not null" json:"file_path"`
	FileSize  int64     `json:"file_size"`
	MimeType  string    `json:"mime_type"`
	CreatedAt time.Time `json:"created_at"`

	Ticket Ticket `gorm:"foreignKey:TicketID" json:"ticket,omitempty"`
	User   User   `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// 工单状态历史
type TicketStatus struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	TicketID   uint      `gorm:"index" json:"ticket_id"`
	UserID     uint      `gorm:"index" json:"user_id"`
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`

	Ticket Ticket `gorm:"foreignKey:TicketID" json:"ticket,omitempty"`
	User   User   `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// 会话模型（更新）
type Session struct {
	ID          string     `gorm:"primaryKey" json:"id"`
	TenantID    string     `gorm:"index:idx_sessions_scope" json:"tenant_id"`
	WorkspaceID string     `gorm:"index:idx_sessions_scope" json:"workspace_id"`
	UserID      uint       `gorm:"index" json:"user_id"`
	AgentID     *uint      `gorm:"index" json:"agent_id"`
	TicketID    *uint      `gorm:"index" json:"ticket_id"`
	Status      string     `gorm:"default:'active'" json:"status"` // active, ended, transferred
	Platform    string     `json:"platform"`                       // web, telegram, wechat, etc.
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	User     User      `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Agent    *User     `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
	Ticket   *Ticket   `gorm:"foreignKey:TicketID" json:"ticket,omitempty"`
	Messages []Message `gorm:"foreignKey:SessionID" json:"messages,omitempty"`
}

// 消息模型（更新）
type Message struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TenantID    string    `gorm:"index:idx_messages_scope" json:"tenant_id"`
	WorkspaceID string    `gorm:"index:idx_messages_scope" json:"workspace_id"`
	SessionID   string    `gorm:"index" json:"session_id"`
	UserID      uint      `gorm:"index" json:"user_id"`
	Content     string    `gorm:"type:text" json:"content"`
	Type        string    `json:"type"`   // text, image, file, system
	Sender      string    `json:"sender"` // user, ai, agent
	CreatedAt   time.Time `json:"created_at"`

	Session Session `gorm:"foreignKey:SessionID" json:"session,omitempty"`
	User    User    `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// 会话转接记录
type TransferRecord struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	TenantID       string    `gorm:"index:idx_transfer_records_scope" json:"tenant_id"`
	WorkspaceID    string    `gorm:"index:idx_transfer_records_scope" json:"workspace_id"`
	SessionID      string    `gorm:"index" json:"session_id"`
	FromAgentID    *uint     `gorm:"index" json:"from_agent_id,omitempty"`
	ToAgentID      *uint     `gorm:"index" json:"to_agent_id,omitempty"`
	Reason         string    `json:"reason"`
	Notes          string    `json:"notes"`
	SessionSummary string    `gorm:"type:text" json:"session_summary"`
	TransferredAt  time.Time `json:"transferred_at"`
	CreatedAt      time.Time `json:"created_at"`
}

// 会话等待队列记录
type WaitingRecord struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	TenantID      string     `gorm:"index:idx_waiting_records_scope" json:"tenant_id"`
	WorkspaceID   string     `gorm:"index:idx_waiting_records_scope" json:"workspace_id"`
	SessionID     string     `gorm:"index" json:"session_id"`
	Reason        string     `json:"reason"`
	TargetSkills  string     `json:"target_skills"`
	TargetGroupID *uint      `gorm:"index" json:"target_group_id,omitempty"` // 指定坐席组（0/nil = 不限组）
	Priority      string     `json:"priority"`
	Notes         string     `json:"notes"`
	Status        string     `gorm:"default:'waiting'" json:"status"` // waiting, transferred, cancelled
	QueuedAt      time.Time  `json:"queued_at"`
	ClaimedAt     *time.Time `gorm:"index:idx_waiting_records_claim" json:"claimed_at,omitempty"` // worker 认领租约起点
	AssignedAt    *time.Time `json:"assigned_at,omitempty"`
	AssignedTo    *uint      `gorm:"index" json:"assigned_to,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// 知识库文档
type KnowledgeDoc struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	TenantID    string `gorm:"index" json:"tenant_id"`
	WorkspaceID string `gorm:"index" json:"workspace_id"`
	ProviderID  string `gorm:"index" json:"provider_id"`
	ExternalID  string `gorm:"index" json:"external_id"`
	Title       string `json:"title"`
	Content     string `gorm:"type:text" json:"content"`
	Category    string `json:"category"`
	Tags        string `json:"tags"`
	IsPublic    bool   `gorm:"default:false;index" json:"is_public"`
	// 新增字段 - pgvector 支持
	Embedding  Embedding `gorm:"type:vector(1536)" json:"embedding,omitempty"`
	ChunkIndex int       `gorm:"default:0" json:"chunk_index,omitempty"`
	DocChunkID string    `gorm:"index" json:"doc_chunk_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// 知识库索引任务
type KnowledgeIndexJob struct {
	ID          string     `gorm:"primaryKey" json:"id"`
	DocumentID  uint       `gorm:"index;not null" json:"document_id"`
	Status      string     `gorm:"index;not null" json:"status"`
	Error       string     `gorm:"type:text" json:"error"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// WebRTC 连接信息
type WebRTCConnection struct {
	ID             string    `gorm:"primaryKey" json:"id"`
	SessionID      string    `gorm:"index" json:"session_id"`
	Status         string    `gorm:"default:'connecting'" json:"status"` // connecting, connected, disconnected
	ConnectionType string    `json:"connection_type"`                    // data, video, screen
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// SLA 配置
type SLAConfig struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	TenantID          string    `gorm:"index" json:"tenant_id"`
	WorkspaceID       string    `gorm:"index" json:"workspace_id"`
	Name              string    `gorm:"unique;not null" json:"name"`
	Priority          string    `gorm:"not null" json:"priority"`            // low, normal, high, urgent
	CustomerTier      string    `gorm:"default:''" json:"customer_tier"`     // 针对特定客户级别（为空表示全部）
	Tags              string    `gorm:"type:text" json:"tags"`               // 逗号分隔标签，用于细分条件
	WarningThreshold  int       `gorm:"default:80" json:"warning_threshold"` // 触发告警的阈值（百分比）
	FirstResponseTime int       `gorm:"not null" json:"first_response_time"` // 分钟
	ResolutionTime    int       `gorm:"not null" json:"resolution_time"`     // 分钟
	EscalationTime    int       `gorm:"not null" json:"escalation_time"`     // 分钟
	BusinessHoursOnly bool      `gorm:"default:false" json:"business_hours_only"`
	Active            bool      `gorm:"default:true" json:"active"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// SLA 违约记录
type SLAViolation struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	TenantID      string     `gorm:"index" json:"tenant_id"`
	WorkspaceID   string     `gorm:"index" json:"workspace_id"`
	TicketID      uint       `gorm:"index" json:"ticket_id"`
	SLAConfigID   uint       `gorm:"index" json:"sla_config_id"`
	ViolationType string     `gorm:"not null" json:"violation_type"` // first_response, resolution, escalation
	Deadline      time.Time  `json:"deadline"`
	ViolatedAt    time.Time  `json:"violated_at"`
	ResolvedAt    *time.Time `json:"resolved_at"`
	Resolved      bool       `gorm:"default:false" json:"resolved"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	Ticket    Ticket    `gorm:"foreignKey:TicketID" json:"ticket,omitempty"`
	SLAConfig SLAConfig `gorm:"foreignKey:SLAConfigID" json:"sla_config,omitempty"`
}

// 客户满意度评价
type CustomerSatisfaction struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TenantID    string    `gorm:"index" json:"tenant_id"`
	WorkspaceID string    `gorm:"index" json:"workspace_id"`
	TicketID    uint      `gorm:"index" json:"ticket_id"`
	CustomerID  uint      `gorm:"index" json:"customer_id"`
	AgentID     *uint     `gorm:"index" json:"agent_id"`
	Rating      int       `gorm:"not null;check:rating >= 1 AND rating <= 5" json:"rating"` // 1-5星
	Comment     string    `gorm:"type:text" json:"comment"`
	Category    string    `json:"category"` // service_quality, response_time, resolution_quality, overall
	CreatedAt   time.Time `json:"created_at"`

	Ticket   Ticket   `gorm:"foreignKey:TicketID" json:"ticket,omitempty"`
	Customer Customer `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	Agent    *User    `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
}

// SatisfactionSurvey CSAT 调查发送与响应记录
type SatisfactionSurvey struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	TenantID       string     `gorm:"index" json:"tenant_id"`
	WorkspaceID    string     `gorm:"index" json:"workspace_id"`
	TicketID       uint       `gorm:"index" json:"ticket_id"`
	CustomerID     uint       `gorm:"index" json:"customer_id"`
	AgentID        *uint      `gorm:"index" json:"agent_id"`
	Channel        string     `gorm:"default:'email'" json:"channel"`
	Status         string     `gorm:"default:'queued';index" json:"status"` // queued, sent, completed, expired
	SurveyToken    string     `gorm:"uniqueIndex" json:"survey_token"`
	SentAt         *time.Time `json:"sent_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	SatisfactionID *uint      `json:"satisfaction_id"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// AppIntegration 市场应用集成定义
type AppIntegration struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	TenantID       string    `gorm:"index" json:"tenant_id"`
	WorkspaceID    string    `gorm:"index" json:"workspace_id"`
	Name           string    `gorm:"unique;not null" json:"name"`
	Slug           string    `gorm:"unique;not null" json:"slug"`
	Vendor         string    `json:"vendor"`
	Category       string    `json:"category"`
	Summary        string    `gorm:"type:text" json:"summary"`
	IconURL        string    `json:"icon_url"`
	Capabilities   string    `gorm:"type:text" json:"capabilities"`  // JSON 数组
	ConfigSchema   string    `gorm:"type:text" json:"config_schema"` // JSON 对象
	IFrameURL      string    `gorm:"type:text" json:"iframe_url"`
	Enabled        bool      `gorm:"default:true" json:"enabled"`
	LastSyncStatus string    `json:"last_sync_status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// 班次管理
type ShiftSchedule struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TenantID    string    `gorm:"index" json:"tenant_id"`
	WorkspaceID string    `gorm:"index" json:"workspace_id"`
	AgentID     uint      `gorm:"index" json:"agent_id"`
	ShiftType   string    `gorm:"not null" json:"shift_type"` // morning, afternoon, evening, night
	StartTime   time.Time `gorm:"not null" json:"start_time"`
	EndTime     time.Time `gorm:"not null" json:"end_time"`
	Date        time.Time `gorm:"index" json:"date"`
	Status      string    `gorm:"default:'scheduled'" json:"status"` // scheduled, active, completed, cancelled
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	Agent User `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
}

// 统计表
type DailyStats struct {
	ID                   uint      `gorm:"primaryKey" json:"id"`
	Date                 time.Time `gorm:"uniqueIndex" json:"date"`
	TotalSessions        int       `gorm:"default:0" json:"total_sessions"`
	TotalMessages        int       `gorm:"default:0" json:"total_messages"`
	TotalTickets         int       `gorm:"default:0" json:"total_tickets"`
	ResolvedTickets      int       `gorm:"default:0" json:"resolved_tickets"`
	AvgResponseTime      int       `gorm:"default:0" json:"avg_response_time"`     // 秒
	AvgResolutionTime    int       `gorm:"default:0" json:"avg_resolution_time"`   // 秒
	CustomerSatisfaction float64   `gorm:"default:0" json:"customer_satisfaction"` // 平均满意度
	AIUsageCount         int       `gorm:"default:0" json:"ai_usage_count"`
	WeKnoraUsageCount    int       `gorm:"default:0" json:"weknora_usage_count"`
	SLAViolations        int       `gorm:"default:0" json:"sla_violations"` // SLA违约次数
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

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

// TenantConfig stores tenant-scoped configuration overrides.
type TenantConfig struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	TenantID        string    `gorm:"uniqueIndex;not null" json:"tenant_id"`
	PortalJSON      string    `gorm:"type:text" json:"portal_json"`
	OpenAIJSON      string    `gorm:"type:text" json:"openai_json"`
	DifyJSON        string    `gorm:"type:text" json:"dify_json"`
	WeKnoraJSON     string    `gorm:"type:text" json:"weknora_json"`
	SessionRiskJSON string    `gorm:"type:text" json:"session_risk_json"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// WorkspaceConfig stores workspace-scoped configuration overrides.
type WorkspaceConfig struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	TenantID        string    `gorm:"uniqueIndex:idx_workspace_configs_scope;not null" json:"tenant_id"`
	WorkspaceID     string    `gorm:"uniqueIndex:idx_workspace_configs_scope;not null" json:"workspace_id"`
	PortalJSON      string    `gorm:"type:text" json:"portal_json"`
	OpenAIJSON      string    `gorm:"type:text" json:"openai_json"`
	DifyJSON        string    `gorm:"type:text" json:"dify_json"`
	WeKnoraJSON     string    `gorm:"type:text" json:"weknora_json"`
	SessionRiskJSON string    `gorm:"type:text" json:"session_risk_json"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// AuditLog records management-surface write operations for traceability.
type AuditLog struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	ActorUserID   *uint     `gorm:"index" json:"actor_user_id"`
	PrincipalKind string    `gorm:"index;not null" json:"principal_kind"`
	Action        string    `gorm:"index;not null" json:"action"`
	ResourceType  string    `gorm:"index;not null" json:"resource_type"`
	ResourceID    string    `gorm:"index" json:"resource_id"`
	Route         string    `gorm:"not null" json:"route"`
	Method        string    `gorm:"not null" json:"method"`
	StatusCode    int       `json:"status_code"`
	Success       bool      `gorm:"index" json:"success"`
	RequestID     string    `gorm:"index" json:"request_id"`
	ClientIP      string    `json:"client_ip"`
	UserAgent     string    `gorm:"type:text" json:"user_agent"`
	TenantID      string    `gorm:"index" json:"tenant_id"`
	WorkspaceID   string    `gorm:"index" json:"workspace_id"`
	RequestJSON   string    `gorm:"type:text" json:"request_json"`
	BeforeJSON    string    `gorm:"type:text" json:"before_json"`
	AfterJSON     string    `gorm:"type:text" json:"after_json"`
	CreatedAt     time.Time `json:"created_at"`
}

// QualityReview 是单个已结束会话的质检记录：规则违规 + LLM 打分 + 人工复核。
// session_id 唯一是 worker 幂等闩；JSON 列一律 type:text（sqlite/pg 双轨方言安全）。
type QualityReview struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	TenantID        string     `gorm:"index:idx_quality_reviews_scope" json:"tenant_id"`
	WorkspaceID     string     `gorm:"index:idx_quality_reviews_scope" json:"workspace_id"`
	SessionID       string     `gorm:"uniqueIndex:uniq_quality_reviews_session;not null" json:"session_id"`
	CustomerID      *uint      `gorm:"index" json:"customer_id"`
	AgentID         *uint      `gorm:"index" json:"agent_id"`                                            // users.id，与 Session.AgentID 同语义
	Status          string     `gorm:"index:idx_quality_reviews_status;default:'pending'" json:"status"` // pending|skipped|scored|failed|confirmed
	Trigger         string     `gorm:"default:'worker'" json:"trigger"`                                  // worker|manual|rescore
	MessageCount    int        `json:"message_count"`
	DurationSeconds int        `json:"duration_seconds"`
	ViolationsJSON  string     `gorm:"type:text" json:"violations_json"`
	ViolationCount  int        `json:"violation_count"`
	MaxSeverity     string     `gorm:"index" json:"max_severity"` // ''|low|medium|high
	DimensionsJSON  string     `gorm:"type:text" json:"dimensions_json"`
	LLMTotalScore   *float64   `json:"llm_total_score"`
	LLMSummary      string     `gorm:"type:text" json:"llm_summary"`
	LLMProvider     string     `json:"llm_provider"`
	LLMModel        string     `json:"llm_model"`
	AttemptCount    int        `json:"attempt_count"`
	NextRetryAt     *time.Time `gorm:"index:idx_quality_reviews_retry" json:"next_retry_at"`
	LastError       string     `gorm:"type:text" json:"last_error"`
	ManualScore     *float64   `json:"manual_score"`
	ManualResult    string     `json:"manual_result"` // ''|pass|violation
	ReviewNote      string     `gorm:"type:text" json:"review_note"`
	ReviewedBy      *uint      `json:"reviewed_by"`
	ReviewedAt      *time.Time `json:"reviewed_at"`
	ScoredAt        *time.Time `json:"scored_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// AgentGroup 坐席组：组内分配 + 溢出策略（global 落全局池 / none 直接失败）。
// agent_id 语义全链路统一 users.id；parent_id 预留列，本期不实现树语义。
type AgentGroup struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	TenantID    string `gorm:"index:idx_agent_groups_scope,priority:1;uniqueIndex:uniq_agent_groups_name,priority:1" json:"tenant_id"`
	WorkspaceID string `gorm:"index:idx_agent_groups_scope,priority:2;uniqueIndex:uniq_agent_groups_name,priority:2" json:"workspace_id"`
	Name        string `gorm:"not null;uniqueIndex:uniq_agent_groups_name,priority:3" json:"name"`
	Description string `gorm:"type:text" json:"description"`
	Priority    int    `json:"priority"`
	// 不加 gorm default tag：零值（enabled=false / policy=""）必须原样落库，
	// 默认值由 CreateAgentGroup（policy 兜底 global）与管理面入参（enabled 缺省 true）保证。
	OverflowPolicy string         `json:"overflow_policy"` // global|none
	Enabled        bool           `json:"enabled"`
	ParentID       *uint          `json:"parent_id,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

// AgentGroupMember 坐席组成员（AgentUserID = users.id，与 Session.AgentID 同语义）。
type AgentGroupMember struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	GroupID     uint      `gorm:"not null;uniqueIndex:uniq_agent_group_members,priority:1;index" json:"group_id"`
	AgentUserID uint      `gorm:"not null;uniqueIndex:uniq_agent_group_members,priority:2" json:"agent_user_id"`
	CreatedAt   time.Time `json:"created_at"`
}
