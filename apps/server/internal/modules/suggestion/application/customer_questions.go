package application

import (
	"context"
	"sort"
	"strconv"
	"strings"

	suggestioncontract "servify/apps/server/internal/modules/suggestion/contract"
)

const customerQuestionDefaultLimit = 8

// customerQuestionLimit 归一化客户侧推荐数量：默认 8，上限 20。
func customerQuestionLimit(reqLimit int) int {
	limit := customerQuestionDefaultLimit
	if reqLimit > 0 {
		limit = reqLimit
	}
	if limit > 20 {
		limit = 20
	}
	return limit
}

// InitialQuestions 首屏热门推荐（P2-0 RQ-1）：客户未输入前的可点击问题。
// 推荐策略为"规则 + 知识库"——公开知识文档标题按 updated_at 新近优先
// （无埋点数据前的稳态排序），不依赖外部 AI provider，可离线验收。
func (s *Service) InitialQuestions(ctx context.Context, req *suggestioncontract.InitialQuestionsRequest) (*suggestioncontract.InitialQuestionsResponse, error) {
	limit := customerQuestionDefaultLimit
	if req != nil {
		limit = customerQuestionLimit(req.Limit)
	}

	rows, err := s.repo.FindPublicKnowledgeDocs(ctx, limit)
	if err != nil {
		return nil, err
	}

	questions := make([]suggestioncontract.RecommendedQuestion, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for i, r := range rows {
		question := strings.TrimSpace(r.Title)
		if question == "" {
			continue
		}
		if _, ok := seen[question]; ok {
			continue
		}
		seen[question] = struct{}{}
		questions = append(questions, suggestioncontract.RecommendedQuestion{
			Question: question,
			Source:   "knowledge_doc",
			SourceID: strconv.FormatUint(uint64(r.ID), 10),
			Category: r.Category,
			// 新近优先：位置即名次，分数随位置线性递减，前端可直接按序展示
			Score: float64(len(rows)-i) / float64(len(rows)),
		})
	}
	return &suggestioncontract.InitialQuestionsResponse{
		Questions: questions,
		Meta: map[string]interface{}{
			"strategy":   "public_knowledge_recency",
			"candidates": len(rows),
		},
	}, nil
}

// NextQuestions 会话内上下文联想（P2-0 RQ-2）：以客户最近一条消息为
// query，复用 Suggest 的 token 提取 / 打分内核，但客户侧只吐公开知识
// 文档——相似工单标题可能携带其他客户的隐私信息，绝不进客户侧响应；
// intent 分类仅作为 meta 信号下发给前端做展示分组。
func (s *Service) NextQuestions(ctx context.Context, req *suggestioncontract.NextQuestionsRequest) (*suggestioncontract.NextQuestionsResponse, error) {
	query := ""
	limit := customerQuestionDefaultLimit
	if req != nil {
		query = strings.TrimSpace(req.Query)
		limit = customerQuestionLimit(req.Limit)
	}

	tokens := ExtractTokens(query)
	rows, err := s.repo.FindPublicKnowledgeDocCandidates(ctx, tokens)
	if err != nil {
		return nil, err
	}

	intent := ClassifyIntent(query)
	questions := make([]suggestioncontract.RecommendedQuestion, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		score := ScoreText(query, r.Title+" "+r.Content+" "+r.Tags)
		if score <= 0 {
			continue
		}
		question := strings.TrimSpace(r.Title)
		if question == "" {
			continue
		}
		if _, ok := seen[question]; ok {
			continue
		}
		seen[question] = struct{}{}
		questions = append(questions, suggestioncontract.RecommendedQuestion{
			Question: question,
			Source:   "knowledge_doc",
			SourceID: strconv.FormatUint(uint64(r.ID), 10),
			Category: r.Category,
			Score:    score,
		})
	}

	sort.Slice(questions, func(i, j int) bool {
		if questions[i].Score == questions[j].Score {
			return questions[i].SourceID < questions[j].SourceID
		}
		return questions[i].Score > questions[j].Score
	})
	if len(questions) > limit {
		questions = questions[:limit]
	}
	return &suggestioncontract.NextQuestionsResponse{
		Query:     query,
		Questions: questions,
		Meta: map[string]interface{}{
			"strategy":          "public_knowledge_scored",
			"intent":            intent.Label,
			"intent_confidence": intent.Confidence,
			"tokens":            tokens,
			"doc_candidates":    len(rows),
		},
	}, nil
}
