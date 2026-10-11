package async

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestInMemoryDeadLetterRecorder_Record(t *testing.T) {
	r := NewInMemoryDeadLetterRecorder(5)

	for i := 0; i < 7; i++ {
		err := r.Record(context.Background(), DeadLetterEntry{
			EventID:    fmt.Sprintf("evt-%d", i),
			EventType:  "test.event",
			Error:      "test error",
			OccurredAt: time.Now(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	entries, err := r.List(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries (capacity limit), got %d", len(entries))
	}
	// Oldest entries should be evicted
	if entries[0].EventID != "evt-2" {
		t.Fatalf("expected evt-2 (oldest surviving), got %s", entries[0].EventID)
	}
}

func TestInMemoryDeadLetterRecorder_FilterByType(t *testing.T) {
	r := NewInMemoryDeadLetterRecorder(100)

	_ = r.Record(context.Background(), DeadLetterEntry{EventID: "1", EventType: "type_a", Error: "err"})
	_ = r.Record(context.Background(), DeadLetterEntry{EventID: "2", EventType: "type_b", Error: "err"})
	_ = r.Record(context.Background(), DeadLetterEntry{EventID: "3", EventType: "type_a", Error: "err"})

	entries, _ := r.List(context.Background(), "type_a", 10)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for type_a, got %d", len(entries))
	}
}

func TestInMemoryDeadLetterRecorder_Limit(t *testing.T) {
	r := NewInMemoryDeadLetterRecorder(100)
	_ = r.Record(context.Background(), DeadLetterEntry{EventID: "1", EventType: "test", Error: "err"})
	_ = r.Record(context.Background(), DeadLetterEntry{EventID: "2", EventType: "test", Error: "err"})

	entries, _ := r.List(context.Background(), "", 1)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (limit), got %d", len(entries))
	}
}
