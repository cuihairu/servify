package application

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"servify/apps/server/internal/models"
)

func TestDefaultWebhookPayloadTicketSnapshotRoundTrip(t *testing.T) {
	// seam 默认路径：正常序列化往返保持与工单 JSON 相同的字段名。
	ticket := &models.Ticket{ID: 9, Title: "printer on fire"}
	var want map[string]interface{}
	raw, err := json.Marshal(ticket)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	got := defaultWebhookPayload(ticket)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload mismatch: got %v want %v", got, want)
	}
}

func TestDefaultWebhookPayloadMarshalFailure(t *testing.T) {
	orig := ticketToJSON
	ticketToJSON = func(v interface{}) ([]byte, error) { return nil, errors.New("boom") }
	defer func() { ticketToJSON = orig }()

	payload := defaultWebhookPayload(&models.Ticket{ID: 11})
	if len(payload) != 1 || payload["ticket_id"] != uint(11) {
		t.Fatalf("expected fallback payload with ticket_id, got %v", payload)
	}
}

func TestDefaultWebhookPayloadUnmarshalFailure(t *testing.T) {
	orig := jsonToMap
	jsonToMap = func(data []byte, v interface{}) error { return errors.New("boom") }
	defer func() { jsonToMap = orig }()

	payload := defaultWebhookPayload(&models.Ticket{ID: 12})
	if len(payload) != 1 || payload["ticket_id"] != uint(12) {
		t.Fatalf("expected fallback payload with ticket_id, got %v", payload)
	}
}
