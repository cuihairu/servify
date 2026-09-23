import Foundation

/// headless 会话核心的协议消费者：帧进 → 语义事件 + 状态转移出。
///
/// 探针与 fixtures 回放测试共用此消费路径（M2 验收①：双端同一套样例、同一套语义断言）；
/// 事件名对齐 fixtures expectations 的 kind 词汇表。
/// Kotlin 镜像：shared/core SessionCore.kt——终末增量非空等契约违约以 Swift throws 传播
/// （对齐 Kotlin require 的未捕获异常语义）。
public final class SessionCore {

    public let handoff: HandoffStateMachine
    public let assembler = StreamingAssembler()

    public init(initialState: HandoffState = .aiAnswering) {
        handoff = HandoffStateMachine(initial: initialState)
    }

    /// 消费一条已解码帧，产出对应的协议语义事件；webrtc/未知帧产出显式忽略事件。
    @discardableResult
    public func consume(_ frame: WireFrame) throws -> ProtocolEvent {
        switch frame {
        case let .visitorEcho(_, _, content):
            return .visitorEcho(content: content)
        case let .agentMessage(_, _, content, sender):
            return .agentMessage(content: content, sender: sender)
        case let .aiResponse(_, _, content, _, _, _, _, _, _):
            assembler.onFinal(content)
            return .aiFinal(content: content, matchesConcatenation: assembler.finalMatchesConcatenation)
        case let .aiResponseDelta(_, _, contentDelta, done):
            try assembler.onDelta(contentDelta, done: done)
            return .aiDelta(contentDelta: contentDelta, done: done)
        case let .transferNotification(_, _, message, agentId):
            let accepted = handoff.onTransferNotification()
            return .transferReceived(agentId: agentId, message: message, stateAccepted: accepted)
        case let .waitingNotification(_, _, message):
            let accepted = handoff.onWaitingNotification()
            return .waitingReceived(message: message, stateAccepted: accepted)
        case let .webRtcSignal(_, _, signalType):
            return .webRtcIgnored(signalType: signalType)
        case let .unknown(_, _, type):
            return .unknownIgnored(type: type)
        }
    }

    /// 流中断判定代理（终末增量已到但无 ai-response 终帧）。
    public var streamInterrupted: Bool { assembler.interrupted }
}

/// 协议语义事件——fixtures 回放与探针断言的共同词汇。
public enum ProtocolEvent: Equatable {
    case visitorEcho(content: String)
    case agentMessage(content: String, sender: String)
    case aiDelta(contentDelta: String, done: Bool)
    case aiFinal(content: String, matchesConcatenation: Bool?)
    case transferReceived(agentId: Int64, message: String, stateAccepted: Bool)
    case waitingReceived(message: String, stateAccepted: Bool)
    case webRtcIgnored(signalType: String)
    case unknownIgnored(type: String)
}
