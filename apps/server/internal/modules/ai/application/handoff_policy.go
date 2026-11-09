package application

import (
	"strings"

	"servify/apps/server/internal/models"
)

// HandoffRules 转人工启发式的可配置规则面：显式意图短语（中文子串匹配 /
// 英文全词边界匹配）、投诉意图、长会话兜底的用户轮数阈值。阈值 0 = 关闭
// 对应规则；租户级差异经 ShouldTransferToHumanWithRules 传入。
type HandoffRules struct {
	// ZhHandoffPhrases 显式转人工意图短语。只收完整意图（"人工客服"），
	// 不收裸 "人工"/"客服"："人工智能" 里的 "人工"、"客服机器人介绍" 里的
	// "客服" 都不是转人工请求。
	ZhHandoffPhrases []string
	// EnHandoffWords 单词级英文意图词（全词边界）：只收无歧义词。
	EnHandoffWords []string
	// EnHandoffPhrases 英文意图短语（全词边界）：裸 "agent"/"manual" 是
	// 普通名词（user agent、user manual），必须带意图搭配才算转人工。
	EnHandoffPhrases []string
	// ZhComplaintPhrases 投诉意图短语（子串匹配）。
	ZhComplaintPhrases []string
	// EnComplaintWords 投诉意图词（全词边界匹配）。
	EnComplaintWords []string
	// HistoryUserTurns 会话内用户消息达到该轮数即建议转人工（长会话兜底）；
	// 0 = 关闭。只数用户消息：assistant 回复不计，避免 3 轮问答（6 条
	// 消息）就被迫转人工的历史误报。
	HistoryUserTurns int
}

// DefaultHandoffRules 返回内置默认规则。
func DefaultHandoffRules() HandoffRules {
	return HandoffRules{
		ZhHandoffPhrases: []string{
			"转人工", "转接人工", "人工客服", "人工服务", "找人工", "找客服", "真人客服", "人工坐席",
		},
		EnHandoffWords: []string{"human"},
		EnHandoffPhrases: []string{
			"human agent", "live agent", "real person", "actual person", "human support",
			"talk to a human", "talk to an agent", "speak to a human", "speak to an agent", "with an agent",
		},
		ZhComplaintPhrases: []string{"投诉", "举报"},
		EnComplaintWords:   []string{"complaint", "complain"},
		HistoryUserTurns:   10,
	}
}

// currentHandoffRules 兜底规则包（legacy 入口每次调用读取；测试可替换）。
var currentHandoffRules = DefaultHandoffRules

// ShouldTransferToHuman centralizes the default handoff heuristic used by
// legacy AI facades while the AI module becomes the primary business entry.
func ShouldTransferToHuman(query string, sessionHistory []models.Message) bool {
	return ShouldTransferToHumanWithRules(currentHandoffRules(), query, sessionHistory)
}

// ShouldTransferToHumanWithRules 按给定规则判定是否建议转人工。判定顺序：
// 显式转人工意图 → 投诉意图 → 长会话兜底（仅用户消息计数）。
func ShouldTransferToHumanWithRules(rules HandoffRules, query string, sessionHistory []models.Message) bool {
	lower := strings.ToLower(query)

	for _, phrase := range rules.ZhHandoffPhrases {
		if strings.Contains(query, phrase) {
			return true
		}
	}
	for _, word := range rules.EnHandoffWords {
		if hasEnWord(lower, word) {
			return true
		}
	}
	for _, phrase := range rules.EnHandoffPhrases {
		if hasEnWord(lower, phrase) {
			return true
		}
	}
	for _, phrase := range rules.ZhComplaintPhrases {
		if strings.Contains(query, phrase) {
			return true
		}
	}
	for _, word := range rules.EnComplaintWords {
		if hasEnWord(lower, word) {
			return true
		}
	}

	if rules.HistoryUserTurns > 0 {
		turns := 0
		for _, msg := range sessionHistory {
			if msg.Sender == "user" {
				turns++
			}
		}
		if turns >= rules.HistoryUserTurns {
			return true
		}
	}
	return false
}

// hasEnWord 全词边界匹配：命中位置两侧必须是非字母数字（中文 UTF-8 字节
// >= 0x80 天然算边界），避免 "user agent"/"user manual" 子串误判。首处
// 边界不成立时从下一字节继续搜（"inhuman or human" 仍命中第二处）。
func hasEnWord(lowerQuery, word string) bool {
	for offset := 0; offset < len(lowerQuery); {
		at := strings.Index(lowerQuery[offset:], word)
		if at < 0 {
			return false
		}
		start := offset + at
		end := start + len(word)
		if enWordBoundary(lowerQuery, start, end) {
			return true
		}
		offset = start + 1
	}
	return false
}

func enWordBoundary(s string, start, end int) bool {
	if start > 0 && isASCIILetterDigit(s[start-1]) {
		return false
	}
	if end < len(s) && isASCIILetterDigit(s[end]) {
		return false
	}
	return true
}

func isASCIILetterDigit(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

// BuildSessionSummaryUnavailable provides the default summary when summary generation fails.
func BuildSessionSummaryUnavailable() string {
	return "无法生成会话摘要"
}
