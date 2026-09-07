package application

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type errorCallRepo struct{ err error }

func (r *errorCallRepo) StartCall(ctx context.Context, cmd StartCallCommand) (*CallDTO, error) {
	return nil, r.err
}
func (r *errorCallRepo) AnswerCall(ctx context.Context, cmd AnswerCallCommand) (*CallDTO, error) {
	return nil, r.err
}
func (r *errorCallRepo) HoldCall(ctx context.Context, cmd HoldCallCommand) (*CallDTO, error) {
	return nil, r.err
}
func (r *errorCallRepo) ResumeCall(ctx context.Context, cmd ResumeCallCommand) (*CallDTO, error) {
	return nil, r.err
}
func (r *errorCallRepo) EndCall(ctx context.Context, cmd EndCallCommand) (*CallDTO, error) {
	return nil, r.err
}
func (r *errorCallRepo) TransferCall(ctx context.Context, cmd TransferCallCommand) (*CallDTO, error) {
	return nil, r.err
}

type okCallRepo struct{}

func (r *okCallRepo) StartCall(ctx context.Context, cmd StartCallCommand) (*CallDTO, error) {
	return &CallDTO{ID: cmd.CallID, Status: "started"}, nil
}
func (r *okCallRepo) AnswerCall(ctx context.Context, cmd AnswerCallCommand) (*CallDTO, error) {
	return &CallDTO{ID: cmd.CallID, Status: "answered"}, nil
}
func (r *okCallRepo) HoldCall(ctx context.Context, cmd HoldCallCommand) (*CallDTO, error) {
	return &CallDTO{ID: cmd.CallID, Status: "held"}, nil
}
func (r *okCallRepo) ResumeCall(ctx context.Context, cmd ResumeCallCommand) (*CallDTO, error) {
	return &CallDTO{ID: cmd.CallID, Status: "answered"}, nil
}
func (r *okCallRepo) EndCall(ctx context.Context, cmd EndCallCommand) (*CallDTO, error) {
	return &CallDTO{ID: cmd.CallID, Status: "ended"}, nil
}
func (r *okCallRepo) TransferCall(ctx context.Context, cmd TransferCallCommand) (*CallDTO, error) {
	return &CallDTO{ID: cmd.CallID, Status: "transferred"}, nil
}

func TestServiceAnswerCallDelegates(t *testing.T) {
	svc := NewService(&okCallRepo{}, nil)
	call, err := svc.AnswerCall(context.Background(), AnswerCallCommand{CallID: "c9"})
	require.NoError(t, err)
	require.NotNil(t, call)
	assert.Equal(t, "answered", call.Status)
}

func TestServiceRepoErrorsPropagate(t *testing.T) {
	boom := errors.New("repo boom")
	svc := NewService(&errorCallRepo{err: boom}, &stubBus{})
	ctx := context.Background()

	_, err := svc.StartCall(ctx, StartCallCommand{CallID: "c1"})
	require.ErrorIs(t, err, boom)
	_, err = svc.AnswerCall(ctx, AnswerCallCommand{CallID: "c1"})
	require.ErrorIs(t, err, boom)
	_, err = svc.HoldCall(ctx, HoldCallCommand{CallID: "c1"})
	require.ErrorIs(t, err, boom)
	_, err = svc.ResumeCall(ctx, ResumeCallCommand{CallID: "c1"})
	require.ErrorIs(t, err, boom)
	_, err = svc.EndCall(ctx, EndCallCommand{CallID: "c1"})
	require.ErrorIs(t, err, boom)
	_, err = svc.TransferCall(ctx, TransferCallCommand{CallID: "c1", ToAgentID: 3})
	require.ErrorIs(t, err, boom)
}

func TestServiceWithoutPublisher(t *testing.T) {
	svc := NewService(&okCallRepo{}, nil)
	ctx := context.Background()

	call, err := svc.StartCall(ctx, StartCallCommand{CallID: "c1"})
	require.NoError(t, err)
	assert.Equal(t, "started", call.Status)

	_, err = svc.HoldCall(ctx, HoldCallCommand{CallID: "c1"})
	require.NoError(t, err)
	_, err = svc.ResumeCall(ctx, ResumeCallCommand{CallID: "c1"})
	require.NoError(t, err)
	_, err = svc.EndCall(ctx, EndCallCommand{CallID: "c1"})
	require.NoError(t, err)
	_, err = svc.TransferCall(ctx, TransferCallCommand{CallID: "c1", ToAgentID: 2})
	require.NoError(t, err)
}

