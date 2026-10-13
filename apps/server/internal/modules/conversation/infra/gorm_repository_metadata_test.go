//go:build integration
// +build integration

package infra

// 历史译文持久化回归（docs/realtime-translation-design.md §1.5「存储列与
// 跨页缓存留后续刀」的收口）：messages.metadata JSON 列的领域往返 +
// 保留键 translations 按语言合并（同语言旧值保留、异语言并存、跨会话/
// 坏入参契约性跳过、作用域隔离）。postgres 列由 000023 提供并受列级
// 守卫钉住；sqlite 走 AutoMigrate。

import (
	"context"
	"fmt"
	"testing"

	"servify/apps/server/internal/models"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	"servify/apps/server/internal/modules/conversation/domain"

	"gorm.io/gorm"
)

func seedMetadataMessage(t *testing.T, db *gorm.DB, id uint, sessionID string, metadata map[string]string) string {
	t.Helper()
	repo := NewGormRepository(db)
	message := &domain.ConversationMessage{
		ConversationID: sessionID,
		Sender:         domain.ParticipantRoleCustomer,
		Kind:           domain.MessageKindText,
		Content:        fmt.Sprintf("visitor text %d", id),
		Metadata:       metadata,
	}
	if err := repo.AppendMessage(context.Background(), message); err != nil {
		t.Fatalf("append message %d: %v", id, err)
	}
	return message.ID
}

func loadMetadataMessage(t *testing.T, db *gorm.DB, sessionID, messageID string) domain.ConversationMessage {
	t.Helper()
	items, err := NewGormRepository(db).ListRecentMessages(context.Background(), sessionID, 50)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	for _, item := range items {
		if item.ID == messageID {
			return item
		}
	}
	t.Fatalf("message %s not found in session %s", messageID, sessionID)
	return domain.ConversationMessage{}
}

func TestMessageMetadataRoundTrip(t *testing.T) {
	db := newConversationInfraTestDB(t)

	id := seedMetadataMessage(t, db, 0, "conv-meta", map[string]string{"source": "admin"})
	got := loadMetadataMessage(t, db, "conv-meta", id)

	if got.Metadata["source"] != "admin" {
		t.Fatalf("metadata must round-trip, got %+v", got.Metadata)
	}

	// 无 metadata 的消息映射为空表（不产生空串键）。
	plain := seedMetadataMessage(t, db, 0, "conv-meta", nil)
	plainItem := loadMetadataMessage(t, db, "conv-meta", plain)
	if len(plainItem.Metadata) != 0 {
		t.Fatalf("plain message metadata must be empty, got %+v", plainItem.Metadata)
	}
}

func TestSaveMessageTranslationsMergesPerLanguage(t *testing.T) {
	db := newConversationInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	id := seedMetadataMessage(t, db, 0, "conv-tl", map[string]string{"source": "admin"})

	if err := repo.SaveMessageTranslations(ctx, []conversationapp.MessageTranslationEntry{{ConversationID: "conv-tl", MessageID: id, Lang: "en", Text: "hello"}}); err != nil {
		t.Fatalf("save en: %v", err)
	}
	got := loadMetadataMessage(t, db, "conv-tl", id)
	if got.Metadata["source"] != "admin" {
		t.Fatalf("existing metadata keys must be preserved, got %+v", got.Metadata)
	}
	if got.Metadata["translations"] != `{"en":"hello"}` {
		t.Fatalf("translations = %q", got.Metadata["translations"])
	}

	// 异语言并存。
	if err := repo.SaveMessageTranslations(ctx, []conversationapp.MessageTranslationEntry{{ConversationID: "conv-tl", MessageID: id, Lang: "zh", Text: "你好"}}); err != nil {
		t.Fatalf("save zh: %v", err)
	}
	got = loadMetadataMessage(t, db, "conv-tl", id)
	if got.Metadata["translations"] != `{"en":"hello","zh":"你好"}` {
		t.Fatalf("translations = %q", got.Metadata["translations"])
	}

	// 同语言键先到先得（并发标注竞态不互相覆盖）。
	if err := repo.SaveMessageTranslations(ctx, []conversationapp.MessageTranslationEntry{{ConversationID: "conv-tl", MessageID: id, Lang: "en", Text: "overwritten"}}); err != nil {
		t.Fatalf("save en again: %v", err)
	}
	got = loadMetadataMessage(t, db, "conv-tl", id)
	if got.Metadata["translations"] != `{"en":"hello","zh":"你好"}` {
		t.Fatalf("existing language key must be preserved, got %q", got.Metadata["translations"])
	}
}

