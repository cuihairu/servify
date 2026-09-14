package scripts

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
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

// TestWorkspaceAcceptanceScriptWritesEvidence 用 mock server 走通
// test-workspace-acceptance.sh 全流程,并校验证据与 manifest 内容。
func TestWorkspaceAcceptanceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	type participant struct {
		ID     string `json:"ID"`
		UserID int    `json:"UserID"`
		Role   string `json:"Role"`
	}

	type message struct {
		ID             string `json:"id"`
		ConversationID string `json:"conversation_id"`
		Sender         string `json:"sender"`
		Kind           string `json:"kind"`
		Content        string `json:"content"`
		CreatedAt      string `json:"created_at"`
	}

	type conversation struct {
		ID            string        `json:"id"`
		Status        string        `json:"status"`
		Channel       string        `json:"channel"`
		Participants  []participant `json:"participants"`
		StartedAt     string        `json:"started_at"`
		LastMessageAt string        `json:"last_message_at,omitempty"`
		EndedAt       string        `json:"ended_at,omitempty"`
	}

	var (
		mu             sync.Mutex
		registerCount  int
		agentCreations int
		wsIngress      int
		nextUserID     = 100
		token          = "workspace-admin-token"
		sessions       = map[string]*conversation{}
		messagesBySess = map[string][]message{}
	)

	ingestMessage := func(sessionID, sender, content string) message {
		messages := messagesBySess[sessionID]
		msg := message{
			ID:             fmt.Sprintf("%s-%d", sessionID, len(messages)+1),
			ConversationID: sessionID,
			Sender:         sender,
			Kind:           "text",
			Content:        content,
			CreatedAt:      "2026-01-01T00:00:00Z",
		}
		messagesBySess[sessionID] = append(messages, msg)
		return msg
	}

	authorized := func(r *http.Request) bool {
		return r.Header.Get("Authorization") == "Bearer "+token
	}

	servify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
			return
		case "/api/v1/auth/login":
			mu.Lock()
			defer mu.Unlock()
			_, _ = w.Write([]byte(`{"token":"` + token + `","user":{"id":1,"role":"admin"}}`))
			return
		case "/api/v1/auth/register":
			mu.Lock()
			defer mu.Unlock()
			registerCount++
			nextUserID++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"token":"reg-token","refresh_token":"reg-refresh","user":{"id":%d,"role":"agent"}}`, nextUserID)))
			return
		case "/api/agents":
			if !authorized(r) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid token"}`))
				return
			}
			mu.Lock()
			defer mu.Unlock()
			agentCreations++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"user_id":` + fmt.Sprint(nextUserID) + `,"status":"online"}`))
			return
		}

		if r.URL.Path == "/api/v1/ws" {
			sessionID := r.URL.Query().Get("session_id")
			if sessionID == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			hj, ok := w.(http.Hijacker)
			if !ok {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			conn, rw, err := hj.Hijack()
			if err != nil {
				return
			}
			defer conn.Close()

			key := r.Header.Get("Sec-WebSocket-Key")
			accepter := sha1.New()
			_, _ = accepter.Write([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
			accept := base64.StdEncoding.EncodeToString(accepter.Sum(nil))
			handshake := "HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
			if _, err := conn.Write([]byte(handshake)); err != nil {
				return
			}

			opcode, payload, err := readMaskedFrame(rw.Reader)
			if err != nil || opcode != 0x1 {
				return
			}
			// 与真实契约一致:内容在 data.content,顶层 content 会被服务端静默丢弃
			var inbound struct {
				Type string `json:"type"`
				Data struct {
					Content string `json:"content"`
				} `json:"data"`
			}
			if json.Unmarshal(payload, &inbound) != nil || inbound.Type != "text-message" || strings.TrimSpace(inbound.Data.Content) == "" {
				return
			}

			mu.Lock()
			if _, exists := sessions[sessionID]; !exists {
				sessions[sessionID] = &conversation{
					ID:        sessionID,
					Status:    "active",
					Channel:   "web",
					StartedAt: "2026-01-01T00:00:00Z",
				}
			}
			ingestMessage(sessionID, "customer", inbound.Data.Content)
			mu.Unlock()
			wsIngress++

			_, _ = conn.Write(buildUnmaskedTextFrame([]byte(`{"type":"message-received"}`)))
			return
		}

		if !authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid token"}`))
			return
		}

		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/api/omni/workspace" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"recent_sessions":[],"online_agents":2}`))
		case strings.HasPrefix(r.URL.Path, "/api/omni/sessions/"):
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/omni/sessions/"), "/")
			sessionID := parts[0]
			rest := ""
			if len(parts) > 1 {
				rest = parts[1]
			}
			sess, exists := sessions[sessionID]
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"Conversation not found"}`))
				return
			}
			switch {
			case rest == "" && r.Method == http.MethodGet:
				_, _ = w.Write(mustJSON(t, ginH("data", sess)))
			case rest == "messages" && r.Method == http.MethodGet:
				_, _ = w.Write(mustJSON(t, ginH("data", messagesBySess[sessionID])))
			case rest == "messages" && r.Method == http.MethodPost:
				var req struct {
					Content string `json:"content"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				if strings.TrimSpace(req.Content) == "" {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"Invalid request body","message":"content is required"}`))
					return
				}
				msg := ingestMessage(sessionID, "agent", req.Content)
				sess.LastMessageAt = msg.CreatedAt
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write(mustJSON(t, ginH("message", "Message sent successfully", "data", msg)))
			case rest == "assign" && r.Method == http.MethodPost:
				var req struct {
					AgentID int `json:"agent_id"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				if req.AgentID == 0 {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				sess.Participants = []participant{{
					ID:     fmt.Sprintf("agent:%d", req.AgentID),
					UserID: req.AgentID,
					Role:   "agent",
				}}
				sess.Status = "active"
				_, _ = w.Write(mustJSON(t, ginH("message", "Agent assigned successfully", "data", sess)))
			case rest == "transfer" && r.Method == http.MethodPost:
				var req struct {
					ToAgentID int `json:"to_agent_id"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				if req.ToAgentID == 0 {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				sess.Participants = []participant{{
					ID:     fmt.Sprintf("agent:%d", req.ToAgentID),
					UserID: req.ToAgentID,
					Role:   "agent",
				}}
				sess.Status = "transferred"
				ingestMessage(sessionID, "system", "会话已转派给客服")
				_, _ = w.Write(mustJSON(t, ginH("message", "Conversation transferred successfully", "data", sess)))
			case rest == "close" && r.Method == http.MethodPost:
				sess.Status = "closed"
				sess.EndedAt = "2026-01-01T01:00:00Z"
				ingestMessage(sessionID, "system", "会话已结束")
				_, _ = w.Write(mustJSON(t, ginH("message", "Conversation closed successfully", "data", sess)))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer servify.Close()

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"WORKSPACE_ACCEPTANCE_MODE=mock SERVIFY_URL=%q EVIDENCE_DIR=%q bash ./test-workspace-acceptance.sh",
		servify.URL,
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected workspace acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"admin-auth.json",
		"session-detail.json",
		"session-messages-visitor.json",
		"agent-message.json",
		"session-messages-after-agent.json",
		"session-assigned.json",
		"session-transferred.json",
		"session-closed.json",
		"workspace-overview.json",
		"unknown-session.json",
		"empty-message.json",
		"unauthenticated-session.json",
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
		"admin_auth_ok=true",
		"agents_ready=true",
		"visitor_ws_ingress_ok=true",
		"session_detail_status_field=active",
		"visitor_messages=1",
		"agent_messages=1",
		"assigned_agent_hits=1",
		"status_after_transfer=transferred",
		"status_after_close=closed",
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
		`"provider": "workspace"`,
		`"mode": "mock"`,
		`"visitor_ws_ingress_ok": "true"`,
		`"assign_ok": "true"`,
		`"transfer_ok": "true"`,
		`"close_ok": "true"`,
		`"unknown_session_rejected": "true"`,
		`"unauthenticated_rejected": "true"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}

	if registerCount != 2 || agentCreations != 2 || wsIngress != 1 {
		t.Fatalf("unexpected request counts register=%d agents=%d ws=%d", registerCount, agentCreations, wsIngress)
	}
}

