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

// TestSatisfactionScriptWritesEvidence 走通 test-satisfaction-acceptance.sh
// 全流程:建客户(user+customer)、建工单、关闭工单触发 CSAT 调查调度、
// 满意度评价创建(重复 409/非所有者 403 负例)、列表/详情/按工单查询、
// 评论更新、统计对账、调查列表与重发、删除后数据证据,校验证据文件
// 与 manifest 内容。
func TestSatisfactionScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"EVIDENCE_DIR=%q bash ./test-satisfaction-acceptance.sh",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected satisfaction acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"unauthenticated-list.txt",
		"register-admin.txt",
		"customer-create.txt",
		"customer-create-other.txt",
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
		"step=ticket_and_survey",
		"step=satisfaction_crud",
		"step=stats_and_surveys",
		"step=delete_and_verify",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, step) {
			t.Fatalf("expected %q in summary, got %s", step, summaryText)
		}
	}

	// 负例:重复评价 409、非所有者 403,错误语义必须精确留档。
	duplicate, err := os.ReadFile(filepath.Join(evidenceDir, "satisfaction-duplicate.txt"))
	if err != nil {
		t.Fatalf("read duplicate evidence: %v", err)
	}
	if !strings.Contains(string(duplicate), "HTTP/1.1 409") ||
		!strings.Contains(string(duplicate), "already exists") {
		t.Fatalf("expected 409 already exists, got %s", string(duplicate))
	}
	nonOwner, err := os.ReadFile(filepath.Join(evidenceDir, "satisfaction-non-owner.txt"))
	if err != nil {
		t.Fatalf("read non-owner evidence: %v", err)
	}
	if !strings.Contains(string(nonOwner), "HTTP/1.1 403") ||
		!strings.Contains(string(nonOwner), "not the owner") {
		t.Fatalf("expected 403 not the owner, got %s", string(nonOwner))
	}

	// 删除后的数据证据:按工单查 204、详情 404。
	afterDelete, err := os.ReadFile(filepath.Join(evidenceDir, "satisfaction-by-ticket-after-delete.txt"))
	if err != nil {
		t.Fatalf("read by-ticket-after-delete evidence: %v", err)
	}
	if !strings.Contains(string(afterDelete), "HTTP/1.1 204") {
		t.Fatalf("expected 204 after delete, got %s", string(afterDelete))
	}

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "satisfaction"`,
		`"mode": "runtime-satisfaction-chain"`,
		`"unauthenticated_rejected_401": "true"`,
		`"customer_created": "true"`,
		`"ticket_created": "true"`,
		`"ticket_closed_survey_scheduled": "true"`,
		`"satisfaction_created": "true"`,
		`"duplicate_satisfaction_rejected_409": "true"`,
		`"non_owner_satisfaction_rejected_403": "true"`,
		`"satisfaction_listed": "true"`,
		`"satisfaction_stats_reconciled": "true"`,
		`"surveys_listed": "true"`,
		`"survey_resent": "true"`,
		`"satisfaction_detail_retrievable": "true"`,
		`"satisfaction_comment_updated": "true"`,
		`"satisfaction_by_ticket_retrievable": "true"`,
		`"satisfaction_deleted_and_gone": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
