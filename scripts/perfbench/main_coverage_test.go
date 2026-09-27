package main

// perfbench 包内单测（全仓覆盖率收口刀·缺口二）：纯函数/直方图/HTTP 负载器/
// WS 编解码/压测配置生成为进程内直驱；main 与 os.Exit 退出路径按仓库 main 包
// 测试先例（cmd/gen-baseline）以子进程 re-exec 覆盖。全部流量只打本机
// httptest/自建 TCP listener，秒级跑完，CI 可直跑。

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// ---- 纯函数 ----

func TestProfileFor(t *testing.T) {
	full := profileFor("full")
	if full.ticketsOps != 600 || full.aiOps != 200 || full.uploadOps != 100 ||
		full.wsTarget != 200 || full.concurrency != 16 || full.wsBatch != 25 {
		t.Fatalf("full 档负载不符：%+v", full)
	}
	smoke := profileFor("smoke")
	if smoke.ticketsOps != 24 || smoke.aiOps != 8 || smoke.uploadOps != 6 ||
		smoke.wsTarget != 20 || smoke.concurrency != 4 || smoke.wsBatch != 10 {
		t.Fatalf("smoke 档负载不符：%+v", smoke)
	}
	if unknown := profileFor("anything-else"); unknown != smoke {
		t.Fatalf("未知档应回退 smoke：%+v", unknown)
	}
}

func TestPercentile(t *testing.T) {
	if got := percentile(nil, 0.5); got != 0 {
		t.Fatalf("空切片应为 0：%v", got)
	}
	sorted := []float64{10, 20, 30, 40}
	cases := map[float64]float64{0: 10, 0.5: 20, 0.75: 30, 1: 40}
	for q, want := range cases {
		if got := percentile(sorted, q); got != want {
			t.Fatalf("q=%v = %v, want %v（idx=floor(q*(n-1))）", q, got, want)
		}
	}
}

func TestExtractIDVariants(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"wrapper 字符串 id", `{"data":{"id":"7"}}`, "7"},
		{"wrapper 数字 id", `{"data":{"id":42}}`, "42"},
		{"顶层 id", `{"id":"flat-9"}`, "flat-9"},
		{"非 JSON", `not-json`, ""},
		{"wrapper 非对象", `{"data":"oops"}`, ""},
		{"无 id", `{"foo":1}`, ""},
	}
	for _, tc := range cases {
		if got := extractID([]byte(tc.body), "data"); got != tc.want {
			t.Fatalf("%s: extractID = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestIDToString(t *testing.T) {
	if got := idToString("s1"); got != "s1" {
		t.Fatalf("字符串应透传：%q", got)
	}
	if got := idToString(float64(88)); got != "88" {
		t.Fatalf("数字应转十进制：%q", got)
	}
	if got := idToString(nil); got != "" {
		t.Fatalf("nil 应为空：%q", got)
	}
	if got := idToString(struct{}{}); got != "" {
		t.Fatalf("其他类型应为空：%q", got)
	}
}

func TestWsURLFromBaseAndFirstLineAndMin(t *testing.T) {
	if got := wsURLFromBase("http://127.0.0.1:8080"); got != "ws://127.0.0.1:8080/api/v1/ws" {
		t.Fatalf("wsURLFromBase = %q", got)
	}
	if got := firstLine("HTTP/1.1 400 Bad\r\nrest"); got != "HTTP/1.1 400 Bad" {
		t.Fatalf("firstLine 应在 CRLF 处截断：%q", got)
	}
	if got := firstLine("no-crlf"); got != "no-crlf" {
		t.Fatalf("无 CRLF 应原样返回：%q", got)
	}
	if min(2, 3) != 2 || min(5, 1) != 1 || min(4, 4) != 4 {
		t.Fatal("min 语义错误")
	}
}

// ---- 直方图 ----

func TestLatencyHistogramSnapshot(t *testing.T) {
	h := &latencyHistogram{}
	res, _ := h.snapshot(time.Second, 4)
	if res.Ops != 0 || res.Errors != 0 {
		t.Fatalf("空直方图应零值：%+v", res)
	}
	h.record(10)
	h.record(20)
	h.record(30)
	h.record(40)
	h.fail()
	res, samples := h.snapshot(time.Second, 3)
	if res.Ops != 4 || res.Errors != 1 || res.Concurrency != 3 {
		t.Fatalf("计数错误：%+v", res)
	}
	// idx = floor(q*(n-1))：P50→20、P95/P99→30、Max→40。
	if res.P50MS != 20 || res.P95MS != 30 || res.P99MS != 30 || res.MaxMS != 40 {
		t.Fatalf("分位数错误：%+v", res)
	}
	if res.OpsPerSec != 4 {
		t.Fatalf("吞吐应 ops/elapsed=4：%v", res.OpsPerSec)
	}
	if len(samples) != 4 || samples[0] != 10 {
		t.Fatalf("samples 应为排序副本：%v", samples)
	}
	// elapsed=0 时吞吐为 0（除零守卫）。
	h2 := &latencyHistogram{}
	h2.record(1)
	res2, _ := h2.snapshot(0, 1)
	if res2.OpsPerSec != 0 {
		t.Fatalf("elapsed=0 吞吐应为 0：%v", res2.OpsPerSec)
	}
}

// ---- HTTP 负载器 ----

func TestHTTPDoSuccessAndErrorStatus(t *testing.T) {
	var seenAuth, seenCT, seenBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		seenBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	data, status, err := httpDo(srv.Client(), "POST", srv.URL, "tok-1", "application/json", `{"q":1}`)
	if err != nil || status != http.StatusOK {
		t.Fatalf("happy path 应成功：%d %v", status, err)
	}
	if string(data) != `{"ok":true}` {
		t.Fatalf("响应体不符：%s", data)
	}
	if seenAuth != "Bearer tok-1" {
		t.Fatalf("Authorization 头缺失：%q", seenAuth)
	}
	if seenCT != "application/json" || seenBody != `{"q":1}` {
		t.Fatalf("请求头/体不符：ct=%q body=%q", seenCT, seenBody)
	}

	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv500.Close()
	_, status, err = httpDo(srv500.Client(), "GET", srv500.URL, "", "", "")
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") || status != 500 {
		t.Fatalf("非 2xx 应报 HTTP 500：%d %v", status, err)
	}
}

func TestHTTPDoRequestBuildAndTransportErrors(t *testing.T) {
	if _, _, err := httpDo(http.DefaultClient, "GET", "http://127.0.0.1\x00bad", "", "", ""); err == nil {
		t.Fatal("非法 URL 应返回构造错误")
	}
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closed.Close()
	if _, _, err := httpDo(http.DefaultClient, "GET", closed.URL, "", "", ""); err == nil {
		t.Fatal("不可达端点应返回传输错误")
	}
}

func TestHTTPDoBodyReadError(t *testing.T) {
	// 劫持连接回半截 chunked 响应再断开，驱动 io.ReadAll 错误分支。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buf, _ := w.(http.Hijacker).Hijack()
		defer conn.Close()
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nab")
		_ = buf.Flush()
	}))
	defer srv.Close()
	_, status, err := httpDo(srv.Client(), "GET", srv.URL, "", "", "")
	if err == nil || status != http.StatusOK {
		t.Fatalf("截断响应应返回读体错误（状态码仍 200）：%d %v", status, err)
	}
}

