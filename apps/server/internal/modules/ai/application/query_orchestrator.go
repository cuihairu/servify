package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"servify/apps/server/internal/platform/knowledgeprovider"
	"servify/apps/server/internal/platform/llm"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// QueryOrchestrator coordinates retrieval and model execution.
type QueryOrchestrator struct {
	llmProvider   llm.LLMProvider
	retriever     *Retriever
	promptBuilder *PromptBuilder
	guardrails    *Guardrails
	metrics       *Metrics
	tracer        trace.Tracer
	policyHooks   []PolicyHook
	auditRecorder PromptAuditRecorder
	toolExecutor  *ToolExecutor
}

func NewQueryOrchestrator(llmProvider llm.LLMProvider, knowledgeProvider knowledgeprovider.KnowledgeProvider) *QueryOrchestrator {
	return &QueryOrchestrator{
		llmProvider:   llmProvider,
		retriever:     NewRetriever(knowledgeProvider),
		promptBuilder: NewPromptBuilder(),
		guardrails:    NewGuardrails(),
		metrics:       NewMetrics(),
		tracer:        otel.Tracer("servify.ai.orchestrator"),
	}
}

func (o *QueryOrchestrator) Handle(ctx context.Context, req AIRequest) (*AIResponse, error) {
	ctx, span := o.tracer.Start(ctx, "ai.query_orchestrator.handle")
	defer span.End()

	span.SetAttributes(
		attribute.String("ai.task_type", string(req.TaskType)),
		attribute.Bool("ai.retrieval.enabled", req.RetrievalPolicy.Enabled),
	)

	start := time.Now()
	o.metrics.RecordQuery()
	if o.llmProvider == nil {
		return nil, nil
	}
	for _, hook := range o.policyHooks {
		decision, err := hook.Evaluate(ctx, req)
		if err != nil {
			o.metrics.RecordError("policy", "policy_hook_error")
			o.metrics.RecordFallback()
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		if !decision.Allowed {
			o.metrics.RecordPolicyRejection(decision.Reason)
			o.metrics.RecordFallback()
			span.SetStatus(codes.Error, decision.Reason)
			return nil, context.Canceled
		}
	}
	if err := o.guardrails.ValidateInput(req); err != nil {
		o.metrics.RecordPolicyRejection("guardrails")
		o.metrics.RecordFallback()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	hits, err := o.retriever.Retrieve(ctx, req)
	if err != nil {
		o.metrics.RecordError("knowledge", "retrieval")
		o.metrics.RecordFallback()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	messages := o.promptBuilder.Build(req, hits)
	if o.auditRecorder != nil {
		_ = o.auditRecorder.RecordPrompt(ctx, PromptAuditRecord{
			PromptVersion:   "v1",
			TaskType:        req.TaskType,
			MessageCount:    len(messages),
			RetrievalHits:   len(hits),
			SystemPromptSet: req.SystemPrompt != "",
		})
	}

	var (
		chatContent      string
		chatFinishReason string
		chatTokenUsage   *llm.TokenUsage
		chatModel        string
		chatProvider     string
	)

	if req.ToolPolicy.Enabled && o.toolExecutor != nil {
		loopResult, err := o.handleWithTools(ctx, req, messages)
		if err != nil {
			o.metrics.RecordError("llm", "chat")
			o.metrics.RecordFallback()
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		chatContent = loopResult.content
		chatFinishReason = loopResult.finishReason
		chatTokenUsage = loopResult.tokens
		chatModel = loopResult.model
		chatProvider = loopResult.provider
	} else {
		chatResp, err := o.llmProvider.Chat(ctx, llm.ChatRequest{
			Messages: messages,
		})
		if err != nil {
			o.metrics.RecordError("llm", "chat")
			o.metrics.RecordFallback()
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		chatContent = chatResp.Content
		chatFinishReason = chatResp.FinishReason
		chatTokenUsage = chatResp.TokenUsage
		chatModel = chatResp.Model
		chatProvider = chatResp.Provider
	}

	content, truncated := o.guardrails.SanitizeOutput(chatContent)
	totalTokens := 0
	if chatTokenUsage != nil {
		totalTokens = chatTokenUsage.TotalTokens
	}
	if chatProvider == "" {
		chatProvider = "llm"
	}
	o.metrics.RecordSuccess(chatProvider, time.Since(start), totalTokens)
	span.SetAttributes(
		attribute.String("ai.provider", chatProvider),
		attribute.Int("ai.tokens.total", totalTokens),
		attribute.Int64("ai.latency.ms", time.Since(start).Milliseconds()),
	)

	return &AIResponse{
		Content:      content,
		Model:        chatModel,
		Provider:     chatProvider,
		Sources:      hits,
		TokenUsage:   chatTokenUsage,
		FinishReason: chatFinishReason,
		Latency:      time.Since(start),
		Truncated:    truncated,
	}, nil
}

func (o *QueryOrchestrator) Metrics() MetricsSnapshot {
	return o.metrics.Snapshot()
}

func (o *QueryOrchestrator) SetPolicyHooks(hooks ...PolicyHook) {
	o.policyHooks = append([]PolicyHook(nil), hooks...)
}

func (o *QueryOrchestrator) SetAuditRecorder(recorder PromptAuditRecorder) {
	o.auditRecorder = recorder
}

func (o *QueryOrchestrator) SetToolExecutor(executor *ToolExecutor) {
	o.toolExecutor = executor
}

// maxToolSteps returns the maximum agent loop iterations allowed.
func (o *QueryOrchestrator) maxToolSteps(req AIRequest) int {
	if req.ToolPolicy.MaxSteps > 0 {
		return req.ToolPolicy.MaxSteps
	}
	return 5
}

// handleWithTools runs the agent loop: Chat → execute tools → feed back → repeat.
func (o *QueryOrchestrator) handleWithTools(ctx context.Context, req AIRequest, messages []llm.ChatMessage) (*agentLoopResult, error) {
	ctx, span := o.tracer.Start(ctx, "ai.agent_loop")
	defer span.End()

	maxSteps := o.maxToolSteps(req)
	tools := o.toolExecutor.Definitions()

	res := &agentLoopResult{
		tokens: &llm.TokenUsage{},
	}
	span.SetAttributes(
		attribute.Int("ai.agent.max_steps", maxSteps),
		attribute.Int("ai.agent.tool_count", len(tools)),
	)

	for step := 1; step <= maxSteps; step++ {
		chatResp, err := o.llmProvider.Chat(ctx, llm.ChatRequest{
			Messages: messages,
			Tools:    tools,
		})
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}

		res.content = chatResp.Content
		res.finishReason = chatResp.FinishReason
		res.model = chatResp.Model
		res.provider = chatResp.Provider
		if chatResp.TokenUsage != nil {
			res.tokens.InputTokens += chatResp.TokenUsage.InputTokens
			res.tokens.OutputTokens += chatResp.TokenUsage.OutputTokens
			if chatResp.TokenUsage.TotalTokens > res.tokens.TotalTokens {
				res.tokens.TotalTokens = chatResp.TokenUsage.TotalTokens
			}
		}

		span.SetAttributes(attribute.Int("ai.agent.step", step))

		if len(chatResp.ToolCalls) == 0 {
			// No more tool calls — done.
			if res.tokens.TotalTokens == 0 {
				res.tokens.TotalTokens = res.tokens.InputTokens + res.tokens.OutputTokens
			}
			return res, nil
		}

		// Append assistant message with tool calls.
		messages = append(messages, llm.ChatMessage{
			Role:      "assistant",
			Content:   chatResp.Content,
			ToolCalls: chatResp.ToolCalls,
		})

		// Execute each tool and feed back results.
		for _, tc := range chatResp.ToolCalls {
			o.metrics.RecordToolCall(tc.Name)
			result, execErr := o.toolExecutor.Execute(ctx, req, tc.Name, tc.Arguments)
			var resultStr string
			if execErr != nil {
				o.metrics.RecordToolError(tc.Name)
				resultStr = fmt.Sprintf("{\"error\":%q}", execErr.Error())
				span.AddEvent("tool.error",
					trace.WithAttributes(
						attribute.String("tool.name", tc.Name),
						attribute.String("tool.error", execErr.Error()),
					),
				)
			} else {
				resultBytes, marshalErr := json.Marshal(result)
				if marshalErr != nil {
					resultStr = fmt.Sprintf("{\"error\":\"marshal result: %v\"}", marshalErr)
				} else {
					resultStr = string(resultBytes)
				}
				span.AddEvent("tool.ok",
					trace.WithAttributes(attribute.String("tool.name", tc.Name)),
				)
			}
			messages = append(messages, llm.ChatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    resultStr,
			})
		}
	}

	// Max steps reached — return last content as-is.
	if res.tokens.TotalTokens == 0 {
		res.tokens.TotalTokens = res.tokens.InputTokens + res.tokens.OutputTokens
	}
	return res, nil
}

// agentLoopResult captures the aggregated output of the agent tool loop.
type agentLoopResult struct {
	content      string
	finishReason string
	model        string
	provider     string
	tokens       *llm.TokenUsage
}
