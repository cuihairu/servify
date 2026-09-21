package ragflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Client 是 ClientInterface 的 HTTP 实现。
type Client struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewClient 构造 RAGFlow HTTP 客户端：超时零值取 30s，BaseURL 空取本地默认。
func NewClient(cfg *Config) *Client {
	timeout := 30 * time.Second
	if cfg != nil && cfg.Timeout > 0 {
		timeout = cfg.Timeout
	}

	baseURL := "http://localhost:9380"
	if cfg != nil && strings.TrimSpace(cfg.BaseURL) != "" {
		baseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	}

	apiKey := ""
	if cfg != nil {
		apiKey = strings.TrimSpace(cfg.APIKey)
	}

	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			Timeout:   timeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

func (c *Client) GetDataset(ctx context.Context, datasetID string) (*Dataset, error) {
	id := strings.TrimSpace(datasetID)
	var datasets []datasetDTO
	query := url.Values{"id": {id}, "page": {"1"}, "page_size": {"1"}}.Encode()
	if err := c.do(ctx, http.MethodGet, "/api/v1/datasets?"+query, nil, &datasets); err != nil {
		return nil, err
	}
	if len(datasets) == 0 {
		return nil, fmt.Errorf("ragflow dataset not found: %s", id)
	}
	return &Dataset{ID: datasets[0].ID, Name: datasets[0].Name}, nil
}

func (c *Client) Retrieve(ctx context.Context, req *RetrieveRequest) (*RetrieveResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("ragflow retrieve request is required")
	}
	var resp retrieveResponseDTO
	if err := c.do(ctx, http.MethodPost, "/api/v1/retrieval", req, &resp); err != nil {
		return nil, err
	}
	chunks := make([]Chunk, 0, len(resp.Chunks))
	chunks = append(chunks, resp.Chunks...)
	return &RetrieveResponse{Total: resp.Total, Chunks: chunks}, nil
}

func (c *Client) ListDocuments(ctx context.Context, datasetID, name string) ([]Document, error) {
	values := url.Values{"page": {"1"}, "page_size": {"100"}}
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		values.Set("name", trimmed)
	}
	path := "/api/v1/datasets/" + strings.TrimSpace(datasetID) + "/documents?" + values.Encode()
	var resp documentListDTO
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	docs := make([]Document, 0, len(resp.Docs))
	for _, doc := range resp.Docs {
		docs = append(docs, Document{ID: doc.ID, Name: doc.Name, Run: doc.Run})
	}
	return docs, nil
}

// UploadDocument 以 multipart（type=local 默认形态）上传文本内容构造的
// .txt 文件。RAGFlow 上传后不自动解析，需另行 ParseDocuments。
func (c *Client) UploadDocument(ctx context.Context, datasetID, name, content string) (*Document, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	// multipart 落到内存 bytes.Buffer，构造错误在契约上不可达，按惯例忽略。
	fileWriter, _ := writer.CreateFormFile("file", strings.TrimSpace(name)+".txt")
	_, _ = fileWriter.Write([]byte(content))
	_ = writer.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/datasets/"+strings.TrimSpace(datasetID)+"/documents", &buf)
	if err != nil {
		return nil, fmt.Errorf("create ragflow request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setAuth(req)

	var docs []documentDTO
	if err := c.doRequest(req, &docs); err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("ragflow upload returned no document")
	}
	return &Document{ID: docs[0].ID, Name: docs[0].Name, Run: docs[0].Run}, nil
}

// ParseDocuments 触发异步解析（POST /datasets/{id}/chunks）。
func (c *Client) ParseDocuments(ctx context.Context, datasetID string, documentIDs []string) error {
	body := struct {
		DocumentIDs []string `json:"document_ids"`
	}{DocumentIDs: documentIDs}
	// data 可能为空对象/null，不解析外出结构。
	return c.do(ctx, http.MethodPost, "/api/v1/datasets/"+strings.TrimSpace(datasetID)+"/chunks", body, nil)
}

func (c *Client) DeleteDocuments(ctx context.Context, datasetID string, documentIDs []string) error {
	body := struct {
		IDs []string `json:"ids"`
	}{IDs: documentIDs}
	return c.do(ctx, http.MethodDelete, "/api/v1/datasets/"+strings.TrimSpace(datasetID)+"/documents", body, nil)
}

// HealthCheck 以带鉴权的 dataset 存在性探测作为可用性检查（对齐 dify
// GetDataset 语义，同时验证 BaseURL、凭证与配置 dataset 三者）。
func (c *Client) HealthCheck(ctx context.Context, datasetID string) error {
	_, err := c.GetDataset(ctx, datasetID)
	return err
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal ragflow request: %w", err)
		}
		reader = bytes.NewBuffer(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("create ragflow request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)
	return c.doRequest(req, out)
}

func (c *Client) doRequest(req *http.Request, out any) error {
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("send ragflow request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read ragflow response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("ragflow http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var envelope apiEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return fmt.Errorf("decode ragflow response: %w", err)
	}
	if envelope.Code != 0 {
		return fmt.Errorf("ragflow api error code=%d: %s", envelope.Code, strings.TrimSpace(envelope.Message))
	}
	if out == nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("decode ragflow data: %w", err)
	}
	return nil
}

func (c *Client) setAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

// apiEnvelope 是 RAGFlow 统一响应包裹；业务码与 HTTP 码双层判定。
type apiEnvelope struct {
	Code    int             `json:"code"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
}

type datasetDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type retrieveResponseDTO struct {
	Total  int     `json:"total"`
	Chunks []Chunk `json:"chunks"`
}

type documentDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Run  string `json:"run"`
}

type documentListDTO struct {
	Docs []documentDTO `json:"docs"`
}
