package servify.sdk.android

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
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
        withTimeout(5_000) { chat.events.connectionState.first { it == ConnectionState.Connected } }
    }

    private suspend fun CompletableDeferred<Unit>.awaitReady() {
        withTimeout(5_000) { await() }
    }

    /**
     * 未读 StateFlow 的 deadline 轮询：handleFrame 内 emit(messages) 与 bumpUnread
     * 之间存在窗口——collector 收到事件返回不代表同帧的 bumpUnread 已执行
     * （MockWebServer 回调在 OkHttp 线程、断言在 runBlocking 线程，跨线程派发序无
     * 保证）。CI 高负载偶发（2026-09-23 实锤：release 变体 AssertionError），与
     * Go 侧"先落库后发事件窗口"同型——事件断言要 deadline 轮询，不裸读 value。
     */
    private suspend fun awaitUnread(expected: Int) {
        withTimeout(5_000) {
            while (chat.events.unreadCount.value != expected) delay(50)
        }
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
        val echoed = chat.events.messages.awaitWhenSubscribed(ready) { it.sender == SenderType.Customer }
        ready.awaitReady()

        chat.connect()
        awaitConnected()
        chat.sendMessage("你好")

        assertEquals("你好", withTimeout(5_000) { echoed.await() }.content)
        assertEquals(ConnectionState.Connected, chat.events.connectionState.value)
    }

    @Test
    fun sendMessageTimesOutWithoutEchoAndEmitsSendTimeout() = runBlocking {
        server.enqueue(MockResponse().withWebSocketUpgrade(object : WebSocketListener() {}))
        chat = newChat(echoTimeoutMs = 250)

        val ready = CompletableDeferred<Unit>()
        val failure = chat.events.error.awaitWhenSubscribed(ready)
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
        val failure = chat.events.error.awaitWhenSubscribed(ready)
        ready.awaitReady()

        chat.connect()

        assertIs<ServifyError.ServerUnavailable>(withTimeout(5_000) { failure.await() })
        assertEquals(ConnectionState.Disconnected, withTimeout(5_000) { chat.events.connectionState.first { it == ConnectionState.Disconnected } })
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
        val streamed = chat.events.messages.awaitWhenSubscribed(ready) { it.isStreaming }
        val final = chat.events.messages.awaitWhenSubscribed(CompletableDeferred()) { it.isAiResponse && !it.isStreaming }
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
        val second = chat.events.messages.awaitWhenSubscribed(ready) { it.content == "第二条" }
        ready.awaitReady()

        chat.connect()

        withTimeout(5_000) { second.await() }
        awaitUnread(2) // 等 bump 链收尾（跨线程窗口），保证 visible 后断言 0 是终态

        chat.onSessionVisible()
        assertEquals(0, chat.events.unreadCount.value)
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
        val assignment = chat.events.agentAssigned.awaitWhenSubscribed(ready)
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
        val waiting = chat.events.waitingInQueue.awaitWhenSubscribed(ready)
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
                chat.events.connectionState.first { it is ConnectionState.Reconnecting }
            }
            withTimeout(5_000) {
                chat.events.connectionState.first { it == ConnectionState.Connected }
            }
            reconnecting
        }
        chat.sendMessage("触发服务端断开") // 回显不来，发送侧按 SendTimeout 超时收尾即可

        val reconnecting = reconnected.await() as ConnectionState.Reconnecting
        assertEquals(1, reconnecting.attempt)
        assertEquals(ConnectionState.Connected, chat.events.connectionState.value)
    }

    @Test
    fun destroyIsIdempotentAndMarksDisconnected() = runBlocking {
        server.enqueue(MockResponse().withWebSocketUpgrade(EchoListener()))
        chat = newChat()
        chat.connect()
        awaitConnected()

        chat.destroy()
        chat.destroy()
        assertEquals(ConnectionState.Disconnected, chat.events.connectionState.value)
    }

    // ---- 刀 4：会话连续性（累积/未读可见性/流中断收口）与 guestToken 握手 ----

    /** 按收到的客户消息序号下发不同服务端帧：1=坐席消息、2=流式增量+终帧、3=坐席消息。 */
    private class ScriptedListener : WebSocketListener() {
        @Volatile
        private var count = 0

        override fun onMessage(webSocket: WebSocket, text: String) {
            count += 1
            when (count) {
                1 -> webSocket.send("""{"type":"agent-message","data":{"content":"坐席A"},"session_id":"test-session"}""")
                2 -> {
                    webSocket.send("""{"type":"ai-response-delta","data":{"content_delta":"根据","done":false}}""")
                    webSocket.send("""{"type":"ai-response-delta","data":{"content_delta":"政策。","done":false}}""")
                    webSocket.send("""{"type":"ai-response","data":{"content":"根据政策。","confidence":0.9,"source":"kb"},"session_id":"test-session"}""")
                }
                else -> webSocket.send("""{"type":"agent-message","data":{"content":"坐席B"},"session_id":"test-session"}""")
            }
        }
    }

    @Test
    fun buildWsUrlCarriesAccessTokenOnlyWhenConfigured() {
        chat = newChat()
        val base = chat.buildWsUrl("https://chat.example.com", null, null)
        assertTrue(base.startsWith("wss://chat.example.com/api/v1/ws?"))
        assertTrue(base.contains("session_id=test-session"))
        assertTrue(!base.contains("access_token"))

        // PROTOCOL §1：access_token 现阶段服务端不读取，端点落地后自动生效；特殊字符须编码
        // （URLEncoder form 语义：空格→+，&/=→%XX——Go 服务端 r.URL.Query() 按 form 解 + 为空格）。
        val withToken = chat.buildWsUrl("https://chat.example.com", null, "tok en&x=1")
        assertTrue(withToken.contains("access_token=tok+en%26x%3D1"), withToken)

        val override = "ws://127.0.0.1:1/ws"
        assertEquals(override, chat.buildWsUrl("https://chat.example.com", override, null))
        assertEquals(
            "$override?access_token=tok+en%26x%3D1",
            chat.buildWsUrl("https://chat.example.com", override, "tok en&x=1"),
        )
    }

    @Test
    fun historySnapshotAccumulatesAndMergesStreamById() = runBlocking {
        server.enqueue(MockResponse().withWebSocketUpgrade(ScriptedListener()))
        chat = newChat(echoTimeoutMs = 100)
        chat.connect()
        awaitConnected()

        chat.sendMessage("触发1") // listener → 坐席帧
        chat.sendMessage("触发AI") // listener → 流式+终帧

        withTimeout(5_000) {
            while (chat.historySnapshot().none { it.isAiResponse && !it.isStreaming }) kotlinx.coroutines.delay(50)
        }
        val snapshot = chat.historySnapshot()
        // 坐席 + AI 终帧两条；delta 中间态同 id 覆盖不重复累积。
        assertEquals(2, snapshot.size)
        assertEquals(SenderType.Agent, snapshot[0].sender)
        assertEquals("根据政策。", snapshot[1].content)
        assertTrue(snapshot[1].isAiResponse)
        assertTrue(!snapshot[1].isStreaming)
    }

    @Test
    fun unreadCountsOnlyWhileSessionHidden() = runBlocking {
        server.enqueue(MockResponse().withWebSocketUpgrade(ScriptedListener()))
        chat = newChat(echoTimeoutMs = 100)
        chat.connect()
        awaitConnected()

        // 可见期：坐席消息不计未读。
        chat.onSessionVisible()
        chat.sendMessage("一")
        withTimeout(5_000) {
            while (chat.historySnapshot().none { it.sender == SenderType.Agent }) kotlinx.coroutines.delay(50)
        }
        assertEquals(0, chat.events.unreadCount.value)

        // 收起后：AI 流式只按终帧计 1（同 id 不重复），坐席消息再计 1。
        chat.onSessionHidden()
        chat.sendMessage("二")
        withTimeout(5_000) {
            while (chat.historySnapshot().none { it.isAiResponse && !it.isStreaming }) kotlinx.coroutines.delay(50)
        }
        assertEquals(1, chat.events.unreadCount.value)

        chat.sendMessage("三")
        withTimeout(5_000) {
            while (chat.historySnapshot().count { it.sender == SenderType.Agent } < 2) kotlinx.coroutines.delay(50)
        }
        assertEquals(2, chat.events.unreadCount.value)

        chat.onSessionVisible()
        assertEquals(0, chat.events.unreadCount.value)
    }

    @Test
    fun streamInterruptionOnDisconnectFinalizesPartialWithHint() = runBlocking {
        // 断线即流中断（PROTOCOL §4.1）：保留已渲染 + 提示行；提示行不计未读。
        // 服务器断开必须走 onMessage 内同步 cancel()（外部线程/SocketPolicy/onOpen 均不传播，
        // 见 reconnectsAfterServerDrop 注释）；delta 与断开拆两条消息时序驱动。
        server.enqueue(
            MockResponse().withWebSocketUpgrade(object : WebSocketListener() {
                @Volatile
                private var count = 0

                override fun onMessage(webSocket: WebSocket, text: String) {
                    count += 1
                    when (count) {
                        1 -> {
                            webSocket.send("""{"type":"ai-response-delta","data":{"content_delta":"根据","done":false}}""")
                            webSocket.send("""{"type":"ai-response-delta","data":{"content_delta":"退货政策。","done":false}}""")
                        }
                        else -> webSocket.cancel()
                    }
                }
            }),
        )
        chat = newChat(echoTimeoutMs = 100)
        chat.connect()
        awaitConnected()

        chat.sendMessage("触发流式") // listener → delta×2（streamingId 挂上）
        chat.sendMessage("触发断开") // listener → 服务器同步 cancel → onFailure → finalize

        try {
            withTimeout(5_000) {
                while (chat.historySnapshot().none { it.content == "回答中断，请重试" }) kotlinx.coroutines.delay(50)
            }
        } catch (e: kotlinx.coroutines.TimeoutCancellationException) {
            println("PROBE-INTERRUPT snapshot=${chat.historySnapshot()} state=${chat.events.connectionState.value}")
            throw e
        }
        val snapshot = chat.historySnapshot()
        val partial = snapshot.first { it.isAiResponse }
        assertEquals("根据退货政策。", partial.content)
        assertTrue(!partial.isStreaming)
        assertEquals(0, chat.events.unreadCount.value)
    }
}
