package application

import (
	"context"

	"servify/apps/server/internal/platform/knowledgeprovider"
)

type PolicyDecision struct {
	Allowed bool
	Reason  string
}

type PolicyHook interface {
	Evaluate(ctx context.Context, req AIRequest) (PolicyDecision, error)
}

// NoEvidenceDecision is the pre-model no-evidence policy decision. When Apply
// is true, orchestration short-circuits before model/tool calls and returns
// Action/Reason to the delivery layer for the fixed visitor-facing response.
type NoEvidenceDecision struct {
	Apply  bool
	Action string
	Reason string
}

// NoEvidencePolicy evaluates retrieval hits before model/tool execution.
type NoEvidencePolicy interface {
	EvaluateNoEvidence(ctx context.Context, req AIRequest, hits []knowledgeprovider.KnowledgeHit) NoEvidenceDecision
}

type PromptAuditRecord struct {
	PromptVersion   string
	TaskType        TaskType
	Provider        string
	MessageCount    int
	RetrievalHits   int
	SystemPromptSet bool
}

type PromptAuditRecorder interface {
	RecordPrompt(ctx context.Context, record PromptAuditRecord) error
}
