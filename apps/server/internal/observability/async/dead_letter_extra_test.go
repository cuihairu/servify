package async

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestNewInMemoryDeadLetterRecorder_DefaultLimit(t *testing.T) {
	for _, arg := range []int{0, -3} {
		r := NewInMemoryDeadLetterRecorder(arg)
		if r.limit != 1000 {
			t.Fatalf("limit %d: expected default limit 1000, got %d", arg, r.limit)
		}
		if len(r.entries) != 0 {
			t.Fatalf("limit %d: expected empty entries, got %d", arg, len(r.entries))
		}
	}
}

func TestInMemoryDeadLetterRecorder_ListEmpty(t *testing.T) {
	r := NewInMemoryDeadLetterRecorder(10)

	entries, err := r.List(context.Background(), "", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}

	filtered, err := r.List(context.Background(), "type_a", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("expected 0 filtered entries, got %d", len(filtered))
	}
}

func TestInMemoryDeadLetterRecorder_ListFilteredStopsAtLimit(t *testing.T) {
	r := NewInMemoryDeadLetterRecorder(100)
	r.Record(context.Background(), DeadLetterEntry{EventID: "1", EventType: "type_a", Error: "err"})
	r.Record(context.Background(), DeadLetterEntry{EventID: "2", EventType: "type_b", Error: "err"})
	r.Record(context.Background(), DeadLetterEntry{EventID: "3", EventType: "type_a", Error: "err"})

	entries, err := r.List(context.Background(), "type_a", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (filtered limit), got %d", len(entries))
	}
	if entries[0].EventID != "1" {
		t.Fatalf("expected first matching entry evt 1, got %s", entries[0].EventID)
	}
}

func TestInMemoryDeadLetterRecorder_ListNonPositiveLimit(t *testing.T) {
	r := NewInMemoryDeadLetterRecorder(100)
	r.Record(context.Background(), DeadLetterEntry{EventID: "1", EventType: "t", Error: "err"})
	r.Record(context.Background(), DeadLetterEntry{EventID: "2", EventType: "t", Error: "err"})

	for _, limit := range []int{0, -7} {
		entries, err := r.List(context.Background(), "", limit)
		if err != nil {
			t.Fatalf("limit %d: unexpected error: %v", limit, err)
		}
		if len(entries) != 2 {
			t.Fatalf("limit %d: expected 2 entries, got %d", limit, len(entries))
		}
	}
}

func TestInMemoryDeadLetterRecorder_ConcurrentAccess(t *testing.T) {
	r := NewInMemoryDeadLetterRecorder(10000)

	const goroutines = 25
	const perGoroutine = 40

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				if err := r.Record(context.Background(), DeadLetterEntry{
					EventID:    fmt.Sprintf("e-%d-%d", i, j),
					EventType:  "t",
					Error:      "err",
					OccurredAt: time.Now(),
				}); err != nil {
					t.Errorf("unexpected record error: %v", err)
				}
			}
		}(i)
	}

	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
				if _, err := r.List(context.Background(), "t", 10); err != nil {
					t.Errorf("unexpected list error: %v", err)
					return
				}
			}
		}
	}()

	wg.Wait()
	close(stop)
	<-readerDone

	entries, err := r.List(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != goroutines*perGoroutine {
		t.Fatalf("expected %d entries, got %d", goroutines*perGoroutine, len(entries))
	}
}
