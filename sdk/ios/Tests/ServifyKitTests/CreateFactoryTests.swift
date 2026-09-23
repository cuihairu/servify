import Testing

@testable import ServifyKit

/**
 * create() 工厂冒烟（§4.2 构造口径）：Darwin 专属路径——Linux CI 编译不到
 * （URLSessionWebSocketTransport 与 canImport(Darwin) 分支同域），由 CI 的
 * ios-macos job 专测（覆盖率口径中唯一的 Darwin 豁免由此覆盖）。
 */
#if canImport(Darwin)
struct CreateFactoryTests {

    @Test func createGeneratesAnonymousSessionAndStaysIdle() {
        let chat = ServifyChat.create(config: try! ServifyConfig(apiUrl: "https://chat.example.com"))
        // 匿名 session 模式（D5/PROTOCOL §1）：SDK 生成 "m-" + UUID。
        #expect(chat.sessionId.hasPrefix("m-"))
        // 惰性连接：create 不建连，首次 connect()（UI 刀后由 show 触发）才发起 WS。
        #expect(chat.events.connectionState.value == .idle)
    }

    @Test func createGeneratesDistinctSessionIds() {
        let a = ServifyChat.create(config: try! ServifyConfig(apiUrl: "https://chat.example.com"))
        let b = ServifyChat.create(config: try! ServifyConfig(apiUrl: "https://chat.example.com"))
        #expect(a.sessionId != b.sessionId)
    }
}
#endif
