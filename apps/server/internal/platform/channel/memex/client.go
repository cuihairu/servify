// Package memex is the Memex bot adapter (contract Phase 2 validation
// platform, docs/chat-platform-integration-contract.md). It owns everything
// platform-specific — the pull/ACK HTTP surface, TEXT-only payload shape and
// error classes — and hands normalized contract events to the inbox sink.
package memex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// ErrorKind is the contract's error-class vocabulary for adapter failures.
type ErrorKind string

const (
	ErrAuth        ErrorKind = "auth"
	ErrPermission  ErrorKind = "permission"
	ErrRateLimited ErrorKind = "rate_limited"
	ErrTransient   ErrorKind = "transient"
	ErrInvalid     ErrorKind = "invalid"
	ErrPermanent   ErrorKind = "permanent"
)

// AdapterError carries the contract's error class for one failed API call so
// callers can decide retry policy without parsing provider strings.
type AdapterError struct {
	Kind   ErrorKind
	Status int
	Op     string
	Detail string
}

func (e *AdapterError) Error() string {
	return fmt.Sprintf("memex %s: status=%d kind=%s: %s", e.Op, e.Status, e.Kind, e.Detail)
}

// classifyStatus maps the Memex webhook status table onto contract classes
// (401 auth, 403 permission, 429 rate limited, 404 permanent target,
// 5xx transient, everything else invalid).
func classifyStatus(status int) ErrorKind {
	switch {
	case status == http.StatusUnauthorized:
		return ErrAuth
	case status == http.StatusForbidden:
		return ErrPermission
	case status == http.StatusTooManyRequests:
		return ErrRateLimited
	case status == http.StatusNotFound:
		return ErrPermanent
	case status >= 500:
		return ErrTransient
	default:
		return ErrInvalid
	}
}

// Update is one decoded Memex bot update. Memex only promises TEXT
// (msg_id/from/to/text/ts_ms); anything richer stays server-side.
type Update struct {
	MsgID string `json:"msg_id"`
	From  string `json:"from"`
	To    string `json:"to"`
	Text  string `json:"text"`
	TsMS  int64  `json:"ts_ms"`
}

// Client is the Memex bot HTTP face (GET /bot/updates, POST /bot/ack,
// POST /bot/send, Bearer token auth).
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient wires a Memex bot client; the http.Client (and its timeouts) stays
// with the caller.
func NewClient(baseURL, token string, hc *http.Client) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: hc}
}

// Updates drains the bot inbox once. timeoutMS > 0 asks the server to long-poll
// (Memex caps at 30000ms); 0 keeps the immediate-drain default.
func (c *Client) Updates(ctx context.Context, timeoutMS int) ([]Update, error) {
	var out struct {
		OK      bool     `json:"ok"`
		Updates []Update `json:"updates"`
	}
	path := "/bot/updates?timeout_ms=" + strconv.Itoa(timeoutMS)
	if err := c.do(ctx, "updates", http.MethodGet, path, "", &out); err != nil {
		return nil, err
	}
	return out.Updates, nil
}

// Ack clears one message from the bot queue. Memex ACKs are per message and
// idempotent; the inbox decides when an ACK is safe, never this client.
func (c *Client) Ack(ctx context.Context, msgID string) error {
	body, _ := json.Marshal(map[string]string{"msg_id": msgID}) // string map: Marshal cannot fail
	var ignored struct{}
	return c.do(ctx, "ack", http.MethodPost, "/bot/ack", string(body), &ignored)
}

// Send delivers one TEXT message to "group:<id>" or an account name and
// returns the platform message id (the retry idempotency anchor).
func (c *Client) Send(ctx context.Context, target, text string) (string, error) {
	body, _ := json.Marshal(map[string]string{"target": target, "text": text}) // string map: Marshal cannot fail
	var out struct {
		OK    bool   `json:"ok"`
		MsgID string `json:"msg_id"`
	}
	if err := c.do(ctx, "send", http.MethodPost, "/bot/send", string(body), &out); err != nil {
		return "", err
	}
	return out.MsgID, nil
}

func (c *Client) do(ctx context.Context, op, method, path, body string, out interface{}) error {
	var payload io.Reader
	if body != "" {
		payload = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
	if err != nil {
		return &AdapterError{Kind: ErrTransient, Op: op, Detail: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &AdapterError{Kind: ErrTransient, Op: op, Detail: err.Error()}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return &AdapterError{Kind: ErrInvalid, Status: resp.StatusCode, Op: op, Detail: err.Error()}
	}
	if resp.StatusCode != http.StatusOK {
		return &AdapterError{
			Kind:   classifyStatus(resp.StatusCode),
			Status: resp.StatusCode,
			Op:     op,
			Detail: errorDetail(data),
		}
	}
	var envelope struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &envelope) != nil || !envelope.OK {
		return &AdapterError{Kind: ErrInvalid, Status: resp.StatusCode, Op: op,
			Detail: errorDetail(data)}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &AdapterError{Kind: ErrInvalid, Status: resp.StatusCode, Op: op,
			Detail: err.Error()}
	}
	return nil
}

// maxResponseBytes bounds one API response; a bot inbox batch is TEXT updates,
// so a larger payload is a broken server, not something to buffer.
const maxResponseBytes = 8 << 20

// errorDetail pulls the provider error string out of an error envelope without
// failing when the body is not the expected shape.
func errorDetail(data []byte) string {
	var envelope struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Error == "" {
		return strings.TrimSpace(string(data))
	}
	return envelope.Error
}
