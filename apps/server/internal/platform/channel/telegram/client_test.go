package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNewClientDefaultsBaseURLAndTrimsSlash(t *testing.T) {
	c := NewClient("", "tok", nil)
	if c.baseURL != DefaultBaseURL {
		t.Errorf("empty base url = %q, want %q", c.baseURL, DefaultBaseURL)
	}
	c = NewClient("http://example.org/", "tok", nil)
	if c.baseURL != "http://example.org" {
		t.Errorf("trailing slash kept: %q", c.baseURL)
	}
}

func TestClientGetUpdates(t *testing.T) {
	var gotPath, gotOffset, gotTimeout, gotLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		gotPath = r.URL.Path
		gotOffset = r.URL.Query().Get("offset")
		gotTimeout = r.URL.Query().Get("timeout")
		gotLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":[
			{"update_id":41,"message":{"message_id":7,"chat":{"id":-100123,"type":"supergroup","title":"ops"},
				"from":{"id":9001,"username":"alice"},"text":"hi","date":1697000000}}]}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "tok", srv.Client())
	updates, err := client.GetUpdates(context.Background(), 42, 25)
	if err != nil {
		t.Fatalf("get updates: %v", err)
	}
	if gotPath != "/bottok/getUpdates" {
		t.Errorf("path = %q, want /bottok/getUpdates (token auth is the path)", gotPath)
	}
	if gotOffset != "42" || gotTimeout != "25" || gotLimit != strconv.Itoa(getUpdatesLimit) {
		t.Errorf("query = offset %q timeout %q limit %q", gotOffset, gotTimeout, gotLimit)
	}
	if len(updates) != 1 || updates[0].UpdateID != 41 {
		t.Fatalf("updates = %+v", updates)
	}
	msg := updates[0].Message
	if msg == nil || msg.MessageID != 7 || msg.Text != "hi" || msg.Date != 1697000000 {
		t.Errorf("message = %+v", msg)
	}
	if msg.Chat == nil || msg.Chat.ID != -100123 || msg.Chat.Type != "supergroup" || msg.Chat.Title != "ops" {
		t.Errorf("chat = %+v", msg.Chat)
	}
	if msg.From == nil || msg.From.ID != 9001 || msg.From.Username != "alice" {
		t.Errorf("from = %+v", msg.From)
	}
}

func TestClientGetUpdatesNullResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":null}`))
	}))
	defer srv.Close()
	updates, err := NewClient(srv.URL, "tok", srv.Client()).GetUpdates(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("null result: %v", err)
	}
	if len(updates) != 0 {
		t.Errorf("updates = %v, want empty", updates)
	}
}

func TestClientSendText(t *testing.T) {
	var gotPath, gotChatID, gotText, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotChatID = r.URL.Query().Get("chat_id")
		gotText = r.URL.Query().Get("text")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":77}}`))
	}))
	defer srv.Close()

	msgID, err := NewClient(srv.URL, "tok", srv.Client()).SendText(context.Background(), -100123, "hello world")
	if err != nil {
		t.Fatalf("send text: %v", err)
	}
	if msgID != 77 {
		t.Errorf("message id = %d, want 77", msgID)
	}
	if gotMethod != http.MethodPost || gotPath != "/bottok/sendMessage" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if gotChatID != "-100123" || gotText != "hello world" {
		t.Errorf("query = chat_id %q text %q", gotChatID, gotText)
	}
}

