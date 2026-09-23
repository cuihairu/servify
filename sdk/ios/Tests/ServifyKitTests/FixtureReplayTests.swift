import XCTest
@testable import ServifyKit

/// 契约回放测试：消费 sdk/protocol-fixtures/ 全部样例（与 core、Android 回放测试同一套
/// 文件，M2 验收①），断言 iOS 协议层解码、流式拼接与转人工状态机的行为。
///
/// 每个用例的"期望转移"取自样例 expectations.state 列表——列表内是合法转移对，
/// 实际发生的转移必须命中其一。Kotlin 镜像：FixtureReplayTest.kt，用例名逐一同名。
final class FixtureReplayTests: XCTestCase {

    /// swift test 的工作目录不保证是包根，从源文件路径锚定仓库内 fixtures 目录。
    private var fixturesDir: URL {
        URL(fileURLWithPath: #filePath)                    // Tests/ServifyKitTests/FixtureReplayTests.swift
            .deletingLastPathComponent()                   // Tests/ServifyKitTests/
            .deletingLastPathComponent()                   // Tests/
            .deletingLastPathComponent()                   // 包根 sdk/ios/
            .deletingLastPathComponent()                   // sdk/
            .appendingPathComponent("protocol-fixtures")
    }

    private func loadFixtures() throws -> [[String: Any]] {
        let files = try FileManager.default.contentsOfDirectory(at: fixturesDir, includingPropertiesForKeys: nil)
            .filter { $0.pathExtension == "json" }
            .sorted { $0.lastPathComponent < $1.lastPathComponent }
        return try files.map { file in
            try JSONSerialization.jsonObject(with: Data(contentsOf: file)) as! [String: Any]
        }
    }

    private func fixtureByKind(_ kind: String, _ fixtures: [[String: Any]]) -> [String: Any] {
        fixtures.first { expectations($0)["kind"] as! String == kind }!
    }

    private func framesOf(_ fixture: [String: Any]) -> [[String: Any]] {
        (fixture["frames"] as? [[String: Any]]) ?? [fixture["frame"] as! [String: Any]]
    }

    private func replay(_ fixture: [String: Any], start state: HandoffState = .aiAnswering) throws -> [ProtocolEvent] {
        let core = SessionCore(initialState: state)
        return try framesOf(fixture).map { frame in
            try core.consume(FrameCodec.decode(serialized(frame)))
        }
    }

    private func serialized(_ obj: [String: Any]) -> String {
        let bytes = try! JSONSerialization.data(withJSONObject: obj)
        return String(data: bytes, encoding: .utf8)!
    }

    private func expectations(_ fixture: [String: Any]) -> [String: Any] {
        fixture["expectations"] as! [String: Any]
    }

    private func asserts(_ fixture: [String: Any]) -> [String: Any] {
        expectations(fixture)["assert"] as! [String: Any]
    }

    private func expect(_ fixture: [String: Any], _ key: String) -> String {
        asserts(fixture)[key] as! String
    }

    private func stateOf(_ name: String) -> HandoffState {
        switch name {
        case "ai_answering": return .aiAnswering
        case "waiting_human": return .waitingHuman
        case "agent_chatting": return .agentChatting
        case "closed": return .closed
        default: fatalError("unknown state name: \(name)")
        }
    }

    func testFixtureKindVocabularyIsFullyCovered() throws {
        // 防新样例绕过双端断言：与 core、Android 回放测试维护同一份 kind 全集
        let kinds = try Set(loadFixtures().map { expectations($0)["kind"] as! String })
        XCTAssertEqual(
            kinds,
            [
                "visitor-echo",
                "agent-message",
                "ai-final",
                "ai-stream-complete",
                "ai-stream-interrupted",
                "transfer",
                "waiting",
                "unknown-ignored",
                "webrtc-ignored-by-mobile",
            ]
        )
    }

    func testVisitorEchoCarriesExactContent() throws {
        let fixtures = try loadFixtures()
        let fixture = fixtureByKind("visitor-echo", fixtures)
        let events = try replay(fixture)
        XCTAssertEqual(events, [.visitorEcho(content: expect(fixture, "content"))])
    }

    func testAgentMessageCarriesContentAndSender() throws {
        let fixtures = try loadFixtures()
        let fixture = fixtureByKind("agent-message", fixtures)
        let events = try replay(fixture)
        XCTAssertEqual(
            events,
            [.agentMessage(content: expect(fixture, "content"), sender: expect(fixture, "sender"))]
        )
    }

    func testAiResponseFullCarriesAllOptionalFields() throws {
        let fixtures = try loadFixtures()
        let fixture = fixtures.first { $0["name"] as! String == "ai-response-full" }!
        let events = try replay(fixture)
        let ai = try XCTUnwrap(events.single)
        XCTAssertEqual(expect(fixture, "content"), ai.aiFinalContent, "assert[content]")
        // 无增量直发终帧（单发模式）：拼接一致性断言不适用
        XCTAssertNil(ai.aiFinalMatchesConcatenation)
    }

    func testAiResponseMinimalOmitsOptionalFields() throws {
        let fixtures = try loadFixtures()
        let fixture = fixtures.first { $0["name"] as! String == "ai-response-minimal" }!
        let events = try replay(fixture)
        XCTAssertEqual(events, [.aiFinal(content: expect(fixture, "content"), matchesConcatenation: nil)])
    }

    func testCompleteDeltaStreamConcatenatesAndMatchesFinal() throws {
        let fixtures = try loadFixtures()
        let fixture = fixtureByKind("ai-stream-complete", fixtures)
        let events = try replay(fixture)
        let deltas = events.compactMap { event -> (String, Bool)? in
            if case let .aiDelta(contentDelta, done) = event { return (contentDelta, done) }
            return nil
        }
        let finals = events.compactMap { event -> (String, Bool?)? in
            if case let .aiFinal(content, matches) = event { return (content, matches) }
            return nil
        }
        let assert = asserts(fixture)

        // 三段契约：普通增量 → 终末增量（done=true）→ ai-response 终帧
        let parts = assert["content_parts"] as! [String]
        XCTAssertEqual(parts, deltas.dropLast().map { $0.0 })
        XCTAssertEqual(true, deltas.last?.1)
        XCTAssertEqual("", deltas.last?.0)
        XCTAssertEqual(expect(fixture, "final_content"), finals.single?.0)
        XCTAssertEqual(true, finals.single?.1)
    }

    func testInterruptedStreamFlagsFailureWithoutFabricatingFinal() throws {
        let fixtures = try loadFixtures()
        let fixture = fixtureByKind("ai-stream-interrupted", fixtures)
        let core = SessionCore()
        let events = try framesOf(fixture).map { frame in
            try core.consume(FrameCodec.decode(serialized(frame)))
        }

        guard case .aiDelta = events.last else { return XCTFail("最后一帧应为 aiDelta") }
        XCTAssertTrue(core.streamInterrupted, "终末增量已到但无终帧 = 流中断")
    }

    func testTransferNotificationDrivesStateMachineToAgentChatting() throws {
        let fixtures = try loadFixtures()
        let fixture = fixtureByKind("transfer", fixtures)
        let assert = asserts(fixture)
        let legalFromStates = Set(
            (expectations(fixture)["state"] as! [[String: Any]]).map { pair in
                stateOf(pair["from"] as! String)
            }
        )

        // 两个合法来源（ai_answering 与 waiting_human）都必须能到达 agent_chatting
        for from in legalFromStates {
            let core = SessionCore(initialState: from)
            let event = try core.consume(FrameCodec.decode(serialized(framesOf(fixture).single!)))
            let transfer = try XCTUnwrap(event.transferPayload)
            XCTAssertEqual(HandoffState.agentChatting, core.handoff.state)
            XCTAssertTrue(transfer.stateAccepted)
            XCTAssertEqual([HandoffTransition(from: from, to: .agentChatting)], core.handoff.transitions)
            XCTAssertEqual(Int64((assert["agent_id"] as! NSNumber).int64Value), transfer.agentId)
        }
    }

    func testWaitingNotificationDrivesStateMachineToWaitingHuman() throws {
        let fixtures = try loadFixtures()
        let fixture = fixtureByKind("waiting", fixtures)
        let core = SessionCore()
        let event = try core.consume(FrameCodec.decode(serialized(framesOf(fixture).single!)))
        let waiting = try XCTUnwrap(event.waitingPayload)

        XCTAssertTrue(waiting.stateAccepted)
        XCTAssertEqual(HandoffState.waitingHuman, core.handoff.state)
        XCTAssertEqual(expect(fixture, "message"), waiting.message)
    }

    func testUnknownDeadBranchFrameIsIgnoredSilently() throws {
        let fixtures = try loadFixtures()
        let events = try replay(fixtureByKind("unknown-ignored", fixtures))
        XCTAssertEqual(events, [.unknownIgnored(type: "session_update")])
    }

    func testWebrtcSignalIsExplicitlyIgnoredByMobileContract() throws {
        let fixtures = try loadFixtures()
        let events = try replay(fixtureByKind("webrtc-ignored-by-mobile", fixtures))
        XCTAssertEqual(events, [.webRtcIgnored(signalType: "webrtc-offer")])
    }
}

// MARK: - 断言助手（镜像 Kotlin 侧的 smart-cast 取值；成员名不能与 enum case 同名，
// transfer/waiting 用 Payload 后缀避撞）

private extension ProtocolEvent {
    var aiFinalContent: String? {
        if case let .aiFinal(content, _) = self { return content }
        return nil
    }

    var aiFinalMatchesConcatenation: Bool? {
        if case let .aiFinal(_, matches) = self { return matches }
        return nil
    }

    var transferPayload: (agentId: Int64, message: String, stateAccepted: Bool)? {
        if case let .transferReceived(agentId, message, accepted) = self {
            return (agentId, message, accepted)
        }
        return nil
    }

    var waitingPayload: (message: String, stateAccepted: Bool)? {
        if case let .waitingReceived(message, accepted) = self { return (message, accepted) }
        return nil
    }
}

private extension Array {
    var single: Element? { count == 1 ? first : nil }
}
