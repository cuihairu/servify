// Package local 提供零外部依赖的确定性抽取式 LLM（本地问答引擎的文本面）。
// 与 openai/anthropic 的 HTTP provider 不同，它不发出任何网络请求：从
// 编排层 prompt 的 Knowledge/Conversation context 段按句切分，以查询
// 词面重叠打分子抽取最相关句子作为回答（extractive QA baseline，与
// embedding/local 同属「零依赖词法级基线」一族）。
//
// 定位是本地零依赖部署的确定性问答基线：知识命中越强、回答越贴原文，
// 同一输入恒得同一输出（可复现、可测试、可离线验收）；生产语义问答仍
// 建议配置 openai/anthropic。回答内容只可能来自上下文本身，绝不编造。
package local

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"servify/apps/server/internal/platform/llm"
)

// 常量在此集中定义（供装配层引用 id、交付层引用模型名）。
const (
	// ProviderID 是 provider 标识（ai/status 上报与 ChatResponse.Provider）。
	ProviderID = "local"
	// DefaultModel 是响应侧模型标识（编排层 RuntimeParams 的默认模型）。
	DefaultModel = "local-extractive"
	// DefaultTopK 是单次回答抽取的句子数。
	DefaultTopK = 1
	// KnowledgeMark 是检索命中段在 system 上下文里的标记（与
	// modules/ai/application/prompt_builder.go 的 buildKnowledgePrompt 一致）。
	KnowledgeMark = "Knowledge context:"
	// ConversationMark 是多轮上下文段的标记（与 buildContextPrompt 一致）。
	ConversationMark = "Conversation context:"
	// maxSentenceLen 是单句抽出的最大长度（防长句拖垮回答）。
	maxSentenceLen = 200
)

// Provider 是零依赖抽取式问答器，多 goroutine 并发安全（只读）。
type Provider struct {
	topK int
}

// NewProvider 构造抽取式问答器。
func NewProvider() *Provider { return &Provider{topK: DefaultTopK} }

// Chat 抽取式回答：查询 = 末条 user 消息；候选 = system 上下文中
// Knowledge context 段（知识命中）优先于 Conversation context 段。
func (p *Provider) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	topK := DefaultTopK
	if p != nil && p.topK > 0 {
		topK = p.topK
	}

	query, systemParts, inputRunes := splitMessages(req.Messages)
	usage := &llm.TokenUsage{
		InputTokens:  inputRunes,
		OutputTokens: 0,
		TotalTokens:  inputRunes,
	}
	if query == "" {
		// 无查询无从抽取：空回答 + 可观测 finish 原由，不倾倒任意句子。
		return llm.ChatResponse{
			Provider:     ProviderID,
			Model:        DefaultModel,
			FinishReason: "empty query",
			TokenUsage:   usage,
		}, nil
	}
	candidates := p.rankSentences(query, systemParts)
	if len(candidates) > topK {
		candidates = candidates[:topK]
	}

	content := strings.Join(candidates, "\n")
	usage.OutputTokens = len([]rune(content))
	usage.TotalTokens = inputRunes + len([]rune(content))
	return llm.ChatResponse{
		Content:      content,
		Provider:     ProviderID,
		Model:        DefaultModel,
		FinishReason: finishReasonFor(content),
		TokenUsage:   usage,
	}, nil
}

// ChatStream 单块输出整段回答后 Done（与 Chat 同一确定性内容，mock 同款
// 缓冲通道模式；编排层流式口可复用）。Chat 恒不报错，无需错误分支。
func (p *Provider) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.ChatChunk, error) {
	resp, _ := p.Chat(ctx, req)
	ch := make(chan llm.ChatChunk, 2)
	if resp.Content != "" {
		ch <- llm.ChatChunk{ContentDelta: resp.Content}
	}
	ch <- llm.ChatChunk{Done: true}
	close(ch)
	return ch, nil
}

