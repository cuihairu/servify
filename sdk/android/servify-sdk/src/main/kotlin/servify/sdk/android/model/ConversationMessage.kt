package servify.sdk.android.model

import servify.sdk.android.protocol.KnowledgeSource

/** 消息来源方（D5 规范形 sender_type 三值；V1 不产生其他值）。 */
enum class SenderType { Customer, Agent, System }

/**
 * 会话消息（设计文档 D5 规范形，跨端一致）：
 * id 缺省回退 "ws-" + 本地序号（对齐 core normalizeMessage）；sources/confidence
 * 由 ai-response 帧透出，供 UI 引用展示与置信门提示（M1 验收范围）。
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
)
