import Testing

@testable import ServifyKit

/**
 * 门面接线集成测试：MockTransport 族承载传输回调行为（镜像 MockWebServer WS 升级），
 * 逐项验证回显判据、流式拼装、转人工事件、未读语义、握手失败与断线重连。
 *
 * Kotlin 镜像：ServifyChatTest.kt——用例名逐一对应防单侧漂移。
 * EventStream/StateStream 的 builder 在 makeStream 时同步注册（探针核实），
 * 先订阅后触发动作即可，无需挂载屏障。
 */
struct ServifyChatTests {

    private func testPolicy(maxAttempts: Int = 3) -> ReconnectPolicy {
        try! ReconnectPolicy(maxAttempts: maxAttempts, initialDelayMs: 100, multiplier: 2, maxDelayMs: 400)
    }

    private func makeChat(
        seq: TransportSequence? = nil,
        echoTimeoutMs: Int = 500,
        policy: ReconnectPolicy? = nil
    ) -> ServifyChat {
        let factory: () -> WebSocketTransport = seq?.factory() ?? { MockTransport() }
        return ServifyChat(
            config: try! ServifyConfig(apiUrl: "https://chat.example.com"),
            sessionId: "test-session",
            policy: policy ?? testPolicy(),
            echoTimeoutMs: echoTimeoutMs,
            wsUrlOverride: "ws://mock.test/api/v1/ws",
            transportFactory: factory
        )
    }

    // MARK: - 回显判据

    @Test func sendMessageCompletesOnEchoAndEmitsCustomerMessage() async throws {
        let chat = makeChat(seq: TransportSequence([EchoTransport()]))
        let messages = chat.events.messages.makeStream()
        try await chat.connect()
        try await awaitConnected(chat)
        try await chat.sendMessage("你好")

        let msg = try await nextMatching(messages, where: { $0.sender == .customer })
        #expect(msg.content == "你好")
        #expect(chat.events.connectionState.value == .connected)
    }

    @Test func sendMessageTimesOutWithoutEchoAndEmitsSendTimeout() async throws {
        let silent = MockTransport()
        let chat = makeChat(seq: TransportSequence([silent]), echoTimeoutMs: 250)
        let errors = chat.events.error.makeStream()
        try await chat.connect()
        silent.emitOpen() // 空 listener：升级即连（镜像 withWebSocketUpgrade(object :) {})
        try await awaitConnected(chat)
        try await chat.sendMessage("无人回显")

        let error = try await nextMatching(errors)
        guard case .sendTimeout = error else {
            Issue.record("expected sendTimeout, got \(error)")
            return
        }
        #expect(error.retryable)
    }

    @Test func sendMessageWithoutConnectionEmitsNetworkError() async throws {
        // 惰性连接语义：未 connect 直接 send → 立即 network 错误，不挂起不伪造成功
        let chat = makeChat()
        let errors = chat.events.error.makeStream()

        await chat.sendMessage("未连接时发送")

        let error = try await nextMatching(errors)
        guard case .network = error else {
            Issue.record("expected network, got \(error)")
            return
        }
        #expect(chat.events.connectionState.value == .idle)
    }

    @Test func sendRejectedEmitsNetworkErrorWithoutSendTimeout() async throws {
        // 已 closing 套接字的 send=false：清理 pendingEcho + network 错误（不进回显等待）
        let chat = makeChat(seq: TransportSequence([SendRejectingTransport()]))
        let errors = chat.events.error.makeStream()
        try await chat.connect()
        try await awaitConnected(chat)

        await chat.sendMessage("发送被拒")

        let error = try await nextMatching(errors)
        guard case .network = error else {
            Issue.record("expected network, got \(error)")
            return
        }
    }

    @Test func webrtcAndUnknownFramesAreContractIgnoredFacade() async throws {
        // 门面层的契约忽略（PROTOCOL §3/§6.4）：webrtc 信令族与未知帧不产生消息/错误/
        // 状态扰动，且不影响后续正常帧处理（证明非连接性故障被静默吞掉）。
        // Kotlin 镜像：ServifyChatTest.webrtcAndUnknownFramesAreContractIgnoredFacade
        let scripted = ScriptedTransport()
        let chat = makeChat(seq: TransportSequence([scripted]))
        let messages = chat.events.messages.makeStream()
        try await chat.connect()
        try await awaitConnected(chat)

        scripted.emitMessage(TestFrames.webrtcOffer())
        scripted.emitMessage(TestFrames.unknownKind())

        // 契约忽略后连接与收发链路完好：正常帧照常驱动
        scripted.emitMessage(TestFrames.agentMessage("坐席A"))
        let msg = try await nextMatching(messages, where: { $0.sender == .agent })
        #expect(msg.content == "坐席A")
        #expect(chat.historySnapshot().count == 1)
        #expect(chat.events.connectionState.value == .connected)
    }

    // MARK: - 握手失败

