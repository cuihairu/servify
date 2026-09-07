package handlers

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	ticketcontract "servify/apps/server/internal/modules/ticket/contract"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func newTicketUnitRouter(svc *unitTicketService, withUser bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	h := NewTicketHandler(svc, logger)
	r := gin.New()
	if withUser {
		r.Use(func(c *gin.Context) {
			c.Set("user_id", uint(5))
			c.Next()
		})
	}
	RegisterTicketRoutes(&r.RouterGroup, h)
	return r
}

func ticketUnitRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func unitTicketFixture() *models.Ticket {
	agentID := uint(7)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return &models.Ticket{
		ID:          42,
		Title:       "t1",
		Description: "d1",
		CustomerID:  3,
		Customer:    models.User{ID: 3, Name: "cust"},
		AgentID:     &agentID,
		Agent:       &models.User{ID: 7, Name: "ag"},
		Category:    "tech",
		Priority:    "high",
		Status:      "open",
		Tags:        "a, b",
		CustomFieldValues: []models.TicketCustomFieldValue{
			{CustomField: models.CustomField{Key: "env"}, Value: "prod"},
			{CustomField: models.CustomField{Key: ""}, Value: "skip"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestTicketHandlerUnitBuilders(t *testing.T) {
	if buildTicketResponse(nil) != nil {
		t.Fatal("nil ticket should produce nil response")
	}
	if got := buildTicketResponses(nil); len(got) != 0 {
		t.Fatalf("empty list should produce empty, got %v", got)
	}

	ticket := unitTicketFixture()
	resp := buildTicketResponse(ticket)
	if resp == nil || resp.CustomerName != "cust" || resp.AgentName != "ag" {
		t.Fatalf("unexpected response %+v", resp)
	}
	if len(resp.TagList) != 2 || resp.CustomFields["env"] != "prod" || len(resp.CustomFields) != 1 {
		t.Fatalf("unexpected tag/custom fields %+v", resp)
	}

	noNames := &models.Ticket{ID: 1, Customer: models.User{}, Agent: &models.User{}}
	if r := buildTicketResponse(noNames); r.CustomerName != "" || r.AgentName != "" {
		t.Fatalf("expected empty names, got %+v", r)
	}

	if got := buildTicketCustomFields(&models.Ticket{}); got != nil {
		t.Fatalf("expected nil custom fields, got %v", got)
	}
	if got := buildTicketCustomFields(&models.Ticket{CustomFieldValues: []models.TicketCustomFieldValue{{CustomField: models.CustomField{Key: ""}}}}); got != nil {
		t.Fatalf("expected nil for empty keys, got %v", got)
	}

	if splitCSV("") != nil || splitCSV(" , ") != nil {
		t.Fatal("expected nil for blank csv")
	}
	if got := splitCSV(" a , b "); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("unexpected split %v", got)
	}
}

func TestTicketHandlerUnitCreate(t *testing.T) {
	body := `{"title":"t","customer_id":3}`
	cases := []struct {
		name string
		svc  *unitTicketService
		body string
		want int
	}{
		{"success", &unitTicketService{ticket: unitTicketFixture()}, body, http.StatusCreated},
		{"invalid body", &unitTicketService{}, `{`, http.StatusBadRequest},
		{"service error", &unitTicketService{createErr: errors.New("boom")}, body, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newTicketUnitRouter(tc.svc, false)
			w := ticketUnitRequest(r, http.MethodPost, "/tickets", tc.body)
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestTicketHandlerUnitGet(t *testing.T) {
	cases := []struct {
		name string
		svc  *unitTicketService
		path string
		want int
	}{
		{"success", &unitTicketService{ticket: unitTicketFixture()}, "/tickets/42", http.StatusOK},
		{"bad id", &unitTicketService{}, "/tickets/xx", http.StatusBadRequest},
		{"not found", &unitTicketService{getErr: errors.New("record not found")}, "/tickets/42", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newTicketUnitRouter(tc.svc, false)
			w := ticketUnitRequest(r, http.MethodGet, tc.path, "")
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestTicketHandlerUnitUpdate(t *testing.T) {
	body := `{"title":"t2"}`
	cases := []struct {
		name string
		svc  *unitTicketService
		user bool
		path string
		want int
	}{
		{"success with user", &unitTicketService{ticket: unitTicketFixture()}, true, "/tickets/42", http.StatusOK},
		{"success without user", &unitTicketService{ticket: unitTicketFixture()}, false, "/tickets/42", http.StatusOK},
		{"bad id", &unitTicketService{}, true, "/tickets/abc", http.StatusBadRequest},
		{"invalid body", &unitTicketService{}, true, "/tickets/42", http.StatusBadRequest},
		{"not found error", &unitTicketService{updateErr: errors.New("ticket not found")}, true, "/tickets/42", http.StatusNotFound},
		{"validation error", &unitTicketService{updateErr: errors.New("title is required")}, true, "/tickets/42", http.StatusBadRequest},
		{"must be error", &unitTicketService{updateErr: errors.New("priority must be one of")}, true, "/tickets/42", http.StatusBadRequest},
		{"conflict error", &unitTicketService{updateErr: errors.New("status transition not allowed")}, true, "/tickets/42", http.StatusConflict},
		{"already error", &unitTicketService{updateErr: errors.New("agent already assigned")}, true, "/tickets/42", http.StatusConflict},
		{"forbidden error", &unitTicketService{updateErr: errors.New("permission denied")}, true, "/tickets/42", http.StatusForbidden},
		{"internal error", &unitTicketService{updateErr: errors.New("boom")}, true, "/tickets/42", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sendBody := body
			if tc.name == "invalid body" {
				sendBody = "{"
			}
			r := newTicketUnitRouter(tc.svc, tc.user)
			w := ticketUnitRequest(r, http.MethodPut, tc.path, sendBody)
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}

	t.Run("before snapshot lookup fails but update succeeds", func(t *testing.T) {
		svc := &unitTicketService{ticket: unitTicketFixture()}
		svc.getErr = errors.New("lookup failed")
		svc.ticket = unitTicketFixture()
		r := newTicketUnitRouter(svc, true)
		w := ticketUnitRequest(r, http.MethodPut, "/tickets/42", body)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestTicketHandlerUnitList(t *testing.T) {
	t.Run("success with custom field filters", func(t *testing.T) {
		svc := &unitTicketService{tickets: []models.Ticket{*unitTicketFixture()}, total: 1}
		r := newTicketUnitRouter(svc, false)
		w := ticketUnitRequest(r, http.MethodGet, "/tickets?page=1&page_size=10&status=open&cf.env=prod&cf_region=eu&cf.empty=&cf.=x", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("bad query", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{}, false)
		w := ticketUnitRequest(r, http.MethodGet, "/tickets?page=zz", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("error", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{listErr: errors.New("boom")}, false)
		w := ticketUnitRequest(r, http.MethodGet, "/tickets", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestTicketHandlerUnitExportCSV(t *testing.T) {
	t.Run("success with custom fields", func(t *testing.T) {
		svc := &unitTicketService{
			tickets: []models.Ticket{*unitTicketFixture()},
			total:   1,
			fields:  []models.CustomField{{Key: "env"}},
		}
		r := newTicketUnitRouter(svc, false)
		w := ticketUnitRequest(r, http.MethodGet, "/tickets/export?limit=50", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
		reader := csv.NewReader(strings.NewReader(w.Body.String()))
		records, err := reader.ReadAll()
		if err != nil {
			t.Fatalf("parse csv: %v", err)
		}
		if len(records) != 2 || records[0][len(records[0])-1] != "cf.env" || records[1][len(records[1])-1] != "prod" {
			t.Fatalf("unexpected csv %+v", records)
		}
	})

	t.Run("limit clamping", func(t *testing.T) {
		svc := &unitTicketService{}
		r := newTicketUnitRouter(svc, false)
		for _, q := range []string{"?limit=0", "?limit=-5", "?limit=99999", ""} {
			w := ticketUnitRequest(r, http.MethodGet, "/tickets/export"+q, "")
			if w.Code != http.StatusOK {
				t.Fatalf("query %s status = %d", q, w.Code)
			}
		}
	})

	t.Run("bad query", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{}, false)
		w := ticketUnitRequest(r, http.MethodGet, "/tickets/export?page=zz", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("fields error", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{fieldsErr: errors.New("boom")}, false)
		w := ticketUnitRequest(r, http.MethodGet, "/tickets/export", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("list error", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{listErr: errors.New("boom")}, false)
		w := ticketUnitRequest(r, http.MethodGet, "/tickets/export", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("agentless row", func(t *testing.T) {
		ticket := unitTicketFixture()
		ticket.AgentID = nil
		svc := &unitTicketService{tickets: []models.Ticket{*ticket}, total: 1}
		r := newTicketUnitRouter(svc, false)
		w := ticketUnitRequest(r, http.MethodGet, "/tickets/export", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestTicketHandlerUnitAssign(t *testing.T) {
	body := `{"agent_id":7}`
	cases := []struct {
		name string
		svc  *unitTicketService
		user bool
		body string
		want int
	}{
		{"success with user", &unitTicketService{ticket: unitTicketFixture()}, true, body, http.StatusOK},
		{"success without user", &unitTicketService{ticket: unitTicketFixture()}, false, body, http.StatusOK},
		{"missing agent", &unitTicketService{}, true, `{}`, http.StatusBadRequest},
		{"bad id", &unitTicketService{}, true, body, http.StatusBadRequest},
		{"not found", &unitTicketService{assignErr: errors.New("ticket not found")}, true, body, http.StatusNotFound},
		{"agent missing", &unitTicketService{assignErr: errors.New("agent not found")}, true, body, http.StatusNotFound},
		{"forbidden", &unitTicketService{assignErr: errors.New("permission denied")}, true, body, http.StatusForbidden},
		{"internal", &unitTicketService{assignErr: errors.New("boom")}, true, body, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := "/tickets/42/assign"
			if tc.name == "bad id" {
				path = "/tickets/zz/assign"
			}
			r := newTicketUnitRouter(tc.svc, tc.user)
			w := ticketUnitRequest(r, http.MethodPost, path, tc.body)
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestTicketHandlerUnitCommentsAndClose(t *testing.T) {
	commentBody := `{"content":"hello","type":"public"}`
	t.Run("comment bad id", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{}, true)
		w := ticketUnitRequest(r, http.MethodPost, "/tickets/zz/comments", commentBody)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("comment invalid body", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{}, true)
		w := ticketUnitRequest(r, http.MethodPost, "/tickets/42/comments", `{}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("comment unauthorized", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{}, false)
		w := ticketUnitRequest(r, http.MethodPost, "/tickets/42/comments", commentBody)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", w.Code)
		}
	})
	commentCases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", errors.New("ticket not found"), http.StatusNotFound},
		{"validation", errors.New("content required"), http.StatusBadRequest},
		{"forbidden", errors.New("unauthorized actor"), http.StatusForbidden},
		{"internal", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range commentCases {
		t.Run("comment "+tc.name, func(t *testing.T) {
			r := newTicketUnitRouter(&unitTicketService{commentErr: tc.err}, true)
			w := ticketUnitRequest(r, http.MethodPost, "/tickets/42/comments", commentBody)
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d", w.Code, tc.want)
			}
		})
	}
	t.Run("comment success", func(t *testing.T) {
		svc := &unitTicketService{comment: &models.TicketComment{ID: 9, Content: "hello"}}
		r := newTicketUnitRouter(svc, true)
		w := ticketUnitRequest(r, http.MethodPost, "/tickets/42/comments", commentBody)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})

	closeBody := `{"reason":"done"}`
	t.Run("close bad id", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{}, true)
		w := ticketUnitRequest(r, http.MethodPost, "/tickets/zz/close", closeBody)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("close invalid body", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{}, true)
		w := ticketUnitRequest(r, http.MethodPost, "/tickets/42/close", `{`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("close unauthorized", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{}, false)
		w := ticketUnitRequest(r, http.MethodPost, "/tickets/42/close", closeBody)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("close conflict", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{closeErr: errors.New("status transition not allowed")}, true)
		w := ticketUnitRequest(r, http.MethodPost, "/tickets/42/close", closeBody)
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("close internal", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{closeErr: errors.New("boom")}, true)
		w := ticketUnitRequest(r, http.MethodPost, "/tickets/42/close", closeBody)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("close success", func(t *testing.T) {
		svc := &unitTicketService{ticket: unitTicketFixture()}
		r := newTicketUnitRouter(svc, true)
		w := ticketUnitRequest(r, http.MethodPost, "/tickets/42/close", closeBody)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestTicketHandlerUnitStatsAndBulk(t *testing.T) {
	t.Run("stats no agent id", func(t *testing.T) {
		svc := &unitTicketService{stats: &ticketcontract.TicketStats{Total: 3}}
		r := newTicketUnitRouter(svc, false)
		w := ticketUnitRequest(r, http.MethodGet, "/tickets/stats", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("stats agent id variants", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{}, false)
		if w := ticketUnitRequest(r, http.MethodGet, "/tickets/stats?agent_id=4", ""); w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		if w := ticketUnitRequest(r, http.MethodGet, "/tickets/stats?agent_id=zz", ""); w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("stats error", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{statsErr: errors.New("boom")}, false)
		w := ticketUnitRequest(r, http.MethodGet, "/tickets/stats", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})

	bulkBody := `{"ticket_ids":[1,2],"status":"closed"}`
	t.Run("bulk success", func(t *testing.T) {
		svc := &unitTicketService{bulkResult: &ticketcontract.BulkUpdateResult{Updated: []uint{1, 2}}}
		r := newTicketUnitRouter(svc, true)
		w := ticketUnitRequest(r, http.MethodPost, "/tickets/bulk", bulkBody)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("bulk without user", func(t *testing.T) {
		svc := &unitTicketService{bulkResult: &ticketcontract.BulkUpdateResult{}}
		r := newTicketUnitRouter(svc, false)
		w := ticketUnitRequest(r, http.MethodPost, "/tickets/bulk", bulkBody)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("bulk invalid body", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{}, true)
		w := ticketUnitRequest(r, http.MethodPost, "/tickets/bulk", `{}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("bulk error", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{bulkErr: errors.New("boom")}, true)
		w := ticketUnitRequest(r, http.MethodPost, "/tickets/bulk", bulkBody)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestTicketHandlerUnitRelatedConversations(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &unitTicketService{sessions: []models.Session{{ID: "s1"}}}
		r := newTicketUnitRouter(svc, false)
		w := ticketUnitRequest(r, http.MethodGet, "/tickets/42/conversations", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("bad id", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{}, false)
		w := ticketUnitRequest(r, http.MethodGet, "/tickets/zz/conversations", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", w.Code)
		}
	})
	t.Run("error", func(t *testing.T) {
		r := newTicketUnitRouter(&unitTicketService{getErr: errors.New("boom")}, false)
		w := ticketUnitRequest(r, http.MethodGet, "/tickets/42/conversations", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestTicketHandlerUnitErrorMapping(t *testing.T) {
	if ticketErrorToStatusCode(nil) != http.StatusOK {
		t.Fatal("nil error should map to 200")
	}
	cases := []struct {
		err  error
		want int
	}{
		{errors.New("ticket not found"), http.StatusNotFound},
		{errors.New("record not found"), http.StatusNotFound},
		{errors.New("field required"), http.StatusBadRequest},
		{errors.New("invalid value"), http.StatusBadRequest},
		{errors.New("priority must be high"), http.StatusBadRequest},
		{errors.New("validation failed"), http.StatusBadRequest},
		{errors.New("already assigned"), http.StatusConflict},
		{errors.New("state conflict"), http.StatusConflict},
		{errors.New("agent not available"), http.StatusConflict},
		{errors.New("bad status transition"), http.StatusConflict},
		{errors.New("permission denied"), http.StatusForbidden},
		{errors.New("forbidden action"), http.StatusForbidden},
		{errors.New("unauthorized user"), http.StatusForbidden},
		{errors.New("mystery"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		if got := ticketErrorToStatusCode(tc.err); got != tc.want {
			t.Fatalf("ticketErrorToStatusCode(%q) = %d want %d", tc.err, got, tc.want)
		}
	}
}

func TestTicketHandlerUnitExtractCustomFieldFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var captured map[string]string
	r.GET("/probe", func(c *gin.Context) {
		captured = extractCustomFieldFilters(c)
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/probe?cf.a=1&cf_b=2&cf.empty=%20&other=3&cf.=x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if len(captured) != 2 || captured["a"] != "1" || captured["b"] != "2" {
		t.Fatalf("unexpected filters %+v", captured)
	}
}

var _ = json.Marshal
var _ = context.Background
