package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"servify/apps/server/internal/models"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

var _ agentdelivery.AgentGroupService = (*cvhAgentGroupService)(nil)

type cvhMemberCall struct {
	groupID uint
	ids     []uint
}

// cvhAgentGroupService 坐席组服务 inline mock。
type cvhAgentGroupService struct {
	listGroups []models.AgentGroup
	listErr    error

	getGroup *models.AgentGroup
	getErr   error

	createErr error
	updateErr error
	deleteErr error

	replaceErr   error
	replaceCalls []cvhMemberCall

	members    []uint
	membersErr error

	lastCreated *models.AgentGroup
	lastUpdated *models.AgentGroup
	lastDeleted uint
}

func (s *cvhAgentGroupService) ListAgentGroups(context.Context) ([]models.AgentGroup, error) {
	return s.listGroups, s.listErr
}

func (s *cvhAgentGroupService) GetAgentGroup(_ context.Context, id uint) (*models.AgentGroup, error) {
	if s.getGroup != nil {
		s.getGroup.ID = id
	}
	return s.getGroup, s.getErr
}

func (s *cvhAgentGroupService) CreateAgentGroup(_ context.Context, group *models.AgentGroup) error {
	s.lastCreated = group
	if s.createErr == nil && group != nil {
		group.ID = 42
	}
	return s.createErr
}

func (s *cvhAgentGroupService) UpdateAgentGroup(_ context.Context, group *models.AgentGroup) error {
	s.lastUpdated = group
	return s.updateErr
}

func (s *cvhAgentGroupService) DeleteAgentGroup(_ context.Context, id uint) error {
	s.lastDeleted = id
	return s.deleteErr
}

func (s *cvhAgentGroupService) ReplaceGroupMembers(_ context.Context, groupID uint, agentUserIDs []uint) error {
	s.replaceCalls = append(s.replaceCalls, cvhMemberCall{groupID: groupID, ids: agentUserIDs})
	return s.replaceErr
}

func (s *cvhAgentGroupService) ListGroupMembers(_ context.Context, groupID uint) ([]uint, error) {
	return s.members, s.membersErr
}

func cvhAgentGroupRouter(svc *cvhAgentGroupService) *gin.Engine {
	r := dxcRouter()
	RegisterAgentGroupRoutes(r.Group("/api"), NewAgentGroupHandler(svc))
	return r
}

func TestCvhNewAgentGroupHandler(t *testing.T) {
	svc := &cvhAgentGroupService{}
	h := NewAgentGroupHandler(svc)
	assert.NotNil(t, h)
	assert.Equal(t, svc, h.service)
}

func TestCvhRegisterAgentGroupRoutes(t *testing.T) {
	r := cvhAgentGroupRouter(&cvhAgentGroupService{})
	routes := r.Routes()
	assert.Len(t, routes, 7)
	want := map[string]bool{
		"GET /api/agents/groups":             false,
		"POST /api/agents/groups":            false,
		"GET /api/agents/groups/:id":         false,
		"PUT /api/agents/groups/:id":         false,
		"DELETE /api/agents/groups/:id":      false,
		"GET /api/agents/groups/:id/members": false,
		"PUT /api/agents/groups/:id/members": false,
	}
	for _, rt := range routes {
		key := rt.Method + " " + rt.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for key, seen := range want {
		assert.True(t, seen, "route %s not registered", key)
	}
}

func TestCvhAgentGroupListGroups(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &cvhAgentGroupService{listGroups: []models.AgentGroup{
			{ID: 1, Name: "g1"}, {ID: 2, Name: "g2"},
		}}
		w := dxcDo(cvhAgentGroupRouter(svc), http.MethodGet, "/api/agents/groups", "")
		assert.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			Groups []models.AgentGroup `json:"groups"`
			Total  int                 `json:"total"`
		}
		dxcDecode(t, w, &resp)
		assert.Len(t, resp.Groups, 2)
		assert.Equal(t, 2, resp.Total)
		assert.Equal(t, "g1", resp.Groups[0].Name)
	})

	t.Run("service error", func(t *testing.T) {
		svc := &cvhAgentGroupService{listErr: errors.New("boom")}
		w := dxcDo(cvhAgentGroupRouter(svc), http.MethodGet, "/api/agents/groups", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to list agent groups")
		assert.Contains(t, w.Body.String(), "boom")
	})
}

