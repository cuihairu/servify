package servify.sdk.android

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import java.io.File
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertIs
import kotlin.test.assertTrue
import servify.sdk.android.core.HandoffState
import servify.sdk.android.core.ProtocolEvent
import servify.sdk.android.core.SessionCore
import servify.sdk.android.protocol.FrameCodec
import servify.sdk.android.protocol.WireFrame

/**
 * 契约回放测试：消费 sdk/protocol-fixtures/ 全部样例（与 core 回放测试同一套文件，
 * M0 验收①），断言 Android 协议层解码、流式拼接与转人工状态机的行为。
 *
 * 每个用例的"期望转移"取自样例 expectations.state 列表——列表内是合法转移对，
 * 实际发生的转移必须命中其一；断言与 core 共同口径由 expectations.kind 决定。
 */
private val AGENT_CHATTING: HandoffState = HandoffState.AgentChatting

class FixtureReplayTest {

    private val fixturesDir = File("../../protocol-fixtures")

    private fun loadFixtures(): List<JsonObject> =
        fixturesDir.listFiles()!!
            .filter { it.name.endsWith(".json") }
            .sortedBy { it.name }
            .map { Json.parseToJsonElement(it.readText()).jsonObject }

    private fun fixtureByKind(kind: String): JsonObject =
        loadFixtures().first { it["expectations"]!!.jsonObject["kind"]!!.jsonPrimitive.content == kind }

    private fun framesOf(fixture: JsonObject): List<JsonObject> =
        (fixture["frames"] as? JsonArray)?.map { it.jsonObject }
            ?: listOf(fixture["frame"]!!.jsonObject)

    private fun replay(fixture: JsonObject, startState: HandoffState = HandoffState.AiAnswering): List<ProtocolEvent> {
        val core = SessionCore(startState)
        return framesOf(fixture).map { frame ->
            core.consume(FrameCodec.decode(frame.toString()))
        }
    }

    private fun assertString(assert: JsonObject, key: String, actual: String) {
        assertEquals(assert[key]!!.jsonPrimitive.content, actual, "assert[$key]")
    }

    @Test
    fun fixtureKindVocabularyIsFullyCovered() {
        // 防新样例绕过双端断言：与 core 回放测试维护同一份 kind 全集
        val kinds = loadFixtures().map { it["expectations"]!!.jsonObject["kind"]!!.jsonPrimitive.content }.toSet()
        assertEquals(
            setOf(
                "visitor-echo",
                "agent-message",
                "ai-final",
                "ai-stream-complete",
                "ai-stream-interrupted",
                "transfer",
                "waiting",
                "unknown-ignored",
                "webrtc-ignored-by-mobile",
                "message-translated",
            ),
            kinds,
        )
    }

    @Test
    fun visitorEchoCarriesExactContent() {
        val fixture = fixtureByKind("visitor-echo")
        val events = replay(fixture)
        assertEquals(listOf(ProtocolEvent.VisitorEcho(fixture.expect("content"))), events)
    }

    @Test
    fun agentMessageCarriesContentAndSender() {
        val fixture = fixtureByKind("agent-message")
        val events = replay(fixture)
        assertEquals(
            listOf(
                ProtocolEvent.AgentMessage(fixture.expect("content"), fixture.expect("sender")),
            ),
            events,
        )
    }

    @Test
    fun aiResponseFullCarriesAllOptionalFields() {
        val fixture = loadFixtures().first { it["name"]!!.jsonPrimitive.content == "ai-response-full" }
        val events = replay(fixture)
        val ai = events.single() as ProtocolEvent.AiFinal
        assertString(fixture.asserts(), "content", ai.content)
        // 无增量直发终帧（单发模式）：拼接一致性断言不适用
        assertEquals(null, ai.matchesConcatenation)
    }

    @Test
    fun aiResponseMinimalOmitsOptionalFields() {
        val fixture = loadFixtures().first { it["name"]!!.jsonPrimitive.content == "ai-response-minimal" }
        val events = replay(fixture)
        assertEquals(listOf(ProtocolEvent.AiFinal(fixture.expect("content"), null)), events)
    }

    @Test
    fun completeDeltaStreamConcatenatesAndMatchesFinal() {
        val fixture = fixtureByKind("ai-stream-complete")
        val events = replay(fixture)
        val deltas = events.filterIsInstance<ProtocolEvent.AiDelta>()
        val finals = events.filterIsInstance<ProtocolEvent.AiFinal>()
        val assert = fixture.asserts()

        // 三段契约：普通增量 → 终末增量（done=true）→ ai-response 终帧
        val parts = assert["content_parts"]!!.jsonArray.map { it.jsonPrimitive.content }
        assertEquals(parts, deltas.dropLast(1).map { it.contentDelta })
        assertTrue(deltas.last().done)
        assertEquals("", deltas.last().contentDelta)
        assertEquals(fixture.expect("final_content"), finals.single().content)
        assertEquals(true, finals.single().matchesConcatenation)
    }

    @Test
    fun interruptedStreamFlagsFailureWithoutFabricatingFinal() {
        val fixture = fixtureByKind("ai-stream-interrupted")
        val core = SessionCore()
        val events = framesOf(fixture).map { core.consume(FrameCodec.decode(it.toString())) }

        assertTrue(events.last() is ProtocolEvent.AiDelta)
        assertTrue(core.streamInterrupted, "终末增量已到但无终帧 = 流中断")
    }

