import Foundation
import Testing

@testable import ServifyKit

/// 推送注册口（M3 刀 4，平台规格 §4 registerPushToken；§10 #5 端点未上线的过渡语义）：
/// 未配置→unsupported；配置+无 token→静默；配置+有 token→过渡期 unsupported。
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

    private func makeChat(pushTokenProvider: (@Sendable () async -> String?)?) -> ServifyChat {
        ServifyChat(
            config: try! ServifyConfig(apiUrl: "https://chat.example.com", pushTokenProvider: pushTokenProvider),
            sessionId: "test-session",
            echoTimeoutMs: 200,
            wsUrlOverride: "ws://mock.test/api/v1/ws",
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

    @Test func emitsUnsupportedWhenTokenAvailableButEndpointNotLive() async throws {
        let chat = makeChat(pushTokenProvider: { "apns-token-abc" })
        let (box, watcher) = captureFirstError(chat)

        let result = await chat.registerPushToken()

        #expect(result == false)
        let error = await awaitCaptured(box)
        #expect(error?.code == "unsupported")
        #expect(error?.message == "push registration endpoint not available")
        watcher.cancel()
    }
}
