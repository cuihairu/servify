import Foundation
import Testing

@testable import ServifyKit

// MARK: - 测试错误

enum TestError: Error {
    case timeout
    case streamEnded
}

/// 传输层断线错误（Linux 无 URLError，测试统一用自有类型）。
struct TransportLost: Error {}

// MARK: - MockTransport（镜像 okhttp MockWebServer WS 升级 + WebSocketListener）

/// 可编程 WS 传输：记录发送与连接，暴露 emit* 供测试驱动回调（对齐 OkHttp
/// 回调线程模型——回调同步、同连接内串行）。
class MockTransport: WebSocketTransport {
    private(set) var connectedURL: String?
    private(set) var sentTexts: [String] = []
    private(set) var canceled = false
    var listener: WebSocketTransportListener?
    /// true = 升级即建立（镜像 MockWebServer withWebSocketUpgrade 的 onOpen 自动回调）。
    var autoOpen = false

    func connect(to url: String, listener: WebSocketTransportListener) {
        connectedURL = url
        self.listener = listener
        if autoOpen { emitOpen() }
    }

    func send(_ text: String) -> Bool {
        sentTexts.append(text)
        return true
    }

    func cancel() {
        canceled = true
    }

    // 测试驱动（镜像服务端 listener 的 webSocket.send / cancel / 握手拒绝）
    func emitOpen() {
        listener?.onOpen()
    }

    func emitMessage(_ text: String) {
        listener?.onMessage(text)
    }

    func emitFailure(_ error: Error, httpStatus: Int? = nil) {
        listener?.onFailure(error, httpStatus: httpStatus)
    }

    func emitClosed(code: Int = 1000) {
        listener?.onClosed(code: code, reason: "")
    }
}

/// 回显判据服务器：升级即建立连接（镜像 MockWebServer withWebSocketUpgrade），
/// 收到 text-message 原样回显帧（镜像 EchoListener）。
final class EchoTransport: MockTransport {
    override init() {
        super.init()
        autoOpen = true
    }

    override func send(_ text: String) -> Bool {
        super.send(text)
        emitMessage(TestFrames.echo(content: "你好"))
        return true
    }
}

/// 首条客户消息后异常断线（镜像 DropOnFirstMessage：onMessage 内同步 cancel——
/// Swift 侧等价手法是向 listener 发 onFailure）。
final class DropOnFirstMessageTransport: MockTransport {
    private var seen = false

    override init() {
        super.init()
        autoOpen = true
    }

    override func send(_ text: String) -> Bool {
        super.send(text)
        if !seen {
            seen = true
            emitFailure(TransportLost())
        }
        return true
    }
}

/// 每条客户消息都触发断线（镜像 onMessage 内 cancel()，用于重连测试）。
final class DropOnMessageTransport: MockTransport {
    override init() {
        super.init()
        autoOpen = true
    }

    override func send(_ text: String) -> Bool {
        super.send(text)
        emitFailure(TransportLost())
        return true
    }
}

/// 握手拒绝：connect 即失败（镜像 server.enqueue(404) 的升级失败路径）。
final class HandshakeRejectTransport: MockTransport {
    let status: Int

    init(status: Int) {
        self.status = status
    }

    override func connect(to url: String, listener: WebSocketTransportListener) {
        super.connect(to: url, listener: listener)
        listener.onFailure(TransportLost(), httpStatus: status)
    }
}

/// 按收到的客户消息序号下发不同服务端帧（镜像 ScriptedListener）：
/// 1=坐席消息、2=流式增量+终帧、3+=坐席消息。
final class ScriptedTransport: MockTransport {
    private var count = 0

    override init() {
        super.init()
        autoOpen = true
    }

    override func send(_ text: String) -> Bool {
        super.send(text)
        count += 1
        switch count {
        case 1:
            emitMessage(TestFrames.agentMessage("坐席A"))
        case 2:
            emitMessage(TestFrames.delta("根据", done: false))
            emitMessage(TestFrames.delta("政策。", done: false))
            emitMessage(TestFrames.aiFinal(content: "根据政策。", confidence: 0.9, source: "kb", sessionId: "test-session"))
        default:
            emitMessage(TestFrames.agentMessage("坐席B"))
        }
        return true
    }
}

