// perfbench 是 servify 的性能压测引擎（P2-8）：对四个容量维度做受控并发
// 负载并输出 JSON 基线数据——ticket/conversation 高并发读写、AI 查询延迟、
// 文件上传与知识同步、WebSocket 连接数。零三方依赖：WS 客户端用标准库
// 手写握手与帧编解码。
//
// 用法：
//
//	go run ./scripts/perfbench -base http://127.0.0.1:18107 -token <jwt> \
//	  -scale smoke -out scripts/test-results/perf-baseline -scenario all
//
// scale 决定各场景的负载规模（smoke 供 CI 驱动测试秒级跑完，full 用于
// 出容量基线）；scenario 可单独跑某一维度便于排查。
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

// Result 是单个场景的基线输出。
type Result struct {
	Scenario    string `json:"scenario"`
	Scale       string `json:"scale"`
	Ops         int    `json:"ops"`
	Concurrency int    `json:"concurrency"`
	Errors      int    `json:"errors"`
	// 延迟毫秒分位数（WS 场景为握手延迟；roundtrip_ms 为消息往返）。
	P50MS     float64 `json:"p50_ms"`
	P95MS     float64 `json:"p95_ms"`
	P99MS     float64 `json:"p99_ms"`
	MaxMS     float64 `json:"max_ms"`
	OpsPerSec float64 `json:"ops_per_sec"`
	// WS 场景专用：最大成功并发连接与对账到的服务端 connected_clients。
	WSMaxConns       int     `json:"ws_max_conns,omitempty"`
	WSServerReported int     `json:"ws_server_reported,omitempty"`
	WSRoundtripMS    float64 `json:"ws_roundtrip_ms,omitempty"`
	WSTarget         int     `json:"ws_target,omitempty"`
	// 瀑布失败原因（fail-soft：容量场景允许"到顶"而非报错）。
	Notes []string `json:"notes,omitempty"`
}

type loadProfile struct {
	ticketsOps  int
	aiOps       int
	uploadOps   int
	wsTarget    int
	concurrency int
	wsBatch     int
}

func profileFor(scale string) loadProfile {
	switch scale {
	case "full":
		return loadProfile{ticketsOps: 600, aiOps: 200, uploadOps: 100, wsTarget: 200, concurrency: 16, wsBatch: 25}
	default: // smoke：CI 驱动测试秒级跑完
		return loadProfile{ticketsOps: 24, aiOps: 8, uploadOps: 6, wsTarget: 20, concurrency: 4, wsBatch: 10}
	}
}

