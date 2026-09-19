package delivery

// HandlerServiceAdapter 的转发覆盖：逐方法对账透传语义与错误传播。

import (
	"context"
	"errors"
	"testing"

	macroapp "servify/apps/server/internal/modules/macro/application"

	"servify/apps/server/internal/models"
)

type adapterStubRepo struct {
	list      []models.Macro
	listErr   error
	createErr error
	get       *models.Macro
	getErr    error
	ticketErr error
}

func (r *adapterStubRepo) List(ctx context.Context) ([]models.Macro, error) {
	return r.list, r.listErr
}

func (r *adapterStubRepo) Create(ctx context.Context, macro *models.Macro) error { return r.createErr }

func (r *adapterStubRepo) GetScoped(ctx context.Context, id uint) (*models.Macro, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.get, nil
}

func (r *adapterStubRepo) Save(ctx context.Context, macro *models.Macro) error { return nil }

func (r *adapterStubRepo) Delete(ctx context.Context, id uint) error { return nil }

func (r *adapterStubRepo) GetTicket(ctx context.Context, id uint) (*models.Ticket, error) {
	if r.ticketErr != nil {
		return nil, r.ticketErr
	}
	return &models.Ticket{}, nil
}

func (r *adapterStubRepo) CreateComment(ctx context.Context, comment *models.TicketComment) error {
	return nil
}

func TestHandlerServiceAdapter_AllMethods(t *testing.T) {
	repo := &adapterStubRepo{list: []models.Macro{{Name: "m"}}, get: &models.Macro{Name: "m", Active: true}}
	adapter := NewHandlerServiceAdapter(macroapp.NewService(repo))
	if adapter == nil {
		t.Fatal("expected adapter instance")
	}
	ctx := context.Background()

	if got, err := adapter.List(ctx); err != nil || len(got) != 1 {
		t.Fatalf("List: %v %+v", err, got)
	}
	if got, err := adapter.Create(ctx, &MacroCreateRequest{Name: "m", Content: "c"}); err != nil || got == nil {
		t.Fatalf("Create: %v %+v", err, got)
	}
	if got, err := adapter.Update(ctx, 1, &MacroUpdateRequest{}); err != nil || got == nil {
		t.Fatalf("Update: %v %+v", err, got)
	}
	if err := adapter.Delete(ctx, 1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := adapter.ApplyToTicket(ctx, 1, 2, 3); err != nil {
		t.Fatalf("ApplyToTicket: %v", err)
	}
}

func TestHandlerServiceAdapter_ErrorPropagation(t *testing.T) {
	boom := errors.New("boom: macro")
	adapter := NewHandlerServiceAdapter(macroapp.NewService(&adapterStubRepo{listErr: boom, createErr: boom, getErr: boom, ticketErr: boom}))
	ctx := context.Background()

	if _, err := adapter.List(ctx); !errors.Is(err, boom) {
		t.Fatalf("List error propagation: %v", err)
	}
	if _, err := adapter.Create(ctx, &MacroCreateRequest{Name: "m", Content: "c"}); !errors.Is(err, boom) {
		t.Fatalf("Create error propagation: %v", err)
	}
	if _, err := adapter.Update(ctx, 1, &MacroUpdateRequest{}); !errors.Is(err, boom) {
		t.Fatalf("Update error propagation: %v", err)
	}
	if _, err := adapter.ApplyToTicket(ctx, 1, 2, 3); !errors.Is(err, boom) {
		t.Fatalf("ApplyToTicket error propagation: %v", err)
	}
}
