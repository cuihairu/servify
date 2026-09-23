import Testing

@testable import ServifyKit

/**
 * 错误七码对账（平台规格 §4.5）：code 字符串与 retryable 逐一锁定。
 * Kotlin 镜像：ServifyErrorTest.kt——表逐项一致防单侧漂移。
 */
struct ServifyErrorTests {

    @Test func sevenErrorCodesCarrySpecifiedRetryability() {
        let errors: [ServifyError] = [
            .configInvalid(message: "bad url"),
            .network(message: "dns"),
            .serverUnavailable(message: "503"),
            .handshakeRejected(message: "origin denied"),
            .sendTimeout(message: "no echo"),
            .unsupported(message: "endpoint not live"),
            .ticketFailed(message: "create failed"),
        ]

        let expected: [(String, Bool)] = [
            ("config_invalid", false),
            ("network", true),
            ("server_unavailable", true),
            ("handshake_rejected", false),
            ("send_timeout", true),
            ("unsupported", false),
            ("ticket_failed", true),
        ]
        for (error, want) in zip(errors, expected) {
            #expect(error.code == want.0)
            #expect(error.retryable == want.1)
        }
        #expect(errors.count == expected.count)
        #expect(errors.allSatisfy { !$0.message.isEmpty })
    }

    /** 模型语义锚定：suggestsHandoff 由 nextAction 驱动（D8 置信门"转人工"强调态）。
     * Kotlin 侧由 ChatUiStateTest 消费——UI 刀 3 接线前先在此锁定判定语义。 */
    @Test func suggestsHandoffTracksNextAction() {
        let plain = ConversationMessage(id: "1", sessionId: "s", sender: .system, content: "常规回答", createdAt: 0)
        #expect(!plain.suggestsHandoff)

        let handoff = ConversationMessage(
            id: "2", sessionId: "s", sender: .system, content: "低置信回答",
            createdAt: 0, isAiResponse: true, nextAction: "handoff"
        )
        #expect(handoff.suggestsHandoff)
    }
}
