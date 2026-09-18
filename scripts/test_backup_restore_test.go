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

// TestBackupRestoreScriptWritesEvidence 走通 test-backup-restore.sh 全流程:
// 驱动 sqlite 恢复演练,校验证据文件、summary 步骤与 manifest 内容。
func TestBackupRestoreScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"EVIDENCE_DIR=%q bash ./test-backup-restore.sh",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected backup restore acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"db-manifest.json",
		"files-manifest.json",
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
		"step=seeded ",
		"step=db-backup ",
		"step=damage ",
		"step=db-restore ",
		"step=files-backup ",
		"step=files-damage ",
		"step=files-restore ",
	} {
		if !strings.Contains(summaryText, step) {
			t.Fatalf("expected %q in summary, got %s", step, summaryText)
		}
	}

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "backup-restore"`,
		`"mode": "drill-sqlite"`,
		`"db_restore_matches_backup": "true"`,
		`"db_survives_post_backup_damage": "true"`,
		`"db_sequence_no_collision": "true"`,
		`"files_restore_matches_backup": "true"`,
		`"files_verify_detects_tamper": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}

	dbManifest, err := os.ReadFile(filepath.Join(evidenceDir, "db-manifest.json"))
	if err != nil {
		t.Fatalf("read db manifest: %v", err)
	}
	if !strings.Contains(string(dbManifest), `"dialect": "sqlite"`) {
		t.Fatalf("expected sqlite dialect in db manifest, got %s", string(dbManifest))
	}
}
