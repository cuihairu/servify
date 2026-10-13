//go:build integration
// +build integration

package infra

import (
	"context"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/ticket/domain"

	"gorm.io/gorm"
)

// 生产侧派单竞态回归：并发显式派单与自动派单在事务外预读工单 agent_id，
// 事务内按 stale 值增减 current_load 会造成坐席负载漂移。修复口径：
// 事务内先锁工单行（postgres/mysql FOR UPDATE），再以库内 agent_id 为增减依据。
// 两个流预读同 stale 值、随后各自开事务——与生产竞态等价（行锁串行化后
// 后一事务读到前一事务提交的库内值）。

func raceSeedAgent(t *testing.T, db *gorm.DB, userID uint, load int) models.Agent {
	t.Helper()
	agent := models.Agent{
		UserID:        userID,
		Status:        "online",
		MaxConcurrent: 10,
		CurrentLoad:   load,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("seed agent %d: %v", userID, err)
	}
	return agent
}

func raceSeedTicket(t *testing.T, db *gorm.DB, agentID *uint) models.Ticket {
	t.Helper()
	ticket := models.Ticket{
		Title:      "race ticket",
		CustomerID: 1,
		AgentID:    agentID,
		Status:     "assigned",
		Priority:   "normal",
		Category:   "support",
		Source:     "web",
	}
	if err := db.Create(&ticket).Error; err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	return ticket
}

func raceLoad(t *testing.T, db *gorm.DB, userID uint) int {
	t.Helper()
	var agent models.Agent
	if err := db.Where("user_id = ?", userID).First(&agent).Error; err != nil {
		t.Fatalf("load agent %d: %v", userID, err)
	}
	return agent.CurrentLoad
}

func raceTicketAgent(t *testing.T, db *gorm.DB, ticketID uint) *uint {
	t.Helper()
	var ticket models.Ticket
	if err := db.Select("id", "agent_id").First(&ticket, ticketID).Error; err != nil {
		t.Fatalf("load ticket %d: %v", ticketID, err)
	}
	return ticket.AgentID
}

func raceUintPtr(v uint) *uint { return &v }

// 两个派单流都预读到 agent_id=nil（显式派单 + 自动派单竞态的最小形态）：
// 修复后第二流按库内值（第一流的坐席）减 1，第一流坐席不留幽灵负载。
func TestAssignTicketModelStaleNilPreviousAgentNoLoadDrift(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	raceSeedAgent(t, db, 11, 0)
	raceSeedAgent(t, db, 22, 0)
	ticket := raceSeedTicket(t, db, nil)

	if err := repo.AssignTicketModel(ctx, ticket.ID, 11, nil, "assigned", "assigned", 1, "assign_ticket"); err != nil {
		t.Fatalf("flow 1 assign to agent 11: %v", err)
	}
	if err := repo.AssignTicketModel(ctx, ticket.ID, 22, nil, "assigned", "assigned", 1, "assign_ticket"); err != nil {
		t.Fatalf("flow 2 assign to agent 22: %v", err)
	}

	if got := raceTicketAgent(t, db, ticket.ID); got == nil || *got != 22 {
		t.Fatalf("ticket agent = %v, want 22", got)
	}
	if got := raceLoad(t, db, 11); got != 0 {
		t.Fatalf("agent 11 load = %d, want 0 (flow 2 must release the DB agent, not a stale nil)", got)
	}
	if got := raceLoad(t, db, 22); got != 1 {
		t.Fatalf("agent 22 load = %d, want 1", got)
	}
}

