package scripts

import (
	"fmt"
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

func TestValidateAcceptanceManifestScriptAcceptsValidRefreshReuseManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":               "ok",
		"off-refresh-reuse.txt":     "HTTP/1.1 401",
		"off-refresh-alive.txt":     "HTTP/1.1 200",
		"revoke-refresh-reuse.txt":  "HTTP/1.1 401",
		"revoke-refresh-latest.txt": "HTTP/1.1 401",
		"revoke-relogin.txt":        "HTTP/1.1 200",
		"audit-refresh.txt":         "HTTP/1.1 200",
		"manifest.json": `{
  "provider": "refresh-reuse",
  "mode": "runtime-family-revocation",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "build_ok": "true",
    "ready_ok": "true",
    "off_reuse_rejected_401": "true",
    "off_session_still_alive": "true",
    "revoke_reuse_rejected_401": "true",
    "revoke_family_latest_token_dead": "true",
    "revoke_relogin_ok": "true",
    "audit_refresh_rejections_audited": "true",
    "audit_refresh_success_audited": "true"
  },
  "evidence_files": [
    "summary.txt",
    "off-refresh-reuse.txt",
    "off-refresh-alive.txt",
    "revoke-refresh-reuse.txt",
    "revoke-refresh-latest.txt",
    "revoke-relogin.txt",
    "audit-refresh.txt"
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

func TestValidateAcceptanceManifestScriptRejectsRefreshReuseWithoutFamilyDeadCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":               "ok",
		"off-refresh-reuse.txt":     "HTTP/1.1 401",
		"off-refresh-alive.txt":     "HTTP/1.1 200",
		"revoke-refresh-reuse.txt":  "HTTP/1.1 401",
		"revoke-refresh-latest.txt": "HTTP/1.1 401",
		"revoke-relogin.txt":        "HTTP/1.1 200",
		"audit-refresh.txt":         "HTTP/1.1 200",
		// 缺 revoke_family_latest_token_dead:没验证"家族最新 token 一并
		// 失效"就不算家族吊销闭环。
		"manifest.json": `{
  "provider": "refresh-reuse",
  "mode": "runtime-family-revocation",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "build_ok": "true",
    "ready_ok": "true",
    "off_reuse_rejected_401": "true",
    "off_session_still_alive": "true",
    "revoke_reuse_rejected_401": "true",
    "revoke_relogin_ok": "true",
    "audit_refresh_rejections_audited": "true",
    "audit_refresh_success_audited": "true"
  },
  "evidence_files": [
    "summary.txt",
    "off-refresh-reuse.txt",
    "off-refresh-alive.txt",
    "revoke-refresh-reuse.txt",
    "revoke-refresh-latest.txt",
    "revoke-relogin.txt",
    "audit-refresh.txt"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected validator failure, got success: %s", string(output))
	}
	if !strings.Contains(string(output), "revoke_family_latest_token_dead") {
		t.Fatalf("expected missing check named in output, got %s", string(output))
	}
}