    @Test func handshake500MarksServerUnavailableAndDisconnected() async throws {
        let chat = makeChat(seq: TransportSequence([HandshakeRejectTransport(status: 500)]))
        let errors = chat.events.error.makeStream()
        let states = chat.events.connectionState.makeStream()
        try await chat.connect()

        let error = try await nextMatching(errors)
        guard case .serverUnavailable = error else {
            Issue.record("expected serverUnavailable, got \(error)")
            return
        }
        let state = try await nextMatching(states, where: { $0 == .disconnected })
        #expect(state == .disconnected)
    }

    // MARK: - 流式拼装

    @Test func deltaStreamAssemblesBubblesThenFinalCarriesSources() async throws {
        let plain = MockTransport()
        let chat = makeChat(seq: TransportSequence([plain]))
        let streamed = chat.events.messages.makeStream()
        let final = chat.events.messages.makeStream()
        try await chat.connect()

        plain.emitOpen()
        plain.emitMessage(TestFrames.delta("根据", done: false))
        plain.emitMessage(TestFrames.delta("退货政策。", done: false))
        plain.emitMessage(TestFrames.aiFinal(content: "根据退货政策。", confidence: 0.87, source: "knowledge", sessionId: chat.sessionId))

        let streaming = try await nextMatching(streamed, where: { $0.isStreaming })
        #expect(streaming.content == "根据")
        #expect(streaming.sender == .system)

        let fin = try await nextMatching(final, where: { $0.isAiResponse && !$0.isStreaming })
        #expect(fin.id == streaming.id)
        #expect(fin.content == "根据退货政策。")
        #expect(fin.confidence == 0.87)
        #expect(fin.sources.count == 1)
        #expect(fin.sources.first?.documentId == "d1")
    }

    // MARK: - 未读语义

    @Test func agentMessagesDriveUnreadAndVisibleClearsIt() async throws {
        let plain = MockTransport()
        let chat = makeChat(seq: TransportSequence([plain]))
        let second = chat.events.messages.makeStream()
        try await chat.connect()

        plain.emitOpen()
        plain.emitMessage(TestFrames.agentMessage("第一条"))
        plain.emitMessage(TestFrames.agentMessage("第二条"))

        _ = try await nextMatching(second, where: { $0.content == "第二条" })
        #expect(chat.events.unreadCount.value == 2)

        chat.onSessionVisible()
        #expect(chat.events.unreadCount.value == 0)
    }

    // MARK: - 转人工 / 等待队列

    @Test func transferNotificationEmitsAgentAssignment() async throws {
        let plain = MockTransport()
        let chat = makeChat(seq: TransportSequence([plain]))
        let assignments = chat.events.agentAssigned.makeStream()
        try await chat.connect()

        plain.emitOpen()
        plain.emitMessage(TestFrames.transfer(agentId: 42, message: "已为您转接人工客服"))

        let assignment = try await nextMatching(assignments)
        #expect(assignment == AgentAssignment(agentId: 42, message: "已为您转接人工客服"))
    }

    @Test func waitingNotificationEmitsQueueEvent() async throws {
        let plain = MockTransport()
        let chat = makeChat(seq: TransportSequence([plain]))
        let waiting = chat.events.waitingInQueue.makeStream()
        try await chat.connect()

        plain.emitOpen()
        plain.emitMessage(TestFrames.waiting("前方还有 1 位等待"))

        let message = try await nextMatching(waiting)
        #expect(message == "前方还有 1 位等待")
    }

    // MARK: - 断线重连

    @Test func reconnectsAfterServerDrop() async throws {
        // 发送触发服务端断线（DropOnMessageTransport 等价 Kotlin onMessage 内 cancel()，
        // 唯一可靠断开手法）：onFailure → §4.4 connected ─断→ reconnecting(n) 自动恢复。
        let chat = makeChat(seq: TransportSequence([DropOnMessageTransport(), EchoTransport()]))
        try await chat.connect()
        try await awaitConnected(chat)

        // reconnecting 是 EventStream（bufferingNewest(256)），emit 不被后续状态挤掉。
        let reconnectingEvents = chat.events.reconnecting.makeStream()
        try await chat.sendMessage("触发服务端断开") // 回显不来，发送侧按 SendTimeout 超时收尾

        let attempt = try await nextMatching(reconnectingEvents)
        #expect(attempt == 1)
        try await awaitConnected(chat)
        #expect(chat.events.connectionState.value == .connected)
    }

    // MARK: - 销毁

    @Test func destroyIsIdempotentAndMarksDisconnected() async throws {
        let chat = makeChat(seq: TransportSequence([EchoTransport()]))
        try await chat.connect()
        try await awaitConnected(chat)

        chat.destroy()
        chat.destroy()
        #expect(chat.events.connectionState.value == .disconnected)
    }

    // MARK: - 刀 4：会话连续性（累积/未读可见性/流中断收口）与 guestToken 握手

