package scripts

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDifyIntegrationScriptRealModeRejectsLocalHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable against httptest servers on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf("DIFY_ACCEPTANCE_MODE=real SERVIFY_URL=%q DIFY_URL=%q DIFY_DATASET_ID=%q EVIDENCE_DIR=%q ./test-dify-integration.sh",
		"http://127.0.0.1:18080",
		"http://127.0.0.1:15001/v1",
		"dataset-1",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected real mode to reject local host, output=%s", string(output))
	}
	if !strings.Contains(string(output), "real 模式拒绝使用本地或私网 Dify 地址") {
		t.Fatalf("expected local-host guard message, output=%s", string(output))
	}

	summary, readErr := os.ReadFile(filepath.Join(evidenceDir, "summary.txt"))
	if readErr != nil {
		t.Fatalf("read summary: %v", readErr)
	}
	if !strings.Contains(string(summary), "real_mode_guard=blocked_private_or_local_host") {
		t.Fatalf("expected real mode guard summary, got %s", string(summary))
	}
}

func TestDifyIntegrationScriptMockModeWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable against httptest servers on Windows")
	}

	servify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
		case "/api/v1/ai/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"type":"orchestrated_enhanced","knowledge_provider_enabled":true,"knowledge_provider":"dify","knowledge_provider_healthy":true}}`))
		case "/api/v1/ai/query":
			_, _ = w.Write([]byte(`{"success":true,"data":{"content":"dify-ok","strategy":"dify"}}`))
		case "/api/v1/ai/knowledge/upload":
			_, _ = w.Write([]byte(`{"success":true}`))
		case "/api/v1/ai/knowledge/sync":
			_, _ = w.Write([]byte(`{"success":true}`))
		case "/api/v1/ai/metrics":
			_, _ = w.Write([]byte(`{"success":true,"data":{"dify_usage_count":2}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer servify.Close()

	difyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/datasets/dataset-1" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"id":"dataset-1","name":"Mock Dify Dataset"}`))
	}))
	defer difyServer.Close()

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf("DIFY_ACCEPTANCE_MODE=mock SERVIFY_URL=%q DIFY_URL=%q DIFY_DATASET_ID=%q EVIDENCE_DIR=%q ./test-dify-integration.sh",
		servify.URL,
		difyServer.URL+"/v1",
		"dataset-1",
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
		"dify-dataset.json",
		"ai-status.json",
		"ai-query.json",
		"knowledge-upload.json",
		"knowledge-sync.json",
		"ai-metrics.json",
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
		"knowledge_provider=dify",
		"knowledge_provider_enabled=true",
		"knowledge_provider_healthy=true",
		"query_ok=true",
		"query_strategy=dify",
		"dify_available=true",
		"dify_usage_count=2",
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
		`"provider": "dify"`,
		`"mode": "mock"`,
		`"knowledge_provider": "dify"`,
		`"knowledge_upload_ok": "true"`,
		`"knowledge_sync_ok": "true"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}

func TestDifyIntegrationScriptMockModePersistsFailureEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable against httptest servers on Windows")
	}

	servify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
		case "/api/v1/ai/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"type":"orchestrated_enhanced","knowledge_provider_enabled":true,"knowledge_provider":"dify","knowledge_provider_healthy":true}}`))
		case "/api/v1/ai/query":
			_, _ = w.Write([]byte(`{"success":true,"data":{"content":"dify-ok","strategy":"dify"}}`))
		case "/api/v1/ai/knowledge/upload":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"success":false,"error":"upload failed"}`))
		case "/api/v1/ai/knowledge/sync":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"success":false,"error":"sync failed"}`))
		case "/api/v1/ai/metrics":
			_, _ = w.Write([]byte(`{"success":true,"data":{"dify_usage_count":2}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer servify.Close()

	difyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/datasets/dataset-1" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"id":"dataset-1","name":"Mock Dify Dataset"}`))
	}))
	defer difyServer.Close()

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf("DIFY_ACCEPTANCE_MODE=mock SERVIFY_URL=%q DIFY_URL=%q DIFY_DATASET_ID=%q EVIDENCE_DIR=%q ./test-dify-integration.sh",
		servify.URL,
		difyServer.URL+"/v1",
		"dataset-1",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected mock mode to preserve failure evidence, err=%v output=%s", err, string(output))
	}

	uploadBody, readErr := os.ReadFile(filepath.Join(evidenceDir, "knowledge-upload.json"))
	if readErr != nil {
		t.Fatalf("read upload evidence: %v\noutput=%s", readErr, string(output))
	}
	if !strings.Contains(string(uploadBody), `"success":false`) {
		t.Fatalf("expected failed upload evidence, got %s", string(uploadBody))
	}

	syncBody, readErr := os.ReadFile(filepath.Join(evidenceDir, "knowledge-sync.json"))
	if readErr != nil {
		t.Fatalf("read sync evidence: %v\noutput=%s", readErr, string(output))
	}
	if !strings.Contains(string(syncBody), `"success":false`) {
		t.Fatalf("expected failed sync evidence, got %s", string(syncBody))
	}

	summary, err := os.ReadFile(filepath.Join(evidenceDir, "summary.txt"))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	summaryText := string(summary)
	for _, want := range []string{
		"query_ok=true",
		"knowledge_upload_ok=false",
		"knowledge_sync_ok=false",
	} {
		if !strings.Contains(summaryText, want) {
			t.Fatalf("expected %q in summary, got %s", want, summaryText)
		}
	}
}

