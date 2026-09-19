package delivery

// 契约侧类型 re-export：字段与 application 全同，零转换；
// handler 只依赖本包。

import (
	macroapp "servify/apps/server/internal/modules/macro/application"
)

type (
	MacroCreateRequest = macroapp.MacroCreateRequest
	MacroUpdateRequest = macroapp.MacroUpdateRequest
)

var (
	ErrMacroNotFound  = macroapp.ErrMacroNotFound
	ErrMacroInactive  = macroapp.ErrMacroInactive
	ErrTicketNotFound = macroapp.ErrTicketNotFound
)
