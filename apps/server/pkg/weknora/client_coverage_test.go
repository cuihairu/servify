package weknora

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientRetriesOnServerErrorThenSucceeds(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`temporary failure`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	}))
	defer server.Close()

	client := NewClient(&Config{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 2,
		RetryDelay: time.Millisecond,
	}, nil)

	if err := client.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

func TestClientRetriesExhaustedReturnsLastError(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`bad gateway`))
	}))
	defer server.Close()

	client := NewClient(&Config{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 2,
		RetryDelay: time.Millisecond,
	}, nil)

	err := client.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("HealthCheck() expected error after retries exhausted")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("error = %v, want 502 status detail", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("attempts = %d, want 3 (initial + 2 retries)", got)
	}
}

func TestClientRetryWaitsRespectContextCancellation(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`down`))
	}))
	defer server.Close()

	client := NewClient(&Config{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 3,
		RetryDelay: 10 * time.Second,
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel while the client is waiting on the long retry backoff.
	go func() {
		for atomic.LoadInt32(&attempts) == 0 {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()

	err := client.HealthCheck(ctx)
	if err == nil {
		t.Fatal("HealthCheck() expected context error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry after cancellation)", got)
	}
}

func TestClientAPIErrorWithStructuredBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"knowledge base not found","error_code":"KB_NOT_FOUND"}`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, Timeout: 5 * time.Second, MaxRetries: 0}, nil)

	err := client.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("HealthCheck() expected error")
	}
	if !strings.Contains(err.Error(), "KB_NOT_FOUND") || !strings.Contains(err.Error(), "knowledge base not found") {
		t.Fatalf("error = %v, want structured API error detail", err)
	}
}

func TestClientRequestFailurePropagates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	client := NewClient(&Config{BaseURL: url, Timeout: 2 * time.Second, MaxRetries: 0}, nil)
	if err := client.HealthCheck(context.Background()); err == nil {
		t.Fatal("HealthCheck() expected transport error")
	}
}

type unserializableBody struct {
	Ch chan int `json:"ch"`
}

func TestClientCreateRequestMarshalFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, Timeout: 5 * time.Second, MaxRetries: 1, RetryDelay: time.Millisecond}, nil)
	req, err := client.createRequest(context.Background(), http.MethodPost, "/api/v1/health", unserializableBody{})
	if err == nil {
		t.Fatal("createRequest() expected marshal error")
	}
	if req != nil {
		t.Fatalf("request = %+v, want nil", req)
	}
}

func TestClientCreateRequestInvalidURL(t *testing.T) {
	client := NewClient(&Config{BaseURL: "http://[::1]:namedport", Timeout: 5 * time.Second}, nil)
	if _, err := client.createRequest(context.Background(), http.MethodGet, "/api/v1/health", nil); err == nil {
		t.Fatal("createRequest() expected URL error")
	}
}

func TestClientDecodeResponseFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, Timeout: 5 * time.Second, MaxRetries: 0}, nil)

	var result HealthResponse
	req, err := client.createRequest(context.Background(), http.MethodGet, "/api/v1/health", nil)
	if err != nil {
		t.Fatalf("createRequest() error = %v", err)
	}
	if err := client.doRequest(req, &result); err == nil {
		t.Fatal("doRequest() expected decode error")
	}
}

func TestClientDoRequestWithoutResultSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ignored body`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, Timeout: 5 * time.Second}, nil)
	req, err := client.createRequest(context.Background(), http.MethodGet, "/api/v1/health", nil)
	if err != nil {
		t.Fatalf("createRequest() error = %v", err)
	}
	if err := client.doRequest(req, nil); err != nil {
		t.Fatalf("doRequest() error = %v", err)
	}
}

func TestClientSuccessFalsePaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"message":"rejected by server"}`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, Timeout: 5 * time.Second, MaxRetries: 0}, nil)
	ctx := context.Background()

	if _, err := client.CreateKnowledgeBase(ctx, &CreateKBRequest{Name: "kb"}); err == nil || !strings.Contains(err.Error(), "rejected by server") {
		t.Fatalf("CreateKnowledgeBase() error = %v", err)
	}
	if _, err := client.GetKnowledgeBase(ctx, "kb-1"); err == nil || !strings.Contains(err.Error(), "rejected by server") {
		t.Fatalf("GetKnowledgeBase() error = %v", err)
	}
	if _, err := client.CreateSession(ctx, &SessionRequest{UserID: "u1"}); err == nil || !strings.Contains(err.Error(), "rejected by server") {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := client.UploadDocument(ctx, "kb-1", &Document{Title: "doc"}); err == nil || !strings.Contains(err.Error(), "rejected by server") {
		t.Fatalf("UploadDocument() error = %v", err)
	}
	if _, err := client.Chat(ctx, "sess-1", &ChatRequest{Message: "hi"}); err == nil || !strings.Contains(err.Error(), "rejected by server") {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestClientHealthCheckUnhealthyStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"degraded"}`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, Timeout: 5 * time.Second, MaxRetries: 0}, nil)
	if err := client.HealthCheck(context.Background()); err == nil || !strings.Contains(err.Error(), "degraded") {
		t.Fatalf("HealthCheck() error = %v, want degraded status error", err)
	}
}

func TestClientHealthCheckOKStatusAccepted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, Timeout: 5 * time.Second, MaxRetries: 0}, nil)
	if err := client.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}
