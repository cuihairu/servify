package ragflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 双层判定的业务码层：HTTP 200 但 body code!=0 必须报错。
func TestAPIErrorBusinessCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = ioWriteString(w, `{"code":102,"message":"dataset_ids is required"}`)
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	_, err := client.Retrieve(context.Background(), &RetrieveRequest{})
	if err == nil || !strings.Contains(err.Error(), "ragflow api error code=102: dataset_ids is required") {
		t.Fatalf("Retrieve() error = %v", err)
	}
}

// 双层判定的 HTTP 层：非 2xx 直接报错且带 body。
func TestHTTPErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	if _, err := client.GetDataset(context.Background(), "ds-1"); err == nil || !strings.Contains(err.Error(), "ragflow http 500") {
		t.Fatalf("GetDataset() error = %v", err)
	}
}

// 200 但 body 非 JSON → envelope 解码错误。
func TestDoInvalidEnvelopeJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = ioWriteString(w, "not-json")
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	if err := client.ParseDocuments(context.Background(), "ds-1", []string{"doc-1"}); err == nil || !strings.Contains(err.Error(), "decode ragflow response") {
		t.Fatalf("ParseDocuments() error = %v", err)
	}
}

// data 与外出结构不匹配 → data 解码错误。
func TestDoInvalidDataJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = ioWriteString(w, okEnvelope(`{"docs":"not-an-array"}`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	if _, err := client.ListDocuments(context.Background(), "ds-1", ""); err == nil || !strings.Contains(err.Error(), "decode ragflow data") {
		t.Fatalf("ListDocuments() error = %v", err)
	}
}

// 空 data（缺 data 字段）与 out=nil 组合直接成功。
func TestDoEmptyDataWithNilOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = ioWriteString(w, `{"code":0,"message":"ok"}`)
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	if err := client.DeleteDocuments(context.Background(), "ds-1", []string{"doc-1"}); err != nil {
		t.Fatalf("DeleteDocuments() error = %v", err)
	}
}

// 响应体中途断连 → 读取错误分支。
func TestDoTruncatedResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		_, _ = ioWriteString(w, `{"code":`)
		w.(http.Flusher).Flush()
		conn, _, _ := w.(http.Hijacker).Hijack()
		if conn != nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	if err := client.ParseDocuments(context.Background(), "ds-1", []string{"doc-1"}); err == nil || !strings.Contains(err.Error(), "read ragflow response") {
		t.Fatalf("ParseDocuments() error = %v", err)
	}
}

type unserializable struct {
	Ch chan int `json:"ch"`
}

// 请求体不可序列化 → marshal 错误分支。
func TestDoFailsOnUnserializableBody(t *testing.T) {
	client := NewClient(&Config{BaseURL: "http://127.0.0.1:1"})
	if err := client.do(context.Background(), http.MethodPost, "/api/v1/retrieval", unserializable{}, nil); err == nil || !strings.Contains(err.Error(), "marshal ragflow request") {
		t.Fatalf("do() error = %v", err)
	}
}

// 坏 BaseURL → 请求构造错误分支。
func TestDoFailsOnInvalidBaseURL(t *testing.T) {
	client := NewClient(&Config{BaseURL: "ht tp://bad url"})
	if err := client.do(context.Background(), http.MethodGet, "/api/v1/datasets", nil, nil); err == nil || !strings.Contains(err.Error(), "create ragflow request") {
		t.Fatalf("do() error = %v", err)
	}
}

// 已取消 context → 发送错误分支。
func TestDoFailsOnCancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL, Timeout: 5 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.GetDataset(ctx, "ds-1"); err == nil || !strings.Contains(err.Error(), "send ragflow request") {
		t.Fatalf("GetDataset() error = %v", err)
	}
}

// 上传路径的请求构造错误分支（坏 BaseURL）。
func TestUploadDocumentFailsOnInvalidBaseURL(t *testing.T) {
	client := NewClient(&Config{BaseURL: "ht tp://bad url"})
	if _, err := client.UploadDocument(context.Background(), "ds-1", "name", "content"); err == nil || !strings.Contains(err.Error(), "create ragflow request") {
		t.Fatalf("UploadDocument() error = %v", err)
	}
}

// 上传路径的发送错误分支（已取消 context）。
func TestUploadDocumentFailsOnCancelledContext(t *testing.T) {
	client := NewClient(&Config{BaseURL: "http://127.0.0.1:1"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.UploadDocument(ctx, "ds-1", "name", "content"); err == nil || !strings.Contains(err.Error(), "send ragflow request") {
		t.Fatalf("UploadDocument() error = %v", err)
	}
}

// api key 为空时不发 Authorization 头。
func TestNoAuthHeaderWhenAPIKeyEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected auth header %q", r.Header.Get("Authorization"))
		}
		_, _ = ioWriteString(w, okEnvelope(`[]`))
	}))
	defer server.Close()

	client := NewClient(&Config{BaseURL: server.URL})
	if _, err := client.GetDataset(context.Background(), "ds-1"); err == nil {
		t.Fatalf("expected not-found error, got nil")
	}
}

func ioWriteString(w http.ResponseWriter, s string) (int, error) {
	return w.Write([]byte(s))
}