func TestSaveMessageTranslationsSkipsInvalidEntries(t *testing.T) {
	db := newConversationInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	id := seedMetadataMessage(t, db, 0, "conv-skip", nil)
	before := loadMetadataMessage(t, db, "conv-skip", id)

	entries := []conversationapp.MessageTranslationEntry{
		{ConversationID: "conv-skip", MessageID: "not-a-number", Lang: "en", Text: "x"},
		{ConversationID: "conv-skip", MessageID: "0", Lang: "en", Text: "x"},
		{ConversationID: "conv-skip", MessageID: id, Lang: "  ", Text: "x"},
		{ConversationID: "conv-skip", MessageID: id, Lang: "en", Text: "  "},
		{ConversationID: "conv-other", MessageID: id, Lang: "en", Text: "cross session"},
		{ConversationID: "conv-skip", MessageID: "99999", Lang: "en", Text: "missing"},
	}
	if err := repo.SaveMessageTranslations(ctx, entries); err != nil {
		t.Fatalf("invalid entries must be skipped, got error: %v", err)
	}
	after := loadMetadataMessage(t, db, "conv-skip", id)
	if len(after.Metadata) != len(before.Metadata) {
		t.Fatalf("no valid entry, metadata must be untouched, got %+v", after.Metadata)
	}
}

func TestSaveMessageTranslationsRespectsScope(t *testing.T) {
	db := newConversationInfraTestDB(t)
	repo := NewGormRepository(db)
	scoped := scopedConversationContext("tenant-a", "workspace-a")

	id := seedMetadataMessage(t, db, 0, "conv-scope", nil)

	// 无作用域上下文写入（消息 tenant/workspace 为空串），带作用域读取
	// 命不中该行 → 条目静默跳过，不报错。
	if err := repo.SaveMessageTranslations(scoped, []conversationapp.MessageTranslationEntry{{ConversationID: "conv-scope", MessageID: id, Lang: "en", Text: "hello"}}); err != nil {
		t.Fatalf("scoped save must not error: %v", err)
	}
	got := loadMetadataMessage(t, db, "conv-scope", id)
	if _, ok := got.Metadata["translations"]; ok {
		t.Fatalf("scoped entry must not hit unscoped row, got %+v", got.Metadata)
	}

	// 空批不触库、不报错。
	if err := repo.SaveMessageTranslations(scoped, nil); err != nil {
		t.Fatalf("empty batch must be a no-op: %v", err)
	}
}

func TestSaveMessageTranslationsPropagatesDBErrors(t *testing.T) {
	db := newConversationInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	entries := []conversationapp.MessageTranslationEntry{{
		ConversationID: "conv-err",
		MessageID:      seedMetadataMessage(t, db, 0, "conv-err", nil),
		Lang:           "en",
		Text:           "hello",
	}}

	// 消息表已 drop → 行查找本身报错，须上抛（历史加载链路由调用方兜底）。
	if err := db.Migrator().DropTable("messages"); err != nil {
		t.Fatalf("drop messages: %v", err)
	}
	if err := repo.SaveMessageTranslations(ctx, entries); err == nil {
		t.Fatal("missing messages table must propagate an error")
	}
}

func TestSaveMessageTranslationsPropagatesUpdateError(t *testing.T) {
	db := newConversationInfraTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	id := seedMetadataMessage(t, db, 0, "conv-trig", nil)
	// 定向拦截 UPDATE：行查找成功、写入失败——覆盖持久化失败上抛分支
	// （调用方按"失败静默、下次翻页再译再写"兜底）。
	if err := db.Exec("CREATE TRIGGER tg_block_metadata BEFORE UPDATE OF metadata ON messages BEGIN SELECT RAISE(ABORT, 'no metadata update'); END").Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	defer db.Exec("DROP TRIGGER tg_block_metadata")

	err := repo.SaveMessageTranslations(ctx, []conversationapp.MessageTranslationEntry{{
		ConversationID: "conv-trig", MessageID: id, Lang: "en", Text: "hello",
	}})
	if err == nil {
		t.Fatal("blocked update must propagate an error")
	}
}

func TestDecodeMessageMetadataIgnoresBrokenJSON(t *testing.T) {
	db := newConversationInfraTestDB(t)
	message := &domain.ConversationMessage{
		ConversationID: "conv-broken",
		Sender:         domain.ParticipantRoleCustomer,
		Kind:           domain.MessageKindText,
		Content:        "visitor text",
	}
	if err := NewGormRepository(db).AppendMessage(context.Background(), message); err != nil {
		t.Fatalf("append message: %v", err)
	}
	// 绕过编解码写入坏 JSON，验证读取侧静默降级为空表（增强能力不因
	// 脏数据断历史加载）。
	if err := db.Model(&models.Message{}).Where("id = ?", message.ID).Update("metadata", "{bad json").Error; err != nil {
		t.Fatalf("write broken metadata: %v", err)
	}
	got := loadMetadataMessage(t, db, "conv-broken", message.ID)
	if len(got.Metadata) != 0 {
		t.Fatalf("broken metadata must decode to an empty map, got %+v", got.Metadata)
	}
}
