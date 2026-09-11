package infra

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/conversation/domain"
	platformauth "servify/apps/server/internal/platform/auth"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newConversationUnitTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Session{}, &models.Message{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestGormConversationRepositoryNilGuards(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	if err := repo.CreateConversation(ctx, nil); err == nil || err.Error() != "conversation required" {
		t.Fatalf("expected nil conversation error, got %v", err)
	}
	if err := repo.UpdateConversation(ctx, nil); err == nil || err.Error() != "conversation required" {
		t.Fatalf("expected nil conversation error on update, got %v", err)
	}
	if err := repo.AppendMessage(ctx, nil); err == nil || err.Error() != "message required" {
		t.Fatalf("expected nil message error, got %v", err)
	}
}

func TestGormConversationRepositoryCreateAndGet(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)

	if err := db.Create(&models.User{ID: 7, Username: "alice", Email: "alice@example.com", Name: "Alice"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&models.User{ID: 9, Username: "bob", Email: "bob@example.com"}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	agentID := uint(9)
	customerID := uint(7)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")

	conversation := &domain.Conversation{
		ID:         "conv-1",
		CustomerID: &customerID,
		Status:     domain.ConversationStatusActive,
		Channel:    domain.ChannelBinding{Channel: "web", SessionID: "conv-1"},
		Participants: []domain.Participant{
			{ID: "customer:7", UserID: &customerID, Role: domain.ParticipantRoleCustomer},
			{ID: "agent:9", UserID: &agentID, Role: domain.ParticipantRoleAgent},
		},
		StartedAt: now,
	}
	if err := repo.CreateConversation(ctx, conversation); err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	var stored models.Session
	if err := db.First(&stored, "id = ?", "conv-1").Error; err != nil {
		t.Fatalf("load stored session: %v", err)
	}
	if stored.TenantID != "tenant-a" || stored.WorkspaceID != "ws-1" {
		t.Fatalf("unexpected scope: %+v", stored)
	}
	if stored.Status != "active" || stored.Platform != "web" || stored.UserID != 7 || stored.AgentID == nil || *stored.AgentID != 9 {
		t.Fatalf("unexpected stored session: %+v", stored)
	}
	if conversation.Channel.Channel != "web" || conversation.Channel.SessionID != "conv-1" {
		t.Fatalf("expected conversation mapped back from model, got %+v", conversation.Channel)
	}

	got, err := repo.GetConversation(ctx, "conv-1")
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if got.ID != "conv-1" || got.CustomerID == nil || *got.CustomerID != 7 {
		t.Fatalf("unexpected conversation: %+v", got)
	}
	if len(got.Participants) != 2 {
		t.Fatalf("expected two participants, got %+v", got.Participants)
	}
	if got.Participants[0].ID != "user:7" || got.Participants[0].DisplayName != "Alice" {
		t.Fatalf("unexpected customer participant: %+v", got.Participants[0])
	}
	if got.Participants[1].ID != "agent:9" || got.Participants[1].DisplayName != "bob" {
		t.Fatalf("expected agent display name to fall back to username, got %+v", got.Participants[1])
	}

	if _, err := repo.GetConversation(platformauth.ContextWithScope(context.Background(), "tenant-b", ""), "conv-1"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected scoped lookup to fail, got %v", err)
	}
	if _, err := repo.GetConversation(ctx, "missing"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestGormConversationRepositoryQueryErrorsAfterDrop(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	if err := db.Create(&models.Session{ID: "conv-1", Status: "active"}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := db.Migrator().DropTable(&models.Message{}); err != nil {
		t.Fatalf("drop messages: %v", err)
	}
	if err := db.Migrator().DropTable(&models.Session{}); err != nil {
		t.Fatalf("drop sessions: %v", err)
	}

	if err := repo.CreateConversation(ctx, &domain.Conversation{ID: "conv-2"}); err == nil {
		t.Fatal("expected create error after drop")
	}
	if _, err := repo.GetConversation(ctx, "conv-1"); err == nil {
		t.Fatal("expected get error after drop")
	}
	if err := repo.UpdateConversation(ctx, &domain.Conversation{ID: "conv-1", Status: domain.ConversationStatusActive}); err == nil {
		t.Fatal("expected update error after drop")
	}
	if err := repo.AppendMessage(ctx, &domain.ConversationMessage{ConversationID: "conv-1", Content: "x"}); err == nil {
		t.Fatal("expected append error after drop")
	}
	if _, err := repo.ListRecentMessages(ctx, "conv-1", 5); err == nil {
		t.Fatal("expected list error after drop")
	}
	if _, err := repo.ListMessagesBefore(ctx, "conv-1", "", 5); err == nil {
		t.Fatal("expected list before error after drop")
	}
}

func TestGormConversationRepositoryUpdateBranches(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")

	if err := db.Create(&models.Session{ID: "conv-1", TenantID: "tenant-a", WorkspaceID: "ws-1", Status: "waiting_human", Platform: "email"}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	lastMessageAt := time.Now().Truncate(time.Second).Add(time.Minute)
	endedAt := lastMessageAt.Add(time.Minute)
	customerID := uint(7)
	agentID := uint(9)

	if err := repo.UpdateConversation(ctx, &domain.Conversation{
		ID:         "conv-1",
		CustomerID: &customerID,
		Status:     domain.ConversationStatusClosed,
		Channel:    domain.ChannelBinding{Channel: "web"},
		Participants: []domain.Participant{
			{ID: "agent:9", UserID: &agentID, Role: domain.ParticipantRoleAgent},
		},
		StartedAt:     lastMessageAt.Add(-time.Hour),
		LastMessageAt: &lastMessageAt,
		EndedAt:       &endedAt,
	}); err != nil {
		t.Fatalf("update conversation: %v", err)
	}

	var stored models.Session
	if err := db.First(&stored, "id = ?", "conv-1").Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if stored.Status != "ended" || stored.UserID != 7 || stored.AgentID == nil || *stored.AgentID != 9 || stored.Platform != "web" {
		t.Fatalf("unexpected updated session: %+v", stored)
	}
	if stored.EndedAt == nil || stored.EndedAt.Unix() != endedAt.Unix() {
		t.Fatalf("expected ended_at, got %+v", stored.EndedAt)
	}
	if stored.UpdatedAt.Unix() != endedAt.Unix() {
		t.Fatalf("expected updated_at to follow ended_at, got %v", stored.UpdatedAt)
	}
}

func TestGormConversationRepositoryUpdateClearsExplicitAgent(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")

	agentID := uint(5)
	if err := db.Create(&models.Session{ID: "conv-1", TenantID: "tenant-a", WorkspaceID: "ws-1", Status: "active", AgentID: &agentID}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	if err := repo.UpdateConversation(ctx, &domain.Conversation{
		ID:     "conv-1",
		Status: domain.ConversationStatusActive,
		Participants: []domain.Participant{
			{ID: "agent:0", Role: domain.ParticipantRoleAgent},
		},
	}); err != nil {
		t.Fatalf("update conversation: %v", err)
	}

	var stored models.Session
	if err := db.First(&stored, "id = ?", "conv-1").Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if stored.AgentID != nil {
		t.Fatalf("expected agent_id cleared, got %+v", stored.AgentID)
	}
}

func TestGormConversationRepositoryUpdateMinimalFields(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	agentID := uint(5)
	started := time.Now().Truncate(time.Second).Add(-time.Hour)
	if err := db.Create(&models.Session{ID: "conv-1", Status: "active", Platform: "email", AgentID: &agentID, StartedAt: started}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	if err := repo.UpdateConversation(ctx, &domain.Conversation{
		ID:        "conv-1",
		Status:    domain.ConversationStatusWaitingHuman,
		StartedAt: started,
	}); err != nil {
		t.Fatalf("update conversation: %v", err)
	}

	var stored models.Session
	if err := db.First(&stored, "id = ?", "conv-1").Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if stored.Status != "waiting_human" {
		t.Fatalf("expected waiting_human, got %q", stored.Status)
	}
	if stored.AgentID == nil || *stored.AgentID != 5 {
		t.Fatalf("expected agent_id untouched, got %+v", stored.AgentID)
	}
	if stored.Platform != "email" {
		t.Fatalf("expected platform untouched, got %q", stored.Platform)
	}
	if stored.UpdatedAt.Unix() != started.Unix() {
		t.Fatalf("expected updated_at to follow started_at, got %v", stored.UpdatedAt)
	}
}

func TestGormConversationRepositoryAppendMessage(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")

	now := time.Now().Truncate(time.Second)
	message := &domain.ConversationMessage{
		ConversationID: "conv-1",
		Sender:         domain.ParticipantRoleAgent,
		Kind:           domain.MessageKindText,
		Content:        "hello",
		CreatedAt:      now,
	}
	if err := repo.AppendMessage(ctx, message); err != nil {
		t.Fatalf("append message: %v", err)
	}
	if message.ID == "" {
		t.Fatal("expected generated numeric message id")
	}

	var stored models.Message
	if err := db.First(&stored, "session_id = ?", "conv-1").Error; err != nil {
		t.Fatalf("load stored message: %v", err)
	}
	if stored.TenantID != "tenant-a" || stored.WorkspaceID != "ws-1" {
		t.Fatalf("unexpected message scope: %+v", stored)
	}
	if stored.Sender != "agent" || stored.Type != "text" || stored.Content != "hello" {
		t.Fatalf("unexpected stored message: %+v", stored)
	}
}

func seedMessages(t *testing.T, db *gorm.DB, count int, conversationID string, tenantID string, workspaceID string) {
	t.Helper()
	base := time.Now().Truncate(time.Second).Add(-time.Duration(count) * time.Minute)
	for i := 0; i < count; i++ {
		msg := models.Message{
			SessionID:   conversationID,
			TenantID:    tenantID,
			WorkspaceID: workspaceID,
			Content:     time.Duration(i).String(),
			Type:        "text",
			Sender:      "user",
			CreatedAt:   base.Add(time.Duration(i) * time.Second),
		}
		if err := db.Create(&msg).Error; err != nil {
			t.Fatalf("seed message %d: %v", i, err)
		}
	}
}

func TestGormConversationRepositoryListRecentMessages(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)

	seedMessages(t, db, 12, "conv-1", "tenant-a", "ws-1")
	seedMessages(t, db, 3, "conv-1", "tenant-b", "ws-2")

	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")
	items, err := repo.ListRecentMessages(ctx, "conv-1", 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(items) != 10 {
		t.Fatalf("expected default limit of 10, got %d", len(items))
	}
	// newest first：tenant-a 最新一条是索引 11（内容为其 Duration 字符串）
	if items[0].Content != time.Duration(11).String() {
		t.Fatalf("expected newest message first, got %+v", items[0])
	}
	if !items[0].CreatedAt.After(items[1].CreatedAt) {
		t.Fatalf("expected descending order, got %v then %v", items[0].CreatedAt, items[1].CreatedAt)
	}
	if items[0].ConversationID != "conv-1" || items[0].ID == "" {
		t.Fatalf("unexpected mapped message: %+v", items[0])
	}

	items, err = repo.ListRecentMessages(ctx, "conv-1", 3)
	if err != nil {
		t.Fatalf("list messages with explicit limit: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(items))
	}
}

func TestGormConversationRepositoryListMessagesBefore(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")

	seedMessages(t, db, 5, "conv-1", "tenant-a", "ws-1")
	seedMessages(t, db, 1, "conv-1", "tenant-b", "ws-2")

	var pivot models.Message
	if err := db.Where("session_id = ? AND tenant_id = ?", "conv-1", "tenant-a").Order("created_at ASC").Offset(2).First(&pivot).Error; err != nil {
		t.Fatalf("load pivot message: %v", err)
	}

	items, err := repo.ListMessagesBefore(ctx, "conv-1", strconv.FormatUint(uint64(pivot.ID), 10), 10)
	if err != nil {
		t.Fatalf("list messages before: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 messages before pivot, got %d", len(items))
	}
	for _, item := range items {
		if !item.CreatedAt.Before(pivot.CreatedAt) {
			t.Fatalf("expected messages older than pivot, got %+v", item)
		}
	}

	if _, err := repo.ListMessagesBefore(ctx, "conv-1", "999999", 10); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected pivot not found, got %v", err)
	}

	// 跨租户 pivot 应被作用域过滤
	var other models.Message
	if err := db.Where("tenant_id = ?", "tenant-b").First(&other).Error; err != nil {
		t.Fatalf("load tenant-b message: %v", err)
	}
	if _, err := repo.ListMessagesBefore(ctx, "conv-1", strconv.FormatUint(uint64(other.ID), 10), 10); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected scoped pivot not found, got %v", err)
	}
}

func TestGormConversationRepositoryListMessagesBeforeDefaultLimit(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	seedMessages(t, db, 55, "conv-1", "", "")

	items, err := repo.ListMessagesBefore(ctx, "conv-1", "", 0)
	if err != nil {
		t.Fatalf("list messages before: %v", err)
	}
	if len(items) != 50 {
		t.Fatalf("expected default limit of 50, got %d", len(items))
	}
}

func TestMapConversationEdgeCases(t *testing.T) {
	agentID := uint(9)
	endedAt := time.Now()

	// 无用户、Agent 关联未加载、UpdatedAt 为零值、EndedAt 为 nil
	got := mapConversation(models.Session{
		ID:        "conv-1",
		AgentID:   &agentID,
		Status:    "active",
		StartedAt: endedAt.Add(-time.Hour),
	})
	if got.CustomerID != nil {
		t.Fatalf("expected nil customer id, got %+v", got.CustomerID)
	}
	if len(got.Participants) != 1 || got.Participants[0].DisplayName != "" {
		t.Fatalf("expected agent participant without display name, got %+v", got.Participants)
	}
	if got.LastMessageAt != nil {
		t.Fatalf("expected nil last_message_at for zero updated_at, got %+v", got.LastMessageAt)
	}
	if got.EndedAt != nil {
		t.Fatalf("expected nil ended_at, got %+v", got.EndedAt)
	}
	if got.Status != domain.ConversationStatusActive {
		t.Fatalf("expected active status, got %q", got.Status)
	}
	if got.Channel.Channel != "" || got.Channel.SessionID != "conv-1" {
		t.Fatalf("unexpected channel binding: %+v", got.Channel)
	}
}

func TestMapConversationModelDefaults(t *testing.T) {
	endedAt := time.Now()
	model := mapConversationModel(domain.Conversation{
		ID:        "conv-1",
		Status:    domain.ConversationStatusClosed,
		EndedAt:   &endedAt,
		StartedAt: endedAt.Add(-time.Hour),
	})
	if model.Platform != "web" {
		t.Fatalf("expected default web platform, got %q", model.Platform)
	}
	if model.Status != "ended" {
		t.Fatalf("expected ended status, got %q", model.Status)
	}
	if model.UpdatedAt.Unix() != endedAt.Unix() {
		t.Fatalf("expected updated_at to follow ended_at, got %v", model.UpdatedAt)
	}
	if model.UserID != 0 || model.AgentID != nil {
		t.Fatalf("expected zero user/agent ids, got %+v", model)
	}
}

func TestMapMessageSenderAndKindMatrix(t *testing.T) {
	senders := []struct {
		stored string
		want   domain.ParticipantRole
	}{
		{"agent", domain.ParticipantRoleAgent},
		{"ai", domain.ParticipantRoleAI},
		{"system", domain.ParticipantRoleSystem},
		{"user", domain.ParticipantRoleCustomer},
		{"unknown", domain.ParticipantRoleCustomer},
		{" AGENT ", domain.ParticipantRoleAgent},
	}
	for _, tc := range senders {
		if got := mapMessageSender(tc.stored); got != tc.want {
			t.Errorf("mapMessageSender(%q) = %q, want %q", tc.stored, got, tc.want)
		}
	}

	roles := []struct {
		role domain.ParticipantRole
		want string
	}{
		{domain.ParticipantRoleAgent, "agent"},
		{domain.ParticipantRoleAI, "ai"},
		{domain.ParticipantRoleSystem, "system"},
		{domain.ParticipantRoleCustomer, "user"},
		{domain.ParticipantRole("other"), "user"},
	}
	for _, tc := range roles {
		if got := mapParticipantRoleToSender(tc.role); got != tc.want {
			t.Errorf("mapParticipantRoleToSender(%q) = %q, want %q", tc.role, got, tc.want)
		}
	}

	kinds := []struct {
		stored string
		want   domain.MessageKind
	}{
		{"system", domain.MessageKindSystem},
		{"text", domain.MessageKindText},
		{"image", domain.MessageKindText},
		{" SYSTEM ", domain.MessageKindSystem},
	}
	for _, tc := range kinds {
		if got := mapMessageKind(tc.stored); got != tc.want {
			t.Errorf("mapMessageKind(%q) = %q, want %q", tc.stored, got, tc.want)
		}
	}

	modelKinds := []struct {
		kind domain.MessageKind
		want string
	}{
		{domain.MessageKindSystem, "system"},
		{domain.MessageKindText, "text"},
		{domain.MessageKind("other"), "text"},
	}
	for _, tc := range modelKinds {
		if got := mapMessageKindToModel(tc.kind); got != tc.want {
			t.Errorf("mapMessageKindToModel(%q) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestMapSessionStatusUnknown(t *testing.T) {
	if got := mapSessionStatusToConversationStatus("weird"); got != domain.ConversationStatusActive {
		t.Fatalf("expected active for unknown status, got %q", got)
	}
	if got := mapConversationStatusToSessionStatus(domain.ConversationStatus("weird")); got != "active" {
		t.Fatalf("expected active for unknown status, got %q", got)
	}
}

func TestResolveHelpers(t *testing.T) {
	agentID := uint(9)
	items := []domain.Participant{
		{ID: "agent:0", Role: domain.ParticipantRoleAgent},
		{ID: "agent:9", UserID: &agentID, Role: domain.ParticipantRoleAgent},
	}
	got := resolveConversationAgentID(items)
	if got == nil || *got != 9 {
		t.Fatalf("expected agent id 9, got %+v", got)
	}
	if resolveConversationAgentID(nil) != nil {
		t.Fatal("expected nil agent id for empty participants")
	}
	if !hasExplicitAgentParticipant(items) {
		t.Fatal("expected explicit agent participant")
	}
	if hasExplicitAgentParticipant([]domain.Participant{{Role: domain.ParticipantRoleCustomer}}) {
		t.Fatal("expected no explicit agent participant")
	}
	if resolveMessageUserID(domain.ConversationMessage{}) != 0 {
		t.Fatal("expected zero user id")
	}
}

func TestFirstNonEmptyVariants(t *testing.T) {
	if got := firstNonEmpty(" ", "", "web", "other"); got != "web" {
		t.Fatalf("expected web, got %q", got)
	}
	if got := firstNonEmpty("", "  "); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestApplyScopeFieldsNilModels(t *testing.T) {
	ctx := platformauth.ContextWithScope(context.Background(), "tenant-a", "ws-1")
	applyConversationScopeFields(ctx, nil)
	applyMessageScopeFields(ctx, nil)
}
