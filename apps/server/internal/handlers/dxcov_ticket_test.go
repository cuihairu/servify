package handlers

import (
	"context"
	"encoding/csv"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	ticketcontract "servify/apps/server/internal/modules/ticket/contract"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

type dxcTicketExportRecorder struct {
	*unitTicketService
	lastPageSize int
}

func (r *dxcTicketExportRecorder) ListTickets(ctx context.Context, req *ticketcontract.ListTicketRequest) ([]models.Ticket, int64, error) {
	r.lastPageSize = req.PageSize
	return r.unitTicketService.ListTickets(ctx, req)
}

func dxcExportTicketFixture() *models.Ticket {
	agentID := uint(7)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return &models.Ticket{
		ID:         42,
		Title:      "t1",
		CustomerID: 3,
		AgentID:    &agentID,
		Status:     "open",
		Priority:   "high",
		Category:   "tech",
		Tags:       "a,b",
		CustomFieldValues: []models.TicketCustomFieldValue{
			{CustomField: models.CustomField{Key: "env"}, Value: "prod"},
			{CustomField: models.CustomField{Key: ""}, Value: "skip"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func dxcExportSvc(fields []models.CustomField, tickets []models.Ticket) *dxcTicketExportRecorder {
	return &dxcTicketExportRecorder{unitTicketService: &unitTicketService{fields: fields, tickets: tickets, total: int64(len(tickets))}}
}

func dxcExportRouter(svc *dxcTicketExportRecorder) *gin.Engine {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	r := dxcRouter()
	r.GET("/tickets/export", NewTicketHandler(svc, logger).ExportTicketsCSV)
	return r
}

func TestDxcTicketExportCSVSucc(t *testing.T) {
	fields := []models.CustomField{{Key: "env", Name: "Env"}}
	tickets := []models.Ticket{
		*dxcExportTicketFixture(),
		{ID: 43, Title: "t2", CustomerID: 4, Status: "closed", Priority: "low", Category: "bill", CreatedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)},
	}
	svc := dxcExportSvc(fields, tickets)
	w := dxcDo(dxcExportRouter(svc), http.MethodGet, "/tickets/export?cf.env=prod&cf_x=y&limit=99999", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/csv; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "tickets_")
	assert.Equal(t, 5000, svc.lastPageSize)

	rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	assert.NoError(t, err)
	assert.Equal(t, 3, len(rows))
	assert.Equal(t, []string{"id", "title", "status", "priority", "category", "customer_id", "agent_id", "tags", "created_at", "updated_at", "cf.env"}, rows[0])
	assert.Equal(t, "42", rows[1][0])
	assert.Equal(t, "7", rows[1][6])
	assert.Equal(t, "prod", rows[1][10])
	assert.Equal(t, "", rows[2][6])
	assert.Equal(t, "", rows[2][10])
}

func TestDxcTicketExportCSVLimits(t *testing.T) {
	cases := []struct {
		name   string
		query  string
		wantPS int
	}{
		{"default", "", 1000},
		{"zero", "?limit=0", 1000},
		{"negative", "?limit=-3", 1000},
		{"invalid", "?limit=abc", 1000},
		{"cap", "?limit=20000", 5000},
		{"explicit", "?limit=250", 250},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := dxcExportSvc(nil, nil)
			w := dxcDo(dxcExportRouter(svc), http.MethodGet, "/tickets/export"+tc.query, "")
			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, tc.wantPS, svc.lastPageSize)
		})
	}
}

func TestDxcTicketExportCSVErrors(t *testing.T) {
	t.Run("custom fields error", func(t *testing.T) {
		svc := &dxcTicketExportRecorder{unitTicketService: &unitTicketService{fieldsErr: errors.New("boom")}}
		w := dxcDo(dxcExportRouter(svc), http.MethodGet, "/tickets/export", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to load custom fields")
	})

	t.Run("list error", func(t *testing.T) {
		svc := &dxcTicketExportRecorder{unitTicketService: &unitTicketService{listErr: errors.New("boom")}}
		w := dxcDo(dxcExportRouter(svc), http.MethodGet, "/tickets/export", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to export tickets")
	})

	t.Run("invalid query", func(t *testing.T) {
		svc := dxcExportSvc(nil, nil)
		w := dxcDo(dxcExportRouter(svc), http.MethodGet, "/tickets/export?page=abc", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid query parameters")
	})
}
