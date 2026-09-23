#if canImport(UIKit)
import SwiftUI
import Combine

/// 主题常量（Kotlin ChatThemeDefaults 镜像，色值逐项一致；D9 零三方依赖——
/// SwiftUI 全系统库，白名单约束由 Package.swift 无 dependencies 结构性保证）。
enum ChatThemeDefaults {
    /// ARGB UInt32 → SwiftUI Color（取低 24 位 RGB，alpha 恒 1——V1 无透明主色场景）。
    static func argb(_ value: UInt32) -> Color {
        Color(
            red: Double((value >> 16) & 0xFF) / 255.0,
            green: Double((value >> 8) & 0xFF) / 255.0,
            blue: Double(value & 0xFF) / 255.0
        )
    }

    /// 未配置 branding.primaryColor 时的默认主色（品牌蓝，0xFF2563EB）。
    static let defaultPrimary: UInt32 = 0xFF2563EB
    static let pageBackground = argb(0xFFF8FAFC)
    static let surfaceWhite = Color.white
    static let textPrimary = argb(0xFF0F172A)
    static let textSecondary = argb(0xFF64748B)
    static let bubbleCustomer = argb(0xFF2563EB)
    static let bubbleOther = Color.white
    static let divider = argb(0xFFE2E8F0)
    static let handoffBadge = argb(0xFFFEF3C7)

    static func resolvePrimary(_ configured: UInt32?) -> Color {
        argb(configured ?? defaultPrimary)
    }
}

/**
 * 面板状态接线（Kotlin 镜像：ChatPanel.kt 的 LaunchedEffect/collectAsState 面）：
 * 门面事件流 → @Published。合并规则不在本层——由 ChatPanelState（Linux 单测）
 * 与门面 historySnapshot（同 id 覆盖累积）承担，这里只做线程搬运与驱动。
 */
@MainActor
final class ChatPanelModel: ObservableObject {
    @Published private(set) var items: [ChatListItem] = []
    @Published private(set) var statusLine: String?
    @Published private(set) var connectionState: ConnectionState = .idle

    private let chat: ServifyChat
    private let panelState: ChatPanelState

    init(chat: ServifyChat, welcomeText: String?) {
        self.chat = chat
        self.panelState = ChatPanelState(welcomeText: welcomeText)
    }

    /// 面板挂载即启动（View 的 .task 调用一次）：
    /// 会话页可见清未读（§4.3）+ 回放门面累积（hide 期间消息不丢，D7 内存级连续性）
    /// + 订阅四条事件流。
    func start() {
        chat.onSessionVisible()
        chat.historySnapshot().forEach { panelState.onMessage($0) }
        items = panelState.items()

        Task {
            for await message in chat.events.messages.makeStream() {
                panelState.onMessage(message)
                items = panelState.items()
            }
        }
        Task {
            for await state in chat.events.connectionState.makeStream() { connectionState = state }
        }
        Task {
            for await message in chat.events.waitingInQueue.makeStream() { statusLine = message }
        }
        Task {
            for await assignment in chat.events.agentAssigned.makeStream() { statusLine = assignment.message }
        }
    }

    /// 发送客户消息；失败（echo 超时）经 error 流暴露给宿主，面板内静默可重发。
    func send(_ text: String) {
        Task { try? await chat.sendMessage(text) }
    }

    /// V1：协议无客户端转人工帧——点击即发送固定文本消息（服务端兜底链路可达）。
    func requestHandoff() {
        send("请转人工客服")
    }

    /// 工单创建（M3 刀 3b；Kotlin 镜像：ChatPanel 的 TicketFormOverlay onSubmit 接线）：
    /// 成功提示行走门面 appendSystemHint（进 history，hide 后可回放）。
    /// 返回是否创建成功（表单按此收起或展示重试提示）。
    @MainActor
    func createTicket(title: String, description: String?) async -> Bool {
        guard let receipt = await chat.createTicket(title: title, description: description) else {
            return false
        }
        chat.appendSystemHint("工单 #\(receipt.ticketId) 已创建，客服将尽快处理")
        return true
    }
}

