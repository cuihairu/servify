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

// TestCustomerAgentScriptWritesEvidence 走通 test-customer-agent-acceptance.sh
// 全流程:客户列表与 search 筛选、客户活动轨迹(工单倒序+空对照)、
// 创建客服(重复 409 负例)、find-available(空池 404/技能偏好)、
// 手动 assign/release 负载对账(满载 500、未分配 404、不存在资源 404 负例),
// 校验证据文件与 manifest 内容。
func TestCustomerAgentScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"EVIDENCE_DIR=%q bash ./test-customer-agent-acceptance.sh",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected customer-agent acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"build-output.txt",
		"ready.txt",
		"unauthenticated-list.txt",
		"register-admin.txt",
		"customer-create-c1.txt",
		"customer-create-c2.txt",
		"customer-list.txt",
		"customer-list-search.txt",
		"ticket-create-t1.txt",
		"ticket-create-t2.txt",
		"customer-activity-c1.txt",
		"customer-activity-c2.txt",
		"agent-create-a1.txt",
		"agent-create-a2.txt",
		"agent-create-duplicate.txt",
		"find-available-empty.txt",
		"find-available-any.txt",
		"find-available-skills.txt",
		"online-baseline.txt",
		"online-after-assign.txt",
		"online-after-release.txt",
		"assign-s1.txt",
		"assign-s3.txt",
		"assign-overcapacity.txt",
		"release-s1.txt",
		"release-unassigned.txt",
		"assign-missing-session.txt",
		"assign-missing-agent.txt",
	} {
		if _, statErr := os.Stat(filepath.Join(evidenceDir, name)); statErr != nil {
			t.Fatalf("expected evidence file %s: %v\noutput=%s", name, statErr, string(output))
		}
	}
	for _, pattern := range []string{
		"agent-register-*.txt",
		"agent-online-*.txt",
		"agent-offline-*.txt",
		"ws-session-*.txt",
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
		"step=customer_list",
		"step=customer_activity",
		"step=agent_setup",
		"step=find_available",
		"step=assign_release",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, step) {
			t.Fatalf("expected %q in summary, got %s", step, summaryText)
		}
	}

	// 负例语义:重复建客服 409、空池 404、满载 500、未分配释放 404、
	// 不存在会话/客服 404,错误消息必须精确留档。
	negativeExpectations := []struct {
		file string
		code string
		msg  string
	}{
		{"agent-create-duplicate.txt", "409", "already an agent"},
		{"find-available-empty.txt", "404", "No available agent found"},
		{"assign-overcapacity.txt", "500", "at maximum capacity"},
		{"release-unassigned.txt", "404", "not found or not assigned"},
		{"assign-missing-session.txt", "404", "session not found"},
		{"assign-missing-agent.txt", "404", "agent not found"},
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

	// search 筛选(跨方言 LOWER LIKE 修复的回归证据):200 且命中唯一客户。
	search, err := os.ReadFile(filepath.Join(evidenceDir, "customer-list-search.txt"))
	if err != nil {
		t.Fatalf("read search evidence: %v", err)
	}
	if !strings.Contains(string(search), "HTTP/1.1 200") || !strings.Contains(string(search), `"total":1`) {
		t.Fatalf("expected 200 total=1 in search evidence, got %s", string(search))
	}

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "customer-agent"`,
		`"mode": "runtime-customer-agent-chain"`,
		`"unauthenticated_rejected_401": "true"`,
		`"customer_created": "true"`,
		`"customer_listed_with_filter": "true"`,
		`"customer_activity_reconciled": "true"`,
		`"agent_created": "true"`,
		`"duplicate_agent_rejected_409": "true"`,
		`"find_available_empty_rejected_404": "true"`,
		`"find_available_selected_with_skills": "true"`,
		`"assigned_session_load_increased": "true"`,
		`"assign_overcapacity_rejected": "true"`,
		`"released_session_load_decreased": "true"`,
		`"release_unassigned_rejected_404": "true"`,
		`"assign_missing_session_rejected_404": "true"`,
		`"assign_missing_agent_rejected_404": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