type scriptedRecordingProvider struct {
	mu          sync.Mutex
	startCalls  int
	stopCalls   int
	failStarts  int
	failStops   int
	hardErr     error
	hardStopErr error
}

func (p *scriptedRecordingProvider) StartRecording(ctx context.Context, cmd StartRecordingCommand) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.startCalls++
	if p.hardErr != nil {
		return "", p.hardErr
	}
	if p.startCalls <= p.failStarts {
		return "", &ProviderError{Code: ProviderErrorUnavailable, Message: "temporarily down", Retryable: true}
	}
	return fmt.Sprintf("rec-%d", p.startCalls), nil
}

func (p *scriptedRecordingProvider) StopRecording(ctx context.Context, cmd StopRecordingCommand) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopCalls++
	if p.hardStopErr != nil {
		return p.hardStopErr
	}
	if p.stopCalls <= p.failStops {
		return &ProviderError{Code: ProviderErrorTimeout, Message: "stop timeout", Retryable: true}
	}
	return nil
}

func (p *scriptedRecordingProvider) startCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.startCalls
}

func (p *scriptedRecordingProvider) stopCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopCalls
}

type stubRecordingRepo struct {
	mu      sync.Mutex
	saved   []RecordingDTO
	saveErr error
	stopped []string
	stopErr error
	find    *RecordingDTO
	findErr error
}

func (r *stubRecordingRepo) Save(ctx context.Context, recording RecordingDTO) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	r.saved = append(r.saved, recording)
	return nil
}

func (r *stubRecordingRepo) MarkStopped(ctx context.Context, recordingID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopErr != nil {
		return r.stopErr
	}
	r.stopped = append(r.stopped, recordingID)
	return nil
}

func (r *stubRecordingRepo) FindByID(ctx context.Context, recordingID string) (*RecordingDTO, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	return r.find, nil
}

type capturingCallbackSink struct {
	mu          sync.Mutex
	recordings  []RecordingDTO
	transcripts []TranscriptDTO
}

func (s *capturingCallbackSink) NotifyRecording(ctx context.Context, recording RecordingDTO) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordings = append(s.recordings, recording)
	return nil
}

func (s *capturingCallbackSink) NotifyTranscript(ctx context.Context, transcript TranscriptDTO) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transcripts = append(s.transcripts, transcript)
	return nil
}

func TestRecordingServiceStartRecordingRetriesRetryableError(t *testing.T) {
	provider := &scriptedRecordingProvider{failStarts: 1}
	repo := &stubRecordingRepo{}
	svc := NewRecordingService(provider, repo, &stubRecordingBus{})

	recording, err := svc.StartRecording(context.Background(), StartRecordingCommand{CallID: "c1", Provider: "mock"})
	require.NoError(t, err)
	assert.Equal(t, "rec-2", recording.ID)
	assert.Equal(t, 2, provider.startCount())
	require.Len(t, repo.saved, 1)
	assert.Equal(t, "c1", repo.saved[0].CallID)
}

func TestRecordingServiceStartRecordingFailsOnNonRetryableError(t *testing.T) {
	provider := &scriptedRecordingProvider{hardErr: errors.New("denied")}
	svc := NewRecordingService(provider, &stubRecordingRepo{}, &stubRecordingBus{})

	_, err := svc.StartRecording(context.Background(), StartRecordingCommand{CallID: "c1"})
	require.ErrorIs(t, err, provider.hardErr)
	assert.Equal(t, 1, provider.startCount())
}

func TestRecordingServiceStartRecordingRepoSaveError(t *testing.T) {
	boom := errors.New("save failed")
	svc := NewRecordingService(&scriptedRecordingProvider{}, &stubRecordingRepo{saveErr: boom}, &stubRecordingBus{})

	_, err := svc.StartRecording(context.Background(), StartRecordingCommand{CallID: "c1"})
	require.ErrorIs(t, err, boom)
}

func TestRecordingServiceStopRecordingRetriesAndSucceeds(t *testing.T) {
	provider := &scriptedRecordingProvider{failStops: 1}
	repo := &stubRecordingRepo{}
	svc := NewRecordingService(provider, repo, &stubRecordingBus{})

	require.NoError(t, svc.StopRecording(context.Background(), StopRecordingCommand{RecordingID: "rec-1"}))
	assert.Equal(t, 2, provider.stopCount())
	assert.Equal(t, []string{"rec-1"}, repo.stopped)
}

