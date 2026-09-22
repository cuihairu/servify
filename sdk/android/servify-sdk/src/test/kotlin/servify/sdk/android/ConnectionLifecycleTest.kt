package servify.sdk.android

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.onSubscription
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import okhttp3.OkHttpClient
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Before
import org.junit.Test
import servify.sdk.android.connect.ConnectionState
import servify.sdk.android.connect.ReconnectPolicy
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * 连接状态机转移表穷举（§4.4，M1 验收⑤）：
 *
 * ```
 * idle ─(首次 connect/show)→ connecting → connected ─(断)→ reconnecting(n) → connected
 *                                      └─(握手失败)→ disconnected
 * disconnected ─(用户再次 connect/show)→ connecting；重连耗尽也落 disconnected
 * connected/connecting 期间重复 connect = 无操作（幂等）
 * ```
 *
 * 断线重连边（connected → reconnecting → connected）由 ServifyChatTest.reconnectsAfterServerDrop
 * 锚定；destroy → disconnected 由 destroyIsIdempotentAndMarksDisconnected 锚定——本类不重复。
 */
class ConnectionLifecycleTest {

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

    private fun newChat(policy: ReconnectPolicy = ReconnectPolicy(maxAttempts = 3, initialDelayMs = 100, multiplier = 2, maxDelayMs = 400)): ServifyChat =
        ServifyChat(
            config = ServifyConfig(apiUrl = "https://chat.example.com"),
            sessionId = "test-session",
            client = OkHttpClient(),
            scope = chatScope,
            policy = policy,
            echoTimeoutMs = 200,
            wsUrlOverride = server.url("/api/v1/ws").toString(),
        )

    private suspend fun awaitConnected() {
        withTimeout(5_000) { chat.events.connectionState.first { it == ConnectionState.Connected } }
    }

    /** 服务器 listener：首条客户消息后断开（onMessage 内同步 cancel，唯一可靠断开手法）。 */
    private class DropOnFirstMessage : WebSocketListener() {
        @Volatile
        private var seen = false

        override fun onMessage(webSocket: WebSocket, text: String) {
            if (!seen) {
                seen = true
                webSocket.cancel()
            }
        }
    }

    @Test
    fun initialConnectionStateIsIdle() {
        chat = newChat()
        assertEquals(ConnectionState.Idle, chat.events.connectionState.value)
    }

    @Test
    fun idlePassesThroughConnectingToConnected() = runBlocking {
        server.enqueue(MockResponse().withWebSocketUpgrade(object : WebSocketListener() {}))
        chat = newChat()

        val ready = CompletableDeferred<Unit>()
        val connecting: Deferred<ConnectionState> = chatScope.async {
            chat.events.connectionState.onSubscription { ready.complete(Unit) }.first { it is ConnectionState.Connecting }
        }
        ready.await()
        chat.connect()

        assertTrue(withTimeout(5_000) { connecting.await() } is ConnectionState.Connecting)
        awaitConnected()
    }

    @Test
    fun repeatedConnectWhileConnectedIsNoop() = runBlocking {
        server.enqueue(MockResponse().withWebSocketUpgrade(EchoListener()))
        chat = newChat()
        chat.connect()
        awaitConnected()

        // 幂等：不替换连接——原连接回显链路依旧可达。
        chat.connect()
        awaitConnected()
        chat.sendMessage("仍在原连接")
        assertEquals(ConnectionState.Connected, chat.events.connectionState.value)
    }

    @Test
    fun handshakeFailureBeforeEverConnectedMarksDisconnected() = runBlocking {
        server.enqueue(MockResponse().setResponseCode(404))
        chat = newChat()

        chat.connect()
        assertEquals(
            ConnectionState.Disconnected,
            withTimeout(5_000) { chat.events.connectionState.first { it == ConnectionState.Disconnected } },
        )
    }

    @Test
    fun reconnectExhaustionMarksDisconnectedAndConnectRecovers() = runBlocking {
        // 第一次连接成功后断线 → 重连 #1 握手被拒（404）→ 再次退避耗尽（maxAttempts=1：
        // delayFor(1) 仍放行、delayFor(2)=null）→ disconnected。
        server.enqueue(MockResponse().withWebSocketUpgrade(DropOnFirstMessage()))
        server.enqueue(MockResponse().setResponseCode(404))
        chat = newChat(policy = ReconnectPolicy(maxAttempts = 1, initialDelayMs = 50, multiplier = 2, maxDelayMs = 100))
        chat.connect()
        awaitConnected()

        chat.sendMessage("触发断线") // 回显不来，按超时收尾
        assertEquals(
            ConnectionState.Disconnected,
            withTimeout(5_000) { chat.events.connectionState.first { it == ConnectionState.Disconnected } },
        )

        // §4.4：disconnected ─(用户再次打开会话页)→ connecting → connected。
        server.enqueue(MockResponse().withWebSocketUpgrade(EchoListener()))
        chat.connect()
        awaitConnected()
    }
}

/** 服务器 WS listener：把收到的 text-message 以回显帧发回（回显判据）。 */
private class EchoListener : WebSocketListener() {
    override fun onMessage(webSocket: WebSocket, text: String) {
        webSocket.send(
            """{"type":"text-message","data":{"content":"你好"},"session_id":"test-session","timestamp":"2026-01-01T00:00:00Z"}""",
        )
    }
}