    @Test
    fun transferNotificationDrivesStateMachineToAgentChatting() {
        val fixture = fixtureByKind("transfer")
        val assert = fixture.asserts()
        val legalPairs = fixture.expectations()["state"]!!.jsonArray.map { pair ->
            val obj = pair.jsonObject
            stateOf(obj["from"]!!.jsonPrimitive.content) to stateOf(obj["to"]!!.jsonPrimitive.content)
        }

        // 两个合法来源（ai_answering 与 waiting_human）都必须能到达 agent_chatting
        for (from in legalPairs.map { it.first }.distinct()) {
            val core = SessionCore(from)
            val event = core.consume(FrameCodec.decode(framesOf(fixture).single().toString()))
            val transfer = event as ProtocolEvent.TransferReceived
            assertEquals(AGENT_CHATTING, core.handoff.state)
            assertTrue(transfer.stateAccepted)
            assertEquals(from to AGENT_CHATTING, core.handoff.transitions.single())
            assertEquals((assert["agent_id"]!!.jsonPrimitive.content).toLong(), transfer.agentId)
        }
    }

    @Test
    fun waitingNotificationDrivesStateMachineToWaitingHuman() {
        val fixture = fixtureByKind("waiting")
        val core = SessionCore()
        val event = core.consume(FrameCodec.decode(framesOf(fixture).single().toString()))
        val waiting = event as ProtocolEvent.WaitingReceived

        assertTrue(waiting.stateAccepted)
        assertEquals(HandoffState.WaitingHuman, core.handoff.state)
        assertString(fixture.asserts(), "message", waiting.message)
    }

    @Test
    fun unknownDeadBranchFrameIsIgnoredSilently() {
        val fixture = fixtureByKind("unknown-ignored")
        val events = replay(fixture)
        assertEquals(listOf(ProtocolEvent.UnknownIgnored("session_update")), events)
    }

    @Test
    fun messageTranslatedIsParallelAnnotationIgnoredByMobile() {
        // message-translated（PROTOCOL.md §4.5）：移动端契约 = 按未知类型静默
        // 忽略（UnknownIgnored），消费半边后续刀接入。
        val fixture = fixtureByKind("message-translated")
        val events = replay(fixture)
        assertEquals(listOf(ProtocolEvent.UnknownIgnored("message-translated")), events)
    }

    @Test
    fun webrtcSignalIsExplicitlyIgnoredByMobileContract() {
        val fixture = fixtureByKind("webrtc-ignored-by-mobile")
        val events = replay(fixture)
        assertEquals(listOf(ProtocolEvent.WebRtcIgnored("webrtc-offer")), events)
    }

    // ---- FrameCodec 解码边界（fixtures 之外的畸形/变体输入，容错语义见 PROTOCOL §3） ----

    @Test
    fun malformedJsonDecodesToUnknownWithoutThrowing() {
        val frame = FrameCodec.decode("{not json")
        assertIs<WireFrame.Unknown>(frame)
        assertEquals("<malformed-json>", frame.type)
        assertEquals(null, frame.sessionId)
    }

    @Test
    fun missingTypeDecodesToUnknown() {
        // missing-type 分支不透传 sessionId（解码器在 type 判定前即短路）
        val frame = FrameCodec.decode("""{"data":{},"session_id":"s1"}""")
        assertIs<WireFrame.Unknown>(frame)
        assertEquals("<missing-type>", frame.type)
        assertEquals(null, frame.sessionId)
    }

    @Test
    fun textMessageBareStringDataDecodesToVisitorEcho() {
        // data 双形态之裸字符串（PROTOCOL §3，服务端两种都接受）
        val frame = FrameCodec.decode("""{"type":"text-message","data":"直接文本","session_id":"s1"}""")
        assertIs<WireFrame.VisitorEcho>(frame)
        assertEquals("直接文本", frame.content)
    }

    @Test
    fun textMessageWithoutDataDecodesToUnknown() {
        // data 缺失（null）三分支的 else 面：无法构造回显判据 → Unknown 而非抛错
        val frame = FrameCodec.decode("""{"type":"text-message","session_id":"s1"}""")
        assertIs<WireFrame.Unknown>(frame)
        assertEquals("text-message", frame.type)
    }

    @Test
    fun aiResponseMissingConfidenceDecodesToUnknown() {
        // 终帧基础三字段缺一即 Unknown（confidence 是成功判据的必需面）
        val frame = FrameCodec.decode("""{"type":"ai-response","data":{"content":"x","source":"kb"}}""")
        assertIs<WireFrame.Unknown>(frame)
        assertEquals("ai-response", frame.type)
    }

    @Test
    fun transferNotificationMissingAgentIdDecodesToUnknown() {
        val frame = FrameCodec.decode("""{"type":"transfer_notification","data":{"message":"m"}}""")
        assertIs<WireFrame.Unknown>(frame)
        assertEquals("transfer_notification", frame.type)
    }

    private fun JsonObject.expectations(): JsonObject = this["expectations"]!!.jsonObject

    private fun JsonObject.asserts(): JsonObject = expectations()["assert"]!!.jsonObject

    private fun JsonObject.expect(key: String): String =
        expectations()["assert"]!!.jsonObject[key]!!.jsonPrimitive.content

    private fun stateOf(name: String): HandoffState = when (name) {
        "ai_answering" -> HandoffState.AiAnswering
        "waiting_human" -> HandoffState.WaitingHuman
        "agent_chatting" -> HandoffState.AgentChatting
        "closed" -> HandoffState.Closed
        else -> error("unknown state name: $name")
    }
}
