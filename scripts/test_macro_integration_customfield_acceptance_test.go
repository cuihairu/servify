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

// TestMacroIntegrationCustomFieldScriptWritesEvidence 走通
// test-macro-integration-customfield-acceptance.sh 全流程:宏 CRUD 与
// apply 对账(缺工单 404/停用 400 负例)、集成 slug 规范化/重复 409/
// search 对账、自定义字段创建与三类负例与删除后 404,校验证据文件与
// manifest 内容。
func TestMacroIntegrationCustomFieldScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"EVIDENCE_DIR=%q bash ./test-macro-integration-customfield-acceptance.sh",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected macro/integration/customfield acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"build-output.txt",
		"ready.txt",
		"unauthenticated-macros.txt",
		"register-admin.txt",
		"customer-create.txt",
		"ticket-create.txt",
		"macro-baseline.txt",
		"macro-create.txt",
		"macro-list.txt",
		"macro-update.txt",
		"macro-apply.txt",
		"macro-apply-missing-ticket.txt",
		"macro-deactivate.txt",
		"macro-apply-inactive.txt",
		"macro-reactivate.txt",
		"macro-delete.txt",
		"macro-list-after-delete.txt",
		"integration-create.txt",
		"integration-list.txt",
		"integration-duplicate.txt",
		"integration-update.txt",
		"integration-search.txt",
		"integration-search-nomatch.txt",
		"integration-delete.txt",
		"integration-list-after-delete.txt",
		"customfield-create.txt",
		"customfield-get.txt",
		"customfield-list.txt",
		"customfield-negative-key.txt",
		"customfield-negative-type.txt",
		"customfield-negative-resource.txt",
		"customfield-update.txt",
		"customfield-delete.txt",
		"customfield-get-after-delete.txt",
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
		"step=macro_chain",
		"step=integration_chain",
		"step=custom_field_chain",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, step) {
			t.Fatalf("expected %q in summary, got %s", step, summaryText)
		}
	}

	// 负例语义:缺工单 404、停用宏 400、重复 slug 409、非法 key/type/resource
	// 400、删除后详情 404,错误消息必须精确留档。
	negativeExpectations := []struct {
		file string
		code string
		msg  string
	}{
		{"macro-apply-missing-ticket.txt", "404", "ticket not found"},
		{"macro-apply-inactive.txt", "400", "macro inactive"},
		{"integration-duplicate.txt", "409", "integration slug already exists"},
		{"customfield-negative-key.txt", "400", "invalid key"},
		{"customfield-negative-type.txt", "400", "invalid type"},
		{"customfield-negative-resource.txt", "400", "unsupported resource"},
		{"customfield-get-after-delete.txt", "404", "Custom field not found"},
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

	// 正面证据:宏 apply 生成 system 评论、slug 规范化、search 命中、字段键回读。
	positiveExpectations := []struct {
		file string
		msg  string
	}{
		{"macro-apply.txt", `"type":"system"`},
		{"integration-create.txt", `"slug":"zapier-bridge"`},
		{"integration-search.txt", `"total":1`},
		{"customfield-create.txt", `"key":"escalation_reason"`},
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

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "macro-integration-customfield"`,
		`"mode": "runtime-ops-tooling-chain"`,
		`"unauthenticated_rejected_401": "true"`,
		`"macro_baseline_empty": "true"`,
		`"macro_created": "true"`,
		`"macro_listed": "true"`,
		`"macro_updated": "true"`,
		`"macro_applied_to_ticket": "true"`,
		`"macro_apply_missing_ticket_rejected_404": "true"`,
		`"macro_inactive_apply_rejected_400": "true"`,
		`"macro_deleted_and_gone": "true"`,
		`"integration_created": "true"`,
		`"integration_listed": "true"`,
		`"integration_duplicate_slug_rejected_409": "true"`,
		`"integration_updated_enabled": "true"`,
		`"integration_search_reconciled": "true"`,
		`"integration_deleted_and_gone": "true"`,
		`"custom_field_created": "true"`,
		`"custom_field_listed": "true"`,
		`"custom_field_invalid_key_rejected_400": "true"`,
		`"custom_field_invalid_type_rejected_400": "true"`,
		`"custom_field_unsupported_resource_rejected_400": "true"`,
		`"custom_field_updated": "true"`,
		`"custom_field_deleted_gone_404": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
