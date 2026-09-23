package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"servify/apps/server/internal/models"
	ticketcontract "servify/apps/server/internal/modules/ticket/contract"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type fakeVisitorTicketService struct {
	ticket *models.Ticket
	err    error
	gotReq *ticketcontract.CreateVisitorTicketRequest
}

func (f *fakeVisitorTicketService) CreateVisitorTicket(ctx context.Context, req *ticketcontract.CreateVisitorTicketRequest) (*models.Ticket, error) {
	f.gotReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.ticket, nil
}

func newVisitorTicketRouter(svc *fakeVisitorTicketService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	r := gin.New()
	r.POST("/api/v1/tickets", NewVisitorTicketHandler(svc, logger).CreateVisitorTicket)
	return r
}

func visitorTicketRequest(r *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tickets", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestVisitorTicketHandlerCreatesWithAISummary(t *testing.T) {
	sessionID := "m-1"
	svc := &fakeVisitorTicketService{ticket: &models.Ticket{ID: 9, Title: "打不开页面", AISummary: "摘要", SessionID: &sessionID}}
	r := newVisitorTicketRouter(svc)

	w := visitorTicketRequest(r, `{"session_id":"m-1","title":"打不开页面","description":"d","ai_summary":"摘要"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if svc.gotReq == nil || svc.gotReq.SessionID != "m-1" || svc.gotReq.AISummary != "摘要" {
		t.Fatalf("unexpected request passthrough: %+v", svc.gotReq)
	}
	var resp models.Ticket
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != 9 || resp.AISummary != "摘要" {
		t.Fatalf("unexpected response ticket: %+v", resp)
	}
}

func TestVisitorTicketHandlerRejectsInvalidBody(t *testing.T) {
	r := newVisitorTicketRouter(&fakeVisitorTicketService{})
	w := visitorTicketRequest(r, `{"title":"缺 session_id"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestVisitorTicketHandlerMapsSessionNotFound(t *testing.T) {
	r := newVisitorTicketRouter(&fakeVisitorTicketService{err: errors.New("session not found: m-gone")})
	w := visitorTicketRequest(r, `{"session_id":"m-gone","title":"t"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestVisitorTicketHandlerMapsInternalError(t *testing.T) {
	r := newVisitorTicketRouter(&fakeVisitorTicketService{err: errors.New("db down")})
	w := visitorTicketRequest(r, `{"session_id":"m-1","title":"t"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestVisitorTicketHandlerBuildResponseCarriesAISummary(t *testing.T) {
	// 坐席侧管理面响应口径：ai_summary 非空可见、空省略（M3 验收②数据面）。
	full := buildTicketResponse(&models.Ticket{ID: 1, Title: "t", AISummary: "摘要"})
	if full.AISummary != "摘要" {
		t.Fatalf("unexpected ai_summary mapping: %+v", full)
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"ai_summary":"摘要"`)) {
		t.Fatalf("response json missing ai_summary: %s", raw)
	}
	empty := buildTicketResponse(&models.Ticket{ID: 1, Title: "t"})
	rawEmpty, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(rawEmpty, []byte("ai_summary")) {
		t.Fatalf("empty ai_summary should be omitted: %s", rawEmpty)
	}
}