/**
 * 会话面板（D8 三形态的抽屉/全屏共体：形态差异只在宿主容器高度，主体组件同一棵）。
 */
struct ChatPanel: View {
    let chat: ServifyChat
    let title: String
    let welcomeText: String?
    var onDismiss: () -> Void

    @StateObject private var model: ChatPanelModel
    @State private var inputText = ""
    @State private var showTicketForm = false

    init(chat: ServifyChat, title: String, welcomeText: String?, onDismiss: @escaping () -> Void) {
        self.chat = chat
        self.title = title
        self.welcomeText = welcomeText
        self.onDismiss = onDismiss
        _model = StateObject(wrappedValue: ChatPanelModel(chat: chat, welcomeText: welcomeText))
    }

    var body: some View {
        VStack(spacing: 0) {
            titleBar
            if let line = model.statusLine { statusLineView(line) }
            messageList
            inputBar(enabled: model.connectionState == .connected)
        }
        .background(ChatThemeDefaults.pageBackground)
        .task { model.start() }
        .overlay {
            if showTicketForm {
                // 工单创建表单（M3 刀 3b）：成功提示行走 appendSystemHint。
                TicketFormOverlay(
                    primary: primary,
                    onSubmit: { titleText, detail in
                        await model.createTicket(title: titleText, description: detail)
                    },
                    onDismiss: { showTicketForm = false }
                )
            }
        }
    }

    private var primary: Color {
        ChatThemeDefaults.resolvePrimary(chat.config.branding.primaryColor)
    }

    // MARK: - 标题栏（主色底 + 标题 + 连接状态 + 关闭）

