package local

import (
	"context"
	"errors"
	"strings"
	"testing"

	"servify/apps/server/internal/platform/llm"
)

// knowledgePrompt 与 conversationPrompt 复刻编排层 prompt_builder 的分段
// 形状（Knowledge context / Conversation context 标记 + "- Title: 内容"）。
func knowledgePrompt(entries ...string) string {
	return "Knowledge context:\n" + strings.Join(prefixEach("- ", entries), "\n")
}

func conversationPrompt(entries ...string) string {
	return "Conversation context:\n" + strings.Join(prefixEach("- ", entries), "\n")
}

func prefixEach(prefix string, items []string) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = prefix + item
	}
	return out
}

// TestChatExtractsBestSentenceFromKnowledgeContext 知识命中的最相关句被
// 原文抽取（不编造），provider/model 标识与 token 估算齐备。
func TestChatExtractsBestSentenceFromKnowledgeContext(t *testing.T) {
	p := NewProvider()
	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: knowledgePrompt(
				"退货政策: 本平台支持七天无理由退货，退款将在三到五个工作日内原路退回。",
				"营业时间: 客服热线的工作时间是每天九点到十八点。",
			)},
			{Role: "user", Content: "退款多久到账"},
		},
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if !strings.Contains(resp.Content, "退款将在三到五个工作日内原路退回") {
		t.Fatalf("content = %q, want extracted refund sentence", resp.Content)
	}
	if strings.Contains(resp.Content, "九点") {
		t.Fatalf("unrelated sentence leaked: %q", resp.Content)
	}
	if resp.Provider != ProviderID || resp.Model != DefaultModel {
		t.Fatalf("provider/model = %s/%s, want %s/%s", resp.Provider, resp.Model, ProviderID, DefaultModel)
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("finish reason = %q, want stop", resp.FinishReason)
	}
	if resp.TokenUsage == nil || resp.TokenUsage.TotalTokens < resp.TokenUsage.OutputTokens {
		t.Fatalf("token usage inconsistent: %+v", resp.TokenUsage)
	}
	// 抽取内容必须逐字来自上下文（确定性基线不产生新句子）。
	if !strings.Contains(knowledgePrompt("退货政策: 本平台支持七天无理由退货，退款将在三到五个工作日内原路退回。", "x"), strings.TrimSpace(resp.Content)) &&
		!strings.Contains("本平台支持七天无理由退货，退款将在三到五个工作日内原路退回。", strings.TrimSpace(resp.Content)) {
		t.Fatalf("content not extracted verbatim from context: %q", resp.Content)
	}
}

// TestChatPrefersKnowledgeOverConversation 知识区与对话区同分时知识区
// 句子优先。
func TestChatPrefersKnowledgeOverConversation(t *testing.T) {
	p := NewProvider()
	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: conversationPrompt("user: 工单升级流程是什么")},
			{Role: "system", Content: knowledgePrompt("流程: 工单升级流程是登记、分派、回访。")},
			{Role: "user", Content: "工单升级流程是什么"},
		},
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if !strings.Contains(resp.Content, "登记、分派、回访") {
		t.Fatalf("content = %q, want knowledge sentence", resp.Content)
	}
}

// TestChatFallsBackToConversation 无知识段时从对话上下文抽取。
func TestChatFallsBackToConversation(t *testing.T) {
	p := NewProvider()
	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: conversationPrompt(
				"user: 发票怎么开",
				"agent: 电子发票在订单详情页申请，开票后一个工作日内发送邮箱。",
			)},
			{Role: "user", Content: "发票怎么开"},
		},
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if !strings.Contains(resp.Content, "订单详情页申请") {
		t.Fatalf("content = %q, want conversation sentence", resp.Content)
	}
}

// TestChatBareSystemPrompt 裸 system prompt（无标记）也作为可抽取上下文。
func TestChatBareSystemPrompt(t *testing.T) {
	p := NewProvider()
	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: "退款将在三个工作日内原路退回。与退款无关的另一句。"},
			{Role: "user", Content: "退款到账时间"},
		},
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if !strings.Contains(resp.Content, "三个工作日内原路退回") {
		t.Fatalf("content = %q, want bare-system sentence", resp.Content)
	}
}

// TestChatNoMatchReturnsObservableFinish 查询与上下文零重叠：空回答 +
// 可观测 finish 原由（不倾倒无关句子）。
func TestChatNoMatchReturnsObservableFinish(t *testing.T) {
	p := NewProvider()
	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: knowledgePrompt("营业时间: 每天九点到十八点。")},
			{Role: "user", Content: "宇宙飞船发射计划"},
		},
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Content != "" {
		t.Fatalf("content = %q, want empty", resp.Content)
	}
	if resp.FinishReason != "no sentence matched" {
		t.Fatalf("finish reason = %q", resp.FinishReason)
	}
}

