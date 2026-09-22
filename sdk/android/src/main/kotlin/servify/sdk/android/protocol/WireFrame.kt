package servify.sdk.android.protocol

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.long
import kotlinx.serialization.json.longOrNull

/**
 * 服务端下行帧的协议语义类型（契约唯一事实源：sdk/PROTOCOL.md）。
 *
 * 未知类型归 [WireFrame.Unknown]——服务端对未知上行"警告后丢弃"，客户端对未知下行
 * 同样忽略而非报错（PROTOCOL.md §3/§6.4）；core 声明过但服务端零发射的死分支帧
 * （session_update/agent_status/typing/message/error/system，PROTOCOL.md §5）自然落进
 * Unknown，移动端契约不含它们。
 */
sealed interface WireFrame {
    val sessionId: String?
    val envelopeTimestamp: String?

    /** text-message 回显：客户自己消息经服务端广播回来（PROTOCOL.md §4.1，发送成功判据）。 */
    data class VisitorEcho(
        override val sessionId: String?,
        override val envelopeTimestamp: String?,
        val content: String,
    ) : WireFrame

    /** agent-message：坐席发言 {content, sender}。 */
    data class AgentMessage(
        override val sessionId: String?,
        override val envelopeTimestamp: String?,
        val content: String,
        val sender: String,
    ) : WireFrame

    /** ai-response 终帧：基础三字段必有，编排附加输出零值省略（PROTOCOL.md §4.1）。 */
    data class AiResponse(
        override val sessionId: String?,
        override val envelopeTimestamp: String?,
        val content: String,
        val confidence: Double,
        val source: String,
        val sources: List<KnowledgeSource>?,
        val strategy: String?,
        val nextAction: String?,
        val handoffReason: String?,
    ) : WireFrame

    /** ai-response-delta 流式增量；done=false 即到即拼，done=true 终末增量（content_delta 为空）。 */
    data class AiResponseDelta(
        override val sessionId: String?,
        override val envelopeTimestamp: String?,
        val contentDelta: String,
        val done: Boolean,
    ) : WireFrame

    /** transfer_notification：会话已分配坐席（含等待队列派发），状态机 → agent_chatting。 */
    data class TransferNotification(
        override val sessionId: String?,
        override val envelopeTimestamp: String?,
        val message: String,
        val agentId: Long,
    ) : WireFrame

    /** waiting_notification：已入等待队列，状态机 → waiting_human。 */
    data class WaitingNotification(
        override val sessionId: String?,
        override val envelopeTimestamp: String?,
        val message: String,
    ) : WireFrame

    /** webrtc 信令族：移动端 V1 不消费（createMobileCapabilitySet voice/remote_assist 置 off）。 */
    data class WebRtcSignal(
        override val sessionId: String?,
        override val envelopeTimestamp: String?,
        val signalType: String,
    ) : WireFrame

    /** 未知/死分支帧：忽略而非报错。 */
    data class Unknown(
        override val sessionId: String?,
        override val envelopeTimestamp: String?,
        val type: String,
    ) : WireFrame
}

/** ai-response.sources 元素（pkg/weknora SearchResult 的 JSON 形状）。 */
data class KnowledgeSource(
    val documentId: String,
    val title: String,
    val content: String?,
    val score: Double?,
)
