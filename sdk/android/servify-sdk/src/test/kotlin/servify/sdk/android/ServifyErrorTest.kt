package servify.sdk.android

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/** 错误七码对账（平台规格 §4.5）：code 字符串与 retryable 逐一锁定。 */
class ServifyErrorTest {

    @Test
    fun sevenErrorCodesCarrySpecifiedRetryability() {
        val errors: List<ServifyError> = listOf(
            ServifyError.ConfigInvalid("bad url"),
            ServifyError.Network("dns"),
            ServifyError.ServerUnavailable("503"),
            ServifyError.HandshakeRejected("origin denied"),
            ServifyError.SendTimeout("no echo"),
            ServifyError.Unsupported("endpoint not live"),
            ServifyError.TicketFailed("create failed"),
        )

        assertEquals(
            listOf(
                "config_invalid" to false,
                "network" to true,
                "server_unavailable" to true,
                "handshake_rejected" to false,
                "send_timeout" to true,
                "unsupported" to false,
                "ticket_failed" to true,
            ),
            errors.map { it.code to it.retryable },
        )
        assertTrue(errors.all { it.message.isNotEmpty() })
        assertFalse(ServifyError.ConfigInvalid("x").retryable && ServifyError.Network("x").retryable == false)
    }
}
