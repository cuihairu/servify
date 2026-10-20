package memex

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientUpdates(t *testing.T) {
	var gotAuth string
	var gotTimeout string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodGet || r.URL.Path != "/bot/updates" {
			t.Errorf("updates request = %s %s", r.Method, r.URL.Path)
		}
		gotTimeout = r.URL.Query().Get("timeout_ms")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"updates":[{"msg_id":"m1","from":"alice","to":"bot:echo","text":"hi","ts_ms":1697000000000}]}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "tok", srv.Client())
	updates, err := client.Updates(context.Background(), 1500)
	if err != nil {
		t.Fatalf("updates: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("authorization = %q, want bearer token", gotAuth)
	}
	if gotTimeout != "1500" {
		t.Errorf("timeout_ms = %q, want 1500", gotTimeout)
	}
	if len(updates) != 1 || updates[0].MsgID != "m1" || updates[0].From != "alice" ||
		updates[0].To != "bot:echo" || updates[0].Text != "hi" || updates[0].TsMS != 1697000000000 {
		t.Errorf("updates decoded wrong: %+v", updates)
	}
}

func TestClientAckAndSend(t *testing.T) {
	var ackBody string
	var sendTarget, sendText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bot/ack":
			buf := make([]byte, 256)
			n, _ := r.Body.Read(buf)
			ackBody = string(buf[:n])
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/bot/send":
			buf := make([]byte, 256)
			n, _ := r.Body.Read(buf)
			payload := string(buf[:n])
			if !strings.Contains(payload, `"target":"group:5"`) || !strings.Contains(payload, `"text":"done"`) {
				t.Errorf("send payload = %s", payload)
			}
			sendTarget, sendText = "group:5", "done"
			_, _ = w.Write([]byte(`{"ok":true,"msg_id":"sent-1","recipients":["alice"]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "tok", srv.Client())
	if err := client.Ack(context.Background(), "m1"); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if !strings.Contains(ackBody, `"msg_id":"m1"`) {
		t.Errorf("ack body = %s", ackBody)
	}
	msgID, err := client.Send(context.Background(), "group:5", "done")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if msgID != "sent-1" {
		t.Errorf("send msg id = %q, want sent-1", msgID)
	}
	if sendTarget != "group:5" || sendText != "done" {
		t.Errorf("send request = %s/%s", sendTarget, sendText)
	}
}

func TestClientErrorClasses(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   ErrorKind
	}{
		{http.StatusUnauthorized, `{"ok":false,"error":"bad token"}`, ErrAuth},
		{http.StatusForbidden, `{"ok":false,"error":"not allowed"}`, ErrPermission},
		{http.StatusTooManyRequests, `{"ok":false,"error":"slow down"}`, ErrRateLimited},
		{http.StatusInternalServerError, `{"ok":false,"error":"boom"}`, ErrTransient},
		{http.StatusNotFound, `{"ok":false,"error":"no such group"}`, ErrPermanent},
		{http.StatusBadRequest, `{"ok":false,"error":"bad field"}`, ErrInvalid},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		client := NewClient(srv.URL, "tok", srv.Client())
		_, err := client.Updates(context.Background(), 0)
		srv.Close()
		var apiErr *AdapterError
		if !errors.As(err, &apiErr) {
			t.Errorf("status %d: err = %v, want AdapterError", tc.status, err)
			continue
		}
		if apiErr.Kind != tc.kind {
			t.Errorf("status %d: kind = %s, want %s", tc.status, apiErr.Kind, tc.kind)
		}
		if apiErr.Status != tc.status {
			t.Errorf("status %d: recorded %d", tc.status, apiErr.Status)
		}
		if apiErr.Detail == "" {
			t.Errorf("status %d: error detail not extracted", tc.status)
		}
	}
}

func TestClientTransportAndPayloadErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	client := NewClient(srv.URL, "tok", srv.Client())
	srv.Close()

	_, err := client.Updates(context.Background(), 0)
	var apiErr *AdapterError
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrTransient {
		t.Errorf("dead server: err = %v, want transient AdapterError", err)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer broken.Close()
	_, err = NewClient(broken.URL, "tok", broken.Client()).Updates(context.Background(), 0)
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrInvalid {
		t.Errorf("malformed json: err = %v, want invalid AdapterError", err)
	}

	okFalse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"refused"}`))
	}))
	defer okFalse.Close()
	_, err = NewClient(okFalse.URL, "tok", okFalse.Client()).Updates(context.Background(), 0)
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrInvalid {
		t.Errorf("ok:false envelope: err = %v, want invalid AdapterError", err)
	}
}

