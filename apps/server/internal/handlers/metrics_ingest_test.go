package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLabelsKey_StableOrder(t *testing.T) {
	m := map[string]string{"b": "2", "a": "1", "c": "3"}
	got := labelsKey(m)
	// expect a sorted order by key: a,b,c
	want := "a=\"1\",b=\"2\",c=\"3\""
	if got != want {
		t.Fatalf("labelsKey order mismatch: got %q, want %q", got, want)
	}
}

func TestMetricsIngest_BasicFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	agg := NewMetricsAggregator()
	h := NewMetricsIngestHandler(agg)

	r := gin.New()
	r.POST("/api/v1/metrics/ingest", h.Ingest)

	// prepare payload: include one allowed and one disallowed metric
	payload := MetricsIngestRequest{
		Source:    "sdk",
		Tenant:    "t1",
		SessionID: "s1",
		Metrics: []IngestedMetric{
			{Name: "sdk_messages_sent_total", Value: 2, Labels: map[string]string{"type": "text"}},
			{Name: "unknown_metric", Value: 10},
		},
	}
	buf, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/metrics/ingest", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ingest status = %d, body=%s", w.Code, w.Body.String())
	}

	snap := agg.SnapshotSeries()
	if len(snap) == 0 {
		t.Fatalf("expected some counters after ingest")
	}
	if _, ok := snap["sdk_messages_sent_total"]; !ok {
		t.Fatalf("expected sdk_messages_sent_total present in snapshot")
	}
	if _, ok := snap["unknown_metric"]; ok {
		t.Fatalf("unknown_metric should be ignored by whitelist")
	}
}
