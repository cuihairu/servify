import Foundation
import Testing

@testable import ServifyKit

// MARK: - MockTicketHTTP（Kotlin 侧 MockWebServer 的镜像：记录请求、回放响应）

private struct TicketHTTPFailure: Error {}

private final class MockTicketHTTP: TicketHTTPPosting, @unchecked Sendable {
    private(set) var postedURL: String?
    private(set) var postedBody: Data?

    /// 队列化响应：先入先出；Error 抛出、(status, json) 正常返回。空队列默认 201+id。
    private var queue: [Result<(status: Int, data: Data), Error>] = []

    func enqueue(_ result: Result<(status: Int, data: Data), Error>) {
        queue.append(result)
    }

    func post(url: String, body: Data) async throws -> (status: Int, data: Data) {
        postedURL = url
        postedBody = body
        guard let result = queue.isEmpty ? nil : queue.removeFirst() else {
            return (201, Data("{}".utf8))
        }
        switch result {
        case let .success(pair): return pair
        case let .failure(error): throw error
        }
    }
}

// MARK: - 摘要纯逻辑（Kotlin 镜像：TicketSummaryTest.kt——用例名逐一对应）

/// 工单 AI 摘要预填纯逻辑（M3 刀 3a）：发言方标注、System 提示行跳过、最近 N 条截断。
struct TicketSummaryTests {

    private func msg(
        id: String,
        sender: SenderType,
        content: String,
        isAiResponse: Bool = false
    ) -> ConversationMessage {
        ConversationMessage(
            id: id,
            sessionId: "s",
            sender: sender,
            content: content,
            createdAt: 0,
            isAiResponse: isAiResponse
        )
    }

    @Test func labelsVisitorAiAndAgentTurns() {
        let summary = TicketSummary.build(messages: [
            msg(id: "1", sender: .customer, content: "打不开页面"),
            // 门面里 AI 回答是 system sender + isAiResponse 标志
            msg(id: "2", sender: .system, content: "已为您排查缓存问题", isAiResponse: true),
            msg(id: "3", sender: .agent, content: "请提供订单号"),
        ])
        #expect(summary == "[访客] 打不开页面\n[AI] 已为您排查缓存问题\n[客服] 请提供订单号")
    }

    @Test func skipsSystemHintLines() {
        // 流中断/离线提示（system 无 AI 标志）不进摘要。
        let summary = TicketSummary.build(messages: [
            msg(id: "1", sender: .customer, content: "咨询"),
            msg(id: "2", sender: .system, content: "回答中断，请重试"),
            msg(id: "3", sender: .system, content: "客服当前不在线"),
        ])
        #expect(summary == "[访客] 咨询")
    }

    @Test func returnsNullForNoUsableConversation() {
        #expect(TicketSummary.build(messages: []) == nil)
        #expect(TicketSummary.build(messages: [
            msg(id: "1", sender: .system, content: "回答中断，请重试"),
        ]) == nil)
    }

    @Test func keepsOnlyLastMaxTurns() {
        let messages = (1...15).map { msg(id: "\($0)", sender: .customer, content: "消息\($0)") }
        let summary = TicketSummary.build(messages: messages, maxTurns: 3)
        #expect(summary == "[访客] 消息13\n[访客] 消息14\n[访客] 消息15")
    }
}

// MARK: - 门面 createTicket（Kotlin 镜像：CreateTicketTest.kt——用例名逐一对应）

/// 会话页工单创建（M3 刀 3a）：请求组装（session_id/title/ai_summary 摘录）与
/// 结果语义（2xx→回执、非 2xx/IO/畸形→nil + 错误流）。
struct CreateTicketTests {

    private func makeChat(http: MockTicketHTTP, transport: MockTransport) -> ServifyChat {
        ServifyChat(
            config: try! ServifyConfig(apiUrl: "https://chat.example.com"),
            sessionId: "test-session",
            echoTimeoutMs: 200,
            wsUrlOverride: "ws://mock.test/api/v1/ws",
            ticketUrlOverride: "https://mock.test/api/v1/tickets",
            ticketHTTP: http,
            transportFactory: { transport }
        )
    }

    /// 经真实 WS 帧注入一条坐席消息，让门面 history 有内容可摘。
    private func seedHistoryViaAgentMessage(_ chat: ServifyChat, transport: MockTransport) async throws {
        transport.autoOpen = true
        let messages = chat.events.messages.makeStream()
        try await chat.connect()
        transport.emitMessage(
            #"{"type":"agent-message","data":{"content":"您好，请问遇到什么问题","sender":"agent"}}"#
        )
        _ = try await nextMatching(messages, where: { $0.sender == .agent })
    }

