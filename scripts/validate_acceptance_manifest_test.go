package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateAcceptanceManifestScriptAcceptsValidDifyManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":           "ok",
		"ai-status.json":        "{}",
		"ai-query.json":         "{}",
		"knowledge-upload.json": "{}",
		"knowledge-sync.json":   "{}",
		"ai-metrics.json":       "{}",
		"manifest.json": `{
  "provider": "dify",
  "mode": "real",
  "status": {
    "knowledge_provider": "dify",
    "knowledge_provider_enabled": "true",
    "knowledge_provider_healthy": "true"
  },
  "checks": {
    "provider_available": "true",
    "query_ok": "true",
    "query_strategy": "dify",
    "knowledge_upload_ok": "true",
    "knowledge_sync_ok": "true"
  },
  "evidence_files": [
    "summary.txt",
    "ai-status.json",
    "ai-query.json",
    "knowledge-upload.json",
    "knowledge-sync.json",
    "ai-metrics.json"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected validator success, err=%v output=%s", err, string(output))
	}
	if !strings.Contains(string(output), "manifest 校验通过") {
		t.Fatalf("expected success output, got %s", string(output))
	}
}

func TestValidateAcceptanceManifestScriptAcceptsValidAuthSessionManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":                             "ok",
		"auth-register.json":                      "{}",
		"auth-login-primary.json":                 "{}",
		"auth-login-secondary.json":               "{}",
		"auth-refresh.json":                       "{}",
		"auth-refresh-reuse-old.json":             "{}",
		"auth-sessions-before.json":               "{}",
		"auth-logout-others.json":                 "{}",
		"auth-sessions-after-logout-others.json":  "{}",
		"auth-logout-current.json":                "{}",
		"auth-sessions-after-logout-current.json": "{}",
		"manifest.json": `{
  "provider": "auth-session",
  "mode": "real",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "register_ok": "true",
    "login_primary_ok": "true",
    "login_secondary_ok": "true",
    "refresh_ok": "true",
    "old_refresh_rejected": "true",
    "sessions_before_ok": "true",
    "logout_others_ok": "true",
    "sessions_after_logout_others_ok": "true",
    "logout_current_ok": "true",
    "post_logout_current_rejected": "true"
  },
  "evidence_files": [
    "summary.txt",
    "auth-register.json",
    "auth-login-primary.json",
    "auth-login-secondary.json",
    "auth-refresh.json",
    "auth-refresh-reuse-old.json",
    "auth-sessions-before.json",
    "auth-logout-others.json",
    "auth-sessions-after-logout-others.json",
    "auth-logout-current.json",
    "auth-sessions-after-logout-current.json"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected validator success, err=%v output=%s", err, string(output))
	}
	if !strings.Contains(string(output), "manifest 校验通过") {
		t.Fatalf("expected success output, got %s", string(output))
	}
}

func TestValidateAcceptanceManifestScriptRejectsInvalidWeKnoraManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":                     "ok",
		"ai-status.json":                  "{}",
		"ai-query.json":                   "{}",
		"knowledge-provider-disable.json": "{}",
		"ai-query-after-disable.json":     "{}",
		"knowledge-provider-enable.json":  "{}",
		"circuit-breaker-reset.json":      "{}",
		"knowledge-upload.json":           "{}",
		"knowledge-sync.json":             "{}",
		"manifest.json": `{
  "provider": "weknora",
  "mode": "real",
  "status": {
    "knowledge_provider": "weknora",
    "knowledge_provider_enabled": "true",
    "knowledge_provider_healthy": "false"
  },
  "checks": {
    "provider_available": "true",
    "knowledge_provider_disable_ok": "true",
    "knowledge_provider_enable_ok": "true",
    "circuit_breaker_reset_ok": "true",
    "fallback_query_ok": "true",
    "fallback_query_strategy": "fallback",
    "knowledge_upload_ok": "true",
    "knowledge_sync_ok": "true"
  },
  "evidence_files": [
    "summary.txt",
    "ai-status.json",
    "ai-query.json",
    "knowledge-provider-disable.json",
    "ai-query-after-disable.json",
    "knowledge-provider-enable.json",
    "circuit-breaker-reset.json",
    "knowledge-upload.json",
    "knowledge-sync.json"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected validator failure, output=%s", string(output))
	}
	if !strings.Contains(string(output), "knowledge_provider_healthy") {
		t.Fatalf("expected failure reason in output, got %s", string(output))
	}
}

func TestValidateAcceptanceManifestScriptAcceptsValidVoicePstnManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":             "ok",
		"webhook-create.json":     "{}",
		"receiver-payloads.jsonl": "{}",
		"manifest.json": `{
  "provider": "voice-pstn",
  "mode": "twilio-signature",
  "status": {
    "exit_code": "0",
    "overall": "passed"
  },
  "checks": {
    "answer_ok": "true",
    "signature_ok": "true",
    "bad_signature_rejected": "true",
    "missing_signature_rejected": "true",
    "unknown_status_rejected": "true",
    "late_invite_short_circuit_ok": "true",
    "duplicate_invite_ok": "true",
    "outbound_ok": "true",
    "hangup_ok": "true",
    "recording_ok": "true",
    "db_call_asserted": "true",
    "db_recording_asserted": "true"
  },
  "evidence_files": [
    "receiver-payloads.jsonl",
    "summary.txt",
    "webhook-create.json"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected validator success, err=%v output=%s", err, string(output))
	}
	if !strings.Contains(string(output), "manifest 校验通过") {
		t.Fatalf("expected success output, got %s", string(output))
	}
}

