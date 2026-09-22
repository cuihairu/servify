package application

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

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
	sessionID := ""
	if req != nil {
		limit = customerQuestionLimit(req.Limit)
		sessionID = strings.TrimSpace(req.SessionID)
	}

	rows, err := s.repo.FindPublicKnowledgeDocs(ctx, limit)
	if err != nil {
		return nil, err
	}

	questions := make([]suggestioncontract.RecommendedQuestion, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	questionTexts := make([]string, 0, len(rows))
	for i, r := range rows {
		question := strings.TrimSpace(r.Title)
		if question == "" {
			continue
		}
		if _, ok := seen[question]; ok {
			continue
		}
		seen[question] = struct{}{}
		questionTexts = append(questionTexts, question)
		questions = append(questions, suggestioncontract.RecommendedQuestion{
			Question: question,
			Source:   "knowledge_doc",
			SourceID: strconv.FormatUint(uint64(r.ID), 10),
			Category: r.Category,
			// 新近优先：位置即名次，分数随位置线性递减，前端可直接按序展示
			Score: float64(len(rows)-i) / float64(len(rows)),
		})
	}
	// 服务端归因：曝光即落库（P2-0 RQ-5），失败不阻断响应
	s.recordExposure(ExposureRecord{
		SessionID: sessionID,
		Kind:      "initial",
		Strategy:  "public_knowledge_recency",
		Questions: questionTexts,
	})
	return &suggestioncontract.InitialQuestionsResponse{
		Questions: questions,
		Meta: map[string]interface{}{
			"strategy":   "public_knowledge_recency",
			"candidates": len(rows),
		},
	}, nil
}

// recordExposure 落一行曝光（P2-0 RQ-5 服务端归因口径：曝光 = 接口成功
// 返回）。曝光统计失败不阻断推荐响应，契约式忽略错误。
func (s *Service) recordExposure(rec ExposureRecord) {
	_ = s.repo.RecordExposure(context.Background(), rec)
}

// NormalizeQuestionText 规范化问题文案用于转化匹配：空白折叠 + 小写，
// 客户端"点击即发送"链路中文案原样透传，规范化只为抵御零星空白/大小写漂移。
func NormalizeQuestionText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// MatchSuggestionConversion 转化归因（P2-0 RQ-5）：客户消息到达时，与该
// session 最近一次未转化曝光的问题列表规范化匹配；命中即标记转化。定义
// 口径为"点击后发送"——嵌入方若把点击改为仅填入输入框则漏归因（已知边界）。
func (s *Service) MatchSuggestionConversion(ctx context.Context, sessionID, content string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || strings.TrimSpace(content) == "" {
		return nil
	}
	open, err := s.repo.FindLatestOpenExposure(ctx, sessionID)
	if err != nil || open == nil {
		return err
	}
	normalized := NormalizeQuestionText(content)
	for _, q := range open.Questions {
		if NormalizeQuestionText(q) == normalized {
			return s.repo.MarkExposureConverted(ctx, open.ID, q, time.Now())
		}
	}
	return nil
}

// NextQuestions 会话内上下文联想（P2-0 RQ-2）：以客户最近一条消息为
// query，复用 Suggest 的 token 提取 / 打分内核，但客户侧只吐公开知识
// 文档——相似工单标题可能携带其他客户的隐私信息，绝不进客户侧响应；
// intent 分类仅作为 meta 信号下发给前端做展示分组。
func (s *Service) NextQuestions(ctx context.Context, req *suggestioncontract.NextQuestionsRequest) (*suggestioncontract.NextQuestionsResponse, error) {
	query := ""
	limit := customerQuestionDefaultLimit
	sessionID := ""
	if req != nil {
		query = strings.TrimSpace(req.Query)
		limit = customerQuestionLimit(req.Limit)
		sessionID = strings.TrimSpace(req.SessionID)
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
	// 服务端归因：曝光即落库（P2-0 RQ-5），只记实际返回的问题列表，
	// 失败不阻断响应
	exposed := make([]string, 0, len(questions))
	for _, q := range questions {
		exposed = append(exposed, q.Question)
	}
	s.recordExposure(ExposureRecord{
		SessionID: sessionID,
		Kind:      "next",
		Strategy:  "public_knowledge_scored",
		Questions: exposed,
	})
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
