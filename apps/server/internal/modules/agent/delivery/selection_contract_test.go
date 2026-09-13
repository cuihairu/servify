package delivery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAgentInfoFromRuntime(t *testing.T) {
	require.Nil(t, AgentInfoFromRuntime(nil), "nil runtime must map to nil")

	last := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	connected := last.Add(-2 * time.Hour)
	info := AgentInfoFromRuntime(&AgentRuntimeDTO{
		UserID:             7,
		Username:           "agent-7",
		Name:               "Agent Seven",
		Department:         "support",
		Skills:             []string{"billing", "vip"},
		Status:             "online",
		MaxChatConcurrency: 4,
		CurrentChatLoad:    1,
		Rating:             4.8,
		AvgResponseTime:    42,
		LastActivity:       last,
		ConnectedAt:        connected,
	})
	require.NotNil(t, info)
	require.Equal(t, uint(7), info.UserID)
	require.Equal(t, "agent-7", info.Username)
	require.Equal(t, "Agent Seven", info.Name)
	require.Equal(t, "support", info.Department)
	require.Equal(t, []string{"billing", "vip"}, info.Skills)
	require.Equal(t, "online", info.Status)
	require.Equal(t, 4, info.MaxConcurrent)
	require.Equal(t, 1, info.CurrentLoad)
	require.Equal(t, 4.8, info.Rating)
	require.Equal(t, 42, info.AvgResponseTime)
	require.Equal(t, last, info.LastActivity)
	require.Equal(t, connected, info.ConnectedAt)

	// 拷贝语义：改返回值不得影响入参切片。
	info.Skills[0] = "mutated"
	require.Equal(t, "billing", AgentInfoFromRuntime(&AgentRuntimeDTO{
		UserID: 7, Skills: []string{"billing"},
	}).Skills[0])
}