func TestValidateAcceptanceManifestScriptAcceptsValidApprovalRollbackManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":                  "ok",
		"unauthenticated-write.txt":    "HTTP/1.1 401",
		"register-operator.txt":        "HTTP/1.1 201",
		"register-reviewer.txt":        "HTTP/1.1 201",
		"reviewer-promote.txt":         "promoted_rows=1",
		"put-no-change-control.txt":    "HTTP/1.1 400",
		"put-update-1.txt":             "HTTP/1.1 200",
		"rollback-no-approval.txt":     "HTTP/1.1 400",
		"rollback-self-approval.txt":   "HTTP/1.1 403",
		"approve-reviewer.txt":         "HTTP/1.1 200",
		"rollback-restore.txt":         "HTTP/1.1 200",
		"config-after-rollback.txt":    "HTTP/1.1 200",
		"verify-update-same-actor.txt": "HTTP/1.1 403",
		"verify-update-cross.txt":      "HTTP/1.1 200",
		"verify-rollback-cross.txt":    "HTTP/1.1 200",
		"history-final.txt":            "HTTP/1.1 200",
		"audit-approve.txt":            "HTTP/1.1 200",
		"audit-verify.txt":             "HTTP/1.1 200",
		"manifest.json": `{
  "provider": "approval-rollback",
  "mode": "runtime-governance-evidence",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "build_ok": "true",
    "ready_ok": "true",
    "unauthenticated_write_rejected_401": "true",
    "second_admin_registration_degraded": "true",
    "put_change_control_enforced_400": "true",
    "update_change_control_recorded": "true",
    "rollback_requires_approval_400": "true",
    "self_approval_rollback_rejected_403": "true",
    "reviewer_approval_rollback_ok": "true",
    "rollback_snapshot_restored": "true",
    "verify_same_actor_rejected_403": "true",
    "cross_reviewer_verify_ok": "true",
    "history_reconciliation_ok": "true",
    "audit_scoped_config_reconciled": "true"
  },
  "evidence_files": [
    "summary.txt",
    "unauthenticated-write.txt",
    "register-operator.txt",
    "register-reviewer.txt",
    "reviewer-promote.txt",
    "put-no-change-control.txt",
    "put-update-1.txt",
    "rollback-no-approval.txt",
    "rollback-self-approval.txt",
    "approve-reviewer.txt",
    "rollback-restore.txt",
    "config-after-rollback.txt",
    "verify-update-same-actor.txt",
    "verify-update-cross.txt",
    "verify-rollback-cross.txt",
    "history-final.txt",
    "audit-approve.txt",
    "audit-verify.txt"
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

func TestValidateAcceptanceManifestScriptRejectsApprovalRollbackWithoutReviewerApproval(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":                  "ok",
		"unauthenticated-write.txt":    "HTTP/1.1 401",
		"register-operator.txt":        "HTTP/1.1 201",
		"register-reviewer.txt":        "HTTP/1.1 201",
		"reviewer-promote.txt":         "promoted_rows=1",
		"put-no-change-control.txt":    "HTTP/1.1 400",
		"put-update-1.txt":             "HTTP/1.1 200",
		"rollback-no-approval.txt":     "HTTP/1.1 400",
		"rollback-self-approval.txt":   "HTTP/1.1 403",
		"approve-reviewer.txt":         "HTTP/1.1 200",
		"rollback-restore.txt":         "HTTP/1.1 200",
		"config-after-rollback.txt":    "HTTP/1.1 200",
		"verify-update-same-actor.txt": "HTTP/1.1 403",
		"verify-update-cross.txt":      "HTTP/1.1 200",
		"verify-rollback-cross.txt":    "HTTP/1.1 200",
		"history-final.txt":            "HTTP/1.1 200",
		"audit-approve.txt":            "HTTP/1.1 200",
		"audit-verify.txt":             "HTTP/1.1 200",
		// 缺 reviewer_approval_rollback_ok:没有"独立审批人放行后回滚
		// 成功"的证据就不算职责分离闭环。
		"manifest.json": `{
  "provider": "approval-rollback",
  "mode": "runtime-governance-evidence",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "build_ok": "true",
    "ready_ok": "true",
    "unauthenticated_write_rejected_401": "true",
    "second_admin_registration_degraded": "true",
    "put_change_control_enforced_400": "true",
    "update_change_control_recorded": "true",
    "rollback_requires_approval_400": "true",
    "self_approval_rollback_rejected_403": "true",
    "rollback_snapshot_restored": "true",
    "verify_same_actor_rejected_403": "true",
    "cross_reviewer_verify_ok": "true",
    "history_reconciliation_ok": "true",
    "audit_scoped_config_reconciled": "true"
  },
  "evidence_files": [
    "summary.txt",
    "unauthenticated-write.txt",
    "register-operator.txt",
    "register-reviewer.txt",
    "reviewer-promote.txt",
    "put-no-change-control.txt",
    "put-update-1.txt",
    "rollback-no-approval.txt",
    "rollback-self-approval.txt",
    "approve-reviewer.txt",
    "rollback-restore.txt",
    "config-after-rollback.txt",
    "verify-update-same-actor.txt",
    "verify-update-cross.txt",
    "verify-rollback-cross.txt",
    "history-final.txt",
    "audit-approve.txt",
    "audit-verify.txt"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected validator failure, got success: %s", string(output))
	}
	if !strings.Contains(string(output), "reviewer_approval_rollback_ok") {
		t.Fatalf("expected missing check named in output, got %s", string(output))
	}
}

