import Foundation

/// 会话页展示形态（D8）：小屏自动升级全屏。
public enum PresentationStyle: Sendable {
    case drawer
    case fullscreen
}

/// 会话页品牌化配置。
public struct Branding: Sendable {
    public var title: String
    /// 平台色；nil = 平台默认主色（UI 层自行取默认主题色）。
    public var primaryColor: UInt32?
    public var welcomeText: String

    public init(
        title: String = "在线客服",
        primaryColor: UInt32? = nil,
        welcomeText: String = "您好，请问有什么可以帮您？"
    ) {
        self.title = title
        self.primaryColor = primaryColor
        self.welcomeText = welcomeText
    }
}

/// 接入配置（平台规格 §4.1，V1 冻结面）。
///
/// apiUrl 强制加密协议：非 https/wss 值构造期直接抛错（对应错误码 config_invalid）。
/// M3 项（pushTokenProvider）随 M3 刀进入冻结面，避免无消费面的预留字段。
public struct ServifyConfig: Sendable {
    public let apiUrl: String
    public let guestToken: String?
    public let branding: Branding
    public let presentationStyle: PresentationStyle
    public let loggingEnabled: Bool

    public init(
        apiUrl: String,
        guestToken: String? = nil,
        branding: Branding = Branding(),
        presentationStyle: PresentationStyle = .drawer,
        loggingEnabled: Bool = false
    ) throws {
        guard apiUrl.hasPrefix("https://") || apiUrl.hasPrefix("wss://") else {
            throw ServifyError.configInvalid(
                message: "apiUrl must use https:// or wss:// (got: \(apiUrl.isEmpty ? "<empty>" : apiUrl))"
            )
        }
        self.apiUrl = apiUrl
        self.guestToken = guestToken
        self.branding = branding
        self.presentationStyle = presentationStyle
        self.loggingEnabled = loggingEnabled
    }
}
