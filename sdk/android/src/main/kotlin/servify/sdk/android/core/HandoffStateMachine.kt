package servify.sdk.android.core

/**
 * 转人工状态机（转移表与 PROTOCOL.md §4.2 逐条对应，事件全部为真实帧）：
 *
 * ```
 * ai_answering  ──(transfer_notification)──> agent_chatting
 * ai_answering  ──(waiting_notification)───> waiting_human
 * waiting_human ──(transfer_notification)──> agent_chatting
 * ```
 *
 * 非法转移被拒绝并返回 false——状态机转移表必须穷举（策划文档 M1 验收⑤），
 * 收到语义上不可能的帧序列时显式暴露而不是静默吞掉。
 */
class HandoffStateMachine(initial: HandoffState = HandoffState.AiAnswering) {

    var state: HandoffState = initial
        private set

    /** 已发生的合法转移记录（from → to），fixtures 回放断言用。 */
    val transitions = mutableListOf<Pair<HandoffState, HandoffState>>()

    fun onTransferNotification(): Boolean {
        val legal = state == HandoffState.AiAnswering || state == HandoffState.WaitingHuman
        if (legal) transitionTo(HandoffState.AgentChatting)
        return legal
    }

    fun onWaitingNotification(): Boolean {
        val legal = state == HandoffState.AiAnswering
        if (legal) transitionTo(HandoffState.WaitingHuman)
        return legal
    }

    private fun transitionTo(next: HandoffState) {
        transitions += state to next
        state = next
    }
}

sealed interface HandoffState {
    data object AiAnswering : HandoffState
    data object WaitingHuman : HandoffState
    data object AgentChatting : HandoffState
    data object Closed : HandoffState
}
