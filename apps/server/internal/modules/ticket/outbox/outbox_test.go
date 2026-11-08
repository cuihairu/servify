package outbox

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/platform/eventbus"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newOutboxTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.TicketOutbox{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

type countingBus struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (b *countingBus) Subscribe(string, eventbus.Handler) {}
func (b *countingBus) Publish(_ context.Context, e eventbus.Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
	return nil
}
func (b *countingBus) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}

type failBus struct{}

func (failBus) Subscribe(string, eventbus.Handler) {}
func (failBus) Publish(context.Context, eventbus.Event) error {
	return errors.New("bus down")
}

func TestOutboxAppendAndDeliverExactlyOnce(t *testing.T) {
	db := newOutboxTestDB(t)
	repo := NewGormRepository(db)
	bus := &countingBus{}
	dispatcher := NewBusDispatcher(bus)
	ctx := context.Background()

	row := &models.TicketOutbox{
		EventID:     "evt-1",
		EventName:   "ticket.created",
		AggregateID: "ticket:1",
		Payload:     `{"event_id":"evt-1","event_name":"ticket.created","event_occurred_at":"2026-11-06T00:00:00Z","event_tenant_id":"t1","event_aggregate_id":"ticket:1","ticket_id":1,"customer_id":1,"status":"open"}`,
	}
	require.NoError(t, repo.Append(ctx, row))

	// 两轮：第一轮投递，第二轮无待办（已 delivered 不再领取）。
	got := ProcessDueEvents(ctx, repo, dispatcher, time.Now(), 50)
	require.Equal(t, 1, got)
	require.Equal(t, 1, bus.count())
	got = ProcessDueEvents(ctx, repo, dispatcher, time.Now(), 50)
	require.Equal(t, 0, got)
	require.Equal(t, 1, bus.count())

	var stored models.TicketOutbox
	require.NoError(t, db.First(&stored, row.ID).Error)
	require.Equal(t, StatusDelivered, stored.Status)
	require.NotNil(t, stored.DeliveredAt)
}

func TestOutboxTransientFailureRetriesAndDeduplicates(t *testing.T) {
	db := newOutboxTestDB(t)
	repo := NewGormRepository(db)
	dispatcher := NewBusDispatcher(failBus{})
	ctx := context.Background()

	row := &models.TicketOutbox{
		EventID:     "evt-2",
		EventName:   "ticket.created",
		AggregateID: "ticket:2",
		Payload:     `{"event_id":"evt-2","event_name":"ticket.created","ticket_id":2,"customer_id":2,"status":"open"}`,
	}
	require.NoError(t, repo.Append(ctx, row))

	got := ProcessDueEvents(ctx, repo, dispatcher, time.Now(), 50)
	require.Equal(t, 0, got)
	var stored models.TicketOutbox
	require.NoError(t, db.First(&stored, row.ID).Error)
	require.Equal(t, StatusFailed, stored.Status)
	require.Equal(t, 1, stored.Attempts)
	require.NotEmpty(t, stored.LastError)
	require.NotNil(t, stored.NextAttemptAt)

	// 未到 next_attempt_at 不领取（backoff 生效）。
	got = ProcessDueEvents(ctx, repo, dispatcher, time.Now(), 50)
	require.Equal(t, 0, got)
}

func TestOutboxDeadAfterMaxAttempts(t *testing.T) {
	db := newOutboxTestDB(t)
	repo := NewGormRepository(db)
	dispatcher := NewBusDispatcher(failBus{})
	ctx := context.Background()

	row := &models.TicketOutbox{
		EventID:     "evt-3",
		EventName:   "ticket.created",
		AggregateID: "ticket:3",
		Payload:     `{"event_id":"evt-3","event_name":"ticket.created","ticket_id":3,"customer_id":3,"status":"open"}`,
	}
	require.NoError(t, repo.Append(ctx, row))

	var stored models.TicketOutbox
	for i := 0; i < MaxAttempts; i++ {
		ProcessDueEvents(ctx, repo, dispatcher, time.Now().Add(time.Duration(i)*time.Hour), 50)
		require.NoError(t, db.First(&stored, row.ID).Error)
	}
	require.Equal(t, StatusDead, stored.Status)
	require.Equal(t, MaxAttempts, stored.Attempts)
}

func TestOutboxRecoveryAfterBusRestoredDeliversOnce(t *testing.T) {
	db := newOutboxTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	row := &models.TicketOutbox{
		EventID:     "evt-4",
		EventName:   "ticket.created",
		AggregateID: "ticket:4",
		Payload:     `{"event_id":"evt-4","event_name":"ticket.created","ticket_id":4,"customer_id":4,"status":"open"}`,
	}
	require.NoError(t, repo.Append(ctx, row))

	// 故障期投递失败，恢复后重投；同 event_id 只发布一次（验收：恢复后事件恰投递可去重）。
	got := ProcessDueEvents(ctx, repo, NewBusDispatcher(failBus{}), time.Now(), 50)
	require.Equal(t, 0, got)

	var stored models.TicketOutbox
	require.NoError(t, db.First(&stored, row.ID).Error)
	require.Equal(t, StatusFailed, stored.Status)
	// 回到可领取窗口（backoff 到期）后重试，bus 已恢复。
	bus := &countingBus{}
	got = ProcessDueEvents(ctx, repo, NewBusDispatcher(bus), stored.NextAttemptAt.Add(time.Second), 50)
	require.Equal(t, 1, got)
	require.Equal(t, 1, bus.count())
	got = ProcessDueEvents(ctx, repo, NewBusDispatcher(bus), stored.NextAttemptAt.Add(time.Second), 50)
	require.Equal(t, 0, got)
	require.Equal(t, 1, bus.count())
}