func TestValidateAcceptanceManifestScriptAcceptsValidWorkspaceManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":                       "ok",
		"admin-auth.json":                   "{}",
		"agent-create-primary.json":         "{}",
		"agent-create-secondary.json":       "{}",
		"session-detail.json":               "{}",
		"session-messages-visitor.json":     "{}",
		"agent-message.json":                "{}",
		"session-messages-after-agent.json": "{}",
		"session-assigned.json":             "{}",
		"session-transferred.json":          "{}",
		"session-closed.json":               "{}",
		"workspace-overview.json":           "{}",
		"unknown-session.json":              "{}",
		"empty-message.json":                "{}",
		"unauthenticated-session.json":      "{}",
		"manifest.json": `{
  "provider": "workspace",
  "mode": "real",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "admin_auth_ok": "true",
    "agents_ready": "true",
    "visitor_ws_ingress_ok": "true",
    "session_created": "true",
    "session_detail_ok": "true",
    "message_list_ok": "true",
    "agent_message_ok": "true",
    "agent_message_persisted": "true",
    "assign_ok": "true",
    "transfer_ok": "true",
    "close_ok": "true",
    "workspace_overview_ok": "true",
    "unknown_session_rejected": "true",
    "empty_message_rejected": "true",
    "unauthenticated_rejected": "true"
  },
  "metrics": {
    "status_after_transfer": "transferred",
    "status_after_close": "closed"
  },
  "evidence_files": [
    "summary.txt",
    "admin-auth.json",
    "agent-create-primary.json",
    "agent-create-secondary.json",
    "session-detail.json",
    "session-messages-visitor.json",
    "agent-message.json",
    "session-messages-after-agent.json",
    "session-assigned.json",
    "session-transferred.json",
    "session-closed.json",
    "workspace-overview.json",
    "unknown-session.json",
    "empty-message.json",
    "unauthenticated-session.json"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected validator success, err=%v output=%s", err, string(output))
	}
	if !strings.Contains(string(output), "manifest 校验通过") {
		t.Fatalf("expected success output, got %s", string(output))
	}
}

func TestValidateAcceptanceManifestScriptAcceptsValidTicketManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":                      "ok",
		"admin-auth.json":                  "{}",
		"ticket-customer.json":             "{}",
		"ticket-agent.json":                "{}",
		"agent-create.json":                "{}",
		"ticket-created.json":              "{}",
		"ticket-updated.json":              "{}",
		"ticket-detail-after-update.json":  "{}",
		"ticket-comment.json":              "{}",
		"ticket-close.json":                "{}",
		"ticket-detail-closed.json":        "{}",
		"ticket-stats.json":                "{}",
		"ticket-export.csv":                "id,title,status\n",
		"unknown-ticket.json":              "{}",
		"ticket-create-missing-title.json": "{}",
		"unauthenticated-tickets.json":     "{}",
		"manifest.json": `{
  "provider": "ticket",
  "mode": "real",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "admin_auth_ok": "true",
    "agents_ready": "true",
    "ticket_created": "true",
    "ticket_updated": "true",
    "comment_added": "true",
    "close_ok": "true",
    "closed_state_verified": "true",
    "stats_ok": "true",
    "export_ok": "true",
    "unknown_ticket_rejected": "true",
    "create_missing_title_rejected": "true",
    "unauthenticated_rejected": "true"
  },
  "evidence_files": [
    "summary.txt",
    "admin-auth.json",
    "ticket-customer.json",
    "ticket-agent.json",
    "agent-create.json",
    "ticket-created.json",
    "ticket-updated.json",
    "ticket-detail-after-update.json",
    "ticket-comment.json",
    "ticket-close.json",
    "ticket-detail-closed.json",
    "ticket-stats.json",
    "ticket-export.csv",
    "unknown-ticket.json",
    "ticket-create-missing-title.json",
    "unauthenticated-tickets.json"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected validator success, err=%v output=%s", err, string(output))
	}
	if !strings.Contains(string(output), "manifest 校验通过") {
		t.Fatalf("expected success output, got %s", string(output))
	}
}

