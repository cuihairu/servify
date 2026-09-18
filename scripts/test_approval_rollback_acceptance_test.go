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

// TestApprovalRollbackScriptWritesEvidence 走通 test-approval-rollback-acceptance.sh
// 全流程:双管理员互审的 scoped config 治理链路(change control 强制、
// 高风险回滚审批、自审 403 职责分离、快照恢复、跨人验证、history 与
// 审计对账),校验证据文件与 manifest 内容。
func TestApprovalRollbackScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"EVIDENCE_DIR=%q bash ./test-approval-rollback-acceptance.sh",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected approval rollback acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"register-operator.txt",
		"register-reviewer.txt",
		"reviewer-promote.txt",
		"reviewer-login.txt",
		"config-before.txt",
		"unauthenticated-write.txt",
		"put-no-change-control.txt",
		"put-update-1.txt",
		"put-update-2.txt",
		"history-list.txt",
		"rollback-no-approval.txt",
		"approve-self.txt",
		"rollback-self-approval.txt",
		"approve-reviewer.txt",
		"rollback-restore.txt",
		"config-after-rollback.txt",
		"verify-update-same-actor.txt",
		"verify-update-cross.txt",
		"verify-rollback-same-actor.txt",
		"verify-rollback-cross.txt",
		"history-final.txt",
		"audit-update.txt",
		"audit-rollback.txt",
		"audit-approve.txt",
		"audit-verify.txt",
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
		"step=users_setup",
		"step=negative_guards",
		"step=approval_rollback",
		"step=verification",
		"step=history_reconciliation",
		"step=audit_reconciliation",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, step) {
			t.Fatalf("expected %q in summary, got %s", step, summaryText)
		}
	}

	// 职责分离负例:自审回滚与同操作人验证都必须 403 留档。
	for _, name := range []string{
		"rollback-self-approval.txt",
		"verify-update-same-actor.txt",
		"verify-rollback-same-actor.txt",
	} {
		raw, err := os.ReadFile(filepath.Join(evidenceDir, name))
		if err != nil {
			t.Fatalf("read separation evidence %s: %v", name, err)
		}
		if !strings.Contains(string(raw), "HTTP/1.1 403") {
			t.Fatalf("expected 403 separation rejection in %s, got %s", name, string(raw))
		}
	}

	// 快照恢复:回滚后 GET 读到的是 U1 基线（threshold=9 + step_up）。
	restored, err := os.ReadFile(filepath.Join(evidenceDir, "config-after-rollback.txt"))
	if err != nil {
		t.Fatalf("read restored config evidence: %v", err)
	}
	restoredText := string(restored)
	if !strings.Contains(restoredText, `"multi_public_ip_threshold":9`) ||
		!strings.Contains(restoredText, `"login_enforcement":"step_up"`) {
		t.Fatalf("expected U1 snapshot restored, got %s", restoredText)
	}

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "approval-rollback"`,
		`"mode": "runtime-governance-evidence"`,
		`"unauthenticated_write_rejected_401": "true"`,
		`"second_admin_registration_degraded": "true"`,
		`"put_change_control_enforced_400": "true"`,
		`"update_change_control_recorded": "true"`,
		`"rollback_requires_approval_400": "true"`,
		`"self_approval_rollback_rejected_403": "true"`,
		`"reviewer_approval_rollback_ok": "true"`,
		`"rollback_snapshot_restored": "true"`,
		`"verify_same_actor_rejected_403": "true"`,
		`"cross_reviewer_verify_ok": "true"`,
		`"history_reconciliation_ok": "true"`,
		`"audit_scoped_config_reconciled": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
