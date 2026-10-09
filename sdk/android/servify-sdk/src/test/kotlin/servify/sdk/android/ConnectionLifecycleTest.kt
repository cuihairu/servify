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
import kotlinx.coroutines.withTimeoutOrNull
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
import servify.sdk.android.model.SenderType
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
    private lateinit var bypass: ReconcileBypassDispatcher
    private lateinit var chatScope: CoroutineScope
    private lateinit var chat: ServifyChat

    @Before
    fun setUp() {
        server = MockWebServer()
        bypass = ReconcileBypassDispatcher()
        server.dispatcher = bypass
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
        policy: ReconnectPolicy = ReconnectPolicy(maxAttempts = 3, initialDelayMs = 100, multiplier = 2, maxDelayMs = 400),
        branding: Branding = Branding(),
    ): ServifyChat =
        ServifyChat(
            config = ServifyConfig(apiUrl = "https://chat.example.com", branding = branding),
            sessionId = "test-session",
            client = OkHttpClient(),
            scope = chatScope,
            policy = policy,
            echoTimeoutMs = 200,
            wsUrlOverride = server.url("/api/v1/ws").toString(),
            messagesUrlOverride = server.url("/api/v1/sessions/test-session/messages").toString(),
        )

    private suspend fun awaitConnected() {
        withTimeout(5_000) { chat.events.connectionState.first { it == ConnectionState.Connected } }
    }

    /**
     * 分段等待（三现诊断化，37649797496）：reconnectExhaustion 用例三段链
     * （首连→耗尽→恢复）任意一段烧穿都会以无行号的 TimeoutCancellationException
     * 落败，CI 无测试报告 artifact 时无法定位段位。本助手给每段带段标与到达时
     * 状态快照，并支持终态 fail-fast（等 Connected 时提前落 Disconnected 即刻报，
     * 不盲等预算）。超时/终态错误带段标+当前状态——四现时数据直达根因段。
     */
    private suspend fun awaitSegmentState(
        label: String,
        budgetMs: Long,
        failFastOn: (ConnectionState) -> Boolean = { false },
        isTarget: (ConnectionState) -> Boolean,
    ): ConnectionState {
        val matched = withTimeoutOrNull(budgetMs) {
            chat.events.connectionState.first { isTarget(it) || failFastOn(it) }
        }
        if (matched != null && !failFastOn(matched)) return matched
        val current = chat.events.connectionState.value
        val reason = if (matched == null) "预算 ${budgetMs}ms 内未达" else "提前落入终态"
        error("[$label] $reason：当前状态=$current")
    }

    @Test
    fun initialConnectionStateIsIdle() {
        chat = newChat()
        assertEquals(ConnectionState.Idle, chat.events.connectionState.value)
    }

    @Test
    fun idlePassesThroughConnectingToConnected() = runBlocking {
        bypass.enqueue(MockResponse().withWebSocketUpgrade(object : WebSocketListener() {}))
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
        bypass.enqueue(MockResponse().withWebSocketUpgrade(EchoListener()))
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
        bypass.enqueue(MockResponse().setResponseCode(404))
        chat = newChat()

        chat.connect()
        assertEquals(
            ConnectionState.Disconnected,
            withTimeout(5_000) { chat.events.connectionState.first { it == ConnectionState.Disconnected } },
        )
    }

    @Test
    fun reconnectExhaustionMarksDisconnectedAndConnectRecovers(): Unit = runBlocking {
        // 首连成功后客户端本地断开 → 重连 #1 握手被拒（404）→ 已建连后失败走
        // scheduleReconnect：maxAttempts=1 的 delayFor(2)=null → 耗尽 → disconnected。
        //
        // 台账：35855565576（二现，改本地 cancel）→ 37649797496（三现，Timeout 无段位）
        // → 37668312028（四现，2026-10-08，段位消息命中：重连耗尽段悬空 Connecting
        // 整 15s——首连/恢复段均过）→ 37867895291（六现，2026-10-09，恢复段悬空
        // Connecting 整 10s——耗尽段双证据生效后烧点迁移至恢复段，恢复段同款补齐）。
        // webSocket/reconnectAttempt/everConnected 均已
        // @Volatile，可见性面排除；三段全部分段标注，首连/恢复 10s，耗尽段 15s 双
        // 证据（状态 + server 请求计数）——五/六现起错误消息直接二分根因面。
        bypass.enqueue(MockResponse().withWebSocketUpgrade(EchoListener()))
        bypass.enqueue(MockResponse().setResponseCode(404))
        chat = newChat(policy = ReconnectPolicy(maxAttempts = 1, initialDelayMs = 50, multiplier = 2, maxDelayMs = 100))
        chat.connect()
        awaitSegmentState("首连", budgetMs = 10_000) { it is ConnectionState.Connected }

        // 断线触发用本地 cancel（接缝）：server 端 cancel() 的传播在 CI 偶发丢失
        // （35855565576，15s 预算都等不到 onFailure），本地 cancel 零传播、与真实
        // 断线同路径。预算保留 15s 防回归。
        //
        // 四现 37668312028 定向数据：该段悬空 Connecting 整 15s（首连/恢复段均过）
        // ——悬空非缓慢，加预算无意义，改双证据二分根因：预算烧穿时按 server 请求
        // 计数分流——重连握手（第 2 请求）已到达 → 404 未回/onFailure 丢失面；
        // 未到达 → 重连接协程或 MockWebServer accept 饿死面。
        chat.disconnectForTesting()
        val exhausted = withTimeoutOrNull(15_000) {
            chat.events.connectionState.first { it == ConnectionState.Disconnected }
        }
        if (exhausted == null) {
            val current = chat.events.connectionState.value
            val handshakes = server.requestCount
            val surface = if (handshakes >= 2) {
                "重连握手已到达 server（请求数=$handshakes）→ 404 未回或 onFailure 丢失面"
            } else {
                "重连握手未到达 server（请求数=$handshakes）→ 重连协程/MockWebServer accept 饿死面"
            }
            error("[重连耗尽] 15s 未落 Disconnected：当前状态=$current；$surface")
        }

        // §4.4：disconnected ─(用户再次打开会话页)→ connecting → connected。
        //
        // 六现 37867895291（2026-10-09，8288d65 纯 test 头）段位消息命中：恢复段
        // 悬空 Connecting 整 10s（首连/耗尽段均过）——与四现耗尽段同症，恢复段
        // 补同款双证据：烧穿按 server 请求计数二分（第 3 请求 = 恢复握手），
        // ≥3 → upgrade 未回/onOpen 丢失面；<3 → 恢复 connect 协程/accept 饿死面。
        // 提前落 Disconnected 同样带计数即刻报（fail-fast 只提前，不改判据）。
        bypass.enqueue(MockResponse().withWebSocketUpgrade(EchoListener()))
        chat.connect()
        val recovered = withTimeoutOrNull(10_000) {
            chat.events.connectionState.first {
                it == ConnectionState.Connected || it == ConnectionState.Disconnected
            }
        }
        if (recovered != ConnectionState.Connected) {
            val current = chat.events.connectionState.value
            val handshakes = server.requestCount
            val surface = if (handshakes >= 3) {
                "恢复握手已到达 server（请求数=$handshakes）→ upgrade 未回或 onOpen 丢失面"
            } else {
                "恢复握手未到达 server（请求数=$handshakes）→ 恢复 connect 协程/MockWebServer accept 饿死面"
            }
            val how = if (recovered == null) "10s 未达 Connected" else "提前落入 Disconnected"
            error("[恢复重连] $how：当前状态=$current；$surface")
        }
    }

    /** M3 Branding 四件套收口：offlineText 在 disconnected 终态（耗尽/握手失败）追加系统提示行。 */

    @Test
    fun offlineHintEmittedOnReconnectExhaustionWhenConfigured() = runBlocking {
        bypass.enqueue(MockResponse().withWebSocketUpgrade(EchoListener()))
        bypass.enqueue(MockResponse().setResponseCode(404))
        chat = newChat(
            policy = ReconnectPolicy(maxAttempts = 1, initialDelayMs = 50, multiplier = 2, maxDelayMs = 100),
            branding = Branding(offlineText = "客服当前不在线，请稍后再来"),
        )
        chat.connect()
        awaitSegmentState("首连", budgetMs = 10_000) { it is ConnectionState.Connected }

        // 提示行走无 replay 的 SharedFlow——先订阅再触发（与 Swift 侧用例同序）。
        val hintDeferred = chatScope.async {
            chat.events.messages.first { it.sender == SenderType.System && it.content == "客服当前不在线，请稍后再来" }
        }
        // 本地断开（同 reconnectExhaustion 用例：零传播依赖）。提示行在耗尽终态后
        // 发射，等待链 = 重连耗尽链（该链 CI 重载下实测能烧穿 5s，见三现台账
        // 37649797496 与 37661395908）——预算同段拉平 15s 纯耐心；提示行不可对
        // Disconnected fail-fast（hint 恰在终态后到）。
        chat.disconnectForTesting()
        val hint = withTimeout(15_000) { hintDeferred.await() }
        assertEquals("test-session", hint.sessionId)
        // 提示行是 SDK 自造 UI 状态行（同流中断提示），不计未读。
        assertEquals(0, chat.events.unreadCount.value)
    }

    @Test
    fun offlineHintOmittedWhenNotConfigured() = runBlocking {
        bypass.enqueue(MockResponse().withWebSocketUpgrade(EchoListener()))
        bypass.enqueue(MockResponse().setResponseCode(404))
        chat = newChat(policy = ReconnectPolicy(maxAttempts = 1, initialDelayMs = 50, multiplier = 2, maxDelayMs = 100))
        chat.connect()
        awaitSegmentState("首连", budgetMs = 10_000) { it is ConnectionState.Connected }

        // 断线→重连耗尽链与 reconnectExhaustion 用例同段（一现 37661395908 烧穿
        // 裸 5s）——awaitSegmentState 同段拉平：15s 纯耐心 + 段标诊断。
        chat.disconnectForTesting()
        awaitSegmentState("重连耗尽", budgetMs = 15_000) { it is ConnectionState.Disconnected }
        // 默认 offlineText=null：快照无任何 System 提示行（流中断提示仅在有活跃流时出现，此处无流）。
        assertEquals(0, chat.historySnapshot().count { it.sender == SenderType.System })
    }

    @Test
    fun offlineHintEmittedOnHandshakeFailureWhenConfigured() = runBlocking {
        bypass.enqueue(MockResponse().setResponseCode(404))
        chat = newChat(branding = Branding(offlineText = "客服当前不在线，请稍后再来"))

        // onSubscription 钩在订阅点后才 connect：hint 是无 replay SharedFlow，回调线程
        // 可能在断言开始前就发射（CI release 变体实测翻车——本地 debug 恰好没翻）。
        val hint = chat.events.messages
            .onSubscription { chat.connect() }
            .first { it.sender == SenderType.System && it.content == "客服当前不在线，请稍后再来" }
        assertEquals("test-session", hint.sessionId)
        // notifyOfflineHint 在 set Disconnected 之后调用：hint 到达即蕴含终态已落。
        assertEquals(ConnectionState.Disconnected, chat.events.connectionState.value)
    }

    @Test
    fun offlineHintNotEmittedOnDestroy() = runBlocking {
        bypass.enqueue(MockResponse().withWebSocketUpgrade(EchoListener()))
        chat = newChat(branding = Branding(offlineText = "客服当前不在线，请稍后再来"))
        chat.connect()
        awaitConnected()

        chat.destroy()
        // 用户主动销毁 ≠ 客服离线：不追加提示行。
        assertTrue(chat.historySnapshot().none { it.sender == SenderType.System })
    }

    @Test
    fun appendSystemHintEntersHistoryAndStreamWithoutUnread() = runBlocking {
        bypass.enqueue(MockResponse().withWebSocketUpgrade(EchoListener()))
        chat = newChat()
        chat.connect()
        awaitConnected()

        // onSubscription：订阅注册完成后才触发 appendSystemHint（messages 无 replay，
        // 先订阅再触发的竞态口诀在此不适用——订阅注册本身异步生效，须钩在订阅点后）。
        val emitted = withTimeout(5_000) {
            chat.events.messages
                .onSubscription { chat.appendSystemHint("工单 #42 已创建，客服将尽快处理") }
                .first { it.sender == SenderType.System }
        }
        assertEquals("test-session", emitted.sessionId)
        // 进 history（面板 hide/重开后随快照回放）；不计未读。
        assertEquals(1, chat.historySnapshot().count { it.sender == SenderType.System })
        assertEquals(0, chat.events.unreadCount.value)
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
