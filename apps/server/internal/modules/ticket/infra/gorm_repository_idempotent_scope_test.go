package infra

// 幂等创建的会话作用域与并发收敛分支（无 build tag）：
// 1. SessionID 为空的同键重试走 session_id IS NULL 预查询命中；
// 2. 预查询通过后插入才撞唯一约束（并发同键）：重查命中既有行，
//    把并发重试收敛为幂等命中（created=false），不再上抛。

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"servify/apps/server/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func idempotentScopeDB(t *testing.T, translateErrors bool) *gorm.DB {
	t.Helper()
	cfg := &gorm.Config{}
	if translateErrors {
		cfg.TranslateError = true
	}
	dsn := uniqueMemDSN("file:ticket_idem_" + strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), cfg)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&models.Ticket{}, &models.TicketOutbox{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

// idempotentOutbox 幂等创建的最小 outbox 事件（buildOutbox 非 nil 才会走到
// 出箱写入分支）。
func idempotentOutbox(ticket *models.Ticket) *models.TicketOutbox {
	return &models.TicketOutbox{
		EventID:     fmt.Sprintf("ticket:%d:created", ticket.ID),
		EventName:   "ticket.created",
		AggregateID: fmt.Sprintf("ticket:%d", ticket.ID),
		Payload:     "{}",
	}
}

func idempotentTicket(key, session string) *models.Ticket {
	ticket := &models.Ticket{
		TenantID:       "tenant-1",
		WorkspaceID:    "ws-1",
		Title:          "idempotent ticket",
		CustomerID:     1,
		Category:       "general",
		Priority:       "normal",
		Status:         "open",
		Source:         "web",
		IdempotencyKey: &key,
	}
	if session != "" {
		ticket.SessionID = &session
	}
	return ticket
}

// SessionID 为空：同键重试经 session_id IS NULL 作用域预查询命中既有行。
func TestCreateTicketAtomicallyIdempotentWithoutSession(t *testing.T) {
	db := idempotentScopeDB(t, false)
	repo := NewGormRepository(db)
	ctx := context.Background()

	created, err := repo.CreateTicketModelAtomically(ctx, idempotentTicket("key-null", ""), nil, nil, idempotentOutbox)
	if err != nil || !created {
		t.Fatalf("first create: created=%v err=%v", created, err)
	}
	retry := idempotentTicket("key-null", "")
	created, err = repo.CreateTicketModelAtomically(ctx, retry, nil, nil, idempotentOutbox)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if created {
		t.Fatal("same-key retry without session must be an idempotent hit")
	}
	if retry.ID == 0 {
		t.Fatalf("retry must adopt the existing ticket id, got %+v", retry)
	}
}

// 预查询通过后插入才撞唯一约束（并发同键的最小形态）：注入回调在 Create
// 前落一条同作用域同键的行，插入报 ErrDuplicatedKey 后重查收敛为幂等命中。
func TestCreateTicketAtomicallyConvergesOnDuplicateKey(t *testing.T) {
	db := idempotentScopeDB(t, true)
	ctx := context.Background()

	var injected bool
	err := db.Callback().Create().Before("gorm:create").Register("test:inject_conflict", func(tx *gorm.DB) {
		if injected || tx.Statement.Table != "tickets" {
			return
		}
		injected = true
		session := "s-9"
		if err := tx.Exec(
			`INSERT INTO tickets (tenant_id, workspace_id, idempotency_key, title, customer_id, session_id, category, priority, status, source) VALUES ('tenant-1','ws-1','key-race','winner',1,?,'general','normal','open','web')`,
			session,
		).Error; err != nil {
			tx.AddError(err)
		}
	})
	if err != nil {
		t.Fatalf("register callback: %v", err)
	}

	repo := NewGormRepository(db)
	ticket := idempotentTicket("key-race", "s-9")
	created, err := repo.CreateTicketModelAtomically(ctx, ticket, nil, nil, idempotentOutbox)
	if err != nil {
		t.Fatalf("duplicate-key create must converge, got %v", err)
	}
	if created {
		t.Fatal("converged duplicate must report created=false")
	}
	if ticket.ID == 0 || ticket.Title != "winner" {
		t.Fatalf("ticket must adopt the existing row, got %+v", ticket)
	}
	if !injected {
		t.Fatal("conflict injection never ran")
	}

	var count int64
	if err := db.Model(&models.Ticket{}).Where("idempotency_key = ?", "key-race").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("duplicate rows = %d, want exactly 1", count)
	}
}
