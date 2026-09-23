package servify.sdk.android

import android.app.Activity
import android.content.Context
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
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.put
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okhttp3.RequestBody.Companion.toRequestBody
import servify.sdk.android.connect.ConnectionState
import servify.sdk.android.connect.ReconnectPolicy
import servify.sdk.android.core.ProtocolEvent
import servify.sdk.android.core.SessionCore
import servify.sdk.android.model.AgentAssignment
import servify.sdk.android.model.ConversationMessage
import servify.sdk.android.model.SenderType
import servify.sdk.android.model.TicketReceipt
import servify.sdk.android.protocol.FrameCodec
import servify.sdk.android.protocol.WireFrame
import java.io.IOException
import java.util.UUID
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
    /** 测试注入：工单创建端点同款（生产路径恒为 apiUrl + /api/v1/tickets）。 */
    private val ticketUrlOverride: String? = null,
) {
    private val core = SessionCore()

    /**
     * 会话消息累积（D7 口径：内存级，不做磁盘持久化同步协议）。hide() 收面板后连接保持、
     * 消息照常累积，面板重开经 [historySnapshot] 完整恢复渲染。OkHttp 线程写、UI 线程读。
     */
    private val history = java.util.concurrent.CopyOnWriteArrayList<ConversationMessage>()

    /** 会话页可见性（未读"本地未渲染过"判据；onSessionVisible/Hidden 由面板接线驱动）。 */
    @Volatile
    private var sessionVisible = false
    // 回调来自 OkHttp 不同线程池线程，跨线程读写的状态必须 volatile（守卫与重连计数依赖可见性）。
    @Volatile
    private var webSocket: WebSocket? = null
    private var reconnectJob: Job? = null
    @Volatile
    private var reconnectAttempt = 0
    @Volatile
    private var everConnected = false

    /** 入口编排（浮钮/面板），与门面生命周期同步。 */
    private val entry = EntryOrchestrator(this, config, scope)

    private var localSeq = 0L
    @Volatile
    private var streamingId: String? = null
    private val streamedContent = StringBuilder()

    private val sendMutex = Mutex()
    private val pendingEcho = AtomicReference<EchoGate?>(null)

    private class EchoGate(val expectedContent: String, val gate: CompletableDeferred<Unit>)

    private val _connectionState = MutableStateFlow<ConnectionState>(ConnectionState.Idle)
    private val _messages = MutableSharedFlow<ConversationMessage>(extraBufferCapacity = 256, onBufferOverflow = BufferOverflow.DROP_OLDEST)
    private val _unreadCount = MutableStateFlow(0)
    private val _reconnecting = MutableSharedFlow<Int>(extraBufferCapacity = 16)
    private val _agentAssigned = MutableSharedFlow<AgentAssignment>(extraBufferCapacity = 16)
    private val _waitingInQueue = MutableSharedFlow<String>(extraBufferCapacity = 16)
    private val _errors = MutableSharedFlow<ServifyError>(extraBufferCapacity = 16)

    /** 事件流聚合面（§4.3 V1 冻结面）。 */
    val events: ServifyEvents = ServifyEvents(
        messages = _messages.asSharedFlow(),
        unreadCount = _unreadCount.asStateFlow(),
        connectionState = _connectionState.asStateFlow(),
        reconnecting = _reconnecting.asSharedFlow(),
        agentAssigned = _agentAssigned.asSharedFlow(),
        waitingInQueue = _waitingInQueue.asSharedFlow(),
        error = _errors.asSharedFlow(),
    )

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
     * 拉起会话 UI（§4.2：抽屉或全屏，首次调用触发 WS 连接）。
     * 挂起连接经内部作用域异步发起，宿主无需协程上下文。
     * coverage-exempt（framework 面）：body 依赖 Activity/EntryOrchestrator（Android
     * runtime，JVM 单测无此环境；白名单冻结不引 Robolectric），由编译期 + 真机手工项覆盖。
     */
    fun show(hostActivity: Activity) {
        scope.launch { connect() }
        entry.attach(hostActivity)
    }

    /** 收起会话 UI，连接保持（§4.2）。浮钮入口保留；面板收起属 UI 刀接线。
     * coverage-exempt（framework 面）：同 [show]。 */
    fun hide() {
        entry.detachPanel()
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
            // coverage-exempt（okhttp 不可注入）：sent=false 需"当前 socket 已 closing 而
            // onClosed 未回调"的窗口——真 OkHttp 的 close 状态机不暴露注入口，无 mock 接缝，
            // 强行时序凑测必 flaky；生产面该行由 WS 协议栈自身兜底（send 拒绝即断线）。
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
        sessionVisible = true
        _unreadCount.value = 0
    }

    /**
     * 会话页工单创建（M3，§10 #4：POST {apiUrl}/api/v1/tickets，免认证访客端点）。
     * ai_summary 由会话历史自动组装（[TicketSummary]，坐席侧直接可读），无对话则
     * 不携带该字段。请求-响应语义：成功返回回执、失败返回 null（错误经 _errors
     * 流同步发出，与 sendMessage 同风格）——调用方（UI 刀）按 null 在表单上反馈。
     */
    suspend fun createTicket(title: String, description: String? = null): TicketReceipt? =
        withContext(Dispatchers.IO) {
            val url = (ticketUrlOverride ?: config.apiUrl.trimEnd('/') + "/api/v1/tickets")
            val body = buildJsonObject {
                put("session_id", sessionId)
                put("title", title)
                if (!description.isNullOrBlank()) put("description", description)
                TicketSummary.build(historySnapshot())?.let { put("ai_summary", it) }
            }.toString()
            val request = Request.Builder()
                .url(url)
                .post(body.toRequestBody("application/json; charset=utf-8".toMediaType()))
                .build()
            val response = try {
                client.newCall(request).execute()
            } catch (e: IOException) {
                _errors.emit(ServifyError.Network("ticket request failed: ${e.message}"))
                return@withContext null
            }
            response.use { resp ->
                val text = resp.body?.string().orEmpty()
                if (!resp.isSuccessful) {
                    _errors.emit(ServifyError.TicketFailed("ticket http ${resp.code}: ${text.take(200)}"))
                    return@withContext null
                }
                val id = runCatching {
                    json.parseToJsonElement(text).jsonObject["id"]?.jsonPrimitive?.longOrNull
                }.getOrNull()
                if (id == null) {
                    _errors.emit(ServifyError.TicketFailed("ticket response missing id: ${text.take(200)}"))
                    return@withContext null
                }
                TicketReceipt(ticketId = id)
            }
        }

    /** 会话页收起（hide()/点 scrim）：连接保持，此后到达的消息计入未读。 */
    internal fun onSessionHidden() {
        sessionVisible = false
    }

    /** 当前累积消息快照（面板初始化回放；同 id 后到覆盖，与渲染层合并规则一致）。 */
    internal fun historySnapshot(): List<ConversationMessage> = history.toList()

    /**
     * 追加 SDK 自造的系统提示行（M3 工单创建成功提示等 UI 刀接线用；协议无此帧，
     * 不计未读）——进 history，面板 hide/重开后随快照回放。
     */
    internal fun appendSystemHint(text: String) {
        recordAndEmit(
            ConversationMessage(
                id = nextId(),
                sessionId = sessionId,
                sender = SenderType.System,
                content = text,
                createdAt = now(),
            ),
        )
    }

    /** 销毁：断连接、取消作用域（含未决重连）、移除 UI 挂载与累积快照。 */
    fun destroy() {
        entry.release()
        webSocket?.cancel()
        webSocket = null
        scope.cancel()
        history.clear()
        sessionVisible = false
        _connectionState.value = ConnectionState.Disconnected
    }

    /**
     * 推送注册口（M3，平台规格 §4 registerPushToken；D7 可选性）：
     * - [ServifyConfig.pushTokenProvider] 未配置 → Unsupported 错误（能力未启用）；
     * - 配置但 provider 返回 null（宿主未授权/无 token）→ 静默返回 false（非错误）；
     * - 取到 token → 上报服务端推送端点。服务端端点未上线（§10 #5），过渡期报
     *   Unsupported——端点落地后仅补本方法的上报实现，冻结面不变。
     */
    suspend fun registerPushToken(): Boolean {
        val provider = config.pushTokenProvider
        if (provider == null) {
            _errors.emit(ServifyError.Unsupported("push not configured"))
            return false
        }
        val token = provider()
        if (token.isNullOrBlank()) {
            return false
        }
        // §10 #5 未上线：上报端点落地前 token 无处可报，过渡期显式 unsupported。
        _errors.emit(ServifyError.Unsupported("push registration endpoint not available"))
        return false
    }

    private fun openSocket() {
        val url = buildWsUrl(config.apiUrl, wsUrlOverride, config.guestToken)
        val request = Request.Builder().url(url).build()
        webSocket = client.newWebSocket(request, WsListener())
    }

    /**
     * 测试接缝：客户端本地断开。CI 环境下 server 端 `cancel()` 的断线传播偶发丢失
     * （MockWebServer WS teardown 同源问题——连接 close 类操作依赖读循环传播），
     * 重连类用例会等满整个超时预算（35855565576）。本地 cancel 立即在客户端触发
     * onFailure，与真实断线走同一条 onTransportFailure → scheduleReconnect 路径，
     * 用例因此完全确定。
     */
    internal fun disconnectForTesting() {
        webSocket?.cancel()
    }

    /**
     * 最终 WS URL：apiUrl 推导或宿主显式 override；guestToken 非空时带 `access_token`
     * 查询参数（PROTOCOL §1：服务端当前不消费，访客 token 端点落地后自动生效，向后兼容）。
     * 注意不能用 HttpUrl 二次解析——ws/wss scheme 不在 okhttp HttpUrl 的合法集内，手工拼接。
     */
    internal fun buildWsUrl(apiUrl: String, override: String?, guestToken: String?): String {
        val base = override ?: deriveWsUrl(apiUrl)
        if (guestToken == null) return base
        val separator = if (base.contains('?')) "&" else "?"
        return base + separator + "access_token=" + java.net.URLEncoder.encode(guestToken, "UTF-8")
    }

    private fun deriveWsUrl(apiUrl: String): String {
        // 不能走 HttpUrl 二次解析：ws/wss scheme 不在 okhttp HttpUrl 合法集内（会抛
        // IllegalArgumentException）。手工拼接；apiUrl 带 query 的场景 V1 不支持。
        val base = if (apiUrl.startsWith("https://")) "wss://" + apiUrl.removePrefix("https://") else apiUrl
        return base.trimEnd('/') + "/api/v1/ws?session_id=" + java.net.URLEncoder.encode(sessionId, "UTF-8")
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
            notifyOfflineHint()
            return
        }
        scheduleReconnect()
    }

    private fun scheduleReconnect() {
        finalizeInterruptedStream()
        reconnectAttempt += 1
        val delayMs = policy.delayFor(reconnectAttempt)
        if (delayMs == null) {
            _errors.tryEmit(ServifyError.Network("reconnect exhausted after ${policy.maxAttempts} attempts"))
            _connectionState.value = ConnectionState.Disconnected
            notifyOfflineHint()
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
                // 未读 = 面板不可见时到达的坐席/AI 内容（设计 §移动端未读口径）。
                if (!sessionVisible) _unreadCount.value += 1
            }
            is WireFrame.AiResponseDelta -> {
                val delta = event as ProtocolEvent.AiDelta
                if (!delta.done) {
                    if (streamingId == null) streamingId = nextId()
                    streamedContent.append(delta.contentDelta)
                    recordAndEmit(
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
                recordAndEmit(
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
                        nextAction = frame.nextAction,
                    ),
                )
                if (!sessionVisible) _unreadCount.value += 1
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
        recordAndEmit(
            ConversationMessage(
                id = nextId(),
                sessionId = sessionId,
                sender = sender,
                content = content,
                createdAt = now(),
            ),
        )
    }

    /** 累积（同 id 后到覆盖，与渲染层合并规则一致）后发往事件流。 */
    private fun recordAndEmit(message: ConversationMessage) {
        val index = history.indexOfLast { it.id == message.id }
        if (index >= 0) history[index] = message else history += message
        _messages.tryEmit(message)
    }

    /**
     * 流中断收口（PROTOCOL §4.1：终末增量已到但无 ai-response 终帧 = 本次回答失败；
     * 断连时流必然中断）——保留已渲染部分（翻 isStreaming=false）+ 追加提示行（D8）。
     * 提示行是 SDK 自造的 UI 状态行（协议无此帧），不计未读。
     */
    private fun finalizeInterruptedStream() {
        val id = streamingId ?: return
        streamingId = null
        streamedContent.setLength(0)
        val index = history.indexOfLast { it.id == id }
        if (index >= 0) {
            val partial = history[index].copy(isStreaming = false)
            history[index] = partial
            _messages.tryEmit(partial)
        }
        appendSystemHint("回答中断，请重试")
    }

    private fun nextId(): String = "ws-${++localSeq}"

    private fun now(): Long = System.currentTimeMillis()

    /**
     * 离线提示（Branding.offlineText）：disconnected 终态（握手失败/重连耗尽）时追加的
     * 系统提示行——同流中断提示模式：SDK 自造的 UI 状态行（协议无此帧），不计未读。
     * destroy 不提示——用户主动关闭不等于客服离线。
     */
    private fun notifyOfflineHint() {
        val text = config.branding.offlineText ?: return
        appendSystemHint(text)
    }

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
            // coverage-exempt（MockWebServer close 族传播全断）：客户端发起 close(1000)
            // 的协议对称回执在 MockWebServer 下同样不传播（loopReader 阻塞同源），onClosed
            // 无法在环回环境触发——路径逻辑与 onFailure 断线支同构（置空 + 退避重连），
            // 重连段已由 disconnectForTesting 系用例锚定。
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

        /** 工单响应解析（仅取 id 字段；与 FrameCodec 同款 ignoreUnknownKeys 口径）。 */
        private val json = Json { ignoreUnknownKeys = true }

        /**
         * 构造门面（§4.2）：载入配置，不建连（惰性连接）；sessionId 由 SDK 生成
         * （"m-" + UUID，D5/PROTOCOL §1 匿名 session 模式）。
         * `context` 为宿主上下文（可选）——SessionSnapshot/安全存储载入属持久化刀消费，
         * V1 快照未启用前不读不写。
         * 重复 create 由接入方避免（单例语义，文档明示）。
         */
        fun create(context: Context?, config: ServifyConfig): ServifyChat {
            val client = OkHttpClient.Builder()
                // PROTOCOL.md §2 移动端口径：协议层保活走平台 WS 实现。
                .pingInterval(30, TimeUnit.SECONDS)
                .build()
            return ServifyChat(
                config = config,
                sessionId = "m-${UUID.randomUUID()}",
                client = client,
                scope = CoroutineScope(SupervisorJob() + Dispatchers.Default),
            )
        }
    }
}
