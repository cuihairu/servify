import Foundation

/// 工单创建回执（M3 会话页工单入口；Kotlin 镜像：TicketReceipt.kt）：服务端 201
/// 响应体 id 的透传，供面板提示行（"工单 #N 已创建"）与宿主后续流转使用。
public struct TicketReceipt: Equatable, Sendable {
    public let ticketId: Int64

    public init(ticketId: Int64) {
        self.ticketId = ticketId
    }
}
