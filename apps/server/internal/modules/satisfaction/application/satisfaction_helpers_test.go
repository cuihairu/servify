package application

// 纯函数测试（GORM 写归属下沉 infra 后，与仓储无关的分支留本包）。

import (
	"context"
	"testing"

	platformauth "servify/apps/server/internal/platform/auth"
)

func scopedTestContext(tenantID, workspaceID string) context.Context {
	return platformauth.ContextWithScope(context.Background(), tenantID, workspaceID)
}

func TestDetectSurveyChannel(t *testing.T) {
	cases := map[string]string{
		"chat":  "chat",
		" IM ":  "chat",
		"voice": "voice",
		"phone": "voice",
		"email": "email",
		"weird": "email",
		"":      "email",
	}
	for in, want := range cases {
		if got := detectSurveyChannel(in); got != want {
			t.Fatalf("detectSurveyChannel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContextWithRecordScope(t *testing.T) {
	if tenant, ws := tenantAndWorkspace(contextWithRecordScope(context.Background(), "rt", "rw")); tenant != "rt" || ws != "rw" {
		t.Fatalf("record scope fallback: %q/%q", tenant, ws)
	}
	if tenant, ws := tenantAndWorkspace(contextWithRecordScope(scopedTestContext("ct", "cw"), "rt", "rw")); tenant != "ct" || ws != "cw" {
		t.Fatalf("record scope keeps context: %q/%q", tenant, ws)
	}
}
