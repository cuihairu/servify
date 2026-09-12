package delivery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/models"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	routingcontract "servify/apps/server/internal/modules/routing/contract"
	ticketdelivery "servify/apps/server/internal/modules/ticket/delivery"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type AISessionService interface {
	ShouldTransferToHuman(query string, sessionHistory []models.Message) bool
	GetSessionSummary(messages []models.Message) (string, error)
}

type agentRuntime interface {
	FindAvailableAgent(ctx context.Context, skills []string, priority string) (*agentdelivery.AgentInfo, error)
	SelectAgent(ctx context.Context, req agentdelivery.SelectionRequest) (*agentdelivery.SelectionResult, error)
	GetOnlineAgent(ctx context.Context, userID uint) (*agentdelivery.AgentInfo, bool)
	ApplySessionTransfer(ctx context.Context, sessionID string, fromAgentID *uint, toAgentID uint)
}

type notifier interface {
	SendToSession(sessionID string, message Notification)
}

type Notification struct {
	Type      string
	Data      interface{}
	SessionID string
	Timestamp time.Time
}

type HandlerDependencies struct {
	DB           *gorm.DB
	Logger       *logrus.Logger
	AI           AISessionService
	Agents       agentRuntime
	Notifier     notifier
	Routing      RuntimeService
	Tickets      ticketdelivery.RuntimeService
	Conversation conversationdelivery.RuntimeService
	AgentLoad    agentdelivery.RuntimeService
	// 等待队列 worker 分派参数（0 取默认值）。
	DispatchBatchSize int
	ClaimLeaseSeconds int
}

// HandlerServiceAdapter is the routing module entry for handler/runtime session transfer flows.
type HandlerServiceAdapter struct {
	db            *gorm.DB
	logger        *logrus.Logger
	aiService     AISessionService
	agentService  agentRuntime
	notifier      notifier
	routing       RuntimeService
	tickets       ticketdelivery.RuntimeService
	conversation  conversationdelivery.RuntimeService
	agents        agentdelivery.RuntimeService
	dispatchBatch int
	claimLease    time.Duration
}

func NewHandlerService(deps HandlerDependencies) *HandlerServiceAdapter {
	logger := deps.Logger
	if logger == nil {
		logger = logrus.New()
	}
	batch := deps.DispatchBatchSize
	if batch <= 0 || batch > 200 {
		batch = 10
	}
	lease := deps.ClaimLeaseSeconds
	if lease <= 0 {
		lease = 120
	}
	return &HandlerServiceAdapter{
		db:            deps.DB,
		logger:        logger,
		aiService:     deps.AI,
		agentService:  deps.Agents,
		notifier:      deps.Notifier,
		routing:       deps.Routing,
		tickets:       deps.Tickets,
		conversation:  deps.Conversation,
		agents:        deps.AgentLoad,
		dispatchBatch: batch,
		claimLease:    time.Duration(lease) * time.Second,
	}
}

