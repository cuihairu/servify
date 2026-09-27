package scripts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// weknora-mock 协议契约测试：直接跑 compose 用的同一份源码
// （infra/compose/weknora-mock/main.go），断言 servify 侧
// apps/server/pkg/weknora 客户端真正解析的每个端点形态。
//
// 之所以值得单独守住：旧版 mock 的 catch-all 对所有路径回
// `{"status":"ok"}`，客户端 `Success` 恒为 false，全栈 mock 栈里
// 上传/检索必失败（而 health 仍正常，故障被伪装成「provider 可用但业务不通」）。
// 这里同时守住失败语义：未实现端点必须显式 success=false，不再静默 200。
func TestWeKnoraMockProtocolSurface(t *testing.T) {
	base := startWeKnoraMock(t)

	// 1) health：status 必须是 ok/healthy（客户端 HealthCheck 只认这两个值）
	health := getJSON(t, base+"/api/v1/health")
	if status, _ := health["status"].(string); status != "ok" && status != "healthy" {
		t.Fatalf("health status 应为 ok/healthy，实际 %v", health["status"])
	}

	// 2) 空库检索：success=true 且 results 为空数组（不是缺字段）
	empty := postJSON(t, base+"/api/v1/knowledge/search", map[string]any{
		"query": "远程协助", "kb_id": "kb-a", "limit": 5, "strategy": "hybrid",
	})
	requireSuccess(t, empty, "空库检索")
	if results := resultsOf(t, empty); len(results) != 0 {
		t.Fatalf("空库检索应无命中，实际 %d 条", len(results))
	}

	// 3) 上传：data.id 非空（provider.UpsertDocument 以它为外部 external_id）
	uploaded := postJSON(t, base+"/api/v1/knowledge/kb-a/documents", map[string]any{
		"type":    "text",
		"title":   "远程协助手册",
		"content": "远程协助与智能客服的使用说明。\n第二行内容。",
		"tags":    []string{"协助", "客服"},
		"metadata": map[string]any{
			"source": "unit-test",
		},
	})
	requireSuccess(t, uploaded, "文档上传")
	data, _ := uploaded["data"].(map[string]any)
	if data == nil {
		t.Fatalf("上传响应缺少 data 对象：%v", uploaded)
	}
	docID, _ := data["id"].(string)
	if strings.TrimSpace(docID) == "" {
		t.Fatalf("上传响应 data.id 为空，external_id 无法回写：%v", uploaded)
	}
	if title, _ := data["title"].(string); title != "远程协助手册" {
		t.Fatalf("上传响应 data.title 未回显，实际 %q", title)
	}

	// 4) 检索命中：上传后同库同关键词必须命中，且字段名与客户端解析一致
	found := postJSON(t, base+"/api/v1/knowledge/search", map[string]any{
		"query": "远程协助", "kb_id": "kb-a", "limit": 5, "threshold": 0.2, "strategy": "hybrid",
	})
	requireSuccess(t, found, "上传后检索")
	hits := resultsOf(t, found)
	if len(hits) != 1 {
		t.Fatalf("应命中 1 条，实际 %d 条：%v", len(hits), found["data"])
	}
	hit := hits[0]
	for _, key := range []string{"document_id", "title", "content", "score", "source"} {
		if _, ok := hit[key]; !ok {
			t.Fatalf("检索结果缺少字段 %q（pkg/weknora.SearchResult 依赖）：%v", key, hit)
		}
	}
	if got, _ := hit["document_id"].(string); got != docID {
		t.Fatalf("命中的 document_id=%v 与上传返回 id=%s 不一致", hit["document_id"], docID)
	}
	if score, ok := hit["score"].(float64); !ok || score <= 0 {
		t.Fatalf("score 应为正数（客户端按 threshold 过滤）：%v", hit["score"])
	}

	// 5) 作用域隔离：换 kb_id 不得命中他库文档
	other := postJSON(t, base+"/api/v1/knowledge/search", map[string]any{
		"query": "远程协助", "kb_id": "kb-b", "limit": 5,
	})
	requireSuccess(t, other, "跨库检索")
	if results := resultsOf(t, other); len(results) != 0 {
		t.Fatalf("kb-b 不应命中 kb-a 的文档，实际 %d 条", len(results))
	}

	// 6) 知识库详情：success=true + data.stats（GetKnowledgeBase 解析路径）
	kb := getJSON(t, base+"/api/v1/knowledge/kb-a")
	requireSuccess(t, kb, "知识库详情")
	if kbData, _ := kb["data"].(map[string]any); kbData == nil {
		t.Fatalf("知识库详情缺少 data 对象：%v", kb)
	}

	// 7) 删除：命中则 success=true，再删同一 id 必须失败（非 2xx 或 success=false）
	deleted := deleteJSON(t, base+"/api/v1/knowledge/kb-a/documents/"+docID)
	requireSuccess(t, deleted, "文档删除")
	missing := deleteJSONStatus(t, base+"/api/v1/knowledge/kb-a/documents/"+docID)
	if missing.status < 400 {
		t.Fatalf("删除不存在文档应返回 4xx，实际 %d：%s", missing.status, missing.body)
	}
	if obj := decodeObject(t, missing.body); obj["success"] == true {
		t.Fatalf("删除不存在文档不得报 success=true：%s", missing.body)
	}
	afterDelete := postJSON(t, base+"/api/v1/knowledge/search", map[string]any{
		"query": "远程协助", "kb_id": "kb-a", "limit": 5,
	})
	if results := resultsOf(t, afterDelete); len(results) != 0 {
		t.Fatalf("删除后不应再命中，实际 %d 条", len(results))
	}

	// 8) 未实现端点必须显式失败（旧 catch-all 会伪装成 200 成功）
	unknownResp := deleteJSONStatus(t, base+"/api/v1/sessions/s-1/chat")
	if unknownResp.status != http.StatusNotFound {
		t.Fatalf("未实现端点应 404，实际 %d：%s", unknownResp.status, unknownResp.body)
	}
	if obj := decodeObject(t, unknownResp.body); obj["success"] != false {
		t.Fatalf("未实现端点必须 success=false 以免被误判为成功：%s", unknownResp.body)
	}

	// 9) 缺必填字段的失败语义：search 缺 kb_id、upload 缺 title 都要被拒
	badSearch := postJSONStatus(t, base+"/api/v1/knowledge/search", map[string]any{"query": "x"})
	if badSearch.status < 400 {
		t.Fatalf("search 缺 kb_id 应 4xx，实际 %d：%s", badSearch.status, badSearch.body)
	}
	badUpload := postJSONStatus(t, base+"/api/v1/knowledge/kb-a/documents", map[string]any{"content": "x"})
	if badUpload.status < 400 {
		t.Fatalf("upload 缺 title 应 4xx，实际 %d：%s", badUpload.status, badUpload.body)
	}
}