func TestDifyIntegrationScriptRealModeRejectsFallbackQuery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable against httptest servers on Windows")
	}

	servify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
		case "/api/v1/ai/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"type":"orchestrated_enhanced","knowledge_provider_enabled":true,"knowledge_provider":"dify","knowledge_provider_healthy":true}}`))
		case "/api/v1/ai/query":
			_, _ = w.Write([]byte(`{"success":true,"data":{"content":"fallback-ok","strategy":"fallback"}}`))
		case "/api/v1/ai/knowledge/upload":
			_, _ = w.Write([]byte(`{"success":true}`))
		case "/api/v1/ai/knowledge/sync":
			_, _ = w.Write([]byte(`{"success":true}`))
		case "/api/v1/ai/metrics":
			_, _ = w.Write([]byte(`{"success":true,"data":{"dify_usage_count":0}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer servify.Close()

	difyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/datasets/dataset-1" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"id":"dataset-1","name":"Real Dify Dataset"}`))
	}))
	defer difyServer.Close()

	evidenceDir := t.TempDir()
	servifyHost := strings.TrimPrefix(servify.URL, "http://")
	difyHost := strings.TrimPrefix(difyServer.URL, "http://")

	cmd := exec.Command("bash", "-lc", fmt.Sprintf("curl() { command curl --resolve real-servify.example.test:%s:127.0.0.1 --resolve real-dify.example.test:%s:127.0.0.1 \"$@\"; }; export -f curl; DIFY_ACCEPTANCE_MODE=real SERVIFY_URL=%q DIFY_URL=%q DIFY_DATASET_ID=%q EVIDENCE_DIR=%q ./test-dify-integration.sh",
		mustPort(t, servifyHost),
		mustPort(t, difyHost),
		"http://real-servify.example.test:"+mustPort(t, servifyHost),
		"http://real-dify.example.test:"+mustPort(t, difyHost)+"/v1",
		"dataset-1",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected real mode to reject fallback query, output=%s", string(output))
	}
	if !strings.Contains(string(output), "不能只落入 fallback") {
		t.Fatalf("expected fallback guard message, output=%s", string(output))
	}
}

func mustPort(t *testing.T, hostport string) string {
	t.Helper()
	_, port, err := net.SplitHostPort(hostport)
	if err != nil {
		t.Fatalf("split host port %q: %v", hostport, err)
	}
	return port
}
