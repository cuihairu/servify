package servify.sdk.android.core

import servify.sdk.android.protocol.WireFrame

/**
 * headless 会话核心的协议消费者：帧进 → 语义事件 + 状态转移出。
 *
 * 探针 CLI 与 fixtures 回放测试共用此消费路径（M0 验收①：双端同一套样例、
 * 同一套语义断言）；事件名对齐 fixtures expectations 的 kind 词汇表。
 */
class SessionCore(initialState: HandoffState = HandoffState.AiAnswering) {

    val handoff = HandoffStateMachine(initialState)
    val assembler = StreamingAssembler()

    /** 消费一条已解码帧，产出对应的协议语义事件；webrtc/未知帧产出显式忽略事件。 */
    fun consume(frame: WireFrame): ProtocolEvent = when (frame) {
        is WireFrame.VisitorEcho -> ProtocolEvent.VisitorEcho(frame.content)
        is WireFrame.AgentMessage -> ProtocolEvent.AgentMessage(frame.content, frame.sender)
        is WireFrame.AiResponse -> {
            assembler.onFinal(frame.content)
            ProtocolEvent.AiFinal(
                content = frame.content,
                matchesConcatenation = assembler.finalMatchesConcatenation,
            )
        }
        is WireFrame.AiResponseDelta -> {
            assembler.onDelta(frame.contentDelta, frame.done)
            ProtocolEvent.AiDelta(frame.contentDelta, frame.done)
        }
        is WireFrame.TransferNotification -> {
            val accepted = handoff.onTransferNotification()
            ProtocolEvent.TransferReceived(frame.agentId, frame.message, accepted)
        }
        is WireFrame.WaitingNotification -> {
            val accepted = handoff.onWaitingNotification()
            ProtocolEvent.WaitingReceived(frame.message, accepted)
        }
        is WireFrame.WebRtcSignal -> ProtocolEvent.WebRtcIgnored(frame.signalType)
        is WireFrame.Unknown -> ProtocolEvent.UnknownIgnored(frame.type)
    }

    /** 流中断判定代理（终末增量已到但无 ai-response 终帧）。 */
    val streamInterrupted: Boolean get() = assembler.interrupted
}

/** 协议语义事件——fixtures 回放与探针断言的共同词汇。 */
sealed interface ProtocolEvent {
    data class VisitorEcho(val content: String) : ProtocolEvent
    data class AgentMessage(val content: String, val sender: String) : ProtocolEvent
    data class AiDelta(val contentDelta: String, val done: Boolean) : ProtocolEvent
    data class AiFinal(val content: String, val matchesConcatenation: Boolean?) : ProtocolEvent
    data class TransferReceived(val agentId: Long, val message: String, val stateAccepted: Boolean) : ProtocolEvent
    data class WaitingReceived(val message: String, val stateAccepted: Boolean) : ProtocolEvent
    data class WebRtcIgnored(val signalType: String) : ProtocolEvent
    data class UnknownIgnored(val type: String) : ProtocolEvent
}
