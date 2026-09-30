import Foundation
import Testing

@testable import ServifyKit

/// 服务端已读游标提交（D7 流程 3 服务端增强面；§10 #3 POST /read）：
/// 无已确认消息（未对账）→ 静默 false 且零请求；有游标 → POST
/// /api/v1/sessions/{id}/read 体 {"last_read_message_id":"<id>"}（2xx→true；
/// IO/HTTP 失败→network + false）。响应体不消费——本地未读推导语义不被
/// 服务端回显改写。Kotlin 镜像：SyncReadStateTest.kt——用例名逐一对应防单侧漂移。
///
/// 游标种子走补拉链（messagesUrlOverride 回放一页含服务端 ID 的消息后直调
/// internal reconcileMessages()，与生产 onOpen 对账同路径）。
private final class CapturedError: @unchecked Sendable {
    private let lock = NSLock()
    private var value: ServifyError?
    func set(_ error: ServifyError) {
        lock.lock()
        value = error
        lock.unlock()
    }

    func read() -> ServifyError? {
        lock.lock()
        defer { lock.unlock() }
        return value
    }
}

struct SyncReadStateTests {

    private func makeChat(
        http: MockTicketHTTP,
        apiUrl: String = "https://chat.example.com",
        readUrlOverride: String? = "https://mock.test/api/v1/sessions/test-session/read"
    ) -> ServifyChat {
        ServifyChat(
            config: try! ServifyConfig(apiUrl: apiUrl),
            sessionId: "test-session",
            policy: try! ReconnectPolicy(maxAttempts: 3, initialDelayMs: 50, multiplier: 2, maxDelayMs: 100),
            echoTimeoutMs: 200,
            wsUrlOverride: "ws://mock.test/api/v1/ws",
            messagesUrlOverride: "https://mock.test/api/v1/sessions/test-session/messages",
            readUrlOverride: readUrlOverride,
            ticketHTTP: http,
            transportFactory: { MockTransport() }
        )
    }

    /// 游标种子页：补拉链回放一页含服务端 ID 的坐席消息（直调 reconcileMessages() 消费）。
    private func seed(id: String = "7") -> Data {
        Data(
            "{\"messages\":[{\"id\":\"\(id)\",\"conversation_id\":\"test-session\",\"sender\":\"agent\",\"kind\":\"text\",\"content\":\"您好\",\"created_at\":\"2026-09-30T12:00:00Z\"}],\"has_more\":false}".utf8
        )
    }

    /// 后台收集第一条错误（"静默"断言用——EventStream 只能 await，无当前值查询）。
    private func captureFirstError(_ chat: ServifyChat) -> (CapturedError, Task<Void, Never>) {
        let box = CapturedError()
        let errors = chat.events.error.makeStream()
        let watcher = Task {
            for await error in errors {
                box.set(error)
                break
            }
        }
        return (box, watcher)
    }

    /// watcher 跨线程调度：轮询等 box 就位（最多 5s）。
    private func awaitCaptured(_ box: CapturedError) async -> ServifyError? {
        for _ in 0..<500 {
            if let error = box.read() { return error }
            try? await Task.sleep(nanoseconds: 10_000_000)
        }
        return box.read()
    }

    private func readRequestBody(_ http: MockTicketHTTP) throws -> [String: Any] {
        let data = try #require(http.postedBody)
        guard let object = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            Issue.record("read cursor body is not a JSON object")
            return [:]
        }
        return object
    }

    @Test func postsCursorAndReturnsTrueOnOk() async throws {
        let http = MockTicketHTTP()
        let chat = makeChat(http: http)
        http.enqueueGet(.success((200, seed())))
        await chat.reconcileMessages()
        http.enqueue(.success((200, Data("{\"unread_count\":0,\"last_read_message_id\":\"7\"}".utf8))))

        let result = await chat.syncReadState()

        #expect(result)
        #expect(http.postedURL == "https://mock.test/api/v1/sessions/test-session/read")
        let body = try readRequestBody(http)
        #expect(body["last_read_message_id"] as? String == "7")
    }

    @Test func silentFalseWithoutCursor() async {
        let http = MockTicketHTTP()
        let chat = makeChat(http: http)
        let (box, watcher) = captureFirstError(chat)

        let result = await chat.syncReadState()

        #expect(!result)
        #expect(http.postedURL == nil, "无游标不应发出任何请求")
        try? await Task.sleep(nanoseconds: 50_000_000)
        #expect(box.read() == nil, "未对账过（无游标）是正常态，不应产生错误")
        watcher.cancel()
    }

    @Test func derivesReadUrlFromApiUrlWhenNoOverride() async throws {
        // 生产路径：无 override 时 read URL 由 apiUrl 派生（尾斜杠修剪 + /api/v1/sessions/{id}/read）。
        let http = MockTicketHTTP()
        let chat = makeChat(http: http, apiUrl: "https://chat.example.com/", readUrlOverride: nil)
        http.enqueueGet(.success((200, seed())))
        await chat.reconcileMessages()

        let result = await chat.syncReadState()

        #expect(result)
        #expect(http.postedURL == "https://chat.example.com/api/v1/sessions/test-session/read")
    }

    @Test func emitsNetworkAndFalseOnHttpError() async {
        let http = MockTicketHTTP()
        let chat = makeChat(http: http)
        http.enqueueGet(.success((200, seed())))
        await chat.reconcileMessages()
        http.enqueue(.success((500, Data("{}".utf8))))
        let (box, watcher) = captureFirstError(chat)

        let result = await chat.syncReadState()

        #expect(!result)
        let error = await awaitCaptured(box)
        #expect(error?.code == "network")
        #expect(error?.message.contains("read cursor sync http 500") == true)
        watcher.cancel()
    }

    @Test func emitsNetworkAndFalseOnNotFound() async {
        let http = MockTicketHTTP()
        let chat = makeChat(http: http)
        http.enqueueGet(.success((200, seed())))
        await chat.reconcileMessages()
        http.enqueue(.success((404, Data("{}".utf8))))
        let (box, watcher) = captureFirstError(chat)

        let result = await chat.syncReadState()

        #expect(!result)
        let error = await awaitCaptured(box)
        #expect(error?.code == "network")
        #expect(error?.message.contains("read cursor sync http 404") == true)
        watcher.cancel()
    }

    @Test func emitsNetworkAndFalseOnIoFailure() async {
        let http = MockTicketHTTP()
        let chat = makeChat(http: http)
        http.enqueueGet(.success((200, seed())))
        await chat.reconcileMessages()
        http.enqueue(.failure(TicketHTTPFailure()))
        let (box, watcher) = captureFirstError(chat)

        let result = await chat.syncReadState()

        #expect(!result)
        let error = await awaitCaptured(box)
        #expect(error?.code == "network")
        #expect(error?.message.contains("read cursor sync failed") == true)
        watcher.cancel()
    }
}
