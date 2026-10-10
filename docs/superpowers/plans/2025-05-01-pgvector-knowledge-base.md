# 基于 pgvector 的自建知识库实施计划

> **状态（2026-10-10 注）：已被取代。** 本计划的 pgvector 自建 provider 与 openai/tei embedding 验收路径已由 local 自含知识引擎（`scripts/test-local-knowledge-acceptance.sh`，`make local-knowledge-acceptance`）及各 provider 验收脚本（ragflow/dify/weknora/pgvector）取代；`test-knowledge-acceptance.sh` 已删除，本计划保留作历史记录。

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现基于 pgvector 的自建知识库，支持 Embedding 服务抽象层和向量检索，作为默认知识库 provider

**Architecture:**
- 新增 EmbeddingProvider 接口，支持 OpenAI/TEI/Xinference 等多种 embedding provider
- 实现 PgvectorProvider，使用 pgvector 扩展进行向量存储和相似度检索
- 扩展配置系统，支持 embedding 和 knowledge provider 配置
- 保留现有 Dify/WeKnora provider 作为可选项

**Tech Stack:** Go, GORM, pgvector, OpenAI API, TEI (Text Embeddings Inference)

---

## 文件结构

### 新增文件
```
apps/server/internal/platform/embedding/
├── provider.go                 # EmbeddingProvider 接口定义
├── factory.go                  # 根据 config 创建 provider
├── openai/
│   ├── provider.go             # OpenAI Embedding 实现
│   └── provider_test.go
├── tei/
│   ├── provider.go             # TEI Embedding 实现
│   └── provider_test.go
└── xinference/
    ├── provider.go             # Xinference Embedding 实现
    └── provider_test.go

apps/server/internal/platform/knowledgeprovider/pgvector/
├── provider.go                 # PgvectorProvider 主实现
├── provider_test.go            # 集成测试
├── chunking.go                 # 文档分块逻辑
└── search.go                   # 向量检索逻辑

scripts/
└── test-knowledge-acceptance.sh  # 知识库验收脚本
```

### 修改文件
```
apps/server/internal/config/config.go           # 添加 EmbeddingConfig, KnowledgeConfig
apps/server/internal/models/models.go           # 扩展 KnowledgeDoc
apps/server/internal/app/bootstrap/app.go       # 添加 EmbeddingProvider 初始化
apps/server/internal/modules/knowledge/application/service.go  # 支持新 provider
scripts/init-db.sql                              # 添加向量列和索引
config.yml                                      # 添加配置示例
```

---

## Phase 1: Embedding 抽象层

### Task 1: 定义 EmbeddingProvider 接口

**Files:**
- Create: `apps/server/internal/platform/embedding/provider.go`
- Test: `apps/server/internal/platform/embedding/provider_test.go`

- [ ] **Step 1: 创建接口定义文件**

```go
// apps/server/internal/platform/embedding/provider.go
package embedding

import "context"

// Provider 定义文本嵌入服务接口
type Provider interface {
	// Embed 返回文本的向量表示
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// Dimension 返回向量维度
	Dimension() int

	// HealthCheck 健康检查
	HealthCheck(ctx context.Context) error
}

// Config 是 embedding provider 的通用配置
type Config struct {
	Provider string `yaml:"provider" json:"provider"`
}
```

- [ ] **Step 2: 创建基础测试文件**

```go
// apps/server/internal/platform/embedding/provider_test.go
package embedding

import (
	"context"
	"testing"
)

// MockProvider 用于测试
type MockProvider struct {
	embedFunc    func(ctx context.Context, texts []string) ([][]float32, error)
	dimensionVal int
}

func (m *MockProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, texts)
	}
	// 返回固定维度零向量
	result := make([][]float32, len(texts))
	for i := range result {
		result[i] = make([]float32, m.Dimension())
	}
	return result, nil
}

func (m *MockProvider) Dimension() int {
	if m.dimensionVal > 0 {
		return m.dimensionVal
	}
	return 1536
}

func (m *MockProvider) HealthCheck(ctx context.Context) error {
	return nil
}

func TestMockProvider(t *testing.T) {
	ctx := context.Background()
	provider := &MockProvider{dimensionVal: 512}

	// Test Embed
	vectors, err := provider.Embed(ctx, []string{"test"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(vectors) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vectors))
	}
	if len(vectors[0]) != 512 {
		t.Fatalf("expected dimension 512, got %d", len(vectors[0]))
	}

	// Test Dimension
	if provider.Dimension() != 512 {
		t.Fatalf("expected dimension 512, got %d", provider.Dimension())
	}

	// Test HealthCheck
	if err := provider.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
}
```

- [ ] **Step 3: 运行测试验证**

```bash
cd apps/server
go test ./internal/platform/embedding -v
```

预期: PASS

- [ ] **Step 4: 提交**

```bash
git add apps/server/internal/platform/embedding/
git commit -m "feat(embedding): define EmbeddingProvider interface with mock implementation"
```

---

### Task 2: 实现 OpenAI Embedding Provider

**Files:**
- Create: `apps/server/internal/platform/embedding/openai/provider.go`
- Create: `apps/server/internal/platform/embedding/openai/provider_test.go`

- [ ] **Step 1: 创建 OpenAI Provider 实现**

```go
// apps/server/internal/platform/embedding/openai/provider.go
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Config struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
}

type Provider struct {
	config Config
	client *http.Client
}

type embedRequest struct {
	Input []string `json:"input"`
	Model string    `json:"model"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func NewProvider(cfg Config) *Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "text-embedding-3-small"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Provider{
		config: cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}
}

func (p *Provider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("no texts provided")
	}

	reqBody := embedRequest{
		Input: texts,
		Model: p.config.Model,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/embeddings", p.config.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai error: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var embedResp embedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if embedResp.Error != nil {
		return nil, fmt.Errorf("openai error: %s", embedResp.Error.Message)
	}

	if len(embedResp.Data) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(embedResp.Data))
	}

	// 按 index 排序
	result := make([][]float32, len(texts))
	for _, item := range embedResp.Data {
		if item.Index < 0 || item.Index >= len(texts) {
			return nil, fmt.Errorf("invalid index: %d", item.Index)
		}
		result[item.Index] = item.Embedding
	}

	return result, nil
}

func (p *Provider) Dimension() int {
	// text-embedding-3-small: 1536
	// text-embedding-3-large: 3072
	// text-embedding-ada-002: 1536
	switch p.config.Model {
	case "text-embedding-3-large":
		return 3072
	default:
		return 1536
	}
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	url := fmt.Sprintf("%s/models", p.config.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("health check failed: status %d", resp.StatusCode)
	}

	return nil
}
```

- [ ] **Step 2: 创建测试文件**

```go
// apps/server/internal/platform/embedding/openai/provider_test.go
package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestProvider_Embed(t *testing.T) {
	// 跳过测试如果没有 API key
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := NewProvider(Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
		Model:  "text-embedding-3-small",
	})

	ctx := context.Background()
	vectors, err := provider.Embed(ctx, []string{"hello world"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(vectors) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vectors))
	}

	if len(vectors[0]) != 1536 {
		t.Fatalf("expected dimension 1536, got %d", len(vectors[0]))
	}

	// 检查向量值不为零
	hasNonZero := false
	for _, v := range vectors[0] {
		if v != 0 {
			hasNonZero = true
			break
		}
	}
	if !hasNonZero {
		t.Fatal("embedding vector is all zeros")
	}
}

func TestProvider_Embed_Multiple(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	provider := NewProvider(Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})

	ctx := context.Background()
	vectors, err := provider.Embed(ctx, []string{"first", "second", "third"})
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

