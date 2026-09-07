package application_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	gamificationapp "servify/apps/server/internal/modules/gamification/application"
)

type leaderboardRepoStub struct {
	department string
	startDate  string
	endDate    string
	profiles   []gamificationapp.AgentProfile
	resolved   []gamificationapp.AgentResolvedCount
	csats      []gamificationapp.AgentCSAT
}

func (r *leaderboardRepoStub) ListAgentProfiles(ctx context.Context, department string) ([]gamificationapp.AgentProfile, error) {
	r.department = department
	out := make([]gamificationapp.AgentProfile, 0, len(r.profiles))
	for _, profile := range r.profiles {
		if department != "" && profile.Department != department {
			continue
		}
		out = append(out, profile)
	}
	return out, nil
}

func (r *leaderboardRepoStub) ListResolvedCounts(ctx context.Context, startDate, endDate string) ([]gamificationapp.AgentResolvedCount, error) {
	r.startDate = startDate
	r.endDate = endDate
	out := make([]gamificationapp.AgentResolvedCount, len(r.resolved))
	copy(out, r.resolved)
	return out, nil
}

func (r *leaderboardRepoStub) ListCSATStats(ctx context.Context, startDate, endDate string) ([]gamificationapp.AgentCSAT, error) {
	r.startDate = startDate
	r.endDate = endDate
	out := make([]gamificationapp.AgentCSAT, len(r.csats))
	copy(out, r.csats)
	return out, nil
}

func hasBadge(entry gamificationapp.LeaderboardEntry, id string) bool {
	for _, badge := range entry.Badges {
		if badge.ID == id {
			return true
		}
	}
	return false
}

func TestServiceGetLeaderboard_SortsAppliesBadgesAndTrimsDepartment(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	end := time.Date(2026, 1, 3, 4, 5, 6, 0, time.UTC)
	repo := &leaderboardRepoStub{
		profiles: []gamificationapp.AgentProfile{
			{UserID: 10, Username: "alice", Name: "Alice", Department: "support", AvgResponseTime: 100},
			{UserID: 11, Username: "bob", Name: "Bob", Department: "support", AvgResponseTime: 100},
			{UserID: 12, Username: "mallory", Name: "Mallory", Department: "sales", AvgResponseTime: 80},
		},
		resolved: []gamificationapp.AgentResolvedCount{
			{AgentID: 10, Count: 3},
			{AgentID: 11, Count: 3},
			{AgentID: 12, Count: 2},
		},
		csats: []gamificationapp.AgentCSAT{
			{AgentID: 10, Avg: 5, Count: 4},
			{AgentID: 11, Avg: 5, Count: 4},
			{AgentID: 12, Avg: 4, Count: 4},
		},
	}
	svc := gamificationapp.NewService(repo)

	resp, err := svc.GetLeaderboard(context.Background(), &gamificationapp.LeaderboardRequest{
		StartDate:  start,
		EndDate:    end,
		Limit:      10,
		Department: " support ",
	})
	if err != nil {
		t.Fatalf("GetLeaderboard() error = %v", err)
	}
	if repo.department != "support" {
		t.Fatalf("unexpected department filter: %q", repo.department)
	}
	if repo.startDate != "2026-01-02 03:04:05" || repo.endDate != "2026-01-03 04:05:06" {
		t.Fatalf("unexpected date range: %s -> %s", repo.startDate, repo.endDate)
	}
	if resp.Limit != 10 {
		t.Fatalf("unexpected limit: %d", resp.Limit)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("unexpected entry count: %d", len(resp.Entries))
	}
	if resp.Entries[0].AgentID != 10 || resp.Entries[1].AgentID != 11 {
		t.Fatalf("unexpected leaderboard order: %+v", resp.Entries)
	}
	if resp.Entries[0].Rank != 1 || resp.Entries[1].Rank != 2 {
		t.Fatalf("unexpected ranks: %+v", resp.Entries)
	}
	if !hasBadge(resp.Entries[0], "top_resolver") || !hasBadge(resp.Entries[0], "customer_hero") || !hasBadge(resp.Entries[0], "speedster") {
		t.Fatalf("unexpected badges for top entry: %+v", resp.Entries[0].Badges)
	}
	if len(resp.Entries[1].Badges) != 0 {
		t.Fatalf("expected no badges for second entry, got %+v", resp.Entries[1].Badges)
	}
}

