package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewProvider_Defaults(t *testing.T) {
	p := NewProvider(Config{})
	if p.config.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("expected default BaseURL, got %s", p.config.BaseURL)
	}
	if p.config.Model != "text-embedding-3-small" {
		t.Fatalf("expected default Model, got %s", p.config.Model)
	}
	if p.config.Timeout != 30*time.Second {
		t.Fatalf("expected default Timeout 30s, got %s", p.config.Timeout)
	}
	if p.client.Timeout != 30*time.Second {
		t.Fatalf("expected client timeout 30s, got %s", p.client.Timeout)
	}

	p = NewProvider(Config{BaseURL: "http://custom", Model: "custom-model", Timeout: 5 * time.Second})
	if p.config.BaseURL != "http://custom" {
		t.Fatalf("expected BaseURL preserved, got %s", p.config.BaseURL)
	}
	if p.config.Model != "custom-model" {
		t.Fatalf("expected Model preserved, got %s", p.config.Model)
	}
	if p.config.Timeout != 5*time.Second {
		t.Fatalf("expected Timeout preserved, got %s", p.config.Timeout)
	}
}

func TestProvider_Embed_Success(t *testing.T) {
	var (
		gotMethod      string
		gotPath        string
		gotAuth        string
		gotContentType string
		gotReq         embedRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3],"index":0}],"model":"text-embedding-3-small"}`))
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL, APIKey: "test-key", Model: "text-embedding-3-small"})
	vectors, err := provider.Embed(context.Background(), []string{"hello world"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(vectors) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vectors))
	}
	if len(vectors[0]) != 3 {
		t.Fatalf("expected 3 dimensions, got %d", len(vectors[0]))
	}
	if vectors[0][0] != 0.1 || vectors[0][1] != 0.2 || vectors[0][2] != 0.3 {
		t.Fatalf("unexpected embedding values: %v", vectors[0])
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/embeddings" {
		t.Fatalf("expected path /embeddings, got %s", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("expected Authorization header, got %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", gotContentType)
	}
	if len(gotReq.Input) != 1 || gotReq.Input[0] != "hello world" {
		t.Fatalf("unexpected request input: %+v", gotReq.Input)
	}
	if gotReq.Model != "text-embedding-3-small" {
		t.Fatalf("unexpected request model: %s", gotReq.Model)
	}
}

func TestProvider_Embed_Multiple_Reordered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[3.0],"index":2},{"embedding":[1.0],"index":0},{"embedding":[2.0],"index":1}],"model":"m"}`))
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	vectors, err := provider.Embed(context.Background(), []string{"first", "second", "third"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(vectors) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(vectors))
	}
	if vectors[0][0] != 1.0 || vectors[1][0] != 2.0 || vectors[2][0] != 3.0 {
		t.Fatalf("expected vectors ordered by index, got %v", vectors)
	}
}

func TestProvider_Embed_NoAPIKey(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.5],"index":0}],"model":"m"}`))
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	if _, err := provider.Embed(context.Background(), []string{"text"}); err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if gotAuth != "" {
		t.Fatalf("expected no Authorization header, got %q", gotAuth)
	}
}

func TestProvider_Embed_EmptyInput(t *testing.T) {
	provider := NewProvider(Config{})
	_, err := provider.Embed(context.Background(), []string{})
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	if !strings.Contains(err.Error(), "no texts provided") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProvider_Embed_CreateRequestError(t *testing.T) {
	provider := NewProvider(Config{BaseURL: "http://[::1]:namedport"})
	_, err := provider.Embed(context.Background(), []string{"text"})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
	if !strings.Contains(err.Error(), "create request") {
		t.Fatalf("expected create request error, got %v", err)
	}
}

func TestProvider_Embed_SendRequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL, Timeout: 1 * time.Nanosecond})
	_, err := provider.Embed(context.Background(), []string{"text"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "send request") {
		t.Fatalf("expected send request error, got %v", err)
	}
}

func TestProvider_Embed_ReadResponseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("response writer does not support hijacking")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()
		if _, err := buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\n{\"data\":[{"); err != nil {
			t.Errorf("write: %v", err)
			return
		}
		if err := buf.Flush(); err != nil {
			t.Errorf("flush: %v", err)
		}
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	_, err := provider.Embed(context.Background(), []string{"text"})
	if err == nil {
		t.Fatal("expected read response error")
	}
	if !strings.Contains(err.Error(), "read response") {
		t.Fatalf("expected read response error, got %v", err)
	}
}

func TestProvider_Embed_HTTPError(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"bad request", http.StatusBadRequest},
		{"unauthorized", http.StatusUnauthorized},
		{"rate limited", http.StatusTooManyRequests},
		{"server error", http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"error":{"message":"mock error","type":"invalid_request_error"}}`))
			}))
			defer server.Close()

			provider := NewProvider(Config{BaseURL: server.URL})
			_, err := provider.Embed(context.Background(), []string{"text"})
			if err == nil {
				t.Fatal("expected error for HTTP error status")
			}
			if !strings.Contains(err.Error(), "openai error") {
				t.Fatalf("expected openai error, got %v", err)
			}
			if !strings.Contains(err.Error(), "mock error") {
				t.Fatalf("expected error body in message, got %v", err)
			}
		})
	}
}