func TestProvider_Dimension(t *testing.T) {
	tests := []struct {
		model     string
		dimension int
	}{
		{"text-embedding-3-small", 1536},
		{"text-embedding-3-large", 3072},
		{"text-embedding-ada-002", 1536},
		{"unknown-model", 1536}, // 默认
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
	// 使用 mock server 测试
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := NewProvider(Config{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})

	ctx := context.Background()
	if err := provider.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
}

func TestProvider_HealthCheck_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	provider := NewProvider(Config{
		BaseURL: server.URL,
	})

	ctx := context.Background()
	if err := provider.HealthCheck(ctx); err == nil {
		t.Fatal("expected error for failed health check")
	}
}
```

- [ ] **Step 3: 运行测试**

```bash
cd apps/server
go test ./internal/platform/embedding/openai -v
```

预期: 跳过需要 API key 的测试，其他测试通过

- [ ] **Step 4: 提交**

```bash
git add apps/server/internal/platform/embedding/openai/
git commit -m "feat(embedding): implement OpenAI embedding provider"
```

---

### Task 3: 实现 TEI Embedding Provider

**Files:**
- Create: `apps/server/internal/platform/embedding/tei/provider.go`
- Create: `apps/server/internal/platform/embedding/tei/provider_test.go`

- [ ] **Step 1: 创建 TEI Provider 实现**

```go
// apps/server/internal/platform/embedding/tei/provider.go
package tei

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Config struct {
	BaseURL string
	Model   string
	Timeout time.Duration
}

type Provider struct {
	config Config
	client *http.Client
}

type embedRequest struct {
	Input *string  `json:"input,omitempty"` // 单个文本
	Inputs []string `json:"inputs,omitempty"` // 多个文本
	Truncate bool `json:"truncate,omitempty"`
}

type embedResponse struct {
	[][]float32 `json:"embeddings"`
}

func NewProvider(cfg Config) *Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:8080"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Provider{
		config: cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}
}

func (p *Provider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("no texts provided")
	}

	var reqBody embedRequest
	if len(texts) == 1 {
		reqBody.Input = &texts[0]
	} else {
		reqBody.Inputs = texts
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/embed", p.config.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("TEI error: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var embedResp embedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(embedResp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(embedResp.Embeddings))
	}

	return embedResp.Embeddings, nil
}

func (p *Provider) Dimension() int {
	// BGE 模型维度
	// bge-small-zh-v1.5: 512
	// bge-base-zh-v1.5: 768
	// bge-large-zh-v1.5: 1024
	switch p.config.Model {
	case "bge-base-zh-v1.5":
		return 768
	case "bge-large-zh-v1.5":
		return 1024
	default:
		return 512
	}
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	url := fmt.Sprintf("%s/health", p.config.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("health check failed: status %d", resp.StatusCode)
	}

	return nil
}
```

- [ ] **Step 2: 创建测试文件**

```go
// apps/server/internal/platform/embedding/tei/provider_test.go
package tei

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
```

- [ ] **Step 3: 运行测试**

```bash
cd apps/server
go test ./internal/platform/embedding/tei -v
```

预期: 全部通过

- [ ] **Step 4: 提交**

```bash
git add apps/server/internal/platform/embedding/tei/
git commit -m "feat(embedding): implement TEI embedding provider"
```

---

### Task 4: 实现 Xinference Embedding Provider

**Files:**
- Create: `apps/server/internal/platform/embedding/xinference/provider.go`
- Create: `apps/server/internal/platform/embedding/xinference/provider_test.go`

- [ ] **Step 1: 创建 Xinference Provider 实现**

```go
// apps/server/internal/platform/embedding/xinference/provider.go
package xinference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Config struct {
	BaseURL  string
	ModelUID string
	Timeout  time.Duration
}

type Provider struct {
	config Config
	client *http.Client
}

