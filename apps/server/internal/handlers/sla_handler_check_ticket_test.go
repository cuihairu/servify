package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	sladelivery "servify/apps/server/internal/modules/sla/delivery"

	"github.com/gin-gonic/gin"
)

type fakeSLAHandlerTicketService struct {
	ticket *models.Ticket
	err    error
}

func (f *fakeSLAHandlerTicketService) GetTicketByID(ctx context.Context, ticketID uint) (*models.Ticket, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.ticket, nil
}

type fakeSLAHandlerSLAService struct {
	violation *models.SLAViolation
	checkErr  error
}

func (f *fakeSLAHandlerSLAService) CreateSLAConfig(ctx context.Context, req *sladelivery.SLAConfigCreateRequest) (*models.SLAConfig, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeSLAHandlerSLAService) GetSLAConfig(ctx context.Context, id uint) (*models.SLAConfig, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeSLAHandlerSLAService) ListSLAConfigs(ctx context.Context, req *sladelivery.SLAConfigListRequest) ([]models.SLAConfig, int64, error) {
	return nil, 0, errors.New("not implemented")
}
func (f *fakeSLAHandlerSLAService) UpdateSLAConfig(ctx context.Context, id uint, req *sladelivery.SLAConfigUpdateRequest) (*models.SLAConfig, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeSLAHandlerSLAService) DeleteSLAConfig(ctx context.Context, id uint) error {
	return errors.New("not implemented")
}
func (f *fakeSLAHandlerSLAService) GetSLAConfigByPriority(ctx context.Context, priority string, customerTier string) (*models.SLAConfig, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeSLAHandlerSLAService) ListSLAViolations(ctx context.Context, req *sladelivery.SLAViolationListRequest) ([]models.SLAViolation, int64, error) {
	return nil, 0, errors.New("not implemented")
}
func (f *fakeSLAHandlerSLAService) ResolveSLAViolation(ctx context.Context, id uint) error {
	return errors.New("not implemented")
}
func (f *fakeSLAHandlerSLAService) GetSLAStats(ctx context.Context) (*sladelivery.SLAStatsResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeSLAHandlerSLAService) CheckSLAViolation(ctx context.Context, ticket *models.Ticket) (*models.SLAViolation, error) {
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	return f.violation, nil
}

func TestSLAHandlerCheckTicketSLATicketNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewSLAHandler(
		&fakeSLAHandlerSLAService{},
		&fakeSLAHandlerTicketService{err: errors.New("not found")},
	)

	r := gin.New()
	r.POST("/api/sla/check/ticket/:ticket_id", handler.CheckTicketSLA)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/sla/check/ticket/42", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSLAHandlerCheckTicketSLANoViolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewSLAHandler(
		&fakeSLAHandlerSLAService{},
		&fakeSLAHandlerTicketService{ticket: &models.Ticket{ID: 42}},
	)

	r := gin.New()
	r.POST("/api/sla/check/ticket/:ticket_id", handler.CheckTicketSLA)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/sla/check/ticket/42", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSLAHandlerCheckTicketSLAViolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now()
	handler := NewSLAHandler(
		&fakeSLAHandlerSLAService{
			violation: &models.SLAViolation{
				ID:            1,
				TicketID:      42,
				ViolationType: "resolution",
				ViolatedAt:    now,
				CreatedAt:     now,
				UpdatedAt:     now,
			},
		},
		&fakeSLAHandlerTicketService{ticket: &models.Ticket{ID: 42}},
	)

	r := gin.New()
	r.POST("/api/sla/check/ticket/:ticket_id", handler.CheckTicketSLA)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/sla/check/ticket/42", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var got models.SLAViolation
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.TicketID != 42 || got.ViolationType != "resolution" {
		t.Fatalf("unexpected response: %+v", got)
	}
}