func TestValidateAcceptanceManifestScriptRejectsWorkspaceWithoutCloseEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":                       "ok",
		"admin-auth.json":                   "{}",
		"agent-create-primary.json":         "{}",
		"agent-create-secondary.json":       "{}",
		"session-detail.json":               "{}",
		"session-messages-visitor.json":     "{}",
		"agent-message.json":                "{}",
		"session-messages-after-agent.json": "{}",
		"session-assigned.json":             "{}",
		"session-transferred.json":          "{}",
		"session-closed.json":               "{}",
		"workspace-overview.json":           "{}",
		"unknown-session.json":              "{}",
		"empty-message.json":                "{}",
		"unauthenticated-session.json":      "{}",
		"manifest.json": `{
  "provider": "workspace",
  "mode": "real",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "admin_auth_ok": "true",
    "agents_ready": "true",
    "visitor_ws_ingress_ok": "true",
    "session_created": "true",
    "session_detail_ok": "true",
    "message_list_ok": "true",
    "agent_message_ok": "true",
    "agent_message_persisted": "true",
    "assign_ok": "true",
    "transfer_ok": "true",
    "close_ok": "true",
    "workspace_overview_ok": "true",
    "unknown_session_rejected": "true",
    "empty_message_rejected": "true",
    "unauthenticated_rejected": "true"
  },
  "metrics": {
    "status_after_transfer": "transferred",
    "status_after_close": "active"
  },
  "evidence_files": [
    "summary.txt",
    "admin-auth.json",
    "agent-create-primary.json",
    "agent-create-secondary.json",
    "session-detail.json",
    "session-messages-visitor.json",
    "agent-message.json",
    "session-messages-after-agent.json",
    "session-assigned.json",
    "session-transferred.json",
    "session-closed.json",
    "workspace-overview.json",
    "unknown-session.json",
    "empty-message.json",
    "unauthenticated-session.json"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected validator failure, output=%s", string(output))
	}
	if !strings.Contains(string(output), "status_after_close") {
		t.Fatalf("expected failure reason in output, got %s", string(output))
	}
}

func TestValidateAcceptanceManifestScriptAcceptsValidSuggestionManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":                       "ok",
		"admin-auth.json":                   "{}",
		"doc-private.json":                  "{}",
		"doc-public-billing.json":           "{}",
		"doc-public-password.json":          "{}",
		"doc-public-agent.json":             "{}",
		"initial-questions.json":            "{}",
		"next-questions-get.json":           "{}",
		"next-questions-post.json":          "{}",
		"next-questions-missing-query.json": "{}",
		"manifest.json": `{
  "provider": "suggestion",
  "mode": "real",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "build_ok": "true",
    "knowledge_docs_ok": "true",
    "initial_questions_ok": "true",
    "next_questions_get_ok": "true",
    "next_questions_post_ok": "true",
    "next_questions_reject_ok": "true"
  },
  "evidence_files": [
    "summary.txt",
    "admin-auth.json",
    "doc-private.json",
    "doc-public-billing.json",
    "doc-public-password.json",
    "doc-public-agent.json",
    "initial-questions.json",
    "next-questions-get.json",
    "next-questions-post.json",
    "next-questions-missing-query.json"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected validator success, err=%v output=%s", err, string(output))
	}
	if !strings.Contains(string(output), "manifest 校验通过") {
		t.Fatalf("expected success output, got %s", string(output))
	}
}

