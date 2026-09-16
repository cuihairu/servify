package scripts

import (
	"encoding/json"
	"fmt"
	"io"
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

// TestSuggestionAcceptanceScriptWritesEvidence 用 mock server 走通
// test-suggestion-acceptance.sh 的 initial/next(GET+POST)/400 流程,并校验证据。
func TestSuggestionAcceptanceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	var (
		mu           sync.Mutex
		token        = "suggestion-admin-token"
		createdDocs  []map[string]interface{}
		publicAuthed = false
	)

	servify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/health":
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
		case r.URL.Path == "/api/v1/auth/register":
			mu.Lock()
			defer mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"` + token + `","refresh_token":"suggestion-refresh","user":{"id":1,"role":"admin"}}`))
		case r.URL.Path == "/api/knowledge-docs" && r.Method == http.MethodPost:
			if r.Header.Get("Authorization") != "Bearer "+token {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid token"}`))
				return
			}
			body, _ := io.ReadAll(r.Body)
			var doc map[string]interface{}
			_ = json.Unmarshal(body, &doc)
			mu.Lock()
			createdDocs = append(createdDocs, doc)
			count := len(createdDocs)
			mu.Unlock()
			doc["id"] = count
			resp, _ := json.Marshal(map[string]interface{}{"success": true, "data": doc})
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(resp)
		case r.URL.Path == "/public/suggestions/initial":
			mu.Lock()
			if r.Header.Get("Authorization") != "" {
				publicAuthed = true
			}
			mu.Unlock()
			_, _ = w.Write([]byte(`{"success":true,"data":{"questions":[` +
				`{"question":"如何联系人工客服","source":"knowledge_doc","source_id":"4","category":"faq","score":1},` +
				`{"question":"如何重置登录密码","source":"knowledge_doc","source_id":"3","category":"account","score":0.66},` +
				`{"question":"如何导出历史账单","source":"knowledge_doc","source_id":"2","category":"billing","score":0.33}` +
				`],"meta":{"strategy":"public_knowledge_recency"}}}`))
		case r.URL.Path == "/public/suggestions/next" && r.Method == http.MethodGet:
			mu.Lock()
			if r.Header.Get("Authorization") != "" {
				publicAuthed = true
			}
			mu.Unlock()
			if r.URL.Query().Get("query") == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"query is required"}`))
				return
			}
			if r.URL.Query().Get("query") != "密码" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"unexpected query"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"query":"密码","questions":[` +
				`{"question":"如何重置登录密码","source":"knowledge_doc","source_id":"3","category":"account","score":1}` +
				`],"meta":{"strategy":"public_knowledge_scored","intent":"general"}}}`))
		case r.URL.Path == "/public/suggestions/next" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var req map[string]interface{}
			_ = json.Unmarshal(body, &req)
			if query, _ := req["query"].(string); query != "账单" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"unexpected query"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"query":"账单","questions":[` +
				`{"question":"如何导出历史账单","source":"knowledge_doc","source_id":"2","category":"billing","score":1}` +
				`],"meta":{"strategy":"public_knowledge_scored","intent":"billing"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer servify.Close()

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"AI_ACCEPTANCE_MODE=mock SERVIFY_URL=%q EVIDENCE_DIR=%q bash ./test-suggestion-acceptance.sh",
		servify.URL,
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected suggestion acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"admin-auth.json",
		"doc-private.json",
		"doc-public-billing.json",
		"doc-public-password.json",
		"doc-public-agent.json",
		"initial-questions.json",
		"next-questions-get.json",
		"next-questions-post.json",
		"next-questions-missing-query.json",
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
		"docs_ok=true",
		"initial_ok=true",
		"next_get_ok=true",
		"next_post_ok=true",
		"next_reject_ok=true",
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
		`"provider": "suggestion"`,
		`"mode": "mock"`,
		`"knowledge_docs_ok": "true"`,
		`"initial_questions_ok": "true"`,
		`"next_questions_get_ok": "true"`,
		`"next_questions_post_ok": "true"`,
		`"next_questions_reject_ok": "true"`,
		`"overall": "passed"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}

	// mock 侧补充校验：4 篇文档恰好 1 篇私有；公开路由全程匿名。
	mu.Lock()
	defer mu.Unlock()
	if len(createdDocs) != 4 {
		t.Fatalf("expected 4 knowledge docs created, got %d", len(createdDocs))
	}
	private := 0
	for _, doc := range createdDocs {
		if public, _ := doc["is_public"].(bool); !public {
			private++
		}
	}
	if private != 1 {
		t.Fatalf("expected exactly 1 private doc, got %d", private)
	}
	if publicAuthed {
		t.Fatal("public suggestion routes must be reachable without Authorization header")
	}
}
