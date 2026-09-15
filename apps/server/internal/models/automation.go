package models

import automationdomain "servify/apps/server/internal/modules/automation/domain"

// AutomationTrigger 自动化触发器定义。定义已迁至 modules/automation/domain，
// 此处保留类型别名供 legacy 引用方使用。
type AutomationTrigger = automationdomain.AutomationTrigger

// AutomationRun 执行记录用于审计。定义已迁至 modules/automation/domain，
// 此处保留类型别名供 legacy 引用方使用。
type AutomationRun = automationdomain.AutomationRun

// AutomationTimer 是 delay 动作产生的到期执行单。定义已迁至 modules/automation/domain，
// 此处保留类型别名供 legacy 引用方使用。
type AutomationTimer = automationdomain.AutomationTimer