type embedRequest struct {
	Input []string `json:"input"`
	Model string    `json:"model"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
}

func NewProvider(cfg Config) *Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:9997"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Provider{
		config: cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}
}

func (p *Provider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("no texts provided")
	}

	reqBody := embedRequest{
		Input: texts,
		Model: p.config.ModelUID,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/embeddings", p.config.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("xinference error: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var embedResp embedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(embedResp.Data) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(embedResp.Data))
	}

	result := make([][]float32, len(texts))
	for i, item := range embedResp.Data {
		result[i] = item.Embedding
	}

	return result, nil
}

func (p *Provider) Dimension() int {
	// Xinference 支持多种模型，维度取决于具体模型
	// 这里返回一个常见默认值，实际应该从模型信息获取
	return 768
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	url := fmt.Sprintf("%s/v1/models", p.config.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("health check failed: status %d", resp.StatusCode)
	}

	return nil
}
```

- [ ] **Step 2: 创建测试文件**

```go
// apps/server/internal/platform/embedding/xinference/provider_test.go
package xinference

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProvider_Embed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		response := map[string]interface{}{
			"data": []map[string][]float32{
				{"embedding": make([]float32, 768)},
			},
			"model": "bge-small-zh",
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	provider := NewProvider(Config{
		BaseURL:  server.URL,
		ModelUID: "bge-small-zh",
	})
	ctx := context.Background()
	vectors, err := provider.Embed(ctx, []string{"test"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(vectors) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vectors))
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

func TestProvider_Dimension(t *testing.T) {
	provider := NewProvider(Config{})
	if dim := provider.Dimension(); dim != 768 {
		t.Fatalf("expected dimension 768, got %d", dim)
	}
}

func TestProvider_HealthCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []interface{}{},
		})
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
```

- [ ] **Step 3: 运行测试**

```bash
cd apps/server
go test ./internal/platform/embedding/xinference -v
```

预期: 全部通过

- [ ] **Step 4: 提交**

```bash
git add apps/server/internal/platform/embedding/xinference/
git commit -m "feat(embedding): implement Xinference embedding provider"
```

---

### Task 5: 实现 EmbeddingProvider Factory

**Files:**
- Create: `apps/server/internal/platform/embedding/factory.go`
- Create: `apps/server/internal/platform/embedding/factory_test.go`

- [ ] **Step 1: 创建 Factory**

```go
// apps/server/internal/platform/embedding/factory.go
package embedding

import (
	"fmt"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/embedding/openai"
	"servify/apps/server/internal/platform/embedding/tei"
	"servify/apps/server/internal/platform/embedding/xinference"
)

// NewProvider 根据配置创建对应的 EmbeddingProvider
func NewProvider(cfg config.EmbeddingConfig) (Provider, error) {
	switch cfg.Provider {
	case "openai":
		return openai.NewProvider(openai.Config{
			APIKey:  cfg.OpenAI.APIKey,
			BaseURL: cfg.OpenAI.BaseURL,
			Model:   cfg.OpenAI.Model,
		}), nil
	case "tei":
		return tei.NewProvider(tei.Config{
			BaseURL: cfg.TEI.BaseURL,
			Model:   cfg.TEI.Model,
		}), nil
	case "xinference":
		return xinference.NewProvider(xinference.Config{
			BaseURL:  cfg.Xinference.BaseURL,
			ModelUID: cfg.Xinference.ModelUID,
		}), nil
	default:
		return nil, fmt.Errorf("unknown embedding provider: %s", cfg.Provider)
	}
}
```

- [ ] **Step 2: 创建测试**

```go
// apps/server/internal/platform/embedding/factory_test.go
package embedding

import (
	"testing"

	"servify/apps/server/internal/config"
)

func TestNewProvider_OpenAI(t *testing.T) {
	cfg := config.EmbeddingConfig{
		Provider: "openai",
		OpenAI: config.OpenAIEmbedConfig{
			APIKey:  "test-key",
			BaseURL: "https://api.openai.com/v1",
			Model:   "text-embedding-3-small",
		},
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}

	if provider == nil {
		t.Fatal("expected non-nil provider")
	}

	if provider.Dimension() != 1536 {
		t.Fatalf("expected dimension 1536, got %d", provider.Dimension())
	}
}

func TestNewProvider_TEI(t *testing.T) {
	cfg := config.EmbeddingConfig{
		Provider: "tei",
		TEI: config.TEIEmbedConfig{
			BaseURL: "http://localhost:8080",
			Model:   "bge-small-zh-v1.5",
		},
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}

	if provider == nil {
		t.Fatal("expected non-nil provider")
	}

	if provider.Dimension() != 512 {
		t.Fatalf("expected dimension 512, got %d", provider.Dimension())
	}
}

func TestNewProvider_Xinference(t *testing.T) {
	cfg := config.EmbeddingConfig{
		Provider: "xinference",
		Xinference: config.XinferenceEmbedConfig{
			BaseURL:  "http://localhost:9997",
			ModelUID: "bge-small-zh",
		},
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}

	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewProvider_Unknown(t *testing.T) {
	cfg := config.EmbeddingConfig{
		Provider: "unknown",
	}

	_, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}
```

- [ ] **Step 3: 运行测试**

```bash
cd apps/server
go test ./internal/platform/embedding -v -run TestNewProvider
```

预期: 全部通过

- [ ] **Step 4: 提交**

```bash
git add apps/server/internal/platform/embedding/factory.go
git add apps/server/internal/platform/embedding/factory_test.go
git commit -m "feat(embedding): add factory for creating embedding providers"
```

---

## Phase 2: 配置扩展

### Task 6: 扩展配置结构

**Files:**
- Modify: `apps/server/internal/config/config.go`

- [ ] **Step 1: 在现有 import 中添加 pgvector 包引用**

在文件开头的 import 区域确保有：
```go
import (
	// ... 现有 imports
)
```

注意：pgvector.Vector 类型将在模型中使用，这里不需要特别导入

- [ ] **Step 2: 在 Config 结构体中添加新字段**

找到 `type Config struct` 定义，在 `Fallback` 字段后添加：

```go
type Config struct {
	Server     ServerConfig     `yaml:"server"`
	EventBus   EventBusConfig   `yaml:"event_bus"`
	Database   DatabaseConfig   `yaml:"database"`
	Redis      RedisConfig      `yaml:"redis"`
	WebRTC     WebRTCConfig     `yaml:"webrtc"`
	Voice      VoiceConfig      `yaml:"voice"`
	AI         AIConfig         `yaml:"ai"`
	Dify       DifyConfig       `yaml:"dify"`
	WeKnora    WeKnoraConfig    `yaml:"weknora"`
	Fallback   FallbackConfig   `yaml:"fallback"`

	// 新增字段
	Embedding  EmbeddingConfig   `yaml:"embedding"`
	Knowledge  KnowledgeConfig   `yaml:"knowledge"`

	JWT        JWTConfig         `yaml:"jwt"`
	Log        LogConfig         `yaml:"log"`
	// ... 其他字段
}
```

- [ ] **Step 3: 在文件末尾添加新的配置结构**

在 `config.go` 文件末尾（最后一个 `type` 定义之后）添加：

```go
// EmbeddingConfig 是文本嵌入服务配置
type EmbeddingConfig struct {
	Provider   string                 `yaml:"provider" json:"provider,omitempty"`
	OpenAI     OpenAIEmbedConfig      `yaml:"openai" json:"openai,omitempty"`
	TEI        TEIEmbedConfig         `yaml:"tei" json:"tei,omitempty"`
	Xinference XinferenceEmbedConfig  `yaml:"xinference" json:"xinference,omitempty"`
}

// OpenAIEmbedConfig 是 OpenAI Embedding 配置
type OpenAIEmbedConfig struct {
	APIKey  string `yaml:"api_key" json:"api_key,omitempty"`
	BaseURL string `yaml:"base_url" json:"base_url,omitempty"`
	Model   string `yaml:"model" json:"model,omitempty"`
}

// TEIEmbedConfig 是 TEI Embedding 配置
type TEIEmbedConfig struct {
	BaseURL string `yaml:"base_url" json:"base_url,omitempty"`
	Model   string `yaml:"model" json:"model,omitempty"`
}

// XinferenceEmbedConfig 是 Xinference Embedding 配置
type XinferenceEmbedConfig struct {
	BaseURL  string `yaml:"base_url" json:"base_url,omitempty"`
	ModelUID string `yaml:"model_uid" json:"model_uid,omitempty"`
}

// KnowledgeConfig 是知识库配置
type KnowledgeConfig struct {
	Provider  string         `yaml:"provider" json:"provider,omitempty"`
	Pgvector  PgvectorConfig `yaml:"pgvector" json:"pgvector,omitempty"`
}

// PgvectorConfig 是 pgvector 知识库配置
type PgvectorConfig struct {
	Search   SearchConfig   `yaml:"search" json:"search,omitempty"`
	Indexing IndexingConfig `yaml:"indexing" json:"indexing,omitempty"`
}

// SearchConfig 是向量搜索配置
type SearchConfig struct {
	TopK      int     `yaml:"top_k" json:"top_k,omitempty"`
	Threshold float64 `yaml:"threshold" json:"threshold,omitempty"`
	Strategy  string  `yaml:"strategy" json:"strategy,omitempty"`
}

// IndexingConfig 是索引配置
type IndexingConfig struct {
	ChunkSize    int `yaml:"chunk_size" json:"chunk_size,omitempty"`
	ChunkOverlap int `yaml:"chunk_overlap" json:"chunk_overlap,omitempty"`
}
```

- [ ] **Step 4: 创建配置测试**

```go
// apps/server/internal/config/embedding_test.go
package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestEmbeddingConfig_DefaultValues(t *testing.T) {
	cfg := EmbeddingConfig{}

	if cfg.Search.TopK != 0 {
		t.Fatalf("expected default TopK 0, got %d", cfg.Search.TopK)
	}
}

func TestKnowledgeConfig_DefaultValues(t *testing.T) {
	cfg := KnowledgeConfig{}

	if cfg.Provider != "" {
		t.Fatalf("expected empty provider, got %s", cfg.Provider)
	}
}
```

- [ ] **Step 5: 运行测试**

```bash
cd apps/server
go test ./internal/config -v -run TestEmbeddingConfig -run TestKnowledgeConfig
```

预期: 通过

- [ ] **Step 6: 更新 config.yml 添加配置示例**

在 `config.yml` 文件中添加（在 `ai` 配置之后）：

```yaml
# Embedding 配置 - 文本向量化服务
embedding:
  provider: "openai"  # openai, tei, xinference
  openai:
    api_key: "${OPENAI_API_KEY}"
    base_url: "https://api.openai.com/v1"
    model: "text-embedding-3-small"  # 1536 维
  tei:
    base_url: "http://localhost:8080"
    model: "BAAI/bge-small-zh-v1.5"  # 512 维
  xinference:
    base_url: "http://localhost:9997"
    model_uid: "bge-small-zh"

# 知识库配置
knowledge:
  provider: "pgvector"  # pgvector (默认), dify, weknora
  pgvector:
    search:
      top_k: 5
      threshold: 0.7
      strategy: "cosine"  # cosine, euclidean
    indexing:
      chunk_size: 500
      chunk_overlap: 50
```

- [ ] **Step 7: 验证配置加载**

```bash
cd apps/server
go run ./cmd/server --help 2>&1 | head -5
```

预期: 正常显示帮助信息，说明配置解析无错误

- [ ] **Step 8: 提交**

```bash
git add apps/server/internal/config/config.go
git add apps/server/internal/config/embedding_test.go
git add config.yml
git commit -m "feat(config): add EmbeddingConfig and KnowledgeConfig"
```

---

## Phase 3: 数据库层

### Task 7: 扩展数据库模型

**Files:**
- Modify: `apps/server/internal/models/models.go`

- [ ] **Step 1: 扩展 KnowledgeDoc 模型**

找到 `type KnowledgeDoc struct` 定义，添加新字段：

```go
// 知识库文档
type KnowledgeDoc struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TenantID    string    `gorm:"index" json:"tenant_id"`
	WorkspaceID string    `gorm:"index" json:"workspace_id"`
	ProviderID  string    `gorm:"index" json:"provider_id"`
	ExternalID  string    `gorm:"index" json:"external_id"`
	Title       string    `json:"title"`
	Content     string    `gorm:"type:text" json:"content"`
	Category    string    `json:"category"`
	Tags        string    `json:"tags"`
	IsPublic    bool      `gorm:"default:false;index" json:"is_public"`

	// 新增字段 - pgvector 支持
	Embedding   pgvector.Vector `gorm:"type:vector(1536)" json:"embedding,omitempty"`
	ChunkIndex  int             `gorm:"default:0" json:"chunk_index,omitempty"`
	DocChunkID  string          `gorm:"index" json:"doc_chunk_id,omitempty"`

	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
```

注意：如果 `pgvector.Vector` 类型未定义，使用 `[]float32` 代替：

```go
Embedding   []float32 `gorm:"type:vector(1536)" json:"embedding,omitempty"`
```

- [ ] **Step 2: 创建模型测试**

```go
// apps/server/internal/models/knowledge_doc_test.go
package models

import (
	"testing"
	"time"
)

func TestKnowledgeDoc_VectorFields(t *testing.T) {
	doc := KnowledgeDoc{
		Title:      "Test Document",
		Content:    "Test content",
		Category:   "test",
		IsPublic:   true,
		Embedding:  make([]float32, 1536),
		ChunkIndex: 0,
		DocChunkID: "test-chunk-0",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if doc.Title != "Test Document" {
		t.Errorf("expected title 'Test Document', got %s", doc.Title)
	}

	if len(doc.Embedding) != 1536 {
		t.Errorf("expected embedding dimension 1536, got %d", len(doc.Embedding))
	}

	if doc.ChunkIndex != 0 {
		t.Errorf("expected chunk index 0, got %d", doc.ChunkIndex)
	}

	if doc.DocChunkID != "test-chunk-0" {
		t.Errorf("expected doc chunk id 'test-chunk-0', got %s", doc.DocChunkID)
	}
}

func TestKnowledgeDoc_EmptyVector(t *testing.T) {
	doc := KnowledgeDoc{
		Title:     "Test",
		Content:   "Content",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if doc.Embedding != nil {
		t.Errorf("expected nil embedding, got %v", doc.Embedding)
	}
}
```

- [ ] **Step 3: 运行测试**

```bash
cd apps/server
go test ./internal/models -v -run TestKnowledgeDoc
```

预期: 通过

- [ ] **Step 4: 提交**

```bash
git add apps/server/internal/models/models.go
git add apps/server/internal/models/knowledge_doc_test.go
git commit -m "feat(models): extend KnowledgeDoc with pgvector support"
```

---

### Task 8: 创建数据库迁移脚本

**Files:**
- Modify: `scripts/init-db.sql`

- [ ] **Step 1: 在 init-db.sql 末尾添加向量列**

在 `scripts/init-db.sql` 文件末尾添加：

```sql
-- 扩展 knowledge_docs 表以支持 pgvector
ALTER TABLE knowledge_docs
ADD COLUMN IF NOT EXISTS embedding vector(1536),
ADD COLUMN IF NOT EXISTS chunk_index int DEFAULT 0,
ADD COLUMN IF NOT EXISTS doc_chunk_id varchar(255);

-- 创建向量索引（使用 ivfflat）
CREATE INDEX IF NOT EXISTS idx_knowledge_docs_embedding
ON knowledge_docs
USING ivfflat (embedding vector_cosine_ops)
WITH (lists = 100);

-- 为 doc_chunk_id 创建索引以优化 chunk 查询
CREATE INDEX IF NOT EXISTS idx_knowledge_docs_doc_chunk_id
ON knowledge_docs(doc_chunk_id);

-- 添加注释
COMMENT ON COLUMN knowledge_docs.embedding IS '文档的向量表示，用于语义搜索';
COMMENT ON COLUMN knowledge_docs.chunk_index IS '文档分块索引，同一文档的不同分块有相同 doc_chunk_id';
COMMENT ON COLUMN knowledge_docs.doc_chunk_id IS '文档块 ID，用于标识属于同一文档的不同分块';
```

- [ ] **Step 2: 验证 SQL 语法**

```bash
cat scripts/init-db.sql | grep -A 20 "扩展 knowledge_docs"
```

预期: 显示新添加的 SQL 语句

- [ ] **Step 3: 提交**

```bash
git add scripts/init-db.sql
git commit -m "feat(db): add pgvector support to knowledge_docs table"
```

---

## Phase 4: PgvectorProvider 实现

### Task 9: 实现文档分块逻辑

**Files:**
- Create: `apps/server/internal/platform/knowledgeprovider/pgvector/chunking.go`
- Create: `apps/server/internal/platform/knowledgeprovider/pgvector/chunking_test.go`

- [ ] **Step 1: 实现分块逻辑**

```go
// apps/server/internal/platform/knowledgeprovider/pgvector/chunking.go
package pgvector

import (
	"strings"
	"unicode"
)

// Chunker 负责将长文档分割成适合嵌入的小块
type Chunker struct {
	ChunkSize    int
	ChunkOverlap int
}

// NewChunker 创建新的 Chunker
func NewChunker(chunkSize, chunkOverlap int) *Chunker {
	if chunkSize <= 0 {
		chunkSize = 500
	}
	if chunkOverlap < 0 {
		chunkOverlap = 50
	}
	if chunkOverlap >= chunkSize {
		chunkOverlap = chunkSize / 10
	}
	return &Chunker{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
	}
}

// Chunk 将文本分割成多个块
func (c *Chunker) Chunk(text string) []string {
	if len(text) <= c.ChunkSize {
		return []string{text}
	}

	var chunks []string
	runes := []rune(text)
	step := c.ChunkSize - c.ChunkOverlap

	if step <= 0 {
		step = 1
	}

	for i := 0; i < len(runes); i += step {
		end := i + c.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}

		chunk := string(runes[i:end])
		chunk = strings.TrimSpace(chunk)
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		if end >= len(runes) {
			break
		}
	}

	return chunks
}

// ChunkByParagraph 按段落分割文本
func (c *Chunker) ChunkByParagraph(text string) []string {
	paragraphs := strings.Split(text, "\n\n")
	var chunks []string
	var currentChunk strings.Builder

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		if currentChunk.Len() + len(para) + 1 > c.ChunkSize {
			if currentChunk.Len() > 0 {
				chunks = append(chunks, strings.TrimSpace(currentChunk.String()))
				currentChunk.Reset()
			}

			// 如果单个段落超过 chunk 大小，需要进一步分割
			if len(para) > c.ChunkSize {
				subChunks := c.Chunk(para)
				chunks = append(chunks, subChunks...)
				continue
			}
		}

		if currentChunk.Len() > 0 {
			currentChunk.WriteString("\n\n")
		}
		currentChunk.WriteString(para)
	}

	if currentChunk.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(currentChunk.String()))
	}

	return chunks
}

// CountTokens 估算文本的 token 数（粗略按字符/单词计算）
func CountTokens(text string) int {
	// 简单估算：中文按字符计算，英文按单词计算
	runes := []rune(text)
	var count int

	for _, r := range runes {
		if unicode.Is(unicode.Han, r) {
			count++ // 中文字符
		} else if unicode.IsSpace(r) {
			// 空格不计入
		} else {
			count++ // 其他字符
		}
	}

	// 英文大约 4 字符 = 1 token
	if count < len(runes)*4/10 {
		return len(runes) / 4
	}
	return count
}
```

- [ ] **Step 2: 创建测试**

```go
// apps/server/internal/platform/knowledgeprovider/pgvector/chunking_test.go
package pgvector

import (
	"testing"
)

func TestChunker_Chunk_ShortText(t *testing.T) {
	chunker := NewChunker(500, 50)
	text := "This is a short text."

	chunks := chunker.Chunk(text)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}

	if chunks[0] != text {
		t.Errorf("expected chunk '%s', got '%s'", text, chunks[0])
	}
}

func TestChunker_Chunk_LongText(t *testing.T) {
	chunker := NewChunker(100, 20)
	text := string(make([]byte, 300)) // 300 字节

	chunks := chunker.Chunk(text)

	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}

	// 验证每个块的大小
	for i, chunk := range chunks {
		if len(chunk) > 100 {
			t.Errorf("chunk %d exceeds max size: %d", i, len(chunk))
		}
	}
}

func TestChunker_Chunk_ExactMultiple(t *testing.T) {
	chunker := NewChunker(10, 2)
	text := "ABCDEFGHIJ" // 正好 10 个字符

	chunks := chunker.Chunk(text)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}

	if chunks[0] != text {
		t.Errorf("expected '%s', got '%s'", text, chunks[0])
	}
}

func TestChunker_ChunkByParagraph(t *testing.T) {
	chunker := NewChunker(100, 20)
	text := "First paragraph.\n\nSecond paragraph.\n\nThird paragraph."

	chunks := chunker.ChunkByParagraph(text)

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
}

func TestChunker_ChunkByParagraph_SingleParagraph(t *testing.T) {
	chunker := NewChunker(100, 20)
	text := "Single paragraph."

	chunks := chunker.ChunkByParagraph(text)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestChunker_ChunkByParagraph_EmptyParagraphs(t *testing.T) {
	chunker := NewChunker(100, 20)
	text := "First.\n\n\n\nSecond." // 多个空行

	chunks := chunker.ChunkByParagraph(text)

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
}

func TestChunker_NewChunker_Defaults(t *testing.T) {
	chunker := NewChunker(0, -1)

	if chunker.ChunkSize != 500 {
		t.Errorf("expected default ChunkSize 500, got %d", chunker.ChunkSize)
	}

	if chunker.ChunkOverlap != 50 {
		t.Errorf("expected default ChunkOverlap 50, got %d", chunker.ChunkOverlap)
	}
}

func TestChunker_NewChunker_OverlapTooLarge(t *testing.T) {
	chunker := NewChunker(100, 100) // overlap = size

	if chunker.ChunkOverlap >= chunker.ChunkSize {
		t.Errorf("ChunkOverlap should be less than ChunkSize")
	}
}

func TestCountTokens(t *testing.T) {
	tests := []struct {
		name string
		text string
		min  int
		max  int
	}{
		{"empty", "", 0, 1},
		{"short", "Hello world", 2, 5},
		{"chinese", "你好世界", 2, 5},
		{"mixed", "Hello 世界", 3, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := CountTokens(tt.text)
			if count < tt.min || count > tt.max {
				t.Errorf("token count %d outside range [%d, %d]", count, tt.min, tt.max)
			}
		})
	}
}
```

- [ ] **Step 3: 运行测试**

```bash
cd apps/server
go test ./internal/platform/knowledgeprovider/pgvector -v -run TestChunker
```

预期: 全部通过

- [ ] **Step 4: 提交**

```bash
git add apps/server/internal/platform/knowledgeprovider/pgvector/chunking.go
git add apps/server/internal/platform/knowledgeprovider/pgvector/chunking_test.go
git commit -m "feat(pgvector): implement document chunking logic"
```

---

### Task 10: 实现向量检索逻辑

**Files:**
- Create: `apps/server/internal/platform/knowledgeprovider/pgvector/search.go`
- Create: `apps/server/internal/platform/knowledgeprovider/pgvector/search_test.go`

- [ ] **Step 1: 实现检索逻辑**

```go
// apps/server/internal/platform/knowledgeprovider/pgvector/search.go
package pgvector

import (
	"fmt"
)

// CosineDistance 计算两个向量之间的余弦距离
// 距离 = 1 - 余弦相似度，越小越相似
func CosineDistance(a, b []float32) float32 {
	if len(a) != len(b) {
		return 1 // 不匹配时返回最大距离
	}

	var dotProduct float32
	var normA float32
	var normB float32

	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 1
	}

	return 1 - (dotProduct / (sqrt32(normA) * sqrt32(normB)))
}

// EuclideanDistance 计算欧几里得距离
func EuclideanDistance(a, b []float32) float32 {
	if len(a) != len(b) {
		return float32(1<<30) // 大数值
	}

	var sum float32
	for i := range a {
		diff := a[i] - b[i]
		sum += diff * diff
	}

	return sqrt32(sum)
}

// sqrt32 计算 float32 的平方根
func sqrt32(x float32) float32 {
	return float32(sqrt(float64(x)))
}

func sqrt(x float64) float64 {
	// 简单的牛顿迭代法
	if x == 0 {
		return 0
	}
	z := 1.0
	for i := 0; i < 10; i++ {
		z -= (z*z - x) / (2 * z)
	}
	return z
}

// BuildCosineSearchSQL 构建 pgvector 余弦相似度搜索 SQL
func BuildCosineSearchSQL(tableName string, dimension int) string {
	return fmt.Sprintf(`
		SELECT id, title, content, category, tags, doc_chunk_id,
		       1 - (embedding <=> $1::vector) as similarity
		FROM %s
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1::vector
		LIMIT $2
	`, tableName)
}

// BuildEuclideanSearchSQL 构建 pgvector 欧几里得距离搜索 SQL
func BuildEuclideanSearchSQL(tableName string) string {
	return fmt.Sprintf(`
		SELECT id, title, content, category, tags, doc_chunk_id,
		       (embedding <-> $1::vector) as distance
		FROM %s
		WHERE embedding IS NOT NULL
		ORDER BY embedding <-> $1::vector
		LIMIT $2
	`, tableName)
}

// FormatVector 将向量转换为 pgvector 格式字符串
func FormatVector(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}

	result := "["
	for i, val := range v {
		if i > 0 {
			result += ","
		}
		result += fmt.Sprintf("%f", val)
	}
	result += "]"
	return result
}

// NormalizeVector L2 归一化向量
func NormalizeVector(v []float32) []float32 {
	if len(v) == 0 {
		return v
	}

	var norm float32
	for _, val := range v {
		norm += val * val
	}
	norm = sqrt32(norm)

	if norm == 0 {
		return v
	}

	result := make([]float32, len(v))
	for i, val := range v {
		result[i] = val / norm
	}
	return result
}
```

- [ ] **Step 2: 创建测试**

```go
// apps/server/internal/platform/knowledgeprovider/pgvector/search_test.go
package pgvector

import (
	"testing"
)

func TestCosineDistance(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 2, 3},
			b:        []float32{1, 2, 3},
			expected: 0,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0, 1, 0},
			expected: 1,
		},
		{
			name:     "different lengths",
			a:        []float32{1, 2},
			b:        []float32{1, 2, 3},
			expected: 1,
		},
		{
			name:     "empty vectors",
			a:        []float32{},
			b:        []float32{},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CosineDistance(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("expected %f, got %f", tt.expected, result)
			}
		})
	}
}

func TestCosineDistance_SimilarVectors(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1.1, 2.1, 3.1}

	distance := CosineDistance(a, b)
	if distance < 0 || distance > 0.1 {
		t.Errorf("similar vectors should have small distance, got %f", distance)
	}
}

func TestEuclideanDistance(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 2, 3},
			b:        []float32{1, 2, 3},
			expected: 0,
		},
		{
			name:     "different vectors",
			a:        []float32{0, 0},
			b:        []float32{3, 4},
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EuclideanDistance(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("expected %f, got %f", tt.expected, result)
			}
		})
	}
}

func TestNormalizeVector(t *testing.T) {
	v := []float32{3, 4}
	normalized := NormalizeVector(v)

	// L2 norm should be 1
	var sum float32
	for _, val := range normalized {
		sum += val * val
	}

	norm := sqrt32(sum)
	if norm < 0.999 || norm > 1.001 {
		t.Errorf("normalized vector should have unit norm, got %f", norm)
	}
}

func TestNormalizeVector_Empty(t *testing.T) {
	v := []float32{}
	normalized := NormalizeVector(v)

	if len(normalized) != 0 {
		t.Errorf("empty vector should remain empty")
	}
}

func TestFormatVector(t *testing.T) {
	v := []float32{1.5, 2.5, 3.5}
	result := FormatVector(v)

	expected := "[1.500000,2.500000,3.500000]"
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

func TestFormatVector_Empty(t *testing.T) {
	v := []float32{}
	result := FormatVector(v)

	expected := "[]"
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

func TestBuildCosineSearchSQL(t *testing.T) {
	sql := BuildCosineSearchSQL("knowledge_docs", 1536)

	if sql == "" {
		t.Error("SQL should not be empty")
	}

	// 检查关键字
	if sql == "" || !contains(sql, "knowledge_docs") || !contains(sql, "<=>") {
		t.Error("SQL should contain table name and cosine operator")
	}
}

func TestBuildEuclideanSearchSQL(t *testing.T) {
	sql := BuildEuclideanSearchSQL("knowledge_docs")

	if sql == "" {
		t.Error("SQL should not be empty")
	}

	if !contains(sql, "<->") {
		t.Error("SQL should contain euclidean operator")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && (s[:1] == substr[:1] || contains(s[1:], substr))))
}
```

- [ ] **Step 3: 运行测试**

```bash
cd apps/server
go test ./internal/platform/knowledgeprovider/pgvector -v -run "TestCosine|TestEuclidean|TestNormalize|TestFormat|TestBuild"
```

预期: 全部通过

- [ ] **Step 4: 提交**

```bash
git add apps/server/internal/platform/knowledgeprovider/pgvector/search.go
git add apps/server/internal/platform/knowledgeprovider/pgvector/search_test.go
git commit -m "feat(pgvector): implement vector search utilities"
```

---

### Task 11: 实现 PgvectorProvider 主逻辑

**Files:**
- Create: `apps/server/internal/platform/knowledgeprovider/pgvector/provider.go`
- Create: `apps/server/internal/platform/knowledgeprovider/pgvector/provider_test.go`

- [ ] **Step 1: 实现 Provider**

```go
// apps/server/internal/platform/knowledgeprovider/pgvector/provider.go
package pgvector

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/platform/embedding"
	"servify/apps/server/internal/platform/knowledgeprovider"
)

type Config struct {
	Search   SearchConfig
	Indexing IndexingConfig
}

type SearchConfig struct {
	TopK      int
	Threshold float64
	Strategy  string // "cosine" or "euclidean"
}

type IndexingConfig struct {
	ChunkSize    int
	ChunkOverlap int
}

type Provider struct {
	db        *gorm.DB
	embedding embedding.Provider
	config    Config
	chunker   *Chunker
}

func NewProvider(db *gorm.DB, emb embedding.Provider, cfg Config) *Provider {
	return &Provider{
		db:        db,
		embedding: emb,
		config:    cfg,
		chunker:   NewChunker(cfg.Indexing.ChunkSize, cfg.Indexing.ChunkOverlap),
	}
}

func (p *Provider) Search(ctx context.Context, req knowledgeprovider.SearchRequest) ([]knowledgeprovider.KnowledgeHit, error) {
	// 1. 将 query 转为向量
	vectors, err := p.embedding.Embed(ctx, []string{req.Query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	queryVec := vectors[0]

	// 2. 构建查询
	topK := req.TopK
	if topK <= 0 {
		topK = p.config.Search.TopK
	}
	if topK <= 0 {
		topK = 5
	}

	threshold := req.Threshold
	if threshold <= 0 {
		threshold = p.config.Search.Threshold
	}

	// 3. 执行向量相似度搜索
	query := p.db.WithContext(ctx).
		Table("knowledge_docs").
		Select("id, title, content, doc_chunk_id, category, tags")

	// 添加过滤条件
	if req.TenantID != "" {
		query = query.Where("tenant_id = ?", req.TenantID)
	}
	if req.KnowledgeID != "" {
		query = query.Where("doc_chunk_id LIKE ?", req.KnowledgeID+"-%")
	}

	// 添加向量条件（使用原生 SQL 实现向量搜索）
	vectorStr := FormatVector(queryVec)
	if p.config.Search.Strategy == "euclidean" {
		// 欧几里得距离
		query = query.Where("embedding IS NOT NULL").
			Order(fmt.Sprintf("embedding <-> '%s'::vector", vectorStr))
	} else {
		// 余弦相似度（默认）
		query = query.Where("embedding IS NOT NULL").
			Order(fmt.Sprintf("embedding <=> '%s'::vector", vectorStr))
	}

	if topK > 0 {
		query = query.Limit(topK)
	}

	var docs []models.KnowledgeDoc
	if err := query.Find(&docs).Error; err != nil {
		return nil, fmt.Errorf("execute search: %w", err)
	}

	// 4. 计算相似度并过滤结果
	hits := make([]knowledgeprovider.KnowledgeHit, 0, len(docs))
	for _, doc := range docs {
		var similarity float32
		if doc.Embedding != nil {
			if p.config.Search.Strategy == "euclidean" {
				// 转换距离为相似度
				dist := EuclideanDistance(doc.Embedding, queryVec)
				similarity = 1 / (1 + dist) // 距离越小，相似度越高
			} else {
				similarity = 1 - CosineDistance(doc.Embedding, queryVec)
			}
		}

		if similarity < float32(threshold) {
			continue
		}

		hits = append(hits, knowledgeprovider.KnowledgeHit{
			DocumentID: fmt.Sprint(doc.ID),
			Title:      doc.Title,
			Content:    doc.Content,
			Score:      float64(similarity),
			Source:     "pgvector",
			Metadata: map[string]interface{}{
				"doc_chunk_id": doc.DocChunkID,
				"chunk_index":  doc.ChunkIndex,
				"category":     doc.Category,
				"tags":         doc.Tags,
			},
		})
	}

	return hits, nil
}

func (p *Provider) UpsertDocument(ctx context.Context, doc knowledgeprovider.KnowledgeDocument) (string, error) {
	// 1. 文档分块
	chunks := p.chunker.Chunk(doc.Content)
	if len(chunks) == 0 {
		return "", fmt.Errorf("no chunks after splitting")
	}

	// 2. 为所有 chunks 生成向量
	vectors, err := p.embedding.Embed(ctx, chunks)
	if err != nil {
		return "", fmt.Errorf("embed chunks: %w", err)
	}
	if len(vectors) != len(chunks) {
		return "", fmt.Errorf("embedding count mismatch: got %d, expected %d", len(vectors), len(chunks))
	}

	// 3. 删除旧的 chunks（如果存在）
	if doc.ExternalID != "" {
		p.db.WithContext(ctx).
			Table("knowledge_docs").
			Where("doc_chunk_id LIKE ?", doc.ExternalID+"-%").
			Delete(&models.KnowledgeDoc{})
	}

	// 4. 存储每个 chunk
	docID := doc.ID
	if docID == "" {
		docID = fmt.Sprintf("doc-%d", len(chunks))
	}

	var firstChunkID string
	for i, chunk := range chunks {
		chunkID := fmt.Sprintf("%s-chunk-%d", docID, i)
		if i == 0 {
			firstChunkID = chunkID
		}

		docRecord := &models.KnowledgeDoc{
			Title:      doc.Title,
			Content:    chunk,
			Category:   getStringValue(doc.Metadata, "category"),
			Tags:       strings.Join(doc.Tags, ","),
			Embedding:  vectors[i],
			ChunkIndex: i,
			DocChunkID: chunkID,
		}

		if err := p.db.WithContext(ctx).Create(docRecord).Error; err != nil {
			return "", fmt.Errorf("save chunk %d: %w", i, err)
		}
	}

	// 返回代表整个文档的 ID
	return fmt.Sprintf("%s (%d chunks)", firstChunkID, len(chunks)), nil
}

func (p *Provider) DeleteDocument(ctx context.Context, id string) error {
	// 删除所有属于该文档的 chunks
	result := p.db.WithContext(ctx).
		Table("knowledge_docs").
		Where("doc_chunk_id LIKE ?", id+"-%").
		Delete(&models.KnowledgeDoc{})

	if result.Error != nil {
		return fmt.Errorf("delete document: %w", result.Error)
	}

	return nil
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	// 检查数据库连接和 pgvector 扩展
	var result string
	err := p.db.WithContext(ctx).Raw("SELECT 1").Scan(&result).Error
	if err != nil {
		return fmt.Errorf("database connection failed: %w", err)
	}

	// 检查 pgvector 扩展
	err = p.db.WithContext(ctx).Raw("SELECT vector('[1,2,3]')::vector").Error
	if err != nil {
		return fmt.Errorf("pgvector extension not available: %w", err)
	}

	// 检查 embedding provider
	if p.embedding != nil {
		if err := p.embedding.HealthCheck(ctx); err != nil {
			return fmt.Errorf("embedding provider health check failed: %w", err)
		}
	}

	return nil
}

func getStringValue(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	val, ok := metadata[key]
	if !ok {
		return ""
	}
	if str, ok := val.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", val)
}
```

- [ ] **Step 2: 创建集成测试**

```go
// apps/server/internal/platform/knowledgeprovider/pgvector/provider_test.go
package pgvector

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/platform/embedding"
	"servify/apps/server/internal/platform/knowledgeprovider"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// 创建表
	err = db.AutoMigrate(&models.KnowledgeDoc{})
	if err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	return db
}

func TestProvider_Search_EmptyResult(t *testing.T) {
	db := setupTestDB(t)
	mockEmbed := &embedding.MockProvider{dimensionVal: 1536}

	provider := NewProvider(db, mockEmbed, Config{
		Search: SearchConfig{TopK: 5, Threshold: 0.7},
	})

	ctx := context.Background()
	hits, err := provider.Search(ctx, knowledgeprovider.SearchRequest{
		Query: "test query",
	})

	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(hits) != 0 {
		t.Fatalf("expected 0 hits, got %d", len(hits))
	}
}

func TestProvider_UpsertDocument(t *testing.T) {
	db := setupTestDB(t)
	mockEmbed := &embedding.MockProvider{dimensionVal: 1536}

	provider := NewProvider(db, mockEmbed, Config{
		Indexing: IndexingConfig{ChunkSize: 50, ChunkOverlap: 10},
	})

	ctx := context.Background()
	docID, err := provider.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{
		ID:      "test-doc-1",
		Title:   "Test Document",
		Content: "This is a test document that should be chunked.",
		Tags:    []string{"test"},
	})

	if err != nil {
		t.Fatalf("UpsertDocument failed: %v", err)
	}

	if docID == "" {
		t.Fatal("expected non-empty doc ID")
	}

	// 验证数据库中的记录
	var count int64
	db.Model(&models.KnowledgeDoc{}).Count(&count)
	if count == 0 {
		t.Fatal("expected at least one record in database")
	}
}

func TestProvider_UpsertDocument_LongContent(t *testing.T) {
	db := setupTestDB(t)
	mockEmbed := &embedding.MockProvider{dimensionVal: 512}

	provider := NewProvider(db, mockEmbed, Config{
		Indexing: IndexingConfig{ChunkSize: 20, ChunkOverlap: 5},
	})

	ctx := context.Background()
	// 创建一个足够长的内容来产生多个 chunks
	longContent := "This is a very long document that will be split into multiple chunks. " +
		"It contains enough text to test the chunking functionality. " +
		"Each chunk should be around 20 characters with 5 characters overlap."

	docID, err := provider.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{
		ID:      "test-doc-long",
		Title:   "Long Document",
		Content: longContent,
	})

	if err != nil {
		t.Fatalf("UpsertDocument failed: %v", err)
	}

	// 验证有多个 chunks
	var count int64
	db.Model(&models.KnowledgeDoc{}).Where("doc_chunk_id LIKE ?", "test-doc-long-%").Count(&count)
	if count < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", count)
	}

	t.Logf("Created %d chunks for document: %s", count, docID)
}

func TestProvider_DeleteDocument(t *testing.T) {
	db := setupTestDB(t)
	mockEmbed := &embedding.MockProvider{dimensionVal: 1536}

	provider := NewProvider(db, mockEmbed, Config{})

	ctx := context.Background()

	// 先创建文档
	_, err := provider.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{
		ID:      "test-doc-delete",
		Title:   "Delete Test",
		Content: "Content to delete",
	})
	if err != nil {
		t.Fatalf("UpsertDocument failed: %v", err)
	}

	// 删除文档
	err = provider.DeleteDocument(ctx, "test-doc-delete")
	if err != nil {
		t.Fatalf("DeleteDocument failed: %v", err)
	}

	// 验证删除
	var count int64
	db.Model(&models.KnowledgeDoc{}).Where("doc_chunk_id LIKE ?", "test-doc-delete-%").Count(&count)
	if count != 0 {
		t.Fatalf("expected 0 records after delete, got %d", count)
	}
}

func TestProvider_HealthCheck(t *testing.T) {
	db := setupTestDB(t)
	mockEmbed := &embedding.MockProvider{}

	provider := NewProvider(db, mockEmbed, Config{})

	ctx := context.Background()
	if err := provider.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
}

func TestProvider_Search_WithEmbedding(t *testing.T) {
	db := setupTestDB(t)
	mockEmbed := &embedding.MockProvider{dimensionVal: 128}

	provider := NewProvider(db, mockEmbed, Config{
		Search: SearchConfig{TopK: 3, Threshold: 0.5},
	})

	ctx := context.Background()

	// 创建测试文档
	_, err := provider.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{
		ID:      "search-test",
		Title:   "Search Test Document",
		Content: "This document contains keywords about search functionality",
		Tags:    []string{"search", "test"},
	})
	if err != nil {
		t.Fatalf("UpsertDocument failed: %v", err)
	}

	// 搜索
	hits, err := provider.Search(ctx, knowledgeprovider.SearchRequest{
		Query: "search functionality",
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	t.Logf("Found %d hits", len(hits))
	for i, hit := range hits {
		t.Logf("Hit %d: score=%f, title=%s", i, hit.Score, hit.Title)
	}
}

func TestProvider_ConfigDefaults(t *testing.T) {
	db := setupTestDB(t)
	mockEmbed := &embedding.MockProvider{}

	provider := NewProvider(db, mockEmbed, Config{})

	if provider.config.Search.TopK != 0 {
		t.Errorf("expected default TopK 0, got %d", provider.config.Search.TopK)
	}

	if provider.config.Indexing.ChunkSize != 0 {
		t.Errorf("expected default ChunkSize 0, got %d", provider.config.Indexing.ChunkSize)
	}

	// 验证 chunker 有默认值
	if provider.chunker.ChunkSize != 500 {
		t.Errorf("expected chunker ChunkSize 500, got %d", provider.chunker.ChunkSize)
	}
}
```

- [ ] **Step 3: 运行测试**

```bash
cd apps/server
go test ./internal/platform/knowledgeprovider/pgvector -v
```

预期: 全部通过

- [ ] **Step 4: 提交**

```bash
git add apps/server/internal/platform/knowledgeprovider/pgvector/provider.go
git add apps/server/internal/platform/knowledgeprovider/pgvector/provider_test.go
git commit -m "feat(pgvector): implement PgvectorProvider for knowledge base"
```

---

## Phase 5: 集成与测试

### Task 12: 更新 Bootstrap 初始化

**Files:**
- Modify: `apps/server/internal/app/bootstrap/app.go`

- [ ] **Step 1: 在 bootstrap 中添加 EmbeddingProvider 初始化**

在 `app.go` 中找到 `BuildApp` 函数，在现有初始化代码之后添加：

```go
// 在 BuildApp 函数中添加
func BuildApp(...) *App {
    // ... 现有代码

    // 初始化 Embedding Provider
    embeddingProvider, err := embedding.NewProvider(cfg.Embedding)
    if err != nil {
        return nil, fmt.Errorf("create embedding provider: %w", err)
    }

    // ... 其他初始化
}
```

并在 `App` 结构体中添加字段：

```go
type App struct {
    // ... 现有字段
    EmbeddingProvider embedding.Provider
}
```

- [ ] **Step 2: 提交**

```bash
git add apps/server/internal/app/bootstrap/app.go
git commit -m "feat(bootstrap): add EmbeddingProvider initialization"
```

---

### Task 13: 更新知识库 Service

**Files:**
- Modify: `apps/server/internal/modules/knowledge/application/service.go`

- [ ] **Step 1: 更新 Service 以支持 PgvectorProvider**

确保 service 可以正确使用新的 PgvectorProvider。主要修改 `syncDocument` 方法，确保 providerID 正确设置：

```go
func (s *Service) syncDocument(ctx context.Context, doc *domain.Document) error {
    if s.provider == nil || doc == nil {
        return nil
    }

    // 设置 provider ID
    if doc.ProviderID == "" {
        doc.ProviderID = "pgvector"
    }

    externalID, err := s.provider.UpsertDocument(ctx, knowledgeprovider.KnowledgeDocument{
        ID:         doc.ID,
        ProviderID: doc.ProviderID,
        ExternalID: doc.ExternalID,
        Title:      doc.Title,
        Content:    doc.Content,
        Tags:       doc.Tags,
        Metadata:   map[string]interface{}{"category": doc.Category},
    })
    if err != nil {
        return err
    }
    if strings.TrimSpace(externalID) != "" {
        doc.ExternalID = strings.TrimSpace(externalID)
    }
    return nil
}
```

- [ ] **Step 2: 提交**

```bash
git add apps/server/internal/modules/knowledge/application/service.go
git commit -m "feat(knowledge): update service for pgvector provider"
```

---

### Task 14: 创建验收脚本

**Files:**
- Create: `scripts/test-knowledge-acceptance.sh`

- [ ] **Step 1: 创建验收脚本**

```bash
#!/bin/bash
# scripts/test-knowledge-acceptance.sh
# 知识库验收脚本

set -e

SERVIFY_URL="${SERVIFY_URL:-http://localhost:8080}"
EMBEDDING_PROVIDER="${EMBEDDING_PROVIDER:-openai}"
EVIDENCE_DIR="${EVIDENCE_DIR:-./scripts/test-results/knowledge-acceptance}"

echo "=== Servify Knowledge Base Acceptance Test ==="
echo "Servify URL: $SERVIFY_URL"
echo "Embedding Provider: $EMBEDDING_PROVIDER"
echo "Evidence Directory: $EVIDENCE_DIR"
echo ""

# 创建证据目录
mkdir -p "$EVIDENCE_DIR"

# 1. 健康检查
echo "1. Health Check..."
curl -s "$SERVIFY_URL/health" | tee "$EVIDENCE_DIR/health.json"
echo ""

# 2. 创建测试文档
echo "2. Creating test document..."
curl -s -X POST "$SERVIFY_URL/api/knowledge-docs" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TEST_TOKEN" \
  -d '{
    "title": "Test Document",
    "content": "This is a test document for knowledge base acceptance testing. It contains information about product features, installation steps, and troubleshooting guides.",
    "category": "test",
    "tags": ["acceptance", "test"],
    "is_public": true
  }' | tee "$EVIDENCE_DIR/create-doc.json"
echo ""

# 3. 列出文档
echo "3. Listing documents..."
curl -s "$SERVIFY_URL/api/knowledge-docs?page=1&page_size=10" \
  -H "Authorization: Bearer $TEST_TOKEN" | tee "$EVIDENCE_DIR/list-docs.json"
echo ""

# 4. 搜索测试
echo "4. Searching documents..."
curl -s -X POST "$SERVIFY_URL/api/v1/ai/query" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "product features and installation",
    "session_id": "acceptance-test"
  }' | tee "$EVIDENCE_DIR/search-result.json"
echo ""

# 5. 生成 manifest
echo "5. Generating manifest..."
cat > "$EVIDENCE_DIR/manifest.json" << EOF
{
  "timestamp": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "servify_url": "$SERVIFY_URL",
  "embedding_provider": "$EMBEDDING_PROVIDER",
  "tests": [
    {"name": "health", "file": "health.json"},
    {"name": "create_doc", "file": "create-doc.json"},
    {"name": "list_docs", "file": "list-docs.json"},
    {"name": "search", "file": "search-result.json"}
  ]
}
EOF

echo "=== Acceptance Test Complete ==="
echo "Evidence saved to: $EVIDENCE_DIR"
```

- [ ] **Step 2: 添加执行权限**

```bash
chmod +x scripts/test-knowledge-acceptance.sh
```

- [ ] **Step 3: 更新 Makefile 添加快捷命令**

在 `Makefile` 中添加：

```makefile
.PHONY: knowledge-acceptance
knowledge-acceptance:
	@echo "Running knowledge base acceptance test..."
	@./scripts/test-knowledge-acceptance.sh
```

- [ ] **Step 4: 提交**

```bash
git add scripts/test-knowledge-acceptance.sh
git add Makefile
git commit -m "feat(testing): add knowledge base acceptance script"
```

---

## Phase 6: 文档与收尾

### Task 15: 更新项目文档

**Files:**
- Modify: `README.md`
- Modify: `docs/acceptance-checklist.md`

- [ ] **Step 1: 更新 README.md**

在 README.md 的 AI 与知识库设计部分添加：

```markdown
### 自建知识库 (pgvector)

Servify 现在支持基于 pgvector 的自建知识库，这是企业私有部署的推荐方案。

**配置:**

```yaml
embedding:
  provider: "openai"  # 或 tei, xinference
  openai:
    api_key: "${OPENAI_API_KEY}"
    model: "text-embedding-3-small"

knowledge:
  provider: "pgvector"
  pgvector:
    search:
      top_k: 5
      threshold: 0.7
```

**内网部署:**

使用 TEI (Text Embeddings Inference) 进行本地 embedding：

```bash
docker run -p 8080:8080 \
  ghcr.io/huggingface/text-embeddings-inference:cpu-1.5 \
  --model-id BAAI/bge-small-zh-v1.5
```

然后配置：

```yaml
embedding:
  provider: "tei"
  tei:
    base_url: "http://localhost:8080"
```
```

- [ ] **Step 2: 更新验收清单**

在 `docs/acceptance-checklist.md` 的 AI 与外部知识库部分添加 pgvector 条目：

```markdown
| 功能项 | 入口 | 验收步骤 | 预期结果 | 自动化证据 | 状态 |
|---|---|---|---|---|---|
| pgvector 知识库 | `POST /api/knowledge-docs` | 创建文档并搜索 | 向量检索返回相似结果 | [provider_test.go](...) | 通过 |
```

- [ ] **Step 3: 提交**

```bash
git add README.md docs/acceptance-checklist.md
git commit -m "docs: add pgvector knowledge base documentation"
```

---

### Task 16: 最终验证与清理

**Files:**
- (全局验证)

- [ ] **Step 1: 运行所有测试**

```bash
cd apps/server
go test ./internal/platform/embedding/...
go test ./internal/platform/knowledgeprovider/pgvector/...
go test ./internal/config/...
go test ./internal/models/...
```

预期: 全部通过

- [ ] **Step 2: 构建验证**

```bash
make build
```

预期: 构建成功

- [ ] **Step 3: 配置文件验证**

```bash
go run ./apps/server/cmd/server --help 2>&1 | head -5
```

预期: 无配置解析错误

- [ ] **Step 4: 创建 README 示例**

在项目根目录创建 `README_KNOWLEDGE.md` 说明如何使用知识库功能：

```markdown
# 知识库使用指南

## 快速开始

### 1. 启动服务

```bash
make migrate
make run CONFIG=./config.yml
```

### 2. 创建知识文档

```bash
curl -X POST http://localhost:8080/api/knowledge-docs \
  -H "Content-Type: application/json" \
  -d '{
    "title": "产品安装指南",
    "content": "详细的安装步骤...",
    "category": "安装",
    "tags": ["指南", "安装"],
    "is_public": true
  }'
