package dify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Config struct {
	BaseURL    string
	APIKey     string
	Timeout    time.Duration
	MaxRetries int
}

type ClientInterface interface {
	GetDataset(ctx context.Context, datasetID string) (*Dataset, error)
	Retrieve(ctx context.Context, datasetID string, req *RetrieveRequest) (*RetrieveResponse, error)
	CreateDocumentFromText(ctx context.Context, datasetID string, req *CreateDocumentRequest) (*Document, error)
	DeleteDocument(ctx context.Context, datasetID, documentID string) error
	HealthCheck(ctx context.Context, datasetID string) error
}

type Client struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewClient(cfg *Config) *Client {
	timeout := 30 * time.Second
	if cfg != nil && cfg.Timeout > 0 {
		timeout = cfg.Timeout
	}

	baseURL := "http://localhost/v1"
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
	var resp datasetResponse
	if err := c.do(ctx, http.MethodGet, "/datasets/"+strings.TrimSpace(datasetID), nil, &resp); err != nil {
		return nil, err
	}
	return &Dataset{ID: resp.ID, Name: resp.Name}, nil
}

func (c *Client) Retrieve(ctx context.Context, datasetID string, req *RetrieveRequest) (*RetrieveResponse, error) {
	var resp retrieveResponse
	if err := c.do(ctx, http.MethodPost, "/datasets/"+strings.TrimSpace(datasetID)+"/retrieve", req, &resp); err != nil {
		return nil, err
	}

	out := &RetrieveResponse{
		Query:   resp.Query,
		Records: make([]RetrieveRecord, 0, len(resp.Records)),
	}
	for _, record := range resp.Records {
		content := strings.TrimSpace(record.Content)
		if content == "" {
			content = strings.TrimSpace(record.Segment.Content)
		}
		title := strings.TrimSpace(record.Title)
		if title == "" {
			title = strings.TrimSpace(record.Document.Name)
		}
		documentID := strings.TrimSpace(record.DocumentID)
		if documentID == "" {
			documentID = strings.TrimSpace(record.Document.ID)
		}
		segmentID := strings.TrimSpace(record.SegmentID)
		if segmentID == "" {
			segmentID = strings.TrimSpace(record.Segment.ID)
		}

		out.Records = append(out.Records, RetrieveRecord{
			SegmentID:  segmentID,
			DocumentID: documentID,
			Title:      title,
			Content:    content,
			Score:      record.Score,
			Metadata:   record.Metadata,
		})
	}
	return out, nil
}

func (c *Client) CreateDocumentFromText(ctx context.Context, datasetID string, req *CreateDocumentRequest) (*Document, error) {
	var resp createDocumentResponse
	if err := c.do(ctx, http.MethodPost, "/datasets/"+strings.TrimSpace(datasetID)+"/document/create-by-text", req, &resp); err != nil {
		return nil, err
	}
	return &Document{ID: resp.Document.ID, Name: resp.Document.Name}, nil
}

func (c *Client) DeleteDocument(ctx context.Context, datasetID, documentID string) error {
	return c.do(ctx, http.MethodDelete, "/datasets/"+strings.TrimSpace(datasetID)+"/documents/"+strings.TrimSpace(documentID), nil, nil)
}

func (c *Client) HealthCheck(ctx context.Context, datasetID string) error {
	_, err := c.GetDataset(ctx, datasetID)
	return err
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal dify request: %w", err)
		}
		reader = bytes.NewBuffer(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("create dify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("send dify request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // 读完即弃，关闭错误不影响响应语义

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read dify response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("dify http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode dify response: %w", err)
	}
	return nil
}

type datasetResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type retrieveResponse struct {
	Query   string                   `json:"query"`
	Records []retrieveResponseRecord `json:"records"`
}

type retrieveResponseRecord struct {
	SegmentID  string                 `json:"segment_id"`
	DocumentID string                 `json:"document_id"`
	Title      string                 `json:"title"`
	Content    string                 `json:"content"`
	Score      float64                `json:"score"`
	Metadata   map[string]interface{} `json:"metadata"`
	Segment    struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	} `json:"segment"`
	Document struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"document"`
}

type createDocumentResponse struct {
	Document struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"document"`
}
