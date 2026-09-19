// Package delivery 对 handler 暴露的请求/错误类型（re-export application 层定义）。
package delivery

import (
	customfieldapp "servify/apps/server/internal/modules/custom_field/application"
)

type CustomFieldCreateRequest = customfieldapp.CustomFieldCreateRequest

type CustomFieldUpdateRequest = customfieldapp.CustomFieldUpdateRequest

var ErrCustomFieldNotFound = customfieldapp.ErrCustomFieldNotFound
