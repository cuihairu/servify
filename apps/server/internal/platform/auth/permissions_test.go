package auth

import (
	"testing"

	"servify/apps/server/internal/config"
)

func TestHasPermission_WildcardsAndExact(t *testing.T) {
	tests := []struct {
		name     string
		granted  []string
		required string
		want     bool
	}{
		{"star", []string{"*"}, "tickets.read", true},
		{"exact", []string{"tickets.read"}, "tickets.read", true},
		{"prefixStar", []string{"tickets.*"}, "tickets.read", true},
		{"prefixStarNested", []string{"tickets.*"}, "tickets.write", true},
		{"noMatch", []string{"customers.read"}, "tickets.read", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasPermission(tt.granted, tt.required); got != tt.want {
				t.Fatalf("HasPermission(%v, %q)=%v want %v", tt.granted, tt.required, got, tt.want)
			}
		})
	}
}

func TestResolverExpandPermissions(t *testing.T) {
	t.Run("rbac enabled", func(t *testing.T) {
		got := (Resolver{
			RBAC: config.RBACConfig{
				Enabled: true,
				Roles: map[string][]string{
					"viewer": {"tickets.read"},
				},
			},
		}).ExpandPermissions([]string{"viewer"}, []string{"customers.read"})
		if len(got) != 2 || got[0] != "customers.read" || got[1] != "tickets.read" {
			t.Fatalf("unexpected permissions: %v", got)
		}
	})

	t.Run("fallback role mapping", func(t *testing.T) {
		got := (Resolver{}).ExpandPermissions([]string{"admin"}, nil)
		if len(got) != 1 || got[0] != "*" {
			t.Fatalf("unexpected fallback permissions: %v", got)
		}
	})

	// RBAC 关闭时的 agent 兜底映射必须覆盖 workspace.write：全能工作台的
	// POST 路由（发消息/认领/转接/关单）挂在 RequireResourcePermission("workspace")
	// 组下，POST → resource.write，缺写权限会导致坐席被 403 挡在工作台外。
	t.Run("fallback agent mapping includes workspace write", func(t *testing.T) {
		got := (Resolver{}).ExpandPermissions([]string{"agent"}, nil)
		want := []string{
			"tickets.read", "tickets.write",
			"conversations.read",
			"customers.read",
			"agents.read",
			"custom_fields.read",
			"session_transfer.read", "session_transfer.write",
			"satisfaction.read", "satisfaction.write",
			"workspace.read", "workspace.write",
			"macros.read",
			"integrations.read",
			"voice.read", "voice.write",
		}
		if len(got) != len(want) {
			t.Fatalf("fallback agent permissions length=%d want %d: %v", len(got), len(want), got)
		}
		for i, p := range want {
			if got[i] != p {
				t.Fatalf("fallback agent permissions[%d]=%q want %q (all: %v)", i, got[i], p, got)
			}
		}
	})
}
