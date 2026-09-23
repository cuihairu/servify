import Foundation

/// 服务端下行帧的协议语义类型（契约唯一事实源：sdk/PROTOCOL.md）。
///
/// 未知类型归 `.unknown`——服务端对未知上行"警告后丢弃"，客户端对未知下行同样忽略而非报错
/// （PROTOCOL.md §3/§6.4）；core 声明过但服务端零发射的死分支帧（session_update/agent_status 等，
/// PROTOCOL.md §5）自然落进 `.unknown`，移动端契约不含它们。
/// Kotlin 镜像：sdk/android/shared/protocol WireFrame.kt——两文件字段逐一同名同义，改一处必改另一处。
public enum WireFrame: Equatable {
    /// text-message 回显：客户自己消息经服务端广播回来（PROTOCOL.md §4.1，发送成功判据）。
    case visitorEcho(sessionId: String?, envelopeTimestamp: String?, content: String)

    /// agent-message：坐席发言 {content, sender}。
    case agentMessage(sessionId: String?, envelopeTimestamp: String?, content: String, sender: String)

    /// ai-response 终帧：基础三字段必有，编排附加输出零值省略（PROTOCOL.md §4.1）。
    case aiResponse(
        sessionId: String?,
        envelopeTimestamp: String?,
        content: String,
        confidence: Double,
        source: String,
        sources: [KnowledgeSource]?,
        strategy: String?,
        nextAction: String?,
        handoffReason: String?
    )

    /// ai-response-delta 流式增量；done=false 即到即拼，done=true 终末增量（content_delta 为空）。
    case aiResponseDelta(sessionId: String?, envelopeTimestamp: String?, contentDelta: String, done: Bool)

    /// transfer_notification：会话已分配坐席（含等待队列派发），状态机 → agent_chatting。
    case transferNotification(sessionId: String?, envelopeTimestamp: String?, message: String, agentId: Int64)

    /// waiting_notification：已入等待队列，状态机 → waiting_human。
    case waitingNotification(sessionId: String?, envelopeTimestamp: String?, message: String)

    /// webrtc 信令族：移动端 V1 不消费（createMobileCapabilitySet voice/remote_assist 置 off）。
    case webRtcSignal(sessionId: String?, envelopeTimestamp: String?, signalType: String)

    /// 未知/死分支帧：忽略而非报错。
    case unknown(sessionId: String?, envelopeTimestamp: String?, type: String)
}

/// ai-response.sources 元素（pkg/weknora SearchResult 的 JSON 形状）。
public struct KnowledgeSource: Equatable, Sendable {
    public let documentId: String
    public let title: String
    public let content: String?
    public let score: Double?

    public init(documentId: String, title: String, content: String?, score: Double?) {
        self.documentId = documentId
        self.title = title
        self.content = content
        self.score = score
    }
}
