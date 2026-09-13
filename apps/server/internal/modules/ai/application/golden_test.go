package application

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/platform/knowledgeprovider"
	mockkp "servify/apps/server/internal/platform/knowledgeprovider/mock"
	"servify/apps/server/internal/platform/llm"
	mockllm "servify/apps/server/internal/platform/llm/mock"
	"servify/apps/server/internal/platform/llm/openai"
)

// goldenSet 用例固化 AI 编排链路的关键行为：prompt 组装、知识注入、
// 来源透传、模型参数透传、guardrails 拒绝与转人工启发。mock 模式
// 零网络随常规 go test 必跑；SERVIFY_GOLDEN_MODE=real 时换真实 LLM
// 只保留稳定断言（见 testdata/golden/README.md）。

type goldenRetrieval struct {
	Enabled bool `json:"enabled"`
	TopK    int  `json:"top_k"`
}

type goldenMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type goldenRequest struct {
	TaskType     string          `json:"task_type"`
	Query        string          `json:"query"`
	Model        string          `json:"model"`
	Temperature  float64         `json:"temperature"`
	SystemPrompt string          `json:"system_prompt"`
	Retrieval    goldenRetrieval `json:"retrieval"`
	Messages     []goldenMessage `json:"messages"`
}

type goldenExpect struct {
	ContentContains    []string `json:"content_contains"`
	ContentNotContains []string `json:"content_not_contains"`
	SourceTitles       []string `json:"source_titles"`
	PromptContains     []string `json:"prompt_contains"`
	PromptNotContains  []string `json:"prompt_not_contains"`
	Handoff            bool     `json:"handoff"`
	ErrorContains      []string `json:"error_contains"`
}

type goldenCase struct {
	Name          string                           `json:"name"`
	Description   string                           `json:"description"`
	Request       goldenRequest                    `json:"request"`
	KnowledgeHits []knowledgeprovider.KnowledgeHit `json:"knowledge_hits"`
	MockReply     string                           `json:"mock_reply"`
	Expect        goldenExpect                     `json:"expect"`
}

func loadGoldenCases(t *testing.T) []goldenCase {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "golden", "*.json"))
	if err != nil {
		t.Fatalf("glob golden cases: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no golden cases found under testdata/golden")
	}
	seen := make(map[string]struct{}, len(paths))
	cases := make([]goldenCase, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var tc goldenCase
		if err := json.Unmarshal(data, &tc); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if tc.Name == "" {
			t.Fatalf("%s: golden case missing name", path)
		}
		if _, dup := seen[tc.Name]; dup {
			t.Fatalf("%s: duplicate golden case name %q", path, tc.Name)
		}
		seen[tc.Name] = struct{}{}
		cases = append(cases, tc)
	}
	return cases
}

func (r goldenRequest) toAIRequest() AIRequest {
	req := AIRequest{
		TaskType:     TaskType(r.TaskType),
		Query:        r.Query,
		Model:        r.Model,
		Temperature:  r.Temperature,
		SystemPrompt: r.SystemPrompt,
		RetrievalPolicy: RetrievalPolicy{
			Enabled: r.Retrieval.Enabled,
			TopK:    r.Retrieval.TopK,
		},
	}
	for _, msg := range r.Messages {
		req.Messages = append(req.Messages, llm.ChatMessage{Role: msg.Role, Content: msg.Content})
	}
	return req
}

func (r goldenRequest) historyMessages() []models.Message {
	history := make([]models.Message, 0, len(r.Messages))
	for _, msg := range r.Messages {
		history = append(history, models.Message{Sender: msg.Role, Content: msg.Content})
	}
	return history
}

func assertSubstrings(t *testing.T, label, haystack string, needles []string, wantPresent bool) {
	t.Helper()
	for _, needle := range needles {
		if strings.Contains(haystack, needle) != wantPresent {
			if wantPresent {
				t.Errorf("%s missing expected substring %q", label, needle)
			} else {
				t.Errorf("%s contains unexpected substring %q", label, needle)
			}
		}
	}
}

