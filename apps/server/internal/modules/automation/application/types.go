package application

import "servify/apps/server/internal/models"

type Event struct {
	Type     string
	TicketID uint
	Payload  interface{}
}

type TriggerCondition struct {
	Field string      `json:"field"`
	Op    string      `json:"op"`
	Value interface{} `json:"value"`
}

type TriggerAction struct {
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params"`
}

// AutomationTimer 的状态机：pending →（worker 乐观翻转）done。
// failed 不改状态，只写 last_error 与 AutomationRun 审计。
const (
	TimerStatusPending = "pending"
	TimerStatusDone    = "done"
)

type TriggerRequest struct {
	Name       string             `json:"name"`
	Event      string             `json:"event"`
	Conditions []TriggerCondition `json:"conditions"`
	Actions    []TriggerAction    `json:"actions"`
	Active     *bool              `json:"active"`
}

type RunListQuery struct {
	Page      int
	PageSize  int
	Status    string
	TriggerID uint
	TicketID  uint
}

type BatchRunRequest struct {
	Event     string `json:"event"`
	TicketIDs []uint `json:"ticket_ids"`
	DryRun    bool   `json:"dry_run"`
}

type BatchRunTicketResult struct {
	TicketID          uint   `json:"ticket_id"`
	MatchedTriggerIDs []uint `json:"matched_trigger_ids"`
}

type BatchRunResponse struct {
	Event            string                 `json:"event"`
	DryRun           bool                   `json:"dry_run"`
	TicketsProcessed int                    `json:"tickets_processed"`
	Matches          int                    `json:"matches"`
	Results          []BatchRunTicketResult `json:"results"`
}

type TicketView = models.Ticket
