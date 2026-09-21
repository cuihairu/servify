package scripts

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// freeTCPPort 探测一个空闲 TCP 端口（关闭监听后由子进程复用）。
func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("close probe listener: %v", err)
	}
	return port
}

// TestRagflowScriptWritesEvidence 走通 test-ragflow-acceptance.sh 的 mock 模式
// 全自含链路（构建真实 servify + 内嵌 python3 RAGFlow mock + sqlite），并校验
// 证据与 manifest。不依赖 pg/redis/docker。
func TestRagflowScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	evidenceDir := t.TempDir()
	servifyPort := freeTCPPort(t)
	mockPort := freeTCPPort(t)

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"RAGFLOW_ACCEPTANCE_MODE=mock SERVIFY_PORT=%d RAGFLOW_MOCK_PORT=%d EVIDENCE_DIR=%q ./test-ragflow-acceptance.sh",
		servifyPort, mockPort, evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=go1.25.7")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected mock mode success, err=%v\noutput=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"ragflow-dataset.json",
		"admin-auth.json",
		"ai-status.json",
		"ai-query.json",
		"knowledge-upload.json",
		"knowledge-upload-repeat.json",
		"knowledge-sync.json",
		"ragflow-mock-requests.jsonl",
		"server-log.txt",
	} {
		if _, statErr := os.Stat(evidenceDir + "/" + name); statErr != nil {
			t.Fatalf("expected evidence file %s: %v\noutput=%s", name, statErr, string(output))
		}
	}

	summary, err := os.ReadFile(evidenceDir + "/summary.txt")
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	summaryText := string(summary)
	for _, want := range []string{
		"mode=mock",
		"build_ok=true",
		"unauthenticated_rejected=true",
		"knowledge_provider=ragflow",
		"status_ok=true",
		"query_ok=true",
		"retrieval_hit=true",
		"knowledge_upload_ok=true",
		"upload_dedup_ok=true",
		"knowledge_sync_ok=true",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, want) {
			t.Fatalf("expected %q in summary, got %s", want, summaryText)
		}
	}

	manifest, err := os.ReadFile(evidenceDir + "/manifest.json")
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "ragflow"`,
		`"mode": "mock"`,
		`"retrieval_hit": "true"`,
		`"knowledge_upload_dedup_ok": "true"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}

// TestRagflowScriptRealModeRejectsLocalHost 对齐 dify 惯例：real 模式拒绝私网
// RAGFlow 地址并留下 guard 证据。
func TestRagflowScriptRealModeRejectsLocalHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"RAGFLOW_ACCEPTANCE_MODE=real SERVIFY_URL=%q RAGFLOW_URL=%q EVIDENCE_DIR=%q ./test-ragflow-acceptance.sh",
		"http://127.0.0.1:18080",
		"http://127.0.0.1:19380",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected real mode to reject local host, output=%s", string(output))
	}
	if !strings.Contains(string(output), "real 模式拒绝使用本地或私网 RAGFlow 地址") {
		t.Fatalf("expected local-host guard message, output=%s", string(output))
	}

	summary, readErr := os.ReadFile(evidenceDir + "/summary.txt")
	if readErr != nil {
		t.Fatalf("read summary: %v", readErr)
	}
	if !strings.Contains(string(summary), "real_mode_guard=blocked_private_or_local_host") {
		t.Fatalf("expected real mode guard summary, got %s", string(summary))
	}
}
