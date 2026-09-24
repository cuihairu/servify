import Foundation

/// 工单创建/推送注册/断线补拉的 HTTP 接缝（M3；对齐 Kotlin 侧复用 OkHttpClient
/// 的定位）：WS 传输之外唯一的 REST 出口，抽象出来供 Linux 测试面注入 mock
/// （不碰真网络）。实现只负责单请求-单响应，错误语义（状态码/体解析）由门面
/// 统一裁定。
public protocol TicketHTTPPosting: Sendable {
    func post(url: String, body: Data) async throws -> (status: Int, data: Data)
    /// 补拉对账的 GET 面（M3 刀 10；§10 #1 端点）：同单请求-单响应语义。
    func get(url: String) async throws -> (status: Int, data: Data)
}

#if canImport(Darwin)
/// 生产实现（Darwin）：URLSession.shared 单发请求。
struct URLSessionTicketHTTP: TicketHTTPPosting {
    func post(url: String, body: Data) async throws -> (status: Int, data: Data) {
        guard let requestURL = URL(string: url) else {
            throw URLError(.badURL)
        }
        var request = URLRequest(url: requestURL)
        request.httpMethod = "POST"
        request.setValue("application/json; charset=utf-8", forHTTPHeaderField: "Content-Type")
        request.httpBody = body
        let (data, response) = try await URLSession.shared.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        return (status, data)
    }

    func get(url: String) async throws -> (status: Int, data: Data) {
        guard let requestURL = URL(string: url) else {
            throw URLError(.badURL)
        }
        var request = URLRequest(url: requestURL)
        request.httpMethod = "GET"
        let (data, response) = try await URLSession.shared.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        return (status, data)
    }
}
#endif