func TestRunWorkersDistributesRemainder(t *testing.T) {
	var counter atomic.Int64
	runWorkers(10, 4, func() { counter.Add(1) })
	if got := counter.Load(); got != 10 {
		t.Fatalf("total=10/concurrency=4 应恰好执行 10 次，实际 %d", got)
	}
	runWorkers(0, 4, func() { counter.Add(1) })
	if got := counter.Load(); got != 10 {
		t.Fatalf("total=0 不应执行任何 op：%d", got)
	}
}

// ---- 场景：fake servify ----

// fakeServify 返回一个最小 servify 替身：可按 "METHOD PATH" 开关让特定端点
// 失败；工单评论路径以 /api/tickets/*/comments 通配。
func fakeServify(t *testing.T, behavior map[string]func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if strings.HasPrefix(r.URL.Path, "/api/tickets/") && strings.HasSuffix(r.URL.Path, "/comments") {
			key = "POST /api/tickets/*/comments"
		}
		if h, ok := behavior[key]; ok {
			h(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"1"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func okJSON(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"data":{"id":"42"}}`))
}

func ticketsBehavior() map[string]func(w http.ResponseWriter, r *http.Request) {
	return map[string]func(w http.ResponseWriter, r *http.Request){
		"POST /api/customers":          okJSON,
		"POST /api/tickets":            okJSON,
		"POST /api/tickets/*/comments": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) },
		"GET /api/tickets":             func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) },
	}
}

func TestRunTicketsHappyAndSetupFailures(t *testing.T) {
	profile := loadProfile{ticketsOps: 6, concurrency: 3}
	srv := fakeServify(t, ticketsBehavior())
	res := runTickets(srv.URL, "tok", profile)
	if res.Errors != 0 || res.Ops == 0 {
		t.Fatalf("happy 场景应零错误有延迟样本：%+v", res)
	}
	if res.Scenario != "tickets-mixed" {
		t.Fatalf("场景名错误：%s", res.Scenario)
	}

	// customer 创建失败 → fail-soft note，不 panic。
	failSrv := fakeServify(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"POST /api/customers": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
	})
	res = runTickets(failSrv.URL, "tok", profile)
	if len(res.Notes) != 1 || !strings.HasPrefix(res.Notes[0], "customer_setup_failed:") {
		t.Fatalf("customer 失败应留 note：%+v", res.Notes)
	}

	// customer 响应无 id → 另一 note 分支。
	noIDSrv := fakeServify(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"POST /api/customers": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"unexpected":1}`))
		},
	})
	res = runTickets(noIDSrv.URL, "tok", profile)
	if len(res.Notes) != 1 || res.Notes[0] != "customer_setup_failed: no id in response" {
		t.Fatalf("无 id 应留专用 note：%+v", res.Notes)
	}
}

func TestRunAIQueryHappyAndFailure(t *testing.T) {
	profile := loadProfile{aiOps: 4, concurrency: 2}
	srv := fakeServify(t, nil)
	res := runAIQuery(srv.URL, "tok", profile)
	if res.Errors != 0 || res.Ops != 4 || res.Scenario != "ai-query" {
		t.Fatalf("ai 场景 happy 不符：%+v", res)
	}
	failSrv := fakeServify(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"POST /api/v1/ai/query": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) },
	})
	res = runAIQuery(failSrv.URL, "tok", profile)
	if res.Errors != 4 {
		t.Fatalf("全失败场景应计满错误：%+v", res)
	}
}

