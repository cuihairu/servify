package auth

import "strings"

const (
	PrincipalUnknown = "unknown"
	PrincipalEndUser = "end_user"
	PrincipalAgent   = "agent"
	PrincipalAdmin   = "admin"
	PrincipalService = "service"
)

func normalizePrincipalKind(v interface{}) string {
	s, _ := v.(string)
	switch strings.ToLower(strings.TrimSpace(s)) {
	case PrincipalEndUser:
		return PrincipalEndUser
	case PrincipalAgent:
		return PrincipalAgent
	case PrincipalAdmin:
		return PrincipalAdmin
	case PrincipalService:
		return PrincipalService
	default:
		return ""
	}
}

func derivePrincipalKind(payload map[string]interface{}, roles []string) string {
	if kind := normalizePrincipalKind(firstValue(
		payload["principal_kind"],
		payload["principal_type"],
		payload["subject_type"],
		payload["token_type"],
	)); kind != "" {
		return kind
	}

	// 访客 token（typ=guest，§10 #2 / D6）：语义上就是 end_user——REST 面
	// （如 POST /api/v1/ai/feedback 的会话绑定校验）按 sid 提取 session_id
	// 后以 end_user 口径校验；WS 握手仍走独立 validator，不受此推导影响。
	if typ, _ := payload["typ"].(string); strings.EqualFold(strings.TrimSpace(typ), "guest") {
		return PrincipalEndUser
	}

	for _, role := range roles {
		switch strings.ToLower(strings.TrimSpace(role)) {
		case PrincipalAdmin, "super_admin":
			return PrincipalAdmin
		case PrincipalAgent:
			return PrincipalAgent
		}
	}

	if _, ok := firstNonNil(payload["user_id"], payload["sub"]); ok {
		return PrincipalEndUser
	}

	return PrincipalUnknown
}
