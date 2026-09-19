package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	satisfactiondelivery "servify/apps/server/internal/modules/satisfaction/delivery"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

type dxcSatisfactionRecorder struct {
	*unitSatisfactionService
	gotDateFrom *time.Time
	gotDateTo   *time.Time
}

func (r *dxcSatisfactionRecorder) ListSatisfactions(ctx context.Context, req *satisfactiondelivery.SatisfactionListRequest) ([]models.CustomerSatisfaction, int64, error) {
	r.gotDateFrom = req.DateFrom
	r.gotDateTo = req.DateTo
	return r.unitSatisfactionService.ListSatisfactions(ctx, req)
}

func dxcSatisfactionSvc() *dxcSatisfactionRecorder {
	return &dxcSatisfactionRecorder{unitSatisfactionService: &unitSatisfactionService{
		sats:  []models.CustomerSatisfaction{{ID: 1, Rating: 5}},
		total: 1,
	}}
}

func dxcSatisfactionRouter(svc *dxcSatisfactionRecorder) *gin.Engine {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	r := dxcRouter()
	r.GET("/satisfactions", NewSatisfactionHandler(svc, logger).ListSatisfactions)
	return r
}

func TestDxcSatisfactionHandlerListSatisfactions(t *testing.T) {
	t.Run("plain success", func(t *testing.T) {
		svc := dxcSatisfactionSvc()
		w := dxcDo(dxcSatisfactionRouter(svc), http.MethodGet, "/satisfactions", "")
		assert.Equal(t, http.StatusOK, w.Code)
		var got PaginatedResponse
		dxcDecode(t, w, &got)
		assert.Equal(t, int64(1), got.Total)
		assert.Nil(t, svc.gotDateFrom)
		assert.Nil(t, svc.gotDateTo)
	})

	t.Run("rfc3339 dates bind but fail plain-date reparse", func(t *testing.T) {
		svc := dxcSatisfactionSvc()
		w := dxcDo(dxcSatisfactionRouter(svc), http.MethodGet, "/satisfactions?date_from=2026-01-01T00:00:00Z&date_to=2026-02-01T00:00:00Z", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid date_from format")
	})

	t.Run("rfc3339 date_to reparse failure", func(t *testing.T) {
		svc := dxcSatisfactionSvc()
		w := dxcDo(dxcSatisfactionRouter(svc), http.MethodGet, "/satisfactions?date_to=2026-02-01T00:00:00Z", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid date_to format")
	})

	t.Run("plain date fails form binding", func(t *testing.T) {
		svc := dxcSatisfactionSvc()
		w := dxcDo(dxcSatisfactionRouter(svc), http.MethodGet, "/satisfactions?date_from=2026-01-01", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid query parameters")
	})

	t.Run("invalid rating filter", func(t *testing.T) {
		svc := dxcSatisfactionSvc()
		w := dxcDo(dxcSatisfactionRouter(svc), http.MethodGet, "/satisfactions?rating=abc", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error", func(t *testing.T) {
		svc := &dxcSatisfactionRecorder{unitSatisfactionService: &unitSatisfactionService{listErr: errors.New("boom")}}
		w := dxcDo(dxcSatisfactionRouter(svc), http.MethodGet, "/satisfactions", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to list satisfactions")
	})
}
