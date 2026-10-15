package quotameter

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestMeter(t *testing.T, window WindowKind) (*Meter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	m, err := New(client, "translation", window)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m, mr
}

func TestParseWindow(t *testing.T) {
	for _, ok := range []string{"hour", "day", "month"} {
		if _, err := ParseWindow(ok); err != nil {
			t.Fatalf("ParseWindow(%q) unexpected error: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "week", "minute", "DAY"} {
		if _, err := ParseWindow(bad); err == nil {
			t.Fatalf("ParseWindow(%q) expected error", bad)
		}
	}
}

func TestNewRejections(t *testing.T) {
	if _, err := New(nil, "translation", WindowDay); err == nil {
		t.Fatal("nil client must be rejected")
	}
	if _, err := New(&redis.Client{}, "translation", WindowKind("week")); err == nil {
		t.Fatal("invalid window must be rejected")
	}
}

func TestUsageMissingKeyIsZero(t *testing.T) {
	m, _ := newTestMeter(t, WindowDay)
	got, err := m.Usage(context.Background(), "tenant-1")
	if err != nil || got != 0 {
		t.Fatalf("Usage on missing key = (%d, %v), want (0, nil)", got, err)
	}
}

func TestAddAccumulatesWithinWindow(t *testing.T) {
	m, _ := newTestMeter(t, WindowDay)
	ctx := context.Background()
	for i := int64(1); i <= 3; i++ {
		total, err := m.Add(ctx, "tenant-1", 5)
		if err != nil {
			t.Fatalf("Add #%d: %v", i, err)
		}
		if total != i*5 {
			t.Fatalf("Add #%d total = %d, want %d", i, total, i*5)
		}
	}
	usage, err := m.Usage(ctx, "tenant-1")
	if err != nil || usage != 15 {
		t.Fatalf("Usage = (%d, %v), want (15, nil)", usage, err)
	}
}

func TestAddScopesIsolated(t *testing.T) {
	m, _ := newTestMeter(t, WindowDay)
	ctx := context.Background()
	if _, err := m.Add(ctx, "tenant-1", 7); err != nil {
		t.Fatalf("Add tenant-1: %v", err)
	}
	got, err := m.Usage(ctx, "tenant-2")
	if err != nil || got != 0 {
		t.Fatalf("tenant-2 usage = (%d, %v), want (0, nil)", got, err)
	}
}

func TestWindowAnchorRolls(t *testing.T) {
	m, _ := newTestMeter(t, WindowHour)
	ctx := context.Background()
	base := time.Date(2026, 10, 15, 10, 30, 0, 0, time.Local)
	m.now = func() time.Time { return base }
	if _, err := m.Add(ctx, "tenant-1", 9); err != nil {
		t.Fatalf("Add in window A: %v", err)
	}
	// 跨入下一小时窗口：锚点变化，归零重计。
	m.now = func() time.Time { return base.Add(time.Hour) }
	got, err := m.Usage(ctx, "tenant-1")
	if err != nil || got != 0 {
		t.Fatalf("Usage after window roll = (%d, %v), want (0, nil)", got, err)
	}
	if _, err := m.Add(ctx, "tenant-1", 2); err != nil {
		t.Fatalf("Add in window B: %v", err)
	}
	got, err = m.Usage(ctx, "tenant-1")
	if err != nil || got != 2 {
		t.Fatalf("Usage in window B = (%d, %v), want (2, nil)", got, err)
	}
}

func TestKeyShape(t *testing.T) {
	m, _ := newTestMeter(t, WindowDay)
	m.now = func() time.Time { return time.Date(2026, 10, 15, 10, 30, 0, 0, time.Local) }
	want := "servify:quota:translation:day:20261015:tenant-1"
	if got := m.key("tenant-1", m.now()); got != want {
		t.Fatalf("key = %q, want %q", got, want)
	}
	if _, err := m.Add(context.Background(), "tenant-1", 1); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !strings.HasPrefix(m.key("tenant-1", m.now()), "servify:quota:translation:day:20261015:") {
		t.Fatalf("key namespace prefix mismatch: %q", m.key("tenant-1", m.now()))
	}
}

func TestAddSetsExpiry(t *testing.T) {
	m, mr := newTestMeter(t, WindowDay)
	m.now = func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.Local) }
	if _, err := m.Add(context.Background(), "tenant-1", 1); err != nil {
		t.Fatalf("Add: %v", err)
	}
	ttl := mr.TTL(m.key("tenant-1", m.now()))
	// 当日窗口起点写入：剩余窗口≈24h，加宽限后略超 24h。
	if ttl < 24*time.Hour || ttl > 25*time.Hour {
		t.Fatalf("TTL = %v, want within (24h, 25h)", ttl)
	}
}

func TestMonthWindowAnchorAndTTL(t *testing.T) {
	m, mr := newTestMeter(t, WindowMonth)
	m.now = func() time.Time { return time.Date(2026, 10, 15, 10, 30, 0, 0, time.Local) }
	ctx := context.Background()
	key := m.key("tenant-1", m.now())
	if key != "servify:quota:translation:month:202610:tenant-1" {
		t.Fatalf("month key = %q", key)
	}
	if _, err := m.Add(ctx, "tenant-1", 4); err != nil {
		t.Fatalf("Add month: %v", err)
	}
	ttl := mr.TTL(key)
	// 当月 15 日写入：到下月一号约 17 天，加宽限后略超。
	if ttl < 16*24*time.Hour || ttl > 18*24*time.Hour {
		t.Fatalf("month TTL = %v, want within (16d, 18d)", ttl)
	}
	if got, err := m.Usage(ctx, "tenant-1"); err != nil || got != 4 {
		t.Fatalf("month Usage = (%d, %v), want (4, nil)", got, err)
	}
}

func TestUsageMalformedCounter(t *testing.T) {
	m, mr := newTestMeter(t, WindowDay)
	m.now = func() time.Time { return time.Date(2026, 10, 15, 10, 30, 0, 0, time.Local) }
	if err := mr.Set(m.key("tenant-1", m.now()), "not-a-number"); err != nil {
		t.Fatalf("seed malformed value: %v", err)
	}
	if _, err := m.Usage(context.Background(), "tenant-1"); err == nil {
		t.Fatal("malformed counter must return error")
	}
}

func TestRedisDownReturnsError(t *testing.T) {
	m, mr := newTestMeter(t, WindowDay)
	mr.Close()
	ctx := context.Background()
	if _, err := m.Add(ctx, "tenant-1", 1); err == nil {
		t.Fatal("Add with redis down must return error")
	}
	if _, err := m.Usage(ctx, "tenant-1"); err == nil {
		t.Fatal("Usage with redis down must return error")
	}
}
