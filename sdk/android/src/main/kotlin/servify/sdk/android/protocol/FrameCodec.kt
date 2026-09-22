package servify.sdk.android.protocol

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull

/**
 * WS 文本帧编解码（帧编码契约：JSON 文本帧，形 {type, data, session_id?, timestamp?}，
 * PROTOCOL.md §1）。
 *
 * decode 对未知 type 返回 [WireFrame.Unknown] 而不是抛异常；对形状不符的已知帧同样
 * 降级为 Unknown（畸形帧静默语义，PROTOCOL.md §6.4——服务端不回错误帧，客户端也不因
 * 单帧崩溃）。
 */
object FrameCodec {
    private val json = Json { ignoreUnknownKeys = true }

    /** 服务端 webrtc 信令族 type 全集（PROTOCOL.md §4.3），移动端显式建模但不消费。 */
    private val WEBRTC_TYPES = setOf(
        "webrtc-offer",
        "webrtc-answer",
        "webrtc-candidate",
        "webrtc-ice-config",
        "webrtc-state-change",
        "data-channel-message",
    )

    fun decode(raw: String): WireFrame {
        val root = runCatching { json.parseToJsonElement(raw).jsonObject }.getOrNull()
            ?: return WireFrame.Unknown(null, null, "<malformed-json>")
        val type = (root["type"] as? JsonPrimitive)?.contentOrNull
            ?: return WireFrame.Unknown(null, null, "<missing-type>")
        val sessionId = (root["session_id"] as? JsonPrimitive)?.contentOrNull
        val timestamp = (root["timestamp"] as? JsonPrimitive)?.contentOrNull
        val data = root["data"]
        return when (type) {
            "text-message" -> {
                // data 双形态：对象 {content} 或裸字符串（PROTOCOL.md §3，服务端两种都接受）
                val content = when (data) {
                    is JsonObject -> data.str("content")
                    is JsonPrimitive -> data.contentOrNull
                    else -> null
                } ?: return WireFrame.Unknown(sessionId, timestamp, type)
                WireFrame.VisitorEcho(sessionId, timestamp, content)
            }
            "agent-message" -> {
                val obj = data as? JsonObject ?: return WireFrame.Unknown(sessionId, timestamp, type)
                val content = obj.str("content") ?: return WireFrame.Unknown(sessionId, timestamp, type)
                WireFrame.AgentMessage(sessionId, timestamp, content, obj.str("sender") ?: "")
            }
            "ai-response" -> {
                val obj = data as? JsonObject ?: return WireFrame.Unknown(sessionId, timestamp, type)
                val content = obj.str("content") ?: return WireFrame.Unknown(sessionId, timestamp, type)
                val confidence = obj["confidence"]?.jsonPrimitive?.doubleOrNull
                    ?: return WireFrame.Unknown(sessionId, timestamp, type)
                val source = obj.str("source") ?: return WireFrame.Unknown(sessionId, timestamp, type)
                // 编排附加输出零值省略：缺失即 null，不做默认填充
                val sources = (obj["sources"] as? kotlinx.serialization.json.JsonArray)?.map { el ->
                    val s = el.jsonObject
                    KnowledgeSource(
                        documentId = s.str("document_id") ?: "",
                        title = s.str("title") ?: "",
                        content = s.str("content"),
                        score = s["score"]?.jsonPrimitive?.doubleOrNull,
                    )
                }
                WireFrame.AiResponse(
                    sessionId = sessionId,
                    envelopeTimestamp = timestamp,
                    content = content,
                    confidence = confidence,
                    source = source,
                    sources = sources,
                    strategy = obj.str("strategy"),
                    nextAction = obj.str("next_action"),
                    handoffReason = obj.str("handoff_reason"),
                )
            }
            "ai-response-delta" -> {
                val obj = data as? JsonObject ?: return WireFrame.Unknown(sessionId, timestamp, type)
                val delta = obj.str("content_delta") ?: return WireFrame.Unknown(sessionId, timestamp, type)
                // 服务端每帧都带 done（streamAIResponse 的 map 显式置值）
                WireFrame.AiResponseDelta(sessionId, timestamp, delta, obj["done"]?.jsonPrimitive?.booleanOrNull ?: false)
            }
            "transfer_notification" -> {
                val obj = data as? JsonObject ?: return WireFrame.Unknown(sessionId, timestamp, type)
                val message = obj.str("message") ?: return WireFrame.Unknown(sessionId, timestamp, type)
                val agentId = obj["agent_id"]?.jsonPrimitive?.longOrNull
                    ?: return WireFrame.Unknown(sessionId, timestamp, type)
                WireFrame.TransferNotification(sessionId, timestamp, message, agentId)
            }
            "waiting_notification" -> {
                val obj = data as? JsonObject ?: return WireFrame.Unknown(sessionId, timestamp, type)
                val message = obj.str("message") ?: return WireFrame.Unknown(sessionId, timestamp, type)
                WireFrame.WaitingNotification(sessionId, timestamp, message)
            }
            in WEBRTC_TYPES -> WireFrame.WebRtcSignal(sessionId, timestamp, type)
            else -> WireFrame.Unknown(sessionId, timestamp, type)
        }
    }

    /** 客户端上行 text-message 编码（PROTOCOL.md §3：对象形态 {content}）。 */
    fun encodeTextMessage(content: String, sessionId: String): String {
        val envelope = JsonObject(
            mapOf(
                "type" to JsonPrimitive("text-message"),
                "data" to JsonObject(mapOf("content" to JsonPrimitive(content))),
                "session_id" to JsonPrimitive(sessionId),
            ),
        )
        return json.encodeToString(JsonObject.serializer(), envelope)
    }

    private fun JsonObject.str(key: String): String? =
        (this[key] as? JsonPrimitive)?.contentOrNull
}
