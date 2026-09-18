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

// TestAuthAuditScriptWritesEvidence 走通 test-auth-audit-acceptance.sh
// 全流程:stub IP 情报源 + block/off 两阶段起服(同库 sqlite),校验风险
// 拦截、失败审计、凭据脱敏与正常登录放行的证据文件与 manifest 内容。
func TestAuthAuditScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"EVIDENCE_DIR=%q bash ./test-auth-audit-acceptance.sh",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected auth audit acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"ready.txt",
		"register.txt",
		"login-blocked.txt",
		"login-bad-credentials.txt",
		"login-clean.txt",
		"audit-logins.txt",
		"audit-registers.txt",
	} {
		if _, statErr := os.Stat(filepath.Join(evidenceDir, name)); statErr != nil {
			t.Fatalf("expected evidence file %s: %v\noutput=%s", name, statErr, string(output))
		}
	}

	summary, err := os.ReadFile(filepath.Join(evidenceDir, "summary.txt"))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	summaryText := string(summary)
	for _, step := range []string{
		"step=make_build",
		"step=phase_block",
		"step=phase_off",
		"step=audit_reconciliation",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, step) {
			t.Fatalf("expected %q in summary, got %s", step, summaryText)
		}
	}

	blocked, err := os.ReadFile(filepath.Join(evidenceDir, "login-blocked.txt"))
	if err != nil {
		t.Fatalf("read blocked evidence: %v", err)
	}
	if !strings.Contains(string(blocked), "403") {
		t.Fatalf("expected 403 in blocked login evidence, got %s", string(blocked))
	}

	clean, err := os.ReadFile(filepath.Join(evidenceDir, "login-clean.txt"))
	if err != nil {
		t.Fatalf("read clean evidence: %v", err)
	}
	if !strings.Contains(string(clean), `"token"`) {
		t.Fatalf("expected session token in clean login evidence, got %s", string(clean))
	}

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "auth-audit"`,
		`"mode": "runtime-risk-enforcement"`,
		`"ready_ok": "true"`,
		`"risky_login_blocked_403": "true"`,
		`"risky_login_audited": "true"`,
		`"bad_credentials_rejected_401": "true"`,
		`"bad_credentials_audited": "true"`,
		`"clean_login_ok": "true"`,
		`"clean_login_audited": "true"`,
		`"credentials_redacted_in_audit": "true"`,
		`"register_audited": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
