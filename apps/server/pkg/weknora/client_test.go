package weknora

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.BaseURL == "" {
		t.Error("expected BaseURL to be set")
	}
	if cfg.Timeout == 0 {
		t.Error("expected Timeout to be set")
	}
	if cfg.MaxRetries == 0 {
		t.Error("expected MaxRetries to be set")
	}
	if cfg.RetryDelay == 0 {
		t.Error("expected RetryDelay to be set")
	}
}

func TestNewClient(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tests := []struct {
		name    string
		config  *Config
		wantNil bool
	}{
		{
			name: "with valid config",
			config: &Config{
				BaseURL:    "http://localhost:9000",
				APIKey:     "test-key",
				TenantID:   "test-tenant",
				Timeout:    10 * time.Second,
				MaxRetries: 3,
			},
			wantNil: false,
		},
		{
			name:    "with nil config",
			config:  nil,
			wantNil: false,
		},
		{
			name:    "with empty config",
			config:  &Config{},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.config, logger)
			if (client == nil) != tt.wantNil {
				t.Errorf("NewClient() = %v, wantNil %v", client, tt.wantNil)
			}
			if client != nil {
				if client.httpClient == nil {
					t.Error("expected httpClient to be initialized")
				}
				if client.logger == nil {
					t.Error("expected logger to be initialized")
				}
				if client.config == nil {
					t.Error("expected config to be initialized")
				}
			}
		})
	}
}

func TestGetStats(t *testing.T) {
	config := &Config{
		BaseURL:    "http://test.example.com",
		APIKey:     "test-key",
		TenantID:   "test-tenant",
		Timeout:    10 * time.Second,
		MaxRetries: 3,
	}
	client := NewClient(config, nil)

	stats := client.GetStats()

	if stats["base_url"] != config.BaseURL {
		t.Errorf("expected base_url %s, got %v", config.BaseURL, stats["base_url"])
	}
	if stats["tenant_id"] != config.TenantID {
		t.Errorf("expected tenant_id %s, got %v", config.TenantID, stats["tenant_id"])
	}
	if stats["timeout"] != config.Timeout {
		t.Errorf("expected timeout %v, got %v", config.Timeout, stats["timeout"])
	}
	if stats["max_retries"] != config.MaxRetries {
		t.Errorf("expected max_retries %d, got %v", config.MaxRetries, stats["max_retries"])
	}
}

func TestSearchKnowledge_Validation(t *testing.T) {
	client := NewClient(nil, nil)

	tests := []struct {
		name       string
		req        *SearchRequest
		wantErrMsg string
	}{
		{
			name: "missing knowledge base ID",
			req: &SearchRequest{
				Query: "test query",
			},
			wantErrMsg: "knowledge base ID is required",
		},
		{
			name: "missing query",
			req: &SearchRequest{
				KnowledgeBaseID: "kb-123",
			},
			wantErrMsg: "query is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.SearchKnowledge(context.Background(), tt.req)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tt.wantErrMsg != "" {
				// 检查错误消息包含预期内容
				if err.Error() != tt.wantErrMsg {
					t.Errorf("expected error message %q, got %q", tt.wantErrMsg, err.Error())
				}
			}
		})
	}
}

func TestUploadDocument_Validation(t *testing.T) {
	client := NewClient(nil, nil)

	tests := []struct {
		name       string
		kbID       string
		doc        *Document
		wantErrMsg string
	}{
		{
			name: "missing knowledge base ID",
			doc: &Document{
				Title:   "Test Doc",
				Content: "Test content",
			},
			wantErrMsg: "knowledge base ID is required",
		},
		{
			name: "missing document title",
			kbID: "kb-123",
			doc: &Document{
				Content: "Test content",
			},
			wantErrMsg: "document title is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.UploadDocument(context.Background(), tt.kbID, tt.doc)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != tt.wantErrMsg {
				t.Errorf("expected error message %q, got %q", tt.wantErrMsg, err.Error())
			}
		})
	}
}

func TestCreateKnowledgeBase_Validation(t *testing.T) {
	client := NewClient(nil, nil)

	_, err := client.CreateKnowledgeBase(context.Background(), &CreateKBRequest{})
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
	if err.Error() != "knowledge base name is required" {
		t.Errorf("expected error message %q, got %q", "knowledge base name is required", err.Error())
	}
}