// Embed 本地问答不提供 LLM 侧嵌入（检索嵌入由 embedding provider 承担）；
// 与 anthropic 同契约显式拒绝。
func (p *Provider) Embed(_ context.Context, _ []string) ([][]float32, error) {
	return nil, &llm.ProviderError{
		Provider:  ProviderID,
		Code:      llm.ProviderErrorNotSupported,
		Message:   "local provider does not embed; use the embedding provider (e.g. embedding.provider=local)",
		Retryable: false,
	}
}

// HealthCheck 本地问答器无外部依赖，恒健康。
func (p *Provider) HealthCheck(_ context.Context) error { return nil }

// splitMessages 提取查询（末条 user 内容）与全部 system 上下文，
// 返回输入侧的估算输入字符数（token 估算按 rune 计，确定性）。
func splitMessages(messages []llm.ChatMessage) (query string, systemParts []string, inputRunes int) {
	var systemContents []string
	for _, m := range messages {
		inputRunes += len([]rune(m.Content))
		switch m.Role {
		case "user":
			query = strings.TrimSpace(m.Content)
		case "system":
			if content := strings.TrimSpace(m.Content); content != "" {
				systemContents = append(systemContents, content)
			}
		}
	}
	return query, systemContents, inputRunes
}

// rankSentences 在 system 上下文里按句切分并打分排序：知识区句子恒排
// 在对话区之前（外部证据结构上优先于历史回声）；区域内按查询词元的句内
// 出现次数降序、同分保持原文顺序。零重叠句子与对话区的「回声句」（原样
// 复述查询的句子——最新 user 消息会经 buildContextPrompt 进入上下文，
// 复述对回答零信息量）不进候选。
func (p *Provider) rankSentences(query string, systemParts []string) []string {
	var knowledge []string
	var conversation []string
	for _, part := range systemParts {
		knowText, convText := splitSections(part)
		knowledge = append(knowledge, extractSentences(knowText)...)
		conversation = append(conversation, extractSentences(convText)...)
	}

	querySet := tokenSet(query)
	var candidates []string
	for _, s := range knowledge {
		if overlapScore(s, querySet) > 0 {
			candidates = append(candidates, s)
		}
	}
	matchedKnowledge := len(candidates)
	for _, s := range conversation {
		if strings.Contains(s, query) {
			continue // 回声抑制
		}
		if overlapScore(s, querySet) > 0 {
			candidates = append(candidates, s)
		}
	}
	return stableRank(candidates, querySet, matchedKnowledge)
}

// splitSections 把单段 system 内容切为 Knowledge/Conversation 两个区间：
// KnowledgeMark 与 ConversationMark（二者可只出现其一）之间的内容按标记
// 归区，重叠时各取到对方标记为止；两者都缺失时整段归对话区（裸 system
// prompt 也作为可抽取上下文）。
func splitSections(content string) (knowledge string, conversation string) {
	knowIdx := strings.Index(content, KnowledgeMark)
	convIdx := strings.Index(content, ConversationMark)
	if knowIdx >= 0 {
		knowEnd := len(content)
		if convIdx > knowIdx {
			knowEnd = convIdx
		}
		knowledge = content[knowIdx:knowEnd]
	}
	if convIdx >= 0 {
		convEnd := len(content)
		if knowIdx > convIdx {
			convEnd = knowIdx
		}
		conversation = content[convIdx:convEnd]
	}
	if knowledge == "" && conversation == "" {
		conversation = content
	}
	return knowledge, conversation
}

// extractSentences 按句子边界切分文本并去空：中文句读（。！？…）、西文
// 句点（后随空白或结尾才触发，缩写/小数如 "3.5" 不触发）以及换行。
func extractSentences(text string) []string {
	var out []string
	var sb strings.Builder
	flush := func() {
		s := strings.TrimSpace(sb.String())
		sb.Reset()
		if s != "" {
			out = append(out, s)
		}
	}
	runes := []rune(text)
	for i, r := range runes {
		if r == '\n' {
			flush()
			continue
		}
		sb.WriteRune(r)
		if isSentenceEnd(r) {
			// 西文句点后随非空白（缩写/小数）不断句；其余句读即断。
			if r == '.' && i+1 < len(runes) && !unicode.IsSpace(runes[i+1]) {
				continue
			}
			flush()
		}
	}
	flush()
	return out
}

