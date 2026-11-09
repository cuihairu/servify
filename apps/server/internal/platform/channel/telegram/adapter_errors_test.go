package telegram

// drain 的两条错误路径：收件箱 Accept 失败（不确认、偏移不前移）与会话授权
// 的能力快照读取失败（整批中止，事件不进入任何通道）。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"servify/apps/server/internal/platform/channel"

	"gorm.io/gorm"
)

type failSink struct{ err error }

func (f failSink) Accept(ctx context.Context, ev channel.ConnectorEvent) (channel.AckDecision, error) {
	return channel.AckCommit, f.err
}

func pollOneUpdate(t *testing.T) *httptest.Server {
	t.Helper()
	fake := &botAPI{t: t}
	fake.onPoll = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":77,"message":{"message_id":8,"chat":{"id":-100123,"type":"supergroup"},"text":"hi","date":1697000000}}]}`))
	}
	return httptest.NewServer(fake.handler())
}

func TestReceiveSinkAcceptErrorAbortsWithoutAdvancingCursor(t *testing.T) {
	boundSink, caps, cursors, _ := newAdapterStores(t)
	enableIngestion(t, caps)
	srv := pollOneUpdate(t)
	defer srv.Close()

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, boundSink, cursors, adapterConfig())
	wantErr := errors.New("inbox down")
	if err := connector.Receive(context.Background(), failSink{err: wantErr}); !errors.Is(err, wantErr) {
		t.Fatalf("receive error = %v, want %v", err, wantErr)
	}
	if cur := storedCursor(t, cursors); cur != "" {
		t.Fatalf("cursor = %q, want empty (accept failure must not advance)", cur)
	}
}

func TestReceiveAuthorizationSnapshotErrorAborts(t *testing.T) {
	boundSink, caps, cursors, db := newAdapterStores(t)
	enableIngestion(t, caps)
	srv := pollOneUpdate(t)
	defer srv.Close()

	// 只让第二次能力快照查询失败（会话授权门那次）；总开关那次放行，错误
	// 才能精确落在授权门的 Latest 错误分支上。
	var capsQueries int
	if err := db.Callback().Query().Before("gorm:query").Register("test:fail_second_caps", func(tx *gorm.DB) {
		if tx.Statement.Table != "chat_connector_capabilities" {
			return
		}
		capsQueries++
		if capsQueries > 1 {
			tx.AddError(errors.New("caps store down"))
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}

	connector := NewConnector(NewClient(srv.URL, "tok", srv.Client()), caps, boundSink, cursors, adapterConfig())
	if err := connector.Receive(context.Background(), boundSink); err == nil {
		t.Fatal("capability snapshot failure must surface from the authorization gate")
	}
	if cur := storedCursor(t, cursors); cur != "" {
		t.Fatalf("cursor = %q, want empty (authorization failure must not advance)", cur)
	}
}
