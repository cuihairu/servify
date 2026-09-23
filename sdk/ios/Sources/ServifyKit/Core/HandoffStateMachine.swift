import Foundation

/// 转人工状态机（转移表与 PROTOCOL.md §4.2 逐条对应，事件全部为真实帧）：
///
/// ```
/// ai_answering  ──(transfer_notification)──> agent_chatting
/// ai_answering  ──(waiting_notification)───> waiting_human
/// waiting_human ──(transfer_notification)──> agent_chatting
/// ```
///
/// 非法转移被拒绝并返回 false——状态机转移表必须穷举（策划文档验收⑤），收到语义上
/// 不可能的帧序列时显式暴露而不是静默吞掉。Kotlin 镜像：shared/core HandoffStateMachine.kt。
public final class HandoffStateMachine {

    public private(set) var state: HandoffState

    /// 已发生的合法转移记录（from → to），fixtures 回放断言用。
    public private(set) var transitions: [HandoffTransition] = []

    public init(initial: HandoffState = .aiAnswering) {
        state = initial
    }

    @discardableResult
    public func onTransferNotification() -> Bool {
        let legal = state == .aiAnswering || state == .waitingHuman
        if legal { transitionTo(.agentChatting) }
        return legal
    }

    @discardableResult
    public func onWaitingNotification() -> Bool {
        let legal = state == .aiAnswering
        if legal { transitionTo(.waitingHuman) }
        return legal
    }

    private func transitionTo(_ next: HandoffState) {
        transitions.append(HandoffTransition(from: state, to: next))
        state = next
    }
}

public enum HandoffState: Equatable, Hashable {
    case aiAnswering
    case waitingHuman
    case agentChatting
    /// 设计在案、V1 无入边：进入依赖补拉语义（服务端配套项落地前不接线，防死代码）。
    case closed
}

public struct HandoffTransition: Equatable {
    public let from: HandoffState
    public let to: HandoffState

    public init(from: HandoffState, to: HandoffState) {
        self.from = from
        self.to = to
    }
}
