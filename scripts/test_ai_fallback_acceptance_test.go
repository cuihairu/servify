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

// TestAIFallbackAcceptanceScriptWritesEvidence 用 mock server 走通
// test-ai-fallback-acceptance.sh 的 status/query/metrics/日志 流程,并校验证据。
func TestAIFallbackAcceptanceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	var (
		mu    sync.Mutex
		token = "ai-fallback-admin-token"
	)

	authorized := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid token"}`))
			return false
		}
		return true
	}

	servify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/health":
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
		case r.URL.Path == "/api/v1/auth/register":
			mu.Lock()
			defer mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"` + token + `","refresh_token":"ai-fallback-refresh","user":{"id":1,"role":"admin"}}`))
		case r.URL.Path == "/api/v1/ai/status":
			if !authorized(w, r) {
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"type":"orchestrated_enhanced","knowledge_provider":"","knowledge_provider_enabled":false,"fallback_enabled":true}}`))
		case r.URL.Path == "/api/v1/ai/query":
			if !authorized(w, r) {
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"content":"您好！我是智能客服助手。","confidence":0.8,"source":"ai","strategy":"fallback","duration":"1ms"},"duration":"1ms"}`))
		case r.URL.Path == "/api/v1/ai/metrics":
			if !authorized(w, r) {
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"query_count":0,"success_count":0,"fallback_usage_count":0,"dify_usage_count":0,"weknora_usage_count":0}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer servify.Close()

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"AI_ACCEPTANCE_MODE=mock SERVIFY_URL=%q EVIDENCE_DIR=%q bash ./test-ai-fallback-acceptance.sh",
		servify.URL,
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected ai fallback acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"admin-auth.json",
		"ai-status.json",
		"ai-query.json",
		"ai-metrics.json",
		"ai-query-unauthorized.json",
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
		"status_ok=true",
		"query_fallback_ok=true",
		"ai_query_strategy=fallback",
		"metrics_ok=true",
		"log_evidence=skipped(mock)",
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
		`"provider": "ai-fallback"`,
		`"mode": "mock"`,
		`"status_ok": "true"`,
		`"query_fallback_ok": "true"`,
		`"metrics_ok": "true"`,
		`"log_evidence_ok": "true"`,
		`"unauthenticated_rejected": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
