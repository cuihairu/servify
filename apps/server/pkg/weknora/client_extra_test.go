package weknora

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientDoRequestWithRetryCreateRequestFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, Timeout: time.Second, MaxRetries: 0}, nil)
	// A method containing a space cannot build a valid request.
	if err := client.doRequestWithRetry(context.Background(), "BAD METHOD", "/api/v1/health", nil, nil); err == nil {
		t.Fatal("doRequestWithRetry() expected request build error")
	}
}

func TestClientDoRequestReadBodyFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		_, _ = w.Write([]byte("short"))
		// Returning early truncates the declared body, forcing a read error
		// on the client side.
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, Timeout: 5 * time.Second, MaxRetries: 0}, nil)

	req, err := client.createRequest(context.Background(), http.MethodGet, "/api/v1/health", nil)
	if err != nil {
		t.Fatalf("createRequest() error = %v", err)
	}
	if err := client.doRequest(req, nil); err == nil {
		t.Fatal("doRequest() expected body read error")
	}
}

func TestClientPublicMethodsPropagateTransportErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	client := NewClient(&Config{BaseURL: url, Timeout: 2 * time.Second, MaxRetries: 0}, nil)
	ctx := context.Background()

	if _, err := client.SearchKnowledge(ctx, &SearchRequest{Query: "q", KnowledgeBaseID: "kb"}); err == nil {
		t.Fatal("SearchKnowledge() expected transport error")
	}
	if _, err := client.UploadDocument(ctx, "kb", &Document{Title: "doc"}); err == nil {
		t.Fatal("UploadDocument() expected transport error")
	}
	if _, err := client.CreateKnowledgeBase(ctx, &CreateKBRequest{Name: "kb"}); err == nil {
		t.Fatal("CreateKnowledgeBase() expected transport error")
	}
	if _, err := client.GetKnowledgeBase(ctx, "kb"); err == nil {
		t.Fatal("GetKnowledgeBase() expected transport error")
	}
	if _, err := client.CreateSession(ctx, &SessionRequest{UserID: "u"}); err == nil {
		t.Fatal("CreateSession() expected transport error")
	}
	if _, err := client.Chat(ctx, "sess", &ChatRequest{Message: "hi"}); err == nil {
		t.Fatal("Chat() expected transport error")
	}
	if err := client.HealthCheck(ctx); err == nil {
		t.Fatal("HealthCheck() expected transport error")
	}
}

func TestClientSearchAppliesDefaults(t *testing.T) {
	var received SearchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"results":[],"total":0}}`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, Timeout: 5 * time.Second, MaxRetries: 0}, nil)
	resp, err := client.SearchKnowledge(context.Background(), &SearchRequest{Query: "hello", KnowledgeBaseID: "kb-1"})
	if err != nil {
		t.Fatalf("SearchKnowledge() error = %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success response: %+v", resp)
	}
	if received.Limit != 5 || received.Threshold != 0.7 || received.Strategy != "hybrid" {
		t.Fatalf("request defaults not applied: %+v", received)
	}
}

func TestClientValidationErrors(t *testing.T) {
	client := NewClient(&Config{BaseURL: "http://127.0.0.1:1", Timeout: time.Second, MaxRetries: 0}, nil)
	ctx := context.Background()

	if _, err := client.SearchKnowledge(ctx, &SearchRequest{Query: "q"}); err == nil {
		t.Fatal("expected missing kb id error")
	}
	if _, err := client.SearchKnowledge(ctx, &SearchRequest{KnowledgeBaseID: "kb"}); err == nil {
		t.Fatal("expected missing query error")
	}
	if _, err := client.UploadDocument(ctx, "", &Document{Title: "t"}); err == nil {
		t.Fatal("expected missing kb id error")
	}
	if _, err := client.UploadDocument(ctx, "kb", &Document{}); err == nil {
		t.Fatal("expected missing title error")
	}
	if _, err := client.CreateKnowledgeBase(ctx, &CreateKBRequest{}); err == nil {
		t.Fatal("expected missing name error")
	}
	if _, err := client.GetKnowledgeBase(ctx, " "); err == nil {
		t.Fatal("expected missing kb id error")
	}
	if _, err := client.CreateSession(ctx, &SessionRequest{}); err == nil {
		t.Fatal("expected missing user id error")
	}
	if _, err := client.Chat(ctx, "", &ChatRequest{Message: "m"}); err == nil {
		t.Fatal("expected missing session id error")
	}
	if _, err := client.Chat(ctx, "sess", &ChatRequest{}); err == nil {
		t.Fatal("expected missing message error")
	}
	if err := client.DeleteDocument(ctx, "", "doc"); err == nil {
		t.Fatal("expected missing kb id error")
	}
	if err := client.DeleteDocument(ctx, "kb", ""); err == nil {
		t.Fatal("expected missing document id error")
	}
}

func TestDeleteDocument_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/knowledge/kb-123/documents/doc-456" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"message":"Deleted successfully"}`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, Timeout: 5 * time.Second}, nil)

	if err := client.DeleteDocument(context.Background(), "kb-123", "doc-456"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteDocument_ServerRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"success":false,"error":{"code":"not_found","message":"document not found"}}`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, Timeout: 5 * time.Second, MaxRetries: 0}, nil)

	err := client.DeleteDocument(context.Background(), "kb-123", "missing")
	if err == nil || !strings.Contains(err.Error(), "document not found") {
		t.Fatalf("expected API error, got %v", err)
	}
}

func TestDeleteDocument_UnsuccessfulResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":false,"message":"deletion is disabled"}`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, Timeout: 5 * time.Second, MaxRetries: 0}, nil)

	err := client.DeleteDocument(context.Background(), "kb-123", "doc-456")
	if err == nil || !strings.Contains(err.Error(), "deletion is disabled") {
		t.Fatalf("expected unsuccessful response error, got %v", err)
	}
}

func TestClientGetStats(t *testing.T) {
	client := NewClient(&Config{
		BaseURL:    "http://weknora.example",
		TenantID:   "tenant-1",
		Timeout:    3 * time.Second,
		MaxRetries: 4,
	}, nil)

	stats := client.GetStats()
	if stats["base_url"] != "http://weknora.example" || stats["tenant_id"] != "tenant-1" {
		t.Fatalf("stats = %v", stats)
	}
	if stats["max_retries"] != 4 {
		t.Fatalf("stats max_retries = %v", stats["max_retries"])
	}
}

func TestNewClientDefaults(t *testing.T) {
	if client := NewClient(nil, nil); client == nil {
		t.Fatal("expected client with default config")
	}
}
