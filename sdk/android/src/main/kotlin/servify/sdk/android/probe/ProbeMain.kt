package servify.sdk.android.probe

import servify.sdk.android.core.HandoffState
import servify.sdk.android.core.ProtocolEvent
import servify.sdk.android.core.SessionCore
import servify.sdk.android.protocol.FrameCodec
import java.net.URI
import java.net.http.HttpClient
import java.net.http.WebSocket
import java.util.concurrent.CompletionStage
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import kotlin.system.exitProcess

/**
 * M0 联调探针 CLI：对真实服务器跑全链路（策划文档 M0 验收②）——
 * 建连 → 客户消息回显 → AI 首答流式（delta 三段） → 转人工（transfer/waiting）
 * → 坐席回复（agent-message）。
 *
 * 环境变量：
 *   PROBE_WS_URL   完整 WS 地址（含 session_id 参数），必填
 *   PROBE_TIMEOUT  单步等待秒数，默认 60
 *
 * 退出码 0 = 全链路通过；非 0 = 某步失败（stdout 打印 PROBE_FAIL）。
 * 帧的实际校验复用 SessionCore（与 fixtures 回放同一消费路径）。
 */
fun main() {
    val url = System.getenv("PROBE_WS_URL") ?: run {
        println("PROBE_FAIL missing PROBE_WS_URL")
        exitProcess(2)
    }
    val timeoutSec = System.getenv("PROBE_TIMEOUT")?.toLongOrNull() ?: 60L

    val sessionId = Regex("session_id=([^&]+)").find(url)?.groupValues?.lastOrNull() ?: "probe-session"
    val core = SessionCore()
    val events = mutableListOf<ProtocolEvent>()
    val stage = ProbeStage()

    val client = HttpClient.newHttpClient()
    val listener = object : WebSocket.Listener {
        private val buffer = StringBuilder()

        override fun onText(webSocket: WebSocket, data: CharSequence, last: Boolean): CompletionStage<*>? {
            buffer.append(data)
            if (last) {
                val frame = FrameCodec.decode(buffer.toString())
                buffer.clear()
                val event = core.consume(frame)
                events += event
                stage.onEvent(event)
            }
            webSocket.request(1)
            return null
        }

        override fun onError(webSocket: WebSocket, error: Throwable) {
            stage.onTransportError(error)
        }
    }

    println("PROBE_STEP connect url=$url")
    val webSocket = client.newWebSocketBuilder()
        .buildAsync(URI.create(url), listener)
        .join()

    // 1) 客户消息 → 期望回显 + AI 首答（流式或单发）
    println("PROBE_STEP send-visitor-message")
    webSocket.sendText(FrameCodec.encodeTextMessage("你好，我想咨询退货政策", sessionId), true).join()
    if (!stage.await("visitor-echo", timeoutSec) || !stage.await("ai-final", timeoutSec)) {
        probeFail(events)
    }
    println("PROBE_STEP ai-answer-complete streaming_deltas=${events.count { it is ProtocolEvent.AiDelta }}")
    if (core.streamInterrupted) {
        println("PROBE_FAIL ai stream interrupted (terminal delta without final frame)")
        exitProcess(3)
    }

    // 2) 转人工关键词 → transfer_notification（坐席在线直转）或 waiting_notification（排队）
    println("PROBE_STEP send-transfer-keyword")
    webSocket.sendText(FrameCodec.encodeTextMessage("转人工", sessionId), true).join()
    if (!stage.awaitAny(listOf("transfer", "waiting"), timeoutSec)) {
        probeFail(events)
    }
    val handoffEvent = events.first { it is ProtocolEvent.TransferReceived || it is ProtocolEvent.WaitingReceived }
    println("PROBE_STEP handoff-state state=${core.handoff.state} event=$handoffEvent")

    // 3) 坐席回复（验收脚本经管理面 omni API 注入）→ agent-message
    if (!stage.await("agent-message", timeoutSec)) {
        probeFail(events)
    }

    // 4) 状态机终态必须是 agent_chatting（transfer 直转或队列派发最终都到这）
    if (core.handoff.state != HandoffState.AgentChatting) {
        println("PROBE_FAIL final handoff state=${core.handoff.state} (expected agent_chatting)")
        exitProcess(4)
    }

    println("PROBE_OK full chain: connect → echo → ai-answer → handoff → agent-reply")
    webSocket.sendClose(WebSocket.NORMAL_CLOSURE, "probe done").join()
    exitProcess(0)
}

private fun probeFail(events: List<ProtocolEvent>) {
    println("PROBE_FAIL steps not completed; events seen:")
    events.forEach { println("  $it") }
    exitProcess(5)
}

/** 探针步骤闸门：事件到达即放行对应 latch。 */
private class ProbeStage {
    private val latch = mapOf(
        "visitor-echo" to CountDownLatch(1),
        "ai-final" to CountDownLatch(1),
        "transfer" to CountDownLatch(1),
        "waiting" to CountDownLatch(1),
        "agent-message" to CountDownLatch(1),
    )
    @Volatile private var transportError: Throwable? = null

    fun onEvent(event: ProtocolEvent) {
        when (event) {
            is ProtocolEvent.VisitorEcho -> latch["visitor-echo"]?.countDown()
            is ProtocolEvent.AiFinal -> latch["ai-final"]?.countDown()
            is ProtocolEvent.TransferReceived -> latch["transfer"]?.countDown()
            is ProtocolEvent.WaitingReceived -> latch["waiting"]?.countDown()
            is ProtocolEvent.AgentMessage -> latch["agent-message"]?.countDown()
            else -> Unit
        }
    }

    fun onTransportError(error: Throwable) {
        transportError = error
        latch.values.forEach { it.countDown() }
    }

    fun await(stageName: String, timeoutSec: Long): Boolean {
        if (!latch.getValue(stageName).await(timeoutSec, TimeUnit.SECONDS)) {
            println("PROBE_FAIL timeout waiting for $stageName (transportError=$transportError)")
            return false
        }
        transportError?.let { println("PROBE_FAIL transport error while waiting $stageName: $it"); return false }
        return true
    }

    fun awaitAny(names: List<String>, timeoutSec: Long): Boolean {
        val deadline = System.nanoTime() + timeoutSec * 1_000_000_000
        for (name in names) {
            val remaining = TimeUnit.NANOSECONDS.toSeconds(deadline - System.nanoTime()).coerceAtLeast(1)
            if (latch.getValue(name).await(remaining, TimeUnit.SECONDS)) {
                transportError?.let { println("PROBE_FAIL transport error while waiting $name: $it"); return false }
                return true
            }
        }
        println("PROBE_FAIL timeout waiting for any of $names")
        return false
    }
}