    @Test func buildWsUrlCarriesAccessTokenOnlyWhenConfigured() throws {
        let chat = makeChat()
        let base = chat.buildWsUrl("https://chat.example.com", nil, nil)
        #expect(base.hasPrefix("wss://chat.example.com/api/v1/ws?"))
        #expect(base.contains("session_id=test-session"))
        #expect(!base.contains("access_token"))

        // PROTOCOL §1：access_token 现阶段服务端不读取，端点落地后自动生效；特殊字符须编码
        // （URLEncoder form 语义：空格→+，&/=→%XX——Go 服务端 r.URL.Query() 按 form 解 + 为空格）。
        let withToken = chat.buildWsUrl("https://chat.example.com", nil, "tok en&x=1")
        #expect(withToken.contains("access_token=tok+en%26x%3D1"))

        let override = "ws://127.0.0.1:1/ws"
        #expect(chat.buildWsUrl("https://chat.example.com", override, nil) == override)
        #expect(
            chat.buildWsUrl("https://chat.example.com", override, "tok en&x=1")
                == "\(override)?access_token=tok+en%26x%3D1"
        )

        // wss:// 直通分支：已是 ws scheme 时原样使用（不再加 wss:// 前缀）
        #expect(chat.buildWsUrl("wss://chat.example.com", nil, nil).hasPrefix("wss://chat.example.com/api/v1/ws?"))
    }

    @Test func historySnapshotAccumulatesAndMergesStreamById() async throws {
        let chat = makeChat(seq: TransportSequence([ScriptedTransport()]), echoTimeoutMs: 100)
        try await chat.connect()
        try await awaitConnected(chat)

        try await chat.sendMessage("触发1") // listener → 坐席帧
        try await chat.sendMessage("触发AI") // listener → 流式+终帧

        try await awaitSnapshot(chat) { snapshot in
            snapshot.contains { $0.isAiResponse && !$0.isStreaming }
        }
        let snapshot = chat.historySnapshot()
        // 坐席 + AI 终帧两条；delta 中间态同 id 覆盖不重复累积。
        #expect(snapshot.count == 2)
        #expect(snapshot[0].sender == .agent)
        #expect(snapshot[1].content == "根据政策。")
        #expect(snapshot[1].isAiResponse)
        #expect(!snapshot[1].isStreaming)
    }

    @Test func unreadCountsOnlyWhileSessionHidden() async throws {
        let chat = makeChat(seq: TransportSequence([ScriptedTransport()]), echoTimeoutMs: 100)
        try await chat.connect()
        try await awaitConnected(chat)

        // 可见期：坐席消息不计未读。
        chat.onSessionVisible()
        try await chat.sendMessage("一")
        try await awaitSnapshot(chat) { $0.contains { $0.sender == .agent } }
        #expect(chat.events.unreadCount.value == 0)

        // 收起后：AI 流式只按终帧计 1（同 id 不重复），坐席消息再计 1。
        chat.onSessionHidden()
        try await chat.sendMessage("二")
        try await awaitSnapshot(chat) { $0.contains { $0.isAiResponse && !$0.isStreaming } }
        #expect(chat.events.unreadCount.value == 1)

        try await chat.sendMessage("三")
        try await awaitSnapshot(chat) { $0.filter { $0.sender == .agent }.count >= 2 }
        #expect(chat.events.unreadCount.value == 2)

        chat.onSessionVisible()
        #expect(chat.events.unreadCount.value == 0)
    }

    @Test func streamInterruptionOnDisconnectFinalizesPartialWithHint() async throws {
        // 断线即流中断（PROTOCOL §4.1）：保留已渲染 + 提示行；提示行不计未读。
        // delta 与断开拆两条消息时序驱动（对齐 Kotlin 的 onMessage 内同步断开）。
        final class DeltaThenDropTransport: MockTransport {
            private var count = 0

            override init() {
                super.init()
                autoOpen = true
            }

            override func send(_ text: String) -> Bool {
                _ = super.send(text)
                count += 1
                if count == 1 {
                    emitMessage(TestFrames.delta("根据", done: false))
                    emitMessage(TestFrames.delta("退货政策。", done: false))
                } else {
                    emitFailure(TransportLost())
                }
                return true
            }
        }

        let chat = makeChat(seq: TransportSequence([DeltaThenDropTransport()]), echoTimeoutMs: 100)
        try await chat.connect()
        try await awaitConnected(chat)

        try await chat.sendMessage("触发流式") // listener → delta×2（streamingId 挂上）
        try await chat.sendMessage("触发断开") // listener → 同步断开 → onFailure → finalize

        try await awaitSnapshot(chat) { snapshot in
            snapshot.contains { $0.content == "回答中断，请重试" }
        }
        let snapshot = chat.historySnapshot()
        let partial = snapshot.first { $0.isAiResponse }
        #expect(partial?.content == "根据退货政策。")
        #expect(partial?.isStreaming == false)
        #expect(chat.events.unreadCount.value == 0)
    }
}
