package infra

// 裸模式（无 integration tag）单测：SaveMessageTranslations 的契约性跳过
// 矩阵、metadata 保留键合并语义（同语言键先到先得）与 DB 错误上抛，以及
// messages.metadata 列编解码的往返表。沿用既有约定：查询错误用 drop-table
// 强制触发（见 gorm_repository_unit_test.go）。

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"servify/apps/server/internal/models"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	platformauth "servify/apps/server/internal/platform/auth"
)

func TestSaveMessageTranslationsContractSkipMatrix(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "t1", "w1")

	seed := &models.Message{TenantID: "t1", WorkspaceID: "w1", SessionID: "conv-1", Content: "你好", Type: "text", Sender: "user", Metadata: `{"custom":"keep"}`}
	if err := db.Create(seed).Error; err != nil {
		t.Fatalf("seed message: %v", err)
	}

	cases := []struct {
		name  string
		entry conversationapp.MessageTranslationEntry
	}{
		{"non numeric id", conversationapp.MessageTranslationEntry{ConversationID: "conv-1", MessageID: "abc", Lang: "en", Text: "hello"}},
		{"zero id", conversationapp.MessageTranslationEntry{ConversationID: "conv-1", MessageID: "0", Lang: "en", Text: "hello"}},
		{"blank lang", conversationapp.MessageTranslationEntry{ConversationID: "conv-1", MessageID: "1", Lang: "   ", Text: "hello"}},
		{"empty text", conversationapp.MessageTranslationEntry{ConversationID: "conv-1", MessageID: "1", Lang: "en", Text: ""}},
		{"blank text", conversationapp.MessageTranslationEntry{ConversationID: "conv-1", MessageID: "1", Lang: "en", Text: "   "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := repo.SaveMessageTranslations(ctx, []conversationapp.MessageTranslationEntry{tc.entry}); err != nil {
				t.Fatalf("expected contract skip, got error %v", err)
			}
			var row models.Message
			if err := db.First(&row, seed.ID).Error; err != nil {
				t.Fatalf("reload: %v", err)
			}
			if row.Metadata != `{"custom":"keep"}` {
				t.Fatalf("metadata changed on contract skip: %s", row.Metadata)
			}
		})
	}
}

