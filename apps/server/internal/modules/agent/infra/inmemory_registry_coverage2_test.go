package infra

import (
	"testing"

	"servify/apps/server/internal/models"
	agentapp "servify/apps/server/internal/modules/agent/application"
	agentdomain "servify/apps/server/internal/modules/agent/domain"

	"github.com/stretchr/testify/require"
)

func memProfile(userID uint) agentdomain.AgentProfile {
	return agentdomain.AgentProfile{
		UserID:             userID,
		Username:           "agent",
		Name:               "Agent",
		Department:         "support",
		Skills:             []string{"billing"},
		MaxChatConcurrency: 2,
		Rating:             4.5,
		AvgResponseTime:    30,
	}
}

func TestInMemoryRegistryGoOnlineBuildsRuntime(t *testing.T) {
	registry := NewInMemoryRegistry()

	dto, err := registry.GoOnline(memProfile(7))
	require.NoError(t, err)
	require.Equal(t, uint(7), dto.UserID)
	require.Equal(t, "agent", dto.Username)
	require.Equal(t, "Agent", dto.Name)
	require.Equal(t, "support", dto.Department)
	require.Equal(t, []string{"billing"}, dto.Skills)
	require.Equal(t, string(agentdomain.PresenceStatusOnline), dto.Status)
	require.Equal(t, 2, dto.MaxChatConcurrency)
	require.Equal(t, 4.5, dto.Rating)
	require.Equal(t, 30, dto.AvgResponseTime)
	require.False(t, dto.LastActivity.IsZero())
	require.False(t, dto.ConnectedAt.IsZero())

	got, ok := registry.Get(7)
	require.True(t, ok)
	require.Equal(t, dto.UserID, got.UserID)

	registry.GoOffline(7)
	_, ok = registry.Get(7)
	require.False(t, ok)
}

func TestInMemoryRegistryUpdateStatus(t *testing.T) {
	registry := NewInMemoryRegistry()
	_, err := registry.GoOnline(memProfile(7))
	require.NoError(t, err)

	registry.UpdateStatus(7, agentdomain.PresenceStatusBusy)
	got, ok := registry.Get(7)
	require.True(t, ok)
	require.Equal(t, string(agentdomain.PresenceStatusBusy), got.Status)

	registry.UpdateStatus(999, agentdomain.PresenceStatusAway)
	_, ok = registry.Get(999)
	require.False(t, ok)
}

func TestInMemoryRegistryAssignSessionBranches(t *testing.T) {
	registry := NewInMemoryRegistry()
	session := &models.Session{ID: "sess-1"}

	_, err := registry.AssignSession(7, session)
	require.EqualError(t, err, "agent 7 is not online")

	_, err = registry.GoOnline(memProfile(7))
	require.NoError(t, err)

	registry.agents[7].MaxChatConcurrency = 1
	registry.agents[7].CurrentChatLoad = 1
	_, err = registry.AssignSession(7, session)
	require.EqualError(t, err, "agent 7 is at maximum capacity")

	registry.agents[7].CurrentChatLoad = 0
	registry.agents[7].Status = string(agentdomain.PresenceStatusOffline)
	_, err = registry.AssignSession(7, session)
	require.EqualError(t, err, "agent 7 is offline")

	registry.agents[7].Status = string(agentdomain.PresenceStatusOnline)
	dto, err := registry.AssignSession(7, session)
	require.NoError(t, err)
	require.Equal(t, 1, dto.CurrentChatLoad)
	require.NotSame(t, session, registry.agents[7].Sessions["sess-1"], "session must be copied")
}

func TestInMemoryRegistryReleaseSessionBranches(t *testing.T) {
	registry := NewInMemoryRegistry()

	_, ok := registry.ReleaseSession(7, "sess-1")
	require.False(t, ok)

	_, err := registry.GoOnline(memProfile(7))
	require.NoError(t, err)
	registry.agents[7].CurrentChatLoad = 0
	registry.agents[7].Sessions["sess-1"] = &models.Session{ID: "sess-1"}

	dto, ok := registry.ReleaseSession(7, "sess-1")
	require.True(t, ok)
	require.Equal(t, 0, dto.CurrentChatLoad, "load must not go negative")
	require.NotContains(t, registry.agents[7].Sessions, "sess-1")

	registry.agents[7].CurrentChatLoad = 2
	registry.agents[7].Sessions["sess-2"] = &models.Session{ID: "sess-2"}
	dto, ok = registry.ReleaseSession(7, "sess-2")
	require.True(t, ok)
	require.Equal(t, 1, dto.CurrentChatLoad)
}

func TestInMemoryRegistryApplyTransferBranches(t *testing.T) {
	registry := NewInMemoryRegistry()

	// Neither agent online: no panic, no state change.
	registry.ApplyTransfer("sess-1", nil, 9)
	require.Empty(t, registry.List())

	from := uint(7)
	registry.ApplyTransfer("sess-1", &from, 9)
	require.Empty(t, registry.List())

	_, err := registry.GoOnline(memProfile(7))
	require.NoError(t, err)
	registry.agents[7].CurrentChatLoad = 1
	registry.agents[7].Sessions["sess-1"] = &models.Session{ID: "sess-1"}

	registry.ApplyTransfer("sess-1", &from, 9)
	got, ok := registry.Get(7)
	require.True(t, ok)
	require.Equal(t, 0, got.CurrentChatLoad)
	require.NotContains(t, registry.agents[7].Sessions, "sess-1")

	_, err = registry.GoOnline(memProfile(9))
	require.NoError(t, err)
	registry.ApplyTransfer("sess-1", &from, 9)
	target, ok := registry.Get(9)
	require.True(t, ok)
	require.Equal(t, 1, target.CurrentChatLoad)
	require.Contains(t, registry.agents[9].Sessions, "sess-1")
}

func TestInMemoryRegistryListSortedByUserID(t *testing.T) {
	registry := NewInMemoryRegistry()
	_, err := registry.GoOnline(memProfile(9))
	require.NoError(t, err)
	_, err = registry.GoOnline(memProfile(3))
	require.NoError(t, err)

	list := registry.List()
	require.Len(t, list, 2)
	require.Equal(t, uint(3), list[0].UserID)
	require.Equal(t, uint(9), list[1].UserID)

	empty := NewInMemoryRegistry().List()
	require.Empty(t, empty)
}

var _ agentapp.RuntimeRegistry = (*InMemoryRegistry)(nil)