func TestRunUploadKnowledgeBranches(t *testing.T) {
	profile := loadProfile{uploadOps: 2, concurrency: 2}

	// 全部成功 + sync ok。
	srv := fakeServify(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"POST /api/v1/upload":              func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) },
		"POST /api/v1/ai/knowledge/upload": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) },
		"POST /api/v1/ai/knowledge/sync":   func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) },
	})
	res := runUploadKnowledge(srv.URL, "tok", profile)
	if res.Errors != 0 || res.Scenario != "upload-knowledge" {
		t.Fatalf("upload 场景 happy 不符：%+v", res)
	}
	if len(res.Notes) != 1 || res.Notes[0] != "knowledge_sync=ok" {
		t.Fatalf("sync ok 应留 note：%+v", res.Notes)
	}

	// 知识上传 503（provider 未启用，预期态）+ sync 失败。
	kbOff := fakeServify(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"POST /api/v1/upload":              func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) },
		"POST /api/v1/ai/knowledge/upload": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) },
		"POST /api/v1/ai/knowledge/sync":   func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) },
	})
	res = runUploadKnowledge(kbOff.URL, "tok", profile)
	if res.Errors != 0 {
		t.Fatalf("503 是预期态不应计错：%+v", res)
	}
	found := map[string]bool{}
	for _, n := range res.Notes {
		found[n] = true
	}
	if !found["knowledge_upload=not-enabled (expected without external provider)"] ||
		!found["knowledge_sync=not-enabled (expected without external provider)"] {
		t.Fatalf("缺预期 note：%+v", res.Notes)
	}

	// 非 503 上传失败要计错误（每 op 在上传一步即短路返回）。
	bad := fakeServify(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"POST /api/v1/upload": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
	})
	res = runUploadKnowledge(bad.URL, "tok", profile)
	if res.Errors != 2 {
		t.Fatalf("上传失败应计错：%+v", res)
	}
}

func TestRunTicketsCountsCommentAndListFailures(t *testing.T) {
	// customer/建单成功、评论与列表失败：每 op 记 1 个成功样本、计 2 个错误。
	srv := fakeServify(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"POST /api/customers":          okJSON,
		"POST /api/tickets":            okJSON,
		"POST /api/tickets/*/comments": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
		"GET /api/tickets":             func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
	})
	res := runTickets(srv.URL, "tok", loadProfile{ticketsOps: 4, concurrency: 2})
	if res.Errors != 8 || res.Ops != 4 {
		t.Fatalf("评论/列表失败应逐次计错、仅建单计入样本：%+v", res)
	}
}

func TestRunUploadKnowledgeUploadAPIFailureCountsError(t *testing.T) {
	// 文件上传成功、知识上传非 503 失败：逐次计错且不短路。
	srv := fakeServify(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"POST /api/v1/upload":              func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) },
		"POST /api/v1/ai/knowledge/upload": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
	})
	res := runUploadKnowledge(srv.URL, "tok", loadProfile{uploadOps: 3, concurrency: 3})
	if res.Errors != 3 {
		t.Fatalf("知识上传非 503 失败应计错：%+v", res)
	}
}

func TestRunAllScenarioRouting(t *testing.T) {
	srv := fakeServify(t, nil)
	profile := loadProfile{ticketsOps: 2, aiOps: 1, uploadOps: 1, wsTarget: 1, wsBatch: 1, concurrency: 1}
	if got := len(runAll(srv.URL, "tok", profile, "unknown")); got != 0 {
		t.Fatalf("未知场景应为空：%d", got)
	}
	for _, scenario := range []string{"tickets", "ai", "upload"} {
		if res := runAll(srv.URL, "tok", profile, scenario); len(res) != 1 {
			t.Fatalf("场景 %s 应只产一条结果：%d", scenario, len(res))
		}
	}
	if all := runAll(srv.URL, "tok", profile, "all"); len(all) != 4 {
		t.Fatalf("all 应产四条：%d", len(all))
	}
}

// ---- uploadFile / buildUploadBody ----

func TestUploadFileHappyAndErrors(t *testing.T) {
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(200)
	}))
	defer srv.Close()
	if err := uploadFile(srv.Client(), srv.URL, "tok"); err != nil {
		t.Fatalf("上传应成功：%v", err)
	}
	if !strings.HasPrefix(gotCT, "multipart/form-data; boundary=") {
		t.Fatalf("Content-Type 应带真实 boundary：%q", gotCT)
	}

	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv500.Close()
	if err := uploadFile(srv500.Client(), srv500.URL, ""); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("非 2xx 应报错：%v", err)
	}

	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closed.Close()
	if err := uploadFile(http.DefaultClient, closed.URL, ""); err == nil {
		t.Fatal("不可达应报传输错误")
	}
}

// failAfterWriter 前 n 字节成功，之后每次 Write 报错。
type failAfterWriter struct{ n int }

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if len(p) > w.n {
		used := w.n
		w.n = 0
		return used, errors.New("writer exhausted")
	}
	w.n -= len(p)
	return len(p), nil
}

// failAtCloseWriter 一切写入成功，直到写入 multipart 收尾 boundary（以
// "\r\n--" 开头）时才报错——对应 mw.Close 的错误分支（首 part 前导无 \r\n，
// 不会误触）。
type failAtCloseWriter struct{ buf bytes.Buffer }

func (w *failAtCloseWriter) Write(p []byte) (int, error) {
	if bytes.HasPrefix(p, []byte("\r\n--")) {
		return 0, errors.New("close rejected")
	}
	return w.buf.Write(p)
}

