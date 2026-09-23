import Testing

@testable import ServifyKit

/**
 * toChatListItems 的渲染规则穷举（D5/D8：欢迎语、流式合并、终帧替换、来源排序、
 * 置信门）。Kotlin 镜像：ChatUiStateTest.kt——用例名逐一对应防单侧漂移。
 */
struct ChatUiStateTests {

    private func message(
        id: String,
        sender: SenderType = .customer,
        content: String = "内容",
        isAi: Bool = false,
        isStreaming: Bool = false,
        nextAction: String? = nil,
        sources: [KnowledgeSource] = []
    ) -> ConversationMessage {
        ConversationMessage(
            id: id,
            sessionId: "s",
            sender: sender,
            content: content,
            createdAt: 0,
            isAiResponse: isAi,
            isStreaming: isStreaming,
            sources: sources,
            confidence: isAi ? 0.9 : nil,
            nextAction: nextAction
        )
    }

    @Test func emptyMessagesYieldOnlyWelcomeWhenPresent() {
        let items = toChatListItems(messages: [], welcomeText: "您好")
        #expect(items == [.welcome(text: "您好")])
    }

    @Test func blankWelcomeTextOmitsWelcomeItem() {
        #expect(toChatListItems(messages: [], welcomeText: "").isEmpty)
        #expect(toChatListItems(messages: [], welcomeText: nil).isEmpty)
    }

    @Test func streamingIntermediateStatesMergeByIdKeepingLatest() {
        let items = toChatListItems(
            messages: [
                message(id: "ws-1", sender: .customer, content: "问"),
                message(id: "m2", sender: .system, content: "根据", isAi: true, isStreaming: true),
                message(id: "m2", sender: .system, content: "根据退货", isAi: true, isStreaming: true),
            ],
            welcomeText: nil
        )
        #expect(items.count == 2)
        guard case let .bubble(bubble) = items[1] else {
            Issue.record("items[1] 应为 bubble")
            return
        }
        #expect(bubble.content == "根据退货")
        #expect(bubble.isStreaming)
    }

    @Test func finalFrameReplacesStreamingBubbleWithSameId() {
        let items = toChatListItems(
            messages: [
                message(id: "m2", sender: .system, content: "根据退货", isAi: true, isStreaming: true),
                message(id: "m2", sender: .system, content: "根据退货政策。", isAi: true),
            ],
            welcomeText: nil
        )
        #expect(items.count == 1)
        guard case let .bubble(bubble) = items[0] else {
            Issue.record("items[0] 应为 bubble")
            return
        }
        #expect(bubble.content == "根据退货政策。")
        #expect(!bubble.isStreaming)
        #expect(bubble.isAiResponse)
    }

    @Test func sourcesSortByScoreDescending() {
        let items = toChatListItems(
            messages: [
                message(
                    id: "m2", sender: .system, content: "答", isAi: true,
                    sources: [
                        KnowledgeSource(documentId: "low", title: "低分文档", content: nil, score: 0.3),
                        KnowledgeSource(documentId: "high", title: "高分文档", content: nil, score: 0.95),
                        KnowledgeSource(documentId: "mid", title: "中文档", content: nil, score: 0.6),
                    ]
                ),
            ],
            welcomeText: nil
        )
        guard case let .bubble(bubble) = items[0] else {
            Issue.record("items[0] 应为 bubble")
            return
        }
        #expect(bubble.sources.map { $0.documentId } == ["high", "mid", "low"])
    }

    @Test func handoffSuggestionCarriesEmphasisFlag() {
        let items = toChatListItems(
            messages: [message(id: "m2", sender: .system, content: "答", isAi: true, nextAction: "handoff")],
            welcomeText: nil
        )
        guard case let .bubble(bubble) = items[0] else {
            Issue.record("items[0] 应为 bubble")
            return
        }
        #expect(bubble.suggestsHandoff)
    }

    @Test func nonHandoffNextActionIsNotEmphasized() {
        let items = toChatListItems(
            messages: [message(id: "m2", sender: .system, content: "答", isAi: true, nextAction: "continue")],
            welcomeText: nil
        )
        guard case let .bubble(bubble) = items[0] else {
            Issue.record("items[0] 应为 bubble")
            return
        }
        #expect(!bubble.suggestsHandoff)
    }

    @Test func messageOrderPreservedAfterMerge() {
        let items = toChatListItems(
            messages: [
                message(id: "a", sender: .customer, content: "1"),
                message(id: "b", sender: .system, content: "2", isAi: true),
                message(id: "c", sender: .agent, content: "3"),
                message(id: "a", sender: .customer, content: "1-edit"), // 同 id 后到覆盖，位置保持
            ],
            welcomeText: nil
        )
        let bubbles = items.compactMap { item -> ChatBubble? in
            if case let .bubble(bubble) = item { return bubble }
            return nil
        }
        #expect(bubbles.map { $0.id } == ["a", "b", "c"])
        #expect(bubbles[0].content == "1-edit")
        #expect(bubbles[2].sender == .agent)
    }
}
