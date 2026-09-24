package servify.sdk.android

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import java.util.concurrent.TimeUnit
import okhttp3.OkHttpClient
import okhttp3.Response
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
import kotlin.test.assertTrue

/**
 * 断线补拉（D7 流程 3；§10 #1 端点）：连接成功后 GET
 * /api/v1/sessions/{id}/messages?after_id=<游标> 增量对账——合并 history、
 * agent/ai 计未读（面板不可见时）、has_more 续拉、全失败面静默、指纹去重。
 *
 * 纯 REST 用例直调 internal reconcileMessages()（MockWebServer 队列响应）；
 * 指纹去重走 WS 集成用例（指纹只在 WS 渲染点积累，生产路径是
 * "WS 渲染 → 断线 → 重连补拉拉回同批"）。
 */
class ReconcileMessagesTest {

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

    private fun newChat(): ServifyChat =
        ServifyChat(
            config = ServifyConfig(apiUrl = "https://chat.example.com"),
            sessionId = "test-session",
            client = OkHttpClient(),
            scope = chatScope,
            policy = ReconnectPolicy(maxAttempts = 3, initialDelayMs = 100, multiplier = 2, maxDelayMs = 400),
            echoTimeoutMs = 200,
            wsUrlOverride = server.url("/api/v1/ws").toString(),
            messagesUrlOverride = server.url("/api/v1/sessions/test-session/messages").toString(),
        )

    private fun page(vararg messages: String, hasMore: Boolean = false): MockResponse =
        MockResponse().setResponseCode(200)
            .setHeader("Content-Type", "application/json")
            .setBody("""{"messages":[${messages.joinToString(",")}],"has_more":$hasMore}""")

    private fun msg(id: String, sender: String, content: String): String =
        """{"id":"$id","conversation_id":"test-session","sender":"$sender","kind":"text","content":"$content","created_at":"2026-09-24T12:00:00Z"}"""

    private suspend fun awaitUntil(timeoutMs: Long = 5_000, condition: () -> Boolean) {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (!condition()) {
            if (System.currentTimeMillis() > deadline) throw AssertionError("条件在 ${timeoutMs}ms 内未达成")
            delay(20)
        }
    }

    @Test
    fun mergesHistoryAndCountsUnreadForAgentAndAi() = runBlocking {
        chat = newChat()
        server.enqueue(
            page(
                msg("1", "customer", "在吗"),
                msg("2", "agent", "您好，我是客服"),
                msg("3", "system", "会话已建立"),
                msg("4", "ai", "猜你想问：如何重置密码"),
            ),
        )

        chat.reconcileMessages()

        // 升序合并：四种来源全部进 history（customer 断线前的本地份指纹命中时才跳过）
        assertEquals(
            listOf("在吗", "您好，我是客服", "会话已建立", "猜你想问：如何重置密码"),
            chat.historySnapshot().map { it.content },
        )
        assertEquals(SenderType.Customer, chat.historySnapshot()[0].sender)
        assertEquals(SenderType.Agent, chat.historySnapshot()[1].sender)
        assertEquals(SenderType.System, chat.historySnapshot()[2].sender)
        // ai 映射为 System + isAiResponse（与 WS 帧口径同构）
        val ai = chat.historySnapshot()[3]
        assertEquals(SenderType.System, ai.sender)
        assertTrue(ai.isAiResponse, "ai 来源应带 isAiResponse 标记")
        // 未读：agent/ai 计（面板不可见），customer/system 不计
        assertEquals(2, chat.events.unreadCount.value)
        val recorded = server.takeRequest(5, TimeUnit.SECONDS) ?: throw AssertionError("补拉请求未到达")
        assertEquals("GET", recorded.method)
        assertEquals("/api/v1/sessions/test-session/messages?limit=100", recorded.path)
    }

    @Test
    fun paginatesOnHasMoreWithAfterIdCursor() = runBlocking {
        chat = newChat()
        server.enqueue(page(msg("5", "agent", "第一页"), hasMore = true))
        server.enqueue(page(msg("9", "agent", "第二页")))

        chat.reconcileMessages()

        assertEquals(listOf("第一页", "第二页"), chat.historySnapshot().map { it.content })
        assertEquals(2, chat.events.unreadCount.value)
        server.takeRequest(5, TimeUnit.SECONDS)
        val second = server.takeRequest(5, TimeUnit.SECONDS) ?: throw AssertionError("第二页请求未到达")
        assertTrue(second.path!!.contains("after_id=5"), "续拉应以末条 id 为游标：${second.path}")
    }

    @Test
    fun cursorAdvancesMonotonicallyWithinPage() = runBlocking {
        chat = newChat()
        // 页内乱序（升序契约外的防御面）：游标取 max 而非首条
        server.enqueue(page(msg("7", "agent", "后到的"), msg("3", "agent", "先落的"), hasMore = true))
        server.enqueue(page())

        chat.reconcileMessages()

        assertEquals(listOf("后到的", "先落的"), chat.historySnapshot().map { it.content })
        server.takeRequest(5, TimeUnit.SECONDS)
        val second = server.takeRequest(5, TimeUnit.SECONDS) ?: throw AssertionError("第二页请求未到达")
        assertTrue(second.path!!.contains("after_id=7"), "游标应推进到页内最大 id：${second.path}")
    }

    @Test
    fun skipsMalformedEntries() = runBlocking {
        chat = newChat()
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(
                """{"messages":["不是对象",{"conversation_id":"x"},{"id":"abc"},""" +
                    msg("12", "agent", "合法") + """],"has_more":false}""",
            ),
        )

