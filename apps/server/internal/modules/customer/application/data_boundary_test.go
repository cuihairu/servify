package application

// V1.0 收敛 B2-2（docs/v1-convergence-plan.md §9.3-2）：数据边界服务的
// 单元行为——导出聚合、擦除分支（有会话/无会话/有工单/无工单）、逐级
// 错误传播、非法入参拒收。仓储全部走桩，不碰数据库。

import (
	"context"
	"errors"
	"testing"
	"time"

	"servify/apps/server/internal/models"
)

// stubDataBoundaryRepo 可编排错误的 DataBoundaryRepository 桩：每个方法
// 的 err 字段非 nil 时直接返回，否则返回预置数据并记录调用次数。
type stubDataBoundaryRepo struct {
	loadErr, sessionsErr, messagesErr, ticketsErr, commentsErr, filesErr  error
	eraseErr, scrubMsgErr, scrubTicketErr, scrubCommentErr, deleteFileErr error

	customer *models.Customer
	user     *models.User
	sessions []models.Session
	messages []models.Message
	tickets  []models.Ticket
	comment  []models.TicketComment
	files    []models.TicketFile

	loads, scrubs, commentsScrubs, filesDeletes int
}

func (s *stubDataBoundaryRepo) LoadCustomerWithUser(context.Context, uint) (*models.Customer, *models.User, error) {
	s.loads++
	if s.loadErr != nil {
		return nil, nil, s.loadErr
	}
	return s.customer, s.user, nil
}

func (s *stubDataBoundaryRepo) ListSessionsByUser(context.Context, uint) ([]models.Session, error) {
	if s.sessionsErr != nil {
		return nil, s.sessionsErr
	}
	return s.sessions, nil
}

func (s *stubDataBoundaryRepo) ListMessagesBySessions(context.Context, []string) ([]models.Message, error) {
	if s.messagesErr != nil {
		return nil, s.messagesErr
	}
	return s.messages, nil
}

func (s *stubDataBoundaryRepo) ListTicketsByUser(context.Context, uint) ([]models.Ticket, error) {
	if s.ticketsErr != nil {
		return nil, s.ticketsErr
	}
	return s.tickets, nil
}

func (s *stubDataBoundaryRepo) ListCommentsByTickets(context.Context, []uint) ([]models.TicketComment, error) {
	if s.commentsErr != nil {
		return nil, s.commentsErr
	}
	return s.comment, nil
}

func (s *stubDataBoundaryRepo) ListFilesByTickets(context.Context, []uint) ([]models.TicketFile, error) {
	if s.filesErr != nil {
		return nil, s.filesErr
	}
	return s.files, nil
}

func (s *stubDataBoundaryRepo) EraseCustomerAndUser(context.Context, uint, uint) error {
	if s.eraseErr != nil {
		return s.eraseErr
	}
	return nil
}

func (s *stubDataBoundaryRepo) ScrubMessagesBySessions(context.Context, []string) (int64, error) {
	s.scrubs++
	if s.scrubMsgErr != nil {
		return 0, s.scrubMsgErr
	}
	return 3, nil
}

func (s *stubDataBoundaryRepo) ScrubTicketsByUser(context.Context, uint) (int64, error) {
	if s.scrubTicketErr != nil {
		return 0, s.scrubTicketErr
	}
	return 2, nil
}

func (s *stubDataBoundaryRepo) ScrubCommentsByTickets(context.Context, []uint) (int64, error) {
	s.commentsScrubs++
	if s.scrubCommentErr != nil {
		return 0, s.scrubCommentErr
	}
	return 1, nil
}

func (s *stubDataBoundaryRepo) DeleteFilesByTickets(context.Context, []uint) (int64, error) {
	s.filesDeletes++
	if s.deleteFileErr != nil {
		return 0, s.deleteFileErr
	}
	return 4, nil
}

// dataBoundaryFixture 档案与用户齐备的默认桩（两条会话、一张工单）。
func dataBoundaryFixture() *stubDataBoundaryRepo {
	return &stubDataBoundaryRepo{
		customer: &models.Customer{UserID: 7},
		user:     &models.User{ID: 7},
		sessions: []models.Session{{ID: "conv-1"}, {ID: "conv-2"}},
		messages: []models.Message{{Content: "hi"}},
		tickets:  []models.Ticket{{ID: 11}},
		comment:  []models.TicketComment{{Content: "c"}},
		files:    []models.TicketFile{{FileName: "a.png"}},
	}
}

func TestExportCustomerDataRejectsZeroID(t *testing.T) {
	svc := NewDataBoundaryService(&stubDataBoundaryRepo{})
	if _, err := svc.ExportCustomerData(context.Background(), 0); err == nil {
		t.Fatal("customer_id=0 must be rejected")
	}
}

func TestExportCustomerDataAggregatesAllSlices(t *testing.T) {
	repo := dataBoundaryFixture()
	svc := NewDataBoundaryService(repo)
	export, err := svc.ExportCustomerData(context.Background(), 1)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if export.ExportedAt.IsZero() || export.Customer == nil || export.Profile.ID != 7 {
		t.Fatalf("export head = %+v", export)
	}
	if len(export.Sessions) != 2 || len(export.Tickets) != 1 || len(export.Comments) != 1 || len(export.Files) != 1 {
		t.Fatalf("aggregation slices wrong: %+v", export)
	}
	if len(export.Catalog) == 0 {
		t.Fatal("catalog must be attached")
	}
	// 凭证类永不导出：结构体本身不含凭证字段，清单里标注 never-exported。
	for _, item := range export.Catalog {
		if item.Category == "凭证与密钥" && (item.Export || item.Erasure != "never-exported") {
			t.Fatalf("credentials must stay non-exported: %+v", item)
		}
	}
}

