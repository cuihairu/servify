package delivery

import (
	"context"
	"fmt"
	"strconv"
	"time"

	knowledgeapp "servify/apps/server/internal/modules/knowledge/application"
	knowledgedomain "servify/apps/server/internal/modules/knowledge/domain"
)

// 来源登记与索引任务的 adapter 面（B3-1a，docs/v1-convergence-plan.md
// §8.1/§8.2）：来源元数据登记 + 文档挂源 + 索引任务排队/执行/重试/按文档
// 列表。业务规则（类型枚举、引用守卫、版本关联）在 application 层。
func (a *HandlerServiceAdapter) ListSources(ctx context.Context, sourceType string) ([]knowledgedomain.KnowledgeSource, error) {
	sources, err := a.service.ListSources(ctx, knowledgeapp.ListSourcesFilter{Type: sourceType})
	if err != nil {
		return nil, err
	}
	out := make([]knowledgedomain.KnowledgeSource, 0, len(sources))
	for _, source := range sources {
		out = append(out, *sourceModelFromDomain(&source))
	}
	return out, nil
}

func (a *HandlerServiceAdapter) CreateSource(ctx context.Context, req *KnowledgeSourceCreateRequest) (*knowledgedomain.KnowledgeSource, error) {
	if req == nil {
		return nil, fmt.Errorf("request required")
	}
	source, err := a.service.CreateSource(ctx, knowledgeapp.CreateSourceRequest{
		Name:        req.Name,
		Type:        req.Type,
		Description: req.Description,
	})
	if err != nil {
		return nil, err
	}
	return sourceModelFromDomain(source), nil
}

func (a *HandlerServiceAdapter) DeleteSource(ctx context.Context, id uint) error {
	return a.service.DeleteSource(ctx, id)
}

func (a *HandlerServiceAdapter) ListIndexJobs(ctx context.Context, documentID string, limit int) ([]knowledgeapp.IndexJobDTO, error) {
	return a.service.ListIndexJobs(ctx, documentID, limit)
}

// IndexDocument 为文档排队并同步执行一次索引（重建索引入口）：任务落
// document_version 关联（执行时文档版本），失败状态可见可重试。
func (a *HandlerServiceAdapter) IndexDocument(ctx context.Context, documentID string) (*knowledgeapp.IndexJobResult, error) {
	if _, err := a.service.GetDocument(ctx, documentID); err != nil {
		return nil, err
	}
	jobID := fmt.Sprintf("idx-%s-%d", documentID, time.Now().UnixNano())
	if _, err := a.service.QueueIndexJob(ctx, knowledgeapp.QueueIndexJobRequest{JobID: jobID, DocumentID: documentID}); err != nil {
		return nil, err
	}
	return a.service.RunIndexJob(ctx, knowledgeapp.RunIndexJobRequest{JobID: jobID})
}

// RetryIndexJob 重跑既有任务（§8.2：失败重试）：done 任务重跑=按当前文档
// 版本重建索引，状态与版本在任务行上可见。
func (a *HandlerServiceAdapter) RetryIndexJob(ctx context.Context, jobID string) (*knowledgeapp.IndexJobResult, error) {
	return a.service.RunIndexJob(ctx, knowledgeapp.RunIndexJobRequest{JobID: jobID})
}

func sourceModelFromDomain(source *knowledgedomain.Source) *knowledgedomain.KnowledgeSource {
	if source == nil {
		return nil
	}
	return &knowledgedomain.KnowledgeSource{
		ID:          source.ID,
		Name:        source.Name,
		Type:        source.Type,
		Description: source.Description,
		CreatedAt:   source.CreatedAt,
		UpdatedAt:   source.UpdatedAt,
	}
}

// parseSourceID 解析来源 id（URL 路径参数 → uint，0/非法拒绝）。
func parseSourceID(raw string) (uint, error) {
	id, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("invalid source id")
	}
	return uint(id), nil
}
