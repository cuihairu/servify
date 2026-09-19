package realtime

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// lockedBuffer 是并发安全的日志缓冲，供 writePump 的 goroutine 写入。
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// TestWebSocketWritePumpWriteJSONError 覆盖 writePump 写消息失败的退出分支：
// 先关闭底层连接，再投递一条消息，writePump 的首个 WriteJSON 必然报错并返回。
func TestWebSocketWritePumpWriteJSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = upgrader.Upgrade(w, r, nil)
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):]+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// 捕获全局日志以确定错误分支真的执行过
	logs := &lockedBuffer{}
	origOut := logrus.StandardLogger().Out
	logrus.StandardLogger().SetOutput(logs)
	t.Cleanup(func() { logrus.StandardLogger().SetOutput(origOut) })

	client := &WebSocketClient{
		ID: "c-wperr", SessionID: "s-wperr", Conn: conn,
		Send: make(chan WebSocketMessage),
	}
	done := make(chan struct{})
	go func() {
		client.writePump()
		close(done)
	}()

	// 关闭传输层后投递消息：非缓冲 channel 的交接保证 writePump
	// 一定收到这条消息并走到 WriteJSON 错误分支
	if err := conn.Close(); err != nil {
		t.Fatalf("close conn: %v", err)
	}
	client.Send <- WebSocketMessage{Type: "text", Data: "hello"}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("writePump did not exit after write failure")
	}
	if !strings.Contains(logs.String(), "WriteJSON error") {
		t.Fatalf("expected WriteJSON error log, got %q", logs.String())
	}
}