func TestGoldenSet(t *testing.T) {
	cases := loadGoldenCases(t)

	realMode := os.Getenv("SERVIFY_GOLDEN_MODE") == "real"
	var realProvider llm.LLMProvider
	if realMode {
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			t.Skip("SERVIFY_GOLDEN_MODE=real but OPENAI_API_KEY is not set; skipping golden set")
		}
		realProvider = openai.NewProvider(apiKey, os.Getenv("OPENAI_BASE_URL"))
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			mockProvider := &mockllm.Provider{
				ChatResponse: llm.ChatResponse{
					Content:      tc.MockReply,
					Model:        tc.Request.Model,
					FinishReason: "stop",
				},
			}
			provider := llm.LLMProvider(mockProvider)
			if realMode {
				provider = realProvider
			}

			orchestrator := NewQueryOrchestrator(provider, &mockkp.Provider{Hits: tc.KnowledgeHits})
			resp, err := orchestrator.Handle(context.Background(), tc.Request.toAIRequest())

			if len(tc.Expect.ErrorContains) > 0 {
				if err == nil {
					t.Fatalf("expected error containing %v, got resp=%+v", tc.Expect.ErrorContains, resp)
				}
				assertSubstrings(t, "error", err.Error(), tc.Expect.ErrorContains, true)
				return
			}
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if resp == nil {
				t.Fatal("Handle() returned nil response without error")
			}

			assertSubstrings(t, "content", resp.Content, tc.Expect.ContentContains, true)
			assertSubstrings(t, "content", resp.Content, tc.Expect.ContentNotContains, false)

			var gotTitles []string
			for _, hit := range resp.Sources {
				gotTitles = append(gotTitles, hit.Title)
			}
			if len(gotTitles) != len(tc.Expect.SourceTitles) {
				t.Fatalf("sources = %v want %v", gotTitles, tc.Expect.SourceTitles)
			}
			for i, want := range tc.Expect.SourceTitles {
				if gotTitles[i] != want {
					t.Fatalf("source[%d] = %q want %q (full: %v)", i, gotTitles[i], want, gotTitles)
				}
			}

			if got := ShouldTransferToHuman(tc.Request.Query, tc.Request.historyMessages()); got != tc.Expect.Handoff {
				t.Errorf("ShouldTransferToHuman(%q) = %v want %v", tc.Request.Query, got, tc.Expect.Handoff)
			}

			// prompt 组装与参数透传只有在 mock provider 上可观测。
			if !realMode {
				requests := mockProvider.RecordedRequests()
				if len(requests) == 0 {
					t.Fatal("mock provider received no chat requests")
				}
				var prompt strings.Builder
				for _, msg := range requests[0].Messages {
					prompt.WriteString(msg.Content)
					prompt.WriteString("\n")
				}
				assertSubstrings(t, "prompt", prompt.String(), tc.Expect.PromptContains, true)
				assertSubstrings(t, "prompt", prompt.String(), tc.Expect.PromptNotContains, false)

				for i, sent := range requests {
					if sent.Model != tc.Request.Model {
						t.Errorf("chat request[%d].model = %q want %q", i, sent.Model, tc.Request.Model)
					}
					if sent.Temperature != tc.Request.Temperature {
						t.Errorf("chat request[%d].temperature = %v want %v", i, sent.Temperature, tc.Request.Temperature)
					}
				}
			}
		})
	}
}

// TestGoldenSetCaseCount 保证目录里至少覆盖既有关键场景，防止用例被
// 静默清空后回归防线失效。
func TestGoldenSetCaseCount(t *testing.T) {
	cases := loadGoldenCases(t)
	if len(cases) < 8 {
		t.Fatalf("golden set has %d cases, want >= 8", len(cases))
	}
}
