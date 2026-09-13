package mock

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/platform/llm"
)

func TestProviderChatRecordsAndForwardsResponse(t *testing.T) {
	want := llm.ChatResponse{Content: "hello", Model: "gpt-test"}
	p := &Provider{ChatResponse: want}

	req := llm.ChatRequest{Model: "gpt-test", Messages: []llm.ChatMessage{{Role: "user", Content: "hi"}}}
	got, err := p.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if got.Content != "hello" {
		t.Fatalf("Chat() = %+v, want %+v", got, want)
	}

	recorded := p.RecordedRequests()
	if len(recorded) != 1 || recorded[0].Messages[0].Content != "hi" {
		t.Fatalf("RecordedRequests() = %+v", recorded)
	}

	// 错误透传
	wantErr := errors.New("chat down")
	p2 := &Provider{ChatError: wantErr}
	if _, err := p2.Chat(context.Background(), req); !errors.Is(err, wantErr) {
		t.Fatalf("Chat() error = %v, want %v", err, wantErr)
	}
}

func TestProviderChatStream(t *testing.T) {
	chunks := []llm.ChatChunk{{ContentDelta: "a"}, {ContentDelta: "b"}}
	p := &Provider{StreamChunks: chunks}

	ch, err := p.ChatStream(context.Background(), llm.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	var got []llm.ChatChunk
	for chunk := range ch {
		got = append(got, chunk)
	}
	if len(got) != 2 || got[0].ContentDelta != "a" || got[1].ContentDelta != "b" {
		t.Fatalf("streamed chunks = %+v, want %+v", got, chunks)
	}
	if len(p.RecordedRequests()) != 1 {
		t.Fatal("ChatStream must record the request")
	}

	wantErr := errors.New("stream down")
	p2 := &Provider{StreamError: wantErr}
	if _, err := p2.ChatStream(context.Background(), llm.ChatRequest{}); !errors.Is(err, wantErr) {
		t.Fatalf("ChatStream() error = %v, want %v", err, wantErr)
	}
}

func TestProviderEmbedAndHealthCheck(t *testing.T) {
	want := [][]float32{{0.1, 0.2}}
	p := &Provider{EmbeddingResponse: want}
	got, err := p.Embed(context.Background(), []string{"text"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(got) != 1 || got[0][0] != 0.1 {
		t.Fatalf("Embed() = %v, want %v", got, want)
	}
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}

	wantErr := errors.New("embed down")
	p2 := &Provider{EmbeddingError: wantErr, HealthError: wantErr}
	if _, err := p2.Embed(context.Background(), []string{"t"}); !errors.Is(err, wantErr) {
		t.Fatalf("Embed() error = %v, want %v", err, wantErr)
	}
	if err := p2.HealthCheck(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("HealthCheck() error = %v, want %v", err, wantErr)
	}
}

func TestProviderRecordedRequestsReturnsSnapshot(t *testing.T) {
	p := &Provider{}
	if got := p.RecordedRequests(); len(got) != 0 {
		t.Fatalf("expected no recorded requests, got %+v", got)
	}

	for i := 0; i < 3; i++ {
		_, _ = p.Chat(context.Background(), llm.ChatRequest{Model: "m"})
	}
	snap := p.RecordedRequests()
	if len(snap) != 3 {
		t.Fatalf("expected 3 recorded requests, got %d", len(snap))
	}
	// 快照必须是拷贝：修改返回值不影响内部状态
	snap[0].Model = "mutated"
	if p.RecordedRequests()[0].Model != "m" {
		t.Fatal("RecordedRequests must return a copy")
	}
}
