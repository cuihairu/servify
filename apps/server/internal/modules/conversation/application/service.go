package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/modules/conversation/domain"
	svcmetrics "servify/apps/server/internal/observability/metrics"
)

type Service struct {
	repo      ConversationRepository
	publisher EventPublisher
	now       func() time.Time
	metrics   *svcmetrics.BusinessMetrics
}

func NewService(repo ConversationRepository, publisher EventPublisher) *Service {
	return &Service{
		repo:      repo,
		publisher: publisher,
		now:       time.Now,
	}
}

// AttachBusinessMetrics 注入进程级业务指标（nil 安全，可链式）。
// 会话创建成功后计数 conversations_created_total{tenant_id, channel}。
func (s *Service) AttachBusinessMetrics(m *svcmetrics.BusinessMetrics) *Service {
	if s == nil {
		return s
	}
	s.metrics = m
	return s
}

func (s *Service) CreateConversation(ctx context.Context, cmd CreateConversationCommand) (*ConversationDTO, error) {
	if strings.TrimSpace(cmd.ConversationID) == "" {
		return nil, fmt.Errorf("conversation_id required")
	}
	now := s.now()
	conversation := &domain.Conversation{
		ID:           cmd.ConversationID,
		CustomerID:   cmd.CustomerID,
		Status:       domain.ConversationStatusActive,
		Subject:      strings.TrimSpace(cmd.Subject),
		Channel:      cmd.Channel,
		Participants: append([]domain.Participant(nil), cmd.Participants...),
		StartedAt:    now,
	}
	if err := s.repo.CreateConversation(ctx, conversation); err != nil {
		return nil, err
	}
	// tenant 体系尚未落到会话模型，先按单租户口径记 default。
	s.metrics.RecordConversationCreated("default", cmd.Channel.Channel)
	s.publish(ctx, ConversationCreatedEventName, conversation.ID, MapConversation(*conversation))
	dto := MapConversation(*conversation)
	return &dto, nil
}

func (s *Service) ResumeConversation(ctx context.Context, query ResumeConversationQuery) (*ConversationDTO, error) {
	if strings.TrimSpace(query.ConversationID) == "" {
		return nil, fmt.Errorf("conversation_id required")
	}
	conversation, err := s.repo.GetConversation(ctx, query.ConversationID)
	if err != nil {
		return nil, err
	}
	dto := MapConversation(*conversation)
	return &dto, nil
}

func (s *Service) GetConversation(ctx context.Context, conversationID string) (*ConversationDTO, error) {
	return s.ResumeConversation(ctx, ResumeConversationQuery{ConversationID: conversationID})
}

// ListConversations 开放 API 只读分页查询（透传作用域过滤）。
func (s *Service) ListConversations(ctx context.Context, query OpenSessionListQuery) ([]ConversationDTO, int64, error) {
	items, total, err := s.repo.ListSessions(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	out := make([]ConversationDTO, 0, len(items))
	for _, item := range items {
		out = append(out, MapConversation(item))
	}
	return out, total, nil
}

func (s *Service) ListRecentMessages(ctx context.Context, conversationID string, limit int) ([]ConversationMessageDTO, error) {
	if strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("conversation_id required")
	}
	if limit <= 0 {
		limit = 10
	}
	items, err := s.repo.ListRecentMessages(ctx, conversationID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ConversationMessageDTO, 0, len(items))
	for _, item := range items {
		out = append(out, MapMessage(item))
	}
	return out, nil
}

func (s *Service) ListMessagesBefore(ctx context.Context, conversationID string, beforeMessageID string, limit int) ([]ConversationMessageDTO, error) {
	if strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("conversation_id required")
	}
	if limit <= 0 {
		limit = 50
	}
	items, err := s.repo.ListMessagesBefore(ctx, conversationID, beforeMessageID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ConversationMessageDTO, 0, len(items))
	for _, item := range items {
		out = append(out, MapMessage(item))
	}
	return out, nil
}

func (s *Service) AssignAgent(ctx context.Context, conversationID string, agentID uint) (*ConversationDTO, error) {
	if strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("conversation_id required")
	}
	conv, err := s.repo.GetConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	// Add or replace agent participant
	found := false
	for i, p := range conv.Participants {
		if p.Role == domain.ParticipantRoleAgent {
			conv.Participants[i].UserID = &agentID
			conv.Participants[i].ID = fmt.Sprintf("agent:%d", agentID)
			found = true
			break
		}
	}
	if !found {
		conv.Participants = append(conv.Participants, domain.Participant{
			ID:     fmt.Sprintf("agent:%d", agentID),
			UserID: &agentID,
			Role:   domain.ParticipantRoleAgent,
		})
	}
	conv.Status = domain.ConversationStatusActive
	if err := s.repo.UpdateConversation(ctx, conv); err != nil {
		return nil, err
	}
	dto := MapConversation(*conv)
	return &dto, nil
}