    private var titleBar: some View {
        ZStack {
            HStack {
                Text(title)
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundColor(.white)
                    .lineLimit(1)
                Spacer()
                Button {
                    showTicketForm = true
                } label: {
                    Text("工单")
                        .font(.system(size: 14))
                        .foregroundColor(.white)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 6)
                }
                Button(action: onDismiss) {
                    Text("✕")
                        .font(.system(size: 18))
                        .foregroundColor(.white)
                        .padding(6)
                }
            }
            if let label = connectionLabel(model.connectionState) {
                Text(label)
                    .font(.system(size: 12))
                    .foregroundColor(.white.opacity(0.85))
                    .lineLimit(1)
            }
        }
        .padding(.horizontal, 16)
        .frame(height: 52)
        .background(primary)
    }

    private func connectionLabel(_ state: ConnectionState) -> String? {
        switch state {
        case .idle: return nil
        case .connecting: return "连接中…"
        case let .reconnecting(attempt): return "重新连接中…（第 \(attempt) 次）"
        case .connected: return nil
        case .disconnected: return "连接已断开"
        }
    }

    // MARK: - 状态条（转人工状态机渲染：排队提示 / 坐席接入提示，显示最近一条）

    private func statusLineView(_ text: String) -> some View {
        Text(text)
            .font(.system(size: 13))
            .foregroundColor(ChatThemeDefaults.textPrimary)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 16)
            .padding(.vertical, 8)
            .background(ChatThemeDefaults.handoffBadge)
    }

    // MARK: - 消息列表（新消息或流式增量到达即滚到底）

    private var messageList: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(model.items.indices, id: \.self) { index in
                        itemView(model.items[index])
                            .id(listItemKey(model.items[index]))
                    }
                }
                .padding(.vertical, 8)
            }
            .onChange(of: model.items) { _ in
                if let last = model.items.last {
                    withAnimation {
                        proxy.scrollTo(listItemKey(last), anchor: .bottom)
                    }
                }
            }
        }
    }

    private func listItemKey(_ item: ChatListItem) -> String {
        switch item {
        case .welcome: return "__welcome"
        case let .bubble(bubble): return bubble.id
        }
    }

    @ViewBuilder
    private func itemView(_ item: ChatListItem) -> some View {
        switch item {
        case let .welcome(text): welcomeItem(text)
        case let .bubble(bubble): messageBubble(bubble)
        }
    }

    private func welcomeItem(_ text: String) -> some View {
        Text(text)
            .font(.system(size: 13))
            .foregroundColor(ChatThemeDefaults.textSecondary)
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .background(ChatThemeDefaults.surfaceWhite)
            .cornerRadius(12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 16)
            .padding(.vertical, 8)
    }

    private func messageBubble(_ item: ChatBubble) -> some View {
        let right = item.sender.isRightAligned
        return HStack {
            if right { Spacer(minLength: 60) }
            VStack(alignment: right ? .trailing : .leading, spacing: 0) {
                if item.isAiResponse && !right {
                    Text("AI 助手")
                        .font(.system(size: 11))
                        .foregroundColor(ChatThemeDefaults.textSecondary)
                        .padding(.bottom, 2)
                        .padding(.leading, 4)
                }
                Text(item.isStreaming ? item.content + "…" : item.content)
                    .font(.system(size: 15))
                    .foregroundColor(right ? .white : ChatThemeDefaults.textPrimary)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .background(right ? ChatThemeDefaults.bubbleCustomer : ChatThemeDefaults.bubbleOther)
                    .cornerRadius(12)
                    .frame(maxWidth: 280, alignment: right ? .trailing : .leading)
                if item.suggestsHandoff {
                    handoffButton
                }
                if !item.sources.isEmpty {
                    sourcesList(item.sources)
                }
            }
            if !right { Spacer(minLength: 60) }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 4)
    }

    /// 置信门"转人工"强调按钮（D8：next_action=handoff 驱动）。
    private var handoffButton: some View {
        Button {
            model.requestHandoff()
        } label: {
            Text("转人工")
                .font(.system(size: 13, weight: .medium))
                .foregroundColor(.white)
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
                .background(primary)
                .cornerRadius(8)
        }
        .padding(.top, 4)
    }

    /// 引用来源列表（D8：按得分降序的展开列表，score 已在映射层排好、null 归底）。
    private func sourcesList(_ sources: [SourceItem]) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text("参考来源")
                .font(.system(size: 11))
                .foregroundColor(ChatThemeDefaults.textSecondary)
            ForEach(sources.indices, id: \.self) { index in
                Text(sourceLine(sources[index]))
                    .font(.system(size: 12))
                    .foregroundColor(ChatThemeDefaults.textSecondary)
                    .lineLimit(1)
            }
        }
        .padding(.top, 4)
    }

    private func sourceLine(_ source: SourceItem) -> String {
        var line = "· \(source.title)（\(source.documentId)"
        if let score = source.score {
            line += String(format: "%.2f", score)
        }
        return line + "）"
    }

    // MARK: - 输入栏

    private func inputBar(enabled: Bool) -> some View {
        HStack(spacing: 8) {
            TextField("输入消息…", text: $inputText)
                .font(.system(size: 15))
                .foregroundColor(ChatThemeDefaults.textPrimary)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(ChatThemeDefaults.pageBackground)
                .cornerRadius(20)
            Button {
                let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
                guard !text.isEmpty else { return }
                inputText = ""
                model.send(text)
            } label: {
                Text("发送")
                    .font(.system(size: 14))
                    .foregroundColor(.white)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 8)
                    .background(canSend(enabled: enabled) ? primary : ChatThemeDefaults.divider)
                    .cornerRadius(20)
            }
            .disabled(!canSend(enabled: enabled))
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(ChatThemeDefaults.surfaceWhite)
    }

    private func canSend(enabled: Bool) -> Bool {
        enabled && !inputText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
}

/**
 * 工单创建表单（M3 刀 3b，Kotlin 镜像：TicketForm.kt；SwiftUI 全系统库，D9 零三方）：
 * 标题必填 + 描述选填；ai_summary 由门面自动组装（TicketSummary），表单不收摘要。
 * 提交经 `onSubmit`（门面 createTicket）——成功返回回执并收起表单，失败展示重试提示。
 */
private struct TicketFormOverlay: View {
    let primary: Color
    let onSubmit: (String, String?) async -> Bool
    let onDismiss: () -> Void