func TestOutboxDispatchNilBusErrors(t *testing.T) {
	d := NewBusDispatcher(nil)
	if err := d.Dispatch(context.Background(), models.TicketOutbox{EventID: "e"}); err == nil {
		t.Fatal("expected error for nil bus")
	}
}

func TestOutboxUnmarshalEventErrors(t *testing.T) {
	if _, err := UnmarshalEvent(models.TicketOutbox{EventID: "e", Payload: ""}); err == nil {
		t.Fatal("expected error for empty payload")
	}
	if _, err := UnmarshalEvent(models.TicketOutbox{EventID: "e", Payload: "not-json"}); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestOutboxProcessNilGuard(t *testing.T) {
	if got := ProcessDueEvents(context.Background(), nil, nil, time.Now(), 50); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestOutboxProcessorAdapter(t *testing.T) {
	db := newOutboxTestDB(t)
	repo := NewGormRepository(db)
	bus := &countingBus{}
	p := &Processor{Repo: repo, Dispatcher: NewBusDispatcher(bus)}
	require.NoError(t, repo.Append(context.Background(), &models.TicketOutbox{
		EventID: "evt-p", EventName: "ticket.created", AggregateID: "ticket:1",
		Payload: `{"event_id":"evt-p","event_name":"ticket.created","ticket_id":1,"customer_id":1,"status":"open"}`,
	}))
	if got := p.ProcessDueEvents(context.Background(), time.Now()); got != 1 {
		t.Fatalf("expected 1 processed, got %d", got)
	}
	if bus.count() != 1 {
		t.Fatalf("expected 1 published, got %d", bus.count())
	}
}

func TestOutboxDispatchUnmarshalError(t *testing.T) {
	d := NewBusDispatcher(&countingBus{})
	if err := d.Dispatch(context.Background(), models.TicketOutbox{EventID: "bad", Payload: "not-json"}); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

type failClaimRepo struct {
	Repository
}

func (failClaimRepo) ClaimDue(context.Context, time.Time, int) ([]models.TicketOutbox, error) {
	return nil, errors.New("claim failed")
}

func TestOutboxProcessClaimError(t *testing.T) {
	if got := ProcessDueEvents(context.Background(), failClaimRepo{}, NewBusDispatcher(&countingBus{}), time.Now(), 50); got != 0 {
		t.Fatalf("expected 0 on claim error, got %d", got)
	}
}

type failDeliverRepo struct {
	Repository
	db *gorm.DB
}

func (f failDeliverRepo) ClaimDue(context.Context, time.Time, int) ([]models.TicketOutbox, error) {
	return []models.TicketOutbox{{ID: 1, Attempts: 0, EventID: "e", EventName: "ticket.created",
		Payload: `{"event_id":"e","event_name":"ticket.created","ticket_id":1,"customer_id":1,"status":"open"}`}}, nil
}
func (f failDeliverRepo) MarkDelivered(context.Context, uint, time.Time) error {
	return errors.New("mark failed")
}
func (f failDeliverRepo) MarkFailed(context.Context, uint, int, string, *time.Time, bool) error {
	return nil
}

func TestOutboxProcessDeliverMarkError(t *testing.T) {
	if got := ProcessDueEvents(context.Background(), failDeliverRepo{}, NewBusDispatcher(&countingBus{}), time.Now(), 50); got != 0 {
		t.Fatalf("expected 0 when mark-delivered fails, got %d", got)
	}
}

// failMarkFailedRepo 让 Dispatch 成功但 MarkFailed 失败——验证 worker 继续
// 处理下一批而不中断（错误路径被吞掉，只计数不返回）。
type failMarkFailedRepo struct {
	Repository
}

func (failMarkFailedRepo) ClaimDue(context.Context, time.Time, int) ([]models.TicketOutbox, error) {
	return []models.TicketOutbox{{ID: 1, Attempts: 0}}, nil
}
func (failMarkFailedRepo) MarkFailed(context.Context, uint, int, string, *time.Time, bool) error {
	return errors.New("mark failed")
}

func TestOutboxProcessDispatchErrorMarkFailedIgnored(t *testing.T) {
	// dispatcher 失败 → MarkFailed 失败被吞掉（不 panic、返回 0）。
	if got := ProcessDueEvents(context.Background(), failMarkFailedRepo{}, NewBusDispatcher(failBus{}), time.Now(), 50); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

// TestOutboxClaimDueAtomicClaimAndStaleReclaim covers the ClaimDue atomic
// claim: a pending row becomes processing (not double-claimed on the same
// batch), a processing row younger than the stale threshold is skipped, and a
// stale processing row (claimed_at older than claimStaleAfter) is reclaimed.
func TestOutboxClaimDueAtomicClaimAndStaleReclaim(t *testing.T) {
	db := newOutboxTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	now := time.Now()

	row := &models.TicketOutbox{
		EventID: "evt-claim", EventName: "ticket.created", AggregateID: "ticket:1",
		Payload: `{"event_id":"evt-claim","event_name":"ticket.created","ticket_id":1,"customer_id":1,"status":"open"}`,
	}
	require.NoError(t, repo.Append(ctx, row))

	// 第一次领取：pending → processing，归本实例。
	claimed, err := repo.ClaimDue(ctx, now, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, StatusProcessing, claimed[0].Status)
	require.NotNil(t, claimed[0].ClaimedAt)

	// 同批再次领取：processing 未过期，不再返回。
	again, err := repo.ClaimDue(ctx, now.Add(time.Second), 10)
	require.NoError(t, err)
	require.Len(t, again, 0)

	// 未过期 processing（模拟并发实例已抢占）不返回。
	staleFuture := now.Add(-claimStaleAfter / 2)
	require.NoError(t, db.Model(&models.TicketOutbox{}).Where("id = ?", row.ID).
		Updates(map[string]interface{}{"claimed_at": staleFuture}).Error)
	young, err := repo.ClaimDue(ctx, now.Add(time.Second), 10)
	require.NoError(t, err)
	require.Len(t, young, 0)

	// 超过阈值：重新领取（崩溃残留回收）。
	stale := now.Add(-claimStaleAfter - time.Minute)
	require.NoError(t, db.Model(&models.TicketOutbox{}).Where("id = ?", row.ID).
		Updates(map[string]interface{}{"claimed_at": stale}).Error)
	reclaimed, err := repo.ClaimDue(ctx, now.Add(time.Second), 10)
	require.NoError(t, err)
	require.Len(t, reclaimed, 1)
	require.Equal(t, row.ID, reclaimed[0].ID)

	// MarkDelivered 只对 processing 生效：delivered 后不再被领取。
	require.NoError(t, repo.MarkDelivered(ctx, row.ID, now))
	var stored models.TicketOutbox
	require.NoError(t, db.First(&stored, row.ID).Error)
	require.Equal(t, StatusDelivered, stored.Status)
	post, err := repo.ClaimDue(ctx, now.Add(2*time.Minute), 10)
	require.NoError(t, err)
	require.Len(t, post, 0)
}

// TestOutboxMarkFailedOnlyProcessing covers MarkFailed being gated on the
// processing status so a raced double-claim cannot mark a re-claimed row.
func TestOutboxMarkFailedOnlyProcessing(t *testing.T) {
	db := newOutboxTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()
	row := &models.TicketOutbox{
		EventID: "evt-fail", EventName: "ticket.created", AggregateID: "ticket:2",
		Payload: `{"event_id":"evt-fail","event_name":"ticket.created","ticket_id":2,"customer_id":2,"status":"open"}`,
	}
	require.NoError(t, repo.Append(ctx, row))
	claimed, err := repo.ClaimDue(ctx, time.Now(), 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	next := time.Now().Add(time.Minute)
	require.NoError(t, repo.MarkFailed(ctx, row.ID, 1, "boom", &next, false))
	var stored models.TicketOutbox
	require.NoError(t, db.First(&stored, row.ID).Error)
	require.Equal(t, StatusFailed, stored.Status)
	require.Equal(t, 1, stored.Attempts)

	// 非 processing（delivered）时 MarkFailed 不改状态：已投递行不受影响。
	// gating 下只有 processing 能 MarkDelivered，须先领取再投递构造 delivered。
	row2 := &models.TicketOutbox{
		EventID: "evt-fail-2", EventName: "ticket.created", AggregateID: "ticket:3",
		Payload: `{"event_id":"evt-fail-2","event_name":"ticket.created","ticket_id":3,"customer_id":3,"status":"open"}`,
	}
	require.NoError(t, repo.Append(ctx, row2))
	// row1 已 failed 且 next_attempt_at 在未来，ClaimDue 只应领取 row2。
	claimed2, err := repo.ClaimDue(ctx, time.Now(), 10)
	require.NoError(t, err)
	require.Len(t, claimed2, 1)
	require.Equal(t, row2.ID, claimed2[0].ID)
	require.NoError(t, repo.MarkDelivered(ctx, row2.ID, time.Now()))
	require.NoError(t, repo.MarkFailed(ctx, row2.ID, 1, "late", &next, false))
	// 复用独立目标变量：stored 已载入 row1（主键 1），GORM 会把既有主键与
	// 实参合并成 id=2 AND id=1，查询恒空。
	var stored2 models.TicketOutbox
	require.NoError(t, db.First(&stored2, row2.ID).Error)
	require.Equal(t, StatusDelivered, stored2.Status)
	require.Equal(t, 0, stored2.Attempts)
}