func TestBuildUploadBodyWriterErrors(t *testing.T) {
	// 建表单文件即失败（底层 writer 一开始就拒绝）。
	if _, err := buildUploadBody(&failAfterWriter{n: 0}, "a.txt", "payload"); err == nil {
		t.Fatal("CreateFormFile 错误分支应上抛")
	}
	// 表单头写成功、写 part 时失败。
	if _, err := buildUploadBody(&failAfterWriter{n: 4096}, "a.txt", strings.Repeat("p", 8192)); err == nil {
		t.Fatal("part.Write 错误分支应上抛")
	}
	// 全部内容写成功、Close 收尾失败。
	if _, err := buildUploadBody(&failAtCloseWriter{}, "a.txt", "payload"); err == nil {
		t.Fatal("mw.Close 错误分支应上抛")
	}
	// happy：返回的 Content-Type 与写出的体一致可解析。
	buf := &bytes.Buffer{}
	ct, err := buildUploadBody(buf, "a.txt", "payload")
	if err != nil || !strings.HasPrefix(ct, "multipart/form-data; boundary=") {
		t.Fatalf("happy 应返回 Content-Type：%q %v", ct, err)
	}
	if !strings.Contains(buf.String(), "payload") {
		t.Fatal("体应含 payload")
	}
}

func TestUploadFileDefensiveBranches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	// multipart 构造失败（seam）。
	origBuild := buildUploadBody
	buildUploadBody = func(io.Writer, string, string) (string, error) { return "", errors.New("build boom") }
	if err := uploadFile(srv.Client(), srv.URL, ""); err == nil || !strings.Contains(err.Error(), "build boom") {
		buildUploadBody = origBuild
		t.Fatalf("构造失败应上抛：%v", err)
	}
	buildUploadBody = origBuild

	// 请求构造失败（非法 URL）。
	if err := uploadFile(http.DefaultClient, "http://127.0.0.1\x00bad", ""); err == nil {
		t.Fatal("非法 URL 应上抛")
	}
}

// ---- WS 编解码与连接场景 ----

// fakeWSServer 手写最小 WS 对端：完成升级握手、读 masked 帧、按 echo=true
// 回发同 opcode 帧；frames 记录收到的 payload 供断言。
type fakeWSServer struct {
	listener net.Listener
	frames   chan []byte
	closed   atomic.Bool
}

func newFakeWS(t *testing.T, echo bool) *fakeWSServer {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	fws := &fakeWSServer{listener: l, frames: make(chan []byte, 64)}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go fws.serve(conn, echo)
		}
	}()
	t.Cleanup(func() { fws.close() })
	return fws
}

func (s *fakeWSServer) serve(conn net.Conn, echo bool) {
	defer conn.Close()
	buf := make([]byte, 0, 1024)
	b := make([]byte, 1)
	for !bytes.HasSuffix(buf, []byte("\r\n\r\n")) {
		if _, err := conn.Read(b); err != nil {
			return
		}
		buf = append(buf, b[0])
	}
	// stats 对账请求按常规 HTTP 回 JSON：不能回 101——Go http 客户端对
	// 101 响应的 body 读取不受 Client.Timeout 保护，会永久阻塞。
	if strings.Contains(string(buf), "/stats") {
		body := `{"data":{"connected_clients":3}}`
		_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
		return
	}
	_, _ = conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: fake\r\n\r\n"))
	for {
		payload, err := readServerFrame(conn)
		if err != nil {
			return
		}
		select {
		case s.frames <- payload:
		default:
		}
		if !echo {
			continue
		}
		if err := writeServerFrame(conn, 0x1, payload); err != nil {
			return
		}
	}
}

func (s *fakeWSServer) close() {
	if s.closed.CompareAndSwap(false, true) {
		_ = s.listener.Close()
	}
}

// readServerFrame 读一条客户端帧（必 masked），返回解 mask 后的 payload。
func readServerFrame(conn net.Conn) ([]byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	length := int(header[1] & 0x7F)
	var extTotal int
	switch length {
	case 126:
		extTotal = 2
	case 127:
		extTotal = 8
	}
	if extTotal > 0 {
		ext := make([]byte, extTotal)
		if _, err := io.ReadFull(conn, ext); err != nil {
			return nil, err
		}
		length = 0
		for _, x := range ext {
			length = length<<8 | int(x)
		}
	}
	mask := make([]byte, 4)
	if _, err := io.ReadFull(conn, mask); err != nil {
		return nil, err
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return payload, nil
}

// writeServerFrame 写一条服务端帧（不 masked），覆盖 <126 / 126 / 127 三档长度。
func writeServerFrame(conn net.Conn, opcode byte, payload []byte) error {
	header := []byte{0x80 | opcode}
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, byte(length))
	case length <= 0xFFFF:
		header = append(header, 126, byte(length>>8), byte(length))
	default:
		header = append(header, 127)
		for shift := 56; shift >= 0; shift -= 8 {
			header = append(header, byte(length>>shift))
		}
	}
	_, err := conn.Write(append(header, payload...))
	return err
}