func main() {
	base := flag.String("base", "http://127.0.0.1:18107", "servify base URL")
	token := flag.String("token", "", "admin JWT")
	scale := flag.String("scale", "smoke", "load scale: smoke | full")
	out := flag.String("out", "", "write combined JSON results to this file")
	scenario := flag.String("scenario", "all", "all | tickets | ai | upload | ws")
	genConfigSrc := flag.String("gen-config-src", "", "generate a load-test config from this source config and exit")
	genConfigDst := flag.String("gen-config-dst", "", "destination path for -gen-config-src")
	mockURL := flag.String("mock-url", "", "mock LLM base URL written into the generated config")
	flag.Parse()

	if *genConfigSrc != "" {
		if *genConfigDst == "" {
			fatal("-gen-config-dst is required with -gen-config-src")
		}
		if err := genLoadTestConfig(*genConfigSrc, *genConfigDst, *mockURL); err != nil {
			fatal("gen config: %v", err)
		}
		fmt.Printf("load-test config written: %s\n", *genConfigDst)
		return
	}

	profile := profileFor(*scale)
	results := runAll(*base, *token, profile, *scenario)
	for i := range results {
		results[i].Scale = *scale
	}

	encoded, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fatal("marshal results: %v", err)
	}
	fmt.Println(string(encoded))
	if *out != "" {
		if writeErr := os.WriteFile(*out, encoded, 0o644); writeErr != nil {
			fatal("write results: %v", writeErr)
		}
	}
	failed := false
	for _, r := range results {
		if r.Errors > 0 && !strings.HasSuffix(r.Scenario, "-capacity") {
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

func runAll(base, token string, profile loadProfile, scenario string) []Result {
	var results []Result
	if scenario == "all" || scenario == "tickets" {
		results = append(results, runTickets(base, token, profile))
	}
	if scenario == "all" || scenario == "ai" {
		results = append(results, runAIQuery(base, token, profile))
	}
	if scenario == "all" || scenario == "upload" {
		results = append(results, runUploadKnowledge(base, token, profile))
	}
	if scenario == "all" || scenario == "ws" {
		results = append(results, runWSConnections(base, token, profile))
	}
	return results
}

// ---- HTTP 负载器 ----

type latencyHistogram struct {
	mu      sync.Mutex
	latency []float64
	errors  int
}

func (h *latencyHistogram) record(ms float64) {
	h.mu.Lock()
	h.latency = append(h.latency, ms)
	h.mu.Unlock()
}

func (h *latencyHistogram) fail() {
	h.mu.Lock()
	h.errors++
	h.mu.Unlock()
}

func (h *latencyHistogram) snapshot(elapsed time.Duration, concurrency int) (Result, []float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sorted := append([]float64(nil), h.latency...)
	sort.Float64s(sorted)
	ops := len(sorted)
	res := Result{
		Ops:         ops,
		Concurrency: concurrency,
		Errors:      h.errors,
	}
	if ops > 0 {
		res.P50MS = percentile(sorted, 0.50)
		res.P95MS = percentile(sorted, 0.95)
		res.P99MS = percentile(sorted, 0.99)
		res.MaxMS = sorted[ops-1]
		if elapsed > 0 {
			res.OpsPerSec = float64(ops) / elapsed.Seconds()
		}
	}
	return res, sorted
}

func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(q * float64(len(sorted)-1))
	return sorted[idx]
}

// httpDo 执行一次带超时的请求并返回响应体与状态码（延迟由调用方测量）。
func httpDo(client *http.Client, method, url, token, contentType, body string) ([]byte, int, error) {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return data, resp.StatusCode, nil
	}
	return data, resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
}

// runWorkers 起 concurrency 个 worker，均摊 total 次操作。
func runWorkers(total, concurrency int, op func()) {
	var wg sync.WaitGroup
	per := total / concurrency
	rest := total % concurrency
	for i := 0; i < concurrency; i++ {
		n := per
		if i < rest {
			n++
		}
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < n; j++ {
				op()
			}
		}(n)
	}
	wg.Wait()
}

// ---- 场景：ticket/conversation 高并发读写 ----

func runTickets(base, token string, profile loadProfile) Result {
	client := &http.Client{Timeout: 30 * time.Second}
	// 基线前置：创建一个客户供工单挂靠（CreateTicketRequest 的 customer_id 必填）。
	now := time.Now().UnixNano()
	custBody := fmt.Sprintf(`{"username":"perf-cust-%d","email":"perf-%d@servify.io","name":"perf customer"}`, now, now)
	custResp, _, err := httpDo(client, "POST", base+"/api/customers", token, "application/json", custBody)
	if err != nil {
		return Result{Scenario: "tickets-mixed", Notes: []string{fmt.Sprintf("customer_setup_failed: %v", err)}}
	}
	customerID := extractID(custResp, "data")
	if customerID == "" {
		return Result{Scenario: "tickets-mixed", Notes: []string{"customer_setup_failed: no id in response"}}
	}

	hist := &latencyHistogram{}
	start := time.Now()
	runWorkers(profile.ticketsOps, profile.concurrency, func() {
		reqID := fmt.Sprintf("perf-%d-%d", time.Now().UnixNano(), rand.Intn(1<<30))
		// 写：创建工单。
		body := fmt.Sprintf(`{"title":"perf ticket %s","description":"P2-8 baseline load","priority":"low","customer_id":%s}`, reqID, customerID)
		t0 := time.Now()
		created, _, err := httpDo(client, "POST", base+"/api/tickets", token, "application/json", body)
		if err != nil {
			hist.fail()
			return
		}
		hist.record(float64(time.Since(t0).Microseconds()) / 1000.0)
		// 会话写：给新建工单发一条评论。
		if id := extractID(created, "data"); id != "" {
			comment := fmt.Sprintf(`{"content":"perf comment %s"}`, reqID)
			t1 := time.Now()
			if _, _, err := httpDo(client, "POST", base+"/api/tickets/"+id+"/comments", token, "application/json", comment); err != nil {
				hist.fail()
			} else {
				hist.record(float64(time.Since(t1).Microseconds()) / 1000.0)
			}
		}
		// 读：工单列表。
		t2 := time.Now()
		if _, _, err := httpDo(client, "GET", base+"/api/tickets?page=1&page_size=10", token, "", ""); err != nil {
			hist.fail()
		} else {
			hist.record(float64(time.Since(t2).Microseconds()) / 1000.0)
		}
	})
	res, _ := hist.snapshot(time.Since(start), profile.concurrency)
	res.Scenario = "tickets-mixed"
	return res
}

