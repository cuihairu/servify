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

// TestSecurityAcceptanceScriptWritesEvidence 走通 test-security-acceptance.sh 全流程,
// 校验 staging 负例被拒绝、生产安全配置 strict 通过,以及证据与 manifest 内容。
func TestSecurityAcceptanceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"SECURITY_ACCEPTANCE_MODE=real EVIDENCE_DIR=%q bash ./test-security-acceptance.sh",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected security acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"security-staging-rejected.txt",
		"security-production-passed.txt",
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
		"staging_example_rejected=true",
		"production_secure_ok=true",
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
		`"provider": "security-baseline"`,
		`"mode": "real"`,
		`"staging_example_rejected": "true"`,
		`"production_secure_ok": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}

	staging, err := os.ReadFile(filepath.Join(evidenceDir, "security-staging-rejected.txt"))
	if err != nil {
		t.Fatalf("read staging evidence: %v", err)
	}
	if !strings.Contains(string(staging), "Security baseline check found") {
		t.Fatalf("expected staging strict run to report issues, got %s", string(staging))
	}

	production, err := os.ReadFile(filepath.Join(evidenceDir, "security-production-passed.txt"))
	if err != nil {
		t.Fatalf("read production evidence: %v", err)
	}
	if !strings.Contains(string(production), "Security baseline check passed") {
		t.Fatalf("expected production strict run to pass, got %s", string(production))
	}
}
