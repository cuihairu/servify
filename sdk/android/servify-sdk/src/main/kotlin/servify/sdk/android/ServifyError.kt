package servify.sdk.android

/**
 * 错误模型（平台规格 §4.5，V1 冻结面）：统一携带 code/message/retryable。
 * code 取值与七类场景一一对应；retryable 语义见规格表。
 */
sealed class ServifyError(val code: String, val message: String, val retryable: Boolean) {

    /** apiUrl 非 https/wss、必填缺失等配置问题。 */
    class ConfigInvalid(message: String) : ServifyError("config_invalid", message, retryable = false)

    /** WS 建连/发送失败、DNS 解析失败。 */
    class Network(message: String) : ServifyError("network", message, retryable = true)

    /** 握手 5xx。 */
    class ServerUnavailable(message: String) : ServifyError("server_unavailable", message, retryable = true)

    /** 握手被拒（400 session_id 缺失 / Origin 白名单拒绝）——配置问题，重试无意义。 */
    class HandshakeRejected(message: String) : ServifyError("handshake_rejected", message, retryable = false)

    /** 发送后超时未见回显帧（成功判据见 PROTOCOL.md §6.3）。 */
    class SendTimeout(message: String) : ServifyError("send_timeout", message, retryable = true)

    /** 调用能力存在但配套未启用/端点未上线（M3 过渡期行为）。 */
    class Unsupported(message: String) : ServifyError("unsupported", message, retryable = false)

    /** 工单创建失败（M3）。 */
    class TicketFailed(message: String) : ServifyError("ticket_failed", message, retryable = true)
}
