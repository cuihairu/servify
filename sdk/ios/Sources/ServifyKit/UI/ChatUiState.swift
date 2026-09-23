import Foundation

/**
 * 会话页渲染项（D8 消息渲染，Kotlin 镜像：ChatUiState.kt）——从消息累积列表到
 * UI 列表项的纯函数映射。状态驱动渲染的关键是把门面事件流的语义（流式中间态、
 * 终帧替换、来源排序、置信门建议）收敛在这里，SwiftUI 组件只做无脑渲染。
 * 纯函数 => 单测穷举（Linux 可测，无平台条件）。
 */

/// 引用来源项（文档标题 + 得分，D8"可展开的来源列表"；score 缺省=协议零值省略）。
public struct SourceItem: Equatable, Sendable {
    public let documentId: String
    public let title: String
    public let score: Double?

    public init(documentId: String, title: String, score: Double?) {
        self.documentId = documentId
        self.title = title
        self.score = score
    }
}

/// 消息气泡（customer 右对齐，agent/AI 左对齐且 AI 带标识；isStreaming 渲染拼接中
/// 状态；suggestsHandoff 驱动"转人工"按钮强调态（D8 置信门提示）。
public struct ChatBubble: Equatable, Sendable {
    public let id: String
    public let sender: SenderType
    public let content: String
    public let isAiResponse: Bool
    public let isStreaming: Bool
    public let suggestsHandoff: Bool
    public let sources: [SourceItem]
}

/// 会话页列表项。
public enum ChatListItem: Equatable, Sendable {
    /// 会话页首条欢迎提示（branding.welcomeText）。
    case welcome(text: String)
    case bubble(ChatBubble)
}

/**
 * 消息累积列表 → 渲染项列表。
 *
 * 规则（D5/D8）：
 * - 首条 welcome（welcomeText 为空则省略）；
 * - 流式气泡按 id 合并只留最新（门面以同 id 递增 content 发射增量中间态，终帧复用
 *   同 id 且 isStreaming=false 整体替换——即到即拼 + 幂等收口，不静默清空）；
 * - sources 按得分降序（score 缺省归底）。
 */
public func toChatListItems(messages: [ConversationMessage], welcomeText: String?) -> [ChatListItem] {
    var items: [ChatListItem] = []
    if let welcomeText, !welcomeText.isEmpty {
        items.append(.welcome(text: welcomeText))
    }
    // 同 id 后到覆盖先到（流式中间态 → 终帧替换），首次出现位置保持。
    var order: [String] = []
    var byId: [String: ConversationMessage] = [:]
    for message in messages {
        if byId[message.id] == nil { order.append(message.id) }
        byId[message.id] = message
    }
    for id in order {
        let message = byId[id]!
        items.append(.bubble(ChatBubble(
            id: message.id,
            sender: message.sender,
            content: message.content,
            isAiResponse: message.isAiResponse,
            isStreaming: message.isStreaming,
            suggestsHandoff: message.suggestsHandoff,
            sources: message.sources
                .sorted { ($0.score ?? -.infinity) > ($1.score ?? -.infinity) }
                .map { SourceItem(documentId: $0.documentId, title: $0.title, score: $0.score) }
        )))
    }
    return items
}

/**
 * 会话面板的状态 holder（Kotlin 镜像：ChatPanelState.kt）：把门面 events.messages
 * 的事件流收敛为累积列表，经 toChatListItems 映射为渲染项。普通类（非 SwiftUI
 * 状态）——累积/合并规则脱离 SwiftUI 单测（Linux 覆盖）。
 */
public final class ChatPanelState {
    private var accumulated: [ConversationMessage] = []
    private let welcomeText: String?

    public init(welcomeText: String?) {
        self.welcomeText = welcomeText
    }

    /// 消费一条门面事件：同 id 覆盖（流式中间态 → 终帧替换），否则追加。
    public func onMessage(_ message: ConversationMessage) {
        if let index = accumulated.lastIndex(where: { $0.id == message.id }) {
            accumulated[index] = message
        } else {
            accumulated.append(message)
        }
    }

    /// 当前渲染项（每次重组时重算——列表规模 = 单次会话消息数，非热点路径）。
    public func items() -> [ChatListItem] {
        toChatListItems(messages: accumulated, welcomeText: welcomeText)
    }

    public func messageCount() -> Int {
        accumulated.count
    }
}

/// 面板形态（D8：抽屉默认；小屏（高度 < 600dp）抽屉自动升级全屏）。
public enum PanelStyle: Equatable, Sendable {
    case drawer
    case fullscreen
}

public let SMALL_SCREEN_HEIGHT_DP = 600

public func resolvePanelStyle(_ style: PresentationStyle, screenHeightDp: Int) -> PanelStyle {
    switch style {
    case .fullscreen: return .fullscreen
    case .drawer: return screenHeightDp < SMALL_SCREEN_HEIGHT_DP ? .fullscreen : .drawer
    }
}

/// 气泡对齐侧（D8：customer 右，agent/AI/system 提示左）。
extension SenderType {
    public var isRightAligned: Bool { self == .customer }
}
