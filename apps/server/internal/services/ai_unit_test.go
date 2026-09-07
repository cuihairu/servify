package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"servify/apps/server/internal/models"
	"servify/apps/server/pkg/weknora"

	"github.com/sirupsen/logrus"
)

func TestAIService_CallOpenAI_HTTP(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
		want    string
	}{
		{"success", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"the answer"}}]}`))
		}, false, "the answer"},
		{"api error field", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]string{"message": "quota exceeded", "type": "insufficient_quota"},
			})
		}, true, ""},
		{"no choices", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"choices":[]}`))
		}, true, ""},
		{"bad json", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("not-json"))
		}, true, ""},
	}

	for _, tc := range cases {
		server := httptest.NewServer(tc.handler)
		svc := NewAIService("test-key", server.URL)
		got, err := svc.callOpenAI(context.Background(), "prompt")
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error", tc.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: got %q", tc.name, got)
		}
		server.Close()
	}
}

func TestAIService_CallOpenAI_RequestFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close() // closed server forces client.Do error

	svc := NewAIService("test-key", url)
	if _, err := svc.callOpenAI(context.Background(), "prompt"); err == nil {
		t.Fatal("expected request error")
	}
}

func TestAIService_CallOpenAI_FallbackWithoutKey(t *testing.T) {
	svc := NewAIService("", "https://invalid.example")
	got, err := svc.callOpenAI(context.Background(), "你好")
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if got == "" {
		t.Fatal("expected fallback response")
	}
}

