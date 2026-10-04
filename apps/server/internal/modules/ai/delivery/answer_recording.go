package delivery

import (
	"context"
	"log"

	aiapp "servify/apps/server/internal/modules/ai/application"
	aidomain "servify/apps/server/internal/modules/ai/domain"
	aiinfra "servify/apps/server/internal/modules/ai/infra"
	"servify/apps/server/pkg/weknora"

	"gorm.io/gorm"
)

// V1.0 收敛 B3-1b（docs/v1-convergence-plan.md §5.3/§8.3）：AI 首答持久化。
// 记录路径经 AnswerStore 注入（REST 在 scopedAIHandlerService、WS 在
// scopedAIRuntimeService 装配点），失败只记日志不阻塞作答——落库是旁路
// 观测，不进入业务写路径。

// AnswerStore 首答持久化口。
type AnswerStore interface {
	RecordAnswer(ctx context.Context, record AnswerRecord) (uint, error)
}

// AnswerRecord 一次首答的落库载荷（Sources 为引用来源精简快照）。
type AnswerRecord struct {
	SessionID  string
	Query      string
	Answer     string
	Confidence float64
	Strategy   string
	Sources    []aidomain.SourceSnapshot
}

// NewGormAnswerStore 装配 db 版记录口（模块内组装 infra，调用方只依赖本包）。
func NewGormAnswerStore(db *gorm.DB) AnswerStore {
	if db == nil {
		return nil
	}
	return &gormAnswerStore{repo: aiinfra.NewGormAnswerRepository(db)}
}

type gormAnswerStore struct {
	repo *aiinfra.GormAnswerRepository
}

func (s *gormAnswerStore) RecordAnswer(ctx context.Context, record AnswerRecord) (uint, error) {
	sourcesJSON, err := aiapp.MarshalSources(record.Sources)
	if err != nil {
		return 0, err
	}
	answer := &aidomain.AIAnswer{
		SessionID:   record.SessionID,
		Query:       record.Query,
		Answer:      record.Answer,
		Confidence:  record.Confidence,
		Strategy:    record.Strategy,
		SourcesJSON: sourcesJSON,
	}
	if err := s.repo.Create(ctx, answer); err != nil {
		return 0, err
	}
	return answer.ID, nil
}

// RecordResponse 把一次作答结果写 AnswerStore，并把落库 ID 回填到响应
// （客户端凭 answer_id 调反馈端点）。store 为 nil 或失败均静默（旁路观测，
// 不进业务写路径）。
func RecordResponse(ctx context.Context, store AnswerStore, query, sessionID string, resp *AIResponse) {
	if store == nil || resp == nil {
		return
	}
	record := AnswerRecord{
		SessionID:  sessionID,
		Query:      query,
		Answer:     resp.Content,
		Confidence: resp.Confidence,
		Strategy:   resp.Strategy,
		Sources:    sourceSnapshots(resp.Sources),
	}
	id, err := store.RecordAnswer(ctx, record)
	if err != nil {
		log.Printf("[ai] answer recording skipped: %v", err)
		return
	}
	resp.AnswerID = id
}

// sourceSnapshots 引用来源 → 落库快照（不存 content 全文）。
func sourceSnapshots(sources []weknora.SearchResult) []aidomain.SourceSnapshot {
	if len(sources) == 0 {
		return nil
	}
	out := make([]aidomain.SourceSnapshot, 0, len(sources))
	for _, source := range sources {
		out = append(out, aidomain.SourceSnapshot{
			DocumentID: source.DocumentID,
			Title:      source.Title,
			Source:     source.Source,
			Score:      source.Score,
		})
	}
	return out
}

// RecordingStreamChan 包装流式事件通道：终末 Done 事件携带完整首答时落库
// 并回填 answer_id，其余事件原样透传（增量不被阻塞）。
func RecordingStreamChan(ctx context.Context, store AnswerStore, query, sessionID string, in <-chan AIStreamEvent) <-chan AIStreamEvent {
	if store == nil || in == nil {
		return in
	}
	out := make(chan AIStreamEvent, cap(in)+1)
	go func() {
		defer close(out)
		for event := range in {
			if event.Done && event.Final != nil {
				RecordResponse(ctx, store, query, sessionID, event.Final)
			}
			out <- event
		}
	}()
	return out
}