// Telegram answers failed calls with HTTP 200 + {"ok":false,error_code,...};
// classification must read the envelope, and parameters.retry_after must
// become the AdapterError pause.
func TestClientEnvelopeErrors(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		kind       ErrorKind
		status     int
		retryAfter time.Duration
		wantDetail string
	}{
		{
			name:       "unauthorized",
			body:       `{"ok":false,"error_code":401,"description":"Unauthorized"}`,
			kind:       ErrAuth,
			status:     401,
			wantDetail: "Unauthorized",
		},
		{
			name:       "forbidden bot blocked",
			body:       `{"ok":false,"error_code":403,"description":"bot was blocked by the user"}`,
			kind:       ErrPermission,
			status:     403,
			wantDetail: "bot was blocked by the user",
		},
		{
			name:       "rate limited with retry_after",
			body:       `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":17}}`,
			kind:       ErrRateLimited,
			status:     429,
			retryAfter: 17 * time.Second,
			wantDetail: "Too Many Requests",
		},
		{
			name:       "not found chat",
			body:       `{"ok":false,"error_code":400,"description":"chat not found"}`,
			kind:       ErrInvalid,
			status:     400,
			wantDetail: "chat not found",
		},
		{
			name:       "server side",
			body:       `{"ok":false,"error_code":502,"description":"Bad Gateway"}`,
			kind:       ErrTransient,
			status:     502,
			wantDetail: "Bad Gateway",
		},
		{
			name:       "no description falls back to raw body",
			body:       `{"ok":false,"error_code":400}`,
			kind:       ErrInvalid,
			status:     400,
			wantDetail: `{"ok":false,"error_code":400}`,
		},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(tc.body))
		}))
		_, err := NewClient(srv.URL, "tok", srv.Client()).GetUpdates(context.Background(), 0, 0)
		srv.Close()
		var apiErr *AdapterError
		if !errors.As(err, &apiErr) {
			t.Errorf("%s: err = %v, want AdapterError", tc.name, err)
			continue
		}
		if apiErr.Kind != tc.kind || apiErr.Status != tc.status {
			t.Errorf("%s: kind/status = %s/%d, want %s/%d", tc.name, apiErr.Kind, apiErr.Status, tc.kind, tc.status)
		}
		if apiErr.RetryAfter != tc.retryAfter {
			t.Errorf("%s: retry after = %v, want %v", tc.name, apiErr.RetryAfter, tc.retryAfter)
		}
		if apiErr.Detail != tc.wantDetail {
			t.Errorf("%s: detail = %q, want %q", tc.name, apiErr.Detail, tc.wantDetail)
		}
		if apiErr.Op == "" {
			t.Errorf("%s: op not stamped", tc.name)
		}
	}
}

func TestClientEnvelopeErrorOnSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error_code":403,"description":"bot is not a member"}`))
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL, "tok", srv.Client()).SendText(context.Background(), 5, "hi")
	var apiErr *AdapterError
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrPermission || apiErr.Detail != "bot is not a member" {
		t.Errorf("send refused: err = %v, want permission AdapterError with description detail", err)
	}
}

// The reply chain rides the standard sendMessage query parameters, so a reply
// to a platform message is one platform call and the client keeps the platform
// message id for the caller's retry anchor.
func TestClientSendMessageReplyChainParams(t *testing.T) {
	var sawReply, sawThread, sawText, sawChat string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		sawChat = q.Get("chat_id")
		sawText = q.Get("text")
		sawReply = q.Get("reply_to_message_id")
		sawThread = q.Get("message_thread_id")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":88}}`))
	}))
	defer srv.Close()

	id, err := NewClient(srv.URL, "tok", srv.Client()).SendMessage(context.Background(), SendParams{
		ChatID:           -100123,
		Text:             "replying",
		ReplyToMessageID: 42,
		MessageThreadID:  7,
	})
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if id != 88 {
		t.Errorf("message id = %d, want 88", id)
	}
	if sawChat != "-100123" || sawText != "replying" || sawReply != "42" || sawThread != "7" {
		t.Errorf("params = chat %q text %q reply %q thread %q, want the reply chain echoed", sawChat, sawText, sawReply, sawThread)
	}
}

// A plain SendText omits the optional reply/thread parameters entirely.
func TestClientSendTextOmitsChainParams(t *testing.T) {
	var sawReply, sawThread string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		sawReply = q.Get("reply_to_message_id")
		sawThread = q.Get("message_thread_id")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL, "tok", srv.Client()).SendText(context.Background(), 5, "hi"); err != nil {
		t.Fatalf("send text: %v", err)
	}
	if sawReply != "" || sawThread != "" {
		t.Errorf("plain send carried chain params reply=%q thread=%q, want absent", sawReply, sawThread)
	}
}

