package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestBusinessMetricsRecordTicketResolved(t *testing.T) {
	reg := NewRegistry()
	m := NewBusinessMetrics(reg)

	m.RecordTicketResolved("tenant-1", "auto-resolved")
	m.RecordTicketResolved("tenant-1", "agent")
	m.RecordTicketResolved("tenant-1", "agent")

	if got := testutil.ToFloat64(m.ticketsResolved.WithLabelValues("tenant-1", "agent")); got != 2 {
		t.Fatalf("agent resolution counter = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.ticketsResolved.WithLabelValues("tenant-1", "auto-resolved")); got != 1 {
		t.Fatalf("auto-resolved counter = %v, want 1", got)
	}
}

func TestRegistryRegisterProcessCollector(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterProcessCollector()
	reg.RegisterGoCollector()

	mfs, err := reg.Gatherer().Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	var names []string
	for _, mf := range mfs {
		names = append(names, mf.GetName())
	}
	joined := strings.Join(names, ",")
	// 进程指标与 Go 运行时指标都应可见
	for _, want := range []string{"process_cpu_seconds_total", "go_goroutines"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing metric %q in gathered: %s", want, joined)
		}
	}
}