// extractID 从响应 JSON 里取 id：优先 wrapper["key"].id，其次顶层 id（兼容字符串与数字）。
func extractID(body []byte, wrapper string) string {
	var withWrapper map[string]json.RawMessage
	if json.Unmarshal(body, &withWrapper) == nil {
		if innerRaw, ok := withWrapper[wrapper]; ok {
			var inner struct {
				ID interface{} `json:"id"`
			}
			if json.Unmarshal(innerRaw, &inner) == nil {
				return idToString(inner.ID)
			}
		}
	}
	var flat struct {
		ID interface{} `json:"id"`
	}
	if json.Unmarshal(body, &flat) == nil {
		return idToString(flat.ID)
	}
	return ""
}

func idToString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%d", int64(t))
	default:
		return ""
	}
}

// ---- 场景：AI 查询延迟 ----

func runAIQuery(base, token string, profile loadProfile) Result {
	client := &http.Client{Timeout: 60 * time.Second}
	hist := &latencyHistogram{}
	start := time.Now()
	runWorkers(profile.aiOps, min(profile.concurrency, profile.aiOps), func() {
		body := fmt.Sprintf(`{"query":"perf ai query %d"}`, time.Now().UnixNano())
		t0 := time.Now()
		_, _, err := httpDo(client, "POST", base+"/api/v1/ai/query", token, "application/json", body)
		elapsed := time.Since(t0)
		if err != nil {
			hist.fail()
			return
		}
		hist.record(float64(elapsed.Microseconds()) / 1000.0)
	})
	res, _ := hist.snapshot(time.Since(start), min(profile.concurrency, profile.aiOps))
	res.Scenario = "ai-query"
	return res
}

// ---- 场景：文件上传与知识同步 ----

func runUploadKnowledge(base, token string, profile loadProfile) Result {
	client := &http.Client{Timeout: 60 * time.Second}
	hist := &latencyHistogram{}
	var kbUnavailable atomic.Bool
	start := time.Now()
	runWorkers(profile.uploadOps, min(profile.concurrency, profile.uploadOps), func() {
		// 文件上传：multipart 小文件（KB 级）。
		t0 := time.Now()
		err := uploadFile(client, base+"/api/v1/upload", token)
		if err != nil {
			hist.fail()
			return
		}
		hist.record(float64(time.Since(t0).Microseconds()) / 1000.0)
		// 知识文档上传（JSON API）。基线口径为无外部知识库 provider 的默认态，
		// "knowledge provider is not enabled"（503）是预期响应：延迟照测、
		// 不计错误，note 里说明该维度未启用。
		t1 := time.Now()
		doc := fmt.Sprintf(`{"title":"perf kb %d","content":"%s","tags":["perf"]}`,
			time.Now().UnixNano(), strings.Repeat("性能压测知识库文档内容。", 40))
		if _, status, err := httpDo(client, "POST", base+"/api/v1/ai/knowledge/upload", token, "application/json", doc); err != nil {
			if status == http.StatusServiceUnavailable {
				kbUnavailable.Store(true)
				hist.record(float64(time.Since(t1).Microseconds()) / 1000.0)
			} else {
				hist.fail()
			}
		} else {
			hist.record(float64(time.Since(t1).Microseconds()) / 1000.0)
		}
	})
	// 同步是一次聚合操作，不计入并发延迟，只作为场景收尾：基线环境通常无
	// 外部知识库 provider，"knowledge provider is not enabled" 是预期响应，
	// 不计入错误。
	res, _ := hist.snapshot(time.Since(start), min(profile.concurrency, profile.uploadOps))
	if kbUnavailable.Load() {
		res.Notes = append(res.Notes, "knowledge_upload=not-enabled (expected without external provider)")
	}
	if _, _, err := httpDo(client, "POST", base+"/api/v1/ai/knowledge/sync", token, "", ""); err == nil {
		res.Notes = append(res.Notes, "knowledge_sync=ok")
	} else {
		res.Notes = append(res.Notes, "knowledge_sync=not-enabled (expected without external provider)")
	}
	res.Scenario = "upload-knowledge"
	return res
}