```

### 3. 搜索知识

```bash
curl -X POST http://localhost:8080/api/v1/ai/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "如何安装产品",
    "session_id": "test-123"
  }'
```

## Embedding Provider 选择

### OpenAI (云部署)

```yaml
embedding:
  provider: "openai"
  openai:
    api_key: "sk-..."
    model: "text-embedding-3-small"
```

### TEI (内网部署)

```bash
# 启动 TEI 服务
docker run -p 8080:8080 \
  -v $PWD/data:/data \
  ghcr.io/huggingface/text-embeddings-inference:cpu-1.5 \
  --model-id BAAI/bge-small-zh-v1.5
```

```yaml
embedding:
  provider: "tei"
  tei:
    base_url: "http://localhost:8080"
    model: "BAAI/bge-small-zh-v1.5"
```

## 验收测试

```bash
make knowledge-acceptance
```
```

- [ ] **Step 5: 提交所有剩余更改**

```bash
git add README_KNOWLEDGE.md
git commit -m "docs: add knowledge base usage guide"
```

---

## 完成检查清单

在实施完成后，验证以下功能：

- [ ] EmbeddingProvider 接口定义完整
- [ ] OpenAI Provider 实现并测试通过
- [ ] TEI Provider 实现并测试通过
- [ ] Xinference Provider 实现并测试通过
- [ ] Factory 可以正确创建 provider
- [ ] 配置结构扩展完成
- [ ] KnowledgeDoc 模型扩展完成
- [ ] 数据库迁移脚本添加
- [ ] 文档分块逻辑实现并测试
- [ ] 向量检索逻辑实现并测试
- [ ] PgvectorProvider 实现并集成测试通过
- [ ] Bootstrap 初始化更新
- [ ] 验收脚本创建
- [ ] 文档更新
- [ ] 所有测试通过
- [ ] 构建成功
