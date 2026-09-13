package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
)

type AIService struct {
	openAIAPIKey  string
	openAIBaseURL string
	client        *http.Client
	knowledgeBase *KnowledgeBase
}

type KnowledgeBase struct {
	documents []models.KnowledgeDoc
	// 在实际项目中，这里会连接向量数据库
}

type OpenAIRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

type AIResponse struct {
	Content    string  `json:"content"`
	Confidence float64 `json:"confidence"`
	Source     string  `json:"source"`
}

func NewAIService(apiKey, baseURL string) *AIService {
	return &AIService{
		openAIAPIKey:  apiKey,
		openAIBaseURL: baseURL,
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		knowledgeBase: &KnowledgeBase{
			documents: []models.KnowledgeDoc{},
		},
	}
}

func (s *AIService) ProcessQuery(ctx context.Context, query string, sessionID string) (*AIResponse, error) {
	// 1. 检查是否需要从知识库搜索
	relevantDocs := s.knowledgeBase.Search(query, 3)

	// 2. 构建提示词
	prompt := s.buildPrompt(query, relevantDocs)

	// 3. 调用 OpenAI API
	response, err := s.callOpenAI(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to call OpenAI: %w", err)
	}

	// 4. 处理响应
	aiResponse := &AIResponse{
		Content:    response,
		Confidence: 0.8, // 简单的置信度，实际项目中需要更复杂的计算
		Source:     "ai",
	}

	return aiResponse, nil
}

func (s *AIService) buildPrompt(query string, docs []models.KnowledgeDoc) string {
	context := ""
	if len(docs) > 0 {
		context = "基于以下知识库内容回答问题：\n"
		for _, doc := range docs {
			context += fmt.Sprintf("- %s: %s\n", doc.Title, doc.Content)
		}
		context += "\n"
	}

	prompt := fmt.Sprintf(`%s你是一个智能客服助手，请根据用户的问题提供准确、友好的回答。

用户问题：%s

请用中文回答，保持专业和友好的语气。如果无法找到相关信息，请礼貌地说明。`, context, query)

	return prompt
}

func (s *AIService) callOpenAI(ctx context.Context, prompt string) (string, error) {
	tracer := otel.Tracer("servify/ai")
	ctx, span := tracer.Start(ctx, "AIService.callOpenAI")
	span.SetAttributes(attribute.String("model", config.DefaultOpenAIModel))
	defer span.End()

	if s.openAIAPIKey == "" {
		return s.getFallbackResponse(prompt), nil
	}

	request := OpenAIRequest{
		Model: config.DefaultOpenAIModel,
		Messages: []Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature: 0.7,
		MaxTokens:   1000,
	}

	// OpenAIRequest/Message 仅含字符串与数值字段，json.Marshal 不可能失败，
	// 错误分支为不可达死代码。
	reqBody, _ := json.Marshal(request)

	url := fmt.Sprintf("%s/chat/completions", s.openAIBaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.openAIAPIKey))

	resp, err := s.client.Do(req)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var openAIResp OpenAIResponse
	if err := json.Unmarshal(body, &openAIResp); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if openAIResp.Error != nil {
		span.SetStatus(codes.Error, openAIResp.Error.Message)
		return "", fmt.Errorf("OpenAI API error: %s", openAIResp.Error.Message)
	}

	if len(openAIResp.Choices) == 0 {
		span.SetStatus(codes.Error, "no response choices")
		return "", fmt.Errorf("no response from OpenAI")
	}

	return openAIResp.Choices[0].Message.Content, nil
}

func (s *AIService) getFallbackResponse(query string) string {
	// 简单的规则基础回复，当没有 OpenAI API 时使用
	query = strings.ToLower(query)

	if strings.Contains(query, "你好") || strings.Contains(query, "hello") {
		return "您好！我是智能客服助手，很高兴为您服务。请问有什么可以帮助您的吗？"
	}

	if strings.Contains(query, "帮助") || strings.Contains(query, "help") {
		return "我可以帮助您解答问题、提供产品信息和技术支持。如果需要更专业的帮助，我也可以为您转接人工客服。"
	}

	if strings.Contains(query, "谢谢") || strings.Contains(query, "thank") {
		return "不客气！如果您还有其他问题，请随时告诉我。"
	}

	return "感谢您的咨询！我已经收到您的问题，正在为您查找相关信息。如果需要更详细的帮助，我可以为您转接人工客服。"
}