// readMaskedFrame 读取一帧客户端->服务端的 WebSocket 帧(必须带掩码)。
func readMaskedFrame(r *bufio.Reader) (byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := ioReadFull(r, header); err != nil {
		return 0, nil, err
	}
	opcode := header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := int(header[1] & 0x7f)
	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err := ioReadFull(r, ext); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := ioReadFull(r, ext); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint64(ext))
	}
	var maskKey [4]byte
	if masked {
		if _, err := ioReadFull(r, maskKey[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := ioReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	return opcode, payload, nil
}

func buildUnmaskedTextFrame(payload []byte) []byte {
	out := []byte{0x81}
	switch {
	case len(payload) < 126:
		out = append(out, byte(len(payload)))
	case len(payload) < 65536:
		out = append(out, 126)
		out = binary.BigEndian.AppendUint16(out, uint16(len(payload)))
	default:
		out = append(out, 127)
		out = binary.BigEndian.AppendUint64(out, uint64(len(payload)))
	}
	return append(out, payload...)
}

func ioReadFull(r *bufio.Reader, buf []byte) (int, error) {
	return io.ReadFull(r, buf)
}

// ginH 是 gin.H 的最小替代,避免 scripts 包引入 gin 依赖。
func ginH(keysAndValues ...interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(keysAndValues)/2)
	for i := 0; i+1 < len(keysAndValues); i += 2 {
		out[keysAndValues[i].(string)] = keysAndValues[i+1]
	}
	return out
}

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}
