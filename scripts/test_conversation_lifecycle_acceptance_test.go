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

// TestConversationLifecycleAcceptanceScriptWritesEvidence 真跑
// test-conversation-lifecycle-acceptance.sh 自起模式（sqlite +
// ai.provider=local 零出站），校验证据与 manifest——CI 此前只校验已入库
// manifest，从不重放脚本，陈旧证据也能蒙混过关。
func TestConversationLifecycleAcceptanceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"SERVIFY_PORT=%s EVIDENCE_DIR=%q bash ./test-conversation-lifecycle-acceptance.sh",
		freePort(t), evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected conversation lifecycle acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"admin-auth.json",
		"build-output.txt",
		"server-log.txt",
		"agent-register.json",
		"agent-create.json",
		"visitor-first-round.json",
		"ai-first-reply.txt",
		"visitor-handoff-round.json",
		"agent-message.json",
		"session-assign.json",
		"session-messages.json",
		"ticket-create.json",
		"ticket-after-close.json",
		"ticket-close.json",
		"session-close.json",
		"timeline.json",
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
	for _, want := range []string{
		"admin_auth_ok=true",
		"visitor_ingress_ok=true",
		"ai_first_reply_ok=true",
		"ai_first_reply_strategy=llm",
		"visitor_handoff_request=true",
		"assign_ok=true",
		"agent_reply_ok=true",
		"ticket_close_ok=true",
		"session_close_ok=true",
		"timeline_event_types=conversation.created,routing.agent_assigned,ticket.created,ticket.closed",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, want) {
			t.Fatalf("expected %q in summary, got %s", want, summaryText)
		}
	}

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "conversation-lifecycle"`,
		`"overall": "passed"`,
		`"ai_first_reply_ok": "true"`,
		`"handoff_queued_ok": "true"`,
		`"timeline_projected_ok": "true"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
