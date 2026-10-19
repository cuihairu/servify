package pgvector

// 检索分支补测（混合检索落地后的遗留 0% 面）：非 sqlite 方言的 pgvector
// 排序与 pg_trgm 关键词分支（postgres 命名包装 sqlite，SQL 构造面即覆盖
// 目标）、未知策略、查询失败传播（闭库 / 次序计数回调）、sqlite 截断与
// fuseRRF 的 k 兜底和 topK 截断。

import (
	"context"
	"errors"
	"strings"
	"testing"

	knowledgedomain "servify/apps/server/internal/modules/knowledge/domain"
	"servify/apps/server/internal/platform/knowledgeprovider"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// gatePostgresNamedSQLite 包装 sqlite 方言、Name 报 postgres：只用于覆盖
// provider 内非 sqlite 分支的 SQL 构造面，查询结果由回调注入或错误兜底，
// 不依赖真实 postgres。
type gatePostgresNamedSQLite struct{ gorm.Dialector }

func (d gatePostgresNamedSQLite) Name() string { return "postgres" }

func newGateDB(t *testing.T, named bool) *gorm.DB {
	t.Helper()
	dialector := sqlite.Open(":memory:")
	if named {
		dialector = gatePostgresNamedSQLite{dialector}
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&knowledgedomain.KnowledgeDoc{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

var gateDocs = []knowledgedomain.KnowledgeDoc{
	{ID: 1, TenantID: "t", WorkspaceID: "kb", ProviderID: "pgvector", ExternalID: "e1", Title: "Alpha", Content: "a", Embedding: knowledgedomain.NewEmbedding([]float32{1, 0})},
	{ID: 2, TenantID: "t", WorkspaceID: "kb", ProviderID: "pgvector", ExternalID: "e2", Title: "Beta", Content: "b", Embedding: knowledgedomain.NewEmbedding([]float32{0, 1})},
}

func TestGateSearchRejectsUnknownStrategy(t *testing.T) {
	db := newGateDB(t, false)
	provider := NewProvider(db, &mockEmbeddingProvider{dimension: 2, vectors: [][]float32{{1, 0}}}, Config{})
	_, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q", Strategy: "dense"})
	if err == nil || !strings.Contains(err.Error(), "unsupported search strategy") {
		t.Fatalf("expected unsupported strategy error, got %v", err)
	}
}

func TestGateClosedDBPropagatesVectorQueryError(t *testing.T) {
	db := newGateDB(t, false)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	provider := NewProvider(db, &mockEmbeddingProvider{dimension: 2, vectors: [][]float32{{1, 0}}}, Config{})
	// 纯向量路径：查询失败在 searchVector 内部包装后透出。
	if _, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q", Strategy: "vector"}); err == nil || !strings.Contains(err.Error(), "failed to execute search query") {
		t.Fatalf("expected vector query error, got %v", err)
	}
	// 混合路径：向量查询失败即短路，关键词查询不执行。
	if _, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q", Strategy: "hybrid"}); err == nil || !strings.Contains(err.Error(), "failed to execute search query") {
		t.Fatalf("expected hybrid short-circuit on vector error, got %v", err)
	}
}

func TestGateNonSQLiteOrderBranches(t *testing.T) {
	// postgres 命名方言下 Order 分支执行，随后查询在 sqlite 上失败即返回。
	for _, strategy := range []string{"vector", "euclidean"} {
		db := newGateDB(t, true)
		provider := NewProvider(db, &mockEmbeddingProvider{dimension: 2, vectors: [][]float32{{1, 0}}}, Config{})
		if _, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q", Strategy: strategy}); err == nil {
			t.Fatalf("strategy %s: expected query failure under postgres-named sqlite", strategy)
		}
	}
}

func TestGateHybridKeywordErrorPropagates(t *testing.T) {
	// 次序计数回调：第一次查询（向量）注入结果，第二次（关键词）报错，
	// 覆盖 searchKeyword 的失败包装与 Search 的混合传播。postgres 命名
	// 方言顺带覆盖 pg_trgm 关键词分支的 SQL 构造面。
	db := newGateDB(t, true)
	calls := 0
	_ = db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		calls++
		if calls == 2 {
			_ = tx.AddError(errors.New("keyword backend down"))
			return
		}
		if dest, ok := tx.Statement.Dest.(*[]knowledgedomain.KnowledgeDoc); ok {
			*dest = gateDocs
		}
	})
	provider := NewProvider(db, &mockEmbeddingProvider{dimension: 2, vectors: [][]float32{{1, 0}}}, Config{})
	_, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q", Strategy: "hybrid"})
	if err == nil || !strings.Contains(err.Error(), "failed to execute keyword search") {
		t.Fatalf("expected keyword search error, got %v", err)
	}
}

func TestGateSQLiteTopKTruncation(t *testing.T) {
	db := newGateDB(t, false)
	interceptQuery(db, gateDocs)
	provider := NewProvider(db, &mockEmbeddingProvider{dimension: 2, vectors: [][]float32{{1, 0}}}, Config{Search: SearchConfig{TopK: 1}})
	hits, err := provider.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q", Strategy: "vector"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected topK truncation to 1 hit, got %d", len(hits))
	}
}

func TestFuseRRFGuardsAndTruncation(t *testing.T) {
	// k<=0 兜底为 60。
	got := fuseRRF([]knowledgeprovider.KnowledgeHit{{DocumentID: "a"}}, nil, 5, 0)
	if len(got) != 1 || got[0].Score < 1.0/61-1e-12 || got[0].Score > 1.0/61+1e-12 {
		t.Fatalf("k fallback: %+v", got)
	}
	// topK 截断：两个来源各命中文档，截到 topK=1。
	vector := []knowledgeprovider.KnowledgeHit{{DocumentID: "a"}}
	keyword := []knowledgeprovider.KnowledgeHit{{DocumentID: "b"}}
	got = fuseRRF(vector, keyword, 1, 60)
	if len(got) != 1 || (got[0].DocumentID != "a" && got[0].DocumentID != "b") {
		t.Fatalf("topK truncation: %+v", got)
	}
}
