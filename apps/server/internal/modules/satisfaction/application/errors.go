package application

import "errors"

var (
	// ErrSurveyNotFound 表示调查不存在或 token 无效
	ErrSurveyNotFound = errors.New("survey not found")
	// ErrSurveyExpired 表示调查已过期
	ErrSurveyExpired = errors.New("survey expired")
	// ErrSurveyCompleted 表示调查已完成
	ErrSurveyCompleted = errors.New("survey already completed")
	// ErrSatisfactionNotFound 表示评价不存在
	ErrSatisfactionNotFound = errors.New("satisfaction not found")
	// ErrTicketNotFound 表示工单不存在或不在当前 scope
	ErrTicketNotFound = errors.New("ticket not found")
	// ErrCustomerNotFound 表示客户不存在或不在当前 scope
	ErrCustomerNotFound = errors.New("customer not found")
	// ErrAgentNotFound 表示客服不存在或不在当前 scope
	ErrAgentNotFound = errors.New("agent not found")
)
