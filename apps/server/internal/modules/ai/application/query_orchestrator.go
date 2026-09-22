package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

	messages, hits, err := o.prepare(ctx, req, span)
	if err != nil {
		return nil, err
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
			Model:       req.Model,
			Messages:    messages,
			Temperature: req.Temperature,
			MaxTokens:   req.MaxTokens,
			Options:     llm.RequestOptions{TimeoutMs: req.TimeoutMs},
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
	return o.finalize(span, start, chatProvider, content, truncated, chatModel, chatFinishReason, chatTokenUsage, hits), nil
}

// prepare 运行模型交互前的共享流水线：策略钩子、输入护栏、知识检索、
// 提示组装与提示审计。Handle 与 HandleStream 共用，错误分支的指标与
// span 记录也在此统一。
func (o *QueryOrchestrator) prepare(ctx context.Context, req AIRequest, span trace.Span) ([]llm.ChatMessage, []knowledgeprovider.KnowledgeHit, error) {
	for _, hook := range o.policyHooks {
		decision, err := hook.Evaluate(ctx, req)
		if err != nil {
			o.metrics.RecordError("policy", "policy_hook_error")
			o.metrics.RecordFallback()
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, nil, err
		}
		if !decision.Allowed {
			o.metrics.RecordPolicyRejection(decision.Reason)
			o.metrics.RecordFallback()
			span.SetStatus(codes.Error, decision.Reason)
			return nil, nil, context.Canceled
		}
	}
	if err := o.guardrails.ValidateInput(req); err != nil {
		o.metrics.RecordPolicyRejection("guardrails")
		o.metrics.RecordFallback()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, nil, err
	}

	hits, err := o.retriever.Retrieve(ctx, req)
	if err != nil {
		o.metrics.RecordError("knowledge", "retrieval")
		o.metrics.RecordFallback()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, nil, err
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
	return messages, hits, nil
}

// finalize 记录成功指标并组装厂商中立响应（Handle 与 HandleStream 共用）。
func (o *QueryOrchestrator) finalize(span trace.Span, start time.Time, provider, content string, truncated bool, model, finishReason string, tokenUsage *llm.TokenUsage, hits []knowledgeprovider.KnowledgeHit) *AIResponse {
	totalTokens := 0
	if tokenUsage != nil {
		totalTokens = tokenUsage.TotalTokens
	}
	if provider == "" {
		provider = "llm"
	}
	o.metrics.RecordSuccess(provider, time.Since(start), totalTokens)
	span.SetAttributes(
		attribute.String("ai.provider", provider),
		attribute.Int("ai.tokens.total", totalTokens),
		attribute.Int64("ai.latency.ms", time.Since(start).Milliseconds()),
	)

	return &AIResponse{
		Content:      content,
		Model:        model,
		Provider:     provider,
		Sources:      hits,
		TokenUsage:   tokenUsage,
		FinishReason: finishReason,
		Latency:      time.Since(start),
		Truncated:    truncated,
	}
}

// StreamEvent is one unit of a streamed orchestration result. The channel
// returned by HandleStream terminates with exactly one Done event: on
// success Response carries the full result (equivalent to Handle, with
// output sanitized and sources attached); on a mid-stream failure Err is
// set and Response is nil. Channel close without Done is reserved for
// caller-side context cancellation while draining.
type StreamEvent struct {
	ContentDelta string
	Done         bool
	Response     *AIResponse
	Err          error
}

// HandleStream runs the same pipeline as Handle (policy hooks, input
// guardrails, retrieval, prompt assembly) but streams model output through
// LLMProvider.ChatStream: content deltas are forwarded as they arrive and
// the complete result lands on the terminal Done event. Pre-model failures
// return as a synchronous error before any event is emitted, so callers can
// fall back to the non-streaming path; failures after streaming started
// surface as the terminal Done event with Err set. Streaming responses carry
// no model/provider/finish-reason/token-usage metadata (chunk contract has
// none) — Provider falls back to "llm" in finalize.
func (o *QueryOrchestrator) HandleStream(ctx context.Context, req AIRequest) (<-chan StreamEvent, error) {
	// span 归流式 goroutine 持有并结束：事件发射可能活到本函数返回之后。
	ctx, span := o.tracer.Start(ctx, "ai.query_orchestrator.handle_stream")
	span.SetAttributes(
		attribute.String("ai.task_type", string(req.TaskType)),
		attribute.Bool("ai.retrieval.enabled", req.RetrievalPolicy.Enabled),
	)

	start := time.Now()
	o.metrics.RecordQuery()
	if o.llmProvider == nil {
		span.End()
		return nil, nil
	}

	messages, hits, err := o.prepare(ctx, req, span)
	if err != nil {
		span.End()
		return nil, err
	}

	events := make(chan StreamEvent)
	go func() {
		defer span.End()
		defer close(events)
		o.streamInteraction(ctx, span, req, messages, hits, start, events)
	}()
	return events, nil
}

// streamInteraction 流式 agent 循环：每步走 ChatStream，内容分片即产即发；
// 分片中出现的完整工具调用触发执行并进入下一步。channel 恒以一条 Done
// 事件收尾（成功带 Response，失败带 Err）。
func (o *QueryOrchestrator) streamInteraction(ctx context.Context, span trace.Span, req AIRequest, messages []llm.ChatMessage, hits []knowledgeprovider.KnowledgeHit, start time.Time, events chan<- StreamEvent) {
	emit := func(evt StreamEvent) bool {
		select {
		case <-ctx.Done():
			return false
		case events <- evt:
			return true
		}
	}

	useTools := req.ToolPolicy.Enabled && o.toolExecutor != nil
	var tools []llm.ToolDefinition
	if useTools {
		tools = o.toolExecutor.Definitions()
	}

	var (
		content      strings.Builder
		model        string
		provider     string
		finishReason string
	)
	finish := func() bool {
		finalContent, truncated := o.guardrails.SanitizeOutput(content.String())
		return emit(StreamEvent{Done: true, Response: o.finalize(span, start, provider, finalContent, truncated, model, finishReason, nil, hits)})
	}

	maxSteps := o.maxToolSteps(req)
	for step := 1; step <= maxSteps; step++ {
		chatReq := llm.ChatRequest{
			Model:       req.Model,
			Messages:    messages,
			Temperature: req.Temperature,
			MaxTokens:   req.MaxTokens,
			Options:     llm.RequestOptions{TimeoutMs: req.TimeoutMs},
		}
		if useTools {
			chatReq.Tools = tools
		}
		span.SetAttributes(attribute.Int("ai.agent.step", step))

		stream, err := o.llmProvider.ChatStream(ctx, chatReq)
		if err != nil {
			o.metrics.RecordError("llm", "chat")
			o.metrics.RecordFallback()
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			emit(StreamEvent{Done: true, Err: err})
			return
		}

		var stepToolCalls []llm.ToolCall
		stepStart := content.Len()
		for chunk := range stream {
			if chunk.ContentDelta != "" {
				content.WriteString(chunk.ContentDelta)
				if !emit(StreamEvent{ContentDelta: chunk.ContentDelta}) {
					return
				}
			}
			// chunk 契约：工具调用在 provider 侧累积完成后整体下发。
			if chunk.ToolCall != nil {
				stepToolCalls = append(stepToolCalls, *chunk.ToolCall)
			}
			if chunk.Done {
				break
			}
		}

		if len(stepToolCalls) == 0 {
			// 无工具调用——回答完成。
			finish()
			return
		}
		messages = o.appendToolFeedback(ctx, req, messages, content.String()[stepStart:], stepToolCalls, span)
	}

	// Max steps reached — finalize what streamed so far.
	finish()
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
			Model:       req.Model,
			Messages:    messages,
			Tools:       tools,
			Temperature: req.Temperature,
			MaxTokens:   req.MaxTokens,
			Options:     llm.RequestOptions{TimeoutMs: req.TimeoutMs},
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

		messages = o.appendToolFeedback(ctx, req, messages, chatResp.Content, chatResp.ToolCalls, span)
	}

	// Max steps reached — return last content as-is.
	if res.tokens.TotalTokens == 0 {
		res.tokens.TotalTokens = res.tokens.InputTokens + res.tokens.OutputTokens
	}
	return res, nil
}

// appendToolFeedback 追加 assistant 工具调用消息、逐个执行工具并回填
// tool 结果消息；非流式与流式 agent 循环共用。
func (o *QueryOrchestrator) appendToolFeedback(ctx context.Context, req AIRequest, messages []llm.ChatMessage, content string, toolCalls []llm.ToolCall, span trace.Span) []llm.ChatMessage {
	messages = append(messages, llm.ChatMessage{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	})

	for _, tc := range toolCalls {
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
	return messages
}

// agentLoopResult captures the aggregated output of the agent tool loop.
type agentLoopResult struct {
	content      string
	finishReason string
	model        string
	provider     string
	tokens       *llm.TokenUsage
}
