import Foundation

/// 串行发送闸（镜像 Kotlin sendMutex）：防止并发 sendMessage 互相覆盖 pendingEcho。
final class AsyncMutex: @unchecked Sendable {
    private let lock = NSLock()
    private var locked = false
    private var waiters: [CheckedContinuation<Void, Never>] = []

    func withLock<T: Sendable>(_ body: @Sendable () async -> T) async -> T {
        await acquire()
        defer { release() }
        return await body()
    }

    private func acquire() async {
        await withCheckedContinuation { (cont: CheckedContinuation<Void, Never>) in
            lock.lock()
            if !locked {
                locked = true
                lock.unlock()
                cont.resume()
                return
            }
            waiters.append(cont)
            lock.unlock()
        }
    }

    /// 测试锚点：排队中的续体数（0 = 无竞争挂起）。竞争测试据此轮询确认
    /// 「后来者确已挂起」后再放行持有者——替代 sleep 屏障（CI 慢机调度下
    /// sleep 窗口不可靠，后来者可能晚到走快路径，唤醒段覆盖漂移）。
    var pendingWaiterCount: Int {
        lock.lock()
        defer { lock.unlock() }
        return waiters.count
    }

    private func release() {
        lock.lock()
        if waiters.isEmpty {
            locked = false
            lock.unlock()
            return
        }
        let next = waiters.removeFirst()
        lock.unlock()
        next.resume()
    }
}

/// 回显判据闸（镜像 Kotlin EchoGate + CompletableDeferred）：send 后等回显，
/// 超时未到返回 false。取消安全——竞败任务经 onCancel 释放续体，不悬挂。
final class EchoGate: @unchecked Sendable {
    private let expectedContent: String
    private let lock = NSLock()
    private var matched = false
    private var continuation: CheckedContinuation<Void, Never>?

    init(expectedContent: String) {
        self.expectedContent = expectedContent
    }

    /// 回显内容匹配判据命中；命中即唤醒等待方。
    func completeIfExpected(_ content: String) {
        guard content == expectedContent else { return }
        lock.lock()
        guard !matched else { lock.unlock(); return }
        matched = true
        let cont = continuation
        continuation = nil
        lock.unlock()
        cont?.resume()
    }

    /// 等待判据命中或超时；返回是否命中。
    func wait(timeoutMs: Int) async -> Bool {
        await withTaskGroup(of: Bool.self) { group in
            group.addTask { await self.suspendUntilMatched() }
            group.addTask {
                try? await Task.sleep(nanoseconds: UInt64(timeoutMs) * 1_000_000)
                return false
            }
            let first = (await group.next()) ?? false
            group.cancelAll()
            return first
        }
    }

    /// 挂起至命中或被取消（取消经 onCancel 释放续体）；仅当判据真正命中时返回 true。
    private func suspendUntilMatched() async -> Bool {
        let resumed = await withTaskCancellationHandler {
            await withCheckedContinuation { (cont: CheckedContinuation<Void, Never>) in
                lock.lock()
                if matched {
                    lock.unlock()
                    cont.resume()
                    return
                }
                continuation = cont
                lock.unlock()
            }
            return true
        } onCancel: { [weak self] in
            self?.releaseIfPending()
        }
        return resumed && isMatched()
    }

    private func releaseIfPending() {
        lock.lock()
        let cont = continuation
        continuation = nil
        lock.unlock()
        cont?.resume()
    }

    private func isMatched() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return matched
    }
}
