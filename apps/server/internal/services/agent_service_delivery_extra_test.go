package services

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/models"
	agentapp "servify/apps/server/internal/modules/agent/application"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"

	"github.com/sirupsen/logrus"
)

// TestAgentService_DeliveryDelegations 覆盖 AgentService 对 module 选坐席与
// 坐席组管理接口的兼容层透传（真实 sqlite 库驱动）。
func TestAgentService_DeliveryDelegations(t *testing.T) {
	db := newServicesTestDB(t,
		&models.User{}, &models.Agent{}, &models.Session{},
		&models.AgentGroup{}, &models.AgentGroupMember{},
	)
	svc := NewAgentService(db, newTestLogger())
	ctx := context.Background()

	// ---- 坐席组 CRUD ----
	if err := svc.CreateAgentGroup(ctx, nil); !errors.Is(err, agentapp.ErrGroupNameRequired) {
		t.Fatalf("CreateAgentGroup(nil) err = %v, want %v", err, agentapp.ErrGroupNameRequired)
	}
	group := &models.AgentGroup{Name: "tier-1"}
	if err := svc.CreateAgentGroup(ctx, group); err != nil {
		t.Fatalf("CreateAgentGroup() error = %v", err)
	}
	if group.ID == 0 {
		t.Fatal("expected group id persisted")
	}

	groups, err := svc.ListAgentGroups(ctx)
	if err != nil || len(groups) != 1 || groups[0].Name != "tier-1" {
		t.Fatalf("ListAgentGroups() = %+v, %v", groups, err)
	}

	got, err := svc.GetAgentGroup(ctx, group.ID)
	if err != nil || got.Name != "tier-1" {
		t.Fatalf("GetAgentGroup() = %+v, %v", got, err)
	}
	if _, err := svc.GetAgentGroup(ctx, 0); !errors.Is(err, agentapp.ErrGroupNotFound) {
		t.Fatalf("GetAgentGroup(0) err = %v, want %v", err, agentapp.ErrGroupNotFound)
	}

	if err := svc.ReplaceGroupMembers(ctx, group.ID, []uint{1, 2}); err != nil {
		t.Fatalf("ReplaceGroupMembers() error = %v", err)
	}
	members, err := svc.ListGroupMembers(ctx, group.ID)
	if err != nil || len(members) != 2 {
		t.Fatalf("ListGroupMembers() = %v, %v", members, err)
	}

	group.Name = "tier-1-renamed"
	if err := svc.UpdateAgentGroup(ctx, group); err != nil {
		t.Fatalf("UpdateAgentGroup() error = %v", err)
	}
	if err := svc.DeleteAgentGroup(ctx, group.ID); err != nil {
		t.Fatalf("DeleteAgentGroup() error = %v", err)
	}
	if _, err := svc.GetAgentGroup(ctx, group.ID); err == nil {
		t.Fatal("expected error after group deletion")
	}

	// ---- SelectAgent 三级管线透传 ----
	seedAgentUser(t, db, 1)
	if _, err := svc.CreateAgent(ctx, &AgentCreateRequest{UserID: 1, Skills: " voice "}); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := svc.AgentGoOnline(ctx, 1); err != nil {
		t.Fatalf("AgentGoOnline: %v", err)
	}

	result, err := svc.SelectAgent(ctx, agentdelivery.SelectionRequest{Skills: []string{"voice"}})
	if err != nil {
		t.Fatalf("SelectAgent() error = %v", err)
	}
	if result == nil || result.Agent == nil || result.Agent.UserID != 1 {
		t.Fatalf("SelectAgent() = %+v", result)
	}
	if result.Source != agentdelivery.SelectionSourceGlobal {
		t.Fatalf("source = %q, want global", result.Source)
	}

	if _, err := svc.SelectAgent(ctx, agentdelivery.SelectionRequest{GroupID: uintPtr(999)}); err == nil {
		t.Fatal("expected error selecting from missing group")
	}
}

// TestAgentService_UpdateAgentGroupGuard 覆盖更新时 id=0 的守卫分支。
func TestAgentService_UpdateAgentGroupGuard(t *testing.T) {
	db := newServicesTestDB(t, &models.AgentGroup{})
	svc := NewAgentService(db, logrus.New())
	if err := svc.UpdateAgentGroup(context.Background(), &models.AgentGroup{}); !errors.Is(err, agentapp.ErrGroupNotFound) {
		t.Fatalf("UpdateAgentGroup(id=0) err = %v, want %v", err, agentapp.ErrGroupNotFound)
	}
	if _, err := svc.ListGroupMembers(context.Background(), 0); !errors.Is(err, agentapp.ErrGroupNotFound) {
		t.Fatalf("ListGroupMembers(0) err = %v", err)
	}
}
