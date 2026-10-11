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

// TestWebhookAcceptanceScriptWritesEvidence 真跑 test-webhook-acceptance.sh
// 自起模式（sqlite + 本地接收端），校验证据与 manifest 内容——CI 此前只
// 校验已入库 manifest，从不重放脚本，陈旧证据也能蒙混过关。
func TestWebhookAcceptanceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"SERVIFY_PORT=%s RECEIVER_PORT=%s EVIDENCE_DIR=%q bash ./test-webhook-acceptance.sh",
		freePort(t), freePort(t), evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected webhook acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"admin-auth.json",
		"build-output.txt",
		"server-log.txt",
		"webhook-endpoint-create.json",
		"webhook-endpoint-list.json",
		"webhook-events.json",
		"webhook-deliveries.json",
		"webhook-delivery-headers.json",
		"webhook-endpoint-test.json",
		"visitor-ticket.json",
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
		"build_ok=true",
		"admin_auth_ok=true",
		"endpoint_created_ok=true",
		"secret_hidden_ok=true",
		"events_whitelist_ok=true",
		"session_msg_ok=true",
		"ticket_created_ok=true",
		"delivery_signature_ok=true",
		"delivery_status_ok=true",
		"selftest_ok=true",
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
		`"provider": "webhook"`,
		`"overall": "passed"`,
		`"secret_hidden_ok": "true"`,
		`"delivery_signature_ok": "true"`,
		`"selftest_ok": "true"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
