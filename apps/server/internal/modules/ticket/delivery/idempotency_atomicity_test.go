package delivery

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"
	ticketcontract "servify/apps/server/internal/modules/ticket/contract"
)

// TestCreateTicketSameKeyIsIdempotentNoOrphans 同幂等键重试：第一次创建成功，
// 第二次同键调用返回同一工单，不产生重复行、不重复初始状态记录、不重复
// outbox 事件（验收：同键重试不产生孤儿记录或重复单）。
func TestCreateTicketSameKeyIsIdempotentNoOrphans(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})
	ctx := context.Background()
	seedTicketUser(t, db, 1, "alice")

	req := &ticketcontract.CreateTicketRequest{
		Title:          "dup ticket",
		CustomerID:     1,
		IdempotencyKey: "req-abc-123",
	}
	first, err := adapter.CreateTicket(ctx, req)
	if err != nil {
		t.Fatalf("first CreateTicket: %v", err)
	}
	second, err := adapter.CreateTicket(ctx, req)
	if err != nil {
		t.Fatalf("second CreateTicket: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same ticket id on idempotent retry, got %d and %d", first.ID, second.ID)
	}

	var tickets []models.Ticket
	if err := db.Where("idempotency_key = ?", "req-abc-123").Find(&tickets).Error; err != nil {
		t.Fatalf("count tickets: %v", err)
	}
	if len(tickets) != 1 {
		t.Fatalf("expected exactly 1 ticket with key, got %d", len(tickets))
	}

	var history []models.TicketStatus
	if err := db.Where("ticket_id = ?", first.ID).Find(&history).Error; err != nil {
		t.Fatalf("count status history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected exactly 1 initial status record, got %d", len(history))
	}

	var outboxRows []models.TicketOutbox
	if err := db.Where("aggregate_id = ?", ticketAgg(first.ID)).Find(&outboxRows).Error; err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if len(outboxRows) != 1 {
		t.Fatalf("expected exactly 1 outbox event, got %d", len(outboxRows))
	}
}

func ticketAgg(id uint) string { return "ticket:" + itoa(id) }

func itoa(id uint) string {
	if id == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for id > 0 {
		i--
		buf[i] = byte('0' + id%10)
		id /= 10
	}
	return string(buf[i:])
}

// TestCreateTicketDifferentKeysProduceDistinctTickets 不同幂等键产生不同工单。
func TestCreateTicketDifferentKeysProduceDistinctTickets(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})
	ctx := context.Background()
	seedTicketUser(t, db, 1, "alice")

	first, err := adapter.CreateTicket(ctx, &ticketcontract.CreateTicketRequest{
		Title: "t1", CustomerID: 1, IdempotencyKey: "k-1",
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := adapter.CreateTicket(ctx, &ticketcontract.CreateTicketRequest{
		Title: "t2", CustomerID: 1, IdempotencyKey: "k-2",
	})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("expected distinct tickets, got same id %d", first.ID)
	}
}

// TestCreateTicketAtomicRollbackNoOrphans 逐步注入故障：事务中段（outbox 写入）
// 失败时，工单行与初始状态记录一并回滚——不产生孤儿记录（验收：逐步注入故障
// 不产生孤儿记录）。
func TestCreateTicketAtomicRollbackNoOrphans(t *testing.T) {
	db := newTicketDeliveryDB(t)
	adapter := newTicketDeliveryAdapter(t, db, &recordingBus{})
	ctx := context.Background()
	seedTicketUser(t, db, 1, "alice")

	// 删除 ticket_outbox 表，使事务内 outbox 写入失败：工单与状态史应一并回滚。
	if err := db.Migrator().DropTable(&models.TicketOutbox{}); err != nil {
		t.Fatalf("drop outbox: %v", err)
	}
	_, err := adapter.CreateTicket(ctx, &ticketcontract.CreateTicketRequest{
		Title: "atomic fail", CustomerID: 1, IdempotencyKey: "fail-1",
	})
	if err == nil {
		t.Fatal("expected atomic create error when outbox table missing")
	}

	var tickets []models.Ticket
	if err := db.Where("idempotency_key = ?", "fail-1").Find(&tickets).Error; err != nil {
		t.Fatalf("count tickets: %v", err)
	}
	if len(tickets) != 0 {
		t.Fatalf("expected no orphan ticket row after failed transaction, got %d", len(tickets))
	}

	var history []models.TicketStatus
	if err := db.Find(&history).Error; err != nil {
		t.Fatalf("count status history: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("expected no orphan status record after failed transaction, got %d", len(history))
	}
}