func TestValidateAcceptanceManifestScriptAcceptsValidSessionTransferManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	evidence := map[string]string{
		"summary.txt":                   "ok",
		"unauthenticated-waiting.txt":   "HTTP/1.1 401",
		"agents-online-list.txt":        "HTTP/1.1 200",
		"to-human-direct.txt":           "HTTP/1.1 200",
		"history-s1.txt":                "HTTP/1.1 200",
		"to-human-queued.txt":           "HTTP/1.1 200",
		"waiting-list.txt":              "HTTP/1.1 200",
		"to-human-duplicate.txt":        "HTTP/1.1 200",
		"process-queue.txt":             "HTTP/1.1 200",
		"waiting-list-after.txt":        "HTTP/1.1 200",
		"to-agent-offline-rejected.txt": "HTTP/1.1 500",
		"to-agent-direct.txt":           "HTTP/1.1 200",
		"cancel-waiting.txt":            "HTTP/1.1 200",
		"cancel-waiting-again.txt":      "HTTP/1.1 200",
		"waiting-list-cancelled.txt":    "HTTP/1.1 200",
		"check-auto.txt":                "HTTP/1.1 200",
		"history-recent.txt":            "HTTP/1.1 200",
		"to-human-unknown-session.txt":  "HTTP/1.1 500",
	}
	names := make([]string, 0, len(evidence))
	for _, name := range []string{
		"summary.txt",
		"unauthenticated-waiting.txt",
		"agents-online-list.txt",
		"to-human-direct.txt",
		"history-s1.txt",
		"to-human-queued.txt",
		"waiting-list.txt",
		"to-human-duplicate.txt",
		"process-queue.txt",
		"waiting-list-after.txt",
		"to-agent-offline-rejected.txt",
		"to-agent-direct.txt",
		"cancel-waiting.txt",
		"cancel-waiting-again.txt",
		"waiting-list-cancelled.txt",
		"check-auto.txt",
		"history-recent.txt",
		"to-human-unknown-session.txt",
	} {
		names = append(names, name)
	}
	evidence["manifest.json"] = fmt.Sprintf(`{
  "provider": "session-transfer",
  "mode": "runtime-transfer-chain",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "build_ok": "true",
    "ready_ok": "true",
    "unauthenticated_rejected_401": "true",
    "agents_ready": "true",
    "agent_online_toggled": "true",
    "visitor_session_created": "true",
    "transfer_to_human_direct_ok": "true",
    "transfer_history_retrievable": "true",
    "transfer_to_waiting_queued": "true",
    "waiting_queue_listed": "true",
    "duplicate_transfer_idempotent": "true",
    "process_queue_dispatched": "true",
    "transfer_to_agent_ok": "true",
    "cancel_waiting_ok": "true",
    "check_auto_ok": "true",
    "recent_history_listed": "true",
    "unknown_session_rejected": "true",
    "offline_target_rejected": "true"
  },
  "evidence_files": [%s]
}`, quotedJSONList(names...))

	writeAcceptanceFixture(t, dir, evidence)

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected validator success, err=%v output=%s", err, string(output))
	}
}

