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

// TestAutomationGamificationScriptWritesEvidence 走通
// test-automation-gamification-acceptance.sh 全流程:触发器创建（事件名
// 归一化）/三负例/dry-run 匹配无副作用/真实运行落标签/runs 审计对账/
// 删除与 404,排行榜分数精确对账/日期窗/非法日期 400/部门过滤,校验
// 证据文件与 manifest 内容。
func TestAutomationGamificationScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"EVIDENCE_DIR=%q bash ./test-automation-gamification-acceptance.sh",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected automation/gamification acceptance success, err=%v output=%s", err, string(output))
	}

	registered, statErr := filepath.Glob(filepath.Join(evidenceDir, "agent-register-st7-agent-*.txt"))
	if statErr != nil || len(registered) != 2 {
		t.Fatalf("expected 2 agent register evidence files, got %v (%v)", registered, statErr)
	}
	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"build-output.txt",
		"ready.txt",
		"server-log.txt",
		"unauthenticated-automations.txt",
		"register-admin.txt",
		"agent-create-a1.txt",
		"agent-create-a2.txt",
		"agent-a1-online.txt",
		"agent-a2-online.txt",
		"customer-create-op.txt",
		"customer-create-a.txt",
		"customer-create-b.txt",
		"automation-baseline.txt",
		"automation-create.txt",
		"automation-unsupported-event.txt",
		"automation-empty-name.txt",
		"automation-delay-misplaced.txt",
		"automation-list.txt",
		"ticket-op-create.txt",
		"automation-dry-run.txt",
		"ticket-op-after-dry-run.txt",
		"automation-run-real.txt",
		"ticket-op-after-run.txt",
		"automation-runs-list.txt",
		"automation-run-missing-trigger.txt",
		"automation-delete.txt",
		"automation-list-after-delete.txt",
		"automation-delete-missing.txt",
		"gamification-unauthenticated.txt",
		"ticket-a1-create.txt",
		"ticket-a1-assign.txt",
		"ticket-a1-resolve.txt",
		"satisfaction-a1-create.txt",
		"ticket-a2-create.txt",
		"ticket-a3-create.txt",
		"satisfaction-a3-create.txt",
		"ticket-b1-create.txt",
		"ticket-b1-assign.txt",
		"ticket-b1-resolve.txt",
		"satisfaction-b1-create.txt",
		"leaderboard-days.txt",
		"leaderboard-date-range.txt",
		"leaderboard-bad-date.txt",
		"leaderboard-dept-filter.txt",
		"leaderboard-dept-nomatch.txt",
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
		"step=setup",
		"step=automation_chain",
		"step=batch_run_chain",
		"step=gamification_chain",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, step) {
			t.Fatalf("expected %q in summary, got %s", step, summaryText)
		}
	}

	// 负例语义:未认证 401、非法事件/空名/delay 错位 400、手动运行不存在
	// 触发器 400、删除不存在 404、非法日期 400。
	negativeExpectations := []struct {
		file string
		code string
		msg  string
	}{
		{"unauthenticated-automations.txt", "401", ""},
		{"automation-unsupported-event.txt", "400", "unsupported event"},
		{"automation-empty-name.txt", "400", "name required"},
		{"automation-delay-misplaced.txt", "400", "delay must be the last action"},
		{"automation-run-missing-trigger.txt", "400", "trigger not found"},
		{"automation-delete-missing.txt", "404", "trigger not found"},
		{"gamification-unauthenticated.txt", "401", ""},
		{"leaderboard-bad-date.txt", "400", "Invalid start_date format"},
	}
	for _, want := range negativeExpectations {
		content, readErr := os.ReadFile(filepath.Join(evidenceDir, want.file))
		if readErr != nil {
			t.Fatalf("read %s: %v", want.file, readErr)
		}
		if !strings.Contains(string(content), "HTTP/1.1 "+want.code) ||
			!strings.Contains(string(content), want.msg) {
			t.Fatalf("expected %s in %s, got %s", want.code+" "+want.msg, want.file, string(content))
		}
	}

	// 正面证据:事件名归一化、dry-run 不落副作用、真实运行落标签、
	// runs 审计 success、排行榜分数精确对账。
	positiveExpectations := []struct {
		file string
		msg  string
	}{
		{"automation-create.txt", `"event":"ticket.updated"`},
		{"automation-dry-run.txt", `"dry_run":true`},
		{"ticket-op-after-run.txt", "st7-auto-hit"},
		{"automation-runs-list.txt", `"status":"success"`},
		{"leaderboard-days.txt", `"score":130`},
	}
	for _, want := range positiveExpectations {
		content, readErr := os.ReadFile(filepath.Join(evidenceDir, want.file))
		if readErr != nil {
			t.Fatalf("read %s: %v", want.file, readErr)
		}
		if !strings.Contains(string(content), want.msg) {
			t.Fatalf("expected %q in %s, got %s", want.msg, want.file, string(content))
		}
	}
	// dry-run 无副作用:标签在 dry-run 后不得出现。
	dryTicket, err := os.ReadFile(filepath.Join(evidenceDir, "ticket-op-after-dry-run.txt"))
	if err != nil {
		t.Fatalf("read dry-run ticket: %v", err)
	}
	if strings.Contains(string(dryTicket), "st7-auto-hit") {
		t.Fatalf("expected no side effect after dry run, got %s", string(dryTicket))
	}

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "automation-gamification"`,
		`"mode": "runtime-automation-chain"`,
		`"unauthenticated_automations_rejected_401": "true"`,
		`"agents_and_customers_prepared": "true"`,
		`"automation_baseline_empty": "true"`,
		`"automation_created_with_normalized_event": "true"`,
		`"automation_unsupported_event_rejected_400": "true"`,
		`"automation_empty_name_rejected_400": "true"`,
		`"automation_delay_misplaced_rejected_400": "true"`,
		`"automation_listed": "true"`,
		`"automation_dry_run_matched_without_side_effect": "true"`,
		`"automation_run_applied_to_ticket": "true"`,
		`"automation_runs_listed": "true"`,
		`"automation_run_missing_trigger_rejected_400": "true"`,
		`"automation_deleted_and_gone": "true"`,
		`"automation_delete_missing_rejected_404": "true"`,
		`"gamification_unauthenticated_rejected_401": "true"`,
		`"leaderboard_reconciled_with_scores": "true"`,
		`"leaderboard_date_range_reconciled": "true"`,
		`"leaderboard_bad_date_rejected_400": "true"`,
		`"leaderboard_department_filtered": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