func uploadFile(client *http.Client, url, token string) error {
	// KB 级小文件直接内存构造 multipart，避免流式 body 的半途关闭语义。
	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)
	name := fmt.Sprintf("perf-upload-%d.txt", time.Now().UnixNano())
	part, err := mw.CreateFormFile("file", name)
	if err != nil {
		return err
	}
	if _, err := part.Write([]byte(strings.Repeat("servify perf upload payload.\n", 200))); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	req, err := http.NewRequest("POST", url, buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("HTTP %d", resp.StatusCode)
}

// ---- 场景：WebSocket 连接数 ----

func runWSConnections(base, token string, profile loadProfile) Result {
	wsURL := wsURLFromBase(base)
	res := Result{Scenario: "ws-connections", WSTarget: profile.wsTarget}
	var handshakes []float64
	var roundtrips []float64
	var conns []*wsConn
	maxConns := 0
	serverReported := 0

	// 阶梯建连：每批 wsBatch 条，批内并发；某批失败率过半即认为到顶（fail-soft）。
	for established := 0; established < profile.wsTarget; established += profile.wsBatch {
		batch := min(profile.wsBatch, profile.wsTarget-established)
		var batchMu sync.Mutex
		var batchOK int
		var batchHandshakes []float64
		var batchConns []*wsConn
		var firstDialErr string
		var wg sync.WaitGroup
		for i := 0; i < batch; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				sessionID := fmt.Sprintf("perf-ws-%d-%d", time.Now().UnixNano(), n)
				t0 := time.Now()
				conn, err := dialWS(wsURL + "?session_id=" + sessionID)
				if err != nil {
					batchMu.Lock()
					if firstDialErr == "" {
						firstDialErr = err.Error()
					}
					batchMu.Unlock()
					return
				}
				batchMu.Lock()
				batchOK++
				batchHandshakes = append(batchHandshakes, float64(time.Since(t0).Microseconds())/1000.0)
				batchConns = append(batchConns, conn)
				batchMu.Unlock()
			}(i)
		}
		wg.Wait()
		handshakes = append(handshakes, batchHandshakes...)
		conns = append(conns, batchConns...)
		maxConns += batchOK
		if firstDialErr != "" {
			res.Notes = append(res.Notes, "first_dial_error: "+firstDialErr)
		}
		if batchOK*2 < batch {
			res.Notes = append(res.Notes, fmt.Sprintf("connection plateau at %d/%d conns", maxConns, profile.wsTarget))
			break
		}
	}

	// 每条存活连接发一条 text-message 并读回帧，测消息往返。
	for _, conn := range conns {
		t0 := time.Now()
		payload := fmt.Sprintf(`{"type":"text-message","data":{"content":"perf ping %d"}}`, time.Now().UnixNano())
		if err := conn.writeText([]byte(payload)); err != nil {
			continue
		}
		if _, err := conn.readFrame(); err != nil {
			continue
		}
		roundtrips = append(roundtrips, float64(time.Since(t0).Microseconds())/1000.0)
	}
	// 服务端对账：/api/v1/ws/stats 的 connected_clients。hub 对连接的登记在
	// 握手 101 之后异步完成（h.register 是 channel，hub 循环稍后消费），慢机
	// 上客户端判定建连时服务端 map 可能尚未收全（CI 上 20 连接读到 19）；
	// 登记 only 增、断连 only 减，峰值即建连数。到位即提前返回。
	if reported, err := wsServerReported(base, token, maxConns); err == nil {
		serverReported = reported
	}

	for _, conn := range conns {
		conn.close()
	}

	res.WSMaxConns = maxConns
	res.WSServerReported = serverReported
	res.Ops = maxConns
	res.Errors = profile.wsTarget - maxConns
	if len(handshakes) > 0 {
		sort.Float64s(handshakes)
		res.P50MS = percentile(handshakes, 0.50)
		res.P95MS = percentile(handshakes, 0.95)
		res.P99MS = percentile(handshakes, 0.99)
		res.MaxMS = handshakes[len(handshakes)-1]
	}
	if len(roundtrips) > 0 {
		sort.Float64s(roundtrips)
		res.WSRoundtripMS = percentile(roundtrips, 0.50)
	}
	return res
}

