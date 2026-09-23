package servify.sdk.android

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue
import kotlin.test.assertFailsWith
import servify.sdk.android.core.HandoffState
import servify.sdk.android.core.HandoffStateMachine
import servify.sdk.android.core.StreamingAssembler

private val AI_ANSWERING: HandoffState = HandoffState.AiAnswering
private val WAITING_HUMAN: HandoffState = HandoffState.WaitingHuman
private val AGENT_CHATTING: HandoffState = HandoffState.AgentChatting

/** 转移表穷举（策划 M1 验收⑤：状态机转移表必须穷举——M0 先在探针层兑现）。 */
class HandoffStateMachineTest {

    @Test
    fun transferFromAiAnsweringAndWaitingHumanAreLegal() {
        for (from in listOf(AI_ANSWERING, WAITING_HUMAN)) {
            val sm = HandoffStateMachine(from)
            assertTrue(sm.onTransferNotification(), "from=$from")
            assertEquals(AGENT_CHATTING, sm.state)
            assertEquals(listOf(from to AGENT_CHATTING), sm.transitions)
        }
    }

    @Test
    fun transferFromAgentChattingIsRejected() {
        val sm = HandoffStateMachine(AGENT_CHATTING)
        assertFalse(sm.onTransferNotification())
        assertEquals(AGENT_CHATTING, sm.state)
        assertTrue(sm.transitions.isEmpty())
    }

    @Test
    fun waitingOnlyFromAiAnsweringIsLegal() {
        val sm = HandoffStateMachine(AI_ANSWERING)
        assertTrue(sm.onWaitingNotification())
        assertEquals(WAITING_HUMAN, sm.state)

        val busy = HandoffStateMachine(AGENT_CHATTING)
        assertFalse(busy.onWaitingNotification())
        val queued = HandoffStateMachine(WAITING_HUMAN)
        // waiting_human 收到重复 waiting（重复排队）拒绝——非法转移显式暴露
        assertFalse(queued.onWaitingNotification())
        assertEquals(WAITING_HUMAN, queued.state)
    }

    @Test
    fun fullQueueThenDispatchPathMatchesServerBehaviour() {
        // 真实路径：ai_answering →(waiting)→ waiting_human →(队列派发 transfer)→ agent_chatting
        val sm = HandoffStateMachine(AI_ANSWERING)
        assertTrue(sm.onWaitingNotification())
        assertTrue(sm.onTransferNotification())
        assertEquals(
            listOf(
                AI_ANSWERING to WAITING_HUMAN,
                WAITING_HUMAN to AGENT_CHATTING,
            ),
            sm.transitions,
        )
    }
}

class StreamingAssemblerTest {

    @Test
    fun completeThreePartStreamConcatenatesAndVerifiesFinal() {
        val asm = StreamingAssembler()
        assertTrue(asm.onDelta("你好", false))
        assertTrue(asm.onDelta("，客服为您服务", false))
        assertTrue(asm.onDelta("", true))
        assertTrue(asm.onFinal("你好，客服为您服务"))
        assertEquals(listOf("你好", "，客服为您服务"), asm.contentParts)
        assertEquals("你好，客服为您服务", asm.rendered)
        assertEquals(true, asm.finalMatchesConcatenation)
        assertFalse(asm.interrupted)
        // 终态观测旗标：终末增量已到 + 终帧已到（渲染层收口判据）
        assertTrue(asm.terminalDeltaSeen)
        assertTrue(asm.finalSeen)
    }

    @Test
    fun mismatchedFinalIsRecordedNotSwallowed() {
        val asm = StreamingAssembler()
        asm.onDelta("增量内容", false)
        asm.onDelta("", true)
        assertTrue(asm.onFinal("另一段内容"))
        assertEquals(false, asm.finalMatchesConcatenation)
    }

    @Test
    fun singleShotFinalWithoutDeltasIsLegal() {
        val asm = StreamingAssembler()
        assertTrue(asm.onFinal("单发回答"))
        assertEquals("单发回答", asm.rendered)
        assertEquals(null, asm.finalMatchesConcatenation)
        // 单发直收：终帧已到、终末增量未到
        assertTrue(asm.finalSeen)
        assertFalse(asm.terminalDeltaSeen)
    }

    @Test
    fun interruptedStreamIsFlagged() {
        val asm = StreamingAssembler()
        asm.onDelta("渲染了一半", false)
        asm.onDelta("", true)
        assertTrue(asm.interrupted)
        assertEquals("渲染了一半", asm.rendered)
    }

    @Test
    fun deltasAfterTerminalViolateContract() {
        val asm = StreamingAssembler()
        assertTrue(asm.onDelta("正常增量", false))
        assertTrue(asm.onDelta("", true))
        // 终末增量之后的普通增量违反三段顺序（PROTOCOL.md §4.1）；终帧则仍是合法收尾
        assertFalse(asm.onDelta("迟到增量", false))
        assertEquals("正常增量", asm.rendered)
        assertTrue(asm.onFinal("正常增量"))
        assertFalse(asm.interrupted)
    }

    @Test
    fun terminalDeltaMustCarryEmptyContent() {
        val asm = StreamingAssembler()
        assertFailsWith<IllegalArgumentException> { asm.onDelta("非空", true) }
    }
}
