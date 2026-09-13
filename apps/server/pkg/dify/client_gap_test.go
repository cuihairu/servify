package dify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClientCreateDocumentFromTextServerError 覆盖 create-by-text 的 HTTP 错误分支。
func TestClientCreateDocumentFromTextServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(&Config{BaseURL: srv.URL, APIKey: "k"})
	doc, err := client.CreateDocumentFromText(context.Background(), "ds1", &CreateDocumentRequest{
		Name: "doc", Text: "content",
	})
	if err == nil {
		t.Fatalf("expected error, got doc %+v", doc)
	}
}

// TestClientCreateDocumentFromTextSuccess 覆盖成功映射。
func TestClientCreateDocumentFromTextSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/datasets/ds1/document/create-by-text" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"document":{"id":"doc-9","name":"Doc9"}}`))
	}))
	defer srv.Close()

	client := NewClient(&Config{BaseURL: srv.URL, APIKey: "k"})
	doc, err := client.CreateDocumentFromText(context.Background(), "ds1", &CreateDocumentRequest{Name: "x", Text: "y"})
	if err != nil {
		t.Fatalf("CreateDocumentFromText() error = %v", err)
	}
	if doc == nil || doc.ID != "doc-9" || doc.Name != "Doc9" {
		t.Fatalf("doc = %+v", doc)
	}
}

// TestClientDoTruncatedResponseBody 覆盖读响应体失败分支:
// 服务端声明的 ContentLength 大于实际写出的字节,提前断开连接,
// 客户端 ReadAll 时得到 unexpected EOF。
func TestClientDoTruncatedResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"`))
		w.(http.Flusher).Flush()
		conn, _, _ := w.(http.Hijacker).Hijack()
		if conn != nil {
			_ = conn.Close()
		}
	}))
	defer srv.Close()

	client := NewClient(&Config{BaseURL: srv.URL})
	if _, err := client.GetDataset(context.Background(), "ds1"); err == nil {
		t.Fatal("expected truncated response body error")
	}
}

// TestClientDoEmptyBodyResponse 覆盖空响应体直接返回的分支。
func TestClientDoEmptyBodyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := NewClient(&Config{BaseURL: srv.URL})
	if err := client.DeleteDocument(context.Background(), "ds1", "doc1"); err != nil {
		t.Fatalf("DeleteDocument() error = %v", err)
	}
}
