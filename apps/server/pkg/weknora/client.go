package weknora

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// Client WeKnora HTTP 客户端
type Client struct {
	baseURL    string
	apiKey     string
	tenantID   string
	httpClient *http.Client
	logger     *logrus.Logger
	config     *Config
}

// WeKnoraInterface 定义 WeKnora 客户端接口
type WeKnoraInterface interface {
	// 知识库管理
	CreateKnowledgeBase(ctx context.Context, req *CreateKBRequest) (*KnowledgeBase, error)
	GetKnowledgeBase(ctx context.Context, kbID string) (*KnowledgeBase, error)

	// 文档管理
	UploadDocument(ctx context.Context, kbID string, doc *Document) (*DocumentInfo, error)
	DeleteDocument(ctx context.Context, kbID, docID string) error

	// 检索功能
	SearchKnowledge(ctx context.Context, req *SearchRequest) (*SearchResponse, error)

	// 会话管理
	CreateSession(ctx context.Context, req *SessionRequest) (*Session, error)
	Chat(ctx context.Context, sessionID string, req *ChatRequest) (*ChatResponse, error)

	// 健康检查
	HealthCheck(ctx context.Context) error
}

// NewClient 创建新的 WeKnora 客户端
func NewClient(config *Config, logger *logrus.Logger) *Client {
	if config == nil {
		config = DefaultConfig()
	}

	if logger == nil {
		logger = logrus.New()
	}

	return &Client{
		baseURL:    config.BaseURL,
		apiKey:     config.APIKey,
		tenantID:   config.TenantID,
		httpClient: &http.Client{Timeout: config.Timeout, Transport: otelhttp.NewTransport(http.DefaultTransport)},
		logger:     logger,
		config:     config,
	}
}

// 私有方法：创建 HTTP 请求
func (c *Client) createRequest(ctx context.Context, method, endpoint string, body interface{}) (*http.Request, error) {
	url := fmt.Sprintf("%s%s", c.baseURL, endpoint)

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	if c.tenantID != "" {
		req.Header.Set("X-Tenant-ID", c.tenantID)
	}
	req.Header.Set("User-Agent", "Servify-WeKnora-Client/1.0")

	return req, nil
}

// 私有方法：执行请求
func (c *Client) doRequest(req *http.Request, result interface{}) error {
	tracer := otel.Tracer("servify/weknora-client")
	ctx, span := tracer.Start(req.Context(), "WeKnora.doRequest")
	span.SetAttributes(attribute.String("method", req.Method), attribute.String("url", req.URL.String()))
	defer span.End()

	resp, err := c.httpClient.Do(req.WithContext(ctx))
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	// 记录请求和响应（调试模式）
	c.logger.Debugf("WeKnora API Request: %s %s", req.Method, req.URL.String())
	c.logger.Debugf("WeKnora API Response: %d %s", resp.StatusCode, string(body))

	// 检查 HTTP 状态码
	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if err := json.Unmarshal(body, &errResp); err == nil {
			span.SetStatus(codes.Error, errResp.Error)
			return fmt.Errorf("API error [%d]: %s (code: %s)", resp.StatusCode, errResp.Error, errResp.ErrorCode)
		}
		span.SetStatus(codes.Error, fmt.Sprintf("status %d", resp.StatusCode))
		return fmt.Errorf("API error [%d]: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	if result != nil {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

// 私有方法：带重试的请求
func (c *Client) doRequestWithRetry(ctx context.Context, method, endpoint string, body interface{}, result interface{}) error {
	var lastErr error

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// 等待重试
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.config.RetryDelay * time.Duration(attempt)):
			}
			c.logger.Warnf("WeKnora API retry attempt %d/%d", attempt, c.config.MaxRetries)
		}

		req, err := c.createRequest(ctx, method, endpoint, body)
		if err != nil {
			lastErr = err
			continue
		}

		if err := c.doRequest(req, result); err != nil {
			lastErr = err
			// 判断是否应该重试
			if attempt < c.config.MaxRetries && c.shouldRetry(err) {
				continue
			}
			break
		}

		// 成功
		return nil
	}

	return lastErr
}

// 判断是否应该重试
func (c *Client) shouldRetry(err error) bool {
	// 简单的重试逻辑，可以根据需要扩展
	// 网络错误或 5xx 错误通常可以重试
	return true
}

// SearchKnowledge 搜索知识库
func (c *Client) SearchKnowledge(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	if req.KnowledgeBaseID == "" {
		return nil, fmt.Errorf("knowledge base ID is required")
	}
	if req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	// 设置默认值
	if req.Limit <= 0 {
		req.Limit = 5
	}
	if req.Threshold <= 0 {
		req.Threshold = 0.7
	}
	if req.Strategy == "" {
		req.Strategy = "hybrid"
	}

	var response SearchResponse
	err := c.doRequestWithRetry(ctx, "POST", "/api/v1/knowledge/search", req, &response)
	if err != nil {
		return nil, fmt.Errorf("search knowledge: %w", err)
	}

	return &response, nil
}

