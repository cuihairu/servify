// WeKnora 协议 mock：只实现 servify knowledge provider 契约真正调用的端点面
// （health / search / 文档上传 / 文档删除 / 知识库详情），用于本地协议回归与
// 验收证据留档，不是真实 WeKnora 部署模板。
//
// 响应形状严格对齐 apps/server/pkg/weknora 的解析结构：业务面统一
// `{"success":bool,"data":...,"message":...}` 包裹，health 为
// `{"status":"ok|healthy"}`。旧版 mock 对所有路径都回 `{"status":"ok"}`，
// 使 `success` 恒为 false，上传/检索链路在栈内必然失败——现按协议逐端点实现，
// 并让上传的文档可被后续检索命中（有状态），使全栈 mock 验收真正可闭环。
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type document struct {
	ID      string                 `json:"id"`
	KBID    string                 `json:"kb_id"`
	Title   string                 `json:"title"`
	Content string                 `json:"content"`
	Tags    []string               `json:"tags,omitempty"`
	Meta    map[string]interface{} `json:"metadata,omitempty"`
}

type mock struct {
	mu   sync.Mutex
	docs map[string]*document
	seq  int
}

func (m *mock) nextID() string {
	m.seq++
	return fmt.Sprintf("weknora-mock-doc-%d", m.seq)
}

func writeJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func envelope(data interface{}, message string) map[string]interface{} {
	resp := map[string]interface{}{"success": true, "request_id": fmt.Sprintf("req-%d", time.Now().UnixNano())}
	if data != nil {
		resp["data"] = data
	}
	if message != "" {
		resp["message"] = message
	}
	return resp
}

func failure(message string) map[string]interface{} {
	return map[string]interface{}{"success": false, "message": message}
}

func (m *mock) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "ok",
		"service":    "weknora-mock",
		"version":    "1.1.0",
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"vector_db":  "pgvector",
		"mq_backend": "memory",
	})
}

// search 在已上传文档里做子串匹配（标题+正文），命中即返回结果，
// 使「上传 → 检索」在同一进程内可自洽验证。
func (m *mock) search(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query           string  `json:"query"`
		KnowledgeBaseID string  `json:"kb_id"`
		Limit           int     `json:"limit"`
		Threshold       float64 `json:"threshold"`
		Strategy        string  `json:"strategy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, failure("invalid search request: "+err.Error()))
		return
	}
	if req.KnowledgeBaseID == "" {
		writeJSON(w, http.StatusBadRequest, failure("kb_id is required"))
		return
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	query := strings.ToLower(strings.TrimSpace(req.Query))
	results := make([]map[string]interface{}, 0, req.Limit)
	for _, id := range m.sortedIDsLocked() {
		doc := m.docs[id]
		if doc.KBID != req.KnowledgeBaseID {
			continue
		}
		haystack := strings.ToLower(doc.Title + "\n" + doc.Content)
		if query != "" && !strings.Contains(haystack, query) {
			continue
		}
		results = append(results, map[string]interface{}{
			"document_id": doc.ID,
			"title":       doc.Title,
			"content":     doc.Content,
			"score":       0.92,
			"highlights":  []string{firstLine(doc.Content)},
			"metadata":    doc.Meta,
			"chunk_index": 0,
			"source":      "weknora-mock",
		})
		if len(results) >= req.Limit {
			break
		}
	}

	strategy := req.Strategy
	if strategy == "" {
		strategy = "hybrid"
	}
	writeJSON(w, http.StatusOK, envelope(map[string]interface{}{
		"results":       results,
		"total":         len(results),
		"strategy":      strategy,
		"query_time_ms": 1,
	}, ""))
}

func (m *mock) upload(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("kb")
	var req struct {
		Type     string                 `json:"type"`
		Title    string                 `json:"title"`
		Content  string                 `json:"content"`
		Tags     []string               `json:"tags"`
		Metadata map[string]interface{} `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, failure("invalid upload request: "+err.Error()))
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeJSON(w, http.StatusBadRequest, failure("title is required"))
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	doc := &document{
		ID:      m.nextID(),
		KBID:    kbID,
		Title:   req.Title,
		Content: req.Content,
		Tags:    req.Tags,
		Meta:    req.Metadata,
	}
	m.docs[doc.ID] = doc
	log.Printf("weknora-mock upload kb=%s id=%s title=%q chunks=1", kbID, doc.ID, doc.Title)

	writeJSON(w, http.StatusOK, envelope(map[string]interface{}{
		"id":           doc.ID,
		"title":        doc.Title,
		"status":       "indexed",
		"processed_at": time.Now().UTC().Format(time.RFC3339),
		"chunk_count":  1,
		"metadata":     doc.Meta,
	}, ""))
}

func (m *mock) remove(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("kb")
	docID := r.PathValue("doc")

	m.mu.Lock()
	defer m.mu.Unlock()

	doc, ok := m.docs[docID]
	if !ok || doc.KBID != kbID {
		log.Printf("weknora-mock delete kb=%s id=%s -> 404", kbID, docID)
		writeJSON(w, http.StatusNotFound, failure("document not found"))
		return
	}
	delete(m.docs, docID)
	log.Printf("weknora-mock delete kb=%s id=%s -> deleted", kbID, docID)
	writeJSON(w, http.StatusOK, envelope(nil, "deleted"))
}

func (m *mock) knowledgeBase(w http.ResponseWriter, r *http.Request) {
	kbID := r.PathValue("kb")

	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	chunks := 0
	for _, id := range m.sortedIDsLocked() {
		if m.docs[id].KBID == kbID {
			count++
			chunks++
		}
	}
	writeJSON(w, http.StatusOK, envelope(map[string]interface{}{
		"id":          kbID,
		"name":        kbID,
		"description": "weknora mock knowledge base",
		"config": map[string]interface{}{
			"embedding_model": "bge-large-zh",
			"retrieval_mode":  "hybrid",
		},
		"stats": map[string]interface{}{
			"document_count": count,
			"chunk_count":    chunks,
		},
	}, ""))
}

// unknown 显式回 success=false：协议面缺失时让调用方立刻报错，
// 而不是像旧 catch-all 那样返回 200 掩盖问题。
func unknown(w http.ResponseWriter, r *http.Request) {
	log.Printf("weknora-mock UNHANDLED %s %s", r.Method, r.URL.Path)
	writeJSON(w, http.StatusNotFound, failure("weknora-mock: unhandled endpoint "+r.Method+" "+r.URL.Path))
}

func (m *mock) sortedIDsLocked() []string {
	ids := make([]string, 0, len(m.docs))
	for id := range m.docs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx > 0 {
		return s[:idx]
	}
	if len(s) > 80 {
		return s[:80]
	}
	return s
}

func main() {
	port := os.Getenv("API_PORT")
	if port == "" {
		port = "9000"
	}

	m := &mock{docs: map[string]*document{}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", m.health)
	mux.HandleFunc("POST /api/v1/knowledge/search", m.search)
	mux.HandleFunc("GET /api/v1/knowledge/{kb}", m.knowledgeBase)
	mux.HandleFunc("POST /api/v1/knowledge/{kb}/documents", m.upload)
	mux.HandleFunc("DELETE /api/v1/knowledge/{kb}/documents/{doc}", m.remove)
	mux.HandleFunc("/", unknown)

	log.Printf("Mock WeKnora service listening on :%s (health / search / documents)", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