func TestGetKnowledgeBase_Validation(t *testing.T) {
	client := NewClient(nil, nil)

	_, err := client.GetKnowledgeBase(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty kbID, got nil")
	}
	if err.Error() != "knowledge base ID is required" {
		t.Errorf("expected error message %q, got %q", "knowledge base ID is required", err.Error())
	}
}

func TestCreateSession_Validation(t *testing.T) {
	client := NewClient(nil, nil)

	_, err := client.CreateSession(context.Background(), &SessionRequest{})
	if err == nil {
		t.Fatal("expected error for empty userID, got nil")
	}
	if err.Error() != "user ID is required" {
		t.Errorf("expected error message %q, got %q", "user ID is required", err.Error())
	}
}

func TestChat_Validation(t *testing.T) {
	client := NewClient(nil, nil)

	tests := []struct {
		name       string
		sessionID  string
		req        *ChatRequest
		wantErrMsg string
	}{
		{
			name: "missing session ID",
			req: &ChatRequest{
				Message: "test message",
			},
			wantErrMsg: "session ID is required",
		},
		{
			name:       "missing message",
			sessionID:  "session-123",
			req:        &ChatRequest{},
			wantErrMsg: "message is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Chat(context.Background(), tt.sessionID, tt.req)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != tt.wantErrMsg {
				t.Errorf("expected error message %q, got %q", tt.wantErrMsg, err.Error())
			}
		})
	}
}

func TestCreateRequest(t *testing.T) {
	client := NewClient(&Config{
		BaseURL:  "http://test.example.com",
		APIKey:   "test-key",
		TenantID: "test-tenant",
	}, nil)

	ctx := context.Background()

	t.Run("valid POST request with body", func(t *testing.T) {
		body := map[string]string{"test": "data"}
		req, err := client.createRequest(ctx, "POST", "/api/test", body)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if req.Method != "POST" {
			t.Errorf("expected method POST, got %s", req.Method)
		}
		if req.URL.String() != "http://test.example.com/api/test" {
			t.Errorf("expected URL http://test.example.com/api/test, got %s", req.URL.String())
		}
		if req.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", req.Header.Get("Content-Type"))
		}
		if req.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("expected X-API-Key test-key, got %s", req.Header.Get("X-API-Key"))
		}
		if req.Header.Get("X-Tenant-ID") != "test-tenant" {
			t.Errorf("expected X-Tenant-ID test-tenant, got %s", req.Header.Get("X-Tenant-ID"))
		}
		if req.Header.Get("User-Agent") != "Servify-WeKnora-Client/1.0" {
			t.Errorf("expected User-Agent Servify-WeKnora-Client/1.0, got %s", req.Header.Get("User-Agent"))
		}
	})

	t.Run("valid GET request without body", func(t *testing.T) {
		req, err := client.createRequest(ctx, "GET", "/api/test", nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if req.Method != "GET" {
			t.Errorf("expected method GET, got %s", req.Method)
		}
		if req.Body != nil {
			t.Error("expected nil body for GET request")
		}
	})
}

func TestShouldRetry(t *testing.T) {
	client := NewClient(nil, nil)

	err := &testError{}
	result := client.shouldRetry(err)

	if !result {
		t.Error("expected shouldRetry to return true for custom error")
	}
}

func TestHealthCheck_Success(t *testing.T) {
	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","version":"1.0.0"}`))
	}))
	defer server.Close()

	client := NewClient(&Config{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	}, nil)

	err := client.HealthCheck(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHealthCheck_Unhealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"unhealthy","version":"1.0.0"}`))
	}))
	defer server.Close()

	client := NewClient(&Config{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	}, nil)

	err := client.HealthCheck(context.Background())
	if err == nil {
		t.Error("expected error for unhealthy status")
	}
}

// 测试辅助类型
type testError struct{}

func (e *testError) Error() string {
	return "test error"
}

func TestSearchKnowledge_DefaultValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证默认值被设置
		var req SearchRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Limit != 5 {
			t.Errorf("expected default Limit 5, got %d", req.Limit)
		}
		if req.Threshold != 0.7 {
			t.Errorf("expected default Threshold 0.7, got %f", req.Threshold)
		}
		if req.Strategy != "hybrid" {
			t.Errorf("expected default Strategy hybrid, got %s", req.Strategy)
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
		Limit:           0,  // 使用默认值
		Threshold:       0,  // 使用默认值
		Strategy:        "", // 使用默认值
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
