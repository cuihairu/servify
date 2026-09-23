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
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.yield
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
 * 推送注册口（M3，平台规格 §4 registerPushToken；§10 #5 注册端点）：
 * 未配置→Unsupported；配置+无 token→静默；配置+有 token→POST
 * /api/v1/push/register（2xx→true；IO/HTTP 失败→Network + false）。
 *
 * 触发统一经 onSubscription 钩在订阅注册点后（registerPushToken 同步 emit，先订阅
 * 再触发的 async 竞态窗口在此必踩——无 replay SharedFlow 的消息直落）。
 */
class RegisterPushTokenTest {

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
        pushTokenProvider: (suspend () -> String?)?,
        apiUrl: String = "https://chat.example.com",
    ): ServifyChat =
        ServifyChat(
            config = ServifyConfig(apiUrl = apiUrl, pushTokenProvider = pushTokenProvider),
            sessionId = "test-session",
            client = OkHttpClient(),
            scope = chatScope,
            policy = ReconnectPolicy(maxAttempts = 3, initialDelayMs = 100, multiplier = 2, maxDelayMs = 400),
            echoTimeoutMs = 200,
            wsUrlOverride = server.url("/api/v1/ws").toString(),
            pushUrlOverride = server.url("/api/v1/push/register").toString(),
        )

    @Test
    fun emitsUnsupportedWhenNotConfigured() = runBlocking {
        chat = newChat(pushTokenProvider = null)

        val errorSeen = chat.events.error
            .onSubscription { chat.registerPushToken() }
            .first { it.code == "unsupported" }

        assertEquals("push not configured", errorSeen.message)
    }

    @Test
    fun silentWhenProviderReturnsNull() = runBlocking {
        var providerCalled = false
        chat = newChat(pushTokenProvider = { providerCalled = true; null })

        var sawError = false
        val triggered = CompletableDeferred<Unit>()
        val job = launch {
            chat.events.error
                .onSubscription {
                    val result = chat.registerPushToken()
                    assertFalse(result)
                    triggered.complete(Unit)
                }
                .collect { sawError = true }
        }
        triggered.await()
        // 触发同步完成后让 emit（若有）送达订阅方
        repeat(5) { yield() }
        job.cancel()

        assertTrue(providerCalled, "provider 应被调用（null 是宿主正常态）")
        assertFalse(sawError, "null token 是宿主正常态（未授权），不应产生 unsupported 错误")
    }

    @Test
    fun postsRegistrationAndReturnsTrueOnCreated() = runBlocking {
        chat = newChat(pushTokenProvider = { "fcm-token-abc" })
        server.enqueue(
            MockResponse().setResponseCode(201)
                .setBody("""{"id":7,"session_id":"test-session","platform":"android","updated_at":"2026-09-23T12:00:00Z"}""")
        )

        val result = chat.registerPushToken()

        assertTrue(result)
        val recorded = server.takeRequest(5, TimeUnit.SECONDS) ?: throw AssertionError("注册请求未到达服务端")
        assertEquals("/api/v1/push/register", recorded.path)
        assertEquals("POST", recorded.method)
        val body = recorded.body.readUtf8()
        assertTrue(body.contains("\"session_id\":\"test-session\""), body)
        assertTrue(body.contains("\"platform\":\"android\""), body)
        assertTrue(body.contains("\"token\":\"fcm-token-abc\""), body)
    }

    @Test
    fun returnsFalseAndEmitsNetworkOnHttpError() = runBlocking {
        chat = newChat(pushTokenProvider = { "fcm-token-abc" })
        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"Session not found"}"""))

        val errorSeen = chat.events.error
            .onSubscription { chat.registerPushToken() }
            .first { it.code == "network" }

        assertTrue(errorSeen.retryable, "network 可重试（注册可重报）")
        assertEquals("push register http 404", errorSeen.message)
    }

    @Test
    fun returnsFalseAndEmitsNetworkOnIoFailure() = runBlocking {
        chat = newChat(pushTokenProvider = { "fcm-token-abc" })
        server.shutdown() // 连接拒绝 → IOException

        val errorSeen = chat.events.error
            .onSubscription { chat.registerPushToken() }
            .first { it.code == "network" }

        assertTrue(errorSeen.message.startsWith("push register failed: "), errorSeen.message)
    }
}
