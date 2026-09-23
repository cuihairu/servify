package servify.sdk.android

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
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
import servify.sdk.android.connect.ReconnectPolicy
import servify.sdk.android.model.SenderType
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * 会话页工单创建（M3 刀 3a）：POST /api/v1/tickets 的请求组装（session_id/title/
 * ai_summary 摘录）与结果语义（201→回执、非 2xx/IO/畸形→null + 错误流）。
 */
class CreateTicketTest {

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

    private fun newChat(ticketUrlOverride: String = server.url("/api/v1/tickets").toString()): ServifyChat =
        ServifyChat(
            config = ServifyConfig(apiUrl = "https://chat.example.com"),
            sessionId = "test-session",
            client = OkHttpClient(),
            scope = chatScope,
            policy = ReconnectPolicy(maxAttempts = 3, initialDelayMs = 100, multiplier = 2, maxDelayMs = 400),
            echoTimeoutMs = 200,
            wsUrlOverride = server.url("/api/v1/ws").toString(),
            ticketUrlOverride = ticketUrlOverride,
        )

    /** 经真实 WS 注入一条坐席消息，让门面 history 有内容可摘。 */
    private suspend fun seedHistoryViaAgentMessage(chat: ServifyChat) {
        val wsListener = object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: okhttp3.Response) {
                webSocket.send(
                    """{"type":"agent-message","data":{"content":"您好，请问遇到什么问题","sender":"agent"}}""",
                )
            }
        }
        server.enqueue(MockResponse().withWebSocketUpgrade(wsListener))
        val received = chatScope.async { chat.events.messages.first { it.sender == SenderType.Agent } }
        chat.connect()
        withTimeout(5_000) { received.await() }
    }

    @Test
    fun createTicketPostsRequestAndParsesReceipt() = runBlocking {
        chat = newChat()
        seedHistoryViaAgentMessage(chat)

        server.enqueue(MockResponse().setResponseCode(201).setBody("""{"id":42,"status":"open"}"""))
        val receipt = chat.createTicket("打不开页面", "按钮无响应")

        assertEquals(42L, receipt?.ticketId)
        // 队首可能是 seed 阶段的 WS 升级请求——循环取到工单 POST 为止。
        val request = generateSequence { server.takeRequest(5, java.util.concurrent.TimeUnit.SECONDS) }
            .first { it.path == "/api/v1/tickets" }
        assertEquals("POST", request.method)
        val body = request.body.readUtf8()
        assertTrue(body.contains("\"session_id\":\"test-session\""))
        assertTrue(body.contains("\"title\":\"打不开页面\""))
        assertTrue(body.contains("\"description\":\"按钮无响应\""))
        // ai_summary 摘录了会话中的坐席消息（坐席侧直接可读）
        assertTrue(body.contains("\"ai_summary\":\"[客服] 您好，请问遇到什么问题\""))
    }

    @Test
    fun createTicketOmitsSummaryAndDescriptionWhenAbsent() = runBlocking {
        chat = newChat()
        server.enqueue(MockResponse().setResponseCode(201).setBody("""{"id":7}"""))

        val receipt = chat.createTicket("咨询")
        assertEquals(7L, receipt?.ticketId)

        val body = server.takeRequest().body.readUtf8()
        assertFalse(body.contains("ai_summary"))
        assertFalse(body.contains("description"))
    }

    @Test
    fun createTicketReturnsNullAndEmitsTicketFailedOnHttpError() = runBlocking {
        chat = newChat()
        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"Session not found"}"""))

        val errorSeen = chatScope.async { chat.events.error.first { it.code == "ticket_failed" } }
        val receipt = chat.createTicket("打不开页面")

        assertNull(receipt)
        assertEquals("ticket_failed", withTimeout(5_000) { errorSeen.await().code })
    }

    @Test
    fun createTicketReturnsNullAndEmitsNetworkOnIoFailure() = runBlocking {
        // 端口 1 无监听 → 连接拒绝 → IOException → Network 错误
        chat = newChat(ticketUrlOverride = "http://127.0.0.1:1/api/v1/tickets")
        server.shutdown()
        val errorSeen = chatScope.async { chat.events.error.first { it.code == "network" } }
        val receipt = chat.createTicket("打不开页面")

        assertNull(receipt)
        assertEquals("network", withTimeout(5_000) { errorSeen.await().code })
    }

    @Test
    fun createTicketReturnsNullAndEmitsTicketFailedOnMalformedBody() = runBlocking {
        chat = newChat()
        server.enqueue(MockResponse().setResponseCode(201).setBody("""{"status":"open"}"""))

        val errorSeen = chatScope.async { chat.events.error.first { it.code == "ticket_failed" } }
        val receipt = chat.createTicket("打不开页面")

        assertNull(receipt)
        assertEquals("ticket_failed", withTimeout(5_000) { errorSeen.await().code })
    }
}