// TestChatEmptyQuery 无 user 消息：空回答 + empty query 原由。
func TestChatEmptyQuery(t *testing.T) {
	p := NewProvider()
	resp, err := p.Chat(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Content != "" || resp.FinishReason != "empty query" {
		t.Fatalf("resp = %+v, want empty content + empty query reason", resp)
	}
}

// TestChatTopKExtraction 多句抽取按分数排序合并（topK>1）。
func TestChatTopKExtraction(t *testing.T) {
	p := &Provider{topK: 2}
	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: knowledgePrompt(
				"政策: 退款三个工作日到账。退货七天无理由。发票随订单开具。",
			)},
			{Role: "user", Content: "退款和退货政策"},
		},
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if !strings.Contains(resp.Content, "退款三个工作日到账") || !strings.Contains(resp.Content, "退货七天无理由") {
		t.Fatalf("content = %q, want top-2 sentences", resp.Content)
	}
	if strings.Contains(resp.Content, "发票") {
		t.Fatalf("third sentence should not be extracted: %q", resp.Content)
	}
}

// TestChatDeterministic 同一输入恒得同一输出。
func TestChatDeterministic(t *testing.T) {
	p := NewProvider()
	req := llm.ChatRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: knowledgePrompt("政策: 退款三个工作日到账。退货七天无理由。")},
			{Role: "user", Content: "退款多久到账"},
		},
	}
	a, _ := p.Chat(context.Background(), req)
	b, _ := p.Chat(context.Background(), req)
	if a.Content != b.Content || a.FinishReason != b.FinishReason {
		t.Fatalf("non-deterministic: %q/%q vs %q/%q", a.Content, a.FinishReason, b.Content, b.FinishReason)
	}
}

// TestChatStream 单块输出 + Done 收尾；空回答只有 Done。
func TestChatStream(t *testing.T) {
	p := NewProvider()
	ch, err := p.ChatStream(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: knowledgePrompt("政策: 退款三个工作日到账。")},
			{Role: "user", Content: "退款多久到账"},
		},
	})
	if err != nil {
		t.Fatalf("ChatStream error: %v", err)
	}
	var parts []string
	done := false
	for chunk := range ch {
		if chunk.ContentDelta != "" {
			parts = append(parts, chunk.ContentDelta)
		}
		if chunk.Done {
			done = true
		}
	}
	if !done || len(parts) != 1 {
		t.Fatalf("chunks = %v done=%v, want single content chunk + done", parts, done)
	}
	// 空回答：仅 Done。
	emptyCh, err := p.ChatStream(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("ChatStream empty error: %v", err)
	}
	seen := 0
	for chunk := range emptyCh {
		seen++
		if chunk.ContentDelta != "" || !chunk.Done {
			t.Fatalf("unexpected chunk %+v", chunk)
		}
	}
	if seen != 1 {
		t.Fatalf("empty stream chunks = %d, want 1 (done only)", seen)
	}
}

// TestEmbedNotSupported 嵌入口显式拒绝（检索嵌入由 embedding provider 承担）。
func TestEmbedNotSupported(t *testing.T) {
	p := NewProvider()
	vecs, err := p.Embed(context.Background(), []string{"x"})
	if err == nil || vecs != nil {
		t.Fatalf("Embed = %v, %v; want not-supported error", vecs, err)
	}
	var perr *llm.ProviderError
	if !errors.As(err, &perr) || perr.Code != llm.ProviderErrorNotSupported {
		t.Fatalf("err = %v, want ProviderError(not supported)", err)
	}
}

// TestHealthCheck 本地问答器恒健康。
func TestHealthCheck(t *testing.T) {
	if err := NewProvider().HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck = %v, want nil", err)
	}
}

// TestSplitSentences 分句边界：中文句读、西文句点后随空白即断（缩写词
// 会过切分，抽取基线的保守策略）、小数不触发、换行强制断句。
func TestSplitSentences(t *testing.T) {
	got := extractSentences("第一句。第二句！第三句？进度 3.5 倍。Dr. Smith 来了。最后一句\n换行句")
	want := []string{"第一句。", "第二句！", "第三句？", "进度 3.5 倍。", "Dr.", "Smith 来了。", "最后一句", "换行句"}
	if len(got) != len(want) {
		t.Fatalf("sentences = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sentence %d = %q, want %q", i, got[i], want[i])
		}
	}
	if s := extractSentences("   \n\t "); len(s) != 0 {
		t.Fatalf("blank sentences = %v, want empty", s)
	}
}