// 转派竞态：调用方持 stale previousAgentID 进入第二流，
// 修复后第二流按库内当前坐席（B）减 1，而非 stale 的 A。
func TestAssignTicketModelStalePreviousAgentNoLoadDrift(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	raceSeedAgent(t, db, 11, 2) // A 持两张单
	raceSeedAgent(t, db, 22, 0)
	raceSeedAgent(t, db, 33, 0)
	ticket := raceSeedTicket(t, db, raceUintPtr(11))
	raceSeedTicket(t, db, raceUintPtr(11)) // A 的第二张单

	if err := repo.AssignTicketModel(ctx, ticket.ID, 22, raceUintPtr(11), "assigned", "assigned", 1, "transfer_ticket"); err != nil {
		t.Fatalf("flow 1 transfer A->B: %v", err)
	}
	// 第二流预读 stale previous=A，但库内已是 B。
	if err := repo.AssignTicketModel(ctx, ticket.ID, 33, raceUintPtr(11), "assigned", "assigned", 1, "transfer_ticket"); err != nil {
		t.Fatalf("flow 2 transfer to C: %v", err)
	}

	if got := raceTicketAgent(t, db, ticket.ID); got == nil || *got != 33 {
		t.Fatalf("ticket agent = %v, want 33", got)
	}
	if got := raceLoad(t, db, 11); got != 1 {
		t.Fatalf("agent 11 load = %d, want 1 (still owns ticket 2)", got)
	}
	if got := raceLoad(t, db, 22); got != 0 {
		t.Fatalf("agent 22 load = %d, want 0 (released from DB value by flow 2)", got)
	}
	if got := raceLoad(t, db, 33); got != 1 {
		t.Fatalf("agent 33 load = %d, want 1", got)
	}
}

// 取消派单竞态：调用方持 stale agentID，修复后按库内 agent 减 1。
func TestUnassignTicketModelStaleAgentNoLoadDrift(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	raceSeedAgent(t, db, 11, 2)
	raceSeedAgent(t, db, 22, 0)
	ticket := raceSeedTicket(t, db, raceUintPtr(11))
	raceSeedTicket(t, db, raceUintPtr(11)) // A 的第二张单

	if err := repo.AssignTicketModel(ctx, ticket.ID, 22, raceUintPtr(11), "assigned", "assigned", 1, "transfer_ticket"); err != nil {
		t.Fatalf("transfer A->B: %v", err)
	}
	if err := repo.UnassignTicketModel(ctx, ticket.ID, 11, "assigned", "open", 1, "unassign_ticket"); err != nil {
		t.Fatalf("unassign with stale agent 11: %v", err)
	}

	if got := raceTicketAgent(t, db, ticket.ID); got != nil {
		t.Fatalf("ticket agent = %v, want nil", *got)
	}
	if got := raceLoad(t, db, 11); got != 1 {
		t.Fatalf("agent 11 load = %d, want 1 (owns ticket 2; stale param must not decrement)", got)
	}
	if got := raceLoad(t, db, 22); got != 0 {
		t.Fatalf("agent 22 load = %d, want 0 (decremented from DB value)", got)
	}
}

// 关闭工单竞态：调用方持 stale agent，修复后按库内 agent 减 1。
func TestCloseTicketStaleAgentNoLoadDrift(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	raceSeedAgent(t, db, 11, 1)
	raceSeedAgent(t, db, 22, 0)
	ticket := raceSeedTicket(t, db, raceUintPtr(11))

	if err := repo.AssignTicketModel(ctx, ticket.ID, 22, raceUintPtr(11), "assigned", "assigned", 1, "transfer_ticket"); err != nil {
		t.Fatalf("transfer A->B: %v", err)
	}

	now := time.Now()
	stale := &domain.Ticket{
		ID:        ticket.ID,
		AgentID:   raceUintPtr(11),
		Status:    "closed",
		ClosedAt:  &now,
		UpdatedAt: now,
	}
	if err := repo.CloseTicket(ctx, stale, "assigned", 1, "close_ticket"); err != nil {
		t.Fatalf("close with stale agent 11: %v", err)
	}

	if got := raceLoad(t, db, 11); got != 0 {
		t.Fatalf("agent 11 load = %d, want 0", got)
	}
	if got := raceLoad(t, db, 22); got != 0 {
		t.Fatalf("agent 22 load = %d, want 0 (decremented from DB value, not stale param)", got)
	}
}
