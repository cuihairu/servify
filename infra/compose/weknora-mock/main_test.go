package main

// weknora-mock 包内单测：httptest 直驱 routes()，逐端点覆盖成功与失败语义
// （协议级黑盒回归另有 scripts/weknora_mock_protocol_test.go，驱动真实二进制）。
// 只用标准库；coverage 依赖包内语句全覆盖以对齐 internal 100% 口径。

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(newMock().routes())
	t.Cleanup(srv.Close)
	return srv
}

func doJSON(t *testing.T, method, url string, body io.Reader) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, url, err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("%s %s 响应不是 JSON 对象：%s", method, url, raw)
	}
	return resp.StatusCode, obj
}

func post(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	return doJSON(t, "POST", url, strings.NewReader(body))
}

func uploadDoc(t *testing.T, base, kb, title string) string {
	t.Helper()
	body := fmt.Sprintf(`{"type":"text","title":%q,"content":%q}`, title, "内容 "+title)
	status, resp := post(t, fmt.Sprintf("%s/api/v1/knowledge/%s/documents", base, kb), body)
	if status != http.StatusOK || resp["success"] != true {
		t.Fatalf("上传 %s 应成功，实际 %d：%v", title, status, resp)
	}
	data, _ := resp["data"].(map[string]any)
	id, _ := data["id"].(string)
	if id == "" {
		t.Fatalf("上传响应缺 data.id：%v", resp)
	}
	return id
}

func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer(t)
	status, resp := doJSON(t, "GET", srv.URL+"/api/v1/health", nil)
	if status != http.StatusOK {
		t.Fatalf("health 应 200，实际 %d", status)
	}
	if s, _ := resp["status"].(string); s != "ok" {
		t.Fatalf("health status 应为 ok：%v", resp["status"])
	}
	if v, _ := resp["version"].(string); v == "" {
		t.Fatalf("health 应带 version：%v", resp)
	}
}

