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

// TestStatisticsScriptWritesEvidence 走通 test-statistics-acceptance.sh
// 全流程:来源差化客户、分类/优先级差化工单、客服上线后工单分配、
// 四类统计聚合对账(分类/优先级/绩效排序/来源)、缺日期负例、
// 每日统计更新与幂等、排班 CRUD 与统计对账(不存在客服/非法时间/
// 不存在班次负例),校验证据文件与 manifest 内容。
func TestStatisticsScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"EVIDENCE_DIR=%q bash ./test-statistics-acceptance.sh",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected statistics acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"build-output.txt",
		"ready.txt",
		"unauthenticated-dashboard.txt",
		"register-admin.txt",
		"customer-create-web1.txt",
		"customer-create-web2.txt",
		"customer-create-ref1.txt",
		"ticket-create-t1.txt",
		"ticket-create-t2.txt",
		"ticket-create-t3.txt",
		"ticket-assign-t1-to-a.txt",
		"ticket-assign-t2-to-a.txt",
		"ticket-assign-t3-to-b.txt",
		"stats-category.txt",
		"stats-priority.txt",
		"stats-agent-perf.txt",
		"stats-agent-perf-missing-dates.txt",
		"stats-source.txt",
		"daily-update.txt",
		"daily-update-default.txt",
		"shift-stats-base.txt",
		"shift-create.txt",
		"shift-create-missing-agent.txt",
		"shift-create-invalid-time.txt",
		"shift-list.txt",
		"shift-update.txt",
		"shift-update-missing.txt",
		"shift-stats-active.txt",
		"shift-delete.txt",
		"shift-stats-after-delete.txt",
	} {
		if _, statErr := os.Stat(filepath.Join(evidenceDir, name)); statErr != nil {
			t.Fatalf("expected evidence file %s: %v\noutput=%s", name, statErr, string(output))
		}
	}
	for _, pattern := range []string{
		"agent-register-*.txt",
		"agent-create-*.txt",
		"agent-online-*.txt",
	} {
		matches, _ := filepath.Glob(filepath.Join(evidenceDir, pattern))
		if len(matches) == 0 {
			t.Fatalf("expected evidence matching %s\noutput=%s", pattern, string(output))
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
		"step=stats_aggregation",
		"step=daily_update",
		"step=shift_crud",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, step) {
			t.Fatalf("expected %q in summary, got %s", step, summaryText)
		}
	}

	// 负例语义:缺日期 400、不存在客服 400、非法时间 400、不存在班次 404,
	// 错误消息必须精确留档。
	negativeExpectations := []struct {
		file string
		code string
		msg  string
	}{
		{"stats-agent-perf-missing-dates.txt", "400", "start_date and end_date are required"},
		{"shift-create-missing-agent.txt", "400", "agent not found"},
		{"shift-create-invalid-time.txt", "400", "end_time must be after start_time"},
		{"shift-update-missing.txt", "404", "shift not found"},
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

	// 统计对账的正面证据:分类聚合 technical=2、来源聚合 web=2 精确出现。
	category, err := os.ReadFile(filepath.Join(evidenceDir, "stats-category.txt"))
	if err != nil {
		t.Fatalf("read category evidence: %v", err)
	}
	if !strings.Contains(string(category), `"category":"technical"`) ||
		!strings.Contains(string(category), `"count":2`) {
		t.Fatalf("expected technical count=2 in category evidence, got %s", string(category))
	}

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "statistics"`,
		`"mode": "runtime-statistics-chain"`,
		`"unauthenticated_rejected_401": "true"`,
		`"ticket_category_stats_reconciled": "true"`,
		`"ticket_priority_stats_reconciled": "true"`,
		`"agent_performance_stats_reconciled": "true"`,
		`"agent_performance_missing_dates_rejected_400": "true"`,
		`"customer_source_stats_reconciled": "true"`,
		`"daily_stats_updated": "true"`,
		`"daily_stats_idempotent": "true"`,
		`"shift_created": "true"`,
		`"shift_missing_agent_rejected_400": "true"`,
		`"shift_invalid_time_rejected_400": "true"`,
		`"shift_listed": "true"`,
		`"shift_updated_active": "true"`,
		`"shift_stats_reconciled": "true"`,
		`"shift_deleted_and_gone": "true"`,
		`"shift_missing_update_rejected_404": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