func TestDialAndRoundtripSmallFrame(t *testing.T) {
	fws := newFakeWS(t, true)
	conn, err := dialWS(fmt.Sprintf("ws://%s/api/v1/ws", fws.listener.Addr()))
	if err != nil {
		t.Fatalf("握手应成功：%v", err)
	}
	defer conn.close()
	if err := conn.writeText([]byte("hello")); err != nil {
		t.Fatalf("写帧失败：%v", err)
	}
	payload, err := conn.readFrame()
	if err != nil || string(payload) != "hello" {
		t.Fatalf("往返应回显：payload=%q err=%v", payload, err)
	}
	select {
	case got := <-fws.frames:
		if string(got) != "hello" {
			t.Fatalf("服务端收到的 payload 不符：%q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("服务端未收到帧")
	}
}

func TestWriteFrameExtendedLengths(t *testing.T) {
	fws := newFakeWS(t, false) // 不回显，只收。
	conn, err := dialWS(fmt.Sprintf("ws://%s/x", fws.listener.Addr()))
	if err != nil {
		t.Fatalf("握手失败：%v", err)
	}
	defer conn.close()

	// 126：126..65535 字节走 16 位扩展长度。
	medium := bytes.Repeat([]byte("m"), 300)
	if err := conn.writeText(medium); err != nil {
		t.Fatalf("medium 帧失败：%v", err)
	}
	// 127：>65535 字节走 64 位扩展长度。
	large := bytes.Repeat([]byte("L"), 65543)
	if err := conn.writeText(large); err != nil {
		t.Fatalf("large 帧失败：%v", err)
	}
	select {
	case got := <-fws.frames:
		if !bytes.Equal(got, medium) {
			t.Fatalf("medium payload 不符：%d 字节", len(got))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("服务端未收到 medium 帧")
	}
	select {
	case got := <-fws.frames:
		if !bytes.Equal(got, large) {
			t.Fatalf("large payload 不符：%d 字节", len(got))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("服务端未收到 large 帧")
	}
}

func TestReadFrameExtendedLengthsFromServer(t *testing.T) {
	// 服务端按 126/127 扩展长度回帧，客户端 readFrame 要能解出。
	l := startRawListener(t, func(conn net.Conn) {
		defer conn.Close()
		buf := make([]byte, 0, 1024)
		b := make([]byte, 1)
		for !bytes.HasSuffix(buf, []byte("\r\n\r\n")) {
			if _, err := conn.Read(b); err != nil {
				return
			}
			buf = append(buf, b[0])
		}
		_, _ = conn.Write([]byte("HTTP/1.1 101 S\r\n\r\n"))
		_ = writeServerFrame(conn, 0x1, bytes.Repeat([]byte("e"), 200))
		_ = writeServerFrame(conn, 0x1, bytes.Repeat([]byte("g"), 70000))
		time.Sleep(500 * time.Millisecond) // 留出客户端读完的时间窗。
	})

	conn, err := dialWS(fmt.Sprintf("ws://%s/x", l.Addr()))
	if err != nil {
		t.Fatalf("握手失败：%v", err)
	}
	defer conn.close()
	got, err := conn.readFrame()
	if err != nil || len(got) != 200 {
		t.Fatalf("126 扩展长度帧解析不符：len=%d err=%v", len(got), err)
	}
	got, err = conn.readFrame()
	if err != nil || len(got) != 70000 {
		t.Fatalf("127 扩展长度帧解析不符：len=%d err=%v", len(got), err)
	}
}

func TestDialWSErrorPaths(t *testing.T) {
	// 目标端口不可达。
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := closed.Addr().String()
	_ = closed.Close()
	if _, err := dialWS("ws://" + addr + "/x"); err == nil {
		t.Fatal("不可达应报拨号错误")
	}

	// 握手响应非 101。
	l := startRawListener(t, func(conn net.Conn) {
		_, _ = conn.Write([]byte("HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n"))
	})
	if _, err := dialWS(fmt.Sprintf("ws://%s/x", l.Addr())); err == nil || !strings.Contains(err.Error(), "unexpected upgrade status") {
		t.Fatalf("非 101 应报状态错误：%v", err)
	}

	// 握手响应超长（>8192 无空行）。
	l2 := startRawListener(t, func(conn net.Conn) {
		_, _ = conn.Write(bytes.Repeat([]byte("x"), 9000))
	})
	if _, err := dialWS(fmt.Sprintf("ws://%s/x", l2.Addr())); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("超长握手响应应报错：%v", err)
	}

	// 握手响应连接即断（读错误）。
	l3 := startRawListener(t, func(conn net.Conn) { conn.Close() })
	if _, err := dialWS(fmt.Sprintf("ws://%s/x", l3.Addr())); err == nil || !strings.Contains(err.Error(), "read upgrade response") {
		t.Fatalf("连接即断应报读错误：%v", err)
	}
}

func TestDialWSRandomFailureClosesConn(t *testing.T) {
	l := startRawListener(t, func(conn net.Conn) {
		_, _ = conn.Write([]byte("HTTP/1.1 101 S\r\n\r\n"))
		time.Sleep(200 * time.Millisecond)
	})
	orig := cryptoRead
	cryptoRead = func(b []byte) (int, error) { return 0, errors.New("no entropy") }
	t.Cleanup(func() { cryptoRead = orig })
	if _, err := dialWS(fmt.Sprintf("ws://%s/x", l.Addr())); err == nil || !strings.Contains(err.Error(), "no entropy") {
		t.Fatalf("随机源失败应上抛：%v", err)
	}
}

func TestDialWSPathlessURLAndRequestWriteFailure(t *testing.T) {
	fws := newFakeWS(t, true)
	// 无路径 URL：hostPort 后无 "/"，走缺省 path="/" 握手。
	conn, err := dialWS(fmt.Sprintf("ws://%s", fws.listener.Addr()))
	if err != nil {
		t.Fatalf("无路径握手应成功：%v", err)
	}
	defer conn.close()

	// tcpDial seam 返回已关闭的管道连接 → 握手请求写失败分支。
	orig := tcpDial
	tcpDial = func(string, string, time.Duration) (net.Conn, error) {
		pc, pe := net.Pipe()
		_ = pe.Close()
		_ = pc.Close()
		return pc, nil
	}
	t.Cleanup(func() { tcpDial = orig })
	if _, err := dialWS("ws://127.0.0.1:1/x"); err == nil {
		t.Fatal("写握手请求失败应上抛")
	}
}

func TestWriteFrameOnClosedConn(t *testing.T) {
	pc, pe := net.Pipe()
	_ = pe.Close()
	_ = pc.Close()
	c := &wsConn{conn: pc, rand: rand.New(rand.NewSource(1))}
	if err := c.writeText([]byte("x")); err == nil {
		t.Fatal("已关闭连接写帧应报错")
	}
}

func TestReadFramePartialInputErrors(t *testing.T) {
	cases := []struct {
		name   string
		script func(pe net.Conn)
	}{
		{"帧头即断", func(pe net.Conn) { _ = pe.Close() }},
		{"126 扩展长度即断", func(pe net.Conn) {
			_, _ = pe.Write([]byte{0x82, 126})
			_ = pe.Close()
		}},
		{"127 扩展长度即断", func(pe net.Conn) {
			_, _ = pe.Write([]byte{0x82, 127})
			_ = pe.Close()
		}},
		{"payload 截断", func(pe net.Conn) {
			_, _ = pe.Write([]byte{0x82, 126, 0x00, 0x64, 1, 2, 3})
			_ = pe.Close()
		}},
	}
	for _, tc := range cases {
		pc, pe := net.Pipe()
		go tc.script(pe)
		c := &wsConn{conn: pc, rand: rand.New(rand.NewSource(1))}
		if _, err := c.readFrame(); err == nil {
			_ = pc.Close()
			t.Fatalf("%s: 应返回读错误", tc.name)
		}
		_ = pc.Close()
	}
}

// startRawListener 起一个把连接交给 fn 处理的裸 TCP listener。
func startRawListener(t *testing.T, fn func(net.Conn)) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go fn(conn)
		}
	}()
	return l
}

