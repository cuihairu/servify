package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"servify/apps/server/internal/models"
)

type Service struct {
	repo              Repository
	webhookDispatcher WebhookDispatcher
}

// WebhookDispatcher 由 webhook 模块实现：触发器命中时一次性外呼（不退避重试，
// 失败直接进 AutomationRun failed，由调用方在投递日志里重放）。
type WebhookDispatcher interface {
	Dispatch(ctx context.Context, url, secret string, payload map[string]interface{}) error
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// SetWebhookDispatcher 注入 call_webhook 动作的外呼实现（可选能力，未注入则该动作报错）。
func (s *Service) SetWebhookDispatcher(d WebhookDispatcher) { s.webhookDispatcher = d }

func (s *Service) HandleEvent(ctx context.Context, evt Event) {
	triggers, err := s.repo.ListActiveTriggersByEvent(ctx, normalizeEvent(evt.Type))
	if err != nil || len(triggers) == 0 {
		return
	}
	var ticket *TicketView
	if evt.TicketID != 0 {
		ticket, _ = s.repo.GetTicket(ctx, evt.TicketID)
	}
	for _, trig := range triggers {
		_ = s.applyTrigger(ctx, trig, evt, ticket, false)
	}
}

func (s *Service) HandleBusEvent(ctx context.Context, eventName string, aggregateID string, payload interface{}) {
	var ticketID uint
	if aggregateID != "" {
		if parsed, err := strconv.ParseUint(aggregateID, 10, 64); err == nil {
			ticketID = uint(parsed)
		}
	}
	s.HandleEvent(ctx, Event{Type: eventName, TicketID: ticketID, Payload: payload})
}

func (s *Service) ListTriggers(ctx context.Context) ([]models.AutomationTrigger, error) {
	return s.repo.ListTriggers(ctx)
}

func (s *Service) CreateTrigger(ctx context.Context, req TriggerRequest) (*models.AutomationTrigger, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name required")
	}
	req.Event = normalizeEvent(req.Event)
	if !isSupportedEvent(req.Event) {
		return nil, fmt.Errorf("unsupported event: %s", req.Event)
	}
	return s.repo.CreateTrigger(ctx, req)
}

func (s *Service) DeleteTrigger(ctx context.Context, id uint) error {
	return s.repo.DeleteTrigger(ctx, id)
}

func (s *Service) ListRuns(ctx context.Context, query RunListQuery) ([]models.AutomationRun, int64, error) {
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 {
		query.PageSize = 20
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}
	return s.repo.ListRuns(ctx, query)
}

func (s *Service) BatchRun(ctx context.Context, req BatchRunRequest) (*BatchRunResponse, error) {
	req.Event = normalizeEvent(req.Event)
	if !isSupportedEvent(req.Event) {
		return nil, fmt.Errorf("unsupported event: %s", req.Event)
	}
	if len(req.TicketIDs) == 0 {
		return nil, fmt.Errorf("ticket_ids required")
	}
	if len(req.TicketIDs) > 500 {
		return nil, fmt.Errorf("too many ticket_ids (max 500)")
	}
	triggers, err := s.repo.ListActiveTriggersByEvent(ctx, req.Event)
	if err != nil {
		return nil, err
	}
	resp := &BatchRunResponse{Event: req.Event, DryRun: req.DryRun}
	for _, ticketID := range req.TicketIDs {
		ticket, err := s.repo.GetTicket(ctx, ticketID)
		if err != nil {
			continue
		}
		evt := Event{Type: req.Event, TicketID: ticket.ID}
		var matched []uint
		for _, trig := range triggers {
			ok := s.applyTrigger(ctx, trig, evt, ticket, req.DryRun)
			if ok {
				matched = append(matched, trig.ID)
			}
		}
		if len(matched) > 0 {
			resp.Matches += len(matched)
		}
		resp.Results = append(resp.Results, BatchRunTicketResult{TicketID: ticket.ID, MatchedTriggerIDs: matched})
		resp.TicketsProcessed++
	}
	return resp, nil
}

func (s *Service) applyTrigger(ctx context.Context, trig models.AutomationTrigger, evt Event, ticket *TicketView, dryRun bool) bool {
	conds := []TriggerCondition{}
	if trig.Conditions != "" {
		if err := json.Unmarshal([]byte(trig.Conditions), &conds); err != nil {
			return false
		}
	}
	attrs := map[string]interface{}{}
	if ticket != nil {
		attrs["ticket.priority"] = ticket.Priority
		attrs["ticket.status"] = ticket.Status
		attrs["ticket.tags"] = ticket.Tags
	}
	if violation, ok := evt.Payload.(*models.SLAViolation); ok {
		attrs["violation.type"] = violation.ViolationType
	}
	for _, cond := range conds {
		if !EvaluateCondition(cond, attrs) {
			return false
		}
	}
	if dryRun {
		return true
	}
	actions := []TriggerAction{}
	if trig.Actions != "" {
		if err := json.Unmarshal([]byte(trig.Actions), &actions); err != nil {
			return false
		}
	}
	for _, act := range actions {
		if err := s.executeAction(ctx, act, ticket); err != nil {
			_ = s.repo.RecordRun(ctx, trig.ID, evt.TicketID, "failed", err.Error())
			return false
		}
	}
	_ = s.repo.RecordRun(ctx, trig.ID, evt.TicketID, "success", "")
	return true
}

func (s *Service) MatchTrigger(ctx context.Context, trig models.AutomationTrigger, evt Event, ticket *TicketView, dryRun bool) bool {
	return s.applyTrigger(ctx, trig, evt, ticket, dryRun)
}

