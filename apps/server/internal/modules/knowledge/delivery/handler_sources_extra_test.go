package delivery

// 覆盖率补充：handler_sources.go（来源登记与索引任务 adapter 面）全量分支——
// 来源创建/列表/删除、文档索引排队执行与失败可见、模型映射与 URL id 解析。
// 本包 HTTP 层不含 gin 路由，按既有风格直接驱动 adapter（HandlerServiceAdapter
// 持有具体 *application.Service，桩接在 application 仓储层）。

import (
	"context"
	"strconv"
	"testing"

	knowledgeapp "servify/apps/server/internal/modules/knowledge/application"
	knowledgedomain "servify/apps/server/internal/modules/knowledge/domain"

	"gorm.io/gorm"
)

func newDeliverySourcesTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newDeliveryTestDB(t)
	if err := db.AutoMigrate(&knowledgedomain.KnowledgeSource{}); err != nil {
		t.Fatalf("automigrate sources: %v", err)
	}
	return db
}

func TestHandlerServiceAdapterSourceLifecycleExtra(t *testing.T) {
	db := newDeliverySourcesTestDB(t)
	adapter := NewHandlerService(db)
	ctx := context.Background()

	if _, err := adapter.CreateSource(ctx, nil); err == nil || err.Error() != "request required" {
		t.Fatalf("expected nil request error, got %v", err)
	}

	created, err := adapter.CreateSource(ctx, &KnowledgeSourceCreateRequest{
		Name:        "帮助中心",
		Type:        "faq",
		Description: "官网 FAQ 抓取",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if created == nil || created.ID == 0 || created.Type != "faq" || created.Description != "官网 FAQ 抓取" {
		t.Fatalf("unexpected created source: %+v", created)
	}
	if _, err := adapter.CreateSource(ctx, &KnowledgeSourceCreateRequest{Name: "x", Type: "video"}); err == nil {
		t.Fatal("expected unknown source type error")
	}

	filtered, err := adapter.ListSources(ctx, "faq")
	if err != nil || len(filtered) != 1 || filtered[0].ID != created.ID {
		t.Fatalf("filtered list = %+v err %v", filtered, err)
	}
	all, err := adapter.ListSources(ctx, "")
	if err != nil || len(all) != 1 {
		t.Fatalf("all list = %+v err %v", all, err)
	}
	if _, err := adapter.ListSources(ctx, "video"); err == nil {
		t.Fatal("expected unknown source type error on list")
	}

	if err := adapter.DeleteSource(ctx, created.ID); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	if err := adapter.DeleteSource(ctx, created.ID); err == nil {
		t.Fatal("expected delete not found error")
	}
}

func TestHandlerServiceAdapterIndexDocumentExtra(t *testing.T) {
	db := newDeliverySourcesTestDB(t)
	adapter := NewHandlerService(db)
	ctx := context.Background()

	created, err := adapter.Create(ctx, &knowledgeapp.KnowledgeDocCreateRequest{
		Title:   "Ix",
		Content: "c",
	})
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}

	docID := strconv.FormatUint(uint64(created.ID), 10)
	result, err := adapter.IndexDocument(ctx, docID)
	if err != nil {
		t.Fatalf("index document: %v", err)
	}
	if result.JobID == "" || result.Status != string(knowledgedomain.IndexJobDone) || result.DocumentVersion != 1 {
		t.Fatalf("unexpected index result: %+v", result)
	}
	if _, err := adapter.IndexDocument(ctx, "9999999"); err == nil {
		t.Fatal("expected get document error for missing doc")
	}

	retried, err := adapter.RetryIndexJob(ctx, result.JobID)
	if err != nil {
		t.Fatalf("retry index job: %v", err)
	}
	if retried.JobID != result.JobID || retried.Status != string(knowledgedomain.IndexJobDone) {
		t.Fatalf("unexpected retry result: %+v", retried)
	}

	// 重试复用同一任务行（done 重跑=按当前文档版本重建），而非新任务。
	jobs, err := adapter.ListIndexJobs(ctx, docID, 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs = %+v err %v", jobs, err)
	}
	if jobs[0].ID != result.JobID || jobs[0].Status != string(knowledgedomain.IndexJobDone) {
		t.Fatalf("unexpected job: %+v", jobs[0])
	}
}

func TestHandlerServiceAdapterIndexQueueErrorExtra(t *testing.T) {
	db := newDeliverySourcesTestDB(t)
	adapter := NewHandlerService(db)
	ctx := context.Background()

	created, err := adapter.Create(ctx, &knowledgeapp.KnowledgeDocCreateRequest{
		Title:   "Ix",
		Content: "c",
	})
	if err != nil {
		t.Fatalf("create doc: %v", err)
	}
	// 任务表不可用 → 排队失败，错误对调用方可见。
	if err := db.Migrator().DropTable(&knowledgedomain.KnowledgeIndexJob{}); err != nil {
		t.Fatalf("drop job table: %v", err)
	}
	if _, err := adapter.IndexDocument(ctx, strconv.FormatUint(uint64(created.ID), 10)); err == nil {
		t.Fatal("expected queue error after drop")
	}
}

func TestSourceModelFromDomainExtra(t *testing.T) {
	if got := sourceModelFromDomain(nil); got != nil {
		t.Fatalf("expected nil model, got %+v", got)
	}
	mapped := sourceModelFromDomain(&knowledgedomain.Source{
		ID:          7,
		Name:        "FAQ",
		Type:        "faq",
		Description: "d",
	})
	if mapped.ID != 7 || mapped.Name != "FAQ" || mapped.Type != "faq" || mapped.Description != "d" {
		t.Fatalf("unexpected mapped source: %+v", mapped)
	}
}

func TestParseSourceIDExtra(t *testing.T) {
	if id, err := parseSourceID("42"); err != nil || id != 42 {
		t.Fatalf("parse 42 = %d err %v", id, err)
	}
	for _, raw := range []string{"", "0", "abc", "-1", "99999999999"} {
		if _, err := parseSourceID(raw); err == nil || err.Error() != "invalid source id" {
			t.Fatalf("parse %q: expected invalid source id, got %v", raw, err)
		}
	}
}