func TestRecordingServiceStopRecordingErrors(t *testing.T) {
	hard := errors.New("stop denied")
	provider := &scriptedRecordingProvider{hardStopErr: hard}
	svc := NewRecordingService(provider, &stubRecordingRepo{}, &stubRecordingBus{})
	require.ErrorIs(t, svc.StopRecording(context.Background(), StopRecordingCommand{RecordingID: "rec-1"}), hard)

	boom := errors.New("mark failed")
	svc = NewRecordingService(&scriptedRecordingProvider{}, &stubRecordingRepo{stopErr: boom}, &stubRecordingBus{})
	require.ErrorIs(t, svc.StopRecording(context.Background(), StopRecordingCommand{RecordingID: "rec-1"}), boom)
}

func TestRecordingServiceGetRecording(t *testing.T) {
	found := &RecordingDTO{ID: "rec-9", CallID: "c1", Status: "recording"}
	svc := NewRecordingService(&scriptedRecordingProvider{}, &stubRecordingRepo{find: found}, nil)
	got, err := svc.GetRecording(context.Background(), "rec-9")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "rec-9", got.ID)

	boom := errors.New("find failed")
	svc = NewRecordingService(&scriptedRecordingProvider{}, &stubRecordingRepo{findErr: boom}, nil)
	_, err = svc.GetRecording(context.Background(), "rec-9")
	require.ErrorIs(t, err, boom)

	svc = NewRecordingService(&scriptedRecordingProvider{}, nil, nil)
	got, err = svc.GetRecording(context.Background(), "rec-9")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestRecordingServiceSetRetryPolicy(t *testing.T) {
	provider := &scriptedRecordingProvider{failStarts: 1}
	svc := NewRecordingService(provider, nil, nil)
	svc.SetRetryPolicy(RetryPolicy{})
	_, err := svc.StartRecording(context.Background(), StartRecordingCommand{CallID: "c1"})
	require.Error(t, err)
	assert.Equal(t, "temporarily down", err.Error())
	assert.Equal(t, 1, provider.startCount(), "clamped policy should only try once")

	provider = &scriptedRecordingProvider{failStarts: 2}
	svc = NewRecordingService(provider, nil, nil)
	svc.SetRetryPolicy(RetryPolicy{MaxAttempts: 3})
	recording, err := svc.StartRecording(context.Background(), StartRecordingCommand{CallID: "c1"})
	require.NoError(t, err)
	assert.Equal(t, "rec-3", recording.ID)
	assert.Equal(t, 3, provider.startCount())
}

func TestRecordingServiceSetCallbackSink(t *testing.T) {
	sink := &capturingCallbackSink{}
	svc := NewRecordingService(&scriptedRecordingProvider{}, &stubRecordingRepo{}, nil)
	svc.SetCallbackSink(sink)

	recording, err := svc.StartRecording(context.Background(), StartRecordingCommand{CallID: "c1", Provider: "mock"})
	require.NoError(t, err)
	require.Len(t, sink.recordings, 1)
	assert.Equal(t, recording.ID, sink.recordings[0].ID)
}

type scriptedTranscriptProvider struct {
	mu       sync.Mutex
	calls    int
	failures int
	hardErr  error
}

func (p *scriptedTranscriptProvider) AppendTranscript(ctx context.Context, cmd AppendTranscriptCommand) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.hardErr != nil {
		return p.hardErr
	}
	if p.calls <= p.failures {
		return &ProviderError{Code: ProviderErrorTimeout, Message: "append timeout", Retryable: true}
	}
	return nil
}

func (p *scriptedTranscriptProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type stubTranscriptRepo struct {
	mu        sync.Mutex
	appended  []TranscriptDTO
	appendErr error
	byCall    []TranscriptDTO
	byCallErr error
	all       []TranscriptDTO
	total     int64
	allErr    error
}

func (r *stubTranscriptRepo) Append(ctx context.Context, transcript TranscriptDTO) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.appendErr != nil {
		return r.appendErr
	}
	r.appended = append(r.appended, transcript)
	return nil
}

func (r *stubTranscriptRepo) ListByCallID(ctx context.Context, callID string) ([]TranscriptDTO, error) {
	if r.byCallErr != nil {
		return nil, r.byCallErr
	}
	return r.byCall, nil
}

func (r *stubTranscriptRepo) ListAll(ctx context.Context, page, pageSize int) ([]TranscriptDTO, int64, error) {
	if r.allErr != nil {
		return nil, 0, r.allErr
	}
	return r.all, r.total, nil
}

