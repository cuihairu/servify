package asr

import (
	"errors"
	"testing"
)

// TestSentinelErrorsDistinct 契约哨兵彼此独立且可 errors.Is 寻址：调用方
// 与 provider 实现都靠它们分支（如装配层按 ErrNotConfigured 跳过接线）。
func TestSentinelErrorsDistinct(t *testing.T) {
	if errors.Is(ErrNotConfigured, ErrSessionClosed) {
		t.Fatal("ErrNotConfigured and ErrSessionClosed must be distinct")
	}
	for _, sentinel := range []error{ErrNotConfigured, ErrSessionClosed} {
		if !errors.Is(sentinel, sentinel) {
			t.Fatalf("%v must match itself", sentinel)
		}
		if sentinel.Error() == "" {
			t.Fatal("sentinel messages must be non-empty")
		}
	}
}

// TestEventKindValues 事件种类词表冻结：VAD/partial/final 是分句策略
// （设计文档 §2.2）的契约面，改名即协议破坏，须显式设计。
func TestEventKindValues(t *testing.T) {
	want := map[EventKind]string{
		EventSpeechStart: "speech_start",
		EventPartial:     "partial",
		EventFinal:       "final",
		EventSpeechEnd:   "speech_end",
	}
	for kind, value := range want {
		if string(kind) != value {
			t.Fatalf("EventKind %q drifted from frozen value %q", kind, value)
		}
	}
}