// isSentenceEnd 判断是否句末标点（。！？… 与西文 . ! ?）。
func isSentenceEnd(r rune) bool {
	switch r {
	case '。', '！', '？', '…', '.', '!', '?':
		return true
	default:
		return false
	}
}

// stableRank 按重叠词元计数降序稳定排序句子：同分保持原文顺序，知识区
// 句子恒排在对话区句子之前（knowledge 区在拼接列表的前缀位）。返回全量
// 已截断列表，由调用方截 topK。
func stableRank(sentences []string, querySet map[string]int, knowledgePrefix int) []string {
	if len(sentences) == 0 {
		return nil
	}
	scores := make([]int, len(sentences))
	for i, s := range sentences {
		scores[i] = overlapScore(s, querySet)
	}
	// SliceStable 保证同分保序（确定性），比较器里 knowledge 区优先。
	sort.SliceStable(sentences, func(i, j int) bool {
		if scores[i] != scores[j] {
			return scores[i] > scores[j]
		}
		inKnowI := i < knowledgePrefix
		inKnowJ := j < knowledgePrefix
		if inKnowI != inKnowJ {
			return inKnowI
		}
		return false
	})
	return truncateAll(sentences)
}

// truncateAll 逐句截断到 maxSentenceLen。
func truncateAll(sentences []string) []string {
	out := make([]string, 0, len(sentences))
	for _, s := range sentences {
		runes := []rune(s)
		if len(runes) > maxSentenceLen {
			runes = runes[:maxSentenceLen]
		}
		out = append(out, string(runes))
	}
	return out
}

// overlapScore 句内查询词元出现次数之和（含句内重复，tf 语义）。
func overlapScore(sentence string, querySet map[string]int) int {
	total := 0
	for _, tok := range tokenize(sentence) {
		if _, hit := querySet[tok]; hit {
			total++
		}
	}
	return total
}

// tokenSet 把查询切为词元集合（值域为出现次数，计算句分时不再除重）。
func tokenSet(query string) map[string]int {
	set := make(map[string]int, len(query)/2+1)
	for _, tok := range tokenize(query) {
		set[tok] = 1
	}
	return set
}

// finishReasonFor 回答为空（无重叠句子）时给出可观测的 finish 原由。
func finishReasonFor(content string) string {
	if strings.TrimSpace(content) == "" {
		return "no sentence matched"
	}
	return "stop"
}

// tokenize 切分文本为词元：拉丁/数字连续段整词（小写）；CJK 连续段取
// 字符二元组（单字段退化为单字）。氢与 embedding/local 的切分语义一致，
// 保证问答抽取的「相关性」与向量检索的「相似度」同源。
func tokenize(text string) []string {
	if text == "" {
		return nil
	}
	tokens := make([]string, 0, len(text)/2+1)
	var latin strings.Builder
	var cjk []rune
	flushLatin := func() {
		if latin.Len() > 0 {
			tokens = append(tokens, strings.ToLower(latin.String()))
			latin.Reset()
		}
	}
	flushCJK := func() {
		if len(cjk) == 1 {
			tokens = append(tokens, strings.ToLower(string(cjk[0])))
			cjk = nil
			return
		}
		for i := 0; i+1 < len(cjk); i++ {
			tokens = append(tokens, strings.ToLower(string(cjk[i])+string(cjk[i+1])))
		}
		cjk = nil
	}
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) && isCJK(r):
			flushLatin()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			latin.WriteRune(r)
		default:
			flushLatin()
			flushCJK()
		}
	}
	flushLatin()
	flushCJK()
	return tokens
}

// isCJK 判断是否中日韩统一表意文字（与 embedding/local 同源判定）。
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF)
}