func TestRunWSConnectionsPlateauAndStats(t *testing.T) {
	// 平原 HTTP 服务当 WS 目标：握手全失败 → plateau + first_dial_error。
	plain := fakeServify(t, nil)
	profile := loadProfile{wsTarget: 4, wsBatch: 2, concurrency: 1}
	res := runWSConnections(plain.URL, "tok", profile)
	if res.WSMaxConns != 0 || res.Errors != 4 {
		t.Fatalf("握手全失败应 0 连接满错误：%+v", res)
	}
	joined := strings.Join(res.Notes, ";")
	if !strings.Contains(joined, "first_dial_error") || !strings.Contains(joined, "connection plateau") {
		t.Fatalf("应留 plateau 与拨号错误 note：%+v", res.Notes)
	}

	// wsServerReported 对账：fake stats 端点报 2 个连接，到位即提前返回。
	statsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"connected_clients":2}}`))
	}))
	defer statsSrv.Close()
	reported, err := wsServerReported(statsSrv.URL, "tok", 2)
	if err != nil || reported != 2 {
		t.Fatalf("对账应提前返回 2：%d %v", reported, err)
	}
	// minExpected=0 → 跑满轮询取峰值。
	reported, err = wsServerReported(statsSrv.URL, "", 0)
	if err != nil || reported != 2 {
		t.Fatalf("无下限轮询应取峰值 2：%d %v", reported, err)
	}

	// 错误路径：构造非法 URL / 端点关闭 / 响应非 JSON。
	if _, err := wsServerReported("http://127.0.0.1\x00bad", "", 0); err == nil {
		t.Fatal("非法 URL 应报构造错误")
	}
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closed.Close()
	if _, err := wsServerReported(closed.URL, "", 0); err == nil {
		t.Fatal("不可达应报传输错误")
	}
	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer badJSON.Close()
	if _, err := wsServerReported(badJSON.URL, "", 0); err == nil {
		t.Fatal("非 JSON 应报解码错误")
	}
}

func TestRunWSConnectionsHappyPath(t *testing.T) {
	fws := newFakeWS(t, true)
	wsBase := "http://" + fws.listener.Addr().String()

	// 3 目标一批全建连；对账由 fakeWSServer 的 /stats 分流回 3 个连接，
	// 提前退出（minExpected=3 即 maxConns）。
	profile := loadProfile{wsTarget: 3, wsBatch: 3, concurrency: 1}
	res := runWSConnections(wsBase, "tok", profile)
	if res.WSMaxConns != 3 || res.Ops != 3 || res.Errors != 0 {
		t.Fatalf("3 目标应全建连：%+v", res)
	}
	if res.WSTarget != 3 {
		t.Fatalf("WSTarget 应回填：%+v", res)
	}
	if res.P50MS < 0 || res.WSRoundtripMS < 0 {
		t.Fatalf("happy 场景指标不符：%+v", res)
	}
	if res.WSServerReported != 3 {
		t.Fatalf("对账应报 3 个连接：%+v", res)
	}
}

func TestRunWSConnectionsRoundtripFailuresAreSkipped(t *testing.T) {
	// 预置连接 1：写即失败（已关闭管道）；连接 2：写成功但回读只有半个帧头。
	deadPc, deadPe := net.Pipe()
	_ = deadPe.Close()
	_ = deadPc.Close()

	halfRd, halfWr := net.Pipe()
	go func() {
		header := make([]byte, 6) // 2 字节帧头 + 4 字节 mask
		if _, err := io.ReadFull(halfWr, header); err != nil {
			_ = halfWr.Close()
			return
		}
		length := int(header[1] & 0x7F)
		if _, err := io.ReadFull(halfWr, make([]byte, length)); err != nil {
			_ = halfWr.Close()
			return
		}
		_, _ = halfWr.Write([]byte{0x82}) // 只回半个帧头即断。
		_ = halfWr.Close()
	}()
	conns := []*wsConn{
		{conn: deadPc, rand: rand.New(rand.NewSource(1))},
		{conn: halfRd, rand: rand.New(rand.NewSource(2))},
	}

	idx := atomic.Int64{}
	orig := dialWSFn
	dialWSFn = func(string) (*wsConn, error) {
		i := int(idx.Add(1)) - 1 // 阶梯建连批内并发，需原子发号。
		if i >= len(conns) {
			return nil, errors.New("no more scripted conns")
		}
		return conns[i], nil
	}
	t.Cleanup(func() { dialWSFn = orig })

	stats := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"connected_clients":2}}`))
	}))
	defer stats.Close()

	res := runWSConnections(stats.URL, "tok", loadProfile{wsTarget: 2, wsBatch: 2, concurrency: 1})
	if res.WSMaxConns != 2 || res.Ops != 2 || res.Errors != 0 {
		t.Fatalf("两条预置连接都应计入建连数：%+v", res)
	}
	if res.WSServerReported != 2 {
		t.Fatalf("对账应为 2：%+v", res)
	}
	if res.WSRoundtripMS != 0 {
		t.Fatalf("往返全部失败不应有 roundtrip 样本：%+v", res)
	}
}