func (s *Service) Transfer(ctx context.Context, conversationID string, toAgentID uint) (*ConversationDTO, error) {
	if strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("conversation_id required")
	}
	conv, err := s.repo.GetConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	// Update agent participant to new agent
	found := false
	for i, p := range conv.Participants {
		if p.Role == domain.ParticipantRoleAgent {
			conv.Participants[i].UserID = &toAgentID
			conv.Participants[i].ID = fmt.Sprintf("agent:%d", toAgentID)
			found = true
			break
		}
	}
	if !found {
		conv.Participants = append(conv.Participants, domain.Participant{
			ID:     fmt.Sprintf("agent:%d", toAgentID),
			UserID: &toAgentID,
			Role:   domain.ParticipantRoleAgent,
		})
	}
	conv.Status = domain.ConversationStatusTransferred
	if err := s.repo.UpdateConversation(ctx, conv); err != nil {
		return nil, err
	}
	// Emit system event for transfer
	_, _ = s.ingestMessage(ctx, conversationID, "", domain.ParticipantRoleSystem, domain.MessageKindSystem,
		fmt.Sprintf("会话已转派给客服 #%d", toAgentID), nil)
	dto := MapConversation(*conv)
	return &dto, nil
}

func (s *Service) Close(ctx context.Context, conversationID string) (*ConversationDTO, error) {
	if strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("conversation_id required")
	}
	conv, err := s.repo.GetConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	conv.Status = domain.ConversationStatusClosed
	conv.EndedAt = &now
	if err := s.repo.UpdateConversation(ctx, conv); err != nil {
		return nil, err
	}
	// Emit system event for close
	_, _ = s.ingestMessage(ctx, conversationID, "", domain.ParticipantRoleSystem, domain.MessageKindSystem,
		"会话已结束", nil)
	dto := MapConversation(*conv)
	return &dto, nil
}

func (s *Service) IngestTextMessage(ctx context.Context, cmd IngestTextMessageCommand) (*ConversationMessageDTO, error) {
	return s.ingestMessage(ctx, cmd.ConversationID, cmd.MessageID, cmd.Sender, domain.MessageKindText, cmd.Content, cmd.Metadata)
}

func (s *Service) IngestSystemEvent(ctx context.Context, cmd IngestSystemEventCommand) (*ConversationMessageDTO, error) {
	return s.ingestMessage(ctx, cmd.ConversationID, cmd.MessageID, domain.ParticipantRoleSystem, domain.MessageKindSystem, cmd.Content, cmd.Metadata)
}

func (s *Service) ingestMessage(
	ctx context.Context,
	conversationID string,
	messageID string,
	sender domain.ParticipantRole,
	kind domain.MessageKind,
	content string,
	metadata map[string]string,
) (*ConversationMessageDTO, error) {
	if strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("conversation_id required")
	}
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("content required")
	}
	conversation, err := s.repo.GetConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	message := &domain.ConversationMessage{
		ID:             strings.TrimSpace(messageID),
		ConversationID: conversationID,
		Sender:         sender,
		Kind:           kind,
		Content:        strings.TrimSpace(content),
		Metadata:       cloneMetadata(metadata),
		CreatedAt:      now,
	}
	if message.ID == "" {
		message.ID = fmt.Sprintf("%s-%d", conversationID, now.UnixNano())
	}
	if err := s.repo.AppendMessage(ctx, message); err != nil {
		return nil, err
	}
	conversation.LastMessageAt = &now
	if err := s.repo.UpdateConversation(ctx, conversation); err != nil {
		return nil, err
	}
	s.publish(ctx, ConversationMessageReceivedEventName, conversationID, MapMessage(*message))
	dto := MapMessage(*message)
	return &dto, nil
}

func (s *Service) publish(ctx context.Context, name string, conversationID string, payload interface{}) {
	if s.publisher == nil {
		return
	}
	_ = s.publisher.Publish(ctx, NewConversationEvent(name, conversationID, payload))
}

func cloneMetadata(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
