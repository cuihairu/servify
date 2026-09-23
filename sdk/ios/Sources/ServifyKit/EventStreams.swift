import Foundation

/// SharedFlow 的 Swift 最小镜像：多订阅者组播事件流，无 replay（对齐 Kotlin 侧
/// MutableSharedFlow(extraBufferCapacity, DROP_OLDEST) 的"无收集者即丢"语义）。
/// 线程安全：OkHttp/URLSession 回调线程写、订阅者协程读。
public final class EventStream<Element: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var continuations: [UUID: AsyncStream<Element>.Continuation] = [:]

    func emit(_ element: Element) {
        lock.lock()
        let sinks = Array(continuations.values)
        lock.unlock()
        for sink in sinks { sink.yield(element) }
    }

    /// 注册订阅者；返回的 AsyncStream 由订阅方 for-await 消费。
    public func makeStream(bufferingPolicy: AsyncStream<Element>.Continuation.BufferingPolicy = .bufferingNewest(256)) -> AsyncStream<Element> {
        AsyncStream(bufferingPolicy: bufferingPolicy) { continuation in
            let id = UUID()
            lock.lock()
            continuations[id] = continuation
            lock.unlock()
            continuation.onTermination = { [weak self] _ in
                guard let self else { return }
                self.lock.lock()
                self.continuations[id] = nil
                self.lock.unlock()
            }
        }
    }
}

/// StateFlow 的 Swift 最小镜像：持当前值 + 订阅即先吐当前值再吐变更。
public final class StateStream<Element: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var _value: Element
    private var continuations: [UUID: AsyncStream<Element>.Continuation] = [:]

    init(_ initial: Element) {
        _value = initial
    }

    public var value: Element {
        lock.lock()
        defer { lock.unlock() }
        return _value
    }

    func set(_ element: Element) {
        lock.lock()
        _value = element
        let sinks = Array(continuations.values)
        lock.unlock()
        for sink in sinks { sink.yield(element) }
    }

    public func makeStream() -> AsyncStream<Element> {
        AsyncStream(bufferingPolicy: .bufferingNewest(1)) { continuation in
            let id = UUID()
            lock.lock()
            continuation.yield(_value) // StateFlow 订阅语义：先吐当前值
            continuations[id] = continuation
            lock.unlock()
            continuation.onTermination = { [weak self] _ in
                guard let self else { return }
                self.lock.lock()
                self.continuations[id] = nil
                self.lock.unlock()
            }
        }
    }
}
