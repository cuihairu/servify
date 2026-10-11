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

// TestTranslationAcceptanceScriptWritesEvidence 真跑 test-translation-acceptance.sh
// 自起模式（sqlite + ai.provider=local 零出站），校验证据与 manifest——
// CI 此前只校验已入库 manifest，从不重放脚本，陈旧证据也能蒙混过关。
func TestTranslationAcceptanceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"SERVIFY_PORT=%s EVIDENCE_DIR=%q bash ./test-translation-acceptance.sh",
		freePort(t), evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected translation acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"admin-auth.json",
		"build-output.txt",
		"server-log.txt",
		"api-key-create.json",
		"api-key-list.json",
		"guest-session.json",
		"pref-agent-put.json",
		"pref-agent-get.json",
		"pref-visitor-put.json",
		"pref-visitor-get.json",
		"pref-visitor-mismatch.json",
		"pref-delete.json",
		"pref-visitor-get-after-delete.json",
		"translate-direct.json",
		"translate-direct-repeat.json",
		"translate-invalid-lang.json",
		"unauthenticated-translate.json",
		"ws-frames.jsonl",
		"agent-send.json",
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
		"ready_ok=true",
		"unauthenticated_translate_rejected_401=true",
		"admin_auth_ok=true",
		"api_key_created_ok=true",
		"api_key_list_hides_plaintext_ok=true",
		"guest_token_issued_ok=true",
		"agent_pref_set_ok=true",
		"visitor_pref_set_ok=true",
		"translate_endpoint_ok=true",
		"translate_deterministic_ok=true",
		"translate_invalid_lang_rejected_400=true",
		"ws_visitor_to_agent_ok=true",
		"ws_agent_to_visitor_ok=true",
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
		`"provider": "translation"`,
		`"overall": "passed"`,
		`"translate_deterministic_ok": "true"`,
		`"api_key_list_hides_plaintext_ok": "true"`,
		`"ws_agent_to_visitor_translation_ok": "true"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
