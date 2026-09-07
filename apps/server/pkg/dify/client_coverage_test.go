package dify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientCreateDocumentFromText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q", r.Method)
		}
		if r.URL.Path != "/datasets/ds-1/document/create-by-text" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var body CreateDocumentRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body.Name != "policy.txt" {
			t.Fatalf("name = %q", body.Name)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"document": map[string]any{"id": "doc-9", "name": "policy.txt"},
		})
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, APIKey: "key"})
	doc, err := client.CreateDocumentFromText(context.Background(), "ds-1", &CreateDocumentRequest{
		Name: "policy.txt",
		Text: "refund policy",
	})
	if err != nil {
		t.Fatalf("CreateDocumentFromText() error = %v", err)
	}
	if doc.ID != "doc-9" || doc.Name != "policy.txt" {
		t.Fatalf("document = %+v", doc)
	}
}

func TestClientDeleteDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("method = %q", r.Method)
		}
		if r.URL.Path != "/datasets/ds-1/documents/doc-9" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	if err := client.DeleteDocument(context.Background(), "ds-1", "doc-9"); err != nil {
		t.Fatalf("DeleteDocument() error = %v", err)
	}
}

func TestClientGetDatasetReturnsErrorOnHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	if _, err := client.GetDataset(context.Background(), "missing"); err == nil {
		t.Fatal("GetDataset() expected error for 404 response")
	}
}

func TestClientRetrieveReturnsErrorOnInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	if _, err := client.Retrieve(context.Background(), "ds-1", &RetrieveRequest{Query: "q"}); err == nil {
		t.Fatal("Retrieve() expected decode error")
	}
}

func TestClientRetrieveUsesTopLevelFieldsFirst(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query": "refund",
			"records": []map[string]any{
				{
					"segment_id":  "seg-top",
					"document_id": "doc-top",
					"title":       "Top Title",
					"content":     "  top-level content  ",
					"score":       0.5,
					"metadata":    map[string]any{"source": "top"},
					"segment": map[string]any{
						"id":      "seg-nested",
						"content": "nested content",
					},
					"document": map[string]any{
						"id":   "doc-nested",
						"name": "Nested Name",
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	resp, err := client.Retrieve(context.Background(), "ds-1", &RetrieveRequest{Query: "refund"})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if len(resp.Records) != 1 {
		t.Fatalf("records = %d", len(resp.Records))
	}
	rec := resp.Records[0]
	if rec.SegmentID != "seg-top" || rec.DocumentID != "doc-top" {
		t.Fatalf("ids = %q/%q, want seg-top/doc-top", rec.SegmentID, rec.DocumentID)
	}
	if rec.Title != "Top Title" {
		t.Fatalf("title = %q", rec.Title)
	}
	if rec.Content != "top-level content" {
		t.Fatalf("content = %q", rec.Content)
	}
	if rec.Metadata["source"] != "top" {
		t.Fatalf("metadata = %v", rec.Metadata)
	}
}

type unserializable struct {
	Ch chan int `json:"ch"`
}

func TestClientRequestFailsOnUnserializableBody(t *testing.T) {
	client := NewClient(&Config{BaseURL: "http://127.0.0.1:1"})
	if err := client.do(context.Background(), http.MethodPost, "/datasets/x", unserializable{}, nil); err == nil {
		t.Fatal("do() expected marshal error")
	}
}

func TestClientRequestFailsOnInvalidBaseURL(t *testing.T) {
	client := NewClient(&Config{BaseURL: "ht tp://bad url"})
	err := client.do(context.Background(), http.MethodGet, "/datasets/x", nil, nil)
	if err == nil {
		t.Fatal("do() expected request creation error")
	}
}

func TestClientRequestFailsOnCancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, Timeout: 5 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.HealthCheck(ctx, "ds-1"); err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestClientRetrieveServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	if _, err := client.Retrieve(context.Background(), "ds-1", &RetrieveRequest{Query: "q"}); err == nil {
		t.Fatal("Retrieve() expected error for 500 response")
	}
}

func TestNewClientDefaults(t *testing.T) {
	client := NewClient(nil)
	if client.baseURL != "http://localhost/v1" {
		t.Fatalf("baseURL = %q", client.baseURL)
	}
	if client.apiKey != "" {
		t.Fatalf("apiKey = %q", client.apiKey)
	}

	client = NewClient(&Config{BaseURL: "  http://example.com/v1/  ", APIKey: " k "})
	if client.baseURL != "http://example.com/v1" {
		t.Fatalf("baseURL = %q", client.baseURL)
	}
	if client.apiKey != "k" {
		t.Fatalf("apiKey = %q", client.apiKey)
	}
}
