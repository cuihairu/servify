package delivery

// V1.0 收敛 B3-1b：首答记录路径的单元行为——响应回填 answer_id、来源快照
// 精简（不落 content 全文）、流式终帧才落库、失败静默不阻塞。

import (
	"context"
	"strings"
	"testing"

	aidomain "servify/apps/server/internal/modules/ai/domain"
	"servify/apps/server/pkg/weknora"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newAnswerRecordingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&aidomain.AIAnswer{}, &aidomain.AnswerFeedback{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestRecordResponsePersistsAndStampsAnswerID(t *testing.T) {
	db := newAnswerRecordingDB(t)
	store := NewGormAnswerStore(db)
	resp := &AIResponse{
		Content:    "7 天无理由退款",
		Confidence: 0.9,
		Strategy:   "weknora",
		Sources: []weknora.SearchResult{
			{DocumentID: "d1", Title: "退款政策", Score: 0.91, Content: "不应落库的全文"},
		},
	}
	RecordResponse(context.Background(), store, "退款政策是什么", "conv-1", resp)
	if resp.AnswerID == 0 {
		t.Fatal("expected answer_id stamped on response")
	}

	var row aidomain.AIAnswer
	if err := db.First(&row, resp.AnswerID).Error; err != nil {
		t.Fatalf("load recorded answer: %v", err)
	}
	if row.Strategy != "weknora" || row.Confidence != 0.9 {
		t.Fatalf("row = %+v", row)
	}
	// 来源快照不落 content 全文。
	if row.SourcesJSON == "" || strings.Contains(row.SourcesJSON, "不应落库的全文") {
		t.Fatalf("sources snapshot leaked content: %q", row.SourcesJSON)
	}
	if !strings.Contains(row.SourcesJSON, "退款政策") {
		t.Fatalf("sources snapshot missing title: %q", row.SourcesJSON)
	}
}

func TestRecordResponseNilStoreIsSilent(t *testing.T) {
	resp := &AIResponse{Content: "x"}
	RecordResponse(context.Background(), nil, "q", "s", resp)
	if resp.AnswerID != 0 {
		t.Fatalf("nil store must not stamp id, got %d", resp.AnswerID)
	}
}

func TestRecordingStreamChanRecordsFinalOnly(t *testing.T) {
	db := newAnswerRecordingDB(t)
	store := NewGormAnswerStore(db)
	in := make(chan AIStreamEvent, 3)
	in <- AIStreamEvent{ContentDelta: "7 天"}
	in <- AIStreamEvent{ContentDelta: "无理由"}
	in <- AIStreamEvent{Done: true, Final: &AIResponse{Content: "7 天无理由", Confidence: 0.9, Strategy: "llm"}}
	close(in)

	out := RecordingStreamChan(context.Background(), store, "退款政策", "conv-9", in)
	var deltas int
	var final *AIResponse
	for event := range out {
		if event.ContentDelta != "" {
			deltas++
		}
		if event.Done {
			final = event.Final
		}
	}
	if deltas != 2 || final == nil || final.AnswerID == 0 {
		t.Fatalf("deltas=%d final=%+v（终帧必须带 answer_id）", deltas, final)
	}
	var row aidomain.AIAnswer
	if err := db.First(&row, final.AnswerID).Error; err != nil {
		t.Fatalf("load recorded answer: %v", err)
	}
	if row.SessionID != "conv-9" || row.Query != "退款政策" || row.SourcesJSON != "" {
		t.Fatalf("row = %+v", row)
	}
}
