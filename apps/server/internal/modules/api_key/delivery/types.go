package delivery

// 契约侧类型 re-export：字段与 application 全同，零转换；
// handler 只依赖本包。

import (
	apikeyapp "servify/apps/server/internal/modules/api_key/application"
)

type APIKeyCreateRequest = apikeyapp.APIKeyCreateRequest

var ErrAPIKeyNotFound = apikeyapp.ErrAPIKeyNotFound
