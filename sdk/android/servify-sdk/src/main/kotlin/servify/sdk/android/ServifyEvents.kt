package servify.sdk.android

import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import servify.sdk.android.connect.ConnectionState
import servify.sdk.android.model.AgentAssignment
import servify.sdk.android.model.ConversationMessage

/**
 * 事件流聚合面（平台规格 §4.3，V1 冻结：与 core `ServifyEventMap` 对齐命名）。
 * 经 [ServifyChat.events] 暴露；接入方 collect / collectLatest 消费。
 *
 * 显式排除：`agent_typing`（core 有、服务端零发射——PROTOCOL.md §5 死分支）、webrtc 系全部。
 */
class ServifyEvents(
    /** 会话消息（含流式增量，`isStreaming` 标记拼接中气泡）。 */
    val messages: SharedFlow<ConversationMessage>,
    /** 未读计数（会话页可见时清零）。 */
    val unreadCount: StateFlow<Int>,
    /** 连接状态机（§4.4）。 */
    val connectionState: StateFlow<ConnectionState>,
    /** 每次重连调度发射尝试序号（1 起）。 */
    val reconnecting: SharedFlow<Int>,
    /** transfer_notification 到达。 */
    val agentAssigned: SharedFlow<AgentAssignment>,
    /** waiting_notification 到达。 */
    val waitingInQueue: SharedFlow<String>,
    /** 协议/传输错误。 */
    val error: SharedFlow<ServifyError>,
)
