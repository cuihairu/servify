import Testing

@testable import ServifyKit

/// 接入配置构造口径（平台规格 §4.1）：apiUrl 强制加密协议，非 https/wss 构造期
/// 直接抛 config_invalid（V1 冻结面唯一构造期校验）。
struct ServifyConfigTests {

    @Test func rejectsPlainHttpAndEmptyApiUrl() {
        #expect(throws: ServifyError.self) {
            _ = try ServifyConfig(apiUrl: "http://chat.example.com")
        }
        #expect(throws: ServifyError.self) {
            _ = try ServifyConfig(apiUrl: "")
        }
    }

    @Test func acceptsHttpsAndWssPrefixes() throws {
        _ = try ServifyConfig(apiUrl: "https://chat.example.com")
        _ = try ServifyConfig(apiUrl: "wss://chat.example.com")
    }
}
