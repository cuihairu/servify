package scripts

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// TestRuntimeBaselineAcceptanceScriptWritesEvidence 用 mock server 走通
// test-runtime-baseline-acceptance.sh 的 ready/metrics/platforms 流程,并校验证据。
func TestRuntimeBaselineAcceptanceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	var (
		mu        sync.Mutex
		registers int
		token     = "runtime-baseline-admin-token"
	)

	servify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/health":
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
		case r.URL.Path == "/ready":
			_, _ = w.Write([]byte(`{"ready":true,"services":{"ai":"ready","database":"ready"}}`))
		case r.URL.Path == "/metrics":
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			_, _ = w.Write([]byte("# HELP servify_up servify up\n# TYPE servify_up gauge\nservify_up 1\n"))
		case r.URL.Path == "/api/v1/auth/register":
			mu.Lock()
			defer mu.Unlock()
			registers++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"` + token + `","refresh_token":"reg-refresh","user":{"id":1,"role":"admin"}}`))
		case r.URL.Path == "/api/v1/messages/platforms":
			if r.Header.Get("Authorization") != "Bearer "+token {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid token"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"total_platforms":2,"active_platforms":["telegram","wechat"]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer servify.Close()

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"RUNTIME_ACCEPTANCE_MODE=mock SERVIFY_URL=%q EVIDENCE_DIR=%q bash ./test-runtime-baseline-acceptance.sh",
		servify.URL,
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected runtime baseline acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"ready.json",
		"metrics.txt",
		"admin-auth.json",
		"platforms.json",
		"platforms-unauthorized.json",
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
		"mode=mock",
		"ready_ok=true",
		"metrics_ok=true",
		"platforms_ok=true",
		"unauthenticated_rejected=true",
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
		`"provider": "runtime-baseline"`,
		`"mode": "mock"`,
		`"ready_ok": "true"`,
		`"metrics_ok": "true"`,
		`"platforms_ok": "true"`,
		`"unauthenticated_rejected": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}

	if registers != 1 {
		t.Fatalf("unexpected register count: %d", registers)
	}
}
