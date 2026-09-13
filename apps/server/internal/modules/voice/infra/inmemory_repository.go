package infra

import (
	"context"
	"fmt"
	"sync"
	"time"

	voiceapp "servify/apps/server/internal/modules/voice/application"
)

type InMemoryRepository struct {
	mu    sync.Mutex
	calls map[string]*voiceapp.CallDTO
}

func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{calls: make(map[string]*voiceapp.CallDTO)}
}

func (r *InMemoryRepository) StartCall(ctx context.Context, cmd voiceapp.StartCallCommand) (*voiceapp.CallDTO, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	callID := cmd.CallID
	if callID == "" {
		callID = cmd.ConnectionID
	}
	now := time.Now()
	call := &voiceapp.CallDTO{
		ID:        callID,
		SessionID: cmd.SessionID,
		Status:    "started",
		StartedAt: now,
	}
	r.calls[callID] = call
	return call, nil
}

func (r *InMemoryRepository) AnswerCall(ctx context.Context, cmd voiceapp.AnswerCallCommand) (*voiceapp.CallDTO, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	call, ok := r.calls[cmd.CallID]
	if !ok {
		return nil, fmt.Errorf("call not found")
	}
	now := time.Now()
	call.Status = "answered"
	call.AnsweredAt = &now
	return call, nil
}

func (r *InMemoryRepository) HoldCall(ctx context.Context, cmd voiceapp.HoldCallCommand) (*voiceapp.CallDTO, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	call, ok := r.calls[cmd.CallID]
	if !ok {
		return nil, fmt.Errorf("call not found")
	}
	now := time.Now()
	call.Status = "held"
	call.HeldAt = &now
	return call, nil
}

func (r *InMemoryRepository) ResumeCall(ctx context.Context, cmd voiceapp.ResumeCallCommand) (*voiceapp.CallDTO, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	call, ok := r.calls[cmd.CallID]
	if !ok {
		return nil, fmt.Errorf("call not found")
	}
	now := time.Now()
	call.Status = "answered"
	call.ResumedAt = &now
	return call, nil
}

func (r *InMemoryRepository) EndCall(ctx context.Context, cmd voiceapp.EndCallCommand) (*voiceapp.CallDTO, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	call, ok := r.calls[cmd.CallID]
	if !ok {
		return nil, fmt.Errorf("call not found")
	}
	now := time.Now()
	call.Status = "ended"
	call.EndedAt = &now
	return call, nil
}

func (r *InMemoryRepository) TransferCall(ctx context.Context, cmd voiceapp.TransferCallCommand) (*voiceapp.CallDTO, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	call, ok := r.calls[cmd.CallID]
	if !ok {
		return nil, fmt.Errorf("call not found")
	}
	call.Status = "transferred"
	call.TransferToAgent = &cmd.ToAgentID
	return call, nil
}

func (r *InMemoryRepository) GetCall(callID string) (*voiceapp.CallDTO, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	call, ok := r.calls[callID]
	if !ok {
		return nil, false
	}

	copy := *call
	return &copy, true
}

// FindByID 供协调层状态守卫查询;未找到返回错误。
func (r *InMemoryRepository) FindByID(ctx context.Context, callID string) (*voiceapp.CallDTO, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	call, ok := r.calls[callID]
	if !ok {
		return nil, fmt.Errorf("call not found")
	}
	copy := *call
	return &copy, nil
}