func TestValidateAcceptanceManifestScriptRejectsSessionTransferWithoutNegativeGuards(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	// 缺 unknown_session_rejected / offline_target_rejected:没有负例拒绝
	// 证据就不算转接链路闭环。
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":                   "ok",
		"unauthenticated-waiting.txt":   "HTTP/1.1 401",
		"agents-online-list.txt":        "HTTP/1.1 200",
		"to-human-direct.txt":           "HTTP/1.1 200",
		"history-s1.txt":                "HTTP/1.1 200",
		"to-human-queued.txt":           "HTTP/1.1 200",
		"waiting-list.txt":              "HTTP/1.1 200",
		"to-human-duplicate.txt":        "HTTP/1.1 200",
		"process-queue.txt":             "HTTP/1.1 200",
		"waiting-list-after.txt":        "HTTP/1.1 200",
		"to-agent-offline-rejected.txt": "HTTP/1.1 500",
		"to-agent-direct.txt":           "HTTP/1.1 200",
		"cancel-waiting.txt":            "HTTP/1.1 200",
		"cancel-waiting-again.txt":      "HTTP/1.1 200",
		"waiting-list-cancelled.txt":    "HTTP/1.1 200",
		"check-auto.txt":                "HTTP/1.1 200",
		"history-recent.txt":            "HTTP/1.1 200",
		"to-human-unknown-session.txt":  "HTTP/1.1 500",
		"manifest.json": `{
  "provider": "session-transfer",
  "mode": "runtime-transfer-chain",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "build_ok": "true",
    "ready_ok": "true",
    "unauthenticated_rejected_401": "true",
    "agents_ready": "true",
    "agent_online_toggled": "true",
    "visitor_session_created": "true",
    "transfer_to_human_direct_ok": "true",
    "transfer_history_retrievable": "true",
    "transfer_to_waiting_queued": "true",
    "waiting_queue_listed": "true",
    "duplicate_transfer_idempotent": "true",
    "process_queue_dispatched": "true",
    "transfer_to_agent_ok": "true",
    "cancel_waiting_ok": "true",
    "check_auto_ok": "true",
    "recent_history_listed": "true"
  },
  "evidence_files": [
    "summary.txt",
    "unauthenticated-waiting.txt",
    "agents-online-list.txt",
    "to-human-direct.txt",
    "history-s1.txt",
    "to-human-queued.txt",
    "waiting-list.txt",
    "to-human-duplicate.txt",
    "process-queue.txt",
    "waiting-list-after.txt",
    "to-agent-offline-rejected.txt",
    "to-agent-direct.txt",
    "cancel-waiting.txt",
    "cancel-waiting-again.txt",
    "waiting-list-cancelled.txt",
    "check-auto.txt",
    "history-recent.txt",
    "to-human-unknown-session.txt"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected validator failure, got success: %s", string(output))
	}
	if !strings.Contains(string(output), "unknown_session_rejected") {
		t.Fatalf("expected missing check named in output, got %s", string(output))
	}
}

// quotedJSONList 把证据文件名序列化成 manifest fixture 里的 JSON 数组条目。
func quotedJSONList(names ...string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, fmt.Sprintf("%q", name))
	}
	return strings.Join(quoted, ",\n    ")
}

func TestValidateAcceptanceManifestScriptAcceptsValidSatisfactionManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	// 证据文件与 checks 同名单（fixture 里 content 无关紧要，validator 只校验存在性）。
	names := []string{
		"summary.txt",
		"unauthenticated-list.txt",
		"customer-create.txt",
		"ticket-create.txt",
		"ticket-close.txt",
		"satisfaction-create.txt",
		"satisfaction-duplicate.txt",
		"satisfaction-non-owner.txt",
		"satisfaction-list.txt",
		"satisfaction-detail.txt",
		"satisfaction-update.txt",
		"satisfaction-by-ticket.txt",
		"satisfaction-stats.txt",
		"surveys-list.txt",
		"survey-resend.txt",
		"satisfaction-delete.txt",
		"satisfaction-by-ticket-after-delete.txt",
		"satisfaction-detail-after-delete.txt",
	}
	checks := []string{
		"build_ok",
		"ready_ok",
		"unauthenticated_rejected_401",
		"customer_created",
		"ticket_created",
		"ticket_closed_survey_scheduled",
		"satisfaction_created",
		"duplicate_satisfaction_rejected_409",
		"non_owner_satisfaction_rejected_403",
		"satisfaction_listed",
		"satisfaction_stats_reconciled",
		"surveys_listed",
		"survey_resent",
		"satisfaction_detail_retrievable",
		"satisfaction_comment_updated",
		"satisfaction_by_ticket_retrievable",
		"satisfaction_deleted_and_gone",
	}
	evidence := map[string]string{}
	for _, name := range names {
		evidence[name] = "HTTP/1.1 200"
	}
	evidence["satisfaction-delete.txt"] = "HTTP/1.1 204"
	evidence["satisfaction-by-ticket-after-delete.txt"] = "HTTP/1.1 204"
	evidence["satisfaction-detail-after-delete.txt"] = "HTTP/1.1 404"
	checkLines := make([]string, 0, len(checks))
	for _, check := range checks {
		checkLines = append(checkLines, fmt.Sprintf("    %q: \"true\"", check))
	}
	evidence["manifest.json"] = fmt.Sprintf(`{
  "provider": "satisfaction",
  "mode": "runtime-satisfaction-chain",
  "status": {
    "overall": "passed"
  },
  "checks": {
%s
  },
  "evidence_files": [%s]
}`, strings.Join(checkLines, ",\n"), quotedJSONList(names...))

	writeAcceptanceFixture(t, dir, evidence)

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected validator success, err=%v output=%s", err, string(output))
	}
}

func TestValidateAcceptanceManifestScriptRejectsSatisfactionWithoutNegativeGuards(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	// 缺 duplicate_satisfaction_rejected_409 / non_owner_satisfaction_rejected_403:
	// 没有负例拒绝证据就不算满意度链路闭环。
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":                             "ok",
		"unauthenticated-list.txt":                "HTTP/1.1 401",
		"customer-create.txt":                     "HTTP/1.1 201",
		"ticket-create.txt":                       "HTTP/1.1 201",
		"ticket-close.txt":                        "HTTP/1.1 200",
		"satisfaction-create.txt":                 "HTTP/1.1 201",
		"satisfaction-duplicate.txt":              "HTTP/1.1 409",
		"satisfaction-non-owner.txt":              "HTTP/1.1 403",
		"satisfaction-list.txt":                   "HTTP/1.1 200",
		"satisfaction-detail.txt":                 "HTTP/1.1 200",
		"satisfaction-update.txt":                 "HTTP/1.1 200",
		"satisfaction-by-ticket.txt":              "HTTP/1.1 200",
		"satisfaction-stats.txt":                  "HTTP/1.1 200",
		"surveys-list.txt":                        "HTTP/1.1 200",
		"survey-resend.txt":                       "HTTP/1.1 200",
		"satisfaction-delete.txt":                 "HTTP/1.1 204",
		"satisfaction-by-ticket-after-delete.txt": "HTTP/1.1 204",
		"satisfaction-detail-after-delete.txt":    "HTTP/1.1 404",
		"manifest.json": `{
  "provider": "satisfaction",
  "mode": "runtime-satisfaction-chain",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "build_ok": "true",
    "ready_ok": "true",
    "unauthenticated_rejected_401": "true",
    "customer_created": "true",
    "ticket_created": "true",
    "ticket_closed_survey_scheduled": "true",
    "satisfaction_created": "true",
    "satisfaction_listed": "true",
    "satisfaction_stats_reconciled": "true",
    "surveys_listed": "true",
    "survey_resent": "true",
    "satisfaction_detail_retrievable": "true",
    "satisfaction_comment_updated": "true",
    "satisfaction_by_ticket_retrievable": "true",
    "satisfaction_deleted_and_gone": "true"
  },
  "evidence_files": [
    "summary.txt",
    "unauthenticated-list.txt",
    "customer-create.txt",
    "ticket-create.txt",
    "ticket-close.txt",
    "satisfaction-create.txt",
    "satisfaction-duplicate.txt",
    "satisfaction-non-owner.txt",
    "satisfaction-list.txt",
    "satisfaction-detail.txt",
    "satisfaction-update.txt",
    "satisfaction-by-ticket.txt",
    "satisfaction-stats.txt",
    "surveys-list.txt",
    "survey-resend.txt",
    "satisfaction-delete.txt",
    "satisfaction-by-ticket-after-delete.txt",
    "satisfaction-detail-after-delete.txt"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected validator failure, got success: %s", string(output))
	}
	if !strings.Contains(string(output), "duplicate_satisfaction_rejected_409") {
		t.Fatalf("expected missing check named in output, got %s", string(output))
	}
}

