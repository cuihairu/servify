package services

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"
	"servify/apps/server/internal/models"
	"servify/apps/server/pkg/weknora"
)

// MockWeKnoraClient 用于测试的 WeKnora 客户端模拟
type MockWeKnoraClient struct {
	searchResults []weknora.SearchResult
	searchError   error
	uploadError   error
}

func (m *MockWeKnoraClient) CreateSession(ctx context.Context, req *weknora.SessionRequest) (*weknora.Session, error) {
	return &weknora.Session{
		ID:     "test-session",
		UserID: req.UserID,
	}, nil
}

func (m *MockWeKnoraClient) SearchKnowledge(ctx context.Context, req *weknora.SearchRequest) (*weknora.SearchResponse, error) {
	if m.searchError != nil {
		return nil, m.searchError
	}

	results := m.searchResults
	if results == nil {
		// 默认返回测试结果
		results = []weknora.SearchResult{
			{
				Title:      "Test Document 1",
				Content:    "Test content 1",
				Score:      0.9,
				DocumentID: "doc-1",
			},
		}
	}

	return &weknora.SearchResponse{
		Success: true,
		Data: weknora.SearchData{
			Results: results,
			Total:   len(results),
		},
	}, nil
}

func (m *MockWeKnoraClient) UploadDocument(ctx context.Context, kbID string, doc *weknora.Document) (*weknora.DocumentInfo, error) {
	if m.uploadError != nil {
		return nil, m.uploadError
	}

	return &weknora.DocumentInfo{
		ID:     "doc-123",
		Title:  doc.Title,
		Status: "active",
	}, nil
}

func (m *MockWeKnoraClient) DeleteDocument(ctx context.Context, kbID, docID string) error {
	return nil
}

func (m *MockWeKnoraClient) CreateKnowledgeBase(ctx context.Context, req *weknora.CreateKBRequest) (*weknora.KnowledgeBase, error) {
	return &weknora.KnowledgeBase{
		ID:          "kb-123",
		Name:        req.Name,
		Description: req.Description,
	}, nil
}

func (m *MockWeKnoraClient) GetKnowledgeBase(ctx context.Context, id string) (*weknora.KnowledgeBase, error) {
	return &weknora.KnowledgeBase{
		ID:   id,
		Name: "Test KB",
	}, nil
}

func (m *MockWeKnoraClient) Chat(ctx context.Context, sessionID string, req *weknora.ChatRequest) (*weknora.ChatResponse, error) {
	return &weknora.ChatResponse{
		Response: "Test response",
	}, nil
}

func (m *MockWeKnoraClient) HealthCheck(ctx context.Context) error {
	return nil
}

func TestEnhancedAI_FallbackFlow_NoWeKnora(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	enh := NewEnhancedAIService(base, nil, "", nil)

	ctx := context.Background()
	resp, err := enh.ProcessQueryEnhanced(ctx, "介绍一下Servify", "s1")
	if err != nil {
		t.Fatalf("ProcessQueryEnhanced error: %v", err)
	}
	if resp == nil || resp.AIResponse == nil || resp.Content == "" {
		t.Fatalf("expected non-empty content")
	}
	if resp.Strategy == "" {
		t.Fatalf("expected non-empty strategy")
	}
}

func TestEnhancedAIService_UploadDocumentToWeKnora_Success(t *testing.T) {
	base := NewAIService("", "")
	mockClient := &MockWeKnoraClient{}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	enh := NewEnhancedAIService(base, mockClient, "kb-test", logger)

	err := enh.UploadDocumentToWeKnora(context.Background(), "Test Title", "Test Content", []string{"test", "doc"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnhancedAIService_UploadKnowledgeDocument_Success(t *testing.T) {
	base := NewAIService("", "")
	mockClient := &MockWeKnoraClient{}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	enh := NewEnhancedAIService(base, mockClient, "kb-test", logger)

	err := enh.UploadKnowledgeDocument(context.Background(), "Test Title", "Test Content", []string{"test", "doc"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnhancedAIService_UploadDocumentToWeKnora_Error(t *testing.T) {
	base := NewAIService("", "")
	mockClient := &MockWeKnoraClient{
		uploadError: context.DeadlineExceeded,
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	enh := NewEnhancedAIService(base, mockClient, "kb-test", logger)

	err := enh.UploadDocumentToWeKnora(context.Background(), "Test Title", "Test Content", []string{"test"})

	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestEnhancedAIService_UploadDocumentToWeKnora_Disabled(t *testing.T) {
	base := NewAIService("", "")
	mockClient := &MockWeKnoraClient{}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	enh := NewEnhancedAIService(base, mockClient, "kb-test", logger)
	enh.SetWeKnoraEnabled(false)

	err := enh.UploadDocumentToWeKnora(context.Background(), "Test Title", "Test Content", []string{"test"})

	// 当WeKnora禁用时应该返回错误
	if err == nil {
		t.Error("expected error when WeKnora disabled, got nil")
	}
}

func TestEnhancedAIService_GetMetrics(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	mockClient := &MockWeKnoraClient{}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	enh := NewEnhancedAIService(base, mockClient, "kb-test", logger)

	// 执行一些查询以生成metrics
	_, _ = enh.ProcessQueryEnhanced(context.Background(), "测试", "session-1")
	_, _ = enh.ProcessQueryEnhanced(context.Background(), "转人工", "session-2")

	metrics := enh.GetMetrics()

	if metrics == nil {
		t.Fatal("expected metrics, got nil")
	}

	if metrics.QueryCount != 2 {
		t.Errorf("expected QueryCount 2, got %d", metrics.QueryCount)
	}
}

func TestEnhancedAIService_SyncKnowledgeBase(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	mockClient := &MockWeKnoraClient{}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	enh := NewEnhancedAIService(base, mockClient, "kb-test", logger)

	// 添加一些测试文档到知识库
	base.knowledgeBase.AddDocument(models.KnowledgeDoc{
		Title:   "Test Doc",
		Content: "Test content",
	})

	err := enh.SyncKnowledgeBase(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnhancedAIService_SyncKnowledgeBase_Disabled(t *testing.T) {
	base := NewAIService("", "")
	base.InitializeKnowledgeBase()
	mockClient := &MockWeKnoraClient{}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	enh := NewEnhancedAIService(base, mockClient, "kb-test", logger)
	enh.SetWeKnoraEnabled(false)

	// 添加一些测试文档到知识库
	base.knowledgeBase.AddDocument(models.KnowledgeDoc{
		Title:   "Test Doc",
		Content: "Test content",
	})

	err := enh.SyncKnowledgeBase(context.Background())

	// 当WeKnora禁用时应该返回错误
	if err == nil {
		t.Error("expected error when WeKnora disabled, got nil")
	}
}