func TestValidateAcceptanceManifestScriptAcceptsValidBackupRestoreManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":         "ok",
		"db-manifest.json":    "{}",
		"files-manifest.json": "{}",
		"manifest.json": `{
  "provider": "backup-restore",
  "mode": "drill-sqlite",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "db_restore_matches_backup": "true",
    "db_survives_post_backup_damage": "true",
    "db_sequence_no_collision": "true",
    "files_restore_matches_backup": "true",
    "files_verify_detects_tamper": "true"
  },
  "evidence_files": [
    "summary.txt",
    "db-manifest.json",
    "files-manifest.json"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected validator success, err=%v output=%s", err, string(output))
	}
	if !strings.Contains(string(output), "manifest 校验通过") {
		t.Fatalf("expected success output, got %s", string(output))
	}
}

func TestValidateAcceptanceManifestScriptRejectsBackupRestoreWithoutDamageCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":         "ok",
		"db-manifest.json":    "{}",
		"files-manifest.json": "{}",
		// 缺 db_survives_post_backup_damage:没有损坏注入对账的演练不算数。
		"manifest.json": `{
  "provider": "backup-restore",
  "mode": "drill-sqlite",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "db_restore_matches_backup": "true",
    "db_sequence_no_collision": "true",
    "files_restore_matches_backup": "true",
    "files_verify_detects_tamper": "true"
  },
  "evidence_files": [
    "summary.txt",
    "db-manifest.json",
    "files-manifest.json"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected validator failure, got success: %s", string(output))
	}
	if !strings.Contains(string(output), "db_survives_post_backup_damage") {
		t.Fatalf("expected missing check named in output, got %s", string(output))
	}
}

func writeAcceptanceFixture(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func TestValidateAcceptanceManifestScriptAcceptsValidPublicSurfaceManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":        "ok",
		"headers-health.txt": "HTTP/1.1 200",
		"cors-echo.txt":      "HTTP/1.1 200",
		"cors-reject.txt":    "HTTP/1.1 200",
		"cors-preflight.txt": "HTTP/1.1 204",
		"body-413.txt":       "HTTP/1.1 413",
		"rate-limit-8.txt":   "HTTP/1.1 429",
		"uploads-file.txt":   "HTTP/1.1 200",
		"uploads-dir.txt":    "HTTP/1.1 404",
		"ws-reject.txt":      "HTTP/1.1 403",
		"ws-admit.txt":       "HTTP/1.1 101",
		"manifest.json": `{
  "provider": "public-surface",
  "mode": "runtime-security-baseline",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "build_ok": "true",
    "ready_ok": "true",
    "security_headers_ok": "true",
    "cors_allowlisted_origin_echoed": "true",
    "cors_foreign_origin_rejected": "true",
    "cors_preflight_vary_origin": "true",
    "oversized_body_rejected_413": "true",
    "rate_limit_enforced_429": "true",
    "uploads_file_served": "true",
    "uploads_directory_404": "true",
    "websocket_foreign_origin_rejected": "true",
    "websocket_allowlisted_origin_admitted": "true"
  },
  "evidence_files": [
    "summary.txt",
    "headers-health.txt",
    "cors-echo.txt",
    "cors-reject.txt",
    "cors-preflight.txt",
    "body-413.txt",
    "rate-limit-8.txt",
    "uploads-file.txt",
    "uploads-dir.txt",
    "ws-reject.txt",
    "ws-admit.txt"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected validator success, err=%v output=%s", err, string(output))
	}
	if !strings.Contains(string(output), "manifest 校验通过") {
		t.Fatalf("expected success output, got %s", string(output))
	}
}

func TestValidateAcceptanceManifestScriptRejectsPublicSurfaceWithoutWSOriginCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":        "ok",
		"headers-health.txt": "HTTP/1.1 200",
		"cors-echo.txt":      "HTTP/1.1 200",
		"cors-reject.txt":    "HTTP/1.1 200",
		"cors-preflight.txt": "HTTP/1.1 204",
		"body-413.txt":       "HTTP/1.1 413",
		"rate-limit-8.txt":   "HTTP/1.1 429",
		"uploads-file.txt":   "HTTP/1.1 200",
		"uploads-dir.txt":    "HTTP/1.1 404",
		"ws-reject.txt":      "HTTP/1.1 403",
		"ws-admit.txt":       "HTTP/1.1 101",
		// 缺 websocket_foreign_origin_rejected:WS 建连没做拒绝对账不算数。
		"manifest.json": `{
  "provider": "public-surface",
  "mode": "runtime-security-baseline",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "build_ok": "true",
    "ready_ok": "true",
    "security_headers_ok": "true",
    "cors_allowlisted_origin_echoed": "true",
    "cors_foreign_origin_rejected": "true",
    "cors_preflight_vary_origin": "true",
    "oversized_body_rejected_413": "true",
    "rate_limit_enforced_429": "true",
    "uploads_file_served": "true",
    "uploads_directory_404": "true",
    "websocket_allowlisted_origin_admitted": "true"
  },
  "evidence_files": [
    "summary.txt",
    "headers-health.txt",
    "cors-echo.txt",
    "cors-reject.txt",
    "cors-preflight.txt",
    "body-413.txt",
    "rate-limit-8.txt",
    "uploads-file.txt",
    "uploads-dir.txt",
    "ws-reject.txt",
    "ws-admit.txt"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected validator failure, got success: %s", string(output))
	}
	if !strings.Contains(string(output), "websocket_foreign_origin_rejected") {
		t.Fatalf("expected missing check named in output, got %s", string(output))
	}
}