func TestValidateAcceptanceManifestScriptAcceptsValidCustomerAgentManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	// 证据文件与 checks 同名单（fixture 里 content 无关紧要，validator 只校验存在性）。
	names := []string{
		"summary.txt",
		"unauthenticated-list.txt",
		"customer-list.txt",
		"customer-list-search.txt",
		"customer-activity-c1.txt",
		"customer-activity-c2.txt",
		"agent-create-a1.txt",
		"agent-create-a2.txt",
		"agent-create-duplicate.txt",
		"find-available-empty.txt",
		"find-available-skills.txt",
		"online-after-assign.txt",
		"online-after-release.txt",
		"assign-s1.txt",
		"assign-overcapacity.txt",
		"release-s1.txt",
		"release-unassigned.txt",
		"assign-missing-session.txt",
		"assign-missing-agent.txt",
	}
	checks := []string{
		"build_ok",
		"ready_ok",
		"unauthenticated_rejected_401",
		"customer_created",
		"customer_listed_with_filter",
		"customer_activity_reconciled",
		"agent_created",
		"duplicate_agent_rejected_409",
		"find_available_empty_rejected_404",
		"find_available_selected_with_skills",
		"assigned_session_load_increased",
		"assign_overcapacity_rejected",
		"released_session_load_decreased",
		"release_unassigned_rejected_404",
		"assign_missing_session_rejected_404",
		"assign_missing_agent_rejected_404",
	}
	evidence := map[string]string{}
	for _, name := range names {
		evidence[name] = "HTTP/1.1 200"
	}
	checkLines := make([]string, 0, len(checks))
	for _, check := range checks {
		checkLines = append(checkLines, fmt.Sprintf("    %q: \"true\"", check))
	}
	evidence["manifest.json"] = fmt.Sprintf(`{
  "provider": "customer-agent",
  "mode": "runtime-customer-agent-chain",
  "status": {
    "overall": "passed"
  },
  "checks": {
%s
  },
  "evidence_files": [%s]
}`, strings.Join(checkLines, ",\n"), quotedJSONList(names...))

	writeAcceptanceFixture(t, dir, evidence)

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected validator success, err=%v output=%s", err, string(output))
	}
}

