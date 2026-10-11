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

// TestRoutingScoringAcceptanceScriptWritesEvidence 真跑
// test-routing-scoring-acceptance.sh 自起模式（sqlite 真实服务 +
// TransferToHuman 直接分配），校验评分审计证据与 manifest——CI 此前只校验
// 已入库 manifest，从不重放脚本，陈旧证据也能蒙混过关。
func TestRoutingScoringAcceptanceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"SERVIFY_PORT=%s EVIDENCE_DIR=%q bash ./test-routing-scoring-acceptance.sh",
		freePort(t), evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected routing scoring acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"build-output.txt",
		"server-log.txt",
		"visitor-ingress.txt",
		"transfer-result.json",
		"transfer-history.json",
		"scoring.json",
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
		"visitor_ingress_ok=true",
		"scoring_audit_ok=true",
		"transfer_record_ok=true",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, want) {
			t.Fatalf("expected %q in summary, got %s", want, summaryText)
		}
	}
	if !strings.Contains(summaryText, "assigned_agent=") {
		t.Fatalf("expected assigned_agent= in summary, got %s", summaryText)
	}

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "routing-scoring"`,
		`"overall": "passed"`,
		`"transfer_assigned_ok": "true"`,
		`"scoring_audit_ok": "true"`,
		`"transfer_record_ok": "true"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
