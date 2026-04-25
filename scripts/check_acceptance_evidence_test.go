package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckAcceptanceEvidenceScriptSkipsWhenNoManifestExists(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	root := t.TempDir()
	mustInitGitRepo(t, root)
	mustWriteExecutable(t, filepath.Join(root, "scripts", "validate-acceptance-manifest.sh"), "#!/usr/bin/env bash\nexit 99\n")
	mustMkdir(t, filepath.Join(root, "scripts", "test-results"))
	copyExecutable(t, "check-acceptance-evidence.sh", filepath.Join(root, "scripts", "check-acceptance-evidence.sh"))

	cmd := exec.Command("bash", "./scripts/check-acceptance-evidence.sh")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected skip success, err=%v output=%s", err, string(output))
	}
	if !strings.Contains(string(output), "No tracked acceptance manifests") {
		t.Fatalf("expected skip output, got %s", string(output))
	}
}

func TestCheckAcceptanceEvidenceScriptValidatesAllFoundManifests(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed script tests are not stable on Windows")
	}

	root := t.TempDir()
	mustInitGitRepo(t, root)
	mustWriteExecutable(t, filepath.Join(root, "scripts", "validate-acceptance-manifest.sh"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$1" >> "$TMP_VALIDATION_LOG"
`)
	copyExecutable(t, "check-acceptance-evidence.sh", filepath.Join(root, "scripts", "check-acceptance-evidence.sh"))

	first := filepath.Join(root, "scripts", "test-results", "dify-acceptance", "mock", "manifest.json")
	second := filepath.Join(root, "scripts", "test-results", "weknora-acceptance", "real", "manifest.json")
	mustWriteFile(t, first, "{}")
	mustWriteFile(t, second, "{}")
	mustGitAdd(t, root, "scripts/test-results/dify-acceptance/mock/manifest.json", "scripts/test-results/weknora-acceptance/real/manifest.json")

	logFile := filepath.Join(root, "validation.log")
	cmd := exec.Command("bash", "./scripts/check-acceptance-evidence.sh")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "TMP_VALIDATION_LOG="+logFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected validation success, err=%v output=%s", err, string(output))
	}
	if !strings.Contains(string(output), "Acceptance evidence checks passed.") {
		t.Fatalf("expected success output, got %s", string(output))
	}

	logBody, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read validation log: %v", err)
	}
	logText := string(logBody)
	for _, want := range []string{
		"scripts/test-results/dify-acceptance/mock/manifest.json",
		"scripts/test-results/weknora-acceptance/real/manifest.json",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("expected %q in validation log, got %s", want, logText)
		}
	}
}

func mustWriteExecutable(t *testing.T, path, body string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func copyExecutable(t *testing.T, sourceName, dest string) {
	t.Helper()
	body, err := os.ReadFile(sourceName)
	if err != nil {
		t.Fatalf("read source script %s: %v", sourceName, err)
	}
	mustWriteExecutable(t, dest, string(body))
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustInitGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v output=%s", err, string(output))
	}
}

func mustGitAdd(t *testing.T, dir string, paths ...string) {
	t.Helper()
	args := append([]string{"add"}, paths...)
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add %v: %v output=%s", paths, err, string(output))
	}
}
