package servify.sdk.android.ui

import servify.sdk.android.model.ConversationMessage
import servify.sdk.android.model.SenderType
import servify.sdk.android.protocol.KnowledgeSource
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertIs
import kotlin.test.assertTrue

/** toChatListItems 的渲染规则穷举（D5/D8：欢迎语、流式合并、终帧替换、来源排序、置信门）。 */
class ChatUiStateTest {

    private fun message(
        id: String,
        sender: SenderType = SenderType.Customer,
        content: String = "内容",
        isAi: Boolean = false,
        isStreaming: Boolean = false,
        nextAction: String? = null,
        sources: List<KnowledgeSource> = emptyList(),
    ) = ConversationMessage(
        id = id,
        sessionId = "s",
        sender = sender,
        content = content,
        createdAt = 0L,
        isAiResponse = isAi,
        isStreaming = isStreaming,
        sources = sources,
        confidence = if (isAi) 0.9 else null,
        nextAction = nextAction,
    )

    @Test
    fun emptyMessagesYieldOnlyWelcomeWhenPresent() {
        val items = toChatListItems(emptyList(), "您好")
        assertEquals(listOf<ChatListItem>(ChatListItem.Welcome("您好")), items)
    }

    @Test
    fun blankWelcomeTextOmitsWelcomeItem() {
        val items = toChatListItems(emptyList(), "")
        assertTrue(items.isEmpty())
        val items2 = toChatListItems(emptyList(), null)
        assertTrue(items2.isEmpty())
    }

    @Test
    fun streamingIntermediateStatesMergeByIdKeepingLatest() {
        val items = toChatListItems(
            listOf(
                message("ws-1", SenderType.Customer, "问"),
                message("m2", SenderType.System, "根据", isAi = true, isStreaming = true),
                message("m2", SenderType.System, "根据退货", isAi = true, isStreaming = true),
            ),
            null,
        )
        assertEquals(2, items.size)
        val bubble = assertIs<ChatListItem.Bubble>(items[1])
        assertEquals("根据退货", bubble.content)
        assertTrue(bubble.isStreaming)
    }

    @Test
    fun finalFrameReplacesStreamingBubbleWithSameId() {
        val items = toChatListItems(
            listOf(
                message("m2", SenderType.System, "根据退货", isAi = true, isStreaming = true),
                message("m2", SenderType.System, "根据退货政策。", isAi = true),
            ),
            null,
        )
        assertEquals(1, items.size)
        val bubble = assertIs<ChatListItem.Bubble>(items[0])
        assertEquals("根据退货政策。", bubble.content)
        assertFalse(bubble.isStreaming)
        assertTrue(bubble.isAiResponse)
    }

    @Test
    fun sourcesSortByScoreDescending() {
        val items = toChatListItems(
            listOf(
                message(
                    "m2", SenderType.System, "答", isAi = true,
                    sources = listOf(
                        KnowledgeSource("low", "低分文档", null, 0.3),
                        KnowledgeSource("high", "高分文档", null, 0.95),
                        KnowledgeSource("mid", "中文档", null, 0.6),
                    ),
                ),
            ),
            null,
        )
        val bubble = assertIs<ChatListItem.Bubble>(items[0])
        assertEquals(listOf("high", "mid", "low"), bubble.sources.map { it.documentId })
    }

    @Test
    fun handoffSuggestionCarriesEmphasisFlag() {
        val items = toChatListItems(
            listOf(message("m2", SenderType.System, "答", isAi = true, nextAction = "handoff")),
            null,
        )
        val bubble = assertIs<ChatListItem.Bubble>(items[0])
        assertTrue(bubble.suggestsHandoff)
    }

    @Test
    fun nonHandoffNextActionIsNotEmphasized() {
        val items = toChatListItems(
            listOf(message("m2", SenderType.System, "答", isAi = true, nextAction = "continue")),
            null,
        )
        val bubble = assertIs<ChatListItem.Bubble>(items[0])
        assertFalse(bubble.suggestsHandoff)
    }

    @Test
    fun messageOrderPreservedAfterMerge() {
        val items = toChatListItems(
            listOf(
                message("a", SenderType.Customer, "1"),
                message("b", SenderType.System, "2", isAi = true),
                message("c", SenderType.Agent, "3"),
                message("a", SenderType.Customer, "1-edit"), // 同 id 后到覆盖，位置保持
            ),
            null,
        )
        val bubbles = items.map { assertIs<ChatListItem.Bubble>(it) }
        assertEquals(listOf("a", "b", "c"), bubbles.map { it.id })
        assertEquals("1-edit", bubbles[0].content)
        assertEquals(SenderType.Agent, bubbles[2].sender)
    }
}
