package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestSessionTransferHandlerUnit(t *testing.T) {
	newRouter := func(svc *unitTransferService, withUser bool) *gin.Engine {
		gin.SetMode(gin.TestMode)
		logger := logrus.New()
		logger.SetLevel(logrus.ErrorLevel)
		h := NewSessionTransferHandler(svc, logger)
		r := gin.New()
		if withUser {
			r.Use(func(c *gin.Context) {
				c.Set("user_id", uint(4))
				c.Next()
			})
		}
		RegisterSessionTransferRoutes(&r.RouterGroup, h)
		return r
	}
	request := func(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	result := transferResultFixture()

	t.Run("to human success", func(t *testing.T) {
		svc := &unitTransferService{transfer: result}
		w := request(newRouter(svc, false), http.MethodPost, "/session-transfer/to-human", `{"session_id":"s1"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("to human bad body", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{}, false), http.MethodPost, "/session-transfer/to-human", `{`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("to human error", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{humanErr: errors.New("boom")}, false), http.MethodPost, "/session-transfer/to-human", `{"session_id":"s1"}`)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("to agent success", func(t *testing.T) {
		svc := &unitTransferService{transfer: result}
		w := request(newRouter(svc, false), http.MethodPost, "/session-transfer/to-agent", `{"session_id":"s1","target_agent_id":3}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("to agent bad body", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{}, false), http.MethodPost, "/session-transfer/to-agent", `{"session_id":"s1"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("to agent error", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{agentErr: errors.New("boom")}, false), http.MethodPost, "/session-transfer/to-agent", `{"session_id":"s1","target_agent_id":3}`)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("history success", func(t *testing.T) {
		svc := &unitTransferService{history: transferHistoryFixture()}
		w := request(newRouter(svc, false), http.MethodGet, "/session-transfer/history/s1", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("history error", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{historyErr: errors.New("boom")}, false), http.MethodGet, "/session-transfer/history/s1", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("history empty session id via direct context", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		logger := logrus.New()
		logger.SetLevel(logrus.ErrorLevel)
		h := NewSessionTransferHandler(&unitTransferService{}, logger)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/history", nil)
		h.GetTransferHistory(c)
		if c.Writer.Status() != http.StatusBadRequest {
			t.Fatalf("status = %d", c.Writer.Status())
		}
	})
	t.Run("recent history success and limit variants", func(t *testing.T) {
		svc := &unitTransferService{history: transferHistoryFixture()}
		r := newRouter(svc, false)
		for _, q := range []string{"?limit=5", "?limit=zz", ""} {
			w := request(r, http.MethodGet, "/session-transfer/history"+q, "")
			if w.Code != http.StatusOK {
				t.Fatalf("query %s status = %d", q, w.Code)
			}
		}
	})
	t.Run("recent history error", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{listErr: errors.New("boom")}, false), http.MethodGet, "/session-transfer/history", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("waiting success variants", func(t *testing.T) {
		svc := &unitTransferService{}
		r := newRouter(svc, false)
		for _, q := range []string{"?status=cancelled&limit=7", "?limit=zz", ""} {
			w := request(r, http.MethodGet, "/session-transfer/waiting"+q, "")
			if w.Code != http.StatusOK {
				t.Fatalf("query %s status = %d", q, w.Code)
			}
		}
	})
	t.Run("waiting error", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{waitingErr: errors.New("boom")}, false), http.MethodGet, "/session-transfer/waiting", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("cancel without user", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{}, false), http.MethodPost, "/session-transfer/cancel", `{"session_id":"s1"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("cancel with user", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{}, true), http.MethodPost, "/session-transfer/cancel", `{"session_id":"s1","reason":"r"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("cancel bad body", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{}, true), http.MethodPost, "/session-transfer/cancel", `{`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("cancel error", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{cancelErr: errors.New("boom")}, true), http.MethodPost, "/session-transfer/cancel", `{"session_id":"s1"}`)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("process queue success", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{}, false), http.MethodPost, "/session-transfer/process-queue", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("process queue error", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{processErr: errors.New("boom")}, false), http.MethodPost, "/session-transfer/process-queue", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("check auto transfer true and false", func(t *testing.T) {
		body := `{"session_id":"s1","messages":[{"content":"hello","sender":"customer"}]}`
		w := request(newRouter(&unitTransferService{shouldTran: true}, false), http.MethodPost, "/session-transfer/check-auto", body)
		if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"should_transfer":true`)) {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
		w = request(newRouter(&unitTransferService{shouldTran: false}, false), http.MethodPost, "/session-transfer/check-auto", body)
		if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"should_transfer":false`)) {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("check auto bad body", func(t *testing.T) {
		w := request(newRouter(&unitTransferService{}, false), http.MethodPost, "/session-transfer/check-auto", `{"session_id":"s1"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
}
