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

// TestLocalKnowledgeScriptWritesEvidence 走通 test-local-knowledge-acceptance.sh
// 全流程：sqlite + knowledge/embedding/ai 三 local（零外部依赖、零网络出站）
// 起服 → 401 负例 → sqlite3 数据面提权 admin → enable→status(local/healthy)
// → 上传即索引（真实分块+确定性哈希嵌入 256 维）→ 语义检索正例命中且回答
// 逐字来自知识原文（extractive）→ 无关 query 阈值过滤。与 pgvector 版不同，
// 本验收完全自含，CI 与本地均可真实执行（不跳过）。
func TestLocalKnowledgeScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"SERVIFY_PORT=%s EVIDENCE_DIR=%q bash ./test-local-knowledge-acceptance.sh",
		freePort(t), evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected local knowledge acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"build-output.txt",
		"server-log.txt",
		"unauthenticated-ai-status.txt",
		"register-admin.txt",
		"login-admin.txt",
		"ai-status.txt",
		"knowledge-provider-enable.txt",
		"ai-status-enabled.parsed",
		"upload-printer.txt",
		"upload-refund.txt",
		"upload-account.txt",
		"knowledge-sync.txt",
		"sqlite-docs.txt",
		"sqlite-dims.txt",
		"query-printer.parsed",
		"query-unrelated.parsed",
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
		"build_ok=true",
		"login_role=admin",
		"step=ai_chain",
		"step=sqlite_evidence",
		"step=semantic_search",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, step) {
			t.Fatalf("expected %q in summary, got %s", step, summaryText)
		}
	}

	// 负例语义：未认证 AI 面板 401；无关 query 阈值过滤后 sources 为空。
	unauth, err := os.ReadFile(filepath.Join(evidenceDir, "unauthenticated-ai-status.txt"))
	if err != nil {
		t.Fatalf("read unauthenticated evidence: %v", err)
	}
	if !strings.Contains(string(unauth), "HTTP/1.1 401") {
		t.Fatalf("expected 401 for unauthenticated ai status, got %s", string(unauth))
	}
	negative, err := os.ReadFile(filepath.Join(evidenceDir, "query-unrelated.parsed"))
	if err != nil {
		t.Fatalf("read negative query evidence: %v", err)
	}
	if strings.Contains(string(negative), "title=") {
		t.Fatalf("expected unrelated query to yield no sources, got %s", string(negative))
	}

	// 正面证据：语义检索命中目标文档且 strategy=local；回答抽取自原文；
	// 状态上报 local 且 enabled/healthy；sqlite 证据（行数/维度）。
	positive, err := os.ReadFile(filepath.Join(evidenceDir, "query-printer.parsed"))
	if err != nil {
		t.Fatalf("read query evidence: %v", err)
	}
	for _, want := range []string{"title=打印机故障排查指南", "strategy=local", "answer_extractive=true"} {
		if !strings.Contains(string(positive), want) {
			t.Fatalf("expected %q in query-printer.parsed, got %s", want, string(positive))
		}
	}
	enabled, err := os.ReadFile(filepath.Join(evidenceDir, "ai-status-enabled.parsed"))
	if err != nil {
		t.Fatalf("read enabled status evidence: %v", err)
	}
	for _, want := range []string{"provider=local", "enabled=True", "healthy=True"} {
		if !strings.Contains(string(enabled), want) {
			t.Fatalf("expected %q in ai-status-enabled.parsed, got %s", want, string(enabled))
		}
	}
	dims, err := os.ReadFile(filepath.Join(evidenceDir, "sqlite-dims.txt"))
	if err != nil {
		t.Fatalf("read dims evidence: %v", err)
	}
	if !strings.Contains(string(dims), "dims=256") {
		t.Fatalf("expected embedding dims 256, got %s", string(dims))
	}

	manifestRaw, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		Status struct {
			Overall string `json:"overall"`
		} `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if jsonErr := json.Unmarshal(manifestRaw, &manifest); jsonErr != nil {
		t.Fatalf("parse manifest: %v", jsonErr)
	}
	if manifest.Status.Overall != "passed" {
		t.Fatalf("manifest status.overall = %q, want passed", manifest.Status.Overall)
	}
	for check, want := range map[string]string{
		"build_ok":                               "true",
		"ready_ok":                               "true",
		"unauthenticated_ai_status_rejected_401": "true",
		"admin_registered":                       "true",
		"ai_status_reports_local":                "true",
		"knowledge_provider_enabled_healthy":     "true",
		"documents_uploaded_and_indexed":         "true",
		"knowledge_sync_attempted":               "true",
		"sqlite_docs_indexed_with_embeddings":    "true",
		"sqlite_chunking_evidence":               "true",
		"sqlite_embedding_dims_256":              "true",
		"semantic_query_hits_expected_doc":       "true",
		"query_answer_extractive_from_doc":       "true",
		"unrelated_query_filtered_out":           "true",
	} {
		if got := manifest.Checks[check]; got != want {
			t.Fatalf("manifest check %s = %q, want %q", check, got, want)
		}
	}
}

// freePort 找一个空闲端口给验收服务器，避免与并行测试/本地服务冲突。
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}
