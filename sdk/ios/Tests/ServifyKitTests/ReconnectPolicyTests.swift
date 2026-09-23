import Testing

@testable import ServifyKit

/// 退避序列对齐 Web core 默认（1s 起 2 倍封顶 30s、5 次）。
/// Kotlin 镜像：connect/ReconnectPolicyTest.kt——用例名逐一对应防单侧漂移。
struct ReconnectPolicyTests {

    @Test func defaultPolicyDoublesWithCap() throws {
        let policy = try ReconnectPolicy()
        let delays = (1 ... 5).map { policy.delayFor($0) }
        #expect(delays == [1_000, 2_000, 4_000, 8_000, 16_000])
    }

    @Test func delaysCapAtMaxDelay() throws {
        let policy = try ReconnectPolicy(maxAttempts: 8, initialDelayMs: 10_000, multiplier: 2, maxDelayMs: 30_000)
        let delays = (1 ... 8).map { policy.delayFor($0) }
        #expect(delays == [10_000, 20_000, 30_000, 30_000, 30_000, 30_000, 30_000, 30_000])
    }

    @Test func attemptBeyondMaxOrZeroReturnsNull() throws {
        let policy = try ReconnectPolicy(maxAttempts: 3)
        #expect(policy.delayFor(4) == nil)
        #expect(policy.delayFor(0) == nil)
        #expect(policy.delayFor(3) != nil)
    }

    @Test func rejectsInvalidParameters() {
        #expect(throws: ReconnectPolicyError.self) { _ = try ReconnectPolicy(maxAttempts: 0) }
        #expect(throws: ReconnectPolicyError.self) { _ = try ReconnectPolicy(initialDelayMs: 0) }
        #expect(throws: ReconnectPolicyError.self) { _ = try ReconnectPolicy(multiplier: 0) }
        #expect(throws: ReconnectPolicyError.self) {
            _ = try ReconnectPolicy(initialDelayMs: 1_000, maxDelayMs: 500)
        }
    }
}
