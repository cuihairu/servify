package scripts

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWeKnoraIntegrationScriptRealModeRejectsLocalHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable against httptest servers on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf("WEKNORA_ACCEPTANCE_MODE=real SERVIFY_URL=%q WEKNORA_URL=%q EVIDENCE_DIR=%q ./test-weknora-integration.sh",
		"http://127.0.0.1:18080",
		"http://127.0.0.1:19000",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected real mode to reject local host, output=%s", string(output))
	}

	if !strings.Contains(string(output), "real 模式拒绝使用本地或私网 WeKnora compatibility 地址") {
		t.Fatalf("expected local-host guard message, output=%s", string(output))
	}

	summaryPath := filepath.Join(evidenceDir, "summary.txt")
	summary, readErr := os.ReadFile(summaryPath)
	if readErr != nil {
		t.Fatalf("read summary: %v", readErr)
	}
	if !strings.Contains(string(summary), "real_mode_guard=blocked_private_or_local_host") {
		t.Fatalf("expected real mode guard summary, got %s", string(summary))
	}
}

func TestWeKnoraIntegrationScriptMockModeWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable against httptest servers on Windows")
	}

	knowledgeProviderEnabled := true
	fallbackUsageCount := 0

	servify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
		case "/api/v1/ai/query":
			if knowledgeProviderEnabled {
				_, _ = w.Write([]byte(`{"success":true,"data":{"content":"ok","strategy":"weknora"}}`))
			} else {
				fallbackUsageCount++
				_, _ = w.Write([]byte(`{"success":true,"data":{"content":"fallback-ok","strategy":"fallback"}}`))
			}
		case "/api/v1/ai/status":
			_, _ = w.Write([]byte(fmt.Sprintf(`{"success":true,"data":{"type":"orchestrated_enhanced","knowledge_provider_enabled":%t,"knowledge_provider":"weknora","knowledge_provider_healthy":%t}}`, knowledgeProviderEnabled, knowledgeProviderEnabled)))
		case "/api/v1/ai/metrics":
			_, _ = w.Write([]byte(fmt.Sprintf(`{"success":true,"data":{"query_count":3,"weknora_usage_count":2,"fallback_usage_count":%d}}`, fallbackUsageCount)))
		case "/api/v1/ai/knowledge-provider/disable":
			knowledgeProviderEnabled = false
			_, _ = w.Write([]byte(`{"success":true,"message":"knowledge provider disabled"}`))
		case "/api/v1/ai/knowledge-provider/enable":
			knowledgeProviderEnabled = true
			_, _ = w.Write([]byte(`{"success":true,"message":"knowledge provider enabled"}`))
		case "/api/v1/ai/circuit-breaker/reset":
			_, _ = w.Write([]byte(`{"success":true,"message":"Circuit breaker reset"}`))
		case "/api/v1/ai/knowledge/upload":
			_, _ = w.Write([]byte(`{"success":true,"data":{"document_id":"doc-1"}}`))
		case "/api/v1/ai/knowledge/sync":
			_, _ = w.Write([]byte(`{"success":true}`))
		case "/api/v1/ws/stats":
			_, _ = w.Write([]byte(`{"success":true,"data":{"client_count":0}}`))
		case "/api/v1/webrtc/connections":
			_, _ = w.Write([]byte(`{"success":true,"data":{"connection_count":0}}`))
		case "/api/customers/stats":
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"missing token"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"total":1}}`))
		case "/api/statistics/dashboard":
			claims := jwtClaimsFromAuthHeader(r.Header.Get("Authorization"))
			if claims["principal_kind"] == "agent" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"forbidden"}`))
				return
			}
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"missing token"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"tickets":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer servify.Close()

	weknora := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/health" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"service":"weknora-mock","status":"ok"}`))
	}))
	defer weknora.Close()

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf("WEKNORA_ACCEPTANCE_MODE=mock SERVIFY_URL=%q WEKNORA_URL=%q EVIDENCE_DIR=%q ./test-weknora-integration.sh",
		servify.URL,
		weknora.URL,
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected mock mode success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"servify-health.json",
		"weknora-health.json",
		"ai-status.json",
		"ai-query.json",
		"knowledge-provider-disable.json",
		"ai-status-after-disable.json",
		"ai-query-after-disable.json",
		"ai-metrics-after-fallback.json",
		"knowledge-provider-enable.json",
		"ai-status-after-enable.json",
		"circuit-breaker-reset.json",
		"knowledge-upload.json",
		"knowledge-sync.json",
		"ws-stats.json",
		"webrtc-connections.json",
	} {
		path := filepath.Join(evidenceDir, name)
		if _, statErr := os.Stat(path); statErr != nil {
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
		"overall_status=healthy",
		"service_type=orchestrated_enhanced",
		"service_provider_capable=true",
		"knowledge_provider=weknora",
		"knowledge_provider_enabled=true",
		"knowledge_provider_healthy=true",
		"weknora_available=true",
		"knowledge_provider_disable_ok=true",
		"knowledge_provider_enable_ok=true",
		"status_after_disable_enabled=false",
		"status_after_enable_enabled=true",
		"circuit_breaker_reset_ok=true",
		"fallback_query_ok=true",
		"fallback_query_strategy=fallback",
		"fallback_usage_count_after_disable=1",
		"knowledge_upload_ok=true",
		"knowledge_sync_ok=true",
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
		`"provider": "weknora"`,
		`"mode": "mock"`,
		`"knowledge_provider": "weknora"`,
		`"fallback_query_strategy": "fallback"`,
		`"knowledge_upload_ok": "true"`,
		`"knowledge_sync_ok": "true"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}

func jwtClaimsFromAuthHeader(header string) map[string]any {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, "Bearer ") {
		return map[string]any{}
	}
	token := strings.TrimPrefix(header, "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return map[string]any{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return map[string]any{}
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return map[string]any{}
	}
	return claims
}
