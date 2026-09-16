package delivery

import (
	"context"

	suggestioncontract "servify/apps/server/internal/modules/suggestion/contract"
)

type HandlerService interface {
	Suggest(ctx context.Context, req *suggestioncontract.SuggestionRequest) (*suggestioncontract.SuggestionResponse, error)
	// InitialQuestions 客户侧首屏推荐问题（P2-0 RQ-1，公开路由消费）。
	InitialQuestions(ctx context.Context, req *suggestioncontract.InitialQuestionsRequest) (*suggestioncontract.InitialQuestionsResponse, error)
	// NextQuestions 客户侧上下文联想问题（P2-0 RQ-2，公开路由消费）。
	NextQuestions(ctx context.Context, req *suggestioncontract.NextQuestionsRequest) (*suggestioncontract.NextQuestionsResponse, error)
}
