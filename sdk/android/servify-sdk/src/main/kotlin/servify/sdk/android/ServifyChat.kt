package servify.sdk.android

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withTimeoutOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okhttp3.HttpUrl.Companion.toHttpUrl
import servify.sdk.android.connect.ConnectionState
import servify.sdk.android.connect.ReconnectPolicy
import servify.sdk.android.core.ProtocolEvent
import servify.sdk.android.core.SessionCore
import servify.sdk.android.model.AgentAssignment
import servify.sdk.android.model.ConversationMessage
import servify.sdk.android.model.SenderType
import servify.sdk.android.protocol.FrameCodec
import servify.sdk.android.protocol.WireFrame
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

/**
 * 移动端 SDK 门面（平台规格 §4.2/§4.3，V1 冻结面）。
 *
 * 惰性连接：create 不建连，首次 [connect]（UI 刀后由 show 触发）才发起 WS；
 * 断线按 [ReconnectPolicy]（对齐 Web core 参数）自动重连，握手失败直接 disconnected。
 *
 * 帧消费顺序保证：全部在 OkHttp 回调线程同步处理（同连接内串行），只把发射交给
 * 线程安全的 StateFlow/SharedFlow；重连调度才进协程作用域。
 * 回显判据按发送内容匹配——迟到的旧回显不会误完成下一次发送。
 */
