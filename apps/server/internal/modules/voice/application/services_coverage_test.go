package application

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServiceFindCall 覆盖状态守卫查询的命中与未命中。
func TestServiceFindCall(t *testing.T) {
	repo := &stubRepo{}
	svc := NewService(repo, nil)
	ctx := context.Background()

	if _, err := svc.FindCall(ctx, "c1"); err == nil {
		t.Fatal("unknown call must surface the repo error")
	}

	if _, err := svc.StartCall(ctx, StartCallCommand{CallID: "c1", SessionID: "s1"}); err != nil {
		t.Fatalf("StartCall() error = %v", err)
	}
	call, err := svc.FindCall(ctx, "c1")
	require.NoError(t, err)
	require.NotNil(t, call)
	assert.Equal(t, "c1", call.ID)

	boom := errors.New("find boom")
	svc = NewService(&errorCallRepo{err: boom}, nil)
	_, err = svc.FindCall(ctx, "c1")
	require.ErrorIs(t, err, boom)
}

// TestRecordingServiceCompleteRecording 覆盖落终态的 repo 缺省、成功与错误分支。
func TestRecordingServiceCompleteRecording(t *testing.T) {
	ctx := context.Background()

	// repo 缺省：直接成功（纯广播语义，无持久化）
	svc := NewRecordingService(&scriptedRecordingProvider{}, nil, &stubRecordingBus{})
	require.NoError(t, svc.CompleteRecording(ctx, CompleteRecordingCommand{RecordingID: "rec-1"}))

	// 成功：upsert + recording.stopped 广播（带 storage_uri）
	repo := &stubRecordingRepo{}
	bus := &stubRecordingBus{}
	svc = NewRecordingService(&scriptedRecordingProvider{}, repo, bus)
	require.NoError(t, svc.CompleteRecording(ctx, CompleteRecordingCommand{
		RecordingID: "rec-2", CallID: "call-1", Provider: "twilio", StorageURI: "http://example.com/rec.mp3",
	}))
	require.Len(t, repo.saved, 1)
	assert.Equal(t, "rec-2", repo.saved[0].ID)
	assert.Equal(t, "stopped", repo.saved[0].Status)
	assert.Equal(t, "http://example.com/rec.mp3", repo.saved[0].StorageURI)
	require.Len(t, bus.events, 1)
	assert.Equal(t, RecordingStoppedEventName, bus.events[0].Name())

	// repo 错误透传，且不广播
	boom := errors.New("upsert boom")
	bus = &stubRecordingBus{}
	svc = NewRecordingService(&scriptedRecordingProvider{}, &stubRecordingRepo{saveErr: boom}, bus)
	require.ErrorIs(t, svc.CompleteRecording(ctx, CompleteRecordingCommand{RecordingID: "rec-3"}), boom)
	assert.Empty(t, bus.events)
}
