package handlers

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsPrometheusCollector_EmptyAggregator(t *testing.T) {
	c := NewMetricsPrometheusCollector(NewMetricsAggregator())
	if got := testutil.CollectAndCount(c); got != 0 {
		t.Fatalf("expected no metrics for empty aggregator, got %d", got)
	}

	// 动态序列不做静态描述（unchecked collector 惯例）
	descCh := make(chan *prometheus.Desc, 8)
	c.Describe(descCh)
	close(descCh)
	if got := len(descCh); got != 0 {
		t.Fatalf("expected no static descriptors, got %d", got)
	}
}

func TestMetricsPrometheusCollector_ExportsAggregatedSeries(t *testing.T) {
	agg := NewMetricsAggregator()
	agg.Add("sdk_messages_sent_total", map[string]string{"source": "sdk", "tenant": "t1", "type": "text"}, 2)
	agg.Add("sdk_messages_sent_total", map[string]string{"source": "sdk", "tenant": "t1", "type": "text"}, 3)
	agg.Add("agent_online_gauge", map[string]string{"source": "sdk"}, -1)
	agg.Add("admin_actions_total", nil, 1)

	expected := `
# HELP sdk_messages_sent_total Client-reported metric aggregated from /api/v1/metrics/ingest.
# TYPE sdk_messages_sent_total counter
sdk_messages_sent_total{source="sdk",tenant="t1",type="text"} 5
# HELP agent_online_gauge Client-reported metric aggregated from /api/v1/metrics/ingest.
# TYPE agent_online_gauge gauge
agent_online_gauge{source="sdk"} -1
# HELP admin_actions_total Client-reported metric aggregated from /api/v1/metrics/ingest.
# TYPE admin_actions_total counter
admin_actions_total 1
`
	if err := testutil.CollectAndCompare(NewMetricsPrometheusCollector(agg), strings.NewReader(expected)); err != nil {
		t.Fatalf("unexpected collection result: %v", err)
	}
}

func TestMetricsAggregator_SnapshotSeries(t *testing.T) {
	agg := NewMetricsAggregator()
	agg.Add("sdk_messages_sent_total", map[string]string{"source": "sdk", "tenant": "t1"}, 1)
	agg.Add("sdk_messages_sent_total", map[string]string{"source": "sdk", "tenant": "t1"}, 4)
	agg.Add("sdk_messages_sent_total", map[string]string{"source": "admin"}, 2)
	agg.Add("agent_online_gauge", nil, -1)

	snap := agg.SnapshotSeries()
	if len(snap) != 2 {
		t.Fatalf("expected 2 metric names, got %d", len(snap))
	}
	series := snap["sdk_messages_sent_total"]
	if len(series) != 2 {
		t.Fatalf("expected 2 series for sdk_messages_sent_total, got %d", len(series))
	}
	bySource := map[string]float64{}
	for _, s := range series {
		bySource[s.Labels["source"]] = s.Value
	}
	if bySource["sdk"] != 5 {
		t.Fatalf("expected same-label series to accumulate to 5, got %v", bySource["sdk"])
	}
	if bySource["admin"] != 2 {
		t.Fatalf("expected admin series value 2, got %v", bySource["admin"])
	}

	gaugeSeries := snap["agent_online_gauge"]
	if len(gaugeSeries) != 1 || gaugeSeries[0].Value != -1 {
		t.Fatalf("expected zero-label gauge series value -1, got %+v", gaugeSeries)
	}

	// 快照里的 Labels 是防御性拷贝：改写不得影响内部状态
	gaugeSeries[0].Labels["source"] = "mutated"
	snap2 := agg.SnapshotSeries()
	if v, ok := snap2["agent_online_gauge"][0].Labels["source"]; ok {
		t.Fatalf("expected snapshot labels to be defensive copies, got %q", v)
	}
}

func TestClientMetricType(t *testing.T) {
	if got := clientMetricType("agent_online_gauge"); got != prometheus.GaugeValue {
		t.Fatalf("expected gauge for _gauge suffix, got %v", got)
	}
	if got := clientMetricType("sdk_messages_sent_total"); got != prometheus.CounterValue {
		t.Fatalf("expected counter for _total suffix, got %v", got)
	}
}

func TestSortedLabelNamesAndValues(t *testing.T) {
	labels := map[string]string{"tenant": "t1", "source": "sdk", "type": "text"}
	names := sortedLabelNames(labels)
	if len(names) != 3 || names[0] != "source" || names[1] != "tenant" || names[2] != "type" {
		t.Fatalf("unexpected label names: %v", names)
	}
	values := labelValues(labels, names)
	if len(values) != 3 || values[0] != "sdk" || values[1] != "t1" || values[2] != "text" {
		t.Fatalf("unexpected label values: %v", values)
	}
	if got := sortedLabelNames(nil); len(got) != 0 {
		t.Fatalf("expected empty names for nil labels, got %v", got)
	}
}