func TestCvhAgentGroupCreateGroup(t *testing.T) {
	t.Run("defaults enabled and policy", func(t *testing.T) {
		svc := &cvhAgentGroupService{}
		w := dxcDo(cvhAgentGroupRouter(svc), http.MethodPost, "/api/agents/groups", `{"name":"售前组","priority":10}`)
		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NotNil(t, svc.lastCreated)
		assert.Equal(t, "售前组", svc.lastCreated.Name)
		assert.Equal(t, 10, svc.lastCreated.Priority)
		assert.True(t, svc.lastCreated.Enabled, "enabled 应缺省为 true")
		assert.Equal(t, "global", svc.lastCreated.OverflowPolicy)
		assert.Contains(t, w.Body.String(), `"id":42`)
	})

	t.Run("explicit fields with members", func(t *testing.T) {
		svc := &cvhAgentGroupService{}
		body := `{"name":"g","overflow_policy":"none","enabled":false,"member_ids":[3,5]}`
		w := dxcDo(cvhAgentGroupRouter(svc), http.MethodPost, "/api/agents/groups", body)
		assert.Equal(t, http.StatusCreated, w.Code)
		assert.False(t, svc.lastCreated.Enabled)
		assert.Equal(t, "none", svc.lastCreated.OverflowPolicy)
		assert.Len(t, svc.replaceCalls, 1)
		assert.Equal(t, uint(42), svc.replaceCalls[0].groupID)
		assert.Equal(t, []uint{3, 5}, svc.replaceCalls[0].ids)
	})

	t.Run("member replace fails", func(t *testing.T) {
		svc := &cvhAgentGroupService{replaceErr: errors.New("db down")}
		w := dxcDo(cvhAgentGroupRouter(svc), http.MethodPost, "/api/agents/groups", `{"name":"g","member_ids":[1]}`)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Group created but failed to set members")
	})

	t.Run("invalid json", func(t *testing.T) {
		w := dxcDo(cvhAgentGroupRouter(&cvhAgentGroupService{}), http.MethodPost, "/api/agents/groups", `{`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid request")
	})

	t.Run("name required", func(t *testing.T) {
		// 请求体必须能通过 binding，否则 400 来自 ShouldBindJSON 而非服务错误映射
		svc := &cvhAgentGroupService{createErr: agentdelivery.ErrGroupNameRequired}
		w := dxcDo(cvhAgentGroupRouter(svc), http.MethodPost, "/api/agents/groups", `{"name":"x"}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to create agent group")
	})

	t.Run("duplicate", func(t *testing.T) {
		svc := &cvhAgentGroupService{createErr: agentdelivery.ErrGroupDuplicate}
		w := dxcDo(cvhAgentGroupRouter(svc), http.MethodPost, "/api/agents/groups", `{"name":"dup"}`)
		assert.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("generic error", func(t *testing.T) {
		svc := &cvhAgentGroupService{createErr: errors.New("boom")}
		w := dxcDo(cvhAgentGroupRouter(svc), http.MethodPost, "/api/agents/groups", `{"name":"g"}`)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestCvhAgentGroupGetGroup(t *testing.T) {
	group := func() *models.AgentGroup { return &models.AgentGroup{ID: 9, Name: "g9"} }
	cases := []struct {
		name string
		svc  *cvhAgentGroupService
		id   string
		want int
	}{
		{"success", &cvhAgentGroupService{getGroup: group()}, "9", http.StatusOK},
		{"invalid id", &cvhAgentGroupService{}, "abc", http.StatusBadRequest},
		{"not found", &cvhAgentGroupService{getErr: agentdelivery.ErrGroupNotFound}, "9", http.StatusNotFound},
		{"name required maps to 400", &cvhAgentGroupService{getErr: agentdelivery.ErrGroupNameRequired}, "9", http.StatusBadRequest},
		{"generic error", &cvhAgentGroupService{getErr: errors.New("boom")}, "9", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := dxcDo(cvhAgentGroupRouter(tc.svc), http.MethodGet, "/api/agents/groups/"+tc.id, "")
			assert.Equal(t, tc.want, w.Code)
			if tc.want == http.StatusOK {
				var got models.AgentGroup
				dxcDecode(t, w, &got)
				assert.Equal(t, uint(9), got.ID)
				assert.Equal(t, "g9", got.Name)
			}
		})
	}
}

func TestCvhAgentGroupUpdateGroup(t *testing.T) {
	base := func() *models.AgentGroup {
		return &models.AgentGroup{ID: 9, Name: "old", Description: "old desc", Priority: 1, OverflowPolicy: "global", Enabled: true}
	}

	t.Run("partial update applies only given fields", func(t *testing.T) {
		svc := &cvhAgentGroupService{getGroup: base()}
		w := dxcDo(cvhAgentGroupRouter(svc), http.MethodPut, "/api/agents/groups/9", `{"description":"new desc"}`)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "old", svc.lastUpdated.Name, "未传字段应保留原值")
		assert.Equal(t, "new desc", svc.lastUpdated.Description)
		assert.Equal(t, 1, svc.lastUpdated.Priority)
		assert.True(t, svc.lastUpdated.Enabled)
		assert.Contains(t, w.Body.String(), "new desc")
	})

	t.Run("full update", func(t *testing.T) {
		svc := &cvhAgentGroupService{getGroup: base()}
		parent := uint(5)
		body := `{"name":"new","description":"d","priority":7,"overflow_policy":"none","enabled":false,"parent_id":5}`
		w := dxcDo(cvhAgentGroupRouter(svc), http.MethodPut, "/api/agents/groups/9", body)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "new", svc.lastUpdated.Name)
		assert.Equal(t, 7, svc.lastUpdated.Priority)
		assert.Equal(t, "none", svc.lastUpdated.OverflowPolicy)
		assert.False(t, svc.lastUpdated.Enabled)
		assert.NotNil(t, svc.lastUpdated.ParentID)
		assert.Equal(t, &parent, svc.lastUpdated.ParentID)
	})

	cases := []struct {
		name string
		svc  *cvhAgentGroupService
		id   string
		body string
		want int
	}{
		{"invalid id", &cvhAgentGroupService{}, "abc", `{}`, http.StatusBadRequest},
		{"get not found", &cvhAgentGroupService{getErr: agentdelivery.ErrGroupNotFound}, "9", `{}`, http.StatusNotFound},
		{"invalid json", &cvhAgentGroupService{getGroup: base()}, "9", `{`, http.StatusBadRequest},
		{"update duplicate", &cvhAgentGroupService{getGroup: base(), updateErr: agentdelivery.ErrGroupDuplicate}, "9", `{}`, http.StatusConflict},
		{"update generic", &cvhAgentGroupService{getGroup: base(), updateErr: errors.New("boom")}, "9", `{}`, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := dxcDo(cvhAgentGroupRouter(tc.svc), http.MethodPut, "/api/agents/groups/"+tc.id, tc.body)
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestCvhAgentGroupDeleteGroup(t *testing.T) {
	cases := []struct {
		name string
		svc  *cvhAgentGroupService
		id   string
		want int
	}{
		{"success", &cvhAgentGroupService{}, "9", http.StatusOK},
		{"invalid id", &cvhAgentGroupService{}, "abc", http.StatusBadRequest},
		{"not found", &cvhAgentGroupService{deleteErr: agentdelivery.ErrGroupNotFound}, "9", http.StatusNotFound},
		{"generic error", &cvhAgentGroupService{deleteErr: errors.New("boom")}, "9", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := dxcDo(cvhAgentGroupRouter(tc.svc), http.MethodDelete, "/api/agents/groups/"+tc.id, "")
			assert.Equal(t, tc.want, w.Code)
			if tc.want == http.StatusOK {
				var resp SuccessResponse
				dxcDecode(t, w, &resp)
				assert.Equal(t, "deleted", resp.Message)
				assert.Equal(t, uint(9), tc.svc.lastDeleted)
			}
		})
	}
}

func TestCvhAgentGroupReplaceGroupMembers(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &cvhAgentGroupService{}
		w := dxcDo(cvhAgentGroupRouter(svc), http.MethodPut, "/api/agents/groups/9/members", `{"member_ids":[1,2,3]}`)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp SuccessResponse
		dxcDecode(t, w, &resp)
		assert.Equal(t, "members updated", resp.Message)
		assert.Len(t, svc.replaceCalls, 1)
		assert.Equal(t, uint(9), svc.replaceCalls[0].groupID)
		assert.Equal(t, []uint{1, 2, 3}, svc.replaceCalls[0].ids)
	})

	cases := []struct {
		name string
		svc  *cvhAgentGroupService
		id   string
		body string
		want int
	}{
		{"invalid id", &cvhAgentGroupService{}, "abc", `{"member_ids":[1]}`, http.StatusBadRequest},
		{"invalid json", &cvhAgentGroupService{}, "9", `{`, http.StatusBadRequest},
		{"not found", &cvhAgentGroupService{replaceErr: agentdelivery.ErrGroupNotFound}, "9", `{"member_ids":[1]}`, http.StatusNotFound},
		{"generic error", &cvhAgentGroupService{replaceErr: errors.New("boom")}, "9", `{"member_ids":[1]}`, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := dxcDo(cvhAgentGroupRouter(tc.svc), http.MethodPut, "/api/agents/groups/"+tc.id+"/members", tc.body)
			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestCvhAgentGroupListGroupMembers(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &cvhAgentGroupService{members: []uint{7, 8}}
		w := dxcDo(cvhAgentGroupRouter(svc), http.MethodGet, "/api/agents/groups/9/members", "")
		assert.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			MemberIDs []uint `json:"member_ids"`
			Total     int    `json:"total"`
		}
		dxcDecode(t, w, &resp)
		assert.Equal(t, []uint{7, 8}, resp.MemberIDs)
		assert.Equal(t, 2, resp.Total)
	})

	cases := []struct {
		name string
		svc  *cvhAgentGroupService
		id   string
		want int
	}{
		{"invalid id", &cvhAgentGroupService{}, "abc", http.StatusBadRequest},
		{"not found", &cvhAgentGroupService{membersErr: agentdelivery.ErrGroupNotFound}, "9", http.StatusNotFound},
		{"generic error", &cvhAgentGroupService{membersErr: errors.New("boom")}, "9", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := dxcDo(cvhAgentGroupRouter(tc.svc), http.MethodGet, "/api/agents/groups/"+tc.id+"/members", "")
			assert.Equal(t, tc.want, w.Code)
		})
	}
}
