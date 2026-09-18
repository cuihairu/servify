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

// TestPerfBaselineScriptWritesEvidence 真跑 test-perf-baseline.sh 的 smoke 档：
// 构建 → mock LLM → 派生压测配置 → sqlite 起服 → 401 负例 → admin 注册登录 →
// perfbench 四场景（ticket 读写/AI 查询/文件上传/WS 连接）→ 结果校验 →
// manifest 写入。smoke 档秒级、无外部依赖（sqlite 内嵌、LLM 为内嵌 mock），
// CI 上作为性能链路的守护测试真实执行，不 skip。
// 端口占用（SERVIFY_PORT/MOCK_PORT 默认 18107/18108）由脚本预检显式报错。
func TestPerfBaselineScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"PERF_SCALE=smoke EVIDENCE_DIR=%q bash ./test-perf-baseline.sh",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected perf baseline success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"results.json",
		"perfbench-output.txt",
		"build-output.txt",
		"server-log.txt",
		"mock-log.txt",
		"unauthenticated-tickets.txt",
		"register-admin.txt",
		"login-admin.txt",
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
		"perf_scale=smoke",
		"build_ok=true",
		"server_ready=true",
		"mock_llm_ok=true",
		"unauthenticated_tickets_rejected_401=true",
		"admin_ready=true",
		"perfbench_ok=true",
		"scenario=tickets-mixed",
		"scenario=ai-query",
		"scenario=upload-knowledge",
		"ws_max_conns=20",
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
		`"provider": "perf"`,
		`"scale": "smoke"`,
		`"overall": "passed"`,
		`"tickets_scenario_passed": "true"`,
		`"ai_query_scenario_passed": "true"`,
		`"upload_knowledge_scenario_passed": "true"`,
		`"ws_connections_passed": "true"`,
		`"ws_stats_reconciled": "true"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
