package services

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// failingBodyTransport 返回一个读取必然失败的响应体，驱动 callOpenAI 的
// io.ReadAll 错误透传分支。
type failingBodyTransport struct{}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("connection reset mid-body") }
func (failingBody) Close() error             { return nil }

func (failingBodyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(failingBody{}),
		Header:     make(http.Header),
	}, nil
}

func TestAIService_CallOpenAI_BodyReadError(t *testing.T) {
	svc := NewAIService("test-key", "http://openai.invalid")
	svc.client = &http.Client{Transport: failingBodyTransport{}}

	_, err := svc.callOpenAI(context.Background(), "prompt")
	if err == nil || !strings.Contains(err.Error(), "failed to read response") {
		t.Fatalf("err = %v, want body read failure", err)
	}
}