// TestSplitSections 标记分区：双标记互截、单标记、全缺失。
func TestSplitSections(t *testing.T) {
	both := "Conversation context:\n- user: hi\nKnowledge context:\n- 政策: 内容\n"
	know, conv := splitSections(both)
	if !strings.HasPrefix(know, "Knowledge context:") || !strings.Contains(know, "政策: 内容") {
		t.Fatalf("knowledge = %q", know)
	}
	if !strings.HasPrefix(conv, "Conversation context:") || strings.Contains(conv, "政策") {
		t.Fatalf("conversation = %q", conv)
	}
	// 顺序反转（knowledge 在前）。
	reversed := "Knowledge context:\n- a: b\nConversation context:\n- user: q\n"
	know2, conv2 := splitSections(reversed)
	if !strings.Contains(know2, "a: b") || strings.Contains(know2, "user: q") {
		t.Fatalf("knowledge(reversed) = %q", know2)
	}
	if !strings.Contains(conv2, "user: q") || strings.Contains(conv2, "a: b") {
		t.Fatalf("conversation(reversed) = %q", conv2)
	}
	// 全缺失 → 整段归对话区。
	know3, conv3 := splitSections("bare prompt")
	if know3 != "" || conv3 != "bare prompt" {
		t.Fatalf("bare = %q / %q", know3, conv3)
	}
}

// TestTokenizeAndTokenSet 词元切分：拉丁整词小写、CJK 连续段取二元组、
// 单字 CJK 段退化为单字、空输入为 nil、tokenSet 去重。
func TestTokenizeAndTokenSet(t *testing.T) {
	got := tokenize("退货REFUND 123 退 货")
	joined := strings.Join(got, "|")
	for _, want := range []string{"refund", "123", "退货", "退", "货"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("tokens %v missing %q", got, want)
		}
	}
	if tokenize("") != nil {
		t.Fatalf("empty tokenize should be nil")
	}
	set := tokenSet("退款 退款 refund")
	if set["refund"] != 1 || len(set) != 2 {
		t.Fatalf("token set = %v", set)
	}
}

// TestChatKnowledgeConversationTie 知识区与对话区句子同分时知识句子恒胜
// （结构上外部证据优先于历史上下文）。
func TestChatKnowledgeConversationTie(t *testing.T) {
	p := NewProvider()
	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: knowledgePrompt("政策: 优惠日全场五折。")},
			{Role: "system", Content: conversationPrompt("agent: 本周有优惠日活动。")},
			{Role: "user", Content: "优惠"},
		},
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if !strings.Contains(resp.Content, "五折") {
		t.Fatalf("content = %q, want knowledge sentence on tie", resp.Content)
	}
}

// TestLongSentenceTruncated 超长句截断到 maxSentenceLen（查询词元取句中
// 真实存在的二元组，确保句子进候选）。
func TestLongSentenceTruncated(t *testing.T) {
	p := NewProvider()
	long := strings.Repeat("退", maxSentenceLen+50)
	resp, err := p.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.ChatMessage{
			{Role: "system", Content: knowledgePrompt("政策: " + long + "。")},
			{Role: "user", Content: "退退"},
		},
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if got := len([]rune(resp.Content)); got != maxSentenceLen {
		t.Fatalf("content runes = %d, want %d", got, maxSentenceLen)
	}
}

// TestStableRankKnowledgeTieBreak 直接核对 stableRank 同分比较器：知识区
// 句（前缀位）与对话区句同分时知识句恒排前、分高者恒胜、空输入返回 nil。
// Chat 层同分用例（TestChatKnowledgeConversationTie）会被对话区回声抑制
// 削成单候选，走不到比较器分支，必须在此直测。
func TestStableRankKnowledgeTieBreak(t *testing.T) {
	querySet := tokenSet("退货流程")
	know := "退货流程需要审核。" // 命中 退货/货流/流程 = 3
	conv := "退货流程已提交。"  // 同样命中 3，同分
	got := stableRank([]string{know, conv}, querySet, 1)
	if len(got) != 2 || got[0] != know || got[1] != conv {
		t.Fatalf("stableRank tie = %v, want knowledge sentence first", got)
	}

	// 分高者恒胜：低分知识句不越过高分对话句。
	high := "退货流程已提交，请查看退货进度。" // 追加 退货 命中 = 4
	got = stableRank([]string{know, high}, querySet, 1)
	if len(got) != 2 || got[0] != high || got[1] != know {
		t.Fatalf("stableRank scores = %v, want higher score first", got)
	}

	if stableRank(nil, querySet, 0) != nil {
		t.Fatal("stableRank(nil) = non-nil, want nil")
	}
}

// TestZeroProviderDefaults nil 接收者按 DefaultTopK 兜底。
func TestZeroProviderDefaults(t *testing.T) {
	var p *Provider
	if p == nil {
		resp, err := p.Chat(context.Background(), llm.ChatRequest{
			Messages: []llm.ChatMessage{
				{Role: "system", Content: knowledgePrompt("政策: 退款三个工作日到账。")},
				{Role: "user", Content: "退款多久到账"},
			},
		})
		if err != nil || resp.Content == "" {
			t.Fatalf("nil provider chat = %q, %v", resp.Content, err)
		}
		return
	}
	t.Fatal("unreachable")
}

// 编译期确认实现端口。
var _ llm.LLMProvider = (*Provider)(nil)
