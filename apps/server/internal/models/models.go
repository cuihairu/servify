package models

import (
	"gorm.io/gorm"
	assistdomain "servify/apps/server/internal/modules/assist/domain"
	knowledgedomain "servify/apps/server/internal/modules/knowledge/domain"
	qualitydomain "servify/apps/server/internal/modules/quality/domain"
	voiceinfra "servify/apps/server/internal/modules/voice/infra"
	"time"
)

// 用户模型
type User struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	Username        string     `gorm:"unique;not null" json:"username"`
	Email           string     `gorm:"unique;not null" json:"email"`
	Password        string     `gorm:"not null" json:"-"` // bcrypt hash, never exposed via JSON
	Name            string     `json:"name"`
	Phone           string     `json:"phone"`
	Avatar          string     `json:"avatar"`
	Role            string     `gorm:"default:'customer'" json:"role"` // customer, agent, admin
	Status          string     `gorm:"default:'active'" json:"status"` // active, inactive, banned
	LastLogin       *time.Time `json:"last_login"`
	TokenValidAfter *time.Time `json:"token_valid_after,omitempty"`
	TokenVersion    int        `gorm:"default:0" json:"token_version"`
	// TOTP 两步验证；totp_secret 绝不进 JSON 响应
	TotpSecret    string         `gorm:"default:''" json:"-"`
	TotpEnabled   bool           `gorm:"default:false" json:"totp_enabled,omitempty"`
	TotpEnabledAt *time.Time     `json:"totp_enabled_at,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`

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

// UserRecoveryCode 是 TOTP 两步验证的恢复码：只存 sha256 原文哈希，
// 核销走原子 UPDATE（used_at IS NULL 才置位），防止并发重复使用同一码。
type UserRecoveryCode struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	UserID    uint       `gorm:"index;not null" json:"user_id"`
	CodeHash  string     `gorm:"size:64;not null" json:"-"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
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
	AISummary   string         `gorm:"type:text" json:"ai_summary"` // 访客工单创建时的 AI 会话摘要（M3 §10 #4）
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

	// 访客已读游标（§10 #3）：访客最后一条已读消息 ID（messages.id 单调序，
	// 只前进不后退）；未读数 = agent/system 消息中 ID 大于该游标的条数。
	VisitorReadMessageID int64 `gorm:"default:0" json:"visitor_read_message_id"`

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

// VoiceCall 语音通话记录。定义已迁至 modules/voice/infra，此处保留类型别名供 legacy 引用方使用。
type VoiceCall = voiceinfra.VoiceCall

// VoiceRecording 语音录音记录。定义已迁至 modules/voice/infra，此处保留类型别名供 legacy 引用方使用。
type VoiceRecording = voiceinfra.VoiceRecording

// VoiceTranscript 语音转写记录。定义已迁至 modules/voice/infra，此处保留类型别名供 legacy 引用方使用。
type VoiceTranscript = voiceinfra.VoiceTranscript

// RemoteAssistSession 远程协助会话。定义已迁至 modules/assist/domain，
// 此处保留类型别名供 legacy 引用方使用（assist_handler.go /
// assist_recording_handler.go 的 swag 注解亦经此别名解析）。
type RemoteAssistSession = assistdomain.RemoteAssistSession

// RemoteAssistAnnotation 远程协助标注。定义已迁至 modules/assist/domain，
// 此处保留类型别名供 legacy 引用方使用（assist_handler.go 的 swag 注解亦经此别名解析）。
type RemoteAssistAnnotation = assistdomain.RemoteAssistAnnotation

// KnowledgeDoc 知识库文档。定义已迁至 modules/knowledge/domain（向量列
// 值对象 Embedding 一并随迁，见 embedding.go），此处保留类型别名供 legacy
// 引用方（pgvector provider / services / migrate 注册）过渡使用。
type KnowledgeDoc = knowledgedomain.KnowledgeDoc

// KnowledgeIndexJob 知识库索引任务。定义已迁至 modules/knowledge/domain，
// 此处保留类型别名供 legacy 引用方过渡使用。
type KnowledgeIndexJob = knowledgedomain.KnowledgeIndexJob

// TenantConfig stores tenant-scoped configuration overrides.
type TenantConfig struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	TenantID        string    `gorm:"uniqueIndex;not null" json:"tenant_id"`
	PortalJSON      string    `gorm:"type:text" json:"portal_json"`
	OpenAIJSON      string    `gorm:"type:text" json:"openai_json"`
	DifyJSON        string    `gorm:"type:text" json:"dify_json"`
	WeKnoraJSON     string    `gorm:"type:text" json:"weknora_json"`
	RagFlowJSON     string    `gorm:"type:text" json:"ragflow_json"`
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
	RagFlowJSON     string    `gorm:"type:text" json:"ragflow_json"`
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

// QualityReview 是单个已结束会话的质检记录。定义已迁至 modules/quality/domain，
// 此处保留类型别名供 legacy 引用方使用（quality_handler.go 的 swag 注解亦经此别名解析）。
type QualityReview = qualitydomain.QualityReview

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

// SuggestionExposureLog 客户侧推荐问题曝光/转化归因日志（P2-0 RQ-5，服务端
// 归因口径）：曝光 = initial/next 接口成功返回（服务端落一行，questions 为
// 本次曝光问题文案的 JSON 数组）；转化 = 该 session 后续客户消息与最近一次
// 未转化曝光的问题列表规范化匹配命中（converted_question 非空即转化）。
// session_id 为空（首屏未带会话标识）的曝光无法归因，仅计曝光。
type SuggestionExposureLog struct {
	ID                uint       `gorm:"primaryKey" json:"id"`
	SessionID         string     `gorm:"index;default:''" json:"session_id"`
	Kind              string     `gorm:"size:16" json:"kind"`     // initial|next
	Strategy          string     `gorm:"size:64" json:"strategy"` // public_knowledge_recency|public_knowledge_scored
	Questions         string     `gorm:"type:text" json:"questions"`
	ConvertedQuestion string     `gorm:"size:512;default:''" json:"converted_question"`
	ConvertedAt       *time.Time `json:"converted_at"`
	CreatedAt         time.Time  `json:"created_at"`
}

// PushToken 推送注册（M3 移动 SDK 配套 §10 #5）：移动端 SDK 经免认证访客端点
// 上报的 FCM/APNs token，按 session 绑定租户 scope（继承 session 行）。同
// (session_id, platform) 幂等：重复注册保活、换 token 更新——SDK 侧 provider
// 每次 connect 均可重报。token 为敏感面不回显（json:"-"，响应走摘要 DTO）；
// 下发侧按 session 查最新 token（平台 → 通道映射：ios=APNs，android=FCM）。
type PushToken struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TenantID    string    `gorm:"index:idx_push_tokens_scope" json:"tenant_id"`
	WorkspaceID string    `gorm:"index:idx_push_tokens_scope" json:"workspace_id"`
	SessionID   string    `gorm:"index:idx_push_tokens_session_platform;default:''" json:"session_id"`
	Platform    string    `gorm:"index:idx_push_tokens_session_platform;size:16" json:"platform"` // ios|android
	Token       string    `gorm:"type:text" json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
