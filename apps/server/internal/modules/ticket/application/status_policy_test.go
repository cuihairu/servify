package application

import (
	"strings"
	"testing"
)

// TestStatusTransitionPolicyMatrix 钉死状态机全集（与 docs/ticket-workflow.md
// §4 同步维护）。重开政策（todo 195）：resolved → open 允许（客户追问已解决
// 工单时坐席重开同一工单，保留关联历史）；closed 终态不可重开，错误信息明确
// 指引新建工单。
func TestStatusTransitionPolicyMatrix(t *testing.T) {
	policy := NewStatusTransitionPolicy()
	allowed := map[string][]string{
		"":            {"assigned", "resolved", "closed"},
		"open":        {"assigned", "resolved", "closed"},
		"assigned":    {"open", "in_progress", "resolved", "closed"},
		"in_progress": {"open", "resolved", "closed"},
		"resolved":    {"open", "closed"},
		"closed":      {"closed"},
	}
	states := []string{"", "open", "assigned", "in_progress", "resolved", "closed"}
	allowedSet := map[string]bool{}
	for from, targets := range allowed {
		for _, to := range targets {
			allowedSet[from+"->"+to] = true
		}
	}
	// 同态提交幂等允许；toStatus=="" 表示未请求改状态（no-op）恒允许。
	for _, state := range states {
		allowedSet[state+"->"+state] = true
		allowedSet[state+"->"] = true
	}
	for _, from := range states {
		for _, to := range states {
			err := policy.Validate(from, to)
			if allowedSet[from+"->"+to] {
				if err != nil {
					t.Fatalf("%s -> %s should be allowed, got %v", from, to, err)
				}
				continue
			}
			if err == nil {
				t.Fatalf("%s -> %s should be rejected", from, to)
			}
		}
	}

	// 同态提交幂等允许。
	for _, state := range states {
		if err := policy.Validate(state, state); err != nil {
			t.Fatalf("same-status %s submit must stay idempotent, got %v", state, err)
		}
	}

	// closed 终态拒绝语义：错误信息指引新建工单而非笼统 invalid。
	err := policy.Validate("closed", "open")
	if err == nil || !strings.Contains(err.Error(), "closed is terminal") {
		t.Fatalf("closed -> open must explain terminal semantics, got %v", err)
	}
}
