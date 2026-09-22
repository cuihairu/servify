package servify.sdk.android.connect

/**
 * 重连退避策略：参数对齐 Web core（contracts/reconnect.ts，源码核实）——
 * 指数退避 1s 起、2 倍封顶 30s、最多 5 次。
 *
 * 纯逻辑、可独立穷举测试；宿主覆盖不进 V1 冻结面（规格 §4.6 AppReconnectPolicy，experimental）。
 */
class ReconnectPolicy(
    val maxAttempts: Int = 5,
    private val initialDelayMs: Long = 1_000,
    private val multiplier: Int = 2,
    private val maxDelayMs: Long = 30_000,
) {
    init {
        require(maxAttempts >= 1) { "maxAttempts must be >= 1" }
        require(initialDelayMs > 0 && multiplier >= 1 && maxDelayMs >= initialDelayMs) {
            "invalid backoff parameters"
        }
    }

    /** 第 attempt 次重连（1 起）前应等待的毫秒数；attempt 超出 maxAttempts 返回 null（放弃）。 */
    fun delayFor(attempt: Int): Long? {
        if (attempt < 1 || attempt > maxAttempts) return null
        var delay = initialDelayMs
        repeat(attempt - 1) {
            delay = (delay * multiplier).coerceAtMost(maxDelayMs)
        }
        return delay.coerceAtMost(maxDelayMs)
    }
}
