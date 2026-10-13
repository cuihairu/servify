//go:build integration
// +build integration

package infra

import (
	"context"
	"testing"
)

// PUT 改派负载回归（todo「生产侧派单竞态收口遗留」第一刀）：通用 UpdateTicket
// 可改派 agent_id 而旧实现不调 current_load，坐席负载随每次改派漂移。修复口径：
// 事务内先锁工单行取库内 agent_id，updates 带 agent_id 且与库值不同才增减
// 负载；增减一律按锁定库值，不按调用方事务外预读的 stale 值。
//
// 两个流各持同一预读值先后开事务——与生产竞态等价（行锁串行化后后一事务
// 读到前一事务提交的库内值）。

// 单流改派 A->B：B 加 1、A 减 1，工单 agent 落 B。
func TestUpdateTicketModelReassignSyncsLoad(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	raceSeedAgent(t, db, 11, 2) // A 持两张单
	raceSeedAgent(t, db, 22, 0)
	ticket := raceSeedTicket(t, db, raceUintPtr(11))
	raceSeedTicket(t, db, raceUintPtr(11))

	if err := repo.UpdateTicketModelWithStatusAndCustomFields(ctx, ticket.ID,
		map[string]interface{}{"agent_id": uint(22)}, nil, false, nil, nil); err != nil {
		t.Fatalf("reassign A->B: %v", err)
	}
	if got := raceTicketAgent(t, db, ticket.ID); got == nil || *got != 22 {
		t.Fatalf("ticket agent = %v, want 22", got)
	}
	if got := raceLoad(t, db, 11); got != 1 {
		t.Fatalf("agent 11 load = %d, want 1 (still owns ticket 2)", got)
	}
	if got := raceLoad(t, db, 22); got != 1 {
		t.Fatalf("agent 22 load = %d, want 1", got)
	}
}

// 两流并发改派：各预读 agent_id=11，先后把工单转给 B 再转给 C。第二流按锁定
// 库值（B）减 1，不能按 stale 预读（A）减 1——否则 A 被减两次、B 漏减。
func TestUpdateTicketModelStaleReassignNoLoadDrift(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	raceSeedAgent(t, db, 11, 2)
	raceSeedAgent(t, db, 22, 0)
	raceSeedAgent(t, db, 33, 0)
	ticket := raceSeedTicket(t, db, raceUintPtr(11))
	raceSeedTicket(t, db, raceUintPtr(11))

	if err := repo.UpdateTicketModelWithStatusAndCustomFields(ctx, ticket.ID,
		map[string]interface{}{"agent_id": uint(22)}, nil, false, nil, nil); err != nil {
		t.Fatalf("flow 1 reassign A->B: %v", err)
	}
	if err := repo.UpdateTicketModelWithStatusAndCustomFields(ctx, ticket.ID,
		map[string]interface{}{"agent_id": uint(33)}, nil, false, nil, nil); err != nil {
		t.Fatalf("flow 2 reassign B->C: %v", err)
	}

	if got := raceTicketAgent(t, db, ticket.ID); got == nil || *got != 33 {
		t.Fatalf("ticket agent = %v, want 33", got)
	}
	if got := raceLoad(t, db, 11); got != 1 {
		t.Fatalf("agent 11 load = %d, want 1 (stale pre-read must not decrement)", got)
	}
	if got := raceLoad(t, db, 22); got != 0 {
		t.Fatalf("agent 22 load = %d, want 0 (released from locked DB value)", got)
	}
	if got := raceLoad(t, db, 33); got != 1 {
		t.Fatalf("agent 33 load = %d, want 1", got)
	}
}

// 两流并发「取消改派」（agent_id=0 是 unassign 哨兵）：按锁定库值减 1，
// 不按 stale 预读——第二流读到工单已清空指派时不应对坐席重复减。
func TestUpdateTicketModelStaleUnassignNoLoadDrift(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	raceSeedAgent(t, db, 11, 2)
	raceSeedAgent(t, db, 22, 0)
	ticket := raceSeedTicket(t, db, raceUintPtr(11))
	raceSeedTicket(t, db, raceUintPtr(11))

	if err := repo.UpdateTicketModelWithStatusAndCustomFields(ctx, ticket.ID,
		map[string]interface{}{"agent_id": uint(0)}, nil, false, nil, nil); err != nil {
		t.Fatalf("flow 1 unassign: %v", err)
	}
	if err := repo.UpdateTicketModelWithStatusAndCustomFields(ctx, ticket.ID,
		map[string]interface{}{"agent_id": uint(0)}, nil, false, nil, nil); err != nil {
		t.Fatalf("flow 2 unassign: %v", err)
	}

	if got := raceTicketAgent(t, db, ticket.ID); got != nil {
		t.Fatalf("ticket agent = %v, want nil", *got)
	}
	if got := raceLoad(t, db, 11); got != 1 {
		t.Fatalf("agent 11 load = %d, want 1 (stale pre-read must not decrement)", got)
	}
	if got := raceLoad(t, db, 22); got != 0 {
		t.Fatalf("agent 22 load = %d, want 0", got)
	}
}

