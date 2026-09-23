package servify.sdk.android

import servify.sdk.android.model.ConversationMessage
import servify.sdk.android.model.SenderType

/**
 * 工单 AI 摘要预填（M3 会话页工单入口）：从会话历史组装坐席可读的上下文摘录，
 * 随创建请求走 ai_summary 字段——坐席侧不读对话即可了解来龙去脉。
 *
 * 发言方标注口径（与门面消息来源一一对应）：Customer→[访客]；isAiResponse→[AI]
 * （门面里 AI 回答是 System sender + isAiResponse 标志）；Agent→[客服]；其余
 * System 行（流中断/离线提示等 SDK 自造提示）跳过。纯函数，无 IO。
 */
internal object TicketSummary {

    /** 摘录最近 [maxTurns] 条；无可用对话返回 null（请求体不带 ai_summary 键）。 */
    fun build(messages: List<ConversationMessage>, maxTurns: Int = 10): String? {
        val lines = messages
            .filter { it.sender == SenderType.Customer || it.sender == SenderType.Agent || it.isAiResponse }
            .takeLast(maxTurns)
            .map { msg -> "[${labelOf(msg)}] ${msg.content}" }
        return lines.takeIf { it.isNotEmpty() }?.joinToString("\n")
    }

    private fun labelOf(msg: ConversationMessage): String =
        when {
            msg.sender == SenderType.Customer -> "访客"
            msg.isAiResponse -> "AI"
            else -> "客服"
        }
}