func (s *AIService) GetStatus(ctx context.Context) map[string]interface{} {
	documentCount := 0
	knowledgeProviderEnabled := false
	knowledgeProvider := ""
	knowledgeMode := "stateless"
	if s.knowledgeBase != nil {
		documentCount = len(s.knowledgeBase.documents)
		knowledgeProviderEnabled = true
		knowledgeProvider = "embedded"
		knowledgeMode = "embedded"
	}

	return map[string]interface{}{
		"type":                       "standard",
		"openai_enabled":             s.openAIAPIKey != "",
		"knowledge_provider":         knowledgeProvider,
		"knowledge_provider_enabled": knowledgeProviderEnabled,
		"knowledge_mode":             knowledgeMode,
		"document_count":             documentCount,
	}
}

// 知识库搜索功能
func (kb *KnowledgeBase) Search(query string, limit int) []models.KnowledgeDoc {
	var results []models.KnowledgeDoc
	query = strings.ToLower(query)

	for _, doc := range kb.documents {
		if strings.Contains(strings.ToLower(doc.Content), query) ||
			strings.Contains(strings.ToLower(doc.Title), query) {
			results = append(results, doc)
			if len(results) >= limit {
				break
			}
		}
	}

	return results
}

// 添加知识库文档
func (kb *KnowledgeBase) AddDocument(doc models.KnowledgeDoc) {
	kb.documents = append(kb.documents, doc)
}

// 初始化默认知识库
func (s *AIService) InitializeKnowledgeBase() {
	defaultDocs := []models.KnowledgeDoc{
		{
			Title:    "产品介绍",
			Content:  "Servify 是一个基于 WebRTC 的智能客服系统，支持文字聊天、语音通话和远程协助功能。",
			Category: "产品",
			Tags:     "介绍,功能",
		},
		{
			Title:    "技术支持",
			Content:  "如果您遇到技术问题，可以通过以下方式联系我们：1. 在线客服 2. 邮件支持 3. 电话支持",
			Category: "支持",
			Tags:     "技术,支持,联系",
		},
		{
			Title:    "远程协助",
			Content:  "远程协助功能允许客服人员远程查看和控制您的屏幕，以便更好地解决技术问题。使用前请确保您同意屏幕共享。",
			Category: "功能",
			Tags:     "远程,协助,屏幕共享",
		},
	}

	for _, doc := range defaultDocs {
		s.knowledgeBase.AddDocument(doc)
	}

	logrus.Info("Knowledge base initialized with default documents")
}

// 判断是否需要转人工客服
func (s *AIService) ShouldTransferToHuman(query string, sessionHistory []models.Message) bool {
	query = strings.ToLower(query)

	// 关键词判断
	humanKeywords := []string{"人工", "客服", "转人工", "manual", "human", "agent"}
	for _, keyword := range humanKeywords {
		if strings.Contains(query, keyword) {
			return true
		}
	}

	// 复杂问题判断
	if strings.Contains(query, "投诉") || strings.Contains(query, "complaint") {
		return true
	}

	// 会话历史判断 - 如果用户多次询问同一类问题
	if len(sessionHistory) > 5 {
		return true
	}

	return false
}

// 获取会话摘要
func (s *AIService) GetSessionSummary(messages []models.Message) (string, error) {
	if len(messages) == 0 {
		return "空会话", nil
	}

	// 构建会话内容
	conversation := "会话内容：\n"
	for _, msg := range messages {
		conversation += fmt.Sprintf("%s: %s\n", msg.Sender, msg.Content)
	}

	prompt := fmt.Sprintf("请为以下客服会话提供简洁的摘要：\n%s\n\n请用中文总结主要问题和解决方案。", conversation)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	summary, err := s.callOpenAI(ctx, prompt)
	if err != nil {
		logrus.Errorf("Failed to generate session summary: %v", err)
		return "无法生成会话摘要", nil
	}

	return summary, nil
}