// MARK: - TransportSequence（镜像 MockWebServer.enqueue 队列：按 connect 次序出队）

final class TransportSequence {
    private let lock = NSLock()
    private var queue: [MockTransport]

    init(_ transports: [MockTransport]) {
        queue = transports
    }

    func factory() -> () -> WebSocketTransport {
        { [self] in
            lock.lock()
            defer { lock.unlock() }
            return queue.isEmpty ? MockTransport() : queue.removeFirst()
        }
    }
}

// MARK: - 帧构造（与 Kotlin 测试 JSON 字面量逐字对齐）

enum TestFrames {
    static func echo(content: String) -> String {
        "{\"type\":\"text-message\",\"data\":{\"content\":\"\(content)\"},\"session_id\":\"test-session\",\"timestamp\":\"2026-01-01T00:00:00Z\"}"
    }

    static func agentMessage(_ content: String) -> String {
        "{\"type\":\"agent-message\",\"data\":{\"content\":\"\(content)\",\"sender\":\"MP Agent\"},\"session_id\":\"test-session\"}"
    }

    static func delta(_ contentDelta: String, done: Bool) -> String {
        "{\"type\":\"ai-response-delta\",\"data\":{\"content_delta\":\"\(contentDelta)\",\"done\":\(done)}}"
    }

    static func aiFinal(content: String, confidence: Double, source: String, sessionId: String) -> String {
        "{\"type\":\"ai-response\",\"data\":{\"content\":\"\(content)\",\"confidence\":\(confidence),\"source\":\"\(source)\",\"sources\":[{\"document_id\":\"d1\",\"title\":\"退货政策\",\"score\":0.9}]},\"session_id\":\"\(sessionId)\"}"
    }

    static func transfer(agentId: Int, message: String) -> String {
        "{\"type\":\"transfer_notification\",\"data\":{\"message\":\"\(message)\",\"agent_id\":\(agentId)},\"session_id\":\"test-session\"}"
    }

    static func waiting(_ message: String) -> String {
        "{\"type\":\"waiting_notification\",\"data\":{\"message\":\"\(message)\"},\"session_id\":\"test-session\"}"
    }
}

// MARK: - 等待助手（镜像 withTimeout + first(pred)）

/// 迭代流至首个命中 pred 的元素；超时抛错（镜像 withTimeout(5s) + first）。
/// AsyncStream 的 builder 在 makeStream 时同步注册（探针核实），
/// 迭代晚启动也不丢 emit——无需额外挂载屏障。
func nextMatching<Element: Sendable>(
    _ stream: AsyncStream<Element>,
    timeoutMs: Int = 5_000,
    where pred: @escaping @Sendable (Element) -> Bool = { _ in true }
) async throws -> Element {
    try await withThrowingTaskGroup(of: Element.self) { group in
        group.addTask {
            for await element in stream where pred(element) { return element }
            throw TestError.streamEnded
        }
        group.addTask {
            try await Task.sleep(nanoseconds: UInt64(timeoutMs) * 1_000_000)
            throw TestError.timeout
        }
        let first = try await group.next()!
        group.cancelAll()
        return first
    }
}

/// 连接就绪等待（StateStream 订阅先吐当前值，已连接时立即返回）。
func awaitConnected(_ chat: ServifyChat) async throws {
    _ = try await nextMatching(chat.events.connectionState.makeStream(), where: { $0 == .connected })
}

/// 轮询 history 快照直至命中（镜像 while(delay) 轮询 + withTimeout）。
func awaitSnapshot(
    _ chat: ServifyChat,
    timeoutMs: Int = 5_000,
    where pred: @escaping ([ConversationMessage]) -> Bool
) async throws {
    let deadline = Date().addingTimeInterval(Double(timeoutMs) / 1_000)
    while Date() < deadline {
        if pred(chat.historySnapshot()) { return }
        try await Task.sleep(nanoseconds: 50_000_000)
    }
    throw TestError.timeout
}
