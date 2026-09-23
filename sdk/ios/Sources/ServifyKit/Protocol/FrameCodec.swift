import Foundation

/// WS 文本帧编解码（帧编码契约：JSON 文本帧，形 {type, data, session_id?, timestamp?}，
/// PROTOCOL.md §1）。
///
/// decode 对未知 type 返回 `.unknown` 而不是抛异常；对形状不符的已知帧同样降级为
/// `.unknown`（畸形帧静默语义，PROTOCOL.md §6.4——服务端不回错误帧，客户端也不因单帧崩溃）。
/// 不用 Codable/JSONDecoder：畸形帧"逐字段降级"语义需要手动逐键导航，JSONSerialization 直给
/// 字典树最贴近 Kotlin 侧 kotlinx.json 的镜像写法。
public enum FrameCodec {

    /// 服务端 webrtc 信令族 type 全集（PROTOCOL.md §4.3），移动端显式建模但不消费。
    private static let webrtcTypes: Set<String> = [
        "webrtc-offer",
        "webrtc-answer",
        "webrtc-candidate",
        "webrtc-ice-config",
        "webrtc-state-change",
        "data-channel-message",
    ]

    public static func decode(_ raw: String) -> WireFrame {
        guard let rawBytes = raw.data(using: .utf8),
              let root = (try? JSONSerialization.jsonObject(with: rawBytes)) as? [String: Any]
        else { return .unknown(sessionId: nil, envelopeTimestamp: nil, type: "<malformed-json>") }
        guard let type = string(root, "type")
        else { return .unknown(sessionId: nil, envelopeTimestamp: nil, type: "<missing-type>") }
        let sessionId = string(root, "session_id")
        let timestamp = string(root, "timestamp")
        let data = root["data"]

        switch type {
        case "text-message":
            // data 双形态：对象 {content} 或裸字符串（PROTOCOL.md §3，服务端两种都接受）
            let content: String?
            if let obj = data as? [String: Any] {
                content = string(obj, "content")
            } else {
                content = data as? String
            }
            guard let content else { return .unknown(sessionId: sessionId, envelopeTimestamp: timestamp, type: type) }
            return .visitorEcho(sessionId: sessionId, envelopeTimestamp: timestamp, content: content)

        case "agent-message":
            guard let obj = data as? [String: Any], let content = string(obj, "content")
            else { return .unknown(sessionId: sessionId, envelopeTimestamp: timestamp, type: type) }
            return .agentMessage(
                sessionId: sessionId, envelopeTimestamp: timestamp,
                content: content, sender: string(obj, "sender") ?? ""
            )

        case "ai-response":
            guard let obj = data as? [String: Any],
                  let content = string(obj, "content"),
                  let confidence = double(obj, "confidence"),
                  let source = string(obj, "source")
            else { return .unknown(sessionId: sessionId, envelopeTimestamp: timestamp, type: type) }
            // 编排附加输出零值省略：缺失即 nil，不做默认填充
            let sources = (obj["sources"] as? [[String: Any]])?.map { element in
                KnowledgeSource(
                    documentId: string(element, "document_id") ?? "",
                    title: string(element, "title") ?? "",
                    content: string(element, "content"),
                    score: double(element, "score")
                )
            }
            return .aiResponse(
                sessionId: sessionId, envelopeTimestamp: timestamp,
                content: content, confidence: confidence, source: source,
                sources: sources,
                strategy: string(obj, "strategy"),
                nextAction: string(obj, "next_action"),
                handoffReason: string(obj, "handoff_reason")
            )

        case "ai-response-delta":
            guard let obj = data as? [String: Any],
                  let delta = string(obj, "content_delta")
            else { return .unknown(sessionId: sessionId, envelopeTimestamp: timestamp, type: type) }
            // 服务端每帧都带 done（streamAIResponse 的 map 显式置值）
            return .aiResponseDelta(
                sessionId: sessionId, envelopeTimestamp: timestamp,
                contentDelta: delta, done: bool(obj, "done") ?? false
            )

        case "transfer_notification":
            guard let obj = data as? [String: Any],
                  let message = string(obj, "message"),
                  let agentId = int64(obj, "agent_id")
            else { return .unknown(sessionId: sessionId, envelopeTimestamp: timestamp, type: type) }
            return .transferNotification(sessionId: sessionId, envelopeTimestamp: timestamp, message: message, agentId: agentId)

        case "waiting_notification":
            guard let obj = data as? [String: Any], let message = string(obj, "message")
            else { return .unknown(sessionId: sessionId, envelopeTimestamp: timestamp, type: type) }
            return .waitingNotification(sessionId: sessionId, envelopeTimestamp: timestamp, message: message)

        case let t where webrtcTypes.contains(t):
            return .webRtcSignal(sessionId: sessionId, envelopeTimestamp: timestamp, signalType: t)

        default:
            return .unknown(sessionId: sessionId, envelopeTimestamp: timestamp, type: type)
        }
    }

    /// 客户端上行 text-message 编码（PROTOCOL.md §3：对象形态 {content}）。
    public static func encodeTextMessage(_ content: String, sessionId: String) -> String {
        let envelope: [String: Any] = [
            "type": "text-message",
            "data": ["content": content],
            "session_id": sessionId,
        ]
        guard let bytes = try? JSONSerialization.data(withJSONObject: envelope),
              let raw = String(data: bytes, encoding: .utf8)
        else { return "{}" } // 构造面恒为合法 JSON，此分支不可达
        return raw
    }

    // MARK: - 取值助手（缺键 / null / 形状不符一律 nil，镜像 kotlinx contentOrNull 语义）

    private static func string(_ obj: [String: Any], _ key: String) -> String? {
        obj[key] as? String
    }

    private static func double(_ obj: [String: Any], _ key: String) -> Double? {
        obj[key] as? Double
    }

    private static func bool(_ obj: [String: Any], _ key: String) -> Bool? {
        obj[key] as? Bool
    }

    private static func int64(_ obj: [String: Any], _ key: String) -> Int64? {
        obj[key] as? Int64
    }
}
