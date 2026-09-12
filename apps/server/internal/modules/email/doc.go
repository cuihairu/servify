// Package email 实现单邮箱 Email 渠道：IMAP 轮询收件 + SMTP 出站。
//
// 零表零迁移：入站邮件按发件人地址归并为 conversation 会话（conversation_bridge.go），
// 去重依赖内存中的 UID 游标与 Message-ID 环形集合——重启窗口内可能有有界重复 ingest，
// 这是已知限制（conversation 消息 Metadata 不落库，无法持久化游标）。
//
// 出站 Send 已实现（SMTP）但本期无生产触发点：坐席回复转 email 与 CSAT 邮件留待后续版本
// （无按渠道出站分派层）。
//
// 启停完全由 config email.enabled 控制，不启用时不构建任何组件、不注册 worker。
package email
