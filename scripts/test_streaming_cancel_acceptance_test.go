package scripts

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// freePortPair 同时持有两个监听再统一释放：freePort 单端口两连取可能拿到
// 同一端口（先取的已释放，后取的复用），servify 与 mock LLM 撞端口。
func freePortPair(t *testing.T) (string, string) {
	t.Helper()
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	second, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for second free port: %v", err)
	}
	_, firstPort, _ := net.SplitHostPort(first.Addr().String())
	_, secondPort, _ := net.SplitHostPort(second.Addr().String())
	_ = first.Close()
	_ = second.Close()
	return firstPort, secondPort
}

// TestStreamingCancelAcceptanceScriptWritesEvidence 真跑
// test-streaming-cancel-acceptance.sh 自起模式（sqlite + 本地 OpenAI 兼容
// SSE mock，零外网出站），校验证据与 manifest——流式取消与持久化验收
// （模型中断 / 客户端断线 / 重复提交 / 转人工）此前只停在 Go 单测的桩面，
// 无真服务 WS 链路证据。
func TestStreamingCancelAcceptanceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()
	servifyPort, mockPort := freePortPair(t)

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"SERVIFY_PORT=%s MOCK_PORT=%s EVIDENCE_DIR=%q bash ./test-streaming-cancel-acceptance.sh",
		servifyPort, mockPort, evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected streaming cancel acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"build-output.txt",
		"server-log.txt",
		"mock-log.txt",
		"ws-normal.json",
		"ws-disconnect.json",
		"ws-retry.json",
		"ws-abort.json",
		"ws-duplicate.json",
		"ws-transfer.json",
	} {
		if _, statErr := os.Stat(filepath.Join(evidenceDir, name)); statErr != nil {
			t.Fatalf("expected evidence file %s: %v\noutput=%s", name, statErr, string(output))
		}
	}

	raw, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		Provider string `json:"provider"`
		Status   struct {
			Overall string `json:"overall"`
		} `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if manifest.Provider != "streaming-cancel" {
		t.Fatalf("provider = %q, want streaming-cancel", manifest.Provider)
	}
	if manifest.Status.Overall != "passed" {
		t.Fatalf("overall = %q, want passed", manifest.Status.Overall)
	}
	for _, check := range []string{
		"build_ok",
		"mock_llm_ok",
		"server_ready_ok",
		"normal_stream_ok",
		"client_disconnect_persist_ok",
		"ws_reconnect_retry_ok",
		"model_interrupt_consistent_ok",
		"no_error_detail_leak_ok",
		"duplicate_submit_ok",
		"transfer_keyword_no_model_ok",
	} {
		if manifest.Checks[check] != "true" {
			t.Fatalf("check %s = %q, want true", check, manifest.Checks[check])
		}
	}

	summary, err := os.ReadFile(filepath.Join(evidenceDir, "summary.txt"))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	summaryText := string(summary)
	for _, want := range []string{
		"mock_llm_ok=true",
		"normal_stream_ok=true",
		"client_disconnect_persist_ok=true",
		"ws_reconnect_retry_ok=true",
		"model_interrupt_consistent_ok=true",
		"duplicate_submit_ok=true",
		"transfer_keyword_no_model_ok=true",
		"no_error_detail_leak_ok=true",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, want) {
			t.Fatalf("expected %q in summary, got %s", want, summaryText)
		}
	}
}
