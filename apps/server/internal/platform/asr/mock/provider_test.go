package mock

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/platform/asr"
)

// 编译期契约断言：替身必须满足 Recognizer/RecognizeSession。
var (
	_ asr.Recognizer       = (*Provider)(nil)
	_ asr.RecognizeSession = (*session)(nil)
)

func drain(t *testing.T, ch <-chan asr.Event) []asr.Event {
	t.Helper()
	var out []asr.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func TestProviderReplaysScriptInFeedOrder(t *testing.T) {
	script := [][]asr.Event{
		{{Kind: asr.EventSpeechStart, Seq: 1}},
		{{Kind: asr.EventPartial, Seq: 1, Text: "你好"}},
		{{Kind: asr.EventFinal, Seq: 1, Text: "你好。", Confidence: 0.97}, {Kind: asr.EventSpeechEnd, Seq: 1}},
	}
	p := &Provider{Script: script}
	sess, events, err := p.NewSession(context.Background(), asr.SessionOptions{
		SourceLang: "auto",
		Format:     asr.AudioFormat{SampleRate: 16000, Channels: 1, Encoding: "opus"},
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	chunks := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	for _, chunk := range chunks {
		if err := sess.FeedAudio(chunk); err != nil {
			t.Fatalf("FeedAudio(%q): %v", chunk, err)
		}
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := drain(t, events)
	if len(got) != 4 {
		t.Fatalf("event count = %d, want 4 (%+v)", len(got), got)
	}
	wantKinds := []asr.EventKind{asr.EventSpeechStart, asr.EventPartial, asr.EventFinal, asr.EventSpeechEnd}
	for i, want := range wantKinds {
		if got[i].Kind != want {
			t.Fatalf("event[%d].Kind = %q, want %q", i, got[i].Kind, want)
		}
	}
	if got[2].Text != "你好。" || got[2].Confidence != 0.97 {
		t.Fatalf("final event payload degraded: %+v", got[2])
	}

	if reqs := p.RecordedRequests(); len(reqs) != 1 || reqs[0].SourceLang != "auto" || reqs[0].Format.SampleRate != 16000 {
		t.Fatalf("session options not recorded verbatim: %+v", reqs)
	}
	recorded := p.RecordedChunks()
	if len(recorded) != 3 {
		t.Fatalf("chunk count = %d, want 3", len(recorded))
	}
	for i, chunk := range chunks {
		if string(recorded[i]) != string(chunk) {
			t.Fatalf("chunk[%d] = %q, want %q", i, recorded[i], chunk)
		}
	}
}

func TestProviderEmptyScriptYieldsClosedChannelWithoutEvents(t *testing.T) {
	p := &Provider{}
	sess, events, err := p.NewSession(context.Background(), asr.SessionOptions{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := sess.FeedAudio([]byte("silence")); err != nil {
		t.Fatalf("FeedAudio: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := drain(t, events); len(got) != 0 {
		t.Fatalf("expected no events, got %+v", got)
	}
}

func TestProviderExhaustedScriptStopsEmitting(t *testing.T) {
	p := &Provider{Script: [][]asr.Event{{{Kind: asr.EventPartial, Seq: 1, Text: "x"}}}}
	sess, events, err := p.NewSession(context.Background(), asr.SessionOptions{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := sess.FeedAudio([]byte{byte(i)}); err != nil {
			t.Fatalf("FeedAudio #%d: %v", i, err)
		}
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := drain(t, events); len(got) != 1 {
		t.Fatalf("event count = %d, want 1 (script exhausted after first feed)", len(got))
	}
}

func TestProviderFeedAfterCloseRejected(t *testing.T) {
	p := &Provider{}
	sess, events, err := p.NewSession(context.Background(), asr.SessionOptions{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := sess.FeedAudio([]byte("late")); !errors.Is(err, asr.ErrSessionClosed) {
		t.Fatalf("FeedAudio after Close err = %v, want ErrSessionClosed", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("second Close must be idempotent, got %v", err)
	}
	go func() {
		for range events {
		}
	}()
}

func TestProviderSessionAndHealthErrors(t *testing.T) {
	sessionErr := errors.New("upstream handshake failed")
	p := &Provider{SessionError: sessionErr}
	sess, events, err := p.NewSession(context.Background(), asr.SessionOptions{})
	if !errors.Is(err, sessionErr) {
		t.Fatalf("NewSession err = %v, want sessionErr", err)
	}
	if sess != nil || events != nil {
		t.Fatalf("failed NewSession must return nil session/channel, got %v/%v", sess, events)
	}

	healthErr := errors.New("health probe failed")
	p2 := &Provider{HealthError: healthErr}
	if err := p2.HealthCheck(context.Background()); !errors.Is(err, healthErr) {
		t.Fatalf("HealthCheck err = %v, want healthErr", err)
	}
}

func TestProviderSessionsAreIndependent(t *testing.T) {
	p := &Provider{Script: [][]asr.Event{{{Kind: asr.EventFinal, Seq: 1, Text: "one"}}}}
	s1, ev1, err := p.NewSession(context.Background(), asr.SessionOptions{SourceLang: "zh"})
	if err != nil {
		t.Fatalf("NewSession #1: %v", err)
	}
	s2, ev2, err := p.NewSession(context.Background(), asr.SessionOptions{SourceLang: "en"})
	if err != nil {
		t.Fatalf("NewSession #2: %v", err)
	}
	// 两个会话各自从头消费脚本：第二个会话也拿到同一批预设事件。
	for _, s := range []asr.RecognizeSession{s1, s2} {
		if err := s.FeedAudio([]byte("x")); err != nil {
			t.Fatalf("FeedAudio: %v", err)
		}
	}
	_ = s1.Close()
	_ = s2.Close()
	for name, ch := range map[string]<-chan asr.Event{"s1": ev1, "s2": ev2} {
		got := drain(t, ch)
		if len(got) != 1 || got[0].Text != "one" {
			t.Fatalf("%s events = %+v, want single scripted final", name, got)
		}
	}
	if reqs := p.RecordedRequests(); len(reqs) != 2 || reqs[0].SourceLang != "zh" || reqs[1].SourceLang != "en" {
		t.Fatalf("recorded sessions = %+v", reqs)
	}
}
