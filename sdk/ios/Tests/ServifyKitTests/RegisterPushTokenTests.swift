import Foundation
import Testing

@testable import ServifyKit

/// 推送注册口（M3，平台规格 §4 registerPushToken；§10 #5 注册端点）：
/// 未配置→unsupported；配置+无 token→静默；配置+有 token→POST
/// /api/v1/push/register（2xx→true；IO/HTTP 失败→network + false）。
/// Kotlin 镜像：RegisterPushTokenTest.kt——用例名逐一对应防单侧漂移。
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

struct RegisterPushTokenTests {

    private func makeChat(
        pushTokenProvider: (@Sendable () async -> String?)?,
        http: MockTicketHTTP? = nil,
        pushUrlOverride: String? = nil,
        apiUrl: String = "https://chat.example.com"
    ) -> ServifyChat {
        ServifyChat(
            config: try! ServifyConfig(apiUrl: apiUrl, pushTokenProvider: pushTokenProvider),
            sessionId: "test-session",
            echoTimeoutMs: 200,
            wsUrlOverride: "ws://mock.test/api/v1/ws",
            pushUrlOverride: pushUrlOverride,
            ticketHTTP: http,
            transportFactory: { MockTransport() }
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

    private func pushRequestBody(_ http: MockTicketHTTP) throws -> [String: Any] {
        let data = try #require(http.postedBody)
        guard let object = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            Issue.record("push register body is not a JSON object")
            return [:]
        }
        return object
    }

    @Test func emitsUnsupportedWhenNotConfigured() async throws {
        let chat = makeChat(pushTokenProvider: nil)
        let (box, watcher) = captureFirstError(chat)

        let result = await chat.registerPushToken()

        #expect(result == false)
        let error = await awaitCaptured(box)
        #expect(error?.code == "unsupported")
        #expect(error?.message == "push not configured")
        watcher.cancel()
    }

    @Test func silentWhenProviderReturnsNil() async {
        let chat = makeChat(pushTokenProvider: { nil })
        let (box, watcher) = captureFirstError(chat)

        let result = await chat.registerPushToken()

        // 无 token 是宿主正常态（未授权），非错误：不发 unsupported，静默 false。
        #expect(result == false)
        try? await Task.sleep(nanoseconds: 50_000_000)
        #expect(box.read() == nil)
        watcher.cancel()
    }

    @Test func postsRegistrationAndReturnsTrueOnCreated() async throws {
        // 服务端 §10 #5 注册端点 201 摘要（id/session/platform/updated_at）。
        let http = MockTicketHTTP()
        http.enqueue(.success((201, Data(#"{"id":7,"session_id":"test-session","platform":"ios","updated_at":"2026-09-23T12:00:00Z"}"#.utf8))))
        let chat = makeChat(
            pushTokenProvider: { "apns-token-abc" },
            http: http,
            pushUrlOverride: "https://mock.test/api/v1/push/register"
        )

        let result = await chat.registerPushToken()

        #expect(result == true)
        #expect(http.postedURL == "https://mock.test/api/v1/push/register")
        let body = try pushRequestBody(http)
        #expect(body["session_id"] as? String == "test-session")
        #expect(body["platform"] as? String == "ios")
        #expect(body["token"] as? String == "apns-token-abc")
    }

    @Test func derivesUrlFromApiUrlTrimmingTrailingSlash() async throws {
        // 无 override 的生产路径：URL 由 apiUrl 派生（尾斜杠修剪 + /api/v1/push/register）。
        let http = MockTicketHTTP()
        let chat = makeChat(pushTokenProvider: { "apns-token-abc" }, http: http, apiUrl: "https://chat.example.com/")

        let result = await chat.registerPushToken()

        #expect(result == true)
        #expect(http.postedURL == "https://chat.example.com/api/v1/push/register")
    }

    @Test func returnsFalseAndEmitsNetworkOnHttpError() async throws {
        // 服务端 4xx/5xx（session 未建/端点异常）：network 错误流 + false（注册可重报）。
        let http = MockTicketHTTP()
        http.enqueue(.success((404, Data(#"{"error":"Session not found"}"#.utf8))))
        let chat = makeChat(pushTokenProvider: { "apns-token-abc" }, http: http, pushUrlOverride: "https://mock.test/api/v1/push/register")
        let (box, watcher) = captureFirstError(chat)

        let result = await chat.registerPushToken()

        #expect(result == false)
        let error = await awaitCaptured(box)
        #expect(error?.code == "network")
        #expect(error?.message.contains("push register http 404") == true)
        watcher.cancel()
    }

    @Test func returnsFalseAndEmitsNetworkOnIoFailure() async throws {
        let http = MockTicketHTTP()
        http.enqueue(.failure(TicketHTTPFailure()))
        let chat = makeChat(pushTokenProvider: { "apns-token-abc" }, http: http, pushUrlOverride: "https://mock.test/api/v1/push/register")
        let (box, watcher) = captureFirstError(chat)

        let result = await chat.registerPushToken()

        #expect(result == false)
        let error = await awaitCaptured(box)
        #expect(error?.code == "network")
        #expect(error?.message.contains("push register failed") == true)
        watcher.cancel()
    }
}