func TestValidateAcceptanceManifestScriptRejectsCustomerAgentWithoutNegativeGuards(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	dir := t.TempDir()
	// 缺 find_available_empty_rejected_404 / assign_overcapacity_rejected /
	// release_unassigned_rejected_404:负例守卫不全不算客服链路闭环。
	// 证据文件全部保留,只从 checks 里去掉目标项（validator 先校验证据覆盖）。
	writeAcceptanceFixture(t, dir, map[string]string{
		"summary.txt":                "ok",
		"unauthenticated-list.txt":   "HTTP/1.1 401",
		"customer-list.txt":          "HTTP/1.1 200",
		"customer-list-search.txt":   "HTTP/1.1 200",
		"customer-activity-c1.txt":   "HTTP/1.1 200",
		"customer-activity-c2.txt":   "HTTP/1.1 200",
		"agent-create-a1.txt":        "HTTP/1.1 201",
		"agent-create-a2.txt":        "HTTP/1.1 201",
		"agent-create-duplicate.txt": "HTTP/1.1 409",
		"find-available-empty.txt":   "HTTP/1.1 404",
		"find-available-skills.txt":  "HTTP/1.1 200",
		"online-after-assign.txt":    "HTTP/1.1 200",
		"online-after-release.txt":   "HTTP/1.1 200",
		"assign-s1.txt":              "HTTP/1.1 200",
		"assign-overcapacity.txt":    "HTTP/1.1 500",
		"release-s1.txt":             "HTTP/1.1 200",
		"release-unassigned.txt":     "HTTP/1.1 404",
		"assign-missing-session.txt": "HTTP/1.1 404",
		"assign-missing-agent.txt":   "HTTP/1.1 404",
		"manifest.json": `{
  "provider": "customer-agent",
  "mode": "runtime-customer-agent-chain",
  "status": {
    "overall": "passed"
  },
  "checks": {
    "build_ok": "true",
    "ready_ok": "true",
    "unauthenticated_rejected_401": "true",
    "customer_created": "true",
    "customer_listed_with_filter": "true",
    "customer_activity_reconciled": "true",
    "agent_created": "true",
    "duplicate_agent_rejected_409": "true",
    "find_available_selected_with_skills": "true",
    "assigned_session_load_increased": "true",
    "released_session_load_decreased": "true",
    "assign_missing_session_rejected_404": "true",
    "assign_missing_agent_rejected_404": "true"
  },
  "evidence_files": [
    "summary.txt",
    "unauthenticated-list.txt",
    "customer-list.txt",
    "customer-list-search.txt",
    "customer-activity-c1.txt",
    "customer-activity-c2.txt",
    "agent-create-a1.txt",
    "agent-create-a2.txt",
    "agent-create-duplicate.txt",
    "find-available-empty.txt",
    "find-available-skills.txt",
    "online-after-assign.txt",
    "online-after-release.txt",
    "assign-s1.txt",
    "assign-overcapacity.txt",
    "release-s1.txt",
    "release-unassigned.txt",
    "assign-missing-session.txt",
    "assign-missing-agent.txt"
  ]
}`,
	})

	cmd := exec.Command("bash", "./validate-acceptance-manifest.sh", filepath.Join(dir, "manifest.json"))
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected validator failure, got success: %s", string(output))
	}
	if !strings.Contains(string(output), "find_available_empty_rejected_404") {
		t.Fatalf("expected missing check named in output, got %s", string(output))
	}
}
