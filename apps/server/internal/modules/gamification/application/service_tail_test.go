package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	gamificationapp "servify/apps/server/internal/modules/gamification/application"
)

type flakyLeaderboardRepo struct {
	profilesErr error
	resolvedErr error
	csatsErr    error
	profiles    []gamificationapp.AgentProfile
	resolved    []gamificationapp.AgentResolvedCount
	csats       []gamificationapp.AgentCSAT
}

func (r *flakyLeaderboardRepo) ListAgentProfiles(ctx context.Context, department string) ([]gamificationapp.AgentProfile, error) {
	if r.profilesErr != nil {
		return nil, r.profilesErr
	}
	return r.profiles, nil
}

func (r *flakyLeaderboardRepo) ListResolvedCounts(ctx context.Context, startDate, endDate string) ([]gamificationapp.AgentResolvedCount, error) {
	if r.resolvedErr != nil {
		return nil, r.resolvedErr
	}
	return r.resolved, nil
}

func (r *flakyLeaderboardRepo) ListCSATStats(ctx context.Context, startDate, endDate string) ([]gamificationapp.AgentCSAT, error) {
	if r.csatsErr != nil {
		return nil, r.csatsErr
	}
	return r.csats, nil
}

func TestServiceGetLeaderboard_RepoErrorBranches(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	req := &gamificationapp.LeaderboardRequest{StartDate: start, EndDate: end}

	cases := []struct {
		name string
		repo *flakyLeaderboardRepo
		want string
	}{
		{"profiles error", &flakyLeaderboardRepo{profilesErr: errors.New("profiles down")}, "profiles down"},
		{"resolved error", &flakyLeaderboardRepo{resolvedErr: errors.New("resolved down")}, "resolved down"},
		{"csats error", &flakyLeaderboardRepo{csatsErr: errors.New("csats down")}, "csats down"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := gamificationapp.NewService(tc.repo).GetLeaderboard(ctx, req)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestServiceGetLeaderboard_SkipsIdleAgentsAndTieBreaks(t *testing.T) {
	ctx := context.Background()
	repo := &flakyLeaderboardRepo{
		profiles: []gamificationapp.AgentProfile{
			{UserID: 5, Username: "a5", Name: "Five", Department: "support"},
			{UserID: 7, Username: "a7", Name: "Seven", Department: "support"},
			{UserID: 3, Username: "a3", Name: "Three", Department: "support"},
			{UserID: 9, Username: "a9", Name: "Idle", Department: "support"},
			{UserID: 11, Username: "a11", Name: "Low", Department: "support"},
		},
		resolved: []gamificationapp.AgentResolvedCount{
			{AgentID: 5, Count: 4},
			{AgentID: 7, Count: 9},
			{AgentID: 3, Count: 9},
			{AgentID: 11, Count: 2},
		},
		csats: []gamificationapp.AgentCSAT{
			{AgentID: 5, Avg: 5.0, Count: 2},
		},
	}

	resp, err := gamificationapp.NewService(repo).GetLeaderboard(ctx, &gamificationapp.LeaderboardRequest{
		StartDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// agent 9 无解决量也无 CSAT -> 跳过
	if len(resp.Entries) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(resp.Entries), resp.Entries)
	}
	// 同分 90（4*10+5.0*10 == 9*10）时先按解决量降序，再按 AgentID 升序；
	// agent 11 分数 20 最低排最后，覆盖分数不等的比较分支。
	wantOrder := []uint{3, 7, 5, 11}
	for i, want := range wantOrder {
		if resp.Entries[i].AgentID != want {
			t.Fatalf("entry[%d].AgentID = %d, want %d (entries: %+v)", i, resp.Entries[i].AgentID, want, resp.Entries)
		}
		if resp.Entries[i].Rank != i+1 {
			t.Fatalf("entry[%d].Rank = %d, want %d", i, resp.Entries[i].Rank, i+1)
		}
	}
}