// UploadDocument 上传文档
func (c *Client) UploadDocument(ctx context.Context, kbID string, doc *Document) (*DocumentInfo, error) {
	if kbID == "" {
		return nil, fmt.Errorf("knowledge base ID is required")
	}
	if doc.Title == "" {
		return nil, fmt.Errorf("document title is required")
	}

	endpoint := fmt.Sprintf("/api/v1/knowledge/%s/documents", kbID)

	var response struct {
		Success bool         `json:"success"`
		Data    DocumentInfo `json:"data"`
		Message string       `json:"message"`
	}

	err := c.doRequestWithRetry(ctx, "POST", endpoint, doc, &response)
	if err != nil {
		return nil, fmt.Errorf("upload document: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("upload failed: %s", response.Message)
	}

	return &response.Data, nil
}

// DeleteDocument 删除文档
func (c *Client) DeleteDocument(ctx context.Context, kbID, docID string) error {
	if kbID == "" {
		return fmt.Errorf("knowledge base ID is required")
	}
	if docID == "" {
		return fmt.Errorf("document ID is required")
	}

	endpoint := fmt.Sprintf("/api/v1/knowledge/%s/documents/%s", kbID, docID)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}

	err := c.doRequestWithRetry(ctx, "DELETE", endpoint, nil, &response)
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}

	if !response.Success {
		return fmt.Errorf("delete failed: %s", response.Message)
	}

	return nil
}

// CreateKnowledgeBase 创建知识库
func (c *Client) CreateKnowledgeBase(ctx context.Context, req *CreateKBRequest) (*KnowledgeBase, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("knowledge base name is required")
	}

	var response struct {
		Success bool          `json:"success"`
		Data    KnowledgeBase `json:"data"`
		Message string        `json:"message"`
	}

	err := c.doRequestWithRetry(ctx, "POST", "/api/v1/knowledge-bases", req, &response)
	if err != nil {
		return nil, fmt.Errorf("create knowledge base: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("create failed: %s", response.Message)
	}

	return &response.Data, nil
}

// GetKnowledgeBase 获取知识库信息
func (c *Client) GetKnowledgeBase(ctx context.Context, kbID string) (*KnowledgeBase, error) {
	if kbID == "" {
		return nil, fmt.Errorf("knowledge base ID is required")
	}

	endpoint := fmt.Sprintf("/api/v1/knowledge/%s", kbID)

	var response struct {
		Success bool          `json:"success"`
		Data    KnowledgeBase `json:"data"`
		Message string        `json:"message"`
	}

	err := c.doRequestWithRetry(ctx, "GET", endpoint, nil, &response)
	if err != nil {
		return nil, fmt.Errorf("get knowledge base: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("get failed: %s", response.Message)
	}

	return &response.Data, nil
}

// CreateSession 创建会话
func (c *Client) CreateSession(ctx context.Context, req *SessionRequest) (*Session, error) {
	if req.UserID == "" {
		return nil, fmt.Errorf("user ID is required")
	}

	var response struct {
		Success bool    `json:"success"`
		Data    Session `json:"data"`
		Message string  `json:"message"`
	}

	err := c.doRequestWithRetry(ctx, "POST", "/api/v1/sessions", req, &response)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("create session failed: %s", response.Message)
	}

	return &response.Data, nil
}

// Chat 发送聊天消息
func (c *Client) Chat(ctx context.Context, sessionID string, req *ChatRequest) (*ChatResponse, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session ID is required")
	}
	if req.Message == "" {
		return nil, fmt.Errorf("message is required")
	}

	endpoint := fmt.Sprintf("/api/v1/sessions/%s/chat", sessionID)

	var response struct {
		Success bool         `json:"success"`
		Data    ChatResponse `json:"data"`
		Message string       `json:"message"`
	}

	err := c.doRequestWithRetry(ctx, "POST", endpoint, req, &response)
	if err != nil {
		return nil, fmt.Errorf("chat: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("chat failed: %s", response.Message)
	}

	return &response.Data, nil
}

// HealthCheck 健康检查
func (c *Client) HealthCheck(ctx context.Context) error {
	var response HealthResponse
	err := c.doRequestWithRetry(ctx, "GET", "/api/v1/health", nil, &response)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	if response.Status != "healthy" && response.Status != "ok" {
		return fmt.Errorf("service unhealthy: %s", response.Status)
	}

	return nil
}

// GetStats 获取客户端统计信息
func (c *Client) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"base_url":    c.baseURL,
		"tenant_id":   c.tenantID,
		"timeout":     c.config.Timeout,
		"max_retries": c.config.MaxRetries,
	}
}
