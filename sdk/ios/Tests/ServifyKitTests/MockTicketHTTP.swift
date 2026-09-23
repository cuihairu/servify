import Foundation

@testable import ServifyKit

// MARK: - MockTicketHTTP（Kotlin 侧 MockWebServer 的镜像：记录请求、回放响应）
// CreateTicketTests / RegisterPushTokenTests 共用（工单创建/推送注册同一
// REST 出站通道 TicketHTTPPosting）。

struct TicketHTTPFailure: Error {}

final class MockTicketHTTP: TicketHTTPPosting, @unchecked Sendable {
    private(set) var postedURL: String?
    private(set) var postedBody: Data?

    /// 队列化响应：先入先出；Error 抛出、(status, json) 正常返回。空队列默认 201+id。
    private var queue: [Result<(status: Int, data: Data), Error>] = []

    func enqueue(_ result: Result<(status: Int, data: Data), Error>) {
        queue.append(result)
    }

    func post(url: String, body: Data) async throws -> (status: Int, data: Data) {
        postedURL = url
        postedBody = body
        guard let result = queue.isEmpty ? nil : queue.removeFirst() else {
            return (201, Data("{}".utf8))
        }
        switch result {
        case let .success(pair): return pair
        case let .failure(error): throw error
        }
    }
}