func TestExportCustomerDataErrorPropagation(t *testing.T) {
	sentinel := errors.New("boom")
	cases := []struct {
		name string
		mut  func(*stubDataBoundaryRepo)
	}{
		{"load", func(r *stubDataBoundaryRepo) { r.loadErr = sentinel }},
		{"sessions", func(r *stubDataBoundaryRepo) { r.sessionsErr = sentinel }},
		{"messages", func(r *stubDataBoundaryRepo) { r.messagesErr = sentinel }},
		{"tickets", func(r *stubDataBoundaryRepo) { r.ticketsErr = sentinel }},
		{"comments", func(r *stubDataBoundaryRepo) { r.commentsErr = sentinel }},
		{"files", func(r *stubDataBoundaryRepo) { r.filesErr = sentinel }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := dataBoundaryFixture()
			tc.mut(repo)
			if _, err := NewDataBoundaryService(repo).ExportCustomerData(context.Background(), 1); !errors.Is(err, sentinel) {
				t.Fatalf("err = %v want sentinel", err)
			}
		})
	}
}

func TestEraseCustomerDataRejectsZeroID(t *testing.T) {
	svc := NewDataBoundaryService(&stubDataBoundaryRepo{})
	if _, err := svc.EraseCustomerData(context.Background(), 0); err == nil {
		t.Fatal("customer_id=0 must be rejected")
	}
}

func TestEraseCustomerDataFullHitCounts(t *testing.T) {
	repo := dataBoundaryFixture()
	svc := NewDataBoundaryService(repo)
	result, err := svc.EraseCustomerData(context.Background(), 1)
	if err != nil {
		t.Fatalf("erase: %v", err)
	}
	if result.CustomerID != 1 || result.UserID != 7 {
		t.Fatalf("anchors = %+v", result)
	}
	if result.SessionsHit != 2 || result.MessagesHit != 3 || result.TicketsHit != 2 ||
		result.CommentsHit != 1 || result.FilesDeleted != 4 {
		t.Fatalf("hit counts = %+v", result)
	}
	if result.ErasedAt.IsZero() {
		t.Fatal("erased_at must be stamped")
	}
}

func TestEraseCustomerDataSkipsScrubWhenNoChildren(t *testing.T) {
	repo := dataBoundaryFixture()
	repo.sessions = nil
	repo.tickets = nil
	result, err := NewDataBoundaryService(repo).EraseCustomerData(context.Background(), 1)
	if err != nil {
		t.Fatalf("erase: %v", err)
	}
	if result.SessionsHit != 0 || result.CommentsHit != 0 || result.FilesDeleted != 0 {
		t.Fatalf("no-children erase should skip scrubs: %+v", result)
	}
	if repo.scrubs != 0 || repo.commentsScrubs != 0 || repo.filesDeletes != 0 {
		t.Fatalf("scrub calls happened without children: %d/%d/%d",
			repo.scrubs, repo.commentsScrubs, repo.filesDeletes)
	}
}

func TestEraseCustomerDataErrorPropagation(t *testing.T) {
	sentinel := errors.New("boom")
	cases := []struct {
		name string
		mut  func(*stubDataBoundaryRepo)
	}{
		{"load", func(r *stubDataBoundaryRepo) { r.loadErr = sentinel }},
		{"sessions", func(r *stubDataBoundaryRepo) { r.sessionsErr = sentinel }},
		{"tickets", func(r *stubDataBoundaryRepo) { r.ticketsErr = sentinel }},
		{"erase", func(r *stubDataBoundaryRepo) { r.eraseErr = sentinel }},
		{"scrub messages", func(r *stubDataBoundaryRepo) { r.scrubMsgErr = sentinel }},
		{"scrub tickets", func(r *stubDataBoundaryRepo) { r.scrubTicketErr = sentinel }},
		{"scrub comments", func(r *stubDataBoundaryRepo) { r.scrubCommentErr = sentinel }},
		{"delete files", func(r *stubDataBoundaryRepo) { r.deleteFileErr = sentinel }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := dataBoundaryFixture()
			tc.mut(repo)
			if _, err := NewDataBoundaryService(repo).EraseCustomerData(context.Background(), 1); !errors.Is(err, sentinel) {
				t.Fatalf("err = %v want sentinel", err)
			}
		})
	}
}

func TestEraseCustomerDataIdempotentShape(t *testing.T) {
	// 幂等口径：重复擦除返回同一统计形状（桩数据不变）。
	repo := dataBoundaryFixture()
	svc := NewDataBoundaryService(repo)
	fixed := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixed }
	first, err := svc.EraseCustomerData(context.Background(), 1)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := svc.EraseCustomerData(context.Background(), 1)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.SessionsHit != second.SessionsHit || first.MessagesHit != second.MessagesHit ||
		!first.ErasedAt.Equal(second.ErasedAt) {
		t.Fatalf("shape drifted: %+v vs %+v", first, second)
	}
}
