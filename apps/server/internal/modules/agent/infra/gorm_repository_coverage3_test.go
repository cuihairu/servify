package infra

import (
	"context"
	"testing"

	agentdomain "servify/apps/server/internal/modules/agent/domain"

	"github.com/stretchr/testify/require"
)

func TestGormRepositoryUpdateErrorBranches(t *testing.T) {
	db := newAgentCoverageDB(t)
	require.NoError(t, db.Migrator().DropTable("agents"))
	repo := NewGormRepository(db)
	ctx := context.Background()

	require.ErrorContains(t, repo.UpdatePresenceStatus(ctx, 7, agentdomain.PresenceStatusOnline), "failed to update agent status")
	require.ErrorContains(t, repo.UpdateLastActivity(ctx, 7), "no such table")
	require.ErrorContains(t, repo.SetConnectedTime(ctx, 7), "no such table")
	require.ErrorContains(t, repo.ClearConnectedTime(ctx, 7), "no such table")
	require.ErrorContains(t, repo.UpdateChatLoad(ctx, 7, 1), "no such table")
}

func TestGormRepositorySessionUpdateErrorBranches(t *testing.T) {
	db := newAgentCoverageDB(t)
	require.NoError(t, db.Migrator().DropTable("sessions"))
	repo := NewGormRepository(db)
	ctx := context.Background()

	require.ErrorContains(t, repo.AssignSession(ctx, "sess-1", 7), "failed to assign session")
	require.ErrorContains(t, repo.ReleaseSession(ctx, "sess-1", 7), "failed to release session")
}
