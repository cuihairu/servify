// Package telegram is the Telegram Bot API adapter (contract Phase 3 pilot,
// docs/chat-platform-integration-contract.md). It owns everything
// platform-specific — the long-poll getUpdates HTTP face, sendMessage, the
// payload shape and the envelope-vs-status error mapping — and hands
// normalized contract events to the inbox sink. The shared contract
// vocabulary (error classes, gate errors, status table, retry budget and the
// poll-loop runner) lives in the channel package so it cannot drift between
// platform adapters.
//
// Contract divergence from Memex (documented in the contract's phase notes):
// Telegram answers failed calls with HTTP 200 plus an {"ok":false} envelope
// carrying error_code/parameters.retry_after, so error classification reads
// the JSON body — not just the HTTP status — and the retry pause comes from
// the envelope, not a header. ACK is not a separate call: advancing the
// getUpdates offset is the platform's acknowledgement, persisted by the
// connector's cursor store only after a durable inbox verdict.
package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"servify/apps/server/internal/platform/channel"
)

// The shared contract vocabulary, re-exported so telegram call sites read like
// the memex ones (one retry budget, every platform).
type (
	ErrorKind       = channel.ErrorKind
	AdapterError    = channel.AdapterError
	DeadLetterError = channel.DeadLetterError
	Runner          = channel.Runner
)

const (
	ErrAuth        = channel.ErrAuth
	ErrPermission  = channel.ErrPermission
	ErrRateLimited = channel.ErrRateLimited
	ErrTransient   = channel.ErrTransient
	ErrInvalid     = channel.ErrInvalid
	ErrPermanent   = channel.ErrPermanent
)

var (
	ErrIngestionDisabled = channel.ErrIngestionDisabled
	ErrSendDisabled      = channel.ErrSendDisabled
)

// DefaultBaseURL is the public Bot API root; a self-hosted Bot API server
// overrides it via the installation row's updates endpoint.
const DefaultBaseURL = "https://api.telegram.org"

// getUpdatesLimit is the Bot API's own per-pull ceiling.
const getUpdatesLimit = 100

// Update is one decoded long-poll update. Telegram also delivers non-message
// shapes (edited_message, callback_query, …); those translate to the contract
// quarantine lane, not to message events.
type Update struct {
	UpdateID int64        `json:"update_id"`
	Message  *ChatMessage `json:"message"`

	// raw is the exact bytes this update was decoded from — the retention copy
	// the contract requires encrypted-at-rest, saved by the connector before
	// the update is normalized.
	raw []byte
}

// UnmarshalJSON decodes the update and keeps the exact bytes it came from; the
// alias type avoids the method re-entering itself.
func (u *Update) UnmarshalJSON(data []byte) error {
	type updateAlias Update
	var alias updateAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*u = Update(alias)
	u.raw = append(u.raw, data...) // copy: data aliases a scratch buffer
	return nil
}

// Raw returns the exact payload bytes this update was decoded from.
func (u Update) Raw() []byte { return u.raw }

// ChatMessage is a message payload. Telegram only promises TEXT via
// message.text here; attachments stay server-side in the raw payload.
type ChatMessage struct {
	MessageID int64  `json:"message_id"`
	Chat      *Chat  `json:"chat"`
	From      *User  `json:"from"`
	Text      string `json:"text"`
	Date      int64  `json:"date"` // unix seconds
}

// Chat identifies the conversation a message belongs to. telegram.type is one
// of "private", "group", "supergroup" or "channel".
type Chat struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

// User identifies the message author.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// Client is the Telegram Bot API HTTP face. Authentication is the token in
// the /bot<token> path (the Bot API's own scheme); the token lives in the
// runtime environment via the installation row's env-var name, never here.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient wires a Bot API client; the http.Client (and its timeouts) stays
// with the caller.
func NewClient(baseURL, token string, hc *http.Client) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: hc}
}

