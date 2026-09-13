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
	Input    *string  `json:"input,omitempty"`  // 单个文本
	Inputs   []string `json:"inputs,omitempty"` // 多个文本
	Truncate bool     `json:"truncate,omitempty"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
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

// jsonMarshal 是包级 seam（默认 json.Marshal）：embedRequest 只含
// string 指针/[]string 字段，序列化生产恒成功，测试注入失败以覆盖防御分支。
var jsonMarshal = json.Marshal

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

	payload, err := jsonMarshal(reqBody)
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
