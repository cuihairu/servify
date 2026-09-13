package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"servify/apps/server/internal/models"
)

type Service struct {
	repo              Repository
	webhookDispatcher WebhookDispatcher
	timerBatchSize    int
}

// WebhookDispatcher 由 webhook 模块实现：触发器命中时一次性外呼（不退避重试，
// 失败直接进 AutomationRun failed，由调用方在投递日志里重放）。
type WebhookDispatcher interface {
	Dispatch(ctx context.Context, url, secret string, payload map[string]interface{}) error
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, timerBatchSize: 50}
}

// SetWebhookDispatcher 注入 call_webhook 动作的外呼实现（可选能力，未注入则该动作报错）。
func (s *Service) SetWebhookDispatcher(d WebhookDispatcher) { s.webhookDispatcher = d }

// SetTimerBatchSize 覆盖单轮 timer 扫描的执行单上限（默认 50）。
func (s *Service) SetTimerBatchSize(n int) {
	if n > 0 {
		s.timerBatchSize = n
	}
}

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
	if err := validateDelayActions(req.Actions, false); err != nil {
		return nil, err
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
	var triggers []models.AutomationTrigger
	if req.TriggerID > 0 {
		// 手动运行：按 ID 定位单个触发器（跳过事件白名单，事件取触发器自身定义）
		all, err := s.repo.ListTriggers(ctx)
		if err != nil {
			return nil, err
		}
		for _, trig := range all {
			if trig.ID == req.TriggerID {
				triggers = []models.AutomationTrigger{trig}
				break
			}
		}
		if len(triggers) == 0 {
			return nil, fmt.Errorf("trigger not found: %d", req.TriggerID)
		}
		req.Event = triggers[0].Event
	} else {
		req.Event = normalizeEvent(req.Event)
		if !isSupportedEvent(req.Event) {
			return nil, fmt.Errorf("unsupported event: %s", req.Event)
		}
		var err error
		triggers, err = s.repo.ListActiveTriggersByEvent(ctx, req.Event)
		if err != nil {
			return nil, err
		}
	}
	if len(req.TicketIDs) == 0 {
		return nil, fmt.Errorf("ticket_ids required")
	}
	if len(req.TicketIDs) > 500 {
		return nil, fmt.Errorf("too many ticket_ids (max 500)")
	}
	resp := &BatchRunResponse{Event: req.Event, DryRun: req.DryRun}
	for _, ticketID := range req.TicketIDs {
		ticket, err := s.repo.GetTicket(ctx, ticketID)
		if err != nil {
			continue
		}
		var matched []uint
		for _, trig := range triggers {
			evt := Event{Type: trig.Event, TicketID: ticket.ID}
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
	for i, act := range actions {
		if act.Type == "delay" {
			// delay 只允许作为最后一个动作（配置期已校验，这里防历史脏数据）：
			// 到期后的动作从嵌套列表快照执行，由 timer worker 驱动。
			if i != len(actions)-1 {
				_ = s.repo.RecordRun(ctx, trig.ID, evt.TicketID, "failed", "delay must be the last action")
				return false
			}
			minutes, err := s.scheduleTimer(ctx, trig, evt, act)
			if err != nil {
				_ = s.repo.RecordRun(ctx, trig.ID, evt.TicketID, "failed", err.Error())
				return false
			}
			_ = s.repo.RecordRun(ctx, trig.ID, evt.TicketID, "delayed", fmt.Sprintf("delayed: timer due in %d minutes", minutes))
			return true
		}
		if err := s.executeAction(ctx, act, ticket); err != nil {
			_ = s.repo.RecordRun(ctx, trig.ID, evt.TicketID, "failed", err.Error())
			return false
		}
	}
	_ = s.repo.RecordRun(ctx, trig.ID, evt.TicketID, "success", "")
	return true
}

// scheduleTimer 校验 delay 参数并入队到期执行单，返回延后分钟数。
func (s *Service) scheduleTimer(ctx context.Context, trig models.AutomationTrigger, evt Event, act TriggerAction) (int, error) {
	minutes, err := delayMinutes(act.Params)
	if err != nil {
		return 0, err
	}
	nested, err := decodeActions(act.Params["actions"])
	if err != nil {
		return 0, err
	}
	if len(nested) == 0 {
		return 0, fmt.Errorf("delay actions must not be empty")
	}
	if err := validateDelayActions(nested, true); err != nil {
		return 0, err
	}
	payload, err := json.Marshal(nested)
	if err != nil {
		return 0, fmt.Errorf("invalid delay actions: %w", err)
	}
	if err := s.repo.CreateTimer(ctx, &models.AutomationTimer{
		TriggerID:   trig.ID,
		TicketID:    evt.TicketID,
		ActionsJSON: string(payload),
		DueAt:       time.Now().Add(time.Duration(minutes) * time.Minute),
		Status:      TimerStatusPending,
		CreatedAt:   time.Now(),
	}); err != nil {
		return 0, err
	}
	return minutes, nil
}

// ProcessDueTimers 扫描到期的 delay 执行单并执行，返回处理的条数。
// 先用 CompleteTimer 把执行单从 pending 乐观翻转为 done，抢到才执行——
// 多实例并发下恰好一次；执行失败不回滚，写 last_error 与 failed 审计供人工重放。
func (s *Service) ProcessDueTimers(ctx context.Context, now time.Time) int {
	limit := s.timerBatchSize
	if limit <= 0 {
		limit = 50
	}
	timers, err := s.repo.ClaimDueTimers(ctx, now, limit)
	if err != nil || len(timers) == 0 {
		return 0
	}
	processed := 0
	for i := range timers {
		timer := &timers[i]
		if !s.repo.CompleteTimer(ctx, timer.ID, now) {
			// 已被其他实例抢占执行
			continue
		}
		processed++
		s.runTimerActions(ctx, timer)
	}
	return processed
}

// runTimerActions 执行单张到期执行单快照里的嵌套动作；
// 执行前重载工单——delay 期间优先级/标签可能已被其他自动化修改。
func (s *Service) runTimerActions(ctx context.Context, timer *models.AutomationTimer) {
	fail := func(err error) {
		_ = s.repo.UpdateTimerLastError(ctx, timer.ID, err.Error())
		_ = s.repo.RecordRun(ctx, timer.TriggerID, timer.TicketID, "failed", "delayed actions failed: "+err.Error())
	}
	actions := []TriggerAction{}
	if err := json.Unmarshal([]byte(timer.ActionsJSON), &actions); err != nil {
		fail(fmt.Errorf("invalid delayed actions: %w", err))
		return
	}
	ticket, err := s.repo.GetTicket(ctx, timer.TicketID)
	if err != nil {
		fail(fmt.Errorf("ticket not found: %w", err))
		return
	}
	for _, act := range actions {
		if act.Type == "delay" {
			// 配置期已拒绝嵌套 delay；历史脏数据到这里只报错不递归入队
			fail(fmt.Errorf("nested delay is not supported"))
			return
		}
		if err := s.executeAction(ctx, act, ticket); err != nil {
			fail(err)
			return
		}
	}
	_ = s.repo.RecordRun(ctx, timer.TriggerID, timer.TicketID, "success", "delayed: actions executed")
}

// validateDelayActions 是 delay 动作的第一道闸（配置期）：delay 必须是最后一个
// 顶层动作、参数完整、嵌套列表非空且不允许再嵌套 delay；其余动作类型仍放行
// （未知类型沿用现状，留到执行期报错进 failed 审计）。nested=true 表示正在校验
// delay 的嵌套列表。
func validateDelayActions(actions []TriggerAction, nested bool) error {
	for i, act := range actions {
		if act.Type != "delay" {
			continue
		}
		if nested {
			return fmt.Errorf("nested delay is not supported")
		}
		if i != len(actions)-1 {
			return fmt.Errorf("delay must be the last action")
		}
		if _, err := delayMinutes(act.Params); err != nil {
			return err
		}
		kids, err := decodeActions(act.Params["actions"])
		if err != nil {
			return err
		}
		if len(kids) == 0 {
			return fmt.Errorf("delay actions must not be empty")
		}
		if err := validateDelayActions(kids, true); err != nil {
			return err
		}
	}
	return nil
}

// delayMinutes 取 delay 的 minutes 参数；JSON 反序列化后数值一律是 float64，
// Go 内部构造则可能是 int，两种都收且必须是正整数。
func delayMinutes(params map[string]interface{}) (int, error) {
	raw, ok := params["minutes"]
	if !ok || raw == nil {
		return 0, fmt.Errorf("delay requires minutes param")
	}
	switch v := raw.(type) {
	case float64:
		if v <= 0 || v != float64(int(v)) {
			return 0, fmt.Errorf("delay minutes must be a positive integer")
		}
		return int(v), nil
	case int:
		if v <= 0 {
			return 0, fmt.Errorf("delay minutes must be positive")
		}
		return v, nil
	default:
		return 0, fmt.Errorf("delay minutes must be a number")
	}
}

// decodeActions 归一化 actions 参数：JSON 反序列化产物是 []interface{}，
// Go 内部调用则是 []TriggerAction，两种都收。
func decodeActions(raw interface{}) ([]TriggerAction, error) {
	switch v := raw.(type) {
	case []TriggerAction:
		return v, nil
	case []interface{}:
		payload, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("invalid delay actions: %w", err)
		}
		out := make([]TriggerAction, 0, len(v))
		if err := json.Unmarshal(payload, &out); err != nil {
			return nil, fmt.Errorf("invalid delay actions: %w", err)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("delay requires actions param")
	}
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
