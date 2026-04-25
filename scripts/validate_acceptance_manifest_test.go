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
		"summary.txt":                          "ok",
		"auth-register.json":                   "{}",
		"auth-login-primary.json":              "{}",
		"auth-login-secondary.json":            "{}",
		"auth-refresh.json":                    "{}",
		"auth-refresh-reuse-old.json":          "{}",
		"auth-sessions-before.json":            "{}",
		"auth-logout-others.json":              "{}",
		"auth-sessions-after-logout-others.json": "{}",
		"auth-logout-current.json":             "{}",
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

func writeAcceptanceFixture(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}
