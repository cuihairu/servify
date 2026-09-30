package servify.sdk.android

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.onSubscription
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.yield
import java.util.concurrent.TimeUnit
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Before
import org.junit.Test
import servify.sdk.android.connect.ReconnectPolicy
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * 服务端已读游标提交（D7 流程 3 服务端增强面；§10 #3 POST /read）：
 * 无已确认消息（未对账）→ 静默 false 且零请求；有游标 → POST
 * /api/v1/sessions/{id}/read 体 {"last_read_message_id":"<id>"}（2xx→true；
 * IO/HTTP 失败→Network + false）。响应体不消费——本地未读推导语义不被
 * 服务端回显改写。Swift 镜像：SyncReadStateTests.swift——用例名逐一对应。
 *
 * 游标种子走补拉链（messagesUrlOverride 回放一页含服务端 ID 的消息后直调
 * internal reconcileMessages()，与生产 onOpen 对账同路径）。
 */
class SyncReadStateTest {

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

    private fun newChat(
        apiUrl: String = "https://chat.example.com",
        readUrlOverride: String? = null,
    ): ServifyChat =
        ServifyChat(
            config = ServifyConfig(apiUrl = apiUrl),
            sessionId = "test-session",
            client = OkHttpClient(),
            scope = chatScope,
            policy = ReconnectPolicy(maxAttempts = 3, initialDelayMs = 100, multiplier = 2, maxDelayMs = 400),
            echoTimeoutMs = 200,
            wsUrlOverride = server.url("/api/v1/ws").toString(),
            messagesUrlOverride = server.url("/api/v1/sessions/test-session/messages").toString(),
            readUrlOverride = readUrlOverride,
        )

    private fun seedCursor(id: String = "7") {
        server.enqueue(
            MockResponse().setResponseCode(200)
                .setHeader("Content-Type", "application/json")
                .setBody("""{"messages":[{"id":"$id","conversation_id":"test-session","sender":"agent","kind":"text","content":"您好","created_at":"2026-09-30T12:00:00Z"}],"has_more":false}"""),
        )
        runBlocking { chat.reconcileMessages() }
    }

    @Test
    fun postsCursorAndReturnsTrueOnOk() = runBlocking {
        chat = newChat(readUrlOverride = server.url("/api/v1/sessions/test-session/read").toString())
        seedCursor()
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"unread_count":0,"last_read_message_id":"7"}"""))

        val result = chat.syncReadState()

        assertTrue(result)
        // 队列头是 seedCursor 的补拉 GET，先排空再断言 read 提交
        val seedGet = server.takeRequest(5, TimeUnit.SECONDS) ?: throw AssertionError("补拉 GET 未到达服务端")
        assertTrue(seedGet.path!!.startsWith("/api/v1/sessions/test-session/messages"), seedGet.path)
        val read = server.takeRequest(5, TimeUnit.SECONDS) ?: throw AssertionError("已读提交请求未到达服务端")
        assertEquals("/api/v1/sessions/test-session/read", read.path)
        assertEquals("POST", read.method)
        val body = read.body.readUtf8()
        assertTrue(body.contains("\"last_read_message_id\":\"7\""), body)
    }

    @Test
    fun silentFalseWithoutCursor() = runBlocking {
        chat = newChat(readUrlOverride = server.url("/api/v1/sessions/test-session/read").toString())

        var sawError = false
        val triggered = CompletableDeferred<Unit>()
        val job = launch {
            chat.events.error
                .onSubscription {
                    val result = chat.syncReadState()
                    assertFalse(result)
                    triggered.complete(Unit)
                }
                .collect { sawError = true }
        }
        triggered.await()
        repeat(5) { yield() }
        job.cancel()

        assertFalse(sawError, "未对账过（无游标）是正常态，不应产生错误")
        assertEquals(0, server.requestCount, "无游标不应发出任何请求")
    }

    @Test
    fun derivesReadUrlFromApiUrlWhenNoOverride() = runBlocking {
        // 生产路径：无 override 时 read URL 由 apiUrl 派生。端点指向本机保留端口
        // （https 校验合法 + 连接拒绝瞬时确定，ReconcileMessagesTest 派生覆盖同口径）
        // ——派生行被执行，提交以 Network 错误收场（可重报）。
        chat = newChat(apiUrl = "https://127.0.0.1:1")
        seedCursor()

        val errorSeen = chat.events.error
            .onSubscription { assertFalse(chat.syncReadState()) }
            .first { it.code == "network" }

        assertTrue(errorSeen.message.contains("read cursor sync failed"), errorSeen.message)
    }

    @Test
    fun emitsNetworkAndFalseOnHttpError() = runBlocking {
        chat = newChat(readUrlOverride = server.url("/api/v1/sessions/test-session/read").toString())
        seedCursor()
        server.enqueue(MockResponse().setResponseCode(500))

        val errorSeen = chat.events.error
            .onSubscription { assertFalse(chat.syncReadState()) }
            .first { it.code == "network" }

        assertTrue(errorSeen.message.contains("read cursor sync http 500"), errorSeen.message)
    }

    @Test
    fun emitsNetworkAndFalseOnNotFound() = runBlocking {
        chat = newChat(readUrlOverride = server.url("/api/v1/sessions/test-session/read").toString())
        seedCursor()
        server.enqueue(MockResponse().setResponseCode(404))

        val errorSeen = chat.events.error
            .onSubscription { assertFalse(chat.syncReadState()) }
            .first { it.code == "network" }

        assertTrue(errorSeen.message.contains("read cursor sync http 404"), errorSeen.message)
    }

    @Test
    fun emitsNetworkAndFalseOnIoFailure() = runBlocking {
        // 不可达端口 → connect 拒绝 → IOException（与 server.shutdown 等效且不影响 tearDown）
        chat = newChat(readUrlOverride = "http://127.0.0.1:1/api/v1/sessions/test-session/read")
        seedCursor()

        val errorSeen = chat.events.error
            .onSubscription { assertFalse(chat.syncReadState()) }
            .first { it.code == "network" }

        assertTrue(errorSeen.message.contains("read cursor sync failed"), errorSeen.message)
    }
}
