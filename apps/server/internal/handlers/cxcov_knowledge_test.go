package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"servify/apps/server/internal/models"

	"github.com/gin-gonic/gin"
)

func cxcNewKnowledgeRouter(svc *unitKnowledgeService) (*gin.Engine, *KnowledgeDocHandler, *KnowledgeDocHandler) {
	gin.SetMode(gin.TestMode)
	admin := NewKnowledgeDocHandler(svc)
	public := NewPublicKnowledgeDocHandler(svc)
	r := gin.New()
	api := r.Group("/api")
	RegisterKnowledgeDocRoutes(api, admin)
	RegisterPublicKnowledgeBaseRoutes(r.Group("/public"), public)
	RegisterPublicKnowledgeBaseRoutes(r.Group("/public2"), admin) // admin handler gets wrapped as public
	return r, admin, public
}

func TestCxcKnowledgeDocList(t *testing.T) {
	svc := &unitKnowledgeService{
		docs:  []models.KnowledgeDoc{{ID: 1, Title: "A"}, {ID: 2, Title: "B"}},
		total: 2,
	}
	r, _, _ := cxcNewKnowledgeRouter(svc)

	// defaults for page/page_size
	w := cxcPerform(r, http.MethodGet, "/api/knowledge-docs", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp PaginatedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	if resp.Page != 1 || resp.PageSize != 20 || resp.Total != 2 {
		t.Fatalf("unexpected response: %+v", resp)
	}

	// explicit pagination echoes back
	w = cxcPerform(r, http.MethodGet, "/api/knowledge-docs?page=3&page_size=7", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("paged list expected 200, got %d", w.Code)
	}
	var resp2 PaginatedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp2.Page != 3 || resp2.PageSize != 7 {
		t.Fatalf("expected page=3 page_size=7, got %+v", resp2)
	}

	// invalid query binding
	w = cxcPerform(r, http.MethodGet, "/api/knowledge-docs?page=abc", nil, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid query expected 400, got %d body=%s", w.Code, w.Body.String())
	}

	// list error
	svc.listErr = errors.New("boom")
	w = cxcPerform(r, http.MethodGet, "/api/knowledge-docs", nil, "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("list error expected 500, got %d", w.Code)
	}
	svc.listErr = nil

	// public list forces PublicOnly flag
	w = cxcPerform(r, http.MethodGet, "/public/kb/docs?page=0&page_size=-1", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("public list expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp3 PaginatedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp3); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp3.Page != 1 || resp3.PageSize != 20 {
		t.Fatalf("expected normalized pagination, got %+v", resp3)
	}
}

func TestCxcKnowledgeDocGet(t *testing.T) {
	svc := &unitKnowledgeService{doc: &models.KnowledgeDoc{ID: 1, Title: "A", IsPublic: false}}
	r, _, _ := cxcNewKnowledgeRouter(svc)

	if w := cxcPerform(r, http.MethodGet, "/api/knowledge-docs/not-a-number", nil, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("bad id expected 400, got %d", w.Code)
	}

	svc.getErr = errors.New("missing")
	if w := cxcPerform(r, http.MethodGet, "/api/knowledge-docs/1", nil, ""); w.Code != http.StatusNotFound {
		t.Fatalf("get error expected 404, got %d", w.Code)
	}
	svc.getErr = nil

	if w := cxcPerform(r, http.MethodGet, "/api/knowledge-docs/1", nil, ""); w.Code != http.StatusOK {
		t.Fatalf("get expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	// public route hides non-public docs
	if w := cxcPerform(r, http.MethodGet, "/public/kb/docs/1", nil, ""); w.Code != http.StatusNotFound {
		t.Fatalf("public get non-public expected 404, got %d body=%s", w.Code, w.Body.String())
	}

	svc.doc = &models.KnowledgeDoc{ID: 2, Title: "P", IsPublic: true}
	if w := cxcPerform(r, http.MethodGet, "/public/kb/docs/2", nil, ""); w.Code != http.StatusOK {
		t.Fatalf("public get expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCxcKnowledgeDocCreateUpdateDelete(t *testing.T) {
	svc := &unitKnowledgeService{doc: &models.KnowledgeDoc{ID: 1, Title: "A"}}
	r, _, _ := cxcNewKnowledgeRouter(svc)

	// create: bad json
	if w := cxcPerform(r, http.MethodPost, "/api/knowledge-docs", strings.NewReader("{bad"), "application/json"); w.Code != http.StatusBadRequest {
		t.Fatalf("create bad json expected 400, got %d", w.Code)
	}
	// create: missing required field
	if w := cxcPerform(r, http.MethodPost, "/api/knowledge-docs", cxcJSONBody(map[string]string{"title": "only-title"}), "application/json"); w.Code != http.StatusBadRequest {
		t.Fatalf("create missing content expected 400, got %d", w.Code)
	}
	// create: service error
	svc.createErr = errors.New("boom")
	if w := cxcPerform(r, http.MethodPost, "/api/knowledge-docs", cxcJSONBody(map[string]string{"title": "T", "content": "C"}), "application/json"); w.Code != http.StatusBadRequest {
		t.Fatalf("create error expected 400, got %d", w.Code)
	}
	svc.createErr = nil
	// create: success
	if w := cxcPerform(r, http.MethodPost, "/api/knowledge-docs", cxcJSONBody(map[string]interface{}{"title": "T", "content": "C", "tags": []string{"x"}}), "application/json"); w.Code != http.StatusCreated {
		t.Fatalf("create expected 201, got %d body=%s", w.Code, w.Body.String())
	}

	// update: bad id
	if w := cxcPerform(r, http.MethodPut, "/api/knowledge-docs/x", cxcJSONBody(map[string]string{"title": "T2"}), "application/json"); w.Code != http.StatusBadRequest {
		t.Fatalf("update bad id expected 400, got %d", w.Code)
	}
	// update: bad json
	if w := cxcPerform(r, http.MethodPut, "/api/knowledge-docs/1", strings.NewReader("{bad"), "application/json"); w.Code != http.StatusBadRequest {
		t.Fatalf("update bad json expected 400, got %d", w.Code)
	}
	// update: service error
	svc.updateErr = errors.New("boom")
	if w := cxcPerform(r, http.MethodPut, "/api/knowledge-docs/1", cxcJSONBody(map[string]string{"title": "T2"}), "application/json"); w.Code != http.StatusBadRequest {
		t.Fatalf("update error expected 400, got %d", w.Code)
	}
	svc.updateErr = nil
	// update: success
	if w := cxcPerform(r, http.MethodPut, "/api/knowledge-docs/1", cxcJSONBody(map[string]interface{}{"title": "T2", "is_public": true}), "application/json"); w.Code != http.StatusOK {
		t.Fatalf("update expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	// delete: bad id
	w := cxcPerform(r, http.MethodDelete, "/api/knowledge-docs/x", nil, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("delete bad id expected 400, got %d", w.Code)
	}
	// delete: service error
	svc.deleteErr = errors.New("boom")
	w = cxcPerform(r, http.MethodDelete, "/api/knowledge-docs/1", nil, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("delete error expected 400, got %d", w.Code)
	}
	svc.deleteErr = nil
	// delete: success
	w = cxcPerform(r, http.MethodDelete, "/api/knowledge-docs/1", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "deleted") {
		t.Fatalf("unexpected delete body: %s", w.Body.String())
	}
}
