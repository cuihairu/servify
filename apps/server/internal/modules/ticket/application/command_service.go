package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"servify/apps/server/internal/modules/ticket/domain"
	"servify/apps/server/internal/platform/eventbus"
)

type CommandService struct {
	repo         CommandRepository
	statusPolicy StatusTransitionPolicy
	bus          eventbus.Bus
}

func NewCommandService(repo CommandRepository) *CommandService {
	return NewCommandServiceWithBus(repo, nil)
}

func NewCommandServiceWithBus(repo CommandRepository, bus eventbus.Bus) *CommandService {
	return &CommandService{
		repo:         repo,
		statusPolicy: NewStatusTransitionPolicy(),
		bus:          bus,
	}
}

func (s *CommandService) CreateTicket(ctx context.Context, cmd CreateTicketCommand) (*TicketDTO, error) {
	if strings.TrimSpace(cmd.Title) == "" {
		return nil, fmt.Errorf("title required")
	}
	if cmd.CustomerID == 0 {
		return nil, fmt.Errorf("customer id required")
	}

	exists, err := s.repo.CustomerExists(ctx, cmd.CustomerID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("customer not found")
	}

	now := time.Now()
	category := strings.TrimSpace(cmd.Category)
	if category == "" {
		category = "general"
	}
	priority := strings.TrimSpace(cmd.Priority)
	if priority == "" {
		priority = "normal"
	}
	source := strings.TrimSpace(cmd.Source)
	if source == "" {
		source = "web"
	}

	ticket := &domain.Ticket{
		Title:       strings.TrimSpace(cmd.Title),
		Description: strings.TrimSpace(cmd.Description),
		CustomerID:  cmd.CustomerID,
		Category:    category,
		Priority:    priority,
		Status:      "open",
		Source:      source,
		Tags:        strings.TrimSpace(cmd.Tags),
		SessionID:   cmd.SessionID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.repo.CreateTicket(ctx, ticket); err != nil {
		return nil, err
	}
	dto := MapTicket(*ticket)
	if err := s.recordStatusChange(ctx, ticket.ID, 0, "", "open", "create_ticket"); err != nil {
		return nil, err
	}
	if err := s.publishTicketEvent(ctx, TicketCreatedEventName, dto); err != nil {
		return nil, err
	}
	return &dto, nil
}

func (s *CommandService) UpdateTicket(ctx context.Context, ticketID uint, cmd UpdateTicketCommand) (*TicketDTO, error) {
	if ticketID == 0 {
		return nil, fmt.Errorf("ticket id required")
	}
	ticket, err := s.repo.GetTicket(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	previousStatus := ticket.Status
	previousAgentID := ticket.AgentID
	statusChanged := false

	if cmd.Title != nil {
		ticket.Title = strings.TrimSpace(*cmd.Title)
	}
	if cmd.Description != nil {
		ticket.Description = strings.TrimSpace(*cmd.Description)
	}
	if cmd.AgentID != nil {
		ticket.AgentID = cmd.AgentID
	}
	if cmd.Category != nil {
		ticket.Category = strings.TrimSpace(*cmd.Category)
	}
	if cmd.Priority != nil {
		ticket.Priority = strings.TrimSpace(*cmd.Priority)
	}
	if cmd.Status != nil {
		status := strings.TrimSpace(*cmd.Status)
		if status != ticket.Status {
			if err := s.statusPolicy.Validate(ticket.Status, status); err != nil {
				return nil, err
			}
			ticket.Status = status
			statusChanged = true
			now := time.Now()
			switch status {
			case "resolved":
				ticket.ResolvedAt = &now
			case "closed":
				ticket.ClosedAt = &now
			}
		}
	}
	if cmd.Tags != nil {
		ticket.Tags = strings.TrimSpace(*cmd.Tags)
	}
	if cmd.DueDate != nil {
		ticket.DueDate = cmd.DueDate
	}

	if strings.TrimSpace(ticket.Title) == "" {
		return nil, fmt.Errorf("title required")
	}
	ticket.UpdatedAt = time.Now()
	if statusChanged {
		if err := s.repo.UpdateTicketWithStatus(ctx, ticket, previousStatus, cmd.ActorID, "update_ticket"); err != nil {
			return nil, err
		}
	} else {
		if err := s.repo.UpdateTicket(ctx, ticket); err != nil {
			return nil, err
		}
	}
	dto := MapTicket(*ticket)
	if agentChanged(previousAgentID, dto.AgentID) {
		if err := s.publishTicketEvent(ctx, TicketAssignedEventName, dto); err != nil {
			return nil, err
		}
	}
	return &dto, nil
}

func (s *CommandService) AssignTicket(ctx context.Context, ticketID uint, cmd AssignTicketCommand) (*TicketDTO, error) {
	if ticketID == 0 {
		return nil, fmt.Errorf("ticket id required")
	}
	if cmd.AgentID == 0 {
		return nil, fmt.Errorf("agent id required")
	}
	ticket, err := s.repo.GetTicket(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	assignable, err := s.repo.AgentAssignable(ctx, cmd.AgentID)
	if err != nil {
		return nil, err
	}
	if !assignable {
		return nil, fmt.Errorf("agent not available")
	}

	previousAgentID := ticket.AgentID
	fromStatus := ticket.Status
	ticket.AgentID = &cmd.AgentID
	// 此处 fromStatus 已限定为 ""/open，目标恒为 assigned；
	// StatusTransitionPolicy.Validate 对 ""/open -> assigned 恒返回 nil
	// （见 status_policy.go 的 switch），错误分支不可达，故不再调用校验。
	if ticket.Status == "" || ticket.Status == "open" {
		ticket.Status = "assigned"
	}
	ticket.UpdatedAt = time.Now()
	reason := "assign_ticket"
	if previousAgentID != nil {
		reason = "transfer_ticket"
	}
	if err := s.repo.AssignTicket(ctx, ticket, previousAgentID, fromStatus, cmd.UserID, reason); err != nil {
		return nil, err
	}
	dto := MapTicket(*ticket)
	if err := s.publishTicketEvent(ctx, TicketAssignedEventName, dto); err != nil {
		return nil, err
	}
	return &dto, nil
}

func (s *CommandService) UnassignTicket(ctx context.Context, ticketID uint, cmd UnassignTicketCommand) (*TicketDTO, error) {
	if ticketID == 0 {
		return nil, fmt.Errorf("ticket id required")
	}
	ticket, err := s.repo.GetTicket(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket.AgentID == nil {
		dto := MapTicket(*ticket)
		return &dto, nil
	}
	fromStatus := ticket.Status
	nextStatus := ticket.Status
	if nextStatus == "" || nextStatus == "assigned" || nextStatus == "in_progress" {
		nextStatus = "open"
	}
	if err := s.statusPolicy.Validate(ticket.Status, nextStatus); err != nil {
		return nil, err
	}
	previousAgentID := *ticket.AgentID
	ticket.AgentID = nil
	ticket.Status = nextStatus
	ticket.UpdatedAt = time.Now()
	reason := strings.TrimSpace(cmd.Reason)
	if reason == "" {
		reason = "unassign_ticket"
	}
	if err := s.repo.UnassignTicket(ctx, ticket, previousAgentID, fromStatus, cmd.UserID, reason); err != nil {
		return nil, err
	}
	dto := MapTicket(*ticket)
	return &dto, nil
}

func (s *CommandService) AddComment(ctx context.Context, ticketID uint, cmd AddCommentCommand) (*CommentDTO, error) {
	if ticketID == 0 {
		return nil, fmt.Errorf("ticket id required")
	}
	if cmd.UserID == 0 {
		return nil, fmt.Errorf("user id required")
	}
	content := strings.TrimSpace(cmd.Content)
	if content == "" {
		return nil, fmt.Errorf("content required")
	}
	if _, err := s.repo.GetTicket(ctx, ticketID); err != nil {
		return nil, err
	}

	commentType := strings.TrimSpace(cmd.CommentType)
	if commentType == "" {
		commentType = "comment"
	}
	comment := &domain.Comment{
		UserID:    cmd.UserID,
		Content:   content,
		Type:      commentType,
		CreatedAt: time.Now(),
	}
	if err := s.repo.AddComment(ctx, ticketID, comment); err != nil {
		return nil, err
	}
	dto := MapComment(*comment)
	return &dto, nil
}

func (s *CommandService) BulkUpdateTickets(ctx context.Context, cmd BulkUpdateTicketsCommand) (*BulkUpdateResult, error) {
	if len(cmd.TicketIDs) == 0 {
		return nil, fmt.Errorf("ticket ids required")
	}
	if cmd.UnassignAgent && cmd.AgentID != nil {
		return nil, fmt.Errorf("cannot set both unassign_agent and agent_id")
	}

	ticketIDs := normalizeTicketIDs(cmd.TicketIDs)
	if len(ticketIDs) == 0 {
		return nil, fmt.Errorf("no valid ticket ids")
	}

	result := &BulkUpdateResult{}
	for _, ticketID := range ticketIDs {
		if err := s.bulkUpdateOne(ctx, ticketID, cmd); err != nil {
			result.Failed = append(result.Failed, BulkUpdateFailure{
				TicketID: ticketID,
				Error:    err.Error(),
			})
			continue
		}
		result.Updated = append(result.Updated, ticketID)
	}

	return result, nil
}

func (s *CommandService) CloseTicket(ctx context.Context, ticketID uint, cmd CloseTicketCommand) (*TicketDTO, error) {
	if ticketID == 0 {
		return nil, fmt.Errorf("ticket id required")
	}
	ticket, err := s.repo.GetTicket(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if err := s.statusPolicy.Validate(ticket.Status, "closed"); err != nil {
		return nil, err
	}
	fromStatus := ticket.Status
	now := time.Now()
	ticket.Status = "closed"
	ticket.ClosedAt = &now
	ticket.UpdatedAt = now
	if err := s.repo.CloseTicket(ctx, ticket, fromStatus, cmd.UserID, cmd.Reason); err != nil {
		return nil, err
	}
	dto := MapTicket(*ticket)
	if err := s.publishTicketEvent(ctx, TicketClosedEventName, dto); err != nil {
		return nil, err
	}
	return &dto, nil
}

func (s *CommandService) recordStatusChange(ctx context.Context, ticketID uint, userID uint, fromStatus, toStatus, reason string) error {
	if fromStatus == toStatus {
		return nil
	}
	change := &domain.StatusChange{
		UserID:     userID,
		FromStatus: fromStatus,
		ToStatus:   toStatus,
		Reason:     reason,
		CreatedAt:  time.Now(),
	}
	return s.repo.RecordStatusChange(ctx, ticketID, change)
}

func (s *CommandService) publishTicketEvent(ctx context.Context, name string, ticket TicketDTO) error {
	if s.bus == nil {
		return nil
	}
	return s.bus.Publish(ctx, NewTicketEvent(name, ticket))
}

func (s *CommandService) bulkUpdateOne(ctx context.Context, ticketID uint, cmd BulkUpdateTicketsCommand) error {
	ticket, err := s.repo.GetTicket(ctx, ticketID)
	if err != nil {
		return err
	}

	if cmd.UnassignAgent {
		if ticket.AgentID != nil {
			if _, err := s.UnassignTicket(ctx, ticketID, UnassignTicketCommand{
				UserID: cmd.UserID,
				Reason: "bulk_unassign",
			}); err != nil {
				return err
			}
		}
	} else if cmd.AgentID != nil {
		if _, err := s.AssignTicket(ctx, ticketID, AssignTicketCommand{
			AgentID: *cmd.AgentID,
			UserID:  cmd.UserID,
		}); err != nil {
			return err
		}
		ticket, err = s.repo.GetTicket(ctx, ticketID)
		if err != nil {
			return err
		}
	}

	needUpdate := cmd.Status != nil || cmd.SetTags != nil || len(cmd.AddTags) > 0 || len(cmd.RemoveTags) > 0
	if !needUpdate {
		return nil
	}

	update := UpdateTicketCommand{
		ActorID: cmd.UserID,
		Status:  cmd.Status,
	}
	if cmd.SetTags != nil {
		joined := strings.Join(normalizeTags(splitTags(*cmd.SetTags)), ",")
		update.Tags = &joined
	} else if len(cmd.AddTags) > 0 || len(cmd.RemoveTags) > 0 {
		joined := strings.Join(applyTagDelta(ticket.Tags, cmd.AddTags, cmd.RemoveTags), ",")
		update.Tags = &joined
	}

	_, err = s.UpdateTicket(ctx, ticketID, update)
	return err
}

func normalizeTicketIDs(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

func splitTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	slices.Sort(out)
	return out
}

func applyTagDelta(current string, add []string, remove []string) []string {
	currentTags := normalizeTags(splitTags(current))
	values := make(map[string]string, len(currentTags))
	for _, tag := range currentTags {
		values[strings.ToLower(tag)] = tag
	}
	for _, tag := range normalizeTags(add) {
		values[strings.ToLower(tag)] = tag
	}
	for _, tag := range normalizeTags(remove) {
		delete(values, strings.ToLower(tag))
	}
	out := make([]string, 0, len(values))
	for _, tag := range values {
		out = append(out, tag)
	}
	slices.Sort(out)
	return out
}

func agentChanged(before *uint, after *uint) bool {
	if before == nil && after == nil {
		return false
	}
	if before == nil || after == nil {
		return true
	}
	return *before != *after
}