class ServifyChat internal constructor(
    private val config: ServifyConfig,
    val sessionId: String,
    private val client: OkHttpClient,
    private val scope: CoroutineScope,
    private val policy: ReconnectPolicy = ReconnectPolicy(),
    /** 发送后等待回显判据的超时；生产默认 10s，测试缩短。 */
    private val echoTimeoutMs: Long = DEFAULT_ECHO_TIMEOUT_MS,
    /** 测试注入：绕过 config.apiUrl（生产路径恒为 https/wss 派生的 wss 地址）。 */
    private val wsUrlOverride: String? = null,
) {
    private val core = SessionCore()
    // 回调来自 OkHttp 不同线程池线程，跨线程读写的状态必须 volatile（守卫与重连计数依赖可见性）。
    @Volatile
    private var webSocket: WebSocket? = null
    private var reconnectJob: Job? = null
    @Volatile
    private var reconnectAttempt = 0
    @Volatile
    private var everConnected = false

    private var localSeq = 0L
    private var streamingId: String? = null
    private val streamedContent = StringBuilder()

    private val sendMutex = Mutex()
    private val pendingEcho = AtomicReference<EchoGate?>(null)

    private class EchoGate(val expectedContent: String, val gate: CompletableDeferred<Unit>)

    private val _connectionState = MutableStateFlow<ConnectionState>(ConnectionState.Idle)
    val connectionState: StateFlow<ConnectionState> = _connectionState.asStateFlow()

    private val _messages = MutableSharedFlow<ConversationMessage>(extraBufferCapacity = 256, onBufferOverflow = BufferOverflow.DROP_OLDEST)
    val messages: SharedFlow<ConversationMessage> = _messages.asSharedFlow()

    private val _unreadCount = MutableStateFlow(0)
    val unreadCount: StateFlow<Int> = _unreadCount.asStateFlow()

    private val _reconnecting = MutableSharedFlow<Int>(extraBufferCapacity = 16)
    val reconnecting: SharedFlow<Int> = _reconnecting.asSharedFlow()

    private val _agentAssigned = MutableSharedFlow<AgentAssignment>(extraBufferCapacity = 16)
    val agentAssigned: SharedFlow<AgentAssignment> = _agentAssigned.asSharedFlow()

    private val _waitingInQueue = MutableSharedFlow<String>(extraBufferCapacity = 16)
    val waitingInQueue: SharedFlow<String> = _waitingInQueue.asSharedFlow()

    private val _errors = MutableSharedFlow<ServifyError>(extraBufferCapacity = 16)
    val error: SharedFlow<ServifyError> = _errors.asSharedFlow()

    /** 惰性连接入口（§4.4：disconnected 后用户再次打开会话页即重新 connecting）。 */
    suspend fun connect() {
        if (_connectionState.value == ConnectionState.Connected ||
            _connectionState.value == ConnectionState.Connecting
        ) {
            return
        }
        reconnectJob?.cancel()
        reconnectJob = null
        reconnectAttempt = 0
        _connectionState.value = ConnectionState.Connecting
        openSocket()
    }

    /**
     * 发送客户消息（V1 仅 text）。成功判据=收到自己回显帧（PROTOCOL.md §6.3），
     * 超时发 [ServifyError.SendTimeout]（retryable）——不本地伪造成功。
     */
    suspend fun sendMessage(text: String) {
        sendMutex.withLock {
            val ws = webSocket
            if (ws == null) {
                _errors.emit(ServifyError.Network("not connected"))
                return
            }
            val echoGate = EchoGate(text, CompletableDeferred())
            pendingEcho.set(echoGate)
            val sent = ws.send(FrameCodec.encodeTextMessage(text, sessionId))
            if (!sent) {
                pendingEcho.set(null)
                _errors.emit(ServifyError.Network("websocket send rejected (closing)"))
                return
            }
            val echoed = withTimeoutOrNull(echoTimeoutMs) {
                echoGate.gate.await()
                true
            } ?: false
            pendingEcho.compareAndSet(echoGate, null)
            if (!echoed) {
                _errors.emit(ServifyError.SendTimeout("no echo within ${echoTimeoutMs}ms"))
            }
        }
    }

    /** 会话页可见时清零未读（§4.3 unreadCount 语义；UI 刀内部接线）。 */
    internal fun onSessionVisible() {
        _unreadCount.value = 0
    }

    /** 销毁：断连接、取消作用域（含未决重连）；快照保留属持久化刀职责。 */
    fun destroy() {
        webSocket?.cancel()
        webSocket = null
        scope.cancel()
        _connectionState.value = ConnectionState.Disconnected
    }

    private fun openSocket() {
        val url = wsUrlOverride ?: deriveWsUrl(config.apiUrl)
        val request = Request.Builder().url(url).build()
        webSocket = client.newWebSocket(request, WsListener())
    }

    private fun deriveWsUrl(apiUrl: String): String {
        val base = if (apiUrl.startsWith("https://")) "wss://" + apiUrl.removePrefix("https://") else apiUrl
        return base.toHttpUrl().newBuilder()
            .addPathSegments("api/v1/ws")
            .addQueryParameter("session_id", sessionId)
            .build()
            .toString()
    }

    private fun onTransportFailure(t: Throwable, httpStatus: Int?) {
        webSocket = null
        if (!everConnected) {
            // §4.4：握手失败 → disconnected（配置/服务端问题，退避重试无意义）。
            _errors.tryEmit(
                when {
                    httpStatus != null && httpStatus >= 500 -> ServifyError.ServerUnavailable("handshake $httpStatus: ${t.message}")
                    httpStatus != null -> ServifyError.HandshakeRejected("handshake rejected ($httpStatus): ${t.message}")
                    else -> ServifyError.Network("handshake failed: ${t.message}")
                },
            )
            _connectionState.value = ConnectionState.Disconnected
            return
        }
        scheduleReconnect()
    }

    private fun scheduleReconnect() {
        reconnectAttempt += 1
        val delayMs = policy.delayFor(reconnectAttempt)
        if (delayMs == null) {
            _errors.tryEmit(ServifyError.Network("reconnect exhausted after ${policy.maxAttempts} attempts"))
            _connectionState.value = ConnectionState.Disconnected
            return
        }
        _reconnecting.tryEmit(reconnectAttempt)
        _connectionState.value = ConnectionState.Reconnecting(reconnectAttempt)
        reconnectJob = scope.launch {
            delay(delayMs)
            _connectionState.value = ConnectionState.Connecting
            openSocket()
        }
    }

    private fun handleFrame(raw: String) {
        val frame = FrameCodec.decode(raw)
        val event = core.consume(frame)
        when (frame) {
            is WireFrame.VisitorEcho -> {
                emitMessage(SenderType.Customer, frame.content)
                pendingEcho.get()?.let { gate ->
                    if (gate.expectedContent == frame.content) gate.gate.complete(Unit)
                }
            }
            is WireFrame.AgentMessage -> {
                emitMessage(SenderType.Agent, frame.content)
                _unreadCount.value += 1
            }
            is WireFrame.AiResponseDelta -> {
                val delta = event as ProtocolEvent.AiDelta
                if (!delta.done) {
                    if (streamingId == null) streamingId = nextId()
                    streamedContent.append(delta.contentDelta)
                    _messages.tryEmit(
                        ConversationMessage(
                            id = streamingId!!,
                            sessionId = sessionId,
                            sender = SenderType.System,
                            content = streamedContent.toString(),
                            createdAt = now(),
                            isAiResponse = true,
                            isStreaming = true,
                        ),
                    )
                }
                // 终末增量（done=true，content_delta 为空）不产生气泡更新，等 ai-response 终帧。
            }
            is WireFrame.AiResponse -> {
                streamedContent.setLength(0)
                val id = streamingId ?: nextId()
                streamingId = null
                _messages.tryEmit(
                    ConversationMessage(
                        id = id,
                        sessionId = sessionId,
                        sender = SenderType.System,
                        content = frame.content,
                        createdAt = now(),
                        isAiResponse = true,
                        isStreaming = false,
                        sources = frame.sources.orEmpty(),
                        confidence = frame.confidence,
                    ),
                )
            }
            is WireFrame.TransferNotification -> {
                _agentAssigned.tryEmit(AgentAssignment(frame.agentId, frame.message))
            }
            is WireFrame.WaitingNotification -> {
                _waitingInQueue.tryEmit(frame.message)
            }
            is WireFrame.WebRtcSignal, is WireFrame.Unknown -> Unit // 移动端契约显式忽略
        }
    }

    private fun emitMessage(sender: SenderType, content: String) {
        _messages.tryEmit(
            ConversationMessage(
                id = nextId(),
                sessionId = sessionId,
                sender = sender,
                content = content,
                createdAt = now(),
            ),
        )
    }

    private fun nextId(): String = "ws-${++localSeq}"

    private fun now(): Long = System.currentTimeMillis()

    private inner class WsListener : WebSocketListener() {
        override fun onOpen(webSocket: WebSocket, response: Response) {
            if (webSocket !== this@ServifyChat.webSocket) return // 过期套接字（已被新连接取代）
            everConnected = true
            reconnectAttempt = 0
            _connectionState.value = ConnectionState.Connected
        }

        override fun onMessage(webSocket: WebSocket, text: String) {
            if (webSocket !== this@ServifyChat.webSocket) return
            handleFrame(text)
        }

        override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
            if (webSocket !== this@ServifyChat.webSocket) return
            // §4.4：connected ─(断)→ reconnecting。服务端主动关（闲置踢线/代理超时）
            // 对移动端同为断线，按退避策略恢复（D7 增量补拉的挂载点）。
            this@ServifyChat.webSocket = null
            scheduleReconnect()
        }

        override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
            if (webSocket !== this@ServifyChat.webSocket) return
            onTransportFailure(t, response?.code)
        }
    }

    companion object {
        /** 回显判据默认超时（PROTOCOL.md §6.3 成功判据的本地兜底）。 */
        const val DEFAULT_ECHO_TIMEOUT_MS = 10_000L

        /** 构造门面：载入配置，不建连（惰性连接）。重复 create 由接入方避免（单例语义，文档明示）。 */
        fun create(config: ServifyConfig, sessionId: String): ServifyChat {
            val client = OkHttpClient.Builder()
                // PROTOCOL.md §2 移动端口径：协议层保活走平台 WS 实现。
                .pingInterval(30, TimeUnit.SECONDS)
                .build()
            return ServifyChat(
                config = config,
                sessionId = sessionId,
                client = client,
                scope = CoroutineScope(SupervisorJob() + Dispatchers.Default),
            )
        }
    }
}
