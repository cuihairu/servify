import XCTest
@testable import ServifyKit

/// 转移表穷举（M1 验收⑤口径：状态机转移表必须穷举）。Kotlin 镜像：StateMachineTest.kt
/// HandoffStateMachineTest，用例名逐一同名——双端断言同型，防单侧漂移。
final class HandoffStateMachineTests: XCTestCase {

    func testTransferFromAiAnsweringAndWaitingHumanAreLegal() {
        for from in [HandoffState.aiAnswering, .waitingHuman] {
            let sm = HandoffStateMachine(initial: from)
            XCTAssertTrue(sm.onTransferNotification(), "from=\(from)")
            XCTAssertEqual(.agentChatting, sm.state)
            XCTAssertEqual([HandoffTransition(from: from, to: .agentChatting)], sm.transitions)
        }
    }

    func testTransferFromAgentChattingIsRejected() {
        let sm = HandoffStateMachine(initial: .agentChatting)
        XCTAssertFalse(sm.onTransferNotification())
        XCTAssertEqual(.agentChatting, sm.state)
        XCTAssertTrue(sm.transitions.isEmpty)
    }

    func testWaitingOnlyFromAiAnsweringIsLegal() {
        let sm = HandoffStateMachine(initial: .aiAnswering)
        XCTAssertTrue(sm.onWaitingNotification())
        XCTAssertEqual(.waitingHuman, sm.state)

        let busy = HandoffStateMachine(initial: .agentChatting)
        XCTAssertFalse(busy.onWaitingNotification())
        let queued = HandoffStateMachine(initial: .waitingHuman)
        // waiting_human 收到重复 waiting（重复排队）拒绝——非法转移显式暴露
        XCTAssertFalse(queued.onWaitingNotification())
        XCTAssertEqual(.waitingHuman, queued.state)
    }

    func testFullQueueThenDispatchPathMatchesServerBehaviour() {
        // 真实路径：ai_answering →(waiting)→ waiting_human →(队列派发 transfer)→ agent_chatting
        let sm = HandoffStateMachine(initial: .aiAnswering)
        XCTAssertTrue(sm.onWaitingNotification())
        XCTAssertTrue(sm.onTransferNotification())
        XCTAssertEqual(
            [
                HandoffTransition(from: .aiAnswering, to: .waitingHuman),
                HandoffTransition(from: .waitingHuman, to: .agentChatting),
            ],
            sm.transitions
        )
    }
}
