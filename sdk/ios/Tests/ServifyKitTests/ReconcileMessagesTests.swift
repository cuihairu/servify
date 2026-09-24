import Foundation
import Testing

@testable import ServifyKit

/// 断线补拉（D7 流程 3；§10 #1 端点）：连接成功后 GET
/// /api/v1/sessions/{id}/messages?after_id=<游标> 增量对账——合并 history、
/// agent/ai 计未读（面板不可见时）、has_more 续拉、全失败面静默、指纹去重。
/// Kotlin 镜像：ReconcileMessagesTest.kt——用例名逐一对应防单侧漂移。
///
/// 纯 REST 用例直调 await chat.reconcileMessages()（MockTicketHTTP 队列响应）；
/// 指纹去重走 WS 集成用例（指纹只在 WS 渲染点积累，生产路径是
/// "WS 渲染 → 断线 → 重连补拉拉回同批"）。
struct ReconcileMessagesTests {

    private func makeChat(
        http: MockTicketHTTP,
        transportFactory: @escaping () -> WebSocketTransport
    ) -> ServifyChat {
        ServifyChat(
            config: try! ServifyConfig(apiUrl: "https://chat.example.com"),
            sessionId: "test-session",
            policy: try! ReconnectPolicy(maxAttempts: 3, initialDelayMs: 50, multiplier: 2, maxDelayMs: 100),
            echoTimeoutMs: 200,
            wsUrlOverride: "ws://mock.test/api/v1/ws",
            messagesUrlOverride: "https://mock.test/api/v1/sessions/test-session/messages",
            ticketHTTP: http,
            transportFactory: transportFactory
        )
    }

    /// 服务端补拉页 JSON（ConversationMessageDTO 数组 + has_more）。
    private func page(_ messages: String..., hasMore: Bool = false) -> Data {
        let items = messages.joined(separator: ",")
        return Data("{\"messages\":[\(items)],\"has_more\":\(hasMore)}".utf8)
    }

    private func msg(id: String, sender: String, content: String) -> String {
        "{\"id\":\"\(id)\",\"conversation_id\":\"test-session\",\"sender\":\"\(sender)\",\"kind\":\"text\",\"content\":\"\(content)\",\"created_at\":\"2026-09-24T12:00:00Z\"}"
    }

    private func contents(_ history: [ConversationMessage]) -> [String] {
        history.map(\.content)
    }

