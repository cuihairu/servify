package application

// Service 的校验、patch 应用与宏应用路径；SQL 语义由 infra 组合测试对账。

import (
	"context"
	"errors"
	"testing"

	platformauth "servify/apps/server/internal/platform/auth"

	"servify/apps/server/internal/models"
)

// scopedContext 构造带租户/工作区的上下文。
func scopedContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

type stubRepo struct {
	list       []models.Macro
	listErr    error
	createErr  error
	get        *models.Macro
	getErr     error
	saved      *models.Macro
	saveErr    error
	deleteErr  error
	ticketErr  error
	comment    *models.TicketComment
	commentErr error
}

func (r *stubRepo) List(ctx context.Context) ([]models.Macro, error) { return r.list, r.listErr }

func (r *stubRepo) Create(ctx context.Context, macro *models.Macro) error { return r.createErr }

func (r *stubRepo) GetScoped(ctx context.Context, id uint) (*models.Macro, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.get, nil
}

func (r *stubRepo) Save(ctx context.Context, macro *models.Macro) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saved = macro
	return nil
}

func (r *stubRepo) Delete(ctx context.Context, id uint) error { return r.deleteErr }

func (r *stubRepo) GetTicket(ctx context.Context, id uint) (*models.Ticket, error) {
	if r.ticketErr != nil {
		return nil, r.ticketErr
	}
	return &models.Ticket{}, nil
}

func (r *stubRepo) CreateComment(ctx context.Context, comment *models.TicketComment) error {
	if r.commentErr != nil {
		return r.commentErr
	}
	r.comment = comment
	return nil
}

func TestServiceCreate(t *testing.T) {
	tests := []struct {
		name    string
		req     *MacroCreateRequest
		wantErr bool
	}{
		{name: "valid", req: &MacroCreateRequest{Name: "m", Content: "c"}},
		{name: "with language", req: &MacroCreateRequest{Name: "m", Content: "c", Language: "en"}},
		{name: "nil request", req: nil, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubRepo{}
			macro, err := NewService(repo).Create(context.Background(), tt.req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Create() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && tt.req.Language == "" && macro.Language != "zh" {
				t.Fatalf("expected default language zh, got %q", macro.Language)
			}
		})
	}

	// 指定语言保留 + 租户/工作区从上下文组装
	repo := &stubRepo{}
	macro, err := NewService(repo).Create(scopedContext("t1", "w1"), &MacroCreateRequest{Name: "m", Content: "c", Language: "en"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if macro.Language != "en" || macro.TenantID != "t1" || macro.WorkspaceID != "w1" {
		t.Fatalf("unexpected macro: %+v", macro)
	}
	if _, err := NewService(&stubRepo{createErr: errors.New("boom")}).Create(context.Background(), &MacroCreateRequest{Name: "m", Content: "c"}); err == nil {
		t.Fatal("expected create error propagation")
	}
}

func TestDefaultLang(t *testing.T) {
	if defaultLang("") != "zh" {
		t.Fatal("expected zh default")
	}
	if defaultLang("en") != "en" {
		t.Fatal("expected en preserved")
	}
}

func TestServiceUpdatePatch(t *testing.T) {
	desc, lang := "new desc", "en"
	content, activeFalse := "new content", false
	activeTrue := true
	req := &MacroUpdateRequest{Description: &desc, Content: &content, Language: &lang, Active: &activeFalse}
	stored := &models.Macro{ID: 3, Name: "m", Content: "old", Language: "zh", Active: true}
	repo := &stubRepo{get: stored}

	out, err := NewService(repo).Update(context.Background(), 3, req)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if out.Content != "new content" || out.Description != "new desc" || out.Language != "en" || out.Active {
		t.Fatalf("patch not applied: %+v", out)
	}
	if out.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt refreshed")
	}

	// nil 指针字段保持原值
	req2 := &MacroUpdateRequest{Active: &activeTrue}
	out, err = NewService(&stubRepo{get: &models.Macro{ID: 3, Content: "old", Language: "zh", Active: true}}).Update(context.Background(), 3, req2)
	if err != nil || !out.Active || out.Content != "old" || out.Language != "zh" {
		t.Fatalf("partial patch unexpected: %+v %v", out, err)
	}

	if _, err := NewService(&stubRepo{}).Update(context.Background(), 1, nil); err == nil {
		t.Fatal("expected nil request error")
	}
	if _, err := NewService(&stubRepo{getErr: errors.New("boom get")}).Update(context.Background(), 1, req); err == nil {
		t.Fatal("expected get error propagation")
	}
	if _, err := NewService(&stubRepo{get: stored, saveErr: errors.New("boom save")}).Update(context.Background(), 3, req); err == nil {
		t.Fatal("expected save error propagation")
	}
}

func TestServiceApplyToTicket(t *testing.T) {
	ctx := context.Background()
	active := &models.Macro{ID: 1, Name: "m", Content: "hello", Active: true}
	inactive := &models.Macro{ID: 2, Name: "n", Content: "old", Active: false}

	repo := &stubRepo{get: active}
	comment, err := NewService(repo).ApplyToTicket(ctx, 1, 7, 9)
	if err != nil {
		t.Fatalf("ApplyToTicket() error = %v", err)
	}
	if comment.TicketID != 7 || comment.UserID != 9 || comment.Content != "hello" || comment.Type != "system" {
		t.Fatalf("unexpected comment: %+v", comment)
	}
	if repo.comment == nil || repo.comment.CreatedAt.IsZero() {
		t.Fatal("expected comment persisted with timestamp")
	}

	if _, err := NewService(&stubRepo{get: inactive}).ApplyToTicket(ctx, 2, 7, 9); !errors.Is(err, ErrMacroInactive) {
		t.Fatalf("ApplyToTicket() err = %v, want %v", err, ErrMacroInactive)
	}
	if _, err := NewService(&stubRepo{getErr: ErrMacroNotFound}).ApplyToTicket(ctx, 99, 7, 9); !errors.Is(err, ErrMacroNotFound) {
		t.Fatalf("ApplyToTicket() err = %v, want %v", err, ErrMacroNotFound)
	}
	if _, err := NewService(&stubRepo{get: active, ticketErr: ErrTicketNotFound}).ApplyToTicket(ctx, 1, 99, 9); !errors.Is(err, ErrTicketNotFound) {
		t.Fatalf("ApplyToTicket() err = %v, want %v", err, ErrTicketNotFound)
	}
	// 评论落库失败传播
	if _, err := NewService(&stubRepo{get: active, commentErr: errors.New("boom comment")}).ApplyToTicket(ctx, 1, 7, 9); err == nil {
		t.Fatal("expected comment create error propagation")
	}
}

func TestServiceListAndDelete(t *testing.T) {
	want := []models.Macro{{Name: "m"}}
	got, err := NewService(&stubRepo{list: want}).List(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("List() = %+v, %v", got, err)
	}
	if _, err := NewService(&stubRepo{listErr: errors.New("boom")}).List(context.Background()); err == nil {
		t.Fatal("expected list error")
	}
	if err := NewService(&stubRepo{}).Delete(context.Background(), 1); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := NewService(&stubRepo{deleteErr: ErrMacroNotFound}).Delete(context.Background(), 1); !errors.Is(err, ErrMacroNotFound) {
		t.Fatalf("Delete() err = %v, want %v", err, ErrMacroNotFound)
	}
}
