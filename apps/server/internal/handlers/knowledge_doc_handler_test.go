//go:build integration
// +build integration

package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	knowledgedelivery "servify/apps/server/internal/modules/knowledge/delivery"
)

func newTestDBForKnowledgeDocs(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	dsn := uniqueMemDSN("file:knowledge_docs_" + name + "")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.KnowledgeDoc{}, &models.KnowledgeIndexJob{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestKnowledgeDocHandler_AdminCRUD_And_PublicList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTestDBForKnowledgeDocs(t)
	svc := knowledgedelivery.NewHandlerService(db)
	h := NewKnowledgeDocHandler(svc)

	r := gin.New()
	api := r.Group("/api")
	RegisterKnowledgeDocRoutes(api, h)
	public := r.Group("/public")
	RegisterPublicKnowledgeBaseRoutes(public, h)

	// create public doc
	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]interface{}{
		"title":     "Hello",
		"content":   "World",
		"category":  "getting-started",
		"tags":      []string{"a", "b"},
		"is_public": true,
	})
	req, _ := http.NewRequest(http.MethodPost, "/api/knowledge-docs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var created models.KnowledgeDoc
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created: %v body=%s", err, w.Body.String())
	}
	if created.ID == 0 || created.Title != "Hello" {
		t.Fatalf("unexpected created doc: %#v", created)
	}
	if !created.IsPublic {
		t.Fatalf("expected created doc to be public: %#v", created)
	}

	// create internal doc
	wInternal := httptest.NewRecorder()
	internalBody, _ := json.Marshal(map[string]interface{}{
		"title":    "Hello internal",
		"content":  "Internal content",
		"category": "getting-started",
	})
	reqInternal, _ := http.NewRequest(http.MethodPost, "/api/knowledge-docs", bytes.NewReader(internalBody))
	reqInternal.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wInternal, reqInternal)
	if wInternal.Code != http.StatusCreated {
		t.Fatalf("create internal status=%d body=%s", wInternal.Code, wInternal.Body.String())
	}
	var internalDoc models.KnowledgeDoc
	if err := json.Unmarshal(wInternal.Body.Bytes(), &internalDoc); err != nil {
		t.Fatalf("unmarshal internal doc: %v body=%s", err, wInternal.Body.String())
	}
	if internalDoc.IsPublic {
		t.Fatalf("expected internal doc to default to non-public: %#v", internalDoc)
	}

	// update
	w2 := httptest.NewRecorder()
	newTitle := "Hello v2"
	upBody, _ := json.Marshal(map[string]interface{}{"title": newTitle})
	req2, _ := http.NewRequest(http.MethodPut, "/api/knowledge-docs/"+itoa(created.ID), bytes.NewReader(upBody))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", w2.Code, w2.Body.String())
	}

	// list public with search
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest(http.MethodGet, "/public/kb/docs?search=Hello&page=1&page_size=10", nil)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("public list status=%d body=%s", w3.Code, w3.Body.String())
	}
	var listResp PaginatedResponse
	if err := json.Unmarshal(w3.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list: %v body=%s", err, w3.Body.String())
	}
	if listResp.Total != 1 {
		t.Fatalf("expected total=1 got %d", listResp.Total)
	}

	// get public
	w4 := httptest.NewRecorder()
	req4, _ := http.NewRequest(http.MethodGet, "/public/kb/docs/"+itoa(created.ID), nil)
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusOK {
		t.Fatalf("public get status=%d body=%s", w4.Code, w4.Body.String())
	}

	// get internal doc from public route -> 404
	wInternalGet := httptest.NewRecorder()
	reqInternalGet, _ := http.NewRequest(http.MethodGet, "/public/kb/docs/"+itoa(internalDoc.ID), nil)
	r.ServeHTTP(wInternalGet, reqInternalGet)
	if wInternalGet.Code != http.StatusNotFound {
		t.Fatalf("public get internal expected 404 got %d body=%s", wInternalGet.Code, wInternalGet.Body.String())
	}

	// admin list still includes both docs
	wAdminList := httptest.NewRecorder()
	reqAdminList, _ := http.NewRequest(http.MethodGet, "/api/knowledge-docs?search=Hello&page=1&page_size=10", nil)
	r.ServeHTTP(wAdminList, reqAdminList)
	if wAdminList.Code != http.StatusOK {
		t.Fatalf("admin list status=%d body=%s", wAdminList.Code, wAdminList.Body.String())
	}
	var adminListResp PaginatedResponse
	if err := json.Unmarshal(wAdminList.Body.Bytes(), &adminListResp); err != nil {
		t.Fatalf("unmarshal admin list: %v body=%s", err, wAdminList.Body.String())
	}
	if adminListResp.Total != 2 {
		t.Fatalf("expected admin total=2 got %d", adminListResp.Total)
	}

	// delete
	w5 := httptest.NewRecorder()
	req5, _ := http.NewRequest(http.MethodDelete, "/api/knowledge-docs/"+itoa(created.ID), nil)
	r.ServeHTTP(w5, req5)
	if w5.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", w5.Code, w5.Body.String())
	}

	// get after delete -> 404
	w6 := httptest.NewRecorder()
	req6, _ := http.NewRequest(http.MethodGet, "/api/knowledge-docs/"+itoa(created.ID), nil)
	r.ServeHTTP(w6, req6)
	if w6.Code != http.StatusNotFound {
		t.Fatalf("get after delete expected 404 got %d body=%s", w6.Code, w6.Body.String())
	}
}

func itoa(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}
