package application

import "fmt"

// StatusTransitionPolicy 是工单状态机（与 docs/ticket-workflow.md §4 同步维护）：
//
//	open ──→ assigned ──→ in_progress ──→ resolved ──→ closed
//	  │         │              │             │    ↖ 重开   │
//	  │         └──────────────┴─────────────┴──────→ open  │
//	  └─────────────────────→ resolved / closed ←───────────┘
//
// 重开政策（todo 195）：客户追问 resolved 工单时坐席重开同一工单
// （resolved → open，保留关联历史与状态流水）；closed 是终态不可重开，
// 客户追问须新建工单（可经 session_id 关联原会话回溯历史）。
type StatusTransitionPolicy struct{}

func NewStatusTransitionPolicy() StatusTransitionPolicy {
	return StatusTransitionPolicy{}
}

func (p StatusTransitionPolicy) Validate(fromStatus, toStatus string) error {
	if toStatus == "" || fromStatus == toStatus {
		return nil
	}

	switch fromStatus {
	case "", "open":
		if toStatus == "assigned" || toStatus == "resolved" || toStatus == "closed" {
			return nil
		}
	case "assigned":
		if toStatus == "open" || toStatus == "in_progress" || toStatus == "resolved" || toStatus == "closed" {
			return nil
		}
	case "in_progress":
		if toStatus == "open" || toStatus == "resolved" || toStatus == "closed" {
			return nil
		}
	case "resolved":
		// 重开：客户追问已解决工单 → 重开同一工单保留关联历史。
		if toStatus == "open" || toStatus == "closed" {
			return nil
		}
	case "closed":
		if toStatus == "closed" {
			return nil
		}
		if toStatus == "open" {
			return fmt.Errorf("invalid status transition: closed -> open: closed is terminal; create a new ticket linked to the same session for customer follow-up")
		}
	}

	return fmt.Errorf("invalid status transition: %s -> %s", fromStatus, toStatus)
}
