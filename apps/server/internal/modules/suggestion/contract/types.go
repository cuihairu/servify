package contract

import "time"

type IntentSuggestion struct {
	Label      string   `json:"label"`
	Confidence float64  `json:"confidence"`
	Matches    []string `json:"matches,omitempty"`
}

type TicketSuggestion struct {
	ID        uint      `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Category  string    `json:"category"`
	Priority  string    `json:"priority"`
	CreatedAt time.Time `json:"created_at"`
	Score     float64   `json:"score"`
}

type KnowledgeDocSuggestion struct {
	ID       uint    `json:"id"`
	Title    string  `json:"title"`
	Category string  `json:"category"`
	Tags     string  `json:"tags"`
	Score    float64 `json:"score"`
}

type SuggestionResponse struct {
	Query          string                   `json:"query"`
	Intent         IntentSuggestion         `json:"intent"`
	SimilarTickets []TicketSuggestion       `json:"similar_tickets"`
	KnowledgeDocs  []KnowledgeDocSuggestion `json:"knowledge_docs"`
	Meta           map[string]interface{}   `json:"meta,omitempty"`
}

type SuggestionRequest struct {
	Query              string `json:"query" binding:"required"`
	TicketLimit        int    `json:"ticket_limit"`
	KnowledgeDocLimit  int    `json:"knowledge_doc_limit"`
	CandidateTicketMax int    `json:"candidate_ticket_max"`
}

// RecommendedQuestion 客户侧推荐问题（P2-0）：首屏热门或会话内上下文联想，
// question 即可点击直接发起提问的文案；source 标明来源（knowledge_doc /
// intent），客户侧只吐公开知识文档，绝不外发工单标题（隐私边界）。
type RecommendedQuestion struct {
	Question string  `json:"question"`
	Source   string  `json:"source"`
	SourceID string  `json:"source_id,omitempty"`
	Category string  `json:"category,omitempty"`
	Score    float64 `json:"score"`
}

// InitialQuestionsRequest 首屏推荐问题请求（客户未输入任何内容）。
// session_id 为客户侧会话标识（如 demo widget 的 WS sessionId），仅用于
// 曝光/转化归因串联（P2-0 RQ-5），不参与检索。
type InitialQuestionsRequest struct {
	Limit     int    `json:"limit" form:"limit"`
	SessionID string `json:"session_id,omitempty" form:"session_id"`
}

// InitialQuestionsResponse 首屏推荐问题响应。
type InitialQuestionsResponse struct {
	Questions []RecommendedQuestion  `json:"questions"`
	Meta      map[string]interface{} `json:"meta,omitempty"`
}

// NextQuestionsRequest 会话内上下文联想请求：query 为客户最近一条消息
// （或会话摘要），session_id 仅用于日志串联，不参与检索。
type NextQuestionsRequest struct {
	Query     string `json:"query" form:"query" binding:"required"`
	SessionID string `json:"session_id,omitempty" form:"session_id"`
	Limit     int    `json:"limit" form:"limit"`
}

// NextQuestionsResponse 上下文联想问题响应。
type NextQuestionsResponse struct {
	Query     string                 `json:"query"`
	Questions []RecommendedQuestion  `json:"questions"`
	Meta      map[string]interface{} `json:"meta,omitempty"`
}

// ExposureKindSummary 曝光/转化聚合的单 kind 行。
type ExposureKindSummary struct {
	Kind               string `json:"kind"`
	TotalExposures     int64  `json:"total_exposures"`
	ConvertedExposures int64  `json:"converted_exposures"`
}

// ExposureSummaryResponse 曝光/转化聚合响应（P2-0 RQ-5 管理面最小口径）。
type ExposureSummaryResponse struct {
	TotalExposures     int64                 `json:"total_exposures"`
	ConvertedExposures int64                 `json:"converted_exposures"`
	ByKind             []ExposureKindSummary `json:"by_kind"`
}