func TestSaveMessageTranslationsMergePreservesKeysFirstWriteWins(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "t1", "w1")

	seed := &models.Message{TenantID: "t1", WorkspaceID: "w1", SessionID: "conv-1", Content: "你好", Type: "text", Sender: "user", Metadata: `{"custom":"v","translations":"{\"zh\":\"你好\"}"}`}
	if err := db.Create(seed).Error; err != nil {
		t.Fatalf("seed message: %v", err)
	}

	// 新语言键落库，既有键（custom / zh）保留。
	if err := repo.SaveMessageTranslations(ctx, []conversationapp.MessageTranslationEntry{
		{ConversationID: "conv-1", MessageID: "1", Lang: "en", Text: "hello"},
	}); err != nil {
		t.Fatalf("save en: %v", err)
	}
	var row models.Message
	if err := db.First(&row, seed.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	var metadata map[string]string
	if err := json.Unmarshal([]byte(row.Metadata), &metadata); err != nil {
		t.Fatalf("metadata not json: %s", row.Metadata)
	}
	if metadata["custom"] != "v" {
		t.Fatalf("custom key lost: %s", row.Metadata)
	}
	var translations map[string]string
	if err := json.Unmarshal([]byte(metadata[conversationapp.MessageTranslationsKey]), &translations); err != nil {
		t.Fatalf("translations not json: %s", row.Metadata)
	}
	if translations["zh"] != "你好" || translations["en"] != "hello" {
		t.Fatalf("translations = %+v（zh 保留 + en 新增）", translations)
	}

	// 同语言键已存在：先到先得，后写不改旧值。
	if err := repo.SaveMessageTranslations(ctx, []conversationapp.MessageTranslationEntry{
		{ConversationID: "conv-1", MessageID: "1", Lang: "zh", Text: "应该写不进去"},
	}); err != nil {
		t.Fatalf("save zh again: %v", err)
	}
	if err := db.First(&row, seed.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := json.Unmarshal([]byte(row.Metadata), &metadata); err != nil {
		t.Fatalf("metadata not json: %s", row.Metadata)
	}
	if err := json.Unmarshal([]byte(metadata[conversationapp.MessageTranslationsKey]), &translations); err != nil {
		t.Fatalf("translations not json: %s", row.Metadata)
	}
	if translations["zh"] != "你好" {
		t.Fatalf("zh = %q（同语言键先到先得）", translations["zh"])
	}
}

func TestSaveMessageTranslationsUnknownMessageAndCrossScopeSkip(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := platformauth.ContextWithScope(context.Background(), "t1", "w1")

	seed := &models.Message{TenantID: "t1", WorkspaceID: "w1", SessionID: "conv-1", Content: "你好", Type: "text", Sender: "user"}
	if err := db.Create(seed).Error; err != nil {
		t.Fatalf("seed message: %v", err)
	}

	// 跨会话/不存在的消息静默跳过，不报错。
	if err := repo.SaveMessageTranslations(ctx, []conversationapp.MessageTranslationEntry{
		{ConversationID: "conv-other", MessageID: "1", Lang: "en", Text: "hello"},
		{ConversationID: "conv-1", MessageID: "999", Lang: "en", Text: "hello"},
	}); err != nil {
		t.Fatalf("expected silent skip, got %v", err)
	}
	var row models.Message
	if err := db.First(&row, seed.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.Metadata != "" {
		t.Fatalf("metadata written on skip: %s", row.Metadata)
	}

	// 跨租户 scope 不可见：同样静默跳过。
	ctxOther := platformauth.ContextWithScope(context.Background(), "t2", "w1")
	if err := repo.SaveMessageTranslations(ctxOther, []conversationapp.MessageTranslationEntry{
		{ConversationID: "conv-1", MessageID: "1", Lang: "en", Text: "hello"},
	}); err != nil {
		t.Fatalf("expected cross-tenant skip, got %v", err)
	}
	if err := db.First(&row, seed.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.Metadata != "" {
		t.Fatalf("metadata written on cross-tenant skip: %s", row.Metadata)
	}
}

func TestSaveMessageTranslationsDBErrorPropagates(t *testing.T) {
	db := newConversationUnitTestDB(t)
	repo := NewGormRepository(db)
	ctx := context.Background()

	if err := db.Migrator().DropTable(&models.Message{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	err := repo.SaveMessageTranslations(ctx, []conversationapp.MessageTranslationEntry{
		{ConversationID: "conv-1", MessageID: "1", Lang: "en", Text: "hello"},
	})
	if err == nil {
		t.Fatal("expected db error to propagate")
	}
}

func TestMessageMetadataCodecTable(t *testing.T) {
	decodeCases := []struct {
		name string
		raw  string
		want map[string]string
	}{
		{"empty column", "", nil},
		{"bad json", "not-json", nil},
		{"json array", "[]", nil},
		{"valid object", `{"a":"b"," translations ":"x"}`, map[string]string{"a": "b", " translations ": "x"}},
		{"empty object", "{}", map[string]string{}},
	}
	for _, tc := range decodeCases {
		t.Run("decode/"+tc.name, func(t *testing.T) {
			got := decodeMessageMetadata(tc.raw)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("decode(%q) = %+v want nil", tc.raw, got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("decode(%q) = %+v want %+v", tc.raw, got, tc.want)
			}
		})
	}

	encodeCases := []struct {
		name  string
		meta  map[string]string
		valid func(t *testing.T, got string)
	}{
		{"nil map", nil, func(t *testing.T, got string) {
			if got != "" {
				t.Fatalf("encode(nil) = %q want empty", got)
			}
		}},
		{"empty map", map[string]string{}, func(t *testing.T, got string) {
			if got != "" {
				t.Fatalf("encode({}) = %q want empty（列保持「无扩展」形态）", got)
			}
		}},
		{"single pair", map[string]string{"a": "b"}, func(t *testing.T, got string) {
			decoded := decodeMessageMetadata(got)
			if !reflect.DeepEqual(decoded, map[string]string{"a": "b"}) {
				t.Fatalf("round trip = %+v via %q", decoded, got)
			}
		}},
	}
	for _, tc := range encodeCases {
		t.Run("encode/"+tc.name, func(t *testing.T) {
			tc.valid(t, encodeMessageMetadata(tc.meta))
		})
	}
}
