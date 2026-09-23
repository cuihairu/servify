import Testing

@testable import ServifyKit

/**
 * 连接状态机转移表穷举（§4.4，M2 验收镜像 M1⑤）：
 *
 * ```
 * idle ─(首次 connect/show)→ connecting → connected ─(断)→ reconnecting(n) → connected
 *                                      └─(握手失败)→ disconnected
 * disconnected ─(用户再次 connect/show)→ connecting；重连耗尽也落 disconnected
 * connected/connecting 期间重复 connect = 无操作（幂等）
 * ```
 *
 * Kotlin 镜像：ConnectionLifecycleTest.kt——用例名逐一对应防单侧漂移。
 * 断线重连边（connected → reconnecting → connected）由
 * ServifyChatTests.reconnectsAfterServerDrop 锚定；destroy → disconnected 由
 * destroyIsIdempotentAndMarksDisconnected 锚定——本类不重复。
 */
struct ConnectionLifecycleTests {

    private func makeChat(
        _ transports: [MockTransport],
        policy: ReconnectPolicy? = nil,
        branding: Branding = Branding()
    ) -> ServifyChat {
        ServifyChat(
            config: try! ServifyConfig(apiUrl: "https://chat.example.com", branding: branding),
            sessionId: "test-session",
            policy: policy ?? (try! ReconnectPolicy(maxAttempts: 3, initialDelayMs: 100, multiplier: 2, maxDelayMs: 400)),
            echoTimeoutMs: 200,
            wsUrlOverride: "ws://mock.test/api/v1/ws",
            transportFactory: TransportSequence(transports).factory()
        )
    }

    @Test func initialConnectionStateIsIdle() throws {
        let chat = makeChat([MockTransport()])
        #expect(chat.events.connectionState.value == .idle)
    }

    @Test func idlePassesThroughConnectingToConnected() async throws {
        let plain = MockTransport()
        let chat = makeChat([plain])
        let states = chat.events.connectionState.makeStream()
        try await chat.connect()

        let connecting = try await nextMatching(states, where: { state in
            if case .connecting = state { return true }
            return false
        })
        #expect(connecting == .connecting)

        plain.emitOpen()
        try await awaitConnected(chat)
    }

    @Test func repeatedConnectWhileConnectedIsNoop() async throws {
        let chat = makeChat([EchoTransport()])
        try await chat.connect()
        try await awaitConnected(chat)

        // 幂等：不替换连接——原连接回显链路依旧可达。
        try await chat.connect()
        try await chat.sendMessage("仍在原连接")
        #expect(chat.events.connectionState.value == .connected)
    }

    @Test func handshakeFailureBeforeEverConnectedMarksDisconnected() async throws {
        let chat = makeChat([HandshakeRejectTransport(status: 404)])
        let states = chat.events.connectionState.makeStream()

        try await chat.connect()
        let state = try await nextMatching(states, where: { $0 == .disconnected })
        #expect(state == .disconnected)
    }

    @Test func serverCloseAfterConnectedTriggersReconnect() async throws {
        // 服务端主动关（1000 正常关码，闲置踢线/代理超时）对移动端同为断线：
        // onClosed → §4.4 connected ─断→ reconnecting(1) → 退避后自动恢复。
        let closing = MockTransport()
        let chat = makeChat([closing, EchoTransport()])
        let states = chat.events.connectionState.makeStream()
        try await chat.connect()
        closing.emitOpen()
        try await awaitConnected(chat)

        closing.emitClosed(code: 1000)
        let reconnecting = try await nextMatching(states, where: { state in
            if case .reconnecting = state { return true }
            return false
        })
        #expect(reconnecting == .reconnecting(attempt: 1))
        try await awaitConnected(chat)
        #expect(chat.events.connectionState.value == .connected)
    }

