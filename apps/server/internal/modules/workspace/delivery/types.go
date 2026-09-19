// Package delivery 对 handler 暴露的 DTO 类型（re-export application 层定义）。
package delivery

import (
	workspaceapp "servify/apps/server/internal/modules/workspace/application"
)

type WorkspaceOverview = workspaceapp.WorkspaceOverview

type ChannelSummary = workspaceapp.ChannelSummary

type AgentStatsOverview = workspaceapp.AgentStatsOverview

type AgentBasicInfo = workspaceapp.AgentBasicInfo

type WorkspaceSession = workspaceapp.WorkspaceSession