// The rarer true non-200 responses take the shared HTTP status table, with the
// Retry-After header as the pause source.
func TestClientHTTPStatusErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"slow down"}`))
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL, "tok", srv.Client()).GetUpdates(context.Background(), 0, 0)
	var apiErr *AdapterError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want AdapterError", err)
	}
	if apiErr.Kind != ErrRateLimited || apiErr.Status != http.StatusTooManyRequests {
		t.Errorf("kind/status = %s/%d", apiErr.Kind, apiErr.Status)
	}
	if apiErr.RetryAfter != 5*time.Second {
		t.Errorf("retry after = %v, want 5s from header", apiErr.RetryAfter)
	}
	if apiErr.Detail != "slow down" {
		t.Errorf("detail = %q, want envelope description", apiErr.Detail)
	}

	unauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("not json at all"))
	}))
	defer unauth.Close()
	_, err = NewClient(unauth.URL, "tok", unauth.Client()).GetUpdates(context.Background(), 0, 0)
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrAuth || apiErr.Detail != "not json at all" {
		t.Errorf("non-200 non-json: err = %v, want auth AdapterError with raw detail", err)
	}
}

func TestClientTransportPayloadAndShapeErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	client := NewClient(srv.URL, "tok", srv.Client())
	srv.Close()
	_, err := client.GetUpdates(context.Background(), 0, 0)
	var apiErr *AdapterError
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrTransient {
		t.Errorf("dead server: err = %v, want transient AdapterError", err)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer broken.Close()
	_, err = NewClient(broken.URL, "tok", broken.Client()).GetUpdates(context.Background(), 0, 0)
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrInvalid {
		t.Errorf("malformed envelope: err = %v, want invalid AdapterError", err)
	}

	// ok:true but the decoded shape is wrong (result is not an update array).
	wrongType := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":"not-an-array"}`))
	}))
	defer wrongType.Close()
	_, err = NewClient(wrongType.URL, "tok", wrongType.Client()).GetUpdates(context.Background(), 0, 0)
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrInvalid {
		t.Errorf("wrong result shape: err = %v, want invalid AdapterError", err)
	}

	short := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte(`{"ok":true}`)) // 12 of 100 promised bytes
	}))
	defer short.Close()
	_, err = NewClient(short.URL, "tok", short.Client()).GetUpdates(context.Background(), 0, 0)
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrInvalid {
		t.Errorf("truncated body: err = %v, want invalid AdapterError", err)
	}
}

// The token authenticates via the URL path, so any detail that echoes the URL
// would leak the credential; it must come back redacted.
func TestClientRedactsTokenFromTransportErrors(t *testing.T) {
	_, err := NewClient("http://bad url", "SECRET-TOKEN", nil).
		GetUpdates(context.Background(), 0, 0)
	var apiErr *AdapterError
	if !errors.As(err, &apiErr) || apiErr.Kind != ErrTransient {
		t.Fatalf("bad url: err = %v, want transient AdapterError", err)
	}
	if strings.Contains(apiErr.Detail, "SECRET-TOKEN") {
		t.Errorf("detail leaks token: %q", apiErr.Detail)
	}
	if !strings.Contains(apiErr.Detail, "[redacted]") {
		t.Errorf("detail missing redaction marker: %q", apiErr.Detail)
	}
}

func TestClientRedactWithoutToken(t *testing.T) {
	c := NewClient("http://example.org", "", nil)
	if got := c.redact("plain failure"); got != "plain failure" {
		t.Errorf("redact with empty token = %q", got)
	}
}

func TestClientRetryAfterHeaderVariants(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"absent", "", 0},
		{"not a number", "soon", 0},
		{"zero", "0", 0},
		{"negative", "-3", 0},
		{"seconds", "7", 7 * time.Second},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tc.header != "" {
				w.Header().Set("Retry-After", tc.header)
			}
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"ok":false,"error_code":429,"description":"t=%s"}`, tc.name)))
		}))
		_, err := NewClient(srv.URL, "tok", srv.Client()).GetUpdates(context.Background(), 0, 0)
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
	body := `{"ok":true,"result":[` +
		`{"update_id":7,"message":{"message_id":11,"chat":{"id":-100123,"type":"supergroup"},"from":{"id":9},"text":"hi","date":1697000000}},` +
		`{"update_id":8}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	updates, err := NewClient(srv.URL, "tok", srv.Client()).GetUpdates(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("get updates: %v", err)
	}
	if len(updates) != 2 {
		t.Fatalf("updates = %d, want 2", len(updates))
	}
	// the retention copy is the payload bytes themselves, not a re-marshal
	wantMsg := `{"update_id":7,"message":{"message_id":11,"chat":{"id":-100123,"type":"supergroup"},"from":{"id":9},"text":"hi","date":1697000000}}`
	if got := string(updates[0].Raw()); got != wantMsg {
		t.Errorf("raw = %q, want the exact payload bytes %q", got, wantMsg)
	}
	// non-message shapes carry their bytes too: the quarantine lane is the
	// one that most needs the raw payload for triage
	if got := string(updates[1].Raw()); got != `{"update_id":8}` {
		t.Errorf("unknown-shape raw = %q, want %q", got, `{"update_id":8}`)
	}

	// an element that is not an update shape fails the decode
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":[7]}`))
	}))
	defer badSrv.Close()
	_, err = NewClient(badSrv.URL, "tok", badSrv.Client()).GetUpdates(context.Background(), 0, 0)
	var ae *AdapterError
	if err == nil || !errors.As(err, &ae) || ae.Kind != ErrInvalid {
		t.Errorf("element decode error: err = %v, want an ErrInvalid adapter error", err)
	}
}
