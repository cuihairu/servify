package servify.sdk.android.ui

import servify.sdk.android.model.ConversationMessage
import servify.sdk.android.model.SenderType

/**
 * 会话页渲染项（D8 消息渲染）：从消息累积列表到 UI 列表项的纯函数映射——
 * 状态驱动渲染的关键是把门面事件流的语义（流式中间态、终帧替换、来源排序、
 * 置信门建议）收敛在这里，Compose 组件只做无脑渲染。纯函数 => 单测穷举。
 */
sealed class ChatListItem {

    /** 会话页首条欢迎提示（branding.welcomeText）。 */
    data class Welcome(val text: String) : ChatListItem()

    /**
     * 消息气泡。customer 右对齐，agent/AI 左对齐且 AI 带标识；
     * [isStreaming] 渲染拼接中状态；[sources] 已按得分降序；
     * [suggestsHandoff] 驱动"转人工"按钮强调态（D8 置信门提示）。
     */
    data class Bubble(
        val id: String,
        val sender: SenderType,
        val content: String,
        val isAiResponse: Boolean,
        val isStreaming: Boolean,
        val suggestsHandoff: Boolean,
        val sources: List<SourceItem>,
    ) : ChatListItem()
}

/** 引用来源项（文档标题 + 得分，D8"可展开的来源列表"；score 缺省=协议零值省略）。 */
data class SourceItem(val documentId: String, val title: String, val score: Double?)

/**
 * 消息累积列表 → 渲染项列表。
 *
 * 规则（D5/D8）：
 * - 首条 [ChatListItem.Welcome]（welcomeText 为空则省略）；
 * - 流式气泡按 id 合并只留最新（门面以同 id 递增 content 发射增量中间态，终帧复用
 *   同 id 且 isStreaming=false 整体替换——即到即拼 + 幂等收口，不静默清空）；
 * - sources 按得分降序（文档标题 + 得分排序，D8）。
 */
fun toChatListItems(messages: List<ConversationMessage>, welcomeText: String?): List<ChatListItem> {
    val items = mutableListOf<ChatListItem>()
    if (!welcomeText.isNullOrEmpty()) {
        items += ChatListItem.Welcome(welcomeText)
    }
    val merged = LinkedHashMap<String, ConversationMessage>()
    for (message in messages) {
        merged[message.id] = message // 同 id 后到覆盖先到（流式中间态 → 终帧替换）
    }
    for (message in merged.values) {
        items += ChatListItem.Bubble(
            id = message.id,
            sender = message.sender,
            content = message.content,
            isAiResponse = message.isAiResponse,
            isStreaming = message.isStreaming,
            suggestsHandoff = message.suggestsHandoff,
            sources = message.sources
                .sortedWith(compareByDescending { it.score ?: Double.NEGATIVE_INFINITY })
                .map { SourceItem(it.documentId, it.title, it.score) },
        )
    }
    return items
}
