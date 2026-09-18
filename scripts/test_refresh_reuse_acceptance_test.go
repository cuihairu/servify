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

// TestRefreshReuseScriptWritesEvidence 走通 test-refresh-reuse-acceptance.sh
// 全流程:off/revoke_family 两阶段起服(同库 sqlite),校验旧 token 重放
// 拒绝、off 会话存活、revoke_family 家族吊销（最新 token 一并死亡）与
// 审计留痕的证据文件与 manifest 内容。
func TestRefreshReuseScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"EVIDENCE_DIR=%q bash ./test-refresh-reuse-acceptance.sh",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected refresh reuse acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"register.txt",
		"off-login.txt",
		"off-refresh-1.txt",
		"off-refresh-reuse.txt",
		"off-refresh-alive.txt",
		"revoke-login.txt",
		"revoke-refresh-1.txt",
		"revoke-refresh-reuse.txt",
		"revoke-refresh-latest.txt",
		"revoke-relogin.txt",
		"audit-refresh.txt",
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
		"step=phase_off",
		"step=phase_revoke",
		"step=audit_reconciliation",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, step) {
			t.Fatalf("expected %q in summary, got %s", step, summaryText)
		}
	}

	alive, err := os.ReadFile(filepath.Join(evidenceDir, "off-refresh-alive.txt"))
	if err != nil {
		t.Fatalf("read alive evidence: %v", err)
	}
	if !strings.Contains(string(alive), "HTTP/1.1 200") {
		t.Fatalf("expected session still alive (200) under off, got %s", string(alive))
	}

	latest, err := os.ReadFile(filepath.Join(evidenceDir, "revoke-refresh-latest.txt"))
	if err != nil {
		t.Fatalf("read latest-token evidence: %v", err)
	}
	if !strings.Contains(string(latest), "HTTP/1.1 401") {
		t.Fatalf("expected family latest token dead (401) under revoke_family, got %s", string(latest))
	}

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "refresh-reuse"`,
		`"mode": "runtime-family-revocation"`,
		`"off_reuse_rejected_401": "true"`,
		`"off_session_still_alive": "true"`,
		`"revoke_reuse_rejected_401": "true"`,
		`"revoke_family_latest_token_dead": "true"`,
		`"revoke_relogin_ok": "true"`,
		`"audit_refresh_rejections_audited": "true"`,
		`"audit_refresh_success_audited": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
