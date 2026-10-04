//go:build integration
// +build integration

package handlers

// V1.0 收敛 B3-1a：来源登记与索引任务的 HTTP 面（docs/v1-convergence-plan.md
// §8.1/§8.2）——登记/挂源/重建索引/按文档看任务/失败重试全链。

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"servify/apps/server/internal/models"
	knowledgedelivery "servify/apps/server/internal/modules/knowledge/delivery"
)

func newKnowledgeSourcesRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := newTestDBForKnowledgeDocs(t)
	if err := db.AutoMigrate(&models.KnowledgeSource{}); err != nil {
		t.Fatalf("automigrate sources: %v", err)
	}
	h := NewKnowledgeDocHandler(knowledgedelivery.NewHandlerService(db))
	r := gin.New()
	RegisterKnowledgeDocRoutes(r.Group("/api"), h)
	return r
}

func doJSONKnowledge(t *testing.T, r *gin.Engine, method, path string, body interface{}) (int, []byte) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func TestKnowledgeSourceCRUD_And_DocumentSourceBinding(t *testing.T) {
	r := newKnowledgeSourcesRouter(t)

	// 登记来源（未知类型拒绝）。
	if code, _ := doJSONKnowledge(t, r, http.MethodPost, "/api/knowledge-docs/sources", map[string]string{
		"name": "x", "type": "video",
	}); code != http.StatusBadRequest {
		t.Fatalf("unknown type status=%d want 400", code)
	}
	code, body := doJSONKnowledge(t, r, http.MethodPost, "/api/knowledge-docs/sources", map[string]string{
		"name": "帮助中心", "type": "website", "description": "官网 FAQ",
	})
	if code != http.StatusCreated {
		t.Fatalf("create source status=%d body=%s", code, body)
	}
	var source models.KnowledgeSource
	_ = json.Unmarshal(body, &source)
	if source.ID == 0 || source.Type != "website" {
		t.Fatalf("unexpected source: %#v", source)
	}

	// 文档挂源。
	code, body = doJSONKnowledge(t, r, http.MethodPost, "/api/knowledge-docs", map[string]interface{}{
		"title": "Refund", "content": "policy", "source_id": source.ID,
	})
	if code != http.StatusCreated {
		t.Fatalf("create doc status=%d body=%s", code, body)
	}
	var doc models.KnowledgeDoc
	_ = json.Unmarshal(body, &doc)
	if doc.SourceID != source.ID || doc.Version != 1 {
		t.Fatalf("doc source/version = %d/%d want %d/1", doc.SourceID, doc.Version, source.ID)
	}

	// 引用中的来源删除被拒。
	if code, _ = doJSONKnowledge(t, r, http.MethodDelete, "/api/knowledge-docs/sources/"+strconv.FormatUint(uint64(source.ID), 10), nil); code != http.StatusBadRequest {
		t.Fatalf("delete referenced source status=%d want 400", code)
	}

	// 列表可见。
	code, body = doJSONKnowledge(t, r, http.MethodGet, "/api/knowledge-docs/sources?type=website", nil)
	if code != http.StatusOK {
		t.Fatalf("list sources status=%d", code)
	}
	var sources []models.KnowledgeSource
	_ = json.Unmarshal(body, &sources)
	if len(sources) != 1 || sources[0].ID != source.ID {
		t.Fatalf("sources = %#v", sources)
	}
}

func TestKnowledgeIndexJobs_EndToEnd(t *testing.T) {
	r := newKnowledgeSourcesRouter(t)

	// 建文档（内存 provider 未配置 → sync no-op，索引路径仍完整走任务状态机）。
	code, body := doJSONKnowledge(t, r, http.MethodPost, "/api/knowledge-docs", map[string]interface{}{
		"title": "Ix", "content": "v1",
	})
	if code != http.StatusCreated {
		t.Fatalf("create doc status=%d body=%s", code, body)
	}
	var doc models.KnowledgeDoc
	_ = json.Unmarshal(body, &doc)

	// 重建索引：POST /:id/index-jobs → done，任务落版本。
	code, body = doJSONKnowledge(t, r, http.MethodPost, "/api/knowledge-docs/"+strconv.FormatUint(uint64(doc.ID), 10)+"/index-jobs", nil)
	if code != http.StatusOK {
		t.Fatalf("index doc status=%d body=%s", code, body)
	}
	var result knowledgedelivery.IndexJobResult
	_ = json.Unmarshal(body, &result)
	if result.Status != "done" || result.DocumentVersion != 1 {
		t.Fatalf("result = %#v want done v1", result)
	}

	// 内容更新 → 版本 2 → 重试同一任务按当前版本重建。
	updateCode, _ := doJSONKnowledge(t, r, http.MethodPut, "/api/knowledge-docs/"+strconv.FormatUint(uint64(doc.ID), 10), map[string]interface{}{
		"content": "v2",
	})
	if updateCode != http.StatusOK {
		t.Fatalf("update doc status=%d", updateCode)
	}
	code, body = doJSONKnowledge(t, r, http.MethodPost, "/api/knowledge-docs/index-jobs/"+result.JobID+"/retry", nil)
	if code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", code, body)
	}
	_ = json.Unmarshal(body, &result)
	if result.Status != "done" || result.DocumentVersion != 2 {
		t.Fatalf("retry result = %#v want done v2", result)
	}

	// 按文档看任务：新任务在前、版本可见。
	code, body = doJSONKnowledge(t, r, http.MethodGet, "/api/knowledge-docs/"+strconv.FormatUint(uint64(doc.ID), 10)+"/index-jobs", nil)
	if code != http.StatusOK {
		t.Fatalf("list jobs status=%d", code)
	}
	var jobs []knowledgedelivery.IndexJobDTO
	_ = json.Unmarshal(body, &jobs)
	if len(jobs) != 1 || jobs[0].DocumentVersion != 2 {
		t.Fatalf("jobs = %#v want single job v2", jobs)
	}
}
