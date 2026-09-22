package servify.sdk.android.ui

import servify.sdk.android.PresentationStyle
import servify.sdk.android.model.ConversationMessage
import servify.sdk.android.model.SenderType
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/** ChatPanelState reducer 与面板形态选择（D8）的穷举。 */
class ChatPanelStateTest {

    private fun message(
        id: String,
        sender: SenderType = SenderType.Customer,
        content: String = "内容",
    ) = ConversationMessage(
        id = id,
        sessionId = "s",
        sender = sender,
        content = content,
        createdAt = 0L,
    )

    @Test
    fun onMessageAppendsNewIds() {
        val state = ChatPanelState(null)
        state.onMessage(message("a"))
        state.onMessage(message("b"))
        assertEquals(2, state.messageCount())
        val bubbles = state.items().map { it as ChatListItem.Bubble }
        assertEquals(listOf("a", "b"), bubbles.map { it.id })
    }

    @Test
    fun onMessageOverwritesSameIdInPlace() {
        val state = ChatPanelState(null)
        state.onMessage(message("a"))
        state.onMessage(message("b"))
        state.onMessage(message("a", content = "a-最新"))
        assertEquals(2, state.messageCount())
        val bubbles = state.items().map { it as ChatListItem.Bubble }
        assertEquals(listOf("a", "b"), bubbles.map { it.id })
        assertEquals("a-最新", bubbles[0].content)
    }

    @Test
    fun welcomeTextFeedsFirstItem() {
        val state = ChatPanelState("您好")
        assertTrue(state.items().first() is ChatListItem.Welcome)
    }

    @Test
    fun nullWelcomeTextYieldsNoWelcomeItem() {
        val state = ChatPanelState(null)
        assertTrue(state.items().isEmpty())
    }

    @Test
    fun resolveStyleConfiguredFullscreenWins() {
        assertEquals(
            PanelStyle.Fullscreen,
            resolvePanelStyle(PresentationStyle.Fullscreen, 1000),
        )
    }

    @Test
    fun resolveStyleSmallScreenUpgradesDrawerToFullscreen() {
        assertEquals(
            PanelStyle.Fullscreen,
            resolvePanelStyle(PresentationStyle.Drawer, SMALL_SCREEN_HEIGHT_DP - 1),
        )
        assertEquals(
            PanelStyle.Drawer,
            resolvePanelStyle(PresentationStyle.Drawer, SMALL_SCREEN_HEIGHT_DP),
        )
    }

    @Test
    fun customerBubblesRightAlignedOthersLeft() {
        assertTrue(SenderType.Customer.isRightAligned())
        assertFalse(SenderType.Agent.isRightAligned())
        assertFalse(SenderType.System.isRightAligned())
    }
}