        chat.reconcileMessages()

        assertEquals(listOf("合法"), chat.historySnapshot().map { it.content })
        assertEquals(1, chat.events.unreadCount.value)
    }

    @Test
    fun fallsBackToApiUrlWhenNoOverride() = runBlocking {
        // messagesUrlOverride 缺省（生产常态）：从 apiUrl 拼对账 URL。端点指向
        // 本机保留端口（连接拒绝瞬时确定），全失败面静默——不动 history/未读。
        chat = ServifyChat(
            config = ServifyConfig(apiUrl = "https://127.0.0.1:1"),
            sessionId = "test-session",
            client = OkHttpClient(),
            scope = chatScope,
            policy = ReconnectPolicy(maxAttempts = 3, initialDelayMs = 100, multiplier = 2, maxDelayMs = 400),
            echoTimeoutMs = 200,
            wsUrlOverride = server.url("/api/v1/ws").toString(),
        )

        chat.reconcileMessages()

        assertTrue(chat.historySnapshot().isEmpty())
        assertEquals(0, chat.events.unreadCount.value)
    }

    @Test
    fun silentWhenSessionMissing() = runBlocking {
        chat = newChat()
        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"session not found"}"""))

        chat.reconcileMessages()

        assertTrue(chat.historySnapshot().isEmpty(), "404 = 会话行未建过，无历史可拉")
        assertEquals(0, chat.events.unreadCount.value)
    }

    @Test
    fun silentOnHttpError() = runBlocking {
        chat = newChat()
        server.enqueue(MockResponse().setResponseCode(500).setBody("""{"error":"boom"}"""))

        chat.reconcileMessages()

        assertTrue(chat.historySnapshot().isEmpty())
        assertEquals(0, chat.events.unreadCount.value)
    }

    @Test
    fun silentOnTransportFailure() = runBlocking {
        chat = newChat()
        server.shutdown() // 连接拒绝 → IOException

        chat.reconcileMessages()

        assertTrue(chat.historySnapshot().isEmpty())
        assertEquals(0, chat.events.unreadCount.value)
    }

    @Test
    fun silentOnMalformedBody() = runBlocking {
        chat = newChat()
        server.enqueue(MockResponse().setResponseCode(200).setBody("not-json"))

        chat.reconcileMessages()

        assertTrue(chat.historySnapshot().isEmpty())
        assertEquals(0, chat.events.unreadCount.value)
    }

    @Test
    fun silentWhenMessagesFieldMissing() = runBlocking {
        chat = newChat()
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"foo":1}"""))

        chat.reconcileMessages()

        assertTrue(chat.historySnapshot().isEmpty())
        assertEquals(0, chat.events.unreadCount.value)
    }

    /**
     * 指纹去重的生产路径（WS 集成）：首连 WS 渲染坐席帧（指纹入表）→ 断线重连
     * → 补拉拉回同一消息。游标此时仍未确立（WS 帧无服务端 ID、空页不推进），
     * 去重只能靠指纹——命中条跳过渲染与未读，同批新消息正常入列。
     */
    @Test
    fun fingerprintSkipsRenderedMessagesOnReconcile() = runBlocking {
        chat = newChat()
        val agentFrame =
            """{"type":"agent-message","data":{"content":"断线期间的消息","sender":"坐席"},"session_id":"test-session"}"""
        server.enqueue(MockResponse().withWebSocketUpgrade(ProactiveAgentListener(agentFrame)))
        server.enqueue(page()) // 首连 reconcile：空页
        server.enqueue(MockResponse().withWebSocketUpgrade(object : WebSocketListener() {}))
        server.enqueue(
            page(
                msg("5", "agent", "断线期间的消息"), // 指纹命中 → 跳过
                msg("6", "agent", "重连后的新消息"), // 正常入列
            ),
        )

        chat.connect()
        // 服务端 onOpen 即发坐席帧；等渲染落 history 再断线
        awaitUntil { chat.historySnapshot().any { it.content == "断线期间的消息" } }
        assertEquals(1, chat.events.unreadCount.value)
        chat.disconnectForTesting()

        // 重连 onOpen 触发补拉；等 REST#2 渲染完成（新消息入列）
        awaitUntil { chat.historySnapshot().any { it.content == "重连后的新消息" } }
        assertEquals(
            1,
            chat.historySnapshot().count { it.content == "断线期间的消息" },
            "指纹命中的同内容消息不得重复渲染",
        )
        assertEquals(2, chat.events.unreadCount.value, "只有新消息计入未读（+1）")

        // 请求顺序：WS#1 → REST#1 → WS#2 → REST#2；重连补拉不带游标（未确立，全量）
        server.takeRequest(5, TimeUnit.SECONDS)
        server.takeRequest(5, TimeUnit.SECONDS)
        server.takeRequest(5, TimeUnit.SECONDS)
        val rest2 = server.takeRequest(5, TimeUnit.SECONDS) ?: throw AssertionError("重连补拉请求未到达")
        assertEquals("GET", rest2.method)
        assertTrue(rest2.path!!.startsWith("/api/v1/sessions/test-session/messages"), rest2.path)
        assertFalse(rest2.path!!.contains("after_id"), "游标未确立时全量拉取：${rest2.path}")
    }

    /** 服务器 WS listener：onOpen 即主动下发一帧（模拟断线前最后一条坐席消息）。 */
    private class ProactiveAgentListener(private val frame: String) : WebSocketListener() {
        override fun onOpen(webSocket: WebSocket, response: Response) {
            webSocket.send(frame)
        }
    }
}
