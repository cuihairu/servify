package tei

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProvider_Embed_Single(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embed" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		// 返回模拟的 512 维向量
		response := map[string][][]float32{
			"embeddings": {
				make([]float32, 512),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	ctx := context.Background()
	vectors, err := provider.Embed(ctx, []string{"test text"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(vectors) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vectors))
	}

	if len(vectors[0]) != 512 {
		t.Fatalf("expected dimension 512, got %d", len(vectors[0]))
	}
}

func TestProvider_Embed_Multiple(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string][][]float32{
			"embeddings": {
				make([]float32, 512),
				make([]float32, 512),
				make([]float32, 512),
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	ctx := context.Background()
	vectors, err := provider.Embed(ctx, []string{"one", "two", "three"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(vectors) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(vectors))
	}
}

func TestProvider_Embed_EmptyInput(t *testing.T) {
	provider := NewProvider(Config{})
	ctx := context.Background()
	_, err := provider.Embed(ctx, []string{})
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestProvider_Embed_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	ctx := context.Background()
	_, err := provider.Embed(ctx, []string{"test"})
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestProvider_Dimension(t *testing.T) {
	tests := []struct {
		model     string
		dimension int
	}{
		{"bge-small-zh-v1.5", 512},
		{"bge-base-zh-v1.5", 768},
		{"bge-large-zh-v1.5", 1024},
		{"unknown", 512}, // 默认
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	ctx := context.Background()
	if err := provider.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
}

func TestProvider_HealthCheck_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	ctx := context.Background()
	if err := provider.HealthCheck(ctx); err == nil {
		t.Fatal("expected error for failed health check")
	}
}

func TestProvider_Embed_UnmarshalError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not-json"))
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	if _, err := provider.Embed(context.Background(), []string{"t"}); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestProvider_Embed_CountMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string][][]float32{
			"embeddings": {make([]float32, 4)},
		})
	}))
	defer server.Close()

	provider := NewProvider(Config{BaseURL: server.URL})
	if _, err := provider.Embed(context.Background(), []string{"a", "b"}); err == nil {
		t.Fatal("expected count mismatch error")
	}
}

func TestProvider_Embed_ConnectionRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	provider := NewProvider(Config{BaseURL: url})
	if _, err := provider.Embed(context.Background(), []string{"t"}); err == nil {
		t.Fatal("expected connection error")
	}
}

func TestProvider_Embed_CanceledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string][][]float32{"embeddings": {make([]float32, 4)}})
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provider := NewProvider(Config{BaseURL: server.URL})
	if _, err := provider.Embed(ctx, []string{"t"}); err == nil {
		t.Fatal("expected context canceled error")
	}
}

func TestProvider_HealthCheck_ConnectionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	provider := NewProvider(Config{BaseURL: url})
	if err := provider.HealthCheck(context.Background()); err == nil {
		t.Fatal("expected connection error")
	}
}

func TestNewProviderDefaults(t *testing.T) {
	p := NewProvider(Config{})
	if p.config.BaseURL != "http://localhost:8080" {
		t.Fatalf("default base url = %q", p.config.BaseURL)
	}
	if p.config.Timeout != 30*time.Second {
		t.Fatalf("default timeout = %v", p.config.Timeout)
	}
}

type erroringReadCloser struct{}

func (erroringReadCloser) Read(_ []byte) (int, error) { return 0, errors.New("read body failed") }
func (erroringReadCloser) Close() error               { return nil }

type stubRoundTripper struct {
	resp *http.Response
	err  error
}

func (s *stubRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	return s.resp, s.err
}

func TestProvider_Embed_BadURL(t *testing.T) {
	p := NewProvider(Config{BaseURL: "http://bad\x7furl:%%"})
	if _, err := p.Embed(context.Background(), []string{"t"}); err == nil {
		t.Fatal("expected create request error")
	}
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Fatal("expected health check create request error")
	}
}

func TestProvider_Embed_ReadBodyError(t *testing.T) {
	p := NewProvider(Config{BaseURL: "http://teistub.local"})
	p.client = &http.Client{Transport: &stubRoundTripper{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Body:       erroringReadCloser{},
			Header:     http.Header{},
		},
	}}
	if _, err := p.Embed(context.Background(), []string{"t"}); err == nil {
		t.Fatal("expected read body error")
	}
}
