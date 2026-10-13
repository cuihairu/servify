package handlers

// 历史译文跨页缓存回归（docs/realtime-translation-design.md §1.5「存储列与
// 跨页缓存留后续刀」的收口）：messages.metadata 保留键 translations 按
// 语言键命中即免 LLM 调用，未命中现译现写；偏好变更不串读旧语言键；
// 持久化失败静默不影响响应标注。

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/models"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// translationStoreStub 译文持久化替身：记录每次保存的按语言条目。
type translationStoreStub struct {
	calls int
	got   []conversationdelivery.TranslationEntry
	err   error
}

func (s *translationStoreStub) SaveTranslations(_ context.Context, entries []conversationdelivery.TranslationEntry) error {
	s.calls++
	s.got = append(s.got, entries...)
	return s.err
}

func seedMessageWithMetadata(t *testing.T, db *gorm.DB, id uint, metadata string) {
	t.Helper()
	if err := db.Create(&models.Message{
		ID:        id,
		SessionID: "sess-1",
		UserID:    1,
		Content:   "cached visitor text",
		Type:      "text",
		Sender:    "customer",
		Metadata:  metadata,
	}).Error; err != nil {
		t.Fatalf("seed message %d: %v", id, err)
	}
}

func TestListMessagesHistoryTranslationCache(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("cached translation served without LLM call", func(t *testing.T) {
		db := newConversationWorkspaceTestDB(t)
		seedMessageWithMetadata(t, db, 100, `{"translations":"{\"en\":\"cached hello\"}"}`)
		history := &historyTranslatorStub{lang: "en", texts: []string{"fresh"}}
		store := &translationStoreStub{}
		handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(seedConversation(t, db)), nil, nil).
			WithHistoryTranslator(history).
			WithTranslationStore(store)

		var hit bool
		for _, item := range listMessages(t, handler) {
			if item.Content == "cached visitor text" {
				hit = true
				if item.Metadata["translation"] != "cached hello" || item.Metadata["translation_lang"] != "en" {
					t.Fatalf("cached entry must be served as response keys, got %+v", item.Metadata)
				}
			}
		}
		if !hit {
			t.Fatal("seeded message must be on the page")
		}
		// 页内未命中段仍需现译（"hello"），缓存命中段不得混入出站文本。
		if len(history.gotTexts) != 1 || history.gotTexts[0] != "hello" {
			t.Fatalf("only uncached segment must hit the LLM, got %v", history.gotTexts)
		}
		// 缓存命中段不落库——持久化的只有"hello"的现译条目。
		if store.calls != 1 || len(store.got) != 1 || store.got[0].Text != "fresh" {
			t.Fatalf("only the fresh entry must be persisted, calls=%d got=%+v", store.calls, store.got)
		}
	})

	t.Run("mixed page persists only the fresh translation", func(t *testing.T) {
		db := newConversationWorkspaceTestDB(t)
		seedMessageWithMetadata(t, db, 100, `{"translations":"{\"en\":\"cached hello\"}"}`)
		history := &historyTranslatorStub{lang: "en", texts: []string{"fresh translation"}}
		store := &translationStoreStub{}
		handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(seedConversation(t, db)), nil, nil).
			WithHistoryTranslator(history).
			WithTranslationStore(store)

		for _, item := range listMessages(t, handler) {
			switch item.Content {
			case "cached visitor text":
				if item.Metadata["translation"] != "cached hello" {
					t.Fatalf("cached entry must be served, got %+v", item.Metadata)
				}
			case "hello":
				if item.Metadata["translation"] != "fresh translation" || item.Metadata["translation_lang"] != "en" {
					t.Fatalf("uncached entry must be annotated, got %+v", item.Metadata)
				}
			case "hi there":
				if item.Metadata["translation"] != "" {
					t.Fatalf("agent message must not be annotated, got %+v", item.Metadata)
				}
			}
		}
		if store.calls != 1 || len(store.got) != 1 {
			t.Fatalf("exactly one fresh entry must be persisted, calls=%d got=%+v", store.calls, store.got)
		}
		entry := store.got[0]
		if entry.ConversationID != "sess-1" || entry.Lang != "en" || entry.Text != "fresh translation" || entry.MessageID == "" {
			t.Fatalf("persisted entry = %+v", entry)
		}
	})

	t.Run("stale language key is not served for a new preference", func(t *testing.T) {
		db := newConversationWorkspaceTestDB(t)
		seedMessageWithMetadata(t, db, 100, `{"translations":"{\"ja\":\"日文缓存\"}"}`)
		history := &historyTranslatorStub{lang: "en", texts: []string{"fresh en"}}
		store := &translationStoreStub{}
		handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(seedConversation(t, db)), nil, nil).
			WithHistoryTranslator(history).
			WithTranslationStore(store)

		for _, item := range listMessages(t, handler) {
			if item.Content == "cached visitor text" && item.Metadata["translation"] != "fresh en" {
				t.Fatalf("stale language must be re-translated, got %+v", item.Metadata)
			}
		}
		if history.gotSession != "sess-1" {
			t.Fatalf("stale key must not short-circuit the translator, session=%q", history.gotSession)
		}
		if len(store.got) != 1 || store.got[0].Lang != "en" {
			t.Fatalf("fresh language must be persisted, got %+v", store.got)
		}
	})

	t.Run("persistence failure keeps the annotation", func(t *testing.T) {
		db := newConversationWorkspaceTestDB(t)
		history := &historyTranslatorStub{lang: "en", texts: []string{"fresh en"}}
		store := &translationStoreStub{err: errors.New("db down")}
		handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(seedConversation(t, db)), nil, nil).
			WithHistoryTranslator(history).
			WithTranslationStore(store)

		for _, item := range listMessages(t, handler) {
			if item.Content == "hello" && item.Metadata["translation"] != "fresh en" {
				t.Fatalf("store failure must not affect annotation, got %+v", item.Metadata)
			}
		}
	})

	t.Run("no preference never reads cache nor persists", func(t *testing.T) {
		db := newConversationWorkspaceTestDB(t)
		seedMessageWithMetadata(t, db, 100, `{"translations":"{\"en\":\"cached hello\"}"}`)
		history := &historyTranslatorStub{}
		store := &translationStoreStub{}
		handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(seedConversation(t, db)), nil, nil).
			WithHistoryTranslator(history).
			WithTranslationStore(store)

		for _, item := range listMessages(t, handler) {
			if item.Metadata["translation"] != "" {
				t.Fatalf("no preference must not annotate, got %+v", item.Metadata)
			}
		}
		if history.gotTexts != nil || store.calls != 0 {
			t.Fatalf("no preference must touch neither translator nor store, texts=%v calls=%d", history.gotTexts, store.calls)
		}
	})

	t.Run("preference read failure leaves history untouched", func(t *testing.T) {
		db := newConversationWorkspaceTestDB(t)
		history := &historyTranslatorStub{lang: "en", targetErr: errors.New("prefs down")}
		handler := NewConversationWorkspaceHandler(conversationdelivery.NewHandlerService(seedConversation(t, db)), nil, nil).
			WithHistoryTranslator(history)

		for _, item := range listMessages(t, handler) {
			if item.Metadata["translation"] != "" {
				t.Fatalf("preference failure must not annotate, got %+v", item.Metadata)
			}
		}
		if history.gotTexts != nil {
			t.Fatalf("preference failure must not reach the translator, texts=%v", history.gotTexts)
		}
	})
}
