import Foundation

/// 工单 AI 摘要预填（M3 会话页工单入口；Kotlin 镜像：TicketSummary.kt）：从会话历史
/// 组装坐席可读的上下文摘录，随创建请求走 ai_summary 字段——坐席侧不读对话即可了解
/// 来龙去脉。
///
/// 发言方标注口径（与门面消息来源一一对应）：customer→[访客]；isAiResponse→[AI]
/// （门面里 AI 回答是 system sender + isAiResponse 标志）；agent→[客服]；其余 system 行
/// （流中断/离线提示等 SDK 自造提示）跳过。纯函数，无 IO。
enum TicketSummary {

    /// 摘录最近 `maxTurns` 条；无可用对话返回 nil（请求体不带 ai_summary 键）。
    static func build(messages: [ConversationMessage], maxTurns: Int = 10) -> String? {
        let lines = messages
            .filter { $0.sender == .customer || $0.sender == .agent || $0.isAiResponse }
            .suffix(maxTurns)
            .map { msg -> String in "[\(labelOf(msg))] \(msg.content)" }
        guard !lines.isEmpty else { return nil }
        return lines.joined(separator: "\n")
    }

    private static func labelOf(_ msg: ConversationMessage) -> String {
        if msg.sender == .customer { return "访客" }
        if msg.isAiResponse { return "AI" }
        return "客服"
    }
}
