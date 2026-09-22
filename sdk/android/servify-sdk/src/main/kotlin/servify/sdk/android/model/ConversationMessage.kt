package servify.sdk.android.model

import servify.sdk.android.protocol.KnowledgeSource

/** 消息来源方（D5 规范形 sender_type 三值；V1 不产生其他值）。 */
enum class SenderType { Customer, Agent, System }

/**
 * 会话消息（设计文档 D5 规范形，跨端一致）：
 * id 缺省回退 "ws-" + 本地序号（对齐 core normalizeMessage）；sources/confidence/nextAction
 * 是 ai-response 帧编排附加输出（D5 metadata 的类型化透出）——sources/confidence 供引用
 * 展示，nextAction=="handoff" 驱动"转人工"按钮强调态（D8 置信门提示）；其余附加输出
 * （strategy/handoff_reason）无 V1 消费面，按死代码纪律不进。
 */
data class ConversationMessage(
    val id: String,
    val sessionId: String,
    val sender: SenderType,
    val content: String,
    val createdAt: Long,
    val isAiResponse: Boolean = false,
    /** true = 流式增量中的气泡（UI 据此渲染拼接中状态）；终帧到达时翻 false。 */
    val isStreaming: Boolean = false,
    val sources: List<KnowledgeSource> = emptyList(),
    val confidence: Double? = null,
    val nextAction: String? = null,
) {
    /** 置信门建议转人工（D8：渲染"转人工"按钮强调态）。 */
    val suggestsHandoff: Boolean get() = nextAction == "handoff"
}
