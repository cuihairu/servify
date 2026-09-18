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
	"time"
)

// TestPgvectorScriptWritesEvidence 走通 test-pgvector-acceptance.sh 全流程：
// embedding/LLM mock → postgres(pgvector) 真库起服 → 401 负例 → psql 提权
// admin → enable→status(pgvector/healthy) → 上传即索引 → psql 数据证据
// （vector 扩展/行数/1536 维/迁移版本）→ 语义检索正负例。
// 该验收依赖一个可达的 pg+pgvector 实例（默认 runner-docker 上的
// servify-pgvector 容器），环境不具备时跳过——CI 上没有 pg。
func TestPgvectorScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	host := getenvOr("PGVECTOR_HOST", "192.168.5.5")
	port := getenvOr("PGVECTOR_PORT", "5433")
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 3*time.Second)
	if err != nil {
		t.Skipf("requires a reachable pg+pgvector instance at %s:%s: %v", host, port, err)
	}
	_ = conn.Close()

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"EVIDENCE_DIR=%q bash ./test-pgvector-acceptance.sh",
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected pgvector acceptance success, err=%v output=%s", err, string(output))
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
		"psql-extension.txt",
		"psql-docs.txt",
		"psql-dims.txt",
		"psql-schema-migrations.txt",
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
		"pg_prepared=true",
		"embedding_mock_ok=true",
		"login_role=admin",
		"step=ai_chain",
		"step=psql_evidence",
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
	if len(strings.TrimSpace(string(negative))) != 0 {
		t.Fatalf("expected unrelated query to yield no sources, got %s", string(negative))
	}

	// 正面证据：语义检索命中目标文档的 chunk；状态上报 pgvector 且 healthy。
	positive, err := os.ReadFile(filepath.Join(evidenceDir, "query-printer.parsed"))
	if err != nil {
		t.Fatalf("read query evidence: %v", err)
	}
	if !strings.Contains(string(positive), "title=打印机故障排查指南") {
		t.Fatalf("expected printer doc hit, got %s", string(positive))
	}
	enabled, err := os.ReadFile(filepath.Join(evidenceDir, "ai-status-enabled.parsed"))
	if err != nil {
		t.Fatalf("read enabled status evidence: %v", err)
	}
	for _, want := range []string{"provider=pgvector", "enabled=True", "healthy=True"} {
		if !strings.Contains(string(enabled), want) {
			t.Fatalf("expected %q in ai-status-enabled.parsed, got %s", want, string(enabled))
		}
	}
	dims, err := os.ReadFile(filepath.Join(evidenceDir, "psql-dims.txt"))
	if err != nil {
		t.Fatalf("read dims evidence: %v", err)
	}
	if !strings.Contains(string(dims), "dims=1536") {
		t.Fatalf("expected embedding dims 1536, got %s", string(dims))
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
		"pg_prepared":                            "true",
		"unauthenticated_ai_status_rejected_401": "true",
		"admin_registered":                       "true",
		"ai_status_reports_pgvector":             "true",
		"knowledge_provider_enabled_healthy":     "true",
		"documents_uploaded_and_indexed":         "true",
		"psql_vector_extension_present":          "true",
		"psql_docs_indexed_with_embeddings":      "true",
		"psql_embedding_dims_1536":               "true",
		"psql_schema_migrations_applied":         "true",
		"semantic_query_hits_expected_doc":       "true",
		"unrelated_query_filtered_out":           "true",
	} {
		if got := manifest.Checks[check]; got != want {
			t.Fatalf("manifest check %s = %q, want %q", check, got, want)
		}
	}
}

func getenvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