func TestProvider_Embed_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not a json response`))
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	_, err := provider.Embed(context.Background(), []string{"text"})
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
	if !strings.Contains(err.Error(), "unmarshal response") {
		t.Fatalf("expected unmarshal response error, got %v", err)
	}
}

func TestProvider_Embed_APIErrorInBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":{"message":"insufficient quota","type":"insufficient_quota"}}`))
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	_, err := provider.Embed(context.Background(), []string{"text"})
	if err == nil {
		t.Fatal("expected API error")
	}
	if !strings.Contains(err.Error(), "insufficient quota") {
		t.Fatalf("expected API error message, got %v", err)
	}
}

func TestProvider_Embed_CountMismatch(t *testing.T) {
	tests := []struct {
		name       string
		texts      []string
		response   string
		wantErrMsg string
	}{
		{"empty data", []string{"text"}, `{"data":[],"model":"m"}`, "expected 1 embeddings, got 0"},
		{"missing data field", []string{"text"}, `{"model":"m"}`, "expected 1 embeddings, got 0"},
		{"too few embeddings", []string{"a", "b", "c"}, `{"data":[{"embedding":[1.0],"index":0}],"model":"m"}`, "expected 3 embeddings, got 1"},
		{"too many embeddings", []string{"a"}, `{"data":[{"embedding":[1.0],"index":0},{"embedding":[2.0],"index":0}],"model":"m"}`, "expected 1 embeddings, got 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			provider := NewProvider(Config{BaseURL: server.URL})
			_, err := provider.Embed(context.Background(), tt.texts)
			if err == nil {
				t.Fatal("expected count mismatch error")
			}
			if !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErrMsg, err)
			}
		})
	}
}

func TestProvider_Embed_InvalidIndex(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{"negative index", `{"data":[{"embedding":[1.0],"index":-1}],"model":"m"}`},
		{"index out of range", `{"data":[{"embedding":[1.0],"index":5}],"model":"m"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			provider := NewProvider(Config{BaseURL: server.URL})
			_, err := provider.Embed(context.Background(), []string{"text"})
			if err == nil {
				t.Fatal("expected invalid index error")
			}
			if !strings.Contains(err.Error(), "invalid index") {
				t.Fatalf("expected invalid index error, got %v", err)
			}
		})
	}
}

func TestProvider_Dimension(t *testing.T) {
	tests := []struct {
		model     string
		dimension int
	}{
		{"text-embedding-3-small", 1536},
		{"text-embedding-3-large", 3072},
		{"text-embedding-ada-002", 1536},
		{"unknown-model", 1536},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			provider := NewProvider(Config{Model: tt.model})
			if dim := provider.Dimension(); dim != tt.dimension {
				t.Fatalf("expected dimension %d, got %d", tt.dimension, dim)
			}
		})
	}
}

func TestProvider_HealthCheck(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotAuth   string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := NewProvider(Config{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})

	if err := provider.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("expected GET, got %s", gotMethod)
	}
	if gotPath != "/models" {
		t.Fatalf("expected path /models, got %s", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("expected Authorization header, got %q", gotAuth)
	}
}

func TestProvider_HealthCheck_NoAPIKey(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	if err := provider.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
	if gotAuth != "" {
		t.Fatalf("expected no Authorization header, got %q", gotAuth)
	}
}

func TestProvider_HealthCheck_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})

	if err := provider.HealthCheck(context.Background()); err == nil {
		t.Fatal("expected error for failed health check")
	}
}

func TestProvider_HealthCheck_CreateRequestError(t *testing.T) {
	provider := NewProvider(Config{BaseURL: "http://[::1]:namedport"})
	err := provider.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
	if !strings.Contains(err.Error(), "create request") {
		t.Fatalf("expected create request error, got %v", err)
	}
}

func TestProvider_HealthCheck_SendRequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL, Timeout: 1 * time.Nanosecond})
	err := provider.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "send request") {
		t.Fatalf("expected send request error, got %v", err)
	}
}