func TestAIService_ProcessQuery_WithServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req OpenAIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.Messages[0].Content == "" {
			t.Error("expected prompt in message")
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"answer"}}]}`))
	}))
	defer server.Close()

	svc := NewAIService("key", server.URL)
	svc.knowledgeBase.AddDocument(models.KnowledgeDoc{Title: "Doc", Content: "body"})

	resp, err := svc.ProcessQuery(context.Background(), "doc question", "sess")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if resp.Content != "answer" || resp.Source != "ai" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestAIService_GetSessionSummary_WithServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"summary text"}}]}`))
	}))
	defer server.Close()

	svc := NewAIService("key", server.URL)
	summary, err := svc.GetSessionSummary([]models.Message{{Sender: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("GetSessionSummary: %v", err)
	}
	if summary != "summary text" {
		t.Fatalf("unexpected summary: %q", summary)
	}

	// error path falls back to canned text
	svc = NewAIService("key", "https://invalid.example")
	fallback, err := svc.GetSessionSummary([]models.Message{{Sender: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("GetSessionSummary fallback err: %v", err)
	}
	if fallback != "无法生成会话摘要" {
		t.Fatalf("unexpected fallback: %q", fallback)
	}
	if summary, err := svc.GetSessionSummary(nil); err != nil || summary != "空会话" {
		t.Fatalf("empty summary: %q %v", summary, err)
	}
}

func TestKnowledgeBase_Search_LimitAndNoMatch(t *testing.T) {
	kb := &KnowledgeBase{}
	kb.AddDocument(models.KnowledgeDoc{Title: "alpha", Content: "needle one"})
	kb.AddDocument(models.KnowledgeDoc{Title: "needle two", Content: "body"})
	kb.AddDocument(models.KnowledgeDoc{Title: "beta", Content: "other"})

	if got := kb.Search("needle", 1); len(got) != 1 {
		t.Fatalf("limit not respected: %+v", got)
	}
	if got := kb.Search("missing", 5); len(got) != 0 {
		t.Fatalf("expected no results: %+v", got)
	}
	if got := kb.Search("", 2); len(got) != 2 {
		t.Fatalf("empty query matches up to limit: %+v", got)
	}
}

// ---- EnhancedAIService extra coverage ----

type healthFailingWeKnoraClient struct {
	*MockWeKnoraClient
	healthErr error
}

func (m *healthFailingWeKnoraClient) HealthCheck(ctx context.Context) error {
	return m.healthErr
}

func TestEnhancedAIService_ProcessQueryEnhanced_Paths(t *testing.T) {
	ctx := context.Background()

	t.Run("transfer shortcut", func(t *testing.T) {
		base := NewAIService("", "")
		enh := NewEnhancedAIService(base, nil, "", logrus.New())
		resp, err := enh.ProcessQueryEnhanced(ctx, "转人工", "s")
		if err != nil {
			t.Fatalf("transfer: %v", err)
		}
		if resp.Strategy != "transfer" || resp.Confidence != 1.0 {
			t.Fatalf("unexpected transfer response: %+v", resp)
		}
	})

	t.Run("weknora success", func(t *testing.T) {
		base := NewAIService("", "")
		enh := NewEnhancedAIService(base, &MockWeKnoraClient{}, "kb", logrus.New())
		resp, err := enh.ProcessQueryEnhanced(ctx, "billing", "s")
		if err != nil {
			t.Fatalf("weknora: %v", err)
		}
		if resp.Strategy != "weknora" || len(resp.Sources) != 1 {
			t.Fatalf("unexpected weknora response: %+v", resp)
		}
		m := enh.GetMetrics()
		if m.WeKnoraUsageCount != 1 || m.KnowledgeProviderUsageCount != 1 || m.SuccessCount != 1 {
			t.Fatalf("unexpected metrics: %+v", m)
		}
	})

	t.Run("weknora failure falls back", func(t *testing.T) {
		base := NewAIService("", "")
		base.InitializeKnowledgeBase()
		client := &MockWeKnoraClient{searchError: context.DeadlineExceeded}
		enh := NewEnhancedAIService(base, client, "kb", logrus.New())
		resp, err := enh.ProcessQueryEnhanced(ctx, "产品", "s")
		if err != nil {
			t.Fatalf("fallback: %v", err)
		}
		if resp.Strategy != "fallback" {
			t.Fatalf("expected fallback strategy, got %q", resp.Strategy)
		}
		if enh.circuitBreaker.FailureCount() != 1 {
			t.Fatalf("expected circuit breaker failure recorded")
		}
	})
}

func TestEnhancedAIService_SearchWithWeKnora_APIError(t *testing.T) {
	base := NewAIService("", "")
	client := &MockWeKnoraClient{}
	client.searchResults = nil
	enh := NewEnhancedAIService(base, client, "kb", logrus.New())

	// Success=false surfaces as an API error
	failing := &unsuccessfulWeKnoraClient{}
	enh2 := NewEnhancedAIService(base, failing, "kb", logrus.New())
	if _, err := enh2.searchWithWeKnora(context.Background(), "q"); err == nil {
		t.Fatal("expected API error")
	}
	if _, err := enh.searchWithWeKnora(context.Background(), "q"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

type unsuccessfulWeKnoraClient struct {
	*MockWeKnoraClient
}

func (u *unsuccessfulWeKnoraClient) SearchKnowledge(ctx context.Context, req *weknora.SearchRequest) (*weknora.SearchResponse, error) {
	return &weknora.SearchResponse{Success: false, Message: "down"}, nil
}

func TestEnhancedAIService_RetrieveKnowledge_NoSources(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	client := &MockWeKnoraClient{}
	client.searchResults = []weknora.SearchResult{}
	enh := NewEnhancedAIService(base, client, "kb", logrus.New())

	// provider returns zero docs -> falls through to legacy KB
	docs, strategy, err := enh.retrieveKnowledge(context.Background(), "产品")
	if err != nil {
		t.Fatalf("retrieveKnowledge: %v", err)
	}
	if strategy != "fallback" || len(docs) == 0 {
		t.Fatalf("unexpected retrieve: %q %d", strategy, len(docs))
	}
}

func TestEnhancedAIService_RetrieveKnowledge_AllDisabled(t *testing.T) {
	base := NewAIService("", "")
	enh := NewEnhancedAIService(base, nil, "kb", logrus.New())
	enh.SetWeKnoraEnabled(false)
	enh.SetFallbackEnabled(false)

	if _, _, err := enh.retrieveKnowledge(context.Background(), "q"); err == nil {
		t.Fatal("expected all-sources-unavailable error")
	}
}

func TestEnhancedAIService_GetStatus_HealthVariants(t *testing.T) {
	base := NewAIService("", "")

	// healthy provider
	enh := NewEnhancedAIService(base, &MockWeKnoraClient{}, "kb", logrus.New())
	st := enh.GetStatus(context.Background())
	if st["knowledge_provider_healthy"] != true {
		t.Fatalf("expected healthy provider: %+v", st)
	}

	// failing health check
	failing := &healthFailingWeKnoraClient{MockWeKnoraClient: &MockWeKnoraClient{}, healthErr: context.DeadlineExceeded}
	enh2 := NewEnhancedAIService(base, failing, "kb", logrus.New())
	st2 := enh2.GetStatus(context.Background())
	if st2["knowledge_provider_healthy"] != false {
		t.Fatalf("expected unhealthy provider: %+v", st2)
	}
	if st2["knowledge_provider_error"] == nil {
		t.Fatal("expected error detail")
	}

	// enabled flag with nil client (constructed manually)
	enh3 := NewEnhancedAIService(base, nil, "kb", logrus.New())
	enh3.SetKnowledgeProviderEnabled(true)
	st3 := enh3.GetStatus(context.Background())
	if st3["knowledge_provider_healthy"] != false {
		t.Fatalf("expected unhealthy nil client: %+v", st3)
	}
}
