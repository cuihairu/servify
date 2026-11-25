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

// TestWebBrowserAcceptanceScriptWritesEvidence 真跑
// test-web-browser-acceptance.sh 全链路（真实 Chromium 驱动真实服务 +
// demo-sdk 挂件 + admin SPA 同源代理：嵌入/鉴权/来源/断线重连/接管/转派/
// 工单/管理端 UI）。浏览器工具链（CHROME_BIN + PLAYWRIGHT_NODE_PATH）
// 缺失时脚本自身整体 SKIP（exit 0 不写证据），本测试跟随同一口径跳过——
// CI 无 Chromium 走该路径；本地具备工具链时真实重放，陈旧入库证据蒙混不过。
func TestWebBrowserAcceptanceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}
	if os.Getenv("CHROME_BIN") == "" || os.Getenv("PLAYWRIGHT_NODE_PATH") == "" {
		t.Skip("browser toolchain (CHROME_BIN, PLAYWRIGHT_NODE_PATH) not set; script would skip")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"SERVIFY_PORT=%s HOST_PORT=%s ADMIN_PORT=%s EVIDENCE_DIR=%q bash ./test-web-browser-acceptance.sh",
		freePort(t), freePort(t), freePort(t), evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected web browser acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"build-output.txt",
		"server-log.txt",
		"admin-auth.json",
		"agent-create-1.json",
		"agent-create-2.json",
		"customer-create.json",
		"kb-upload.json",
		"browser-summary.txt",
		"browser-result.json",
		"visitor-ws-frames.json",
		"visitor-effects.json",
		"admin-ui.json",
		"driver-output.txt",
		"visitor-embed.png",
		"visitor-sources.png",
		"visitor-reconnect.png",
		"admin-workspace.png",
		"admin-ticket.png",
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
		"agent_created_ok=true",
		"customer_created_ok=true",
		"kb_seeded_ok=true",
		"browser_ok=true",
		"assign_ok=true",
		"agent_reply_ok=true",
		"transfer_ok=true",
		"ticket_ok=true",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, want) {
			t.Fatalf("expected %q in summary, got %s", want, summaryText)
		}
	}

	// 浏览器侧逐项判定（browser-summary.txt，键同驱动 rec 名）：
	// 访客四项（embed/auth/sources/reconnect）+ 会话动作四项
	// （assign/agent_reply/transfer/ticket）+ 管理端四项
	// （admin_login/admin_workspace/transfer_ui/ticket_ui）。
	browserSummary, err := os.ReadFile(filepath.Join(evidenceDir, "browser-summary.txt"))
	if err != nil {
		t.Fatalf("read browser-summary: %v", err)
	}
	browserText := string(browserSummary)
	for _, want := range []string{
		"embed=true",
		"auth=true",
		"sources=true",
		"reconnect=true",
		"assign=true",
		"agent_reply=true",
		"transfer=true",
		"ticket=true",
		"admin_login=true",
		"admin_workspace=true",
		"transfer_ui=true",
		"ticket_ui=true",
	} {
		if !strings.Contains(browserText, want) {
			t.Fatalf("expected %q in browser-summary, got %s", want, browserText)
		}
	}

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "web-browser"`,
		`"overall": "passed"`,
		`"browser_ok": "true"`,
		`"customer_created_ok": "true"`,
		`"agent_reply_ok": "true"`,
		`"transfer_ok": "true"`,
		`"ticket_ok": "true"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
