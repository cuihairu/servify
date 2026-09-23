import Foundation
import Testing

@testable import ServifyKit

/// AsyncMutex 锁竞争路径（串行发送闸，镜像 Kotlin sendMutex 语义）：
/// 后到者挂起进 waiters、持锁者释放后按序唤醒——无竞争快路径之外的全部分支。
struct AsyncMutexTests {

    /// 跨线程安全的布尔与日志（await 侧轮询挂载屏障用）。
    private final class Flag: @unchecked Sendable {
        private let lock = NSLock()
        private var value = false
        func set() { lock.lock(); value = true; lock.unlock() }
        var isSet: Bool { lock.lock(); defer { lock.unlock() }; return value }
    }

    private final class Log: @unchecked Sendable {
        private let lock = NSLock()
        private var entries: [String] = []
        func record(_ e: String) { lock.lock(); entries.append(e); lock.unlock() }
        var snapshot: [String] { lock.lock(); defer { lock.unlock() }; return entries }
    }

    @Test func contendedAcquireSuspendsAndResumesInOrder() async throws {
        let mutex = AsyncMutex()
        let insideA = Flag()
        let releaseA = Flag()
        let log = Log()

        // A 先持锁，在 body 内等待放行旗标——持锁窗口确定存在
        let taskA = Task {
            await mutex.withLock {
                log.record("a-start")
                insideA.set()
                while !releaseA.isSet { await Task.yield() }
                log.record("a-end")
            }
        }

        // 等 A 确认持锁，再起 B——B 的 acquire 必走 locked 分支挂起进 waiters
        while !insideA.isSet { await Task.yield() }
        let taskB = Task {
            await mutex.withLock {
                log.record("b-start")
            }
        }
        // B 的挂起（waiters.append）是 acquire 同步段，启动后毫秒级完成；
        // 此 sleep 只是挂载屏障（同 awaitSnapshot 轮询性质），非被测行为时序
        try await Task.sleep(nanoseconds: 200_000_000)

        releaseA.set()
        await taskA.value
        await taskB.value

        // 互斥与唤醒顺序：B 不与 A 重叠，A 释放后 B 才进入
        #expect(log.snapshot == ["a-start", "a-end", "b-start"])
    }
}
