package models

import "time"

// AutomationTrigger 自动化触发器定义
type AutomationTrigger struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	Name       string    `gorm:"unique;not null" json:"name"`
	Event      string    `gorm:"not null" json:"event"`       // ticket_created, ticket_updated, sla_violation
	Conditions string    `gorm:"type:text" json:"conditions"` // JSON: [{field,op,value}]
	Actions    string    `gorm:"type:text" json:"actions"`    // JSON: [{type,params}]
	Active     bool      `gorm:"default:true" json:"active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// AutomationRun 执行记录用于审计
type AutomationRun struct {
	ID        uint              `gorm:"primaryKey" json:"id"`
	TriggerID uint              `gorm:"index" json:"trigger_id"`
	TicketID  uint              `gorm:"index" json:"ticket_id"`
	Status    string            `gorm:"index" json:"status"` // success, skipped, failed, delayed
	Message   string            `gorm:"type:text" json:"message"`
	CreatedAt time.Time         `json:"created_at"`
	Trigger   AutomationTrigger `gorm:"foreignKey:TriggerID" json:"trigger,omitempty"`
}

// AutomationTimer 是 delay 动作产生的到期执行单：入队时快照嵌套动作，
// 到期由 timer worker 乐观抢占执行（多实例下恰好一次），不回查触发器当前定义。
type AutomationTimer struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	TriggerID   uint       `gorm:"index" json:"trigger_id"`
	TicketID    uint       `gorm:"index" json:"ticket_id"`
	ActionsJSON string     `gorm:"type:text" json:"actions_json"` // 到期执行的嵌套动作 JSON: [{type,params}]
	DueAt       time.Time  `gorm:"index:idx_automation_timers_status_due_at,priority:2" json:"due_at"`
	Status      string     `gorm:"index:idx_automation_timers_status_due_at,priority:1;default:pending" json:"status"` // pending, done
	LastError   string     `gorm:"type:text" json:"last_error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ExecutedAt  *time.Time `json:"executed_at,omitempty"`
}
