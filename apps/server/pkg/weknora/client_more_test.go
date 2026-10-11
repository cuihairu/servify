package weknora

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestChat_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求
		if r.Method != "POST" {
			t.Errorf("expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/sessions/session-123/chat" {
			t.Errorf("expected path /api/v1/sessions/session-123/chat, got %s", r.URL.Path)
		}

		var req ChatRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Message != "test message" {
			t.Errorf("expected message 'test message', got '%s'", req.Message)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{"response":"test response"}}`))
	}))
	defer server.Close()

	client := NewClient(&Config{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	}, nil)

	resp, err := client.Chat(context.Background(), "session-123", &ChatRequest{
		Message: "test message",
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp.Response != "test response" {
		t.Errorf("expected response 'test response', got '%s'", resp.Response)
	}
}

func TestChat_WithMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.MessageType != "text" {
			t.Errorf("expected message_type 'text', got '%s'", req.MessageType)
		}
		if req.Metadata["session_id"] != "custom-session" {
			t.Errorf("expected session_id in metadata")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{"response":"test response with metadata"}}`))
	}))
	defer server.Close()

	client := NewClient(&Config{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	}, nil)

	resp, err := client.Chat(context.Background(), "session-123", &ChatRequest{
		Message:     "test message",
		MessageType: "text",
		Metadata:    map[string]interface{}{"session_id": "custom-session"},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp.Response != "test response with metadata" {
		t.Errorf("expected response 'test response with metadata', got '%s'", resp.Response)
	}
}

func TestSearchKnowledge_WithStrategy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req SearchRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Strategy != "vector" {
			t.Errorf("expected strategy 'vector', got '%s'", req.Strategy)
		}
		if req.Limit != 10 {
			t.Errorf("expected limit 10, got %d", req.Limit)
		}
		if req.Threshold != 0.8 {
			t.Errorf("expected threshold 0.8, got %f", req.Threshold)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{"results":[],"total":0}}`))
	}))
	defer server.Close()

	client := NewClient(&Config{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	}, nil)

	_, err := client.SearchKnowledge(context.Background(), &SearchRequest{
		KnowledgeBaseID: "kb-123",
		Query:           "test query",
		Strategy:        "vector",
		Limit:           10,
		Threshold:       0.8,
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUploadDocument_WithMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var doc Document
		err := json.NewDecoder(r.Body).Decode(&doc)
		if err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if doc.Title != "Test Doc" {
			t.Errorf("expected title 'Test Doc', got '%s'", doc.Title)
		}
		if doc.Type != "text" {
			t.Errorf("expected type 'text', got '%s'", doc.Type)
		}
		if len(doc.Tags) != 2 {
			t.Errorf("expected 2 tags, got %d", len(doc.Tags))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":"doc-123","title":"Test Doc"}}`))
	}))
	defer server.Close()

	client := NewClient(&Config{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	}, nil)

	result, err := client.UploadDocument(context.Background(), "kb-123", &Document{
		Type:     "text",
		Title:    "Test Doc",
		Content:  "Test content",
		Tags:     []string{"api", "guide"},
		Metadata: map[string]interface{}{"author": "test"},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.ID != "doc-123" {
		t.Errorf("expected document id 'doc-123', got '%s'", result.ID)
	}
}

func TestCreateKnowledgeBase_WithConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req CreateKBRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Name != "Test KB" {
			t.Errorf("expected name 'Test KB', got '%s'", req.Name)
		}
		if req.Description != "Test knowledge base" {
			t.Errorf("expected description 'Test knowledge base', got '%s'", req.Description)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":"kb-123","name":"Test KB"}}`))
	}))
	defer server.Close()

	client := NewClient(&Config{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	}, nil)

	result, err := client.CreateKnowledgeBase(context.Background(), &CreateKBRequest{
		Name:        "Test KB",
		Description: "Test knowledge base",
		Config: KBConfig{
			ChunkSize:    500,
			ChunkOverlap: 50,
		},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.ID != "kb-123" {
		t.Errorf("expected kb id 'kb-123', got '%s'", result.ID)
	}
}

func TestGetKnowledgeBase_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":"kb-123","name":"Test KB","description":"Test"}}`))
	}))
	defer server.Close()

	client := NewClient(&Config{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	}, nil)

	result, err := client.GetKnowledgeBase(context.Background(), "kb-123")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.ID != "kb-123" {
		t.Errorf("expected kb id 'kb-123', got '%s'", result.ID)
	}
	if result.Name != "Test KB" {
		t.Errorf("expected name 'Test KB', got '%s'", result.Name)
	}
}

func TestCreateSession_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req SessionRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.UserID != "user-123" {
			t.Errorf("expected user_id 'user-123', got '%s'", req.UserID)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":"session-456","user_id":"user-123"}}`))
	}))
	defer server.Close()

	client := NewClient(&Config{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	}, nil)

	result, err := client.CreateSession(context.Background(), &SessionRequest{
		UserID:      "user-123",
		SessionType: "chat",
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.ID != "session-456" {
		t.Errorf("expected session id 'session-456', got '%s'", result.ID)
	}
}

func TestChat_WithSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{"response":"test response","sources":[{"title":"Source 1","content":"Content 1"}],"confidence":0.9}}`))
	}))
	defer server.Close()

	client := NewClient(&Config{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	}, nil)

	resp, err := client.Chat(context.Background(), "session-123", &ChatRequest{
		Message: "test message",
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp.Response != "test response" {
		t.Errorf("expected response 'test response', got '%s'", resp.Response)
	}
	if len(resp.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(resp.Sources))
	}
	if resp.Confidence != 0.9 {
		t.Errorf("expected confidence 0.9, got %f", resp.Confidence)
	}
}
