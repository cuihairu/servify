package servify.sdk.android.ui

import servify.sdk.android.PresentationStyle
import servify.sdk.android.model.ConversationMessage
import servify.sdk.android.model.SenderType

/**
 * 会话面板的状态 holder：把门面 `events.messages` 的事件流收敛为累积列表，
 * 经 [toChatListItems] 映射为渲染项。普通类（非 Compose 状态）——
 * 累积/合并规则可脱离 Compose 单测；Compose 侧经 produceState 驱动重组。
 */
class ChatPanelState(private val welcomeText: String?) {

    private val accumulated = mutableListOf<ConversationMessage>()

    /** 消费一条门面事件：同 id 覆盖（流式中间态 → 终帧替换），否则追加。 */
    fun onMessage(message: ConversationMessage) {
        val index = accumulated.indexOfLast { it.id == message.id }
        if (index >= 0) {
            accumulated[index] = message
        } else {
            accumulated += message
        }
    }

    /** 当前渲染项（每次重组时重算——列表规模 = 单次会话消息数，非热点路径）。 */
    fun items(): List<ChatListItem> = toChatListItems(accumulated, welcomeText)

    fun messageCount(): Int = accumulated.size
}

/** 面板形态（D8：抽屉默认；小屏（高度 < 600dp）抽屉自动升级全屏）。 */
enum class PanelStyle { Drawer, Fullscreen }

fun resolvePanelStyle(presentationStyle: PresentationStyle, screenHeightDp: Int): PanelStyle =
    when {
        presentationStyle == PresentationStyle.Fullscreen -> PanelStyle.Fullscreen
        screenHeightDp < SMALL_SCREEN_HEIGHT_DP -> PanelStyle.Fullscreen
        else -> PanelStyle.Drawer
    }

const val SMALL_SCREEN_HEIGHT_DP = 600

/** 气泡对齐侧（D8：customer 右，agent/AI/system 提示左）。 */
fun SenderType.isRightAligned(): Boolean = this == SenderType.Customer
