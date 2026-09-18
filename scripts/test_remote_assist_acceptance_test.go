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

// TestRemoteAssistScriptWritesEvidence 走通 test-remote-assist-acceptance.sh
// 全流程:WS 真实访客会话、远程协助发起(缺会话 404)/列表/详情、标注增删查
// 与升序对账(非法 shape 400/不存在删除 500)、带录制元数据结束(重复 409/
// 不存在 404)、辅助建议未认证 401 与 GET/POST 相似工单对账,校验证据文件
// 与 manifest 内容。
func TestRemoteAssistScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"EVIDENCE_DIR=%q bash ./test-remote-assist-acceptance.sh",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected remote assist acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"build-output.txt",
		"ready.txt",
		"server-log.txt",
		"unauthenticated-assist.txt",
		"register-admin.txt",
		"customer-create.txt",
		"ticket-create.txt",
		"assist-start-missing-conversation.txt",
		"assist-start.txt",
		"assist-list.txt",
		"assist-get.txt",
		"annotation-add-1.txt",
		"annotation-add-2.txt",
		"annotation-list.txt",
		"annotation-shape-invalid.txt",
		"annotation-delete.txt",
		"annotation-list-after-delete.txt",
		"annotation-delete-missing.txt",
		"assist-end.txt",
		"assist-reend-conflict.txt",
		"assist-end-missing.txt",
		"suggest-unauthenticated.txt",
		"suggest-get.txt",
		"suggest-post.txt",
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
		"step=assist_chain",
		"step=annotation_chain",
		"step=assist_end_chain",
		"step=suggest_chain",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, step) {
			t.Fatalf("expected %q in summary, got %s", step, summaryText)
		}
	}

	// 负例语义:未认证 401、缺会话发起 404、非法 shape 400、不存在标注删除
	// 500(裸错误未映射,如实留档)、重复结束 409、不存在结束 404。
	negativeExpectations := []struct {
		file string
		code string
		msg  string
	}{
		{"unauthenticated-assist.txt", "401", ""},
		{"assist-start-missing-conversation.txt", "404", "remote assist session not found"},
		{"annotation-shape-invalid.txt", "400", "annotation shape must be one of"},
		{"annotation-delete-missing.txt", "404", "remote assist annotation not found"},
		{"assist-reend-conflict.txt", "409", "already ended"},
		{"assist-end-missing.txt", "404", ""},
		{"suggest-unauthenticated.txt", "401", ""},
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

	// 正面证据:协助会话 active、标注升序 total、录制元数据回读、suggest 命中。
	positiveExpectations := []struct {
		file string
		msg  string
	}{
		{"assist-start.txt", `"status":"active"`},
		{"annotation-list.txt", `"total":2`},
		{"assist-end.txt", `"recording_key":"ra/acc/rec-1.webm"`},
		{"suggest-get.txt", `"query":"printer"`},
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
		`"provider": "remote-assist"`,
		`"mode": "runtime-assist-chain"`,
		`"unauthenticated_rejected_401": "true"`,
		`"visitor_session_created": "true"`,
		`"assist_start_missing_conversation_rejected_404": "true"`,
		`"assist_started": "true"`,
		`"assist_listed": "true"`,
		`"assist_get_reconciled": "true"`,
		`"annotation_added": "true"`,
		`"annotation_listed_ascending": "true"`,
		`"annotation_shape_invalid_rejected_400": "true"`,
		`"annotation_deleted_and_gone": "true"`,
		`"annotation_missing_delete_rejected_404": "true"`,
		`"assist_ended_with_recording": "true"`,
		`"assist_reend_conflict_rejected_409": "true"`,
		`"assist_end_missing_rejected_404": "true"`,
		`"suggest_unauthenticated_rejected_401": "true"`,
		`"suggest_get_ticket_reconciled": "true"`,
		`"suggest_post_ticket_reconciled": "true"`,
		`"suggest_intent_present": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