    @State private var titleText = ""
    @State private var detail = ""
    @State private var submitting = false
    @State private var errorText: String?

    var body: some View {
        ZStack {
            Color.black.opacity(0.45)
                .ignoresSafeArea()
                .onTapGesture { if !submitting { onDismiss() } }
            VStack(alignment: .leading, spacing: 0) {
                Text("创建工单")
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundColor(ChatThemeDefaults.textPrimary)
                Text("会话摘要将随工单一并发给客服")
                    .font(.system(size: 12))
                    .foregroundColor(ChatThemeDefaults.textSecondary)
                    .padding(.top, 4)
                TicketFormField(
                    hint: "问题标题",
                    text: $titleText,
                    enabled: !submitting,
                    singleLine: true
                )
                .padding(.top, 16)
                TicketFormField(
                    hint: "补充描述（选填）",
                    text: $detail,
                    enabled: !submitting,
                    singleLine: false
                )
                .padding(.top, 10)
                if let errorText {
                    Text(errorText)
                        .font(.system(size: 12))
                        .foregroundColor(Color(red: 0.86, green: 0.15, blue: 0.15))
                        .padding(.top, 8)
                }
                HStack {
                    Spacer()
                    Button {
                        onDismiss()
                    } label: {
                        Text("取消")
                            .font(.system(size: 14))
                            .foregroundColor(ChatThemeDefaults.textSecondary)
                            .padding(.horizontal, 12)
                            .padding(.vertical, 8)
                    }
                    .disabled(submitting)
                    Button {
                        submit()
                    } label: {
                        Text(submitting ? "提交中…" : "提交")
                            .font(.system(size: 14))
                            .foregroundColor(.white)
                            .padding(.horizontal, 16)
                            .padding(.vertical, 8)
                            .background(canSubmit ? primary : ChatThemeDefaults.divider)
                            .cornerRadius(8)
                    }
                    .disabled(!canSubmit)
                }
                .padding(.top, 16)
            }
            .padding(20)
            .background(ChatThemeDefaults.surfaceWhite)
            .cornerRadius(16)
            .padding(.horizontal, 24)
            .onTapGesture {} // 消费卡片内点击，避免穿透到 scrim 关闭
        }
    }

    private var canSubmit: Bool {
        !titleText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !submitting
    }

    private func submit() {
        let trimmedTitle = titleText.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedDetail = detail.trimmingCharacters(in: .whitespacesAndNewlines)
        submitting = true
        errorText = nil
        Task {
            let created = await onSubmit(trimmedTitle, trimmedDetail.isEmpty ? nil : trimmedDetail)
            submitting = false
            if created {
                onDismiss()
            } else {
                errorText = "创建失败，请稍后重试"
            }
        }
    }
}

private struct TicketFormField: View {
    let hint: String
    @Binding var text: String
    let enabled: Bool
    let singleLine: Bool

    var body: some View {
        Group {
            if singleLine {
                // 单行：TextField（iOS 15 兼容；axis: 参数是 iOS 16+，禁用）
                ZStack(alignment: .leading) {
                    if text.isEmpty { hintView }
                    TextField(hint, text: $text)
                        .font(.system(size: 14))
                        .foregroundColor(ChatThemeDefaults.textPrimary)
                        .disabled(!enabled)
                }
                .padding(.vertical, 4)
            } else {
                // 多行：TextEditor（iOS 14+）+ 手动 placeholder（对齐其内建内边距）
                ZStack(alignment: .topLeading) {
                    if text.isEmpty {
                        hintView.padding(.top, 8).padding(.leading, 5)
                    }
                    TextEditor(text: $text)
                        .font(.system(size: 14))
                        .foregroundColor(ChatThemeDefaults.textPrimary)
                        .frame(minHeight: 72)
                        .disabled(!enabled)
                }
            }
        }
        .padding(.horizontal, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ChatThemeDefaults.pageBackground)
        .cornerRadius(10)
    }

    private var hintView: some View {
        Text(hint)
            .font(.system(size: 14))
            .foregroundColor(ChatThemeDefaults.textSecondary)
    }
}
#endif
