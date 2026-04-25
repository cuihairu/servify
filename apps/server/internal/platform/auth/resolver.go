package auth

import (
	"strings"

	"servify/apps/server/internal/config"
)

// Resolver expands roles and explicit claims into effective permissions.
type Resolver struct {
	RBAC config.RBACConfig
}

func (r Resolver) ExpandPermissions(roles, explicit []string) []string {
	perms := append([]string(nil), explicit...)
	if r.RBAC.Enabled {
		for _, role := range roles {
			for _, p := range r.RBAC.Roles[role] {
				if s := strings.TrimSpace(p); s != "" {
					perms = append(perms, s)
				}
			}
		}
		return dedupeStrings(perms)
	}

	for _, role := range roles {
		switch role {
		case "admin":
			perms = append(perms, "*")
		case "agent":
			perms = append(perms,
				"tickets.read", "tickets.write",
				"customers.read",
				"agents.read",
				"custom_fields.read",
				"session_transfer.read", "session_transfer.write",
				"satisfaction.read", "satisfaction.write",
				"workspace.read",
				"macros.read",
				"integrations.read",
				"voice.read", "voice.write",
			)
		}
	}
	return dedupeStrings(perms)
}
