package ragflow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// okEnvelope 返回 code=0 的统一包裹体。
func okEnvelope(data string) string {
	if data == "" {
		return `{"code":0}`
	}
	return `{"code":0,"data":` + data + `}`
}

func TestNewClientDefaults(t *testing.T) {
	client := NewClient(nil)
	if client.baseURL != "http://localhost:9380" {
		t.Fatalf("baseURL = %q", client.baseURL)
	}
	if client.client.Timeout != 30*time.Second {
		t.Fatalf("timeout = %v", client.client.Timeout)
	}
	if client.apiKey != "" {
		t.Fatalf("apiKey = %q", client.apiKey)
	}
}

func TestNewClientTrimsBaseURLAndAPIKey(t *testing.T) {
	client := NewClient(&Config{BaseURL: "http://example.com:9380/", APIKey: "  ragflow-key  ", Timeout: 5 * time.Second})
	if client.baseURL != "http://example.com:9380" {
		t.Fatalf("baseURL = %q", client.baseURL)
	}
	if client.apiKey != "ragflow-key" {
		t.Fatalf("apiKey = %q", client.apiKey)
	}
	if client.client.Timeout != 5*time.Second {
		t.Fatalf("timeout = %v", client.client.Timeout)
	}
}

func TestGetDatasetSuccess(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api/v1/datasets" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("id") != "ds-1" {
			t.Errorf("id = %q", r.URL.Query().Get("id"))
		}
		_, _ = io.WriteString(w, okEnvelope(`[{"id":"ds-1","name":"kb"}]`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, APIKey: "ragflow-key"})
	dataset, err := client.GetDataset(context.Background(), "ds-1")
	if err != nil {
		t.Fatalf("GetDataset() error = %v", err)
	}
	if dataset.ID != "ds-1" || dataset.Name != "kb" {
		t.Fatalf("dataset = %+v", dataset)
	}
	if gotAuth != "Bearer ragflow-key" {
		t.Fatalf("auth = %q", gotAuth)
	}
}

func TestGetDatasetNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, okEnvelope(`[]`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	_, err := client.GetDataset(context.Background(), "ds-missing")
	if err == nil || !strings.Contains(err.Error(), "ragflow dataset not found: ds-missing") {
		t.Fatalf("GetDataset() error = %v", err)
	}
}

func TestRetrieveSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/retrieval" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req RetrieveRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.Question != "refund policy" || len(req.DatasetIDs) != 1 || req.DatasetIDs[0] != "ds-1" {
			t.Errorf("request = %+v", req)
		}
		if req.PageSize != 5 || req.SimilarityThreshold != 0.3 || req.TopK != 64 {
			t.Errorf("request tuning = %+v", req)
		}
		_, _ = io.WriteString(w, okEnvelope(`{"total":1,"chunks":[{"content":"chunk text","document_id":"doc-1","document_keyword":"manual.txt","dataset_id":"ds-1","similarity":0.87}]}`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	resp, err := client.Retrieve(context.Background(), &RetrieveRequest{
		Question:            "refund policy",
		DatasetIDs:          []string{"ds-1"},
		PageSize:            5,
		SimilarityThreshold: 0.3,
		TopK:                64,
	})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if resp.Total != 1 || len(resp.Chunks) != 1 {
		t.Fatalf("resp = %+v", resp)
	}
	chunk := resp.Chunks[0]
	if chunk.Content != "chunk text" || chunk.DocumentID != "doc-1" || chunk.DocumentKeyword != "manual.txt" || chunk.Similarity != 0.87 {
		t.Fatalf("chunk = %+v", chunk)
	}
}

func TestRetrieveNilRequest(t *testing.T) {
	client := NewClient(nil)
	if _, err := client.Retrieve(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "ragflow retrieve request is required") {
		t.Fatalf("Retrieve() error = %v", err)
	}
}

func TestListDocuments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/datasets/ds-1/documents" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if name := r.URL.Query().Get("name"); name != "manual.txt" {
			t.Errorf("name = %q", name)
		}
		_, _ = io.WriteString(w, okEnvelope(`{"docs":[{"id":"doc-1","name":"manual.txt","run":"DONE"}],"total":1}`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	docs, err := client.ListDocuments(context.Background(), "ds-1", "manual.txt")
	if err != nil {
		t.Fatalf("ListDocuments() error = %v", err)
	}
	if len(docs) != 1 || docs[0].ID != "doc-1" || docs[0].Run != "DONE" {
		t.Fatalf("docs = %+v", docs)
	}
}

func TestUploadDocumentMultipart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/datasets/ds-1/documents" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("form file: %v", err)
			return
		}
		defer file.Close()
		if header.Filename != "manual.txt" {
			t.Errorf("filename = %q", header.Filename)
		}
		content, _ := io.ReadAll(file)
		if string(content) != "file body" {
			t.Errorf("content = %q", string(content))
		}
		_, _ = io.WriteString(w, okEnvelope(`[{"id":"doc-9","name":"manual.txt","run":"UNSTART"}]`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, APIKey: "ragflow-key"})
	doc, err := client.UploadDocument(context.Background(), "ds-1", "manual", "file body")
	if err != nil {
		t.Fatalf("UploadDocument() error = %v", err)
	}
	if doc.ID != "doc-9" || doc.Run != "UNSTART" {
		t.Fatalf("doc = %+v", doc)
	}
}

func TestUploadDocumentNoDocumentReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, okEnvelope(`[]`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	if _, err := client.UploadDocument(context.Background(), "ds-1", "name", "content"); err == nil || !strings.Contains(err.Error(), "ragflow upload returned no document") {
		t.Fatalf("UploadDocument() error = %v", err)
	}
}

func TestParseDocumentsToleratesNullData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/datasets/ds-1/chunks" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"document_ids":["doc-9"]}` {
			t.Errorf("body = %q", string(body))
		}
		_, _ = io.WriteString(w, `{"code":0,"data":null}`)
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	if err := client.ParseDocuments(context.Background(), "ds-1", []string{"doc-9"}); err != nil {
		t.Fatalf("ParseDocuments() error = %v", err)
	}
}

func TestDeleteDocumentsSendsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q", r.Method)
		}
		if r.URL.Path != "/api/v1/datasets/ds-1/documents" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"ids":["doc-1","doc-2"]}` {
			t.Errorf("body = %q", string(body))
		}
		_, _ = io.WriteString(w, okEnvelope(""))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	if err := client.DeleteDocuments(context.Background(), "ds-1", []string{"doc-1", "doc-2"}); err != nil {
		t.Fatalf("DeleteDocuments() error = %v", err)
	}
}

func TestHealthCheckDelegatesToGetDataset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, okEnvelope(`[{"id":"ds-1","name":"kb"}]`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	if err := client.HealthCheck(context.Background(), "ds-1"); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}