    @Test func mergesHistoryAndCountsUnreadForAgentAndAi() async {
        let http = MockTicketHTTP()
        http.enqueueGet(.success((200, page(
            msg(id: "1", sender: "customer", content: "在吗"),
            msg(id: "2", sender: "agent", content: "您好，我是客服"),
            msg(id: "3", sender: "system", content: "会话已建立"),
            msg(id: "4", sender: "ai", content: "猜你想问：如何重置密码")
        ))))
        let chat = makeChat(http: http) { MockTransport() }

        await chat.reconcileMessages()

        // 升序合并：四种来源全部进 history（customer 断线前的本地份指纹命中时才跳过）
        #expect(contents(chat.historySnapshot()) == [
            "在吗", "您好，我是客服", "会话已建立", "猜你想问：如何重置密码",
        ])
        #expect(chat.historySnapshot()[0].sender == .customer)
        #expect(chat.historySnapshot()[1].sender == .agent)
        #expect(chat.historySnapshot()[2].sender == .system)
        // ai 映射为 System + isAiResponse（与 WS 帧口径同构）
        let ai = chat.historySnapshot()[3]
        #expect(ai.sender == .system)
        #expect(ai.isAiResponse)
        // 未读：agent/ai 计（面板不可见），customer/system 不计
        #expect(chat.events.unreadCount.value == 2)
        #expect(http.gotURL == "https://mock.test/api/v1/sessions/test-session/messages?limit=100")
    }

    @Test func paginatesOnHasMoreWithAfterIdCursor() async {
        let http = MockTicketHTTP()
        http.enqueueGet(.success((200, page(msg(id: "5", sender: "agent", content: "第一页"), hasMore: true))))
        http.enqueueGet(.success((200, page(msg(id: "9", sender: "agent", content: "第二页")))))
        let chat = makeChat(http: http) { MockTransport() }

        await chat.reconcileMessages()

        #expect(contents(chat.historySnapshot()) == ["第一页", "第二页"])
        #expect(chat.events.unreadCount.value == 2)
        #expect(http.gotURLs.count == 2)
        #expect(http.gotURLs[1].contains("after_id=5"), "续拉应以末条 id 为游标：\(http.gotURLs[1])")
    }

    @Test func cursorAdvancesMonotonicallyWithinPage() async {
        let http = MockTicketHTTP()
        // 页内乱序（升序契约外的防御面）：游标取 max 而非首条
        http.enqueueGet(.success((200, page(
            msg(id: "7", sender: "agent", content: "后到的"),
            msg(id: "3", sender: "agent", content: "先落的"),
            hasMore: true
        ))))
        http.enqueueGet(.success((200, page())))
        let chat = makeChat(http: http) { MockTransport() }

        await chat.reconcileMessages()

        #expect(contents(chat.historySnapshot()) == ["后到的", "先落的"])
        #expect(http.gotURLs.count == 2)
        #expect(http.gotURLs[1].contains("after_id=7"), "游标应推进到页内最大 id：\(http.gotURLs[1])")
    }

    @Test func skipsMalformedEntries() async {
        let http = MockTicketHTTP()
        http.enqueueGet(.success((200, Data("""
        {"messages":["不是对象",{"conversation_id":"x"},{"id":"abc"},\(msg(id: "12", sender: "agent", content: "合法"))],"has_more":false}
        """.utf8))))
        let chat = makeChat(http: http) { MockTransport() }

        await chat.reconcileMessages()

        #expect(contents(chat.historySnapshot()) == ["合法"])
        #expect(chat.events.unreadCount.value == 1)
    }

    @Test func silentWhenSessionMissing() async {
        let http = MockTicketHTTP()
        http.enqueueGet(.success((404, Data("{\"error\":\"session not found\"}".utf8))))
        let chat = makeChat(http: http) { MockTransport() }

        await chat.reconcileMessages()

        #expect(chat.historySnapshot().isEmpty, "404 = 会话行未建过，无历史可拉")
        #expect(chat.events.unreadCount.value == 0)
    }

    @Test func silentOnHttpError() async {
        let http = MockTicketHTTP()
        http.enqueueGet(.success((500, Data("{\"error\":\"boom\"}".utf8))))
        let chat = makeChat(http: http) { MockTransport() }

        await chat.reconcileMessages()

        #expect(chat.historySnapshot().isEmpty)
        #expect(chat.events.unreadCount.value == 0)
    }

    @Test func silentOnTransportFailure() async {
        let http = MockTicketHTTP()
        http.enqueueGet(.failure(TicketHTTPFailure()))
        let chat = makeChat(http: http) { MockTransport() }

        await chat.reconcileMessages()

        #expect(chat.historySnapshot().isEmpty)
        #expect(chat.events.unreadCount.value == 0)
    }

    @Test func silentOnMalformedBody() async {
        let http = MockTicketHTTP()
        http.enqueueGet(.success((200, Data("not-json".utf8))))
        let chat = makeChat(http: http) { MockTransport() }

        await chat.reconcileMessages()

        #expect(chat.historySnapshot().isEmpty)
        #expect(chat.events.unreadCount.value == 0)
    }

    @Test func silentWhenMessagesFieldMissing() async {
        let http = MockTicketHTTP()
        http.enqueueGet(.success((200, Data("{\"foo\":1}".utf8))))
        let chat = makeChat(http: http) { MockTransport() }

        await chat.reconcileMessages()

        #expect(chat.historySnapshot().isEmpty)
        #expect(chat.events.unreadCount.value == 0)
    }

    /// 指纹去重的生产路径（WS 集成）：首连 WS 渲染坐席帧（指纹入表）→ 断线重连
    /// → 补拉拉回同一消息。游标此时仍未确立（WS 帧无服务端 ID、空页不推进），
    /// 去重只能靠指纹——命中条跳过渲染与未读，同批新消息正常入列。
    @Test func fingerprintSkipsRenderedMessagesOnReconcile() async throws {
        let http = MockTicketHTTP()
        http.enqueueGet(.success((200, page()))) // 首连 reconcile：空页
        http.enqueueGet(.success((200, page(
            msg(id: "5", sender: "agent", content: "断线期间的消息"), // 指纹命中 → 跳过
            msg(id: "6", sender: "agent", content: "重连后的新消息") // 正常入列
        ))))
        let first = MockTransport()
        first.autoOpen = true
        let second = MockTransport()
        second.autoOpen = true
        let chat = makeChat(http: http) { [first, second] in
            // 两次 connect 依序出队（镜像 TransportSequence，两连接足额）
            first.listener == nil ? first : second
        }

        await chat.connect()
        // 首连 onOpen 即补拉（空页）；驱动服务端坐席帧渲染（指纹入表）
        first.emitMessage(TestFrames.agentMessage("断线期间的消息"))
        try await awaitSnapshot(chat) { $0.contains { $0.content == "断线期间的消息" } }
        #expect(chat.events.unreadCount.value == 1)
        first.emitFailure(TransportLost())

        // 重连 onOpen 触发补拉；等 REST#2 渲染完成（新消息入列）
        try await awaitSnapshot(chat) { $0.contains { $0.content == "重连后的新消息" } }
        #expect(chat.historySnapshot().filter { $0.content == "断线期间的消息" }.count == 1,
                "指纹命中的同内容消息不得重复渲染")
        #expect(chat.events.unreadCount.value == 2, "只有新消息计入未读（+1）")

        // 重连补拉不带游标（未确立，全量拉取由指纹去重）
        #expect(http.gotURLs.count == 2)
        #expect(!http.gotURLs[1].contains("after_id"), "游标未确立时全量拉取：\(http.gotURLs[1])")
    }
}