func TestSearchValidationAndEmptyLibrary(t *testing.T) {
	srv := newTestServer(t)

	// 非法 JSON 体。
	status, resp := post(t, srv.URL+"/api/v1/knowledge/search", "{not-json")
	if status != http.StatusBadRequest || resp["success"] != false {
		t.Fatalf("非法 JSON 应 400+success=false，实际 %d：%v", status, resp)
	}
	// 缺 kb_id。
	status, resp = post(t, srv.URL+"/api/v1/knowledge/search", `{"query":"x"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("缺 kb_id 应 400，实际 %d：%v", status, resp)
	}
	// 空库命中为空数组（limit<=0 走默认 10）。
	status, resp = post(t, srv.URL+"/api/v1/knowledge/search", `{"query":"x","kb_id":"kb-a"}`)
	if status != http.StatusOK || resp["success"] != true {
		t.Fatalf("空库检索应成功，实际 %d：%v", status, resp)
	}
	data, _ := resp["data"].(map[string]any)
	results, _ := data["results"].([]any)
	if len(results) != 0 {
		t.Fatalf("空库应无命中：%v", results)
	}
	if total, _ := data["total"].(float64); total != 0 {
		t.Fatalf("total 应为 0：%v", data["total"])
	}
	if strategy, _ := data["strategy"].(string); strategy != "hybrid" {
		t.Fatalf("缺省 strategy 应为 hybrid：%v", data["strategy"])
	}
}

func TestUploadSearchRoundTripAndIsolation(t *testing.T) {
	srv := newTestServer(t)
	id := uploadDoc(t, srv.URL, "kb-a", "打印机卡纸手册")

	// 命中：字段面与分值契约。
	status, resp := post(t, srv.URL+"/api/v1/knowledge/search", `{"query":"卡纸","kb_id":"kb-a","limit":5}`)
	if status != http.StatusOK {
		t.Fatalf("检索应 200，实际 %d", status)
	}
	data, _ := resp["data"].(map[string]any)
	results, _ := data["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("应命中 1 条：%v", results)
	}
	hit, _ := results[0].(map[string]any)
	if got, _ := hit["document_id"].(string); got != id {
		t.Fatalf("document_id 不一致：%v vs %s", hit["document_id"], id)
	}
	if score, _ := hit["score"].(float64); score <= 0 {
		t.Fatalf("score 应为正：%v", hit["score"])
	}
	if _, ok := hit["highlights"].([]any); !ok {
		t.Fatalf("highlights 应为数组：%v", hit)
	}
	if chunk, _ := hit["chunk_index"].(float64); chunk != 0 {
		t.Fatalf("chunk_index 应为 0：%v", hit["chunk_index"])
	}
	if src, _ := hit["source"].(string); src != "weknora-mock" {
		t.Fatalf("source 应为 weknora-mock：%v", hit["source"])
	}

	// 跨库隔离 + 上传的 metadata 在检索结果回显。
	status, resp = post(t, srv.URL+"/api/v1/knowledge/search", `{"query":"卡纸","kb_id":"kb-b"}`)
	results, _ = resp["data"].(map[string]any)["results"].([]any)
	if status != http.StatusOK || len(results) != 0 {
		t.Fatalf("kb-b 不应命中 kb-a 文档：%d %v", status, results)
	}

	// 同库但不匹配的关键词：query 子串过滤路径。
	_, resp = post(t, srv.URL+"/api/v1/knowledge/search", `{"query":"不存在的词","kb_id":"kb-a"}`)
	results, _ = resp["data"].(map[string]any)["results"].([]any)
	if len(results) != 0 {
		t.Fatalf("不匹配关键词不应命中：%v", results)
	}

	// limit 截断：同库第二篇后 limit=1 只回 1 条。
	uploadDoc(t, srv.URL, "kb-a", "扫描仪手册")
	status, resp = post(t, srv.URL+"/api/v1/knowledge/search", `{"query":"手册","kb_id":"kb-a","limit":1}`)
	results, _ = resp["data"].(map[string]any)["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("limit=1 应只回 1 条：%v", results)
	}
}

func TestUploadValidationAndEcho(t *testing.T) {
	srv := newTestServer(t)

	status, resp := post(t, srv.URL+"/api/v1/knowledge/kb-a/documents", "{bad")
	if status != http.StatusBadRequest || resp["success"] != false {
		t.Fatalf("非法 JSON 应 400+success=false，实际 %d：%v", status, resp)
	}
	status, resp = post(t, srv.URL+"/api/v1/knowledge/kb-a/documents", `{"content":"x"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("缺 title 应 400，实际 %d", status)
	}

	status, resp = post(t, srv.URL+"/api/v1/knowledge/kb-a/documents",
		`{"title":"带元数据","content":"正文","tags":["t1"],"metadata":{"source":"unit"}}`)
	if status != http.StatusOK || resp["success"] != true {
		t.Fatalf("上传应成功：%d %v", status, resp)
	}
	data, _ := resp["data"].(map[string]any)
	if s, _ := data["status"].(string); s != "indexed" {
		t.Fatalf("status 应为 indexed：%v", data["status"])
	}
	if chunk, _ := data["chunk_count"].(float64); chunk != 1 {
		t.Fatalf("chunk_count 应为 1：%v", data["chunk_count"])
	}
	meta, _ := data["metadata"].(map[string]any)
	if meta["source"] != "unit" {
		t.Fatalf("metadata 应回显：%v", data["metadata"])
	}
	if _, ok := data["processed_at"].(string); !ok {
		t.Fatalf("processed_at 应为字符串：%v", data)
	}
}

func TestDeleteValidationLifecycle(t *testing.T) {
	srv := newTestServer(t)
	id := uploadDoc(t, srv.URL, "kb-a", "待删除文档")

	// 不存在 / 跨库删除都 404。
	status, resp := doJSON(t, "DELETE", srv.URL+"/api/v1/knowledge/kb-a/documents/missing", nil)
	if status != http.StatusNotFound || resp["success"] != false {
		t.Fatalf("删除不存在文档应 404+success=false，实际 %d：%v", status, resp)
	}
	status, _ = doJSON(t, "DELETE", srv.URL+"/api/v1/knowledge/kb-b/documents/"+id, nil)
	if status != http.StatusNotFound {
		t.Fatalf("跨库删除应 404，实际 %d", status)
	}

	status, resp = doJSON(t, "DELETE", srv.URL+"/api/v1/knowledge/kb-a/documents/"+id, nil)
	if status != http.StatusOK || resp["success"] != true {
		t.Fatalf("删除应成功：%d %v", status, resp)
	}
	// 删除后检索不再命中。
	_, resp = post(t, srv.URL+"/api/v1/knowledge/search", `{"query":"待删除","kb_id":"kb-a"}`)
	results, _ := resp["data"].(map[string]any)["results"].([]any)
	if len(results) != 0 {
		t.Fatalf("删除后不应命中：%v", results)
	}
}

func TestKnowledgeBaseStats(t *testing.T) {
	srv := newTestServer(t)
	uploadDoc(t, srv.URL, "kb-stats", "统计文档一")
	uploadDoc(t, srv.URL, "kb-stats", "统计文档二")

	status, resp := doJSON(t, "GET", srv.URL+"/api/v1/knowledge/kb-stats", nil)
	if status != http.StatusOK || resp["success"] != true {
		t.Fatalf("知识库详情应成功：%d %v", status, resp)
	}
	data, _ := resp["data"].(map[string]any)
	stats, _ := data["stats"].(map[string]any)
	if count, _ := stats["document_count"].(float64); count != 2 {
		t.Fatalf("document_count 应为 2：%v", stats)
	}
	if chunk, _ := stats["chunk_count"].(float64); chunk != 2 {
		t.Fatalf("chunk_count 应为 2：%v", stats)
	}
	config, _ := data["config"].(map[string]any)
	if config["embedding_model"] != "bge-large-zh" {
		t.Fatalf("config 应含 embedding_model：%v", config)
	}
}

func TestUnknownEndpointFails(t *testing.T) {
	srv := newTestServer(t)
	status, resp := doJSON(t, "DELETE", srv.URL+"/api/v1/sessions/s-1/chat", nil)
	if status != http.StatusNotFound || resp["success"] != false {
		t.Fatalf("未实现端点应 404+success=false，实际 %d：%v", status, resp)
	}
	if msg, _ := resp["message"].(string); !strings.Contains(msg, "unhandled endpoint") {
		t.Fatalf("失败信息应含 unhandled endpoint：%v", resp["message"])
	}
}

// failingWriter 让 json.Encoder 走 Encode 错误分支（writeJSON 的日志路径）。
type failingWriter struct{}

func (failingWriter) Header() http.Header       { return http.Header{} }
func (failingWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("write failed") }
func (failingWriter) WriteHeader(int)           {}

func TestWriteJSONEncodeErrorIsLoggedNotPanicked(t *testing.T) {
	writeJSON(failingWriter{}, http.StatusOK, envelope(nil, "x"))
}

func TestEnvelopeAndFailureShapes(t *testing.T) {
	withData := envelope(map[string]any{"k": "v"}, "msg")
	if withData["success"] != true || withData["message"] != "msg" {
		t.Fatalf("envelope 形态错误：%v", withData)
	}
	if _, ok := withData["request_id"].(string); !ok {
		t.Fatalf("envelope 应带 request_id：%v", withData)
	}
	noMessage := envelope(map[string]any{"k": "v"}, "")
	if _, ok := noMessage["message"]; ok {
		t.Fatalf("空 message 不应输出：%v", noMessage)
	}
	noData := envelope(nil, "")
	if _, ok := noData["data"]; ok {
		t.Fatalf("nil data 不应输出 data 键：%v", noData)
	}
	f := failure("boom")
	if f["success"] != false || f["message"] != "boom" {
		t.Fatalf("failure 形态错误：%v", f)
	}
}

func TestFirstLineVariants(t *testing.T) {
	cases := []struct{ in, want string }{
		{"第一行\n第二行", "第一行"},
		{strings.Repeat("x", 100), strings.Repeat("x", 80)},
		{"短句", "短句"},
		{"\n 前置空白", "前置空白"},
	}
	for _, tc := range cases {
		if got := firstLine(tc.in); got != tc.want {
			t.Fatalf("firstLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := firstLine(""); got != "" {
		t.Fatalf("firstLine(\"\") = %q", got)
	}
}

func TestNextIDIncrements(t *testing.T) {
	m := newMock()
	first := m.nextID()
	if first != "weknora-mock-doc-1" {
		t.Fatalf("首个 id 应为 weknora-mock-doc-1，实际 %s", first)
	}
	if second := m.nextID(); second != "weknora-mock-doc-2" {
		t.Fatalf("id 应递增：%s", second)
	}
}

func TestPortFromEnv(t *testing.T) {
	t.Setenv("API_PORT", "")
	if got := portFromEnv(); got != "9000" {
		t.Fatalf("缺省端口应为 9000，实际 %s", got)
	}
	t.Setenv("API_PORT", "12345")
	if got := portFromEnv(); got != "12345" {
		t.Fatalf("API_PORT 应透传，实际 %s", got)
	}
}

// TestMockMainSubprocess 以仓库 main 包测试先例（cmd/gen-baseline）的
// 子进程 re-exec 手法覆盖 main()：子进程起真实监听并自检就绪后 os.Exit(0)。
func TestMockMainSubprocess(t *testing.T) {
	if os.Getenv("WEKNORA_MOCK_SUBPROCESS") != "1" {
		return
	}
	// 子进程分支：起就绪探针 goroutine，health 可达即退出。
	port := portFromEnv()
	go func() {
		base := "http://127.0.0.1:" + port
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			resp, err := http.Get(base + "/api/v1/health")
			if err == nil {
				_ = resp.Body.Close()
				fmt.Println("subprocess-ready")
				os.Exit(0)
			}
			time.Sleep(20 * time.Millisecond)
		}
		os.Exit(1)
	}()
	main()
}

func TestMainStartsAndServes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)
	_ = listener.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^TestMockMainSubprocess$")
	cmd.Env = append(os.Environ(), "WEKNORA_MOCK_SUBPROCESS=1", "API_PORT="+port)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "subprocess-ready") {
		t.Fatalf("子进程应就绪后干净退出：err=%v out=%s", err, out)
	}
	if !strings.Contains(string(out), "listening on :"+port) {
		t.Fatalf("子进程应输出监听日志：%s", out)
	}
}