// ---- 压测配置生成 ----

const sampleServifyConfig = `# 顶部注释应保留
security:
  rate_limiting:
    enabled: false
    requests_per_minute: 60
upload:
  provider: s3
ai:
  openai:
    api_key: "${OPENAI_API_KEY}"
knowledge:
  provider: pgvector
`

func TestGenLoadTestConfigHappy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "config.yml")
	dst := filepath.Join(dir, "out.yml")
	if err := os.WriteFile(src, []byte(sampleServifyConfig), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := genLoadTestConfig(src, dst, "http://mock-llm:9999"); err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	out, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	text := string(out)
	for _, want := range []string{
		"requests_per_minute: 100000",
		"burst: 10000",
		"prefix: /api/",
		"provider: local",
		"base_url: http://mock-llm:9999/v1",
		// 源值 "${OPENAI_API_KEY}" 是双引号风格；mapSet 只改 Value/Tag，
		// 序列化保留节点原 Style。
		`api_key: "perf-not-a-real-key"`,
		"provider: \"\"",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("输出缺 %q：\n%s", want, text)
		}
	}
	if strings.Contains(text, "provider: s3") || strings.Contains(text, "provider: pgvector") {
		t.Fatalf("旧值应被覆盖：\n%s", text)
	}
	// yaml.Node API 改写保留源注释。
	if !strings.Contains(text, "# 顶部注释应保留") {
		t.Fatalf("注释应保留：\n%s", text)
	}

	// mockURL 为空：ai 段不被改写。
	dst2 := filepath.Join(dir, "out2.yml")
	if err := genLoadTestConfig(src, dst2, ""); err != nil {
		t.Fatalf("生成失败：%v", err)
	}
	out2, _ := os.ReadFile(dst2)
	if strings.Contains(string(out2), "perf-not-a-real-key") {
		t.Fatalf("无 mockURL 不应改写 ai 段：\n%s", out2)
	}
}

