package infra

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"servify/apps/server/internal/modules/webhook/application"
	webhookdelivery "servify/apps/server/internal/modules/webhook/delivery"
)

func TestHTTPDelivererSignsAndDelivers(t *testing.T) {
	var gotSignature, gotEvent, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotSignature = r.Header.Get("X-Servify-Signature")
		gotEvent = r.Header.Get("X-Servify-Event")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	deliverer := NewHTTPDelivererWithClient(server.Client())
	body := []byte(`{"event":"ticket.created","id":"evt-1"}`)
	result := deliverer.Deliver(context.Background(), application.DeliveryRequest{
		URL:        server.URL,
		Secret:     "shhh",
		EventName:  "ticket.created",
		DeliveryID: 42,
		Body:       body,
		Timeout:    5 * time.Second,
	})
	if !result.Success || result.StatusCode != http.StatusOK {
		t.Fatalf("expected success, got %+v", result)
	}
	if gotEvent != "ticket.created" || gotBody != string(body) {
		t.Fatalf("headers/body not delivered: %q %q", gotEvent, gotBody)
	}
	if !webhookdelivery.VerifySignature("shhh", body, gotSignature, time.Now()) {
		t.Fatalf("delivered signature should verify: %q", gotSignature)
	}
}

func TestHTTPDelivererReportsNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer server.Close()

	result := NewHTTPDelivererWithClient(server.Client()).Deliver(context.Background(), application.DeliveryRequest{
		URL: server.URL, EventName: "ping", Body: []byte("{}"),
	})
	if result.Success || result.StatusCode != http.StatusGone {
		t.Fatalf("expected 410 failure, got %+v", result)
	}
	if result.Error == "" {
		t.Fatal("failure should carry an error message")
	}
}

func TestHTTPDelivererReportsTransportError(t *testing.T) {
	deliverer := NewHTTPDelivererWithClient(&http.Client{Timeout: time.Second})
	result := deliverer.Deliver(context.Background(), application.DeliveryRequest{
		URL: "http://127.0.0.1:1/nope", EventName: "ping", Body: []byte("{}"),
	})
	if result.Success || result.Error == "" {
		t.Fatalf("expected transport failure, got %+v", result)
	}
}

func TestHTTPDelivererUnsignedWhenSecretEmpty(t *testing.T) {
	var gotSignature string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSignature = r.Header.Get("X-Servify-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result := NewHTTPDelivererWithClient(server.Client()).Deliver(context.Background(), application.DeliveryRequest{
		URL: server.URL, EventName: "automation.call_webhook", Body: []byte("{}"),
	})
	if !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}
	if gotSignature != "" {
		t.Fatalf("empty secret must not sign: %q", gotSignature)
	}
}
