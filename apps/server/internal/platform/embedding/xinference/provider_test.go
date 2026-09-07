package xinference

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewProvider_Defaults(t *testing.T) {
	p := NewProvider(Config{})
	if p.config.BaseURL != "http://localhost:9997" {
		t.Fatalf("expected default base URL http://localhost:9997, got %s", p.config.BaseURL)
	}
	if p.config.Timeout != 30*time.Second {
		t.Fatalf("expected default timeout 30s, got %s", p.config.Timeout)
	}
}

func TestNewProvider_CustomConfig(t *testing.T) {
	p := NewProvider(Config{
		BaseURL:  "http://example.com:1234",
		ModelUID: "bge-large-zh",
		Timeout:  5 * time.Second,
	})
	if p.config.BaseURL != "http://example.com:1234" {
		t.Fatalf("unexpected base URL: %s", p.config.BaseURL)
	}
	if p.config.ModelUID != "bge-large-zh" {
		t.Fatalf("unexpected model uid: %s", p.config.ModelUID)
	}
	if p.config.Timeout != 5*time.Second {
		t.Fatalf("unexpected timeout: %s", p.config.Timeout)
	}
}

func TestProvider_Embed_Single(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("unexpected content type: %s", ct)
		}
		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if len(req.Input) != 1 || req.Input[0] != "hello world" {
			t.Errorf("unexpected input: %v", req.Input)
		}
		if req.Model != "test-model" {
			t.Errorf("unexpected model: %s", req.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"embedding":[0.1,0.2,0.3],"index":0}],"model":"test-model"}`)
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL, ModelUID: "test-model"})
	vectors, err := provider.Embed(context.Background(), []string{"hello world"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(vectors) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vectors))
	}
	if len(vectors[0]) != 3 || vectors[0][0] != 0.1 || vectors[0][1] != 0.2 || vectors[0][2] != 0.3 {
		t.Fatalf("unexpected vector: %v", vectors[0])
	}
}

func TestProvider_Embed_Multiple_SortByIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"embedding":[3],"index":2},{"embedding":[1],"index":0},{"embedding":[2],"index":1}],"model":"m"}`)
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	vectors, err := provider.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(vectors) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(vectors))
	}
	for i, v := range vectors {
		if len(v) != 1 || v[0] != float32(i+1) {
			t.Fatalf("expected vector [%d] to be [%d], got %v", i, i+1, v)
		}
	}
}

func TestProvider_Embed_EmptyInput(t *testing.T) {
	provider := NewProvider(Config{})
	ctx := context.Background()
	_, err := provider.Embed(ctx, []string{})
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	if !strings.Contains(err.Error(), "no texts provided") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProvider_Embed_HTTPError(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"bad request", http.StatusBadRequest},
		{"server error", http.StatusInternalServerError},
		{"service unavailable", http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, "boom")
			}))
			defer server.Close()

			provider := NewProvider(Config{BaseURL: server.URL})
			_, err := provider.Embed(context.Background(), []string{"test"})
			if err == nil {
				t.Fatal("expected error for HTTP error status")
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("status %d", tt.status)) {
				t.Fatalf("expected status %d in error, got: %v", tt.status, err)
			}
			if !strings.Contains(err.Error(), "boom") {
				t.Fatalf("expected body in error, got: %v", err)
			}
		})
	}
}

func TestProvider_Embed_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{invalid`)
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	_, err := provider.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "unmarshal response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProvider_Embed_ResponseErrorField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[],"model":"m","error":{"message":"model not found","code":"404"}}`)
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	_, err := provider.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected error for response error field")
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProvider_Embed_CountMismatch(t *testing.T) {
	tests := []struct {
		name    string
		texts   []string
		dataLen int
	}{
		{"empty data", []string{"a"}, 0},
		{"too few", []string{"a", "b"}, 1},
		{"too many", []string{"a"}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var data []string
				for i := 0; i < tt.dataLen; i++ {
					data = append(data, fmt.Sprintf(`{"embedding":[1],"index":%d}`, i))
				}
				fmt.Fprintf(w, `{"data":[%s],"model":"m"}`, strings.Join(data, ","))
			}))
			defer server.Close()

			provider := NewProvider(Config{BaseURL: server.URL})
			_, err := provider.Embed(context.Background(), tt.texts)
			if err == nil {
				t.Fatal("expected count mismatch error")
			}
			if !strings.Contains(err.Error(), "expected") || !strings.Contains(err.Error(), "embeddings") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestProvider_Embed_InvalidIndex(t *testing.T) {
	tests := []struct {
		name  string
		index int
	}{
		{"negative index", -1},
		{"index out of range", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"data":[{"embedding":[1],"index":%d},{"embedding":[2],"index":0}],"model":"m"}`, tt.index)
			}))
			defer server.Close()

			provider := NewProvider(Config{BaseURL: server.URL})
			_, err := provider.Embed(context.Background(), []string{"a", "b"})
			if err == nil {
				t.Fatal("expected invalid index error")
			}
			if !strings.Contains(err.Error(), "invalid index") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestProvider_Embed_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL, Timeout: 50 * time.Millisecond})
	_, err := provider.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "send request") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProvider_Embed_InvalidURL(t *testing.T) {
	provider := NewProvider(Config{BaseURL: "http://loc alhost:9997"})
	_, err := provider.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected create request error")
	}
	if !strings.Contains(err.Error(), "create request") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProvider_Embed_ReadBodyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("server does not support hijacking")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack failed: %v", err)
			return
		}
		// 声明 Content-Length 大于实际写入的 body，随后关闭连接，
		// 客户端读取 body 时会得到 unexpected EOF
		conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nshort"))
		conn.Close()
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	_, err := provider.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected read body error")
	}
	if !strings.Contains(err.Error(), "read response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProvider_Dimension(t *testing.T) {
	provider := NewProvider(Config{})
	if dim := provider.Dimension(); dim != 768 {
		t.Fatalf("expected dimension 768, got %d", dim)
	}
}

func TestProvider_HealthCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	if err := provider.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
}

func TestProvider_HealthCheck_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	err := provider.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error for failed health check")
	}
	if !strings.Contains(err.Error(), "health check failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProvider_HealthCheck_InvalidURL(t *testing.T) {
	provider := NewProvider(Config{BaseURL: "http://loc alhost:9997"})
	err := provider.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected create request error")
	}
	if !strings.Contains(err.Error(), "create request") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProvider_HealthCheck_ConnectionRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	baseURL := server.URL
	server.Close()

	provider := NewProvider(Config{BaseURL: baseURL})
	err := provider.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected send request error")
	}
	if !strings.Contains(err.Error(), "send request") {
		t.Fatalf("unexpected error: %v", err)
	}
}
