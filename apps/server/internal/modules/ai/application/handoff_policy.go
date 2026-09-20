package application

import (
	"strings"

	"servify/apps/server/internal/models"
)

var humanHandoffKeywords = []string{
	"人工",
	"客服",
	"转人工",
	"manual",
	"human",
	"agent",
}

// ShouldTransferToHuman centralizes the default handoff heuristic used by
// legacy AI facades while the AI module becomes the primary business entry.
func ShouldTransferToHuman(query string, sessionHistory []models.Message) bool {
	query = strings.ToLower(query)

	for _, keyword := range humanHandoffKeywords {
		if strings.Contains(query, keyword) {
			return true
		}
	}

	if strings.Contains(query, "投诉") || strings.Contains(query, "complaint") {
		return true
	}

	return len(sessionHistory) > 5
}

// BuildSessionSummaryUnavailable provides the default summary when summary generation fails.
func BuildSessionSummaryUnavailable() string {
	return "无法生成会话摘要"
}
