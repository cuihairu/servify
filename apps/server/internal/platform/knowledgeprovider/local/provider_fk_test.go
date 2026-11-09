package local

// 全新 PostgreSQL 上无 id=0 knowledge_bases 父行，无归属文档以
// knowledge_base_id=0 落库会撞 fk_knowledge_docs_knowledge_base
// （SQLSTATE 23503，todo.md 198）。sqlite 默认不强制外键，历史单测因此未
// 暴露；本文件用 DSN 级 _foreign_keys=on 让池内每条连接都强制约束，并先做
// 悬空引用自校验（探针必须真的会失败，否则 harness 无效）。修复语义与
// legacy CRUD（GormDocumentRepository.Create FirstOrCreate）及迁移 000025
// 回填一致：无归属文档挂当前作用域 Default 知识库。

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	knowledgedomain "servify/apps/server/internal/modules/knowledge/domain"
	"servify/apps/server/internal/platform/knowledgeprovider"
)

// openFKDB 打开强制外键的 sqlite 内存库（DSN 按用例命名避免共享缓存串态），
// 迁移知识库与文档两张表，并以悬空引用自校验约束探针生效。
func openFKDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := "knowledge_fk_local_" + strings.ReplaceAll(t.Name(), "/", "_")
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared&_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&knowledgedomain.KnowledgeBase{}, &knowledgedomain.KnowledgeDoc{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	dangling := &knowledgedomain.KnowledgeDoc{TenantID: "probe", WorkspaceID: "probe", KnowledgeBaseID: 99999, Title: "probe", Content: "probe"}
	if err := db.Create(dangling).Error; err == nil {
		t.Fatal("foreign key probe passed unexpectedly; enforcement harness invalid")
	}
	if err := db.Exec("DELETE FROM knowledge_docs").Error; err != nil {
		t.Fatalf("cleanup probe row: %v", err)
	}
	return db
}

// TestUpsertDocumentAttachesDefaultKnowledgeBaseUnderFK 无归属上传在强制外键
// 的全新库上必须成功，且文档挂到已存在的当前作用域 Default 知识库。
func TestUpsertDocumentAttachesDefaultKnowledgeBaseUnderFK(t *testing.T) {
	db := openFKDB(t)
	p, err := newTestProvider(t, db, Config{Search: SearchConfig{TopK: 3, Threshold: 0.7}})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if _, err := p.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ID: "doc-fk", TenantID: "tenant-a", KnowledgeID: "ws-a",
		Title:   "退货政策",
		Content: "本平台支持七天无理由退货，退款将在三到五个工作日内原路退回。",
	}); err != nil {
		t.Fatalf("upsert under enforced foreign keys: %v", err)
	}
	var row knowledgeDocModel
	if err := db.Where("external_id = ?", "doc-fk").First(&row).Error; err != nil {
		t.Fatalf("load doc: %v", err)
	}
	var base knowledgedomain.KnowledgeBase
	if err := db.First(&base, row.KnowledgeBaseID).Error; err != nil {
		t.Fatalf("doc references missing knowledge base %d: %v", row.KnowledgeBaseID, err)
	}
	if base.Name != "Default" || base.TenantID != "tenant-a" || base.WorkspaceID != "ws-a" {
		t.Fatalf("default base = %+v, want tenant-a/ws-a Default", base)
	}
}

// TestUpsertDocumentDefaultScopeFallbackUnderFK 空 tenant/workspace 归一为
// default 作用域（与 legacy CRUD 同回退），外键强制下同样可落库。
func TestUpsertDocumentDefaultScopeFallbackUnderFK(t *testing.T) {
	db := openFKDB(t)
	p, err := newTestProvider(t, db, Config{Search: SearchConfig{TopK: 3, Threshold: 0.7}})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if _, err := p.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{
		ID:      "doc-fk-anon",
		Title:   "匿名作用域",
		Content: "无租户上下文的上传落 default 作用域默认知识库。",
	}); err != nil {
		t.Fatalf("upsert under enforced foreign keys: %v", err)
	}
	var base knowledgedomain.KnowledgeBase
	if err := db.Where("tenant_id = ? AND workspace_id = ? AND name = ?", "default", "default", "Default").First(&base).Error; err != nil {
		t.Fatalf("default-scope knowledge base missing: %v", err)
	}
}

// TestUpsertDocumentDefaultKnowledgeBaseErrorWrapped 默认库解析失败（库不可
// 写）时上传报错且错误可辨识，不得静默落 knowledge_base_id=0。
func TestUpsertDocumentDefaultKnowledgeBaseErrorWrapped(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&knowledgedomain.KnowledgeDoc{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	_ = sqlDB.Close()
	p, err := newTestProvider(t, db, Config{})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	// externalID 为空 → 删除步骤短路 → 首个触库点即默认库解析，错误须可辨识。
	_, err = p.UpsertDocument(context.Background(), knowledgeprovider.KnowledgeDocument{Title: "t", Content: "c"})
	if err == nil || !strings.Contains(err.Error(), "default knowledge base") {
		t.Fatalf("want default knowledge base error, got %v", err)
	}
}