    private func ticketRequestBody(_ http: MockTicketHTTP) throws -> [String: Any] {
        let data = try #require(http.postedBody)
        guard let object = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            Issue.record("ticket body is not a JSON object")
            return [:]
        }
        return object
    }

    @Test func createTicketPostsRequestAndParsesReceipt() async throws {
        let http = MockTicketHTTP()
        http.enqueue(.success((201, Data(#"{"id":42,"status":"open"}"#.utf8))))
        let transport = MockTransport()
        let chat = makeChat(http: http, transport: transport)
        try await seedHistoryViaAgentMessage(chat, transport: transport)

        let receipt = await chat.createTicket(title: "打不开页面", description: "按钮无响应")

        #expect(receipt?.ticketId == 42)
        #expect(http.postedURL == "https://mock.test/api/v1/tickets")
        let body = try ticketRequestBody(http)
        #expect(body["session_id"] as? String == "test-session")
        #expect(body["title"] as? String == "打不开页面")
        #expect(body["description"] as? String == "按钮无响应")
        // ai_summary 摘录了会话中的坐席消息（坐席侧直接可读）
        #expect(body["ai_summary"] as? String == "[客服] 您好，请问遇到什么问题")
    }

    @Test func createTicketOmitsSummaryAndDescriptionWhenAbsent() async throws {
        let http = MockTicketHTTP()
        http.enqueue(.success((201, Data(#"{"id":7}"#.utf8))))
        let chat = makeChat(http: http, transport: MockTransport())

        let receipt = await chat.createTicket(title: "咨询")

        #expect(receipt?.ticketId == 7)
        let body = try ticketRequestBody(http)
        #expect(body["ai_summary"] == nil)
        #expect(body["description"] == nil)
    }

    @Test func createTicketReturnsNilAndEmitsTicketFailedOnHttpError() async throws {
        let http = MockTicketHTTP()
        http.enqueue(.success((404, Data(#"{"error":"Session not found"}"#.utf8))))
        let chat = makeChat(http: http, transport: MockTransport())
        let errors = chat.events.error.makeStream()

        let receipt = await chat.createTicket(title: "打不开页面")

        #expect(receipt == nil)
        let error = try await nextMatching(errors, where: { $0.code == "ticket_failed" })
        #expect(error.code == "ticket_failed")
    }

    @Test func createTicketReturnsNilAndEmitsNetworkOnIoFailure() async throws {
        let http = MockTicketHTTP()
        http.enqueue(.failure(TicketHTTPFailure()))
        let chat = makeChat(http: http, transport: MockTransport())
        let errors = chat.events.error.makeStream()

        let receipt = await chat.createTicket(title: "打不开页面")

        #expect(receipt == nil)
        let error = try await nextMatching(errors, where: { $0.code == "network" })
        #expect(error.code == "network")
    }

    @Test func createTicketReturnsNilAndEmitsTicketFailedOnMalformedBody() async throws {
        let http = MockTicketHTTP()
        http.enqueue(.success((201, Data(#"{"status":"open"}"#.utf8))))
        let chat = makeChat(http: http, transport: MockTransport())
        let errors = chat.events.error.makeStream()

        let receipt = await chat.createTicket(title: "打不开页面")

        #expect(receipt == nil)
        let error = try await nextMatching(errors, where: { $0.code == "ticket_failed" })
        #expect(error.code == "ticket_failed")
    }

    @Test func createTicketDerivesUrlFromApiUrlTrimmingTrailingSlash() async throws {
        // 无 override 的生产路径：URL 由 apiUrl 派生（尾斜杠修剪 + /api/v1/tickets 拼接）
        let http = MockTicketHTTP()
        http.enqueue(.success((201, Data(#"{"id":9}"#.utf8))))
        let transport = MockTransport()
        let chat = ServifyChat(
            config: try! ServifyConfig(apiUrl: "https://chat.example.com/"),
            sessionId: "test-session",
            echoTimeoutMs: 200,
            wsUrlOverride: "ws://mock.test/api/v1/ws",
            ticketHTTP: http,
            transportFactory: { transport }
        )

        let receipt = await chat.createTicket(title: "咨询")

        #expect(receipt?.ticketId == 9)
        #expect(http.postedURL == "https://chat.example.com/api/v1/tickets")
    }
}
