package servify.sdk.android.connect

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertNotNull
import kotlin.test.assertNull

/** 退避序列对齐 Web core 默认（1s 起 2 倍封顶 30s、5 次）。 */
class ReconnectPolicyTest {

    @Test
    fun defaultPolicyDoublesWithCap() {
        val policy = ReconnectPolicy()
        assertEquals(listOf(1_000L, 2_000L, 4_000L, 8_000L, 16_000L), (1..5).map { policy.delayFor(it) })
    }

    @Test
    fun delaysCapAtMaxDelay() {
        val policy = ReconnectPolicy(maxAttempts = 8, initialDelayMs = 10_000, multiplier = 2, maxDelayMs = 30_000)
        assertEquals(
            listOf(10_000L, 20_000L, 30_000L, 30_000L, 30_000L, 30_000L, 30_000L, 30_000L),
            (1..8).map { policy.delayFor(it) },
        )
    }

    @Test
    fun attemptBeyondMaxOrZeroReturnsNull() {
        val policy = ReconnectPolicy(maxAttempts = 3)
        assertNull(policy.delayFor(4))
        assertNull(policy.delayFor(0))
        assertNotNull(policy.delayFor(3))
    }

    @Test
    fun rejectsInvalidParameters() {
        assertFailsWith<IllegalArgumentException> { ReconnectPolicy(maxAttempts = 0) }
        assertFailsWith<IllegalArgumentException> { ReconnectPolicy(initialDelayMs = 0) }
        assertFailsWith<IllegalArgumentException> { ReconnectPolicy(multiplier = 0) }
        assertFailsWith<IllegalArgumentException> {
            ReconnectPolicy(initialDelayMs = 1_000, maxDelayMs = 500)
        }
    }
}
