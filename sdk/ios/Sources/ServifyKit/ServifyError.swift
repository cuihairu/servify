import Foundation

/// 错误模型（平台规格 §4.5，V1 冻结面）：统一携带 code/message/retryable。
/// code 取值与七类场景一一对应；retryable 语义见规格表。
public enum ServifyError: Error, Equatable {
    case configInvalid(message: String)
    case network(message: String)
    case serverUnavailable(message: String)
    case handshakeRejected(message: String)
    case sendTimeout(message: String)
    case unsupported(message: String)
    case ticketFailed(message: String)

    public var code: String {
        switch self {
        case .configInvalid: return "config_invalid"
        case .network: return "network"
        case .serverUnavailable: return "server_unavailable"
        case .handshakeRejected: return "handshake_rejected"
        case .sendTimeout: return "send_timeout"
        case .unsupported: return "unsupported"
        case .ticketFailed: return "ticket_failed"
        }
    }

    public var message: String {
        switch self {
        case let .configInvalid(m), let .network(m), let .serverUnavailable(m),
             let .handshakeRejected(m), let .sendTimeout(m), let .unsupported(m), let .ticketFailed(m):
            return m
        }
    }

    public var retryable: Bool {
        switch self {
        case .network, .serverUnavailable, .sendTimeout, .ticketFailed: return true
        case .configInvalid, .handshakeRejected, .unsupported: return false
        }
    }
}