func TestClientTruncatedBodyAndBadURL(t *testing.T) {
	short := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte(`{"ok":true}`)) // 12 of 100 promised bytes
	}))
	defer short.Close()
	_, err := NewClient(short.URL, "tok", short.Client()).Updates(context.Background(), 0)
	var apiErr *AdapterError
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrInvalid {
		t.Errorf("truncated body: err = %v, want invalid AdapterError", err)
	}

	_, err = NewClient("http://bad url with space", "tok", short.Client()).
		Updates(context.Background(), 0)
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrTransient {
		t.Errorf("bad url: err = %v, want transient AdapterError", err)
	}
}

func TestClientSendFailureAndMalformedEnvelopeField(t *testing.T) {
	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"ok":false,"error":"group not allowed"}`))
	}))
	defer refused.Close()
	_, err := NewClient(refused.URL, "tok", refused.Client()).
		Send(context.Background(), "group:9", "hello")
	var apiErr *AdapterError
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrPermission || apiErr.Detail == "" {
		t.Errorf("send refused: err = %v, want permission AdapterError with detail", err)
	}

	wrongType := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"updates":"not-an-array"}`))
	}))
	defer wrongType.Close()
	_, err = NewClient(wrongType.URL, "tok", wrongType.Client()).
		Updates(context.Background(), 0)
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrInvalid {
		t.Errorf("malformed field: err = %v, want invalid AdapterError", err)
	}
}

func TestClientRetryAfterHeader(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"seconds", "7", 7 * time.Second},
		{"absent", "", 0},
		{"not a number", "soon", 0},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tc.header != "" {
				w.Header().Set("Retry-After", tc.header)
			}
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"error":"slow down"}`))
		}))
		_, err := NewClient(srv.URL, "tok", srv.Client()).Updates(context.Background(), 0)
		srv.Close()
		var apiErr *AdapterError
		if !errors.As(err, &apiErr) {
			t.Errorf("%s: err = %v, want AdapterError", tc.name, err)
			continue
		}
		if apiErr.RetryAfter != tc.want {
			t.Errorf("%s: retry after = %v, want %v", tc.name, apiErr.RetryAfter, tc.want)
		}
	}
}

func TestUpdateKeepsRawBytesAndSurfacesDecodeErrors(t *testing.T) {
	body := `{"ok":true,"updates":[{"msg_id":"m-9","from":"alice","to":"ops","text":"hi","ts_ms":1697000000123},{"msg_id":"m-10"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	updates, err := NewClient(srv.URL, "tok", srv.Client()).Updates(context.Background(), 0)
	if err != nil {
		t.Fatalf("updates: %v", err)
	}
	if len(updates) != 2 {
		t.Fatalf("updates = %d, want 2", len(updates))
	}
	// the retention copy is the payload bytes themselves, not a re-marshal
	want := `{"msg_id":"m-9","from":"alice","to":"ops","text":"hi","ts_ms":1697000000123}`
	if got := string(updates[0].Raw()); got != want {
		t.Errorf("raw = %q, want the exact payload bytes %q", got, want)
	}
	if got := string(updates[1].Raw()); got != `{"msg_id":"m-10"}` {
		t.Errorf("sparse raw = %q, want %q", got, `{"msg_id":"m-10"}`)
	}

	// an element that is not an update shape fails the decode
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"updates":[7]}`))
	}))
	defer badSrv.Close()
	_, err = NewClient(badSrv.URL, "tok", badSrv.Client()).Updates(context.Background(), 0)
	var ae *AdapterError
	if err == nil || !errors.As(err, &ae) || ae.Kind != ErrInvalid {
		t.Errorf("element decode error: err = %v, want an ErrInvalid adapter error", err)
	}
}