func TestGenLoadTestConfigErrors(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(src, []byte("security: [broken"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := genLoadTestConfig(src, filepath.Join(dir, "out.yml"), ""); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("非法 yaml 应报解析错误：%v", err)
	}
	if err := os.WriteFile(src, []byte("- a\n- b\n"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := genLoadTestConfig(src, filepath.Join(dir, "out.yml"), ""); err == nil || !strings.Contains(err.Error(), "top-level mapping expected") {
		t.Fatalf("顶层非 mapping 应报错：%v", err)
	}
	if err := os.WriteFile(src, []byte(""), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := genLoadTestConfig(src, filepath.Join(dir, "out.yml"), ""); err == nil || !strings.Contains(err.Error(), "top-level mapping expected") {
		t.Fatalf("空文档应报错：%v", err)
	}
	if err := os.WriteFile(src, []byte(sampleServifyConfig), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := genLoadTestConfig(src, filepath.Join(dir, "missing-dir", "out.yml"), ""); err == nil {
		t.Fatal("不可写目标应报错")
	}
	if err := genLoadTestConfig(filepath.Join(dir, "nope.yml"), filepath.Join(dir, "out.yml"), ""); err == nil {
		t.Fatal("源不存在应报错")
	}
}

func TestGenLoadTestConfigMarshalError(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(src, []byte(sampleServifyConfig), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	// 文本可达的循环锚点在解码时即被解环，Marshal 对改写后的 Node 树实际
	// 不会失败——以 seam 换失败实现覆盖防御分支。
	orig := marshalYAML
	marshalYAML = func(*yaml.Node) ([]byte, error) { return nil, errors.New("yaml boom") }
	t.Cleanup(func() { marshalYAML = orig })
	err := genLoadTestConfig(src, filepath.Join(dir, "out.yml"), "")
	if err == nil || !strings.Contains(err.Error(), "marshal: yaml boom") {
		t.Fatalf("序列化失败应上抛：%v", err)
	}
}

func TestMapHelpers(t *testing.T) {
	if mapGet(nil, "k") != nil {
		t.Fatal("nil parent 应返回 nil")
	}
	if mapGet(&yaml.Node{Kind: yaml.ScalarNode}, "k") != nil {
		t.Fatal("非 mapping parent 应返回 nil")
	}

	parent := &yaml.Node{Kind: yaml.MappingNode}
	child := mapGetOrCreate(parent, "absent")
	if child == nil || child.Kind != yaml.MappingNode {
		t.Fatal("缺席键应建 mapping 并返回")
	}
	if len(parent.Content) != 2 {
		t.Fatalf("应追加 key+value 两节点：%d", len(parent.Content))
	}
	if again := mapGetOrCreate(parent, "absent"); again != child {
		t.Fatal("已有键应原样返回")
	}

	mapSet(parent, "k2", "v2", "!!str")
	if got := mapGet(parent, "k2"); got == nil || got.Value != "v2" || got.Tag != "!!str" {
		t.Fatalf("新键应追加：%+v", got)
	}
	mapSet(parent, "k2", "v2b", "!!int")
	if got := mapGet(parent, "k2"); got == nil || got.Value != "v2b" || got.Tag != "!!int" {
		t.Fatalf("已有键应原位更新：%+v", got)
	}

	replacement := &yaml.Node{Kind: yaml.SequenceNode}
	mapSetNode(parent, "k2", replacement)
	if mapGet(parent, "k2") != replacement {
		t.Fatal("mapSetNode 应原位替换")
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode}
	mapSetNode(parent, "k3", seq)
	if mapGet(parent, "k3") != seq {
		t.Fatal("mapSetNode 应追加新键")
	}
}

// ---- main 与退出路径（子进程 re-exec，先例：cmd/gen-baseline） ----

func TestPerfbenchMainSubprocess(t *testing.T) {
	if os.Getenv("PERFBENCH_SUBPROCESS") != "1" {
		return
	}
	switch os.Getenv("PERFBENCH_CASE") {
	case "fatal-marshal":
		orig := marshalResults
		marshalResults = func([]Result) ([]byte, error) { return nil, errors.New("marshal boom") }
		t.Cleanup(func() { marshalResults = orig })
	}
	args := strings.Split(os.Getenv("PERFBENCH_ARGS"), "\x1f")
	if len(args) == 1 && args[0] == "" {
		args = nil
	}
	os.Args = append([]string{"perfbench.test"}, args...)
	main()
	// 与 gen-baseline 先例一致：正常返回路径显式退出，触发子进程覆盖率
	// 计数落盘（fatal 路径经 os.Exit(1) 自带 flush）。
	os.Exit(0)
}

func runMainSubprocess(t *testing.T, args []string, extraEnv ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestPerfbenchMainSubprocess$")
	cmd.Env = append(os.Environ(),
		"PERFBENCH_SUBPROCESS=1",
		"PERFBENCH_ARGS="+strings.Join(args, "\x1f"),
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestMainHappyPathWritesResults(t *testing.T) {
	srv := fakeServify(t, ticketsBehavior())
	outFile := filepath.Join(t.TempDir(), "results.json")
	out, err := runMainSubprocess(t, []string{
		"-base", srv.URL, "-token", "tok", "-scale", "smoke",
		"-scenario", "tickets", "-out", outFile,
	})
	if err != nil {
		t.Fatalf("happy 子进程应干净退出：%v\n%s", err, out)
	}
	if !strings.Contains(out, `"scenario": "tickets-mixed"`) {
		t.Fatalf("stdout 应输出 JSON 结果：%s", out)
	}
	data, readErr := os.ReadFile(outFile)
	if readErr != nil || !strings.Contains(string(data), "tickets-mixed") {
		t.Fatalf("-out 文件应写入结果：%v %s", readErr, data)
	}
}

func TestMainGenConfigPath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "config.yml")
	dst := filepath.Join(dir, "out.yml")
	if err := os.WriteFile(src, []byte(sampleServifyConfig), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	out, err := runMainSubprocess(t, []string{
		"-gen-config-src", src, "-gen-config-dst", dst, "-mock-url", "http://m:1",
	})
	if err != nil {
		t.Fatalf("gen-config 子进程应干净退出：%v\n%s", err, out)
	}
	if !strings.Contains(out, "load-test config written") {
		t.Fatalf("应输出生成信息：%s", out)
	}
	if _, statErr := os.Stat(dst); statErr != nil {
		t.Fatalf("目标配置应已生成：%v", statErr)
	}
}

func TestMainFatalPaths(t *testing.T) {
	out, err := runMainSubprocess(t, []string{"-gen-config-src", "x"})
	if err == nil || !strings.Contains(out, "-gen-config-dst is required") {
		t.Fatalf("缺 dst 应 fatal：%v %s", err, out)
	}

	out, err = runMainSubprocess(t, []string{"-gen-config-src", "/nonexistent/perfbench.yml", "-gen-config-dst", filepath.Join(t.TempDir(), "o.yml")})
	if err == nil || !strings.Contains(out, "gen config:") {
		t.Fatalf("生成失败应 fatal：%v %s", err, out)
	}

	srv := fakeServify(t, ticketsBehavior())
	out, err = runMainSubprocess(t, []string{
		"-base", srv.URL, "-scenario", "tickets", "-out", "/nonexistent-dir/results.json",
	})
	if err == nil || !strings.Contains(out, "write results") {
		t.Fatalf("结果落盘失败应 fatal：%v %s", err, out)
	}

	// customer 建成功、后续全 500：ops 层错误会计入 → 非 "-capacity" 场景退出码 1。
	// （若 customer 也 500，runTickets 会 fail-soft 短路为 note、errors=0、退出码 0。）
	badSrv := fakeServify(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"POST /api/customers":          okJSON,
		"POST /api/tickets":            func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
		"POST /api/tickets/*/comments": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
		"GET /api/tickets":             func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
	})
	out, err = runMainSubprocess(t, []string{"-base", badSrv.URL, "-scenario", "tickets"})
	if err == nil {
		t.Fatalf("非容量场景有错应以非零退出：%s", out)
	}
}

func TestMainFatalMarshalSeam(t *testing.T) {
	out, err := runMainSubprocess(t, []string{"-scenario", "tickets"}, "PERFBENCH_CASE=fatal-marshal")
	if err == nil || !strings.Contains(out, "marshal results: marshal boom") {
		t.Fatalf("marshal seam 失败应 fatal：%v %s", err, out)
	}
}
