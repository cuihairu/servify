package handlers

import (
	"sort"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// clientMetricHelp 是客户端上报指标族的统一 Help 文本。
const clientMetricHelp = "Client-reported metric aggregated from /api/v1/metrics/ingest."

// MetricsPrometheusCollector 把 MetricsAggregator 的内存聚合桥接为
// prometheus 指标族，使客户端上报指标随 /metrics 端点对外暴露。
// 序列集合随上报动态增减，运行期才能确定，按 unchecked collector
// 惯例 Describe 不输出静态描述符。
type MetricsPrometheusCollector struct {
	agg *MetricsAggregator
}

// NewMetricsPrometheusCollector 构造桥接 collector。
func NewMetricsPrometheusCollector(agg *MetricsAggregator) *MetricsPrometheusCollector {
	return &MetricsPrometheusCollector{agg: agg}
}

// Describe 实现 prometheus.Collector：动态序列不做静态描述。
// 空函数体无法被覆盖统计，故显式丢弃参数。
func (c *MetricsPrometheusCollector) Describe(ch chan<- *prometheus.Desc) {
	_ = ch
}

// Collect 实现 prometheus.Collector：导出当前全部聚合序列。
// 名字带 _gauge 后缀的白名单指标（如 agent_online_gauge，在线量可增可减）
// 按 Gauge 导出，其余按 Counter 导出。
func (c *MetricsPrometheusCollector) Collect(ch chan<- prometheus.Metric) {
	for name, series := range c.agg.SnapshotSeries() {
		for _, s := range series {
			labelNames := sortedLabelNames(s.Labels)
			ch <- prometheus.MustNewConstMetric(
				prometheus.NewDesc(name, clientMetricHelp, labelNames, nil),
				clientMetricType(name),
				s.Value,
				labelValues(s.Labels, labelNames)...,
			)
		}
	}
}

func clientMetricType(name string) prometheus.ValueType {
	if strings.HasSuffix(name, "_gauge") {
		return prometheus.GaugeValue
	}
	return prometheus.CounterValue
}

func sortedLabelNames(labels map[string]string) []string {
	names := make([]string, 0, len(labels))
	for k := range labels {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func labelValues(labels map[string]string, names []string) []string {
	values := make([]string, 0, len(names))
	for _, n := range names {
		values = append(values, labels[n])
	}
	return values
}