    @Test func reconnectExhaustionMarksDisconnectedAndConnectRecovers() async throws {
        // 第一次连接成功后断线 → 重连 #1 握手被拒（404）→ 再次退避耗尽（maxAttempts=1：
        // delayFor(1) 仍放行、delayFor(2)=null）→ disconnected。
        let first = DropOnFirstMessageTransport()
        let chat = makeChat(
            [first, HandshakeRejectTransport(status: 404), EchoTransport()],
            policy: try! ReconnectPolicy(maxAttempts: 1, initialDelayMs: 50, multiplier: 2, maxDelayMs: 100)
        )
        let states = chat.events.connectionState.makeStream()
        try await chat.connect()
        first.emitOpen()
        try await awaitConnected(chat)

        try await chat.sendMessage("触发断线") // 回显不来，按超时收尾
        let disconnected = try await nextMatching(states, where: { $0 == .disconnected })
        #expect(disconnected == .disconnected)

        // §4.4：disconnected ─(用户再次打开会话页)→ connecting → connected。
        try await chat.connect()
        try await awaitConnected(chat)
    }

    // MARK: - M3 Branding 四件套收口：offlineText 在 disconnected 终态追加系统提示行。

    @Test func offlineHintEmittedOnReconnectExhaustionWhenConfigured() async throws {
        // 第一次连接成功后断线 → 重连 #1 握手被拒（404）→ 再次退避耗尽 → disconnected。
        let first = DropOnFirstMessageTransport()
        let chat = makeChat(
            [first, HandshakeRejectTransport(status: 404), EchoTransport()],
            policy: try! ReconnectPolicy(maxAttempts: 1, initialDelayMs: 50, multiplier: 2, maxDelayMs: 100),
            branding: Branding(offlineText: "客服当前不在线，请稍后再来")
        )
        try await chat.connect()
        first.emitOpen()
        try await awaitConnected(chat)

        let states = chat.events.connectionState.makeStream()
        let messages = chat.events.messages.makeStream()
        try await chat.sendMessage("触发断线") // 回显不来，按超时收尾
        let disconnected = try await nextMatching(states, where: { $0 == .disconnected })
        #expect(disconnected == .disconnected)
        let hint = try await nextMatching(messages, where: {
            $0.sender == .system && $0.content == "客服当前不在线，请稍后再来"
        })
        #expect(hint.sessionId == "test-session")
        // 提示行是 SDK 自造 UI 状态行（同流中断提示），不计未读。
        #expect(chat.events.unreadCount.value == 0)
    }

    @Test func offlineHintOmittedWhenNotConfigured() async throws {
        let first = DropOnFirstMessageTransport()
        let chat = makeChat(
            [first, HandshakeRejectTransport(status: 404), EchoTransport()],
            policy: try! ReconnectPolicy(maxAttempts: 1, initialDelayMs: 50, multiplier: 2, maxDelayMs: 100)
        )
        try await chat.connect()
        first.emitOpen()
        try await awaitConnected(chat)

        let states = chat.events.connectionState.makeStream()
        try await chat.sendMessage("触发断线")
        _ = try await nextMatching(states, where: { $0 == .disconnected })
        // 默认 offlineText=nil：快照无任何 System 提示行（流中断提示仅在有活跃流时出现，此处无流）。
        #expect(!chat.historySnapshot().contains { $0.sender == .system })
    }

    @Test func offlineHintEmittedOnHandshakeFailureWhenConfigured() async throws {
        let chat = makeChat(
            [HandshakeRejectTransport(status: 404)],
            branding: Branding(offlineText: "客服当前不在线，请稍后再来")
        )
        let states = chat.events.connectionState.makeStream()
        let messages = chat.events.messages.makeStream()

        try await chat.connect()
        let state = try await nextMatching(states, where: { $0 == .disconnected })
        #expect(state == .disconnected)
        let hint = try await nextMatching(messages, where: {
            $0.sender == .system && $0.content == "客服当前不在线，请稍后再来"
        })
        #expect(hint.sessionId == "test-session")
    }

    @Test func offlineHintNotEmittedOnDestroy() async throws {
        let chat = makeChat([EchoTransport()], branding: Branding(offlineText: "客服当前不在线，请稍后再来"))
        try await chat.connect()
        try await awaitConnected(chat)

        chat.destroy()
        // 用户主动销毁 ≠ 客服离线：不追加提示行。
        #expect(!chat.historySnapshot().contains { $0.sender == .system })
    }
}