// 工单从未指派（agent_id 为空）时改派：目标坐席加 1，无来源可减。
func TestUpdateTicketModelAssignFromNilSyncsLoad(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	raceSeedAgent(t, db, 11, 0)
	ticket := raceSeedTicket(t, db, nil)

	if err := repo.UpdateTicketModelWithStatusAndCustomFields(ctx, ticket.ID,
		map[string]interface{}{"agent_id": uint(11)}, nil, false, nil, nil); err != nil {
		t.Fatalf("assign nil->A: %v", err)
	}
	if got := raceTicketAgent(t, db, ticket.ID); got == nil || *got != 11 {
		t.Fatalf("ticket agent = %v, want 11", got)
	}
	if got := raceLoad(t, db, 11); got != 1 {
		t.Fatalf("agent 11 load = %d, want 1", got)
	}
}

// updates 不含 agent_id（仅改标题/分类等）时坐席负载不得变动。
func TestUpdateTicketModelWithoutAgentKeepsLoad(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	raceSeedAgent(t, db, 11, 1)
	ticket := raceSeedTicket(t, db, raceUintPtr(11))

	if err := repo.UpdateTicketModelWithStatusAndCustomFields(ctx, ticket.ID,
		map[string]interface{}{"title": "renamed", "category": "billing"}, nil, false, nil, nil); err != nil {
		t.Fatalf("plain update: %v", err)
	}
	if got := raceLoad(t, db, 11); got != 1 {
		t.Fatalf("agent 11 load = %d, want 1 (load must not move without agent change)", got)
	}
}

// 改派到同一坐席（幂等）不得增减负载。
func TestUpdateTicketModelSameAgentKeepsLoad(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	raceSeedAgent(t, db, 11, 1)
	ticket := raceSeedTicket(t, db, raceUintPtr(11))

	if err := repo.UpdateTicketModelWithStatusAndCustomFields(ctx, ticket.ID,
		map[string]interface{}{"agent_id": uint(11)}, nil, false, nil, nil); err != nil {
		t.Fatalf("reassign to same agent: %v", err)
	}
	if got := raceLoad(t, db, 11); got != 1 {
		t.Fatalf("agent 11 load = %d, want 1 (same-agent reassign is a no-op for load)", got)
	}
}

// ticketAgentFromRaw 分支覆盖：nil 接口（updates 显式置 nil）与非 uint 类型
// （调用方传入非法值）都归一为 nil（未指派哨兵）。
func TestTicketAgentFromRawBranches(t *testing.T) {
	if got := ticketAgentFromRaw(nil); got != nil {
		t.Fatalf("nil raw = %v, want nil", got)
	}
	if got := ticketAgentFromRaw("11"); got != nil {
		t.Fatalf("string raw = %v, want nil", got)
	}
	if got := ticketAgentFromRaw(11); got != nil {
		t.Fatalf("int raw = %v, want nil", got)
	}
	if got := ticketAgentFromRaw(uint(11)); got == nil || *got != 11 {
		t.Fatalf("uint raw = %v, want 11", got)
	}
	if got := ticketAgentFromRaw(uint(0)); got != nil {
		t.Fatalf("zero raw = %v, want nil (sentinel)", got)
	}
	v := uint(7)
	if got := ticketAgentFromRaw(&v); got == nil || *got != 7 {
		t.Fatalf("ptr raw = %v, want 7", got)
	}
	z := uint(0)
	if got := ticketAgentFromRaw(&z); got != nil {
		t.Fatalf("zero ptr raw = %v, want nil (sentinel)", got)
	}
}

// syncAgentLoadDelta 增减分支错误路径：库连接关闭后 UPDATE/UpdateColumn 均报错，
// 错误原样上抛（负载同步不静默——失败即整体回滚）。
func TestSyncAgentLoadDeltaErrors(t *testing.T) {
	db := newTicketInfraTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	from, to := uint(11), uint(9)
	if err := syncAgentLoadDelta(db, &from, &to); err == nil {
		t.Fatal("decrement must error on closed db")
	}
	// 来源为空时跳过扣减，直接命中加 1 的错误分支。
	if err := syncAgentLoadDelta(db, nil, &to); err == nil {
		t.Fatal("increment must error on closed db")
	}
}

// 事务内锁工单行失败（工单不存在）时错误上抛，不静默跳过负载同步。
func TestUpdateTicketModelLockError(t *testing.T) {
	db := newTicketInfraTestDB(t)
	repo := NewGormRepository(db)

	err := repo.UpdateTicketModelWithStatusAndCustomFields(context.Background(), 99999,
		map[string]interface{}{"agent_id": uint(5)}, nil, false, nil, nil)
	if err == nil {
		t.Fatal("expected error locking non-existent ticket")
	}
}
