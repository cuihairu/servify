package local

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"servify/apps/server/internal/models"
	localllm "servify/apps/server/internal/platform/embedding/local"
	"servify/apps/server/internal/platform/knowledgeprovider"
)

// knowledgeDocModel 是 KnowledgeDoc 的测试侧别名（表结构唯一真源在 domain）。
type knowledgeDocModel = models.KnowledgeDoc

// newTestProvider 构造 sqlite in-memory 库 + 真实本地嵌入器 + 本地引擎。
func newTestProvider(t *testing.T, db *gorm.DB, cfg Config) (*Provider, error) {
	t.Helper()
	return NewProvider(db, localllm.NewProvider(localllm.Config{}), cfg), nil
}

// openDB 打开 sqlite in-memory 并迁移 KnowledgeDoc 表（pgvector 测试同款）。
func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&knowledgeDocModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// TestSearchExactOverlapHits 强词面命中越过编排默认 0.7 相关门且排序第一；
// 不相关查询被阈值滤除。
func TestSearchExactOverlapHits(t *testing.T) {
	p, err := newTestProvider(t, openDB(t), Config{
		Search: SearchConfig{TopK: 3, Threshold: 0.7},
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	doc := knowledgeprovider.KnowledgeDocument{
		ID:       "doc-1",
		TenantID: "tenant-a",
		Title:    "退货政策",
		Content:  "本平台支持七天无理由退货，退款将在三到五个工作日内原路退回。",
	}
	if _, err := p.UpsertDocument(context.Background(), doc); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	hits, err := p.Search(context.Background(), knowledgeprovider.SearchRequest{
		Query:           "七天无理由退货退款",
		TenantID:        "tenant-a",
		TopK:            3,
		Threshold:       0.7,
		Strategy:        "semantic",
		ConsistencyMode: knowledgeprovider.ConsistencyEventual,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].Title != "退货政策" || hits[0].Source != ProviderID {
		t.Fatalf("hit = %+v, want title/source match", hits[0])
	}
	if hits[0].Score < 0.7 {
		t.Fatalf("score = %v, want >= 0.7", hits[0].Score)
	}
	// 不相关查询：无重叠词元 → 余弦 0 → 校准 0 → 被阈值滤除。
	none, err := p.Search(context.Background(), knowledgeprovider.SearchRequest{
		Query:    "宇宙飞船发射计划",
		TenantID: "tenant-a",
		TopK:     3,
	})
	if err != nil {
		t.Fatalf("search none: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("unrelated query hits = %d, want 0", len(none))
	}
}

// TestSearchTenantAndKnowledgeIsolation 租户/知识库维度隔离：跨维不命中。
func TestSearchTenantAndKnowledgeIsolation(t *testing.T) {
	p, _ := newTestProvider(t, openDB(t), Config{})
	base := knowledgeprovider.KnowledgeDocument{
		ID:       "doc-iso",
		TenantID: "tenant-a",
		Title:    "内部流程",
		Content:  "服务台工单升级流程共三步：登记、分派、回访。",
	}
	if _, err := p.UpsertDocument(context.Background(), base); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	for _, req := range []knowledgeprovider.SearchRequest{
		{Query: "工单升级流程", TenantID: "tenant-b"},     // 异租户
		{Query: "工单升级流程", KnowledgeID: "kb-x"},      // 异知识库（workspace）
		{TenantID: "tenant-a", Strategy: "unknown"}, // 带非法面（依旧隔租户）
	} {
		hits, err := p.Search(context.Background(), req)
		if err != nil && req.Strategy != "unknown" {
			t.Fatalf("search %+v: %v", req, err)
		}
		if err == nil && len(hits) != 0 {
			t.Fatalf("isolated search %+v hits = %d, want 0", req, len(hits))
		}
	}
}

// TestUpsertReplaceAndDelete 更新替换全部分块、删除清空可检索面。
func TestUpsertReplaceAndDelete(t *testing.T) {
	p, _ := newTestProvider(t, openDB(t), Config{Search: SearchConfig{TopK: 5}})
	seed := knowledgeprovider.KnowledgeDocument{
		ID:       "doc-r",
		TenantID: "t",
		Content:  "旧版客服热线为 400-100-2000，工作时间 9 点到 18 点。",
		Tags:     []string{"旧版"},
	}
	externalID, err := p.UpsertDocument(context.Background(), seed)
	if err != nil || externalID != "doc-r" {
		t.Fatalf("upsert old: %v, %v", externalID, err)
	}
	// 更新：内容改为新热线，旧的对外部 id 的分块应被全量替换。
	fresh := seed
	fresh.Content = "新版客服热线为 400-888-9999，支持 7x24 小时服务。"
	fresh.Tags = []string{"新版"}
	if _, err := p.UpsertDocument(context.Background(), fresh); err != nil {
		t.Fatalf("upsert new: %v", err)
	}
	hits, err := p.Search(context.Background(), knowledgeprovider.SearchRequest{
		Query:    "新版热线 400 888 9999",
		TenantID: "t", TopK: 5,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 || !strings.Contains(hits[0].Content, "400-888-9999") {
		t.Fatalf("updated content not retrievable: %+v", hits)
	}
	var count int64
	if err := p.db.Model(&knowledgeDocModel{}).Where("external_id = ?", "doc-r").Count(&count).Error; err != nil || count > 4 {
		t.Fatalf("chunk count = %d (err %v), want <= 4", count, err)
	}
	if err := p.DeleteDocument(context.Background(), "doc-r"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	none, err := p.Search(context.Background(), knowledgeprovider.SearchRequest{
		Query: "400 888 9999", TenantID: "t", TopK: 5,
	})
	if err != nil || len(none) != 0 {
		t.Fatalf("after delete: hits=%d err=%v, want 0", len(none), err)
	}
}

// TestUpsertChunksWithOverlap 超长内容按配置分块：多块落库、块的文档 id 相同。
func TestUpsertChunksWithOverlap(t *testing.T) {
	p, _ := newTestProvider(t, openDB(t), Config{
		Indexing: IndexingConfig{ChunkSize: 40, ChunkOverlap: 10},
	})
	long := strings.Repeat("服务台处理工单的标准流程是登记分派回访闭环，", 20)
	externalID, err := p.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ID: "doc-long", TenantID: "t", Content: long,
	})
	if err != nil || externalID != "doc-long" {
		t.Fatalf("upsert: %v, %v", externalID, err)
	}
	var count int64
	if err := p.db.Model(&knowledgeDocModel{}).Where("external_id = ?", "doc-long").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count < 2 {
		t.Fatalf("chunks = %d, want >= 2", count)
	}
}

// TestSearchTopKAndRanking 超出 topK 时的排序截断：相关度高的排前。
func TestSearchTopKAndRanking(t *testing.T) {
	p, _ := newTestProvider(t, openDB(t), Config{})
	docs := []knowledgeprovider.KnowledgeDocument{
		{ID: "a-1", TenantID: "t", Content: "售后退款流程：联系客服提交退款申请。"},
		{ID: "a-2", TenantID: "t", Content: "退货地址：北京市朝阳区某某仓库。"},
		{ID: "a-3", TenantID: "t", Content: "欢迎语：感谢选择本平台。"},
	}
	for _, d := range docs {
		if _, err := p.UpsertDocument(context.Background(), d); err != nil {
			t.Fatalf("upsert %s: %v", d.ID, err)
		}
	}
	hits, err := p.Search(context.Background(), knowledgeprovider.SearchRequest{
		Query: "售后退款如何联系客服提交申请", TenantID: "t", TopK: 2,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].DocumentID == "" || hits[1].DocumentID == "" {
		t.Fatalf("hit document ids empty")
	}
	if hits[0].Score < hits[1].Score {
		t.Fatalf("ranking not descending: %v < %v", hits[0].Score, hits[1].Score)
	}
	// 阈值 0 不过滤、topK 兜底默认 10。
	all, err := p.Search(context.Background(), knowledgeprovider.SearchRequest{
		Query: "售后退款如何联系客服提交申请", TenantID: "t", TopK: 0, Threshold: 0,
	})
	if err != nil || len(all) < 3 {
		t.Fatalf("topK default: %d hits, err %v", len(all), err)
	}
}

// TestSearchErrorPaths 嵌入失败/空结果/未知策略/维度不匹配的显式错误。
func TestSearchErrorPaths(t *testing.T) {
	db := openDB(t)
	// 嵌入失败：p.embedding 用注入错误桩。
	failingEmb := &failingEmbedding{err: errors.New("embed api down")}
	prov := NewProvider(db, failingEmb, Config{})
	if _, err := prov.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q"}); err == nil {
		t.Fatalf("embed error should surface")
	}
	// 空结果嵌入返回。
	emptyEmb := &failingEmbedding{empty: true}
	prov2 := NewProvider(db, emptyEmb, Config{})
	if _, err := prov2.Search(context.Background(), knowledgeprovider.SearchRequest{Query: "q"}); err == nil {
		t.Fatalf("empty embed result should surface")
	}
	// 非法策略显式报错。
	p, _ := newTestProvider(t, db, Config{})
	if _, err := p.Search(context.Background(), knowledgeprovider.SearchRequest{
		Query: "q", Strategy: "bm25",
	}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unknown strategy should fail, got %v", err)
	}
	// 维度不匹配的分块被跳过：直插一条异维向量。
	realEmb := localllm.NewProvider(localllm.Config{})
	prov3 := NewProvider(db, realEmb, Config{Search: SearchConfig{TopK: 5}})
	if _, err := prov3.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ID: "doc-x", TenantID: "t", Content: "某一维度不匹配分块内容",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.Model(&knowledgeDocModel{}).Where("external_id = ?", "doc-x").
		Update("embedding", "[]").Error; err != nil {
		t.Fatalf("corrupt embedding: %v", err)
	}
	hits, err := prov3.Search(context.Background(), knowledgeprovider.SearchRequest{
		Query: "某一维度不匹配分块内容", TenantID: "t", TopK: 5,
	})
	if err != nil || len(hits) != 0 {
		t.Fatalf("mismatched-dim chunks should be skipped, hits=%d err=%v", len(hits), err)
	}
}

// TestHealthCheck 数据库连通 + 嵌入器健康；注入失败的嵌入器让健康检查失败。
func TestHealthCheck(t *testing.T) {
	db := openDB(t)
	realEmb := localllm.NewProvider(localllm.Config{})
	prov, _ := newTestProvider(t, db, Config{})
	if err := prov.HealthCheck(context.Background()); err != nil {
		t.Fatalf("health = %v, want nil", err)
	}
	prov.embedding = &failingEmbedding{healthErr: errors.New("embedding down")}
	if err := prov.HealthCheck(context.Background()); err == nil {
		t.Fatalf("embedding health failure should surface")
	}
	closed, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, _ := closed.DB()
	_ = sqlDB.Close()
	provBroken := NewProvider(closed, realEmb, Config{})
	if err := provBroken.HealthCheck(context.Background()); err == nil {
		t.Fatalf("closed db health should fail")
	}
}

// TestCalibrationAndNorm 校准函数保序、非负；cosine 零/负值归零。
func TestCalibrationAndNorm(t *testing.T) {
	if calibratedScore(0) != 0 || calibratedScore(-0.5) != 0 {
		t.Fatalf("non-positive cosine should map to 0")
	}
	if calibratedScore(0.5) <= calibratedScore(0.3) {
		t.Fatalf("calibration must be monotonic")
	}
	if calibratedScore(1) != 1 {
		t.Fatalf("calibrated(1) = %v, want 1", calibratedScore(1))
	}
	if got := normalizedCosine(); got < 0 || got > 1 {
		t.Fatalf("sim = %v", got)
	}
	zeroA, zeroB := make([]float32, 8), make([]float32, 8)
	if cosineSimilarity(zeroA, zeroB) != 0 {
		t.Fatalf("zero vectors similarity should be 0")
	}
	a := make([]float32, 8)
	a[3] = 1
	if got := cosineSimilarity(a, zeroB); got != 0 {
		t.Fatalf("zero-vs-nonzero similarity should be 0, got %v", got)
	}
}

// normalizedCosine 构造归一化真值样本，返回余弦（验证 clamp 路径非负）。
func normalizedCosine() float64 {
	a := []float32{0.6, 0.8}
	b := []float32{-0.6, -0.8} // 夹角的余弦 < 0 → clamp 0
	return cosineSimilarity(a, b)
}

// TestStrategyNormalize 策略别名归一化。
func TestStrategyNormalize(t *testing.T) {
	for _, in := range []string{"", "semantic", "hybrid", "COSINE", " cosine "} {
		if got := normalizeSearchStrategy(in); got != "cosine" {
			t.Fatalf("normalize(%q) = %q, want cosine", in, got)
		}
	}
	if got := normalizeSearchStrategy("euclidean"); got != "euclidean" {
		t.Fatalf("normalize(euclidean) = %q", got)
	}
}

// TestConfigDefaults 零配置兜底：topK 10、策略 cosine、chunk 500。
func TestConfigDefaults(t *testing.T) {
	p, _ := newTestProvider(t, openDB(t), Config{})
	if p.config.Search.TopK != 10 || p.config.Search.Strategy != "cosine" || p.config.Indexing.ChunkSize != 500 {
		t.Fatalf("defaults mismatch: %+v", p.config)
	}
}

// TestDeleteUnknownId 删除不存在的文档不报错（与 pgvector 同契约）。
func TestDeleteUnknownId(t *testing.T) {
	p, _ := newTestProvider(t, openDB(t), Config{})
	if err := p.DeleteDocument(context.Background(), "no-such-doc"); err != nil {
		t.Fatalf("delete unknown = %v, want nil", err)
	}
}

// TestUpsertEmbedError 嵌入失败时 Upsert 显式报错且不落库。
func TestUpsertEmbedError(t *testing.T) {
	db := openDB(t)
	prov := NewProvider(db, &failingEmbedding{err: errors.New("down")}, Config{})
	if _, err := prov.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ID: "doc-e", Content: "内容",
	}); err == nil {
		t.Fatalf("embed failure should surface in upsert")
	}
	prov2 := NewProvider(db, &failingEmbedding{empty: true}, Config{})
	if _, err := prov2.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ID: "doc-e2", Content: "内容",
	}); err == nil {
		t.Fatalf("embed count mismatch should surface")
	}
}

// failingEmbedding 是注入失败的嵌入桩（错误 / 空结果 / 健康错误）。
type failingEmbedding struct {
	err       error
	empty     bool
	healthErr error
}

func (f *failingEmbedding) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.empty {
		return [][]float32{}, nil
	}
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = make([]float32, 8)
		out[i][0] = 1
	}
	return out, nil
}

func (f *failingEmbedding) Dimension() int { return 8 }

func (f *failingEmbedding) HealthCheck(_ context.Context) error { return f.healthErr }

// 编译期确认实现端口（与 quality/infra 同款防御）。
var _ knowledgeprovider.KnowledgeProvider = (*Provider)(nil)
