package servify.sdk.android

import servify.sdk.android.model.ConversationMessage
import servify.sdk.android.model.SenderType
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

/** 工单 AI 摘要预填纯逻辑（M3 刀 3a）：发言方标注、System 提示行跳过、最近 N 条截断。 */
class TicketSummaryTest {

    private fun msg(
        id: String,
        sender: SenderType,
        content: String,
        isAiResponse: Boolean = false,
    ) = ConversationMessage(
        id = id,
        sessionId = "s",
        sender = sender,
        content = content,
        createdAt = 0L,
        isAiResponse = isAiResponse,
    )

    @Test
    fun labelsVisitorAiAndAgentTurns() {
        val summary = TicketSummary.build(
            listOf(
                msg("1", SenderType.Customer, "打不开页面"),
                // 门面里 AI 回答是 System sender + isAiResponse 标志
                msg("2", SenderType.System, "已为您排查缓存问题", isAiResponse = true),
                msg("3", SenderType.Agent, "请提供订单号"),
            ),
        )
        assertEquals(
            "[访客] 打不开页面\n[AI] 已为您排查缓存问题\n[客服] 请提供订单号",
            summary,
        )
    }

    @Test
    fun skipsSystemHintLines() {
        // 流中断/离线提示（System 无 AI 标志）不进摘要。
        val summary = TicketSummary.build(
            listOf(
                msg("1", SenderType.Customer, "咨询"),
                msg("2", SenderType.System, "回答中断，请重试"),
                msg("3", SenderType.System, "客服当前不在线"),
            ),
        )
        assertEquals("[访客] 咨询", summary)
    }

    @Test
    fun returnsNullForNoUsableConversation() {
        assertNull(TicketSummary.build(emptyList()))
        assertNull(TicketSummary.build(listOf(msg("1", SenderType.System, "回答中断，请重试"))))
    }

    @Test
    fun keepsOnlyLastMaxTurns() {
        val messages = (1..15).map { msg("$it", SenderType.Customer, "消息$it") }
        val summary = TicketSummary.build(messages, maxTurns = 3)!!
        assertEquals("[访客] 消息13\n[访客] 消息14\n[访客] 消息15", summary)
    }
}