func TestTranscriptServiceAppendRetriesRetryableError(t *testing.T) {
	provider := &scriptedTranscriptProvider{failures: 1}
	repo := &stubTranscriptRepo{}
	svc := NewTranscriptService(provider, repo, &stubTranscriptBus{})

	transcript, err := svc.Append(context.Background(), AppendTranscriptCommand{CallID: "c1", Content: "hi", Language: "en", Finalized: true})
	require.NoError(t, err)
	assert.Equal(t, "hi", transcript.Content)
	assert.True(t, transcript.Finalized)
	assert.False(t, transcript.AppendedAt.IsZero())
	assert.Equal(t, 2, provider.callCount())
	require.Len(t, repo.appended, 1)
}

func TestTranscriptServiceAppendErrors(t *testing.T) {
	hard := errors.New("append denied")
	provider := &scriptedTranscriptProvider{hardErr: hard}
	svc := NewTranscriptService(provider, &stubTranscriptRepo{}, &stubTranscriptBus{})
	_, err := svc.Append(context.Background(), AppendTranscriptCommand{CallID: "c1"})
	require.ErrorIs(t, err, hard)
	assert.Equal(t, 1, provider.callCount())

	boom := errors.New("repo append failed")
	svc = NewTranscriptService(&scriptedTranscriptProvider{}, &stubTranscriptRepo{appendErr: boom}, &stubTranscriptBus{})
	_, err = svc.Append(context.Background(), AppendTranscriptCommand{CallID: "c1"})
	require.ErrorIs(t, err, boom)
}

func TestTranscriptServiceSetRetryPolicy(t *testing.T) {
	provider := &scriptedTranscriptProvider{failures: 1}
	svc := NewTranscriptService(provider, nil, nil)
	svc.SetRetryPolicy(RetryPolicy{})
	_, err := svc.Append(context.Background(), AppendTranscriptCommand{CallID: "c1"})
	require.Error(t, err)
	assert.Equal(t, "append timeout", err.Error())
	assert.Equal(t, 1, provider.callCount(), "clamped policy should only try once")

	provider = &scriptedTranscriptProvider{failures: 2}
	svc = NewTranscriptService(provider, nil, nil)
	svc.SetRetryPolicy(RetryPolicy{MaxAttempts: 3})
	_, err = svc.Append(context.Background(), AppendTranscriptCommand{CallID: "c1"})
	require.NoError(t, err)
	assert.Equal(t, 3, provider.callCount())
}

func TestTranscriptServiceSetCallbackSink(t *testing.T) {
	sink := &capturingCallbackSink{}
	svc := NewTranscriptService(&scriptedTranscriptProvider{}, &stubTranscriptRepo{}, nil)
	svc.SetCallbackSink(sink)

	_, err := svc.Append(context.Background(), AppendTranscriptCommand{CallID: "c1", Content: "hello"})
	require.NoError(t, err)
	require.Len(t, sink.transcripts, 1)
	assert.Equal(t, "hello", sink.transcripts[0].Content)
}

func TestTranscriptServiceListByCallID(t *testing.T) {
	items := []TranscriptDTO{{CallID: "c1", Content: "a"}, {CallID: "c1", Content: "b"}}
	svc := NewTranscriptService(&scriptedTranscriptProvider{}, &stubTranscriptRepo{byCall: items}, nil)
	got, err := svc.ListByCallID(context.Background(), "c1")
	require.NoError(t, err)
	assert.Len(t, got, 2)

	boom := errors.New("list failed")
	svc = NewTranscriptService(&scriptedTranscriptProvider{}, &stubTranscriptRepo{byCallErr: boom}, nil)
	_, err = svc.ListByCallID(context.Background(), "c1")
	require.ErrorIs(t, err, boom)

	svc = NewTranscriptService(&scriptedTranscriptProvider{}, nil, nil)
	got, err = svc.ListByCallID(context.Background(), "c1")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTranscriptServiceListAll(t *testing.T) {
	items := []TranscriptDTO{{CallID: "c1", Content: "a"}, {CallID: "c2", Content: "b"}}
	svc := NewTranscriptService(&scriptedTranscriptProvider{}, &stubTranscriptRepo{all: items, total: 5}, nil)
	got, total, err := svc.ListAll(context.Background(), 1, 2)
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.EqualValues(t, 5, total)

	boom := errors.New("list all failed")
	svc = NewTranscriptService(&scriptedTranscriptProvider{}, &stubTranscriptRepo{allErr: boom}, nil)
	_, _, err = svc.ListAll(context.Background(), 1, 2)
	require.ErrorIs(t, err, boom)

	svc = NewTranscriptService(&scriptedTranscriptProvider{}, nil, nil)
	got, total, err = svc.ListAll(context.Background(), 1, 2)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NotNil(t, got)
	assert.Zero(t, total)
}