func wsURLFromBase(base string) string {
	s := strings.TrimPrefix(base, "http://")
	return "ws://" + s + "/api/v1/ws"
}

// wsServerReported 轮询 stats 端点取 connected_clients 峰值：到位（≥
// minExpected）提前返回，最多 10 次 × 200ms。
func wsServerReported(base, token string, minExpected int) (int, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	best := 0
	for attempt := 0; attempt < 10; attempt++ {
		if attempt > 0 {
			time.Sleep(200 * time.Millisecond)
		}
		req, err := http.NewRequest("GET", base+"/api/v1/ws/stats", nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return 0, err
		}
		var parsed struct {
			Data struct {
				ConnectedClients int `json:"connected_clients"`
			} `json:"data"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&parsed)
		resp.Body.Close()
		if decodeErr != nil {
			return 0, decodeErr
		}
		if parsed.Data.ConnectedClients > best {
			best = parsed.Data.ConnectedClients
		}
		if minExpected > 0 && best >= minExpected {
			break
		}
	}
	return best, nil
}

// ---- 最小 WS 客户端（标准库） ----

type wsConn struct {
	conn net.Conn
	rand *rand.Rand
}

func dialWS(rawURL string) (*wsConn, error) {
	// ws://host:port/path
	rest := strings.TrimPrefix(rawURL, "ws://")
	var hostPort, path string
	if idx := strings.Index(rest, "/"); idx >= 0 {
		hostPort, path = rest[:idx], rest[idx:]
	} else {
		hostPort, path = rest, "/"
	}
	conn, err := net.DialTimeout("tcp", hostPort, 15*time.Second)
	if err != nil {
		return nil, err
	}
	// RFC 6455/gorilla 要求 Sec-WebSocket-Key 解码后恰好 16 字节。
	var keyBytes [16]byte
	if _, err := rand.Read(keyBytes[:]); err != nil {
		conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes[:])
	request := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", path, hostPort, key)
	if _, err := conn.Write([]byte(request)); err != nil {
		conn.Close()
		return nil, err
	}
	if err := readUpgradeResponse(conn, key); err != nil {
		conn.Close()
		return nil, err
	}
	return &wsConn{conn: conn, rand: rand.New(rand.NewSource(time.Now().UnixNano()))}, nil
}

func readUpgradeResponse(conn net.Conn, key string) error {
	buf := make([]byte, 0, 1024)
	b := make([]byte, 1)
	for !strings.Contains(string(buf), "\r\n\r\n") {
		if _, err := conn.Read(b); err != nil {
			return fmt.Errorf("read upgrade response: %w", err)
		}
		buf = append(buf, b[0])
		if len(buf) > 8192 {
			return fmt.Errorf("upgrade response too large")
		}
	}
	head := string(buf)
	if !strings.Contains(head, "101") {
		return fmt.Errorf("unexpected upgrade status: %s", firstLine(head))
	}
	return nil
}

func firstLine(s string) string {
	if idx := strings.Index(s, "\r\n"); idx >= 0 {
		return s[:idx]
	}
	return s
}

// writeText 发送一个 masked text 帧。
func (c *wsConn) writeText(payload []byte) error {
	return c.writeFrame(0x1, payload)
}

func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	header := []byte{0x80 | opcode}
	length := len(payload)
	maskBit := byte(0x80)
	switch {
	case length < 126:
		header = append(header, maskBit|byte(length))
	case length <= 0xFFFF:
		header = append(header, maskBit|126, byte(length>>8), byte(length))
	default:
		header = append(header, maskBit|127)
		for shift := 56; shift >= 0; shift -= 8 {
			header = append(header, byte(length>>shift))
		}
	}
	var mask [4]byte
	_, _ = c.rand.Read(mask[:])
	header = append(header, mask[:]...)
	masked := make([]byte, length)
	for i, b := range payload {
		masked[i] = b ^ mask[i%4]
	}
	if _, err := c.conn.Write(append(header, masked...)); err != nil {
		return err
	}
	return nil
}

// readFrame 读一帧（服务端帧不 mask），返回 payload。
func (c *wsConn) readFrame() ([]byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return nil, err
	}
	length := int(header[1] & 0x7F)
	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(c.conn, ext); err != nil {
			return nil, err
		}
		length = int(ext[0])<<8 | int(ext[1])
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(c.conn, ext); err != nil {
			return nil, err
		}
		length = 0
		for _, b := range ext {
			length = length<<8 | int(b)
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (c *wsConn) close() {
	// 尽力发 close 帧（opcode 8），随后直接断开——压测不追求优雅收尾。
	_ = c.writeFrame(0x8, nil)
	_ = c.conn.Close()
}

// ---- 压测配置生成 ----

// genLoadTestConfig 从生产配置派生一份压测配置：放宽限流（压测流量远超默认
// 阈值，限流器不应成为被测对象）、upload 走 local、AI LLM 指向 mock LLM、
// knowledge provider 置空（基线口径=无外部知识库的默认态）。用 yaml Node API
// 修改以保留源配置的注释、键序与 ${ENV} 占位符。
func genLoadTestConfig(srcPath, dstPath, mockURL string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse %s: %w", srcPath, err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s: top-level mapping expected", srcPath)
	}
	doc := root.Content[0]

	rateLimiting := mapGetOrCreate(mapGetOrCreate(doc, "security"), "rate_limiting")
	mapSet(rateLimiting, "enabled", "true", "!!bool")
	mapSet(rateLimiting, "requests_per_minute", "100000", "!!int")
	mapSet(rateLimiting, "burst", "10000", "!!int")
	entry := &yaml.Node{Kind: yaml.MappingNode}
	mapSet(entry, "enabled", "true", "!!bool")
	mapSet(entry, "prefix", "/api/", "!!str")
	mapSet(entry, "requests_per_minute", "100000", "!!int")
	mapSet(entry, "burst", "10000", "!!int")
	paths := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{entry}}
	mapSetNode(rateLimiting, "paths", paths)

	upload := mapGetOrCreate(doc, "upload")
	mapSet(upload, "provider", "local", "!!str")

	if mockURL != "" {
		openai := mapGetOrCreate(mapGetOrCreate(doc, "ai"), "openai")
		mapSet(openai, "api_key", "perf-not-a-real-key", "!!str")
		mapSet(openai, "base_url", strings.TrimSuffix(mockURL, "/")+"/v1", "!!str")
	}

	knowledge := mapGetOrCreate(doc, "knowledge")
	mapSet(knowledge, "provider", "", "!!str")

	encoded, err := yaml.Marshal(&root)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return os.WriteFile(dstPath, encoded, 0o644)
}

func mapGet(parent *yaml.Node, key string) *yaml.Node {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			return parent.Content[i+1]
		}
	}
	return nil
}

func mapGetOrCreate(parent *yaml.Node, key string) *yaml.Node {
	if n := mapGet(parent, key); n != nil {
		return n
	}
	child := &yaml.Node{Kind: yaml.MappingNode}
	mapSetNode(parent, key, child)
	return child
}

func mapSet(parent *yaml.Node, key, value, tag string) {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content[i+1].Value = value
			parent.Content[i+1].Tag = tag
			return
		}
	}
	mapSetNode(parent, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value})
}

func mapSetNode(parent *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content[i+1] = value
			return
		}
	}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

// ---- 杂项 ----

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "perfbench: "+format+"\n", args...)
	os.Exit(1)
}