func (s *Service) executeAction(ctx context.Context, act TriggerAction, ticket *TicketView) error {
	switch act.Type {
	case "set_priority":
		if ticket == nil {
			return fmt.Errorf("ticket not loaded")
		}
		val, _ := act.Params["priority"].(string)
		if val == "" {
			return fmt.Errorf("priority param required")
		}
		return s.repo.UpdateTicketPriority(ctx, ticket.ID, val)
	case "add_tag":
		if ticket == nil {
			return fmt.Errorf("ticket not loaded")
		}
		val, _ := act.Params["tag"].(string)
		if val == "" {
			return fmt.Errorf("tag param required")
		}
		tags := splitTags(ticket.Tags)
		if containsTag(tags, val) {
			// 按 token 精确判重：已是既存标签时不再写同一值
			return nil
		}
		tags = append(tags, val)
		return s.repo.UpdateTicketTags(ctx, ticket.ID, strings.Join(tags, ","))
	case "remove_tag":
		if ticket == nil {
			return fmt.Errorf("ticket not loaded")
		}
		val, _ := act.Params["tag"].(string)
		if val == "" {
			return fmt.Errorf("tag param required")
		}
		tags := splitTags(ticket.Tags)
		if !containsTag(tags, val) {
			return nil
		}
		kept := make([]string, 0, len(tags))
		for _, t := range tags {
			if t != val {
				kept = append(kept, t)
			}
		}
		return s.repo.UpdateTicketTags(ctx, ticket.ID, strings.Join(kept, ","))
	case "escalate_priority":
		if ticket == nil {
			return fmt.Errorf("ticket not loaded")
		}
		rank := priorityRank(ticket.Priority)
		if rank < 0 {
			return fmt.Errorf("unknown current priority: %s", ticket.Priority)
		}
		if rank >= len(priorityOrder)-1 {
			// 已是最高级：默认 no-op，params.wrap=true 才回绕到 low
			if wrap, _ := act.Params["wrap"].(bool); wrap {
				return s.repo.UpdateTicketPriority(ctx, ticket.ID, priorityOrder[0])
			}
			return nil
		}
		return s.repo.UpdateTicketPriority(ctx, ticket.ID, priorityOrder[rank+1])
	case "add_comment":
		if ticket == nil {
			return fmt.Errorf("ticket not loaded")
		}
		content, _ := act.Params["content"].(string)
		if content == "" {
			return fmt.Errorf("content required")
		}
		return s.repo.CreateTicketComment(ctx, ticket.ID, content)
	case "notify_log":
		return nil
	case "call_webhook":
		url, _ := act.Params["url"].(string)
		if url == "" {
			return fmt.Errorf("url param required")
		}
		if s.webhookDispatcher == nil {
			return fmt.Errorf("webhook dispatcher not configured")
		}
		secret, _ := act.Params["secret"].(string)
		payload := defaultWebhookPayload(ticket)
		if raw, ok := act.Params["payload"]; ok && raw != nil {
			if obj, isObj := raw.(map[string]interface{}); isObj {
				payload = obj
			} else {
				payload = map[string]interface{}{"payload": raw}
			}
		}
		return s.webhookDispatcher.Dispatch(ctx, url, secret, payload)
	default:
		return fmt.Errorf("unsupported action type: %s", act.Type)
	}
}

// priorityOrder 是工单优先级从低到高的固定序；escalate_priority 按此升一级。
var priorityOrder = []string{"low", "normal", "high", "urgent"}

func priorityRank(priority string) int {
	for i, p := range priorityOrder {
		if p == priority {
			return i
		}
	}
	return -1
}

// splitTags 把逗号分隔的标签串拆成去空白、去空项的标签列表，
// 供 add/remove_tag 按 token 精确判重（子串匹配会把 urgent 误判已含于 not_urgent）。
func splitTags(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	tags := make([]string, 0, len(parts))
	for _, p := range parts {
		if tag := strings.TrimSpace(p); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// defaultWebhookPayload 未显式给 payload 参数时，用工单快照作为请求体
// （JSON 序列化保持与 webhook 订阅投递相同的字段名）。
func defaultWebhookPayload(ticket *models.Ticket) map[string]interface{} {
	if ticket == nil {
		return map[string]interface{}{}
	}
	raw, err := json.Marshal(ticket)
	if err != nil {
		return map[string]interface{}{"ticket_id": ticket.ID}
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]interface{}{"ticket_id": ticket.ID}
	}
	return out
}

func EvaluateCondition(cond TriggerCondition, attrs map[string]interface{}) bool {
	val, ok := attrs[cond.Field]
	if !ok {
		return false
	}
	actual := fmt.Sprintf("%v", val)
	expected := fmt.Sprintf("%v", cond.Value)
	switch cond.Op {
	case "eq":
		return actual == expected
	case "neq":
		return actual != expected
	case "contains":
		return strings.Contains(actual, expected)
	default:
		return false
	}
}

func normalizeEvent(event string) string {
	switch event {
	case "ticket_created":
		return "ticket.created"
	case "ticket_updated":
		return "ticket.updated"
	case "sla_violation":
		return "sla.violation"
	default:
		return event
	}
}

func isSupportedEvent(event string) bool {
	switch event {
	case "ticket.created", "ticket.updated", "ticket.closed", "ticket.assigned", "conversation.created", "conversation.message_received", "routing.agent_assigned", "routing.transfer_completed", "sla.violation":
		return true
	default:
		return false
	}
}

func IsSupportedEvent(event string) bool {
	return isSupportedEvent(normalizeEvent(event))
}
