package delivery

// types.go re-export 应用层类型：handler 只 import delivery 一包。

import (
	appintegrationapp "servify/apps/server/internal/modules/app_integration/application"
)

type (
	// AppIntegration 返回给 API 的集成结构
	AppIntegration = appintegrationapp.AppIntegration
	// AppIntegrationListRequest 查询条件
	AppIntegrationListRequest = appintegrationapp.AppIntegrationListRequest
	// AppIntegrationCreateRequest 创建请求
	AppIntegrationCreateRequest = appintegrationapp.AppIntegrationCreateRequest
	// AppIntegrationUpdateRequest 更新请求
	AppIntegrationUpdateRequest = appintegrationapp.AppIntegrationUpdateRequest
)

// ErrIntegrationNotFound 集成不存在（消息保持原字符串）。
var ErrIntegrationNotFound = appintegrationapp.ErrIntegrationNotFound
