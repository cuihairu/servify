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
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Before
import org.junit.Test
import servify.sdk.android.connect.ReconnectPolicy
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * 推送注册口（M3 刀 4，平台规格 §4 registerPushToken；§10 #5 端点未上线的过渡语义）：
 * 未配置→Unsupported；配置+无 token→静默；配置+有 token→过渡期 Unsupported。
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

    private fun newChat(pushTokenProvider: (suspend () -> String?)?): ServifyChat =
        ServifyChat(
            config = ServifyConfig(apiUrl = "https://chat.example.com", pushTokenProvider = pushTokenProvider),
            sessionId = "test-session",
            client = OkHttpClient(),
            scope = chatScope,
            policy = ReconnectPolicy(maxAttempts = 3, initialDelayMs = 100, multiplier = 2, maxDelayMs = 400),
            echoTimeoutMs = 200,
            wsUrlOverride = server.url("/api/v1/ws").toString(),
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
    fun emitsUnsupportedWhenTokenAvailableButEndpointNotLive() = runBlocking {
        chat = newChat(pushTokenProvider = { "fcm-token-abc" })

        val errorSeen = chat.events.error
            .onSubscription { chat.registerPushToken() }
            .first { it.code == "unsupported" }

        assertEquals("push registration endpoint not available", errorSeen.message)
    }
}
