package application

import (
	"context"
	"time"
)

type TranscriptService struct {
	provider     TranscriptProvider
	repo         TranscriptRepository
	publisher    Publisher
	retryPolicy  RetryPolicy
	callbackSink AsyncCallbackSink
}

func NewTranscriptService(provider TranscriptProvider, repo TranscriptRepository, publisher Publisher) *TranscriptService {
	return &TranscriptService{
		provider:    provider,
		repo:        repo,
		publisher:   publisher,
		retryPolicy: RetryPolicy{MaxAttempts: 2},
	}
}

func (s *TranscriptService) Append(ctx context.Context, cmd AppendTranscriptCommand) (*TranscriptDTO, error) {
	if err := applyRetry(s.retryPolicy.MaxAttempts, func() error {
		return s.provider.AppendTranscript(ctx, cmd)
	}); err != nil {
		return nil, err
	}
	transcript := &TranscriptDTO{
		CallID:     cmd.CallID,
		Content:    cmd.Content,
		Language:   cmd.Language,
		Finalized:  cmd.Finalized,
		AppendedAt: time.Now(),
	}
	if s.repo != nil {
		if err := s.repo.Append(ctx, *transcript); err != nil {
			return nil, err
		}
	}
	s.publish(ctx, TranscriptAppendedEventName, cmd.CallID, transcript)
	if s.callbackSink != nil {
		_ = s.callbackSink.NotifyTranscript(ctx, *transcript)
	}
	return transcript, nil
}

func (s *TranscriptService) ListByCallID(ctx context.Context, callID string) ([]TranscriptDTO, error) {
	if s.repo == nil {
		return nil, nil
	}
	return s.repo.ListByCallID(ctx, callID)
}

func (s *TranscriptService) ListAll(ctx context.Context, page, pageSize int) ([]TranscriptDTO, int64, error) {
	if s.repo == nil {
		return []TranscriptDTO{}, 0, nil
	}
	return s.repo.ListAll(ctx, page, pageSize)
}

func (s *TranscriptService) publish(ctx context.Context, name, aggregateID string, payload interface{}) {
	if s.publisher == nil {
		return
	}
	_ = s.publisher.Publish(ctx, NewVoiceEvent(ctx, name, aggregateID, payload))
}

func (s *TranscriptService) SetRetryPolicy(policy RetryPolicy) {
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 1
	}
	s.retryPolicy = policy
}

func (s *TranscriptService) SetCallbackSink(sink AsyncCallbackSink) {
	s.callbackSink = sink
}
