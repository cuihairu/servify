import Foundation

/// 连接状态机（平台规格 §4.4）：
/// idle ─(首次 show/connect)→ connecting → connected ─(断)→ reconnecting(n/5) → connected
///                                     └─(握手失败)→ disconnected
/// disconnected ─(用户再次打开会话页)→ connecting；后台切换不产生 disconnected。
public enum ConnectionState: Equatable, Sendable {
    case idle
    case connecting
    case connected
    case reconnecting(attempt: Int)
    case disconnected
}

/// 退避参数校验失败（对齐 Kotlin require 的 IllegalArgumentException 语义）。
public struct ReconnectPolicyError: Error, Equatable {
    public let reason: String
    public init(reason: String) { self.reason = reason }
}

/// 重连退避策略：参数对齐 Web core（contracts/reconnect.ts，源码核实）——
/// 指数退避 1s 起、2 倍封顶 30s、最多 5 次。
///
/// 纯逻辑、可独立穷举测试；宿主覆盖不进 V1 冻结面（规格 §4.6 AppReconnectPolicy，experimental）。
public struct ReconnectPolicy: Sendable {
    /// Web core 默认策略（静态常量：默认参数表达式不能 throws）。
    public static let standard = try! ReconnectPolicy(
        maxAttempts: 5,
        initialDelayMs: 1_000,
        multiplier: 2,
        maxDelayMs: 30_000
    )

    public let maxAttempts: Int
    private let initialDelayMs: Int
    private let multiplier: Int
    private let maxDelayMs: Int

    public init(
        maxAttempts: Int = 5,
        initialDelayMs: Int = 1_000,
        multiplier: Int = 2,
        maxDelayMs: Int = 30_000
    ) throws {
        guard maxAttempts >= 1 else {
            throw ReconnectPolicyError(reason: "maxAttempts must be >= 1")
        }
        guard initialDelayMs > 0, multiplier >= 1, maxDelayMs >= initialDelayMs else {
            throw ReconnectPolicyError(reason: "invalid backoff parameters")
        }
        self.maxAttempts = maxAttempts
        self.initialDelayMs = initialDelayMs
        self.multiplier = multiplier
        self.maxDelayMs = maxDelayMs
    }

    /// 第 attempt 次重连（1 起）前应等待的毫秒数；attempt 超出 maxAttempts 返回 nil（放弃）。
    public func delayFor(_ attempt: Int) -> Int? {
        guard attempt >= 1, attempt <= maxAttempts else { return nil }
        var delay = initialDelayMs
        for _ in 0 ..< attempt - 1 {
            delay = min(delay * multiplier, maxDelayMs)
        }
        return min(delay, maxDelayMs)
    }
}
