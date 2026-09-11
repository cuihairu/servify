//go:build integration
// +build integration

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	gamificationcontract "servify/apps/server/internal/modules/gamification/contract"
	gamificationdelivery "servify/apps/server/internal/modules/gamification/delivery"
)

func newTestDBForGamification(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := uniqueMemDSN("file:gamification_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.User{}, &models.Agent{}, &models.Ticket{}, &models.CustomerSatisfaction{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestGamificationHandler_Leaderboard_BadgesAndOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTestDBForGamification(t)
	svc := gamificationdelivery.NewHandlerService(db)
	h := NewGamificationHandler(svc)

	// seed agents
	u1 := &models.User{ID: 101, Username: "a1", Email: "a1@example.com", Name: "Agent One", Role: "agent", Status: "active"}
	u2 := &models.User{ID: 102, Username: "a2", Email: "a2@example.com", Name: "Agent Two", Role: "agent", Status: "active"}
	if err := db.Create(u1).Error; err != nil {
		t.Fatalf("seed u1: %v", err)
	}
	if err := db.Create(u2).Error; err != nil {
		t.Fatalf("seed u2: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: u1.ID, Department: "support", AvgResponseTime: 120}).Error; err != nil {
		t.Fatalf("seed agent1: %v", err)
	}
	if err := db.Create(&models.Agent{UserID: u2.ID, Department: "support", AvgResponseTime: 30}).Error; err != nil {
		t.Fatalf("seed agent2: %v", err)
	}

	now := time.Now()
	start := now.AddDate(0, 0, -7).Truncate(24 * time.Hour)
	end := now.Truncate(24 * time.Hour).Add(24*time.Hour - time.Nanosecond)

	// tickets: agent1 resolved 3, agent2 resolved 1
	for i := 0; i < 3; i++ {
		resolvedAt := now.Add(-time.Duration(i) * time.Hour)
		if err := db.Create(&models.Ticket{
			Title:      "t",
			CustomerID: 1,
			AgentID:    ptrUint(u1.ID),
			Status:     "resolved",
			CreatedAt:  now.Add(-48 * time.Hour),
			UpdatedAt:  now.Add(-48 * time.Hour),
			ResolvedAt: &resolvedAt,
		}).Error; err != nil {
			t.Fatalf("seed ticket a1: %v", err)
		}
	}
	resolvedAt := now.Add(-2 * time.Hour)
	if err := db.Create(&models.Ticket{
		Title:      "t",
		CustomerID: 1,
		AgentID:    ptrUint(u2.ID),
		Status:     "resolved",
		CreatedAt:  now.Add(-24 * time.Hour),
		UpdatedAt:  now.Add(-24 * time.Hour),
		ResolvedAt: &resolvedAt,
	}).Error; err != nil {
		t.Fatalf("seed ticket a2: %v", err)
	}

	// csat: agent2 has 3 ratings of 5, agent1 has 1 rating of 4
	for i := 0; i < 3; i++ {
		if err := db.Create(&models.CustomerSatisfaction{
			TicketID:   uint(i + 1),
			CustomerID: 1,
			AgentID:    ptrUint(u2.ID),
			Rating:     5,
			Category:   "overall",
			CreatedAt:  now.Add(-time.Duration(i) * time.Hour),
		}).Error; err != nil {
			t.Fatalf("seed csat a2: %v", err)
		}
	}
	if err := db.Create(&models.CustomerSatisfaction{
		TicketID:   99,
		CustomerID: 1,
		AgentID:    ptrUint(u1.ID),
		Rating:     4,
		Category:   "overall",
		CreatedAt:  now.Add(-3 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed csat a1: %v", err)
	}

	r := gin.New()
	api := r.Group("/api")
	RegisterGamificationRoutes(api, h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/gamification/leaderboard?start_date="+start.Format("2006-01-02")+"&end_date="+end.Format("2006-01-02")+"&limit=10", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp gamificationcontract.LeaderboardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries got %d", len(resp.Entries))
	}

	// agent2 should rank higher due to CSAT weight and speed.
	if resp.Entries[0].AgentID != u2.ID {
		t.Fatalf("expected rank1 agent2 got %d", resp.Entries[0].AgentID)
	}

	// badges:
	// - top_resolver should be on agent1
	// - customer_hero should be on agent2
	// - speedster should be on agent2
	entryByID := map[uint]gamificationcontract.LeaderboardEntry{}
	for _, e := range resp.Entries {
		entryByID[e.AgentID] = e
	}
	hasBadge := func(e gamificationcontract.LeaderboardEntry, id string) bool {
		for _, b := range e.Badges {
			if b.ID == id {
				return true
			}
		}
		return false
	}
	if !hasBadge(entryByID[u1.ID], "top_resolver") {
		t.Fatalf("agent1 expected top_resolver badge")
	}
	if !hasBadge(entryByID[u2.ID], "customer_hero") || !hasBadge(entryByID[u2.ID], "speedster") {
		t.Fatalf("agent2 expected customer_hero + speedster badges, got=%v", entryByID[u2.ID].Badges)
	}
}

func ptrUint(v uint) *uint { return &v }
