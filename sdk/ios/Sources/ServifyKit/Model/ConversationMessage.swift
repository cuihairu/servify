import Foundation

/// 消息来源方（D5 规范形 sender_type 三值；V1 不产生其他值）。
public enum SenderType: Equatable, Sendable {
    case customer
    case agent
    case system
}

/// 会话消息（设计文档 D5 规范形，跨端一致）：
/// id 缺省回退 "ws-" + 本地序号（对齐 core normalizeMessage）；sources/confidence/nextAction
/// 是 ai-response 帧编排附加输出（D5 metadata 的类型化透出）——sources/confidence 供引用
/// 展示，nextAction=="handoff" 驱动"转人工"按钮强调态（D8 置信门提示）；其余附加输出
/// （strategy/handoff_reason）无 V1 消费面，按死代码纪律不进。
public struct ConversationMessage: Equatable, Sendable {
    public let id: String
    public let sessionId: String
    public let sender: SenderType
    public let content: String
    public let createdAt: Int64
    /// true = 流式增量中的气泡（UI 据此渲染拼接中状态）；终帧到达时翻 false。
    public let isAiResponse: Bool
    public let isStreaming: Bool
    public let sources: [KnowledgeSource]
    public let confidence: Double?
    public let nextAction: String?

    public init(
        id: String,
        sessionId: String,
        sender: SenderType,
        content: String,
        createdAt: Int64,
        isAiResponse: Bool = false,
        isStreaming: Bool = false,
        sources: [KnowledgeSource] = [],
        confidence: Double? = nil,
        nextAction: String? = nil
    ) {
        self.id = id
        self.sessionId = sessionId
        self.sender = sender
        self.content = content
        self.createdAt = createdAt
        self.isAiResponse = isAiResponse
        self.isStreaming = isStreaming
        self.sources = sources
        self.confidence = confidence
        self.nextAction = nextAction
    }

    /// 置信门建议转人工（D8：渲染"转人工"按钮强调态）。
    public var suggestsHandoff: Bool { nextAction == "handoff" }
}

/// transfer_notification 的事件载荷（平台规格 §4.3 agentAssigned）。
public struct AgentAssignment: Equatable, Sendable {
    public let agentId: Int64
    public let message: String

    public init(agentId: Int64, message: String) {
        self.agentId = agentId
        self.message = message
    }
}
