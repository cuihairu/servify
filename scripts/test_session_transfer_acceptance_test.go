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

// TestSessionTransferScriptWritesEvidence 走通 test-session-transfer-acceptance.sh
// 全流程:真实访客 WS 建会话后的转人工链路(直转、无可用客服自动入队、
// 重复转接幂等、手动派发、指定客服转接、离线目标拒绝、取消等待幂等、
// 自动转接检查、近期历史与负例),校验证据文件与 manifest 内容。
func TestSessionTransferScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"EVIDENCE_DIR=%q bash ./test-session-transfer-acceptance.sh",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected session transfer acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"unauthenticated-waiting.txt",
		"register-admin.txt",
		"agent-register-st-agent-a-*.txt",
		"agent-create-*.txt",
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
		"to-human-s4.txt",
		"cancel-waiting.txt",
		"cancel-waiting-again.txt",
		"waiting-list-cancelled.txt",
		"check-auto.txt",
		"history-recent.txt",
		"to-human-unknown-session.txt",
	} {
		matches, globErr := filepath.Glob(filepath.Join(evidenceDir, name))
		if globErr != nil || len(matches) == 0 {
			t.Fatalf("expected evidence file %s: glob=%v\noutput=%s", name, globErr, string(output))
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
		"step=transfer_to_human",
		"step=waiting_queue",
		"step=transfer_to_agent",
		"step=cancel_waiting",
		"step=check_and_history",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, step) {
			t.Fatalf("expected %q in summary, got %s", step, summaryText)
		}
	}

	// 直转与排队的响应语义必须区分:直转 success=true 且拿到客服,排队 is_waiting=true。
	direct, err := os.ReadFile(filepath.Join(evidenceDir, "to-human-direct.txt"))
	if err != nil {
		t.Fatalf("read direct transfer evidence: %v", err)
	}
	directText := string(direct)
	if !strings.Contains(directText, "HTTP/1.1 200") ||
		!strings.Contains(directText, `"success":true`) ||
		strings.Contains(directText, `"is_waiting":true`) {
		t.Fatalf("expected direct transfer assigned to an agent, got %s", directText)
	}
	queued, err := os.ReadFile(filepath.Join(evidenceDir, "to-human-queued.txt"))
	if err != nil {
		t.Fatalf("read queued transfer evidence: %v", err)
	}
	if !strings.Contains(string(queued), `"is_waiting":true`) {
		t.Fatalf("expected waiting queue enrollment, got %s", string(queued))
	}

	// 负例:不存在会话与离线目标都必须 500 拒绝留档。
	for _, tc := range []struct {
		name    string
		message string
	}{
		{"to-human-unknown-session.txt", "session not found"},
		{"to-agent-offline-rejected.txt", "not online"},
	} {
		raw, err := os.ReadFile(filepath.Join(evidenceDir, tc.name))
		if err != nil {
			t.Fatalf("read rejection evidence %s: %v", tc.name, err)
		}
		if !strings.Contains(string(raw), "HTTP/1.1 500") || !strings.Contains(string(raw), tc.message) {
			t.Fatalf("expected 500 %q in %s, got %s", tc.message, tc.name, string(raw))
		}
	}

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "session-transfer"`,
		`"mode": "runtime-transfer-chain"`,
		`"unauthenticated_rejected_401": "true"`,
		`"agents_ready": "true"`,
		`"agent_online_toggled": "true"`,
		`"visitor_session_created": "true"`,
		`"transfer_to_human_direct_ok": "true"`,
		`"transfer_history_retrievable": "true"`,
		`"transfer_to_waiting_queued": "true"`,
		`"waiting_queue_listed": "true"`,
		`"duplicate_transfer_idempotent": "true"`,
		`"process_queue_dispatched": "true"`,
		`"transfer_to_agent_ok": "true"`,
		`"cancel_waiting_ok": "true"`,
		`"check_auto_ok": "true"`,
		`"recent_history_listed": "true"`,
		`"unknown_session_rejected": "true"`,
		`"offline_target_rejected": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
