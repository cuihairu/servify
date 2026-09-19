package delivery

import (
	"servify/apps/server/internal/models"
	"testing"
)

func TestAIService_BuildPrompt_IncludesDocsAndQuery(t *testing.T) {
	s := NewAIService("", "")
	docs := []models.KnowledgeDoc{{Title: "Intro", Content: "Servify"}}
	p := s.buildPrompt("什么是Servify?", docs)
	if !contains(p, "Intro") || !contains(p, "Servify") || !contains(p, "什么是Servify?") {
		t.Fatalf("prompt missing expected content: %s", p)
	}
}

// tiny helper to avoid importing strings across tests in this package
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if s[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
