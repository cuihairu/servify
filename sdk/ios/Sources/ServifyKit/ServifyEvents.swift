import Foundation

/// 事件流聚合面（平台规格 §4.3，V1 冻结：与 core `ServifyEventMap` 对齐命名）。
/// 经 `ServifyChat.events` 暴露；接入方 for-await 消费。
///
/// 显式排除：`agent_typing`（core 有、服务端零发射——PROTOCOL.md §5 死分支）、webrtc 系全部。
public struct ServifyEvents: Sendable {
    /// 会话消息（含流式增量，`isStreaming` 标记拼接中气泡）。
    public let messages: EventStream<ConversationMessage>
    /// 未读计数（会话页可见时清零）。
    public let unreadCount: StateStream<Int>
    /// 连接状态机（§4.4）。
    public let connectionState: StateStream<ConnectionState>
    /// 每次重连调度发射尝试序号（1 起）。
    public let reconnecting: EventStream<Int>
    /// transfer_notification 到达。
    public let agentAssigned: EventStream<AgentAssignment>
    /// waiting_notification 到达。
    public let waitingInQueue: EventStream<String>
    /// 协议/传输错误。
    public let error: EventStream<ServifyError>

    init(
        messages: EventStream<ConversationMessage>,
        unreadCount: StateStream<Int>,
        connectionState: StateStream<ConnectionState>,
        reconnecting: EventStream<Int>,
        agentAssigned: EventStream<AgentAssignment>,
        waitingInQueue: EventStream<String>,
        error: EventStream<ServifyError>
    ) {
        self.messages = messages
        self.unreadCount = unreadCount
        self.connectionState = connectionState
        self.reconnecting = reconnecting
        self.agentAssigned = agentAssigned
        self.waitingInQueue = waitingInQueue
        self.error = error
    }
}