func (s *HandlerServiceAdapter) TransferToHuman(ctx context.Context, req *routingcontract.TransferRequest) (*routingcontract.TransferResult, error) {
	session, err := s.loadTransferSession(ctx, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	if session.Status == "ended" {
		return nil, fmt.Errorf("session already ended")
	}
	if session.AgentID != nil {
		return nil, fmt.Errorf("session already assigned")
	}
	if existing, err := s.getActiveWaitingRecord(ctx, session.ID); err == nil {
		return &routingcontract.TransferResult{
			Success:   true,
			SessionID: session.ID,
			IsWaiting: true,
			QueuedAt:  &existing.QueuedAt,
			Summary:   "会话已在等待队列中",
		}, nil
	}
	agent, err := s.selectAgentForTransfer(ctx, req.TargetSkills, req.Priority, req.TargetGroupID, session.CustomerID)
	if err != nil {
		return s.addToWaitingQueue(ctx, session, req)
	}
	return s.executeTransfer(ctx, session, agent.UserID, req.Reason, req.Notes)
}

// selectAgentForTransfer 统一的三级分配入口：亲和（老客户回原坐席）→ 指定组 → 全局池。
func (s *HandlerServiceAdapter) selectAgentForTransfer(ctx context.Context, skills []string, priority string, targetGroupID uint, customerUserID uint) (*agentdelivery.AgentInfo, error) {
	req := agentdelivery.SelectionRequest{Skills: skills, Priority: priority}
	if targetGroupID != 0 {
		gid := targetGroupID
		req.GroupID = &gid
	}
	if customerUserID != 0 {
		req.Affinity = &agentdelivery.AffinityHint{CustomerUserID: customerUserID}
	}
	result, err := s.agentService.SelectAgent(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentdelivery.AgentInfoFromRuntime(result.Agent), nil
}

func (s *HandlerServiceAdapter) TransferToAgent(ctx context.Context, sessionID string, targetAgentID uint, reason string) (*routingcontract.TransferResult, error) {
	session, err := s.loadTransferSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	agentInfo, ok := s.agentService.GetOnlineAgent(ctx, targetAgentID)
	if !ok {
		return nil, fmt.Errorf("target agent is not online")
	}
	if agentInfo.CurrentLoad >= agentInfo.MaxConcurrent {
		return nil, fmt.Errorf("target agent is at maximum capacity")
	}
	return s.executeTransfer(ctx, session, targetAgentID, reason, "")
}

func (s *HandlerServiceAdapter) executeTransfer(ctx context.Context, session *conversationdelivery.TransferSession, targetAgentID uint, reason, notes string) (*routingcontract.TransferResult, error) {
	if session.Status == "ended" {
		return nil, fmt.Errorf("session already ended")
	}
	if session.AgentID != nil && *session.AgentID == targetAgentID {
		return &routingcontract.TransferResult{
			Success:    true,
			SessionID:  session.ID,
			NewAgentID: targetAgentID,
			Summary:    "会话已指派给目标客服",
		}, nil
	}

	fromAgentID := session.AgentID
	transferAt := time.Now()
	summary, err := s.generateSessionSummary(session)
	if err != nil {
		s.logger.Warnf("Failed to generate session summary: %v", err)
		summary = "无法生成会话摘要"
	}
	transferMessageContent := buildTransferMessage(reason, notes)
	transferRecord := &models.TransferRecord{
		SessionID:      session.ID,
		FromAgentID:    fromAgentID,
		ToAgentID:      &targetAgentID,
		Reason:         reason,
		Notes:          notes,
		SessionSummary: summary,
		TransferredAt:  transferAt,
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.syncTransferSession(ctx, tx, session, targetAgentID); err != nil {
			return fmt.Errorf("update session: %w", err)
		}
		if err := s.syncTransferTicket(ctx, tx, session, targetAgentID, transferAt); err != nil {
			return fmt.Errorf("update ticket: %w", err)
		}
		if err := s.syncTransferAgentLoad(ctx, tx, fromAgentID, targetAgentID); err != nil {
			return fmt.Errorf("sync agent load: %w", err)
		}
		if err := s.appendTransferSystemMessage(ctx, tx, session.ID, targetAgentID, transferMessageContent, transferAt); err != nil {
			return fmt.Errorf("create transfer message: %w", err)
		}
		createdRecord, err := s.routing.AssignAgent(ctx, tx, AssignAgentCommand{
			SessionID:      session.ID,
			AgentID:        targetAgentID,
			FromAgentID:    fromAgentID,
			Reason:         reason,
			Notes:          notes,
			SessionSummary: summary,
			AssignedAt:     transferAt,
		})
		if err != nil {
			return fmt.Errorf("create transfer record: %w", err)
		}
		transferRecord = createdRecord
		if err := s.markWaitingTransferred(ctx, tx, session.ID, targetAgentID, transferAt); err != nil {
			return fmt.Errorf("sync waiting record: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if s.agentService != nil {
		s.agentService.ApplySessionTransfer(ctx, session.ID, fromAgentID, targetAgentID)
	}
	s.notifyTransfer(session.ID, targetAgentID, transferMessageContent)
	s.logger.Infof("Successfully transferred session %s to agent %d", session.ID, targetAgentID)
	return &routingcontract.TransferResult{
		Success:       true,
		SessionID:     session.ID,
		NewAgentID:    targetAgentID,
		TransferredAt: transferRecord.TransferredAt,
		Summary:       summary,
	}, nil
}

func (s *HandlerServiceAdapter) addToWaitingQueue(ctx context.Context, session *conversationdelivery.TransferSession, req *routingcontract.TransferRequest) (*routingcontract.TransferResult, error) {
	if existing, err := s.getActiveWaitingRecord(ctx, session.ID); err == nil {
		return &routingcontract.TransferResult{
			Success:   true,
			SessionID: session.ID,
			IsWaiting: true,
			QueuedAt:  &existing.QueuedAt,
			Summary:   "会话已在等待队列中",
		}, nil
	}
	waitingMessage := &models.Message{
		SessionID: session.ID,
		UserID:    session.CustomerID,
		Content:   "您的会话已加入人工客服等待队列，我们会尽快为您安排客服。请耐心等待。",
		Type:      "system",
		Sender:    "system",
	}
	var waitingRecord *models.WaitingRecord
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.ensureWaitingSessionState(ctx, tx, session); err != nil {
			return fmt.Errorf("failed to ensure session active: %w", err)
		}
		createdRecord, err := s.routing.AddToWaitingQueue(ctx, tx, session.ID, req.Reason, req.TargetSkills, req.TargetGroupID, req.Priority, req.Notes)
		if err != nil {
			return fmt.Errorf("failed to create waiting record: %w", err)
		}
		waitingRecord = createdRecord
		if err := s.appendWaitingSystemMessage(ctx, tx, waitingMessage); err != nil {
			return fmt.Errorf("create waiting message: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	s.notifyWaiting(session.ID, waitingMessage.Content)
	s.logger.Infof("Added session %s to waiting queue", session.ID)
	return &routingcontract.TransferResult{
		Success:   true,
		SessionID: session.ID,
		IsWaiting: true,
		QueuedAt:  &waitingRecord.QueuedAt,
		Summary:   "会话已加入等待队列",
	}, nil
}

// ProcessWaitingQueue claim-then-process 分派等待队列：认领（租约防双发）→
// 三级选坐席 → 执行转接；失败归还租约等下一轮。返回本轮实际转接数。
func (s *HandlerServiceAdapter) ProcessWaitingQueue(ctx context.Context) (int, error) {
	now := time.Now()
	leaseBefore := now.Add(-s.claimLease)
	waitingRecords, err := s.routing.ClaimWaitingRecords(ctx, now, leaseBefore, s.dispatchBatch)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, record := range waitingRecords {
		skills := []string{}
		if record.TargetSkills != "" {
			skills = strings.Split(record.TargetSkills, ",")
		}
		session, err := s.loadTransferSession(ctx, record.SessionID)
		if err != nil {
			s.releaseClaim(ctx, record.SessionID)
			continue
		}
		agent, err := s.selectAgentForTransfer(ctx, skills, record.Priority, derefTargetGroupID(record.TargetGroupID), session.CustomerID)
		if err != nil {
			s.releaseClaim(ctx, record.SessionID)
			continue
		}
		result, err := s.executeTransfer(ctx, session, agent.UserID, record.Reason, record.Notes)
		if err != nil {
			s.logger.Errorf("Failed to transfer waiting session %s: %v", record.SessionID, err)
			s.releaseClaim(ctx, record.SessionID)
			continue
		}
		processed++
		s.logger.Infof("Successfully transferred waiting session %s to agent %d", result.SessionID, result.NewAgentID)
	}
	return processed, nil
}

// releaseClaim 归还租约的容错包装：归还失败只记日志（租约到期仍会复活）。
func (s *HandlerServiceAdapter) releaseClaim(ctx context.Context, sessionID string) {
	if err := s.routing.ReleaseWaitingClaim(ctx, sessionID); err != nil {
		s.logger.Warnf("Failed to release waiting claim for %s: %v", sessionID, err)
	}
}

func derefTargetGroupID(id *uint) uint {
	if id == nil {
		return 0
	}
	return *id
}

func normalizeWaitingRecordQuery(status string, limit int) (string, int) {
	if status == "" {
		status = "waiting"
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return status, limit
}

func (s *HandlerServiceAdapter) GetTransferHistory(ctx context.Context, sessionID string) ([]models.TransferRecord, error) {
	records, err := s.routing.GetTransferHistory(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transfer history: %w", err)
	}
	return records, nil
}

func (s *HandlerServiceAdapter) ListRecentTransferHistory(ctx context.Context, limit int) ([]models.TransferRecord, error) {
	records, err := s.routing.ListRecentTransferHistory(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list recent transfer history: %w", err)
	}
	return records, nil
}

func (s *HandlerServiceAdapter) ListWaitingRecords(ctx context.Context, status string, limit int) ([]models.WaitingRecord, error) {
	status, limit = normalizeWaitingRecordQuery(status, limit)
	records, err := s.routing.ListWaitingRecords(ctx, status, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list waiting records: %w", err)
	}
	return records, nil
}

func (s *HandlerServiceAdapter) CancelWaitingRecord(ctx context.Context, sessionID string, operatorID uint, reason string) error {
	if sessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	if reason == "" {
		reason = "cancelled"
	}
	now := time.Now()
	return s.db.Transaction(func(tx *gorm.DB) error {
		_, err := s.routing.CancelWaiting(ctx, tx, sessionID, reason)
		if err != nil && err != gorm.ErrRecordNotFound {
			return fmt.Errorf("update waiting record: %w", err)
		}
		_ = s.appendCancellationSystemMessage(ctx, tx, sessionID, operatorID, reason, now)
		return nil
	})
}

func (s *HandlerServiceAdapter) AutoTransferCheck(ctx context.Context, sessionID string, messages []models.Message) bool {
	if s.aiService == nil {
		return false
	}
	lastMessages := messages
	if len(messages) > 5 {
		lastMessages = messages[len(messages)-5:]
	}
	var queryBuilder strings.Builder
	for _, msg := range lastMessages {
		if msg.Sender == "user" {
			queryBuilder.WriteString(msg.Content)
			queryBuilder.WriteString(" ")
		}
	}
	return s.aiService.ShouldTransferToHuman(queryBuilder.String(), messages)
}

func (s *HandlerServiceAdapter) loadTransferSession(ctx context.Context, sessionID string) (*conversationdelivery.TransferSession, error) {
	return s.conversation.LoadTransferSession(ctx, sessionID)
}

func (s *HandlerServiceAdapter) loadWaitingRecord(ctx context.Context, sessionID string) (*models.WaitingRecord, error) {
	return s.routing.GetWaitingRecord(ctx, sessionID)
}

func (s *HandlerServiceAdapter) getActiveWaitingRecord(ctx context.Context, sessionID string) (*models.WaitingRecord, error) {
	record, err := s.loadWaitingRecord(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if record.Status != "waiting" {
		return nil, gorm.ErrRecordNotFound
	}
	return record, nil
}

func (s *HandlerServiceAdapter) syncTransferSession(ctx context.Context, tx *gorm.DB, session *conversationdelivery.TransferSession, targetAgentID uint) error {
	return s.conversation.SyncTransferAssignment(ctx, tx, session.ID, session.CustomerID, targetAgentID)
}

func (s *HandlerServiceAdapter) syncTransferTicket(ctx context.Context, tx *gorm.DB, session *conversationdelivery.TransferSession, targetAgentID uint, transferAt time.Time) error {
	if session.TicketID == nil || *session.TicketID == 0 {
		return nil
	}
	err := s.tickets.SyncTransferAssignment(ctx, tx, *session.TicketID, targetAgentID, targetAgentID)
	if err == gorm.ErrRecordNotFound {
		return nil
	}
	return err
}

func buildTransferTicketUpdate(targetAgentID uint, currentStatus string) (map[string]interface{}, string, string) {
	updates := map[string]interface{}{"agent_id": targetAgentID}
	fromStatus := currentStatus
	toStatus := fromStatus
	if fromStatus == "open" || fromStatus == "" {
		toStatus = "assigned"
		updates["status"] = toStatus
	}
	return updates, fromStatus, toStatus
}

func (s *HandlerServiceAdapter) syncTransferAgentLoad(ctx context.Context, tx *gorm.DB, fromAgentID *uint, targetAgentID uint) error {
	return s.agents.SyncTransferLoad(ctx, tx, fromAgentID, targetAgentID)
}

func (s *HandlerServiceAdapter) appendTransferSystemMessage(ctx context.Context, tx *gorm.DB, sessionID string, targetAgentID uint, content string, createdAt time.Time) error {
	return s.conversation.AppendSystemMessage(ctx, tx, sessionID, content, createdAt)
}

func (s *HandlerServiceAdapter) markWaitingTransferred(ctx context.Context, tx *gorm.DB, sessionID string, targetAgentID uint, transferAt time.Time) error {
	_, err := s.routing.MarkWaitingTransferred(ctx, tx, sessionID, targetAgentID, transferAt)
	if err == gorm.ErrRecordNotFound {
		return nil
	}
	return err
}

func (s *HandlerServiceAdapter) ensureWaitingSessionState(ctx context.Context, tx *gorm.DB, session *conversationdelivery.TransferSession) error {
	return s.conversation.SyncWaitingAssignment(ctx, tx, session.ID, session.CustomerID)
}

func (s *HandlerServiceAdapter) appendWaitingSystemMessage(ctx context.Context, tx *gorm.DB, message *models.Message) error {
	return s.conversation.AppendSystemMessage(ctx, tx, message.SessionID, message.Content, message.CreatedAt)
}

func (s *HandlerServiceAdapter) appendCancellationSystemMessage(ctx context.Context, tx *gorm.DB, sessionID string, operatorID uint, reason string, createdAt time.Time) error {
	content := fmt.Sprintf("已取消人工客服等待队列（原因：%s）", reason)
	return s.conversation.AppendSystemMessage(ctx, tx, sessionID, content, createdAt)
}

func (s *HandlerServiceAdapter) generateSessionSummary(session *conversationdelivery.TransferSession) (string, error) {
	var messages []models.Message
	if err := s.db.Where("session_id = ?", session.ID).
		Order("created_at ASC").
		Find(&messages).Error; err != nil {
		return "", err
	}
	if len(messages) < 3 {
		userLabel := session.UserUsername
		if userLabel == "" {
			userLabel = session.UserName
		}
		if userLabel == "" {
			userLabel = fmt.Sprintf("ID=%d", session.CustomerID)
		}
		return fmt.Sprintf("用户%s的简短会话，共%d条消息", userLabel, len(messages)), nil
	}
	return s.aiService.GetSessionSummary(messages)
}

func buildTransferMessage(reason, notes string) string {
	message := "您的会话已转接至人工客服"
	if reason != "" {
		message += fmt.Sprintf("。转接原因：%s", reason)
	}
	if notes != "" {
		message += fmt.Sprintf("。备注：%s", notes)
	}
	message += "。客服将很快为您提供帮助。"
	return message
}

func (s *HandlerServiceAdapter) notifyTransfer(sessionID string, agentID uint, message string) {
	if s.notifier == nil {
		return
	}
	s.notifier.SendToSession(sessionID, Notification{
		Type: "transfer_notification",
		Data: map[string]interface{}{
			"message":   message,
			"agent_id":  agentID,
			"timestamp": time.Now(),
		},
	})
}

func (s *HandlerServiceAdapter) notifyWaiting(sessionID string, message string) {
	if s.notifier == nil {
		return
	}
	s.notifier.SendToSession(sessionID, Notification{
		Type: "waiting_notification",
		Data: map[string]interface{}{
			"message":   message,
			"timestamp": time.Now(),
		},
	})
}

var _ HandlerService = (*HandlerServiceAdapter)(nil)
