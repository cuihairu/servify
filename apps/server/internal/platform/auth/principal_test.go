package auth

import "testing"

func TestDerivePrincipalKind(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]interface{}
		roles   []string
		want    string
	}{
		{
			name:    "explicit service token type wins",
			payload: map[string]interface{}{"token_type": "service", "roles": []string{"admin"}},
			want:    PrincipalService,
		},
		{
			name:    "admin role infers admin principal",
			payload: map[string]interface{}{"roles": []string{"admin"}},
			roles:   []string{"admin"},
			want:    PrincipalAdmin,
		},
		{
			name:    "agent role infers agent principal",
			payload: map[string]interface{}{"roles": []string{"agent"}},
			roles:   []string{"agent"},
			want:    PrincipalAgent,
		},
		{
			name:    "user subject falls back to end user",
			payload: map[string]interface{}{"sub": "customer-1"},
			want:    PrincipalEndUser,
		},
		{
			name:    "guest token type infers end user (B3-2)",
			payload: map[string]interface{}{"typ": "guest", "sid": "ws_1"},
			want:    PrincipalEndUser,
		},
		{
			name:    "explicit principal kind overrides guest typ",
			payload: map[string]interface{}{"typ": "guest", "token_type": "agent"},
			want:    PrincipalAgent,
		},
		{
			name:    "missing signals stays unknown",
			payload: map[string]interface{}{},
			want:    PrincipalUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := derivePrincipalKind(tt.payload, tt.roles); got != tt.want {
				t.Fatalf("derivePrincipalKind() = %q want %q", got, tt.want)
			}
		})
	}
}

func TestExtractClaims_ProjectTenantAndWorkspace(t *testing.T) {
	claims := extractClaims(map[string]interface{}{
		"user_id":      9,
		"roles":        []string{"agent"},
		"tenant_id":    "tenant-a",
		"workspace_id": "workspace-1",
	}, Resolver{})

	if claims.TenantID != "tenant-a" {
		t.Fatalf("TenantID = %q want tenant-a", claims.TenantID)
	}
	if claims.WorkspaceID != "workspace-1" {
		t.Fatalf("WorkspaceID = %q want workspace-1", claims.WorkspaceID)
	}
}
