package servify.sdk.android.connect

/**
 * 连接状态机（平台规格 §4.4）：
 * idle ─(首次 show/connect)→ connecting → connected ─(断)→ reconnecting(n/5) → connected
 *                                     └─(握手失败)→ disconnected
 * disconnected ─(用户再次打开会话页)→ connecting；后台切换不产生 disconnected。
 */
sealed class ConnectionState {
    data object Idle : ConnectionState()
    data object Connecting : ConnectionState()
    data object Connected : ConnectionState()
    data class Reconnecting(val attempt: Int) : ConnectionState()
    data object Disconnected : ConnectionState()
}