func TestValidateAcceptanceManifestScriptAcceptsValidAuthAuditManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":               "ok",
		"login-blocked.txt":         "HTTP/1.1 403",
		"login-bad-credentials.txt": "HTTP/1.1 401",
		"login-clean.txt":           "HTTP/1.1 200",
		"audit-logins.txt":          "HTTP/1.1 200",
		"audit-registers.txt":       "HTTP/1.1 200",
		"manifest.json": `{
  "provider": "auth-audit",
  "mode": "runtime-risk-enforcement",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "build_ok": "true",
    "ready_ok": "true",
    "risky_login_blocked_403": "true",
    "risky_login_audited": "true",
    "bad_credentials_rejected_401": "true",
    "bad_credentials_audited": "true",
    "clean_login_ok": "true",
    "clean_login_audited": "true",
    "credentials_redacted_in_audit": "true",
    "register_audited": "true"
  },
  "evidence_files": [
    "summary.txt",
    "login-blocked.txt",
    "login-bad-credentials.txt",
    "login-clean.txt",
    "audit-logins.txt",
    "audit-registers.txt"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected validator success, err=%v output=%s", err, string(output))
	}
	if !strings.Contains(string(output), "manifest 校验通过") {
		t.Fatalf("expected success output, got %s", string(output))
	}
}

func TestValidateAcceptanceManifestScriptRejectsAuthAuditWithoutRedactionCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":               "ok",
		"login-blocked.txt":         "HTTP/1.1 403",
		"login-bad-credentials.txt": "HTTP/1.1 401",
		"login-clean.txt":           "HTTP/1.1 200",
		"audit-logins.txt":          "HTTP/1.1 200",
		"audit-registers.txt":       "HTTP/1.1 200",
		// 缺 credentials_redacted_in_audit:审计未对账凭据脱敏不算数。
		"manifest.json": `{
  "provider": "auth-audit",
  "mode": "runtime-risk-enforcement",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "build_ok": "true",
    "ready_ok": "true",
    "risky_login_blocked_403": "true",
    "risky_login_audited": "true",
    "bad_credentials_rejected_401": "true",
    "bad_credentials_audited": "true",
    "clean_login_ok": "true",
    "clean_login_audited": "true",
    "register_audited": "true"
  },
  "evidence_files": [
    "summary.txt",
    "login-blocked.txt",
    "login-bad-credentials.txt",
    "login-clean.txt",
    "audit-logins.txt",
    "audit-registers.txt"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected validator failure, got success: %s", string(output))
	}
	if !strings.Contains(string(output), "credentials_redacted_in_audit") {
		t.Fatalf("expected missing check named in output, got %s", string(output))
	}
}