func TestServiceGetLeaderboard_ValidationAndDefaultLimit(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	end := time.Date(2026, 1, 3, 4, 5, 6, 0, time.UTC)
	repo := &leaderboardRepoStub{
		profiles: []gamificationapp.AgentProfile{
			{UserID: 1, Username: "agent-1", Name: "Agent 1", Department: "support", AvgResponseTime: 100},
		},
		resolved: []gamificationapp.AgentResolvedCount{
			{AgentID: 1, Count: 1},
		},
		csats: []gamificationapp.AgentCSAT{
			{AgentID: 1, Avg: 4.5, Count: 3},
		},
	}
	svc := gamificationapp.NewService(repo)

	tests := []struct {
		name      string
		req       *gamificationapp.LeaderboardRequest
		wantErr   bool
		wantLimit int
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "missing start date",
			req:     &gamificationapp.LeaderboardRequest{EndDate: end},
			wantErr: true,
		},
		{
			name:    "missing end date",
			req:     &gamificationapp.LeaderboardRequest{StartDate: start},
			wantErr: true,
		},
		{
			name:    "end before start",
			req:     &gamificationapp.LeaderboardRequest{StartDate: end, EndDate: start},
			wantErr: true,
		},
		{
			name:      "zero limit defaults to ten",
			req:       &gamificationapp.LeaderboardRequest{StartDate: start, EndDate: end, Limit: 0},
			wantLimit: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := svc.GetLeaderboard(context.Background(), tt.req)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("GetLeaderboard() error = %v", err)
			}
			if resp.Limit != tt.wantLimit {
				t.Fatalf("unexpected limit: %d", resp.Limit)
			}
			if len(resp.Entries) != 1 {
				t.Fatalf("unexpected entry count: %d", len(resp.Entries))
			}
		})
	}
}

func TestServiceGetLeaderboard_CapsLimitAt100(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	end := time.Date(2026, 1, 3, 4, 5, 6, 0, time.UTC)
	repo := &leaderboardRepoStub{}
	for i := 1; i <= 101; i++ {
		id := uint(i)
		repo.profiles = append(repo.profiles, gamificationapp.AgentProfile{
			UserID:          id,
			Username:        fmt.Sprintf("agent-%d", i),
			Name:            fmt.Sprintf("Agent %d", i),
			Department:      "support",
			AvgResponseTime: 100,
		})
		repo.resolved = append(repo.resolved, gamificationapp.AgentResolvedCount{AgentID: id, Count: 1})
		repo.csats = append(repo.csats, gamificationapp.AgentCSAT{AgentID: id, Avg: 4.0, Count: 3})
	}
	svc := gamificationapp.NewService(repo)

	resp, err := svc.GetLeaderboard(context.Background(), &gamificationapp.LeaderboardRequest{
		StartDate: start,
		EndDate:   end,
		Limit:     500,
	})
	if err != nil {
		t.Fatalf("GetLeaderboard() error = %v", err)
	}
	if resp.Limit != 100 {
		t.Fatalf("unexpected limit: %d", resp.Limit)
	}
	if len(resp.Entries) != 100 {
		t.Fatalf("unexpected entry count: %d", len(resp.Entries))
	}
	if resp.Entries[0].AgentID != 1 || resp.Entries[99].AgentID != 100 {
		t.Fatalf("unexpected leaderboard bounds: first=%d last=%d", resp.Entries[0].AgentID, resp.Entries[99].AgentID)
	}
}
