package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gamificationcontract "servify/apps/server/internal/modules/gamification/contract"
	gamificationdelivery "servify/apps/server/internal/modules/gamification/delivery"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type dxcGamificationRecorder struct {
	resp    *gamificationcontract.LeaderboardResponse
	err     error
	lastReq *gamificationdelivery.LeaderboardRequest
}

func (g *dxcGamificationRecorder) GetLeaderboard(ctx context.Context, req *gamificationdelivery.LeaderboardRequest) (*gamificationcontract.LeaderboardResponse, error) {
	g.lastReq = req
	if g.err != nil {
		return nil, g.err
	}
	return g.resp, nil
}

func dxcLeaderboardRouter(rec *dxcGamificationRecorder) *gin.Engine {
	r := dxcRouter()
	r.GET("/gamification/leaderboard", NewGamificationHandler(rec).GetLeaderboard)
	return r
}

func TestDxcNewGamificationHandler(t *testing.T) {
	rec := &dxcGamificationRecorder{}
	h := NewGamificationHandler(rec)
	assert.NotNil(t, h)
	assert.Equal(t, rec, h.service)
}

func TestDxcGamificationHandlerGetLeaderboard(t *testing.T) {
	t.Run("explicit dates and params", func(t *testing.T) {
		rec := &dxcGamificationRecorder{resp: &gamificationcontract.LeaderboardResponse{Limit: 3}}
		r := dxcLeaderboardRouter(rec)
		w := dxcDo(r, http.MethodGet, "/gamification/leaderboard?start_date=2026-01-01&end_date=2026-01-31&limit=3&department=sales", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.NotNil(t, rec.lastReq)
		assert.Equal(t, 3, rec.lastReq.Limit)
		assert.Equal(t, "sales", rec.lastReq.Department)
		wantStart, _ := time.Parse("2006-01-02", "2026-01-01")
		wantEnd, _ := time.Parse("2006-01-02", "2026-01-31")
		wantEnd = wantEnd.Add(24*time.Hour - time.Nanosecond)
		assert.True(t, rec.lastReq.StartDate.Equal(wantStart))
		assert.True(t, rec.lastReq.EndDate.Equal(wantEnd))
	})

	t.Run("invalid start_date", func(t *testing.T) {
		rec := &dxcGamificationRecorder{}
		r := dxcLeaderboardRouter(rec)
		w := dxcDo(r, http.MethodGet, "/gamification/leaderboard?start_date=2026/01/01&end_date=2026-01-31", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid start_date format")
	})

	t.Run("invalid end_date", func(t *testing.T) {
		rec := &dxcGamificationRecorder{}
		r := dxcLeaderboardRouter(rec)
		w := dxcDo(r, http.MethodGet, "/gamification/leaderboard?start_date=2026-01-01&end_date=31-01-2026", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid end_date format")
	})

	t.Run("default days window", func(t *testing.T) {
		rec := &dxcGamificationRecorder{}
		r := dxcLeaderboardRouter(rec)
		w := dxcDo(r, http.MethodGet, "/gamification/leaderboard", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 10, rec.lastReq.Limit)
		assert.WithinDuration(t, time.Now(), rec.lastReq.EndDate, time.Minute)
		assert.WithinDuration(t, time.Now().AddDate(0, 0, -7), rec.lastReq.StartDate, time.Minute)
	})

	t.Run("negative days clamped to 7", func(t *testing.T) {
		rec := &dxcGamificationRecorder{}
		r := dxcLeaderboardRouter(rec)
		w := dxcDo(r, http.MethodGet, "/gamification/leaderboard?days=-5", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.WithinDuration(t, time.Now().AddDate(0, 0, -7), rec.lastReq.StartDate, time.Minute)
	})

	t.Run("large days clamped to 365", func(t *testing.T) {
		rec := &dxcGamificationRecorder{}
		r := dxcLeaderboardRouter(rec)
		w := dxcDo(r, http.MethodGet, "/gamification/leaderboard?days=400", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.WithinDuration(t, time.Now().AddDate(0, 0, -365), rec.lastReq.StartDate, time.Minute)
	})

	t.Run("invalid limit falls back to default", func(t *testing.T) {
		rec := &dxcGamificationRecorder{}
		r := dxcLeaderboardRouter(rec)
		w := dxcDo(r, http.MethodGet, "/gamification/leaderboard?limit=abc", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 10, rec.lastReq.Limit)
	})

	t.Run("service error", func(t *testing.T) {
		r := dxcLeaderboardRouter(&dxcGamificationRecorder{err: errors.New("boom")})
		w := dxcDo(r, http.MethodGet, "/gamification/leaderboard", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to get leaderboard")
	})
}

func TestDxcRegisterGamificationRoutes(t *testing.T) {
	r := dxcRouter()
	RegisterGamificationRoutes(&r.RouterGroup, NewGamificationHandler(&dxcGamificationRecorder{}))
	w := dxcDo(r, http.MethodGet, "/gamification/leaderboard", "")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDxcParseIntQuery(t *testing.T) {
	cases := []struct {
		query string
		want  int
	}{
		{"", 10},
		{"?limit=42", 42},
		{"?limit=abc", 10},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/x"+tc.query, nil)
		assert.Equal(t, tc.want, parseIntQuery(c, "limit", 10))
	}
}