type httpResult struct {
	status int
	body   []byte
}

func startWeKnoraMock(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("weknora-mock 契约测试按 unix 进程管理编写")
	}

	repoRoot := weknoraMockRepoRoot(t)
	bin := filepath.Join(t.TempDir(), "weknora-mock")
	build := exec.Command("go", "build", "-o", bin, "./infra/compose/weknora-mock")
	build.Dir = repoRoot
	build.Env = append(os.Environ(), "GOFLAGS=", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build weknora-mock: %v\n%s", err, out)
	}

	port := freeTCPPort(t)
	cmd := exec.Command(bin)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), fmt.Sprintf("API_PORT=%d", port))
	logs := &bytes.Buffer{}
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start weknora-mock: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if logs.Len() > 0 && t.Failed() {
			t.Logf("weknora-mock 输出：\n%s", logs.String())
		}
	})

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			return base
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("weknora-mock 未在 %s 就绪，输出：%s", base, logs.String())
	return base
}

func weknoraMockRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Dir(wd)
}

func getJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read GET %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s 状态 %d：%s", url, resp.StatusCode, body)
	}
	return decodeObject(t, body)
}

func postJSON(t *testing.T, url string, payload any) map[string]any {
	t.Helper()
	res := postJSONStatus(t, url, payload)
	require2xx(t, res, "POST "+url)
	return decodeObject(t, res.body)
}

func postJSONStatus(t *testing.T, url string, payload any) httpResult {
	t.Helper()
	buf, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read POST %s: %v", url, err)
	}
	return httpResult{status: resp.StatusCode, body: body}
}

func deleteJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	res := deleteJSONStatus(t, url)
	require2xx(t, res, "DELETE "+url)
	return decodeObject(t, res.body)
}

func deleteJSONStatus(t *testing.T, url string) httpResult {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatalf("build DELETE %s: %v", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read DELETE %s: %v", url, err)
	}
	return httpResult{status: resp.StatusCode, body: body}
}

func require2xx(t *testing.T, res httpResult, what string) {
	t.Helper()
	if res.status < 200 || res.status > 299 {
		t.Fatalf("%s 期望 2xx，实际 %d：%s", what, res.status, res.body)
	}
}

func decodeObject(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatalf("响应不是 JSON 对象：%s (%v)", body, err)
	}
	return obj
}

func requireSuccess(t *testing.T, payload map[string]any, what string) {
	t.Helper()
	if payload["success"] != true {
		t.Fatalf("%s 期望 success=true，实际 %v", what, payload)
	}
}

func resultsOf(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("检索响应缺少 data 对象：%v", payload)
	}
	raw, ok := data["results"].([]any)
	if !ok {
		t.Fatalf("检索响应缺少 data.results 数组：%v", payload)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		hit, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("data.results 元素不是对象：%v", item)
		}
		out = append(out, hit)
	}
	return out
}