// GetUpdates long-polls once. offset 0 asks for the recent default window;
// otherwise it is the acknowledged frontier (previous update_id + 1), so every
// update with update_id >= offset is delivered. timeoutSec > 0 long-polls
// server-side (Bot API cap 50); 0 short-polls.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	q := url.Values{}
	q.Set("offset", strconv.FormatInt(offset, 10))
	q.Set("timeout", strconv.Itoa(timeoutSec))
	q.Set("limit", strconv.Itoa(getUpdatesLimit))
	var out struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := c.do(ctx, "get_updates", http.MethodGet, "/bot"+c.token+"/getUpdates?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out.Result, nil
}

// SendText delivers one TEXT message as the bot and returns the platform
// message id (the retry idempotency anchor).
func (c *Client) SendText(ctx context.Context, chatID int64, text string) (int64, error) {
	q := url.Values{}
	q.Set("chat_id", strconv.FormatInt(chatID, 10))
	q.Set("text", text)
	var out struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	if err := c.do(ctx, "send_message", http.MethodPost, "/bot"+c.token+"/sendMessage?"+q.Encode(), &out); err != nil {
		return 0, err
	}
	return out.Result.MessageID, nil
}

// do performs one Bot API call. Every parameter goes in the query string (the
// Bot API's scheme for sendMessage and getUpdates), so there is no request
// body to serialize.
func (c *Client) do(ctx context.Context, op, method, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		// URL-construction errors echo the URL, which carries the bot token in
		// the path: redact before the detail can reach a log.
		return &AdapterError{Kind: ErrTransient, Op: op, Detail: c.redact(err.Error())}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &AdapterError{Kind: ErrTransient, Op: op, Detail: c.redact(err.Error())}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return &AdapterError{Kind: ErrInvalid, Status: resp.StatusCode, Op: op, Detail: err.Error()}
	}
	// Failed calls arrive as HTTP 200 with {"ok":false,error_code,...}; the
	// HTTP-status classes only apply to the rarer true non-200 responses.
	if resp.StatusCode != http.StatusOK {
		return &AdapterError{
			Kind:       channel.StatusKind(resp.StatusCode),
			Status:     resp.StatusCode,
			Op:         op,
			Detail:     telegramErrorDetail(data),
			RetryAfter: retryAfterHeader(resp),
		}
	}
	var envelope struct {
		OK          bool   `json:"ok"`
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return &AdapterError{Kind: ErrInvalid, Status: resp.StatusCode, Op: op, Detail: errorDetail(data)}
	}
	if !envelope.OK {
		kind := channel.StatusKind(envelope.ErrorCode)
		detail := envelope.Description
		if detail == "" {
			detail = errorDetail(data)
		}
		// The envelope's parameters.retry_after is the Bot API's rate-limit
		// pause (whole seconds); 0 when absent → caller falls back to its own
		// backoff.
		return &AdapterError{
			Kind:       kind,
			Status:     envelope.ErrorCode,
			Op:         op,
			Detail:     detail,
			RetryAfter: time.Duration(envelope.Parameters.RetryAfter) * time.Second,
		}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &AdapterError{Kind: ErrInvalid, Status: resp.StatusCode, Op: op, Detail: err.Error()}
	}
	return nil
}

// redact strips the bearer token from an error string so a URL that leaked
// into a detail never carries credentials into a log.
func (c *Client) redact(msg string) string {
	if c.token != "" {
		return strings.ReplaceAll(msg, c.token, "[redacted]")
	}
	return msg
}

// maxResponseBytes bounds one API response; a long-poll batch is TEXT
// updates, so a larger payload is a broken server, not something to buffer.
const maxResponseBytes = 8 << 20

// retryAfterHeader reads the Retry-After header in whole seconds (Telegram
// also uses the JSON parameters.retry_after for the common 200-envelope path);
// anything unparsable or non-positive yields 0 and the caller falls back to
// its own backoff.
func retryAfterHeader(resp *http.Response) time.Duration {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	secs, err := strconv.Atoi(v)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// telegramErrorDetail pulls the description out of an ok:false envelope,
// falling back to the raw body when the shape is wrong.
func telegramErrorDetail(data []byte) string {
	var envelope struct {
		Description string `json:"description"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Description == "" {
		return errorDetail(data)
	}
	return envelope.Description
}

// errorDetail is the raw-body fallback: never fails on a non-JSON body.
func errorDetail(data []byte) string {
	return strings.TrimSpace(string(data))
}
