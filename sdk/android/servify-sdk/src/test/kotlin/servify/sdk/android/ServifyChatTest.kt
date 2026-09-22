package servify.sdk.android

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.onSubscription
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import okhttp3.OkHttpClient
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.SocketPolicy
import org.junit.After
import org.junit.Before
import org.junit.Test
import servify.sdk.android.connect.ConnectionState
import servify.sdk.android.connect.ReconnectPolicy
import servify.sdk.android.model.AgentAssignment
import servify.sdk.android.model.SenderType
import kotlin.test.assertEquals
import kotlin.test.assertIs
import kotlin.test.assertTrue

/**
 * 门面接线集成测试：MockWebServer 的 WS 升级承载真实 OkHttp WebSocket 行为，
 * 逐项验证回显判据、流式拼装、转人工事件、未读语义、握手失败与断线重连。
 *
 * SharedFlow 无 replay，先 onSubscription 挂就绪闸门再触发动作，杜绝订阅竞态丢帧。
 */
class ServifyChatTest {

    private lateinit var server: MockWebServer
    private lateinit var chatScope: CoroutineScope
    private lateinit var chat: ServifyChat

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
        chatScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    }

    @After
    fun tearDown() {
        if (::chat.isInitialized) chat.destroy()
        chatScope.cancel()
        server.shutdown()
    }

    private fun newChat(echoTimeoutMs: Long = 500, policy: ReconnectPolicy = ReconnectPolicy(maxAttempts = 3, initialDelayMs = 100, multiplier = 2, maxDelayMs = 400)): ServifyChat =
        ServifyChat(
            config = ServifyConfig(apiUrl = "https://chat.example.com"),
            sessionId = "test-session",
            client = OkHttpClient(),
            scope = chatScope,
            policy = policy,
            echoTimeoutMs = echoTimeoutMs,
            wsUrlOverride = server.url("/api/v1/ws").toString(),
        )

    /** 订阅就绪后返回事件；ready 在 collector 真正挂上后放行调用方。 */
    private fun <T> SharedFlow<T>.awaitWhenSubscribed(ready: CompletableDeferred<Unit>, pred: (T) -> Boolean = { true }): Deferred<T> =
        testScope.async { onSubscription { ready.complete(Unit) }.first(pred) }

    private val testScope get() = chatScope

    private suspend fun awaitConnected() {
        withTimeout(5_000) { chat.connectionState.first { it == ConnectionState.Connected } }
    }

    private suspend fun CompletableDeferred<Unit>.awaitReady() {
        withTimeout(5_000) { await() }
    }

    /** 服务端 WS 监听器：把收到的 text-message 内容原样以回显帧发回。 */
    private class EchoListener : WebSocketListener() {
        override fun onMessage(webSocket: WebSocket, text: String) {
            webSocket.send(
                """{"type":"text-message","data":{"content":"你好"},"session_id":"test-session","timestamp":"2026-01-01T00:00:00Z"}""",
            )
        }
    }

    @Test
    fun sendMessageCompletesOnEchoAndEmitsCustomerMessage() = runBlocking {
        server.enqueue(MockResponse().withWebSocketUpgrade(EchoListener()))
        chat = newChat()

        val ready = CompletableDeferred<Unit>()
        val echoed = chat.messages.awaitWhenSubscribed(ready) { it.sender == SenderType.Customer }
        ready.awaitReady()

        chat.connect()
        awaitConnected()
        chat.sendMessage("你好")

        assertEquals("你好", withTimeout(5_000) { echoed.await() }.content)
        assertEquals(ConnectionState.Connected, chat.connectionState.value)
    }

    @Test
    fun sendMessageTimesOutWithoutEchoAndEmitsSendTimeout() = runBlocking {
        server.enqueue(MockResponse().withWebSocketUpgrade(object : WebSocketListener() {}))
        chat = newChat(echoTimeoutMs = 250)

        val ready = CompletableDeferred<Unit>()
        val failure = chat.error.awaitWhenSubscribed(ready)
        ready.awaitReady()

        chat.connect()
        awaitConnected()
        chat.sendMessage("无人回显")

        val error = withTimeout(5_000) { failure.await() }
        assertIs<ServifyError.SendTimeout>(error)
        assertTrue(error.retryable)
    }

    @Test
    fun handshake500MarksServerUnavailableAndDisconnected() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(500))
        chat = newChat()

        val ready = CompletableDeferred<Unit>()
        val failure = chat.error.awaitWhenSubscribed(ready)
        ready.awaitReady()

        chat.connect()

        assertIs<ServifyError.ServerUnavailable>(withTimeout(5_000) { failure.await() })
        assertEquals(ConnectionState.Disconnected, withTimeout(5_000) { chat.connectionState.first { it == ConnectionState.Disconnected } })
    }

    @Test
    fun deltaStreamAssemblesBubblesThenFinalCarriesSources() = runBlocking {
        server.enqueue(
            MockResponse().withWebSocketUpgrade(object : WebSocketListener() {
                override fun onOpen(webSocket: WebSocket, response: okhttp3.Response) {
                    webSocket.send("""{"type":"ai-response-delta","data":{"content_delta":"根据","done":false}}""")
                    webSocket.send("""{"type":"ai-response-delta","data":{"content_delta":"退货政策。","done":false}}""")
                    webSocket.send(
                        """{"type":"ai-response","data":{"content":"根据退货政策。","confidence":0.87,"source":"knowledge","sources":[{"document_id":"d1","title":"退货政策","score":0.9}]}}""",
                    )
                }
            }),
        )
        chat = newChat()

        val ready = CompletableDeferred<Unit>()
        val streamed = chat.messages.awaitWhenSubscribed(ready) { it.isStreaming }
        val final = chat.messages.awaitWhenSubscribed(CompletableDeferred()) { it.isAiResponse && !it.isStreaming }
        ready.awaitReady()

        chat.connect()

        val streaming = withTimeout(5_000) { streamed.await() }
        assertEquals("根据", streaming.content)
        assertEquals(SenderType.System, streaming.sender)

        val fin = withTimeout(5_000) { final.await() }
        assertEquals(streaming.id, fin.id)
        assertEquals("根据退货政策。", fin.content)
        assertEquals(0.87, fin.confidence)
        assertEquals(1, fin.sources.size)
        assertEquals("d1", fin.sources.single().documentId)
    }

    @Test
    fun agentMessagesDriveUnreadAndVisibleClearsIt() = runBlocking {
        server.enqueue(
            MockResponse().withWebSocketUpgrade(object : WebSocketListener() {
                override fun onOpen(webSocket: WebSocket, response: okhttp3.Response) {
                    webSocket.send("""{"type":"agent-message","data":{"content":"第一条","sender":"MP Agent"}}""")
                    webSocket.send("""{"type":"agent-message","data":{"content":"第二条","sender":"MP Agent"}}""")
                }
            }),
        )
        chat = newChat()

        val ready = CompletableDeferred<Unit>()
        val second = chat.messages.awaitWhenSubscribed(ready) { it.content == "第二条" }
        ready.awaitReady()

        chat.connect()

        withTimeout(5_000) { second.await() }
        assertEquals(2, chat.unreadCount.value)

        chat.onSessionVisible()
        assertEquals(0, chat.unreadCount.value)
    }

    @Test
    fun transferNotificationEmitsAgentAssignment() = runBlocking {
        server.enqueue(
            MockResponse().withWebSocketUpgrade(object : WebSocketListener() {
                override fun onOpen(webSocket: WebSocket, response: okhttp3.Response) {
                    webSocket.send(
                        """{"type":"transfer_notification","data":{"message":"已为您转接人工客服","agent_id":42},"session_id":"test-session"}""",
                    )
                }
            }),
        )
        chat = newChat()

        val ready = CompletableDeferred<Unit>()
        val assignment = chat.agentAssigned.awaitWhenSubscribed(ready)
        ready.awaitReady()

        chat.connect()

        assertEquals(AgentAssignment(42, "已为您转接人工客服"), withTimeout(5_000) { assignment.await() })
    }

    @Test
    fun waitingNotificationEmitsQueueEvent() = runBlocking {
        server.enqueue(
            MockResponse().withWebSocketUpgrade(object : WebSocketListener() {
                override fun onOpen(webSocket: WebSocket, response: okhttp3.Response) {
                    webSocket.send("""{"type":"waiting_notification","data":{"message":"前方还有 1 位等待"},"session_id":"test-session"}""")
                }
            }),
        )
        chat = newChat()

        val ready = CompletableDeferred<Unit>()
        val waiting = chat.waitingInQueue.awaitWhenSubscribed(ready)
        ready.awaitReady()

        chat.connect()

        assertEquals("前方还有 1 位等待", withTimeout(5_000) { waiting.await() })
    }

    @Test
    fun reconnectsAfterServerDrop() = runBlocking {
        // 服务器在 onMessage 里暴力 cancel 模拟异常断线。MockWebServer 对升级后的连接，
        // close(1000)/SocketPolicy 均不传播为客户端回调（handleWebSocketUpgrade 同步
        // loopReader 阻塞，DISCONNECT_AT_END 的 socket.close() 执行不到；onOpen 时序上
        // 服务端 writer 尚未建立）；onMessage 里 cancel() 是实测唯一可靠断开。
        server.enqueue(
            MockResponse().withWebSocketUpgrade(object : WebSocketListener() {
                override fun onMessage(webSocket: WebSocket, text: String) {
                    webSocket.cancel()
                }
            }),
        )
        server.enqueue(MockResponse().withWebSocketUpgrade(EchoListener()))
        chat = newChat()

        chat.connect()
        awaitConnected()

        // 触发断线； onFailure → §4.4 connected ─断→ reconnecting(n) 自动恢复
        val reconnected = async {
            val reconnecting = withTimeout(5_000) {
                chat.connectionState.first { it is ConnectionState.Reconnecting }
            }
            withTimeout(5_000) {
                chat.connectionState.first { it == ConnectionState.Connected }
            }
            reconnecting
        }
        chat.sendMessage("触发服务端断开") // 回显不来，发送侧按 SendTimeout 超时收尾即可

        val reconnecting = reconnected.await() as ConnectionState.Reconnecting
        assertEquals(1, reconnecting.attempt)
        assertEquals(ConnectionState.Connected, chat.connectionState.value)
    }

    @Test
    fun destroyIsIdempotentAndMarksDisconnected() = runBlocking {
        server.enqueue(MockResponse().withWebSocketUpgrade(EchoListener()))
        chat = newChat()
        chat.connect()
        awaitConnected()

        chat.destroy()
        chat.destroy()
        assertEquals(ConnectionState.Disconnected, chat.connectionState.value)
    }
}
