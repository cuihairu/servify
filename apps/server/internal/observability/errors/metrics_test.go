package errors

import (
	"errors"
	"testing"

	"servify/apps/server/internal/observability/metrics"
)

func TestRecordError_NilAppError(t *testing.T) {
	RecordError(nil)
}

func TestRegisterErrorMetricsAndRecordError(t *testing.T) {
	reg := metrics.NewRegistry()
	RegisterErrorMetrics(reg)

	appErr := New(errors.New("boom"), SeverityDependency, CategoryAI, WithModule("llm"))
	RecordError(appErr)
	RecordError(appErr)

	mfs, err := reg.Gatherer().Gather()
	if err != nil {
		t.Fatalf("unexpected gather error: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "errors_total" {
			continue
		}
		if len(mf.GetMetric()) != 1 {
			t.Fatalf("expected 1 series, got %d", len(mf.GetMetric()))
		}
		m := mf.GetMetric()[0]
		if m.GetCounter().GetValue() != 2 {
			t.Fatalf("expected counter 2, got %v", m.GetCounter().GetValue())
		}
		labels := map[string]string{}
		for _, lp := range m.GetLabel() {
			labels[lp.GetName()] = lp.GetValue()
		}
		if labels["severity"] != string(SeverityDependency) {
			t.Fatalf("expected severity label %s, got %s", SeverityDependency, labels["severity"])
		}
		if labels["error_category"] != string(CategoryAI) {
			t.Fatalf("expected category label %s, got %s", CategoryAI, labels["error_category"])
		}
		if labels["error_module"] != "llm" {
			t.Fatalf("expected module label llm, got %s", labels["error_module"])
		}
		return
	}
	t.Fatal("expected errors_total metric")
}
