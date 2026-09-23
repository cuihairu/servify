import Testing

@testable import ServifyKit

/**
 * ChatPanelState reducer 与面板形态选择（D8）的穷举。
 * Kotlin 镜像：ChatPanelStateTest.kt——用例名逐一对应防单侧漂移。
 */
struct ChatPanelStateTests {

    private func message(id: String, sender: SenderType = .customer, content: String = "内容") -> ConversationMessage {
        ConversationMessage(
            id: id,
            sessionId: "s",
            sender: sender,
            content: content,
            createdAt: 0
        )
    }

    @Test func onMessageAppendsNewIds() {
        let state = ChatPanelState(welcomeText: nil)
        state.onMessage(message(id: "a"))
        state.onMessage(message(id: "b"))
        #expect(state.messageCount() == 2)
        let ids = state.items().compactMap { item -> String? in
            if case let .bubble(bubble) = item { return bubble.id }
            return nil
        }
        #expect(ids == ["a", "b"])
    }

    @Test func onMessageOverwritesSameIdInPlace() {
        let state = ChatPanelState(welcomeText: nil)
        state.onMessage(message(id: "a"))
        state.onMessage(message(id: "b"))
        state.onMessage(message(id: "a", content: "a-最新"))
        #expect(state.messageCount() == 2)
        let bubbles = state.items().compactMap { item -> ChatBubble? in
            if case let .bubble(bubble) = item { return bubble }
            return nil
        }
        #expect(bubbles.map { $0.id } == ["a", "b"])
        #expect(bubbles[0].content == "a-最新")
    }

    @Test func welcomeTextFeedsFirstItem() {
        let state = ChatPanelState(welcomeText: "您好")
        guard case .welcome = state.items().first else {
            Issue.record("首项应为 welcome")
            return
        }
    }

    @Test func nullWelcomeTextYieldsNoWelcomeItem() {
        let state = ChatPanelState(welcomeText: nil)
        #expect(state.items().isEmpty)
    }

    @Test func resolveStyleConfiguredFullscreenWins() {
        #expect(resolvePanelStyle(.fullscreen, screenHeightDp: 1000) == .fullscreen)
    }

    @Test func resolveStyleSmallScreenUpgradesDrawerToFullscreen() {
        #expect(resolvePanelStyle(.drawer, screenHeightDp: SMALL_SCREEN_HEIGHT_DP - 1) == .fullscreen)
        #expect(resolvePanelStyle(.drawer, screenHeightDp: SMALL_SCREEN_HEIGHT_DP) == .drawer)
    }

    @Test func customerBubblesRightAlignedOthersLeft() {
        #expect(SenderType.customer.isRightAligned)
        #expect(!SenderType.agent.isRightAligned)
        #expect(!SenderType.system.isRightAligned)
    }
}
