import Foundation

@testable import ServifyKit

// MARK: - MockTicketHTTP（Kotlin 侧 MockWebServer 的镜像：记录请求、回放响应）
// CreateTicketTests / RegisterPushTokenTests / ReconcileMessagesTests 共用
// （工单创建/推送注册/断线补拉同一 REST 出站通道 TicketHTTPPosting）。

struct TicketHTTPFailure: Error {}

final class MockTicketHTTP: TicketHTTPPosting, @unchecked Sendable {
    private(set) var postedURL: String?
    private(set) var postedBody: Data?
    private(set) var gotURL: String?
    /// 逐次 GET 记录（补拉分页断言用：单值 gotURL 会被下一页覆盖）。
    private(set) var gotURLs: [String] = []

    /// 队列化响应：post/get 各自独立先入先出（onOpen 自动补拉挂在所有 connect
    /// 类用例上——共用单队列会被补拉消费掉工单/推送的响应，seed connect 的
    /// 用例必踩）。Error 抛出、(status, json) 正常返回；空队列默认 POST 201 /
    /// GET 200 + 空 JSON 对象。
    private var postQueue: [Result<(status: Int, data: Data), Error>] = []
    private var getQueue: [Result<(status: Int, data: Data), Error>] = []

    /// 工单创建/推送注册（POST 面）的回放响应。
    func enqueue(_ result: Result<(status: Int, data: Data), Error>) {
        postQueue.append(result)
    }

    /// 断线补拉（GET 面）的回放响应。
    func enqueueGet(_ result: Result<(status: Int, data: Data), Error>) {
        getQueue.append(result)
    }

    func post(url: String, body: Data) async throws -> (status: Int, data: Data) {
        postedURL = url
        postedBody = body
        guard let result = postQueue.isEmpty ? nil : postQueue.removeFirst() else {
            return (201, Data("{}".utf8))
        }
        switch result {
        case let .success(pair): return pair
        case let .failure(error): throw error
        }
    }

    func get(url: String) async throws -> (status: Int, data: Data) {
        gotURL = url
        gotURLs.append(url)
        guard let result = getQueue.isEmpty ? nil : getQueue.removeFirst() else {
            return (200, Data("{}".utf8))
        }
        switch result {
        case let .success(pair): return pair
        case let .failure(error): throw error
        }
    }
}
