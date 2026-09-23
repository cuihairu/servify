#if canImport(UIKit)
import SwiftUI

/**
 * 宿主入口（Kotlin 镜像：EntryOrchestrator + FloatingButtonView 的 SwiftUI 形态）：
 * 浮钮 + 点开面板（scrim + 底部抽屉/全屏，D8 三形态由 resolvePanelStyle 决定，
 * 小屏 <600dp 自动升级全屏）。
 *
 * 接线语义对齐 Kotlin：
 * - 首次展开触发 WS 连接（§4.2 show = connect + 拉起会话 UI；惰性连接）；
 * - 展开即会话页可见（清未读，ChatPanelModel.start 内二次保险）；
 * - 收起（点 ✕ / 点 scrim）连接保持，此后到达的消息计入未读（浮钮角标）。
 *
 * 宿主一行接入：`ServifyView(config: try! ServifyConfig(apiUrl: "..."))`。
 * 重复创建由接入方避免（单例语义，与 Kotlin 一致，文档明示）。
 */
public struct ServifyView: View {
    private let chat: ServifyChat
    private let config: ServifyConfig

    @State private var expanded = false
    @State private var unreadCount = 0

    /// 宿主自持门面（跨视图保会话连续性时用）或传 config 内部 create（一行接入）。
    public init(config: ServifyConfig) {
        self.config = config
        self.chat = ServifyChat.create(config: config)
    }

    /// 高级接入：宿主自己 create 并持有门面（如多入口共享同一会话）。
    public init(chat: ServifyChat) {
        self.config = chat.config
        self.chat = chat
    }

    public var body: some View {
        ZStack(alignment: .bottom) {
            if expanded {
                // scrim：点空白收面板（V1 无拖拽手势）。
                Color.black.opacity(0.32)
                    .ignoresSafeArea()
                    .onTapGesture { collapse() }
                panel
            }
            floatingButton
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottomTrailing)
                .padding(16)
        }
        .task {
            // 角标 = 面板收起时到达的消息数（未读语义 §4.3）。
            for await count in chat.events.unreadCount.makeStream() { unreadCount = count }
        }
    }

    // MARK: - 面板容器（形态差异只在高度：全屏 / ≤85% 屏高抽屉，小屏自动升级）

    private var panel: some View {
        let screenHeight = UIScreen.main.bounds.height
        let style = resolvePanelStyle(config.presentationStyle, screenHeightDp: Int(screenHeight))
        let height: CGFloat? = (style == .drawer) ? screenHeight * 0.85 : nil

        return ChatPanel(
            chat: chat,
            title: config.branding.title,
            welcomeText: config.branding.welcomeText.isEmpty ? nil : config.branding.welcomeText,
            onDismiss: { collapse() }
        )
        .frame(height: height)
        .frame(maxWidth: .infinity)
        .ignoresSafeArea(edges: .bottom)
        .transition(.move(edge: .bottom))
    }

    // MARK: - 浮动按钮（D8：56pt 圆形悬浮，默认右下角；角标 = 收起时的未读计数）

    private var floatingButton: some View {
        let primary = ChatThemeDefaults.resolvePrimary(config.branding.primaryColor)
        return Button {
            expand()
        } label: {
            ZStack(alignment: .topTrailing) {
                Image(systemName: "message.fill")
                    .font(.system(size: 24))
                    .foregroundColor(.white)
                    .frame(width: 56, height: 56)
                    .background(Circle().fill(primary))
                if unreadCount > 0 {
                    Text(badgeLabel(unreadCount))
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundColor(.white)
                        .padding(.horizontal, 5)
                        .frame(minWidth: 20, minHeight: 20)
                        .background(Circle().fill(ChatThemeDefaults.argb(0xFFEF4444)))
                        .offset(x: 6, y: -4)
                }
            }
        }
    }

    private func badgeLabel(_ count: Int) -> String {
        count > 99 ? "99+" : String(count)
    }

    // MARK: - 展开/收起（§4.2 show/hide 语义）

    private func expand() {
        guard !expanded else { return }
        expanded = true
        // 首次展开触发连接（惰性连接；重复 connect 幂等）。
        Task { await chat.connect() }
        chat.onSessionVisible()
    }

    private func collapse() {
        guard expanded else { return }
        expanded = false
        chat.onSessionHidden()
    }
}
#endif
