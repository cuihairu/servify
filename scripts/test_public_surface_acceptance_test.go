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

// TestPublicSurfaceScriptWritesEvidence 走通 test-public-surface-acceptance.sh
// 全流程:真实起服(定制 security 配置+sqlite),校验安全头/CORS/body 上限/
// 限流/uploads/WS 白名单的证据文件与 manifest 内容。
func TestPublicSurfaceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"EVIDENCE_DIR=%q bash ./test-public-surface-acceptance.sh",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected public surface acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"headers-health.txt",
		"cors-echo.txt",
		"cors-reject.txt",
		"cors-preflight.txt",
		"body-413.txt",
		"rate-limit-8.txt",
		"uploads-file.txt",
		"uploads-dir.txt",
		"ws-reject.txt",
		"ws-admit.txt",
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
		"step=ready",
		"step=security_headers",
		"step=cors",
		"step=body_limit",
		"step=rate_limit",
		"step=uploads",
		"step=ws_origin",
	} {
		if !strings.Contains(summaryText, step) {
			t.Fatalf("expected %q in summary, got %s", step, summaryText)
		}
	}

	headers, err := os.ReadFile(filepath.Join(evidenceDir, "cors-echo.txt"))
	if err != nil {
		t.Fatalf("read cors evidence: %v", err)
	}
	headersText := string(headers)
	if !strings.Contains(headersText, "Access-Control-Allow-Origin: https://admin.example.com") {
		t.Fatalf("expected per-request origin echo in cors evidence, got %s", headersText)
	}
	if !strings.Contains(headersText, "Vary: Origin") {
		t.Fatalf("expected Vary: Origin in cors evidence, got %s", headersText)
	}

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "public-surface"`,
		`"mode": "runtime-security-baseline"`,
		`"security_headers_ok": "true"`,
		`"cors_allowlisted_origin_echoed": "true"`,
		`"cors_foreign_origin_rejected": "true"`,
		`"cors_preflight_vary_origin": "true"`,
		`"oversized_body_rejected_413": "true"`,
		`"rate_limit_enforced_429": "true"`,
		`"uploads_file_served": "true"`,
		`"uploads_directory_404": "true"`,
		`"websocket_foreign_origin_rejected": "true"`,
		`"websocket_allowlisted_origin_admitted": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
