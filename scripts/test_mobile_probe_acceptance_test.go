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

// TestMobileProbeAcceptanceScriptWritesEvidence 真跑
// test-mobile-probe-acceptance.sh 全链路（服务端 + mock LLM + Android probe
// 二进制，SDK M0 验收②）。需 Android 工具链（脚本内 gradle 构建 probe），
// 故挂在 android-probe job 而非 script-checks；CI 此前只校验已入库
// manifest，从不重放脚本，陈旧证据也能蒙混过关。
func TestMobileProbeAcceptanceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"SERVIFY_PORT=%s MOCK_LLM_PORT=%s EVIDENCE_DIR=%q bash ./test-mobile-probe-acceptance.sh",
		freePort(t), freePort(t), evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected mobile probe acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"build-output.txt",
		"probe-build-output.txt",
		"server-log.txt",
		"mock-llm-log.txt",
		"probe.log",
		"ready.txt",
		"register-admin.txt",
		"register-agent.txt",
		"agent-create.txt",
		"agent-online.txt",
		"agent-send-message.txt",
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
		"probe_build_ok=true",
		"mock_llm_ok=true",
		"ready_ok=true",
		"agent_online=true",
		"visitor_echo_ok=true",
		"ai_final_ok=true",
		"handoff_ok=true",
		"agent_reply_ok=true",
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
		`"provider": "mobile-probe"`,
		`"overall": "passed"`,
		`"probe_build_ok": "true"`,
		`"visitor_echo_ok": "true"`,
		`"ai_final_ok": "true"`,
		`"handoff_ok": "true"`,
		`"agent_reply_ok": "true"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
