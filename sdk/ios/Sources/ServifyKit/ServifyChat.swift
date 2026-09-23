import Foundation

/// 移动端 SDK 门面（平台规格 §4.2/§4.3，V1 冻结面；Kotlin 镜像：ServifyChat.kt）。
///
/// 惰性连接：create 不建连，首次 `connect()`（UI 刀后由 show 触发）才发起 WS；
/// 断线按 `ReconnectPolicy`（对齐 Web core 参数）自动重连，握手失败直接 disconnected。
///
/// 帧消费顺序保证：全部在传输回调线程同步处理（同连接内串行），只把发射交给
/// 线程安全的 EventStream/StateStream；重连调度才进 Task。可变状态由 stateLock
/// 守卫（对齐 Kotlin 的 @Volatile + CopyOnWriteArrayList 口径）。
/// 回显判据按发送内容匹配——迟到的旧回显不会误完成下一次发送。
public final class ServifyChat: @unchecked Sendable {

    /// 回显判据默认超时（PROTOCOL.md §6.3 成功判据的本地兜底）。
    public static let defaultEchoTimeoutMs = 10_000

    /// 构造门面（§4.2）：载入配置，不建连（惰性连接）；sessionId 由 SDK 生成
    /// （"m-" + UUID，D5/PROTOCOL §1 匿名 session 模式）。
    /// 重复 create 由接入方避免（单例语义，文档明示）。
    public static func create(config: ServifyConfig) -> ServifyChat {
        #if canImport(Darwin)
        return ServifyChat(
            config: config,
            sessionId: "m-" + UUID().uuidString,
            transportFactory: { URLSessionWebSocketTransport() }
        )
        #else
        // Linux（CI 测试面）无生产传输实现；测试直接构造并注入 mock 传输。
        fatalError("ServifyChat.create 仅支持 Darwin 平台")
        #endif
    }

    let config: ServifyConfig
    /// 会话标识（匿名 session 模式，随 SDK 生命周期）。
    public let sessionId: String
    private let policy: ReconnectPolicy
    private let echoTimeoutMs: Int
    /// 测试注入：绕过 config.apiUrl（生产路径恒为 https/wss 派生的 wss 地址）。
    private let wsUrlOverride: String?
    private let transportFactory: () -> WebSocketTransport

    private let core = SessionCore()

    /// 会话消息累积（D7 口径：内存级，不做磁盘持久化同步协议）。hide() 收面板后连接保持、
    /// 消息照常累积，面板重开经 historySnapshot() 完整恢复渲染。
    private var history: [ConversationMessage] = []

    /// 回显判据闸（内容匹配）；同一时刻至多一个未决发送（sendMutex 串行）。
    private var pendingEcho: EchoGate?

    // 以下可变状态由 stateLock 守卫（回调线程写、订阅方/宿主线程读）。
    private let stateLock = NSLock()
    private var currentTransport: WebSocketTransport?
    private var reconnectTask: Task<Void, Never>?
    private var reconnectAttempt = 0
    private var everConnected = false
    private var destroyed = false
    /// 会话页可见性（未读"本地未渲染过"判据；onSessionVisible/Hidden 由面板接线驱动）。
    private var sessionVisible = false
    private var localSeq: Int64 = 0
    private var streamingId: String?
    private var streamedContent = ""

    /// 串行发送闸：防止并发 sendMessage 互相覆盖 pendingEcho。
    private let sendMutex = AsyncMutex()

    private let _connectionState = StateStream<ConnectionState>(.idle)
    private let _messages = EventStream<ConversationMessage>()
    private let _unreadCount = StateStream<Int>(0)
    private let _reconnecting = EventStream<Int>()
    private let _agentAssigned = EventStream<AgentAssignment>()
    private let _waitingInQueue = EventStream<String>()
    private let _errors = EventStream<ServifyError>()

    /// 事件流聚合面（§4.3 V1 冻结面）。
    public let events: ServifyEvents

    init(
        config: ServifyConfig,
        sessionId: String,
        policy: ReconnectPolicy = .standard,
        echoTimeoutMs: Int = ServifyChat.defaultEchoTimeoutMs,
        wsUrlOverride: String? = nil,
        transportFactory: @escaping () -> WebSocketTransport
    ) {
        self.config = config
        self.sessionId = sessionId
        self.policy = policy
        self.echoTimeoutMs = echoTimeoutMs
        self.wsUrlOverride = wsUrlOverride
        self.transportFactory = transportFactory
        events = ServifyEvents(
            messages: _messages,
            unreadCount: _unreadCount,
            connectionState: _connectionState,
            reconnecting: _reconnecting,
            agentAssigned: _agentAssigned,
            waitingInQueue: _waitingInQueue,
            error: _errors
        )
    }

    // MARK: - 冻结面（宿主 API）

    /// 惰性连接入口（§4.4：disconnected 后用户再次打开会话页即重新 connecting）。幂等。
    public func connect() async {
        let current = _connectionState.value
        if current == .connected || current == .connecting { return }
        cancelPendingReconnect()
        _connectionState.set(.connecting)
        openSocket()
    }

    /**
     * 发送客户消息（V1 仅 text）。成功判据=收到自己回显帧（PROTOCOL.md §6.3），
     * 超时发 sendTimeout（retryable）——不本地伪造成功。
     */
    public func sendMessage(_ text: String) async {
        await sendMutex.withLock {
            guard let transport = currentTransportNow() else {
                _errors.emit(.network(message: "not connected"))
                return
            }
            let gate = EchoGate(expectedContent: text)
            setPendingEcho(gate)
            let sent = transport.send(FrameCodec.encodeTextMessage(text, sessionId: sessionId))
            if !sent {
                clearPendingEcho(matching: gate)
                _errors.emit(.network(message: "websocket send rejected (closing)"))
                return
            }
            let echoed = await gate.wait(timeoutMs: echoTimeoutMs)
            clearPendingEcho(matching: gate)
            if !echoed {
                _errors.emit(.sendTimeout(message: "no echo within \(echoTimeoutMs)ms"))
            }
        }
    }

    /** 会话页可见时清零未读（§4.3 unreadCount 语义；UI 刀内部接线）。 */
    func onSessionVisible() {
        stateLock.lock()
        sessionVisible = true
        stateLock.unlock()
        _unreadCount.set(0)
    }

    /** 会话页收起（hide()/点 scrim）：连接保持，此后到达的消息计入未读。 */
    func onSessionHidden() {
        stateLock.lock()
        sessionVisible = false
        stateLock.unlock()
    }

    /** 当前累积消息快照（面板初始化回放；同 id 后到覆盖，与渲染层合并规则一致）。 */
    func historySnapshot() -> [ConversationMessage] {
        stateLock.lock()
        defer { stateLock.unlock() }
        return history
    }

    /** 销毁：断连接、取消未决重连、清累积快照。过期套接字回调经身份守卫自然失效。 */
    public func destroy() {
        stateLock.lock()
        destroyed = true
        let transport = currentTransport
        currentTransport = nil
        let pending = reconnectTask
        reconnectTask = nil
        pendingEcho = nil
        history.removeAll()
        sessionVisible = false
        stateLock.unlock()
        transport?.cancel()
        pending?.cancel()
        _connectionState.set(.disconnected)
    }

    // MARK: - 连接生命周期

    private func openSocket() {
        let url = buildWsUrl(config.apiUrl, wsUrlOverride, config.guestToken)
        let transport = transportFactory()
        stateLock.lock()
        currentTransport = transport
        stateLock.unlock()
        transport.connect(to: url, listener: WsListener(facade: self, transport: transport))
    }

    /**
     * 最终 WS URL：apiUrl 推导或宿主显式 override；guestToken 非空时带 `access_token`
     * 查询参数（PROTOCOL §1：服务端当前不消费，访客 token 端点落地后自动生效，向后兼容）。
     * 不能走 URL/URLComponents 二次解析后再拼——手工拼接与 Kotlin 侧保持字符串级同构。
     */
    func buildWsUrl(_ apiUrl: String, _ override: String?, _ guestToken: String?) -> String {
        let base = override ?? deriveWsUrl(apiUrl)
        guard let guestToken else { return base }
        let separator = base.contains("?") ? "&" : "?"
        return base + separator + "access_token=" + Self.formEncode(guestToken)
    }

    private func deriveWsUrl(_ apiUrl: String) -> String {
        // apiUrl 带 query 的场景 V1 不支持（对齐 Kotlin 注释）。
        let base: String
        if apiUrl.hasPrefix("https://") {
            base = "wss://" + apiUrl.dropFirst("https://".count)
        } else {
            base = apiUrl
        }
        let trimmed = base.hasSuffix("/") ? String(base.dropLast()) : base
        return trimmed + "/api/v1/ws?session_id=" + Self.formEncode(sessionId)
    }

    /// java.net.URLEncoder.encode(UTF-8) 同口径（application/x-www-form-urlencoded：
    /// 保留字母数字与 .-*_，空格编为 +）——两侧生成串逐字符一致，测试可跨端锚定。
    private static func formEncode(_ value: String) -> String {
        var allowed = CharacterSet.alphanumerics
        allowed.insert(charactersIn: "*-._")
        let percent = value.addingPercentEncoding(withAllowedCharacters: allowed) ?? value
        return percent.replacingOccurrences(of: "%20", with: "+")
    }

    fileprivate func handleOpen() {
        stateLock.lock()
        everConnected = true
        reconnectAttempt = 0
        stateLock.unlock()
        _connectionState.set(.connected)
    }

    fileprivate func handleClosed() {
        stateLock.lock()
        currentTransport = nil
        stateLock.unlock()
        // §4.4：connected ─(断)→ reconnecting。服务端主动关（闲置踢线/代理超时）
        // 对移动端同为断线，按退避策略恢复（D7 增量补拉的挂载点）。
        scheduleReconnect()
    }

    fileprivate func handleFailure(_ error: Error, httpStatus: Int?) {
        stateLock.lock()
        currentTransport = nil
        stateLock.unlock()
        if !isEverConnected() {
            // §4.4：握手失败 → disconnected（配置/服务端问题，退避重试无意义）。
            let err: ServifyError
            if let status = httpStatus, status >= 500 {
                err = .serverUnavailable(message: "handshake \(status): \(error.localizedDescription)")
            } else if httpStatus != nil {
                err = .handshakeRejected(message: "handshake rejected (\(httpStatus!)): \(error.localizedDescription)")
            } else {
                err = .network(message: "handshake failed: \(error.localizedDescription)")
            }
            _errors.emit(err)
            _connectionState.set(.disconnected)
            return
        }
        scheduleReconnect()
    }

    private func scheduleReconnect() {
        if isDestroyed() { return }
        finalizeInterruptedStream()
        stateLock.lock()
        reconnectAttempt += 1
        let attempt = reconnectAttempt
        stateLock.unlock()
        guard let delayMs = policy.delayFor(attempt) else {
            _errors.emit(.network(message: "reconnect exhausted after \(policy.maxAttempts) attempts"))
            _connectionState.set(.disconnected)
            return
        }
        _reconnecting.emit(attempt)
        _connectionState.set(.reconnecting(attempt: attempt))
        let task = Task { [weak self] in
            try? await Task.sleep(nanoseconds: UInt64(delayMs) * 1_000_000)
            guard let self, !Task.isCancelled, !self.isDestroyed() else { return }
            self._connectionState.set(.connecting)
            self.openSocket()
        }
        stateLock.lock()
        reconnectTask = task
        stateLock.unlock()
    }

    // MARK: - 帧消费（传输回调线程串行）

    fileprivate func handleFrame(_ raw: String) {
        let frame = FrameCodec.decode(raw)
        // 契约违约（终末增量非空等）不因单帧崩溃生产：吞掉并继续（Kotlin 侧 require
        // 抛出由测试面断言；门面侧降级处理，见 README 已知边界）。
        _ = try? core.consume(frame)
        switch frame {
        case let .visitorEcho(_, _, content):
            emitMessage(.customer, content)
            stateLock.lock()
            let gate = pendingEcho
            stateLock.unlock()
            gate?.completeIfExpected(content)

        case let .agentMessage(_, _, content, _):
            emitMessage(.agent, content)
            // 未读 = 面板不可见时到达的坐席/AI 内容（设计 §移动端未读口径）。
            bumpUnreadIfHidden()

        case let .aiResponseDelta(_, _, contentDelta, done):
            if !done {
                stateLock.lock()
                if streamingId == nil { streamingId = nextIdLocked() }
                let id = streamingId!
                streamedContent += contentDelta
                let accumulated = streamedContent
                stateLock.unlock()
                recordAndEmit(ConversationMessage(
                    id: id,
                    sessionId: sessionId,
                    sender: .system,
                    content: accumulated,
                    createdAt: now(),
                    isAiResponse: true,
                    isStreaming: true
                ))
            }
            // 终末增量（done=true，content_delta 为空）不产生气泡更新，等 ai-response 终帧。

        case let .aiResponse(_, _, content, confidence, _, sources, _, nextAction, _):
            stateLock.lock()
            streamedContent = ""
            let id = streamingId ?? nextIdLocked()
            streamingId = nil
            stateLock.unlock()
            recordAndEmit(ConversationMessage(
                id: id,
                sessionId: sessionId,
                sender: .system,
                content: content,
                createdAt: now(),
                isAiResponse: true,
                isStreaming: false,
                sources: sources ?? [],
                confidence: confidence,
                nextAction: nextAction
            ))
            bumpUnreadIfHidden()

        case let .transferNotification(_, _, message, agentId):
            _agentAssigned.emit(AgentAssignment(agentId: agentId, message: message))

        case let .waitingNotification(_, _, message):
            _waitingInQueue.emit(message)

        case .webRtcSignal, .unknown:
            break // 移动端契约显式忽略
        }
    }

    private func emitMessage(_ sender: SenderType, _ content: String) {
        stateLock.lock()
        let id = nextIdLocked()
        stateLock.unlock()
        recordAndEmit(ConversationMessage(
            id: id,
            sessionId: sessionId,
            sender: sender,
            content: content,
            createdAt: now()
        ))
    }

    /// 累积（同 id 后到覆盖，与渲染层合并规则一致）后发往事件流。
    private func recordAndEmit(_ message: ConversationMessage) {
        stateLock.lock()
        if let index = history.lastIndex(where: { $0.id == message.id }) {
            history[index] = message
        } else {
            history.append(message)
        }
        stateLock.unlock()
        _messages.emit(message)
    }

    /**
     * 流中断收口（PROTOCOL §4.1：终末增量已到但无 ai-response 终帧 = 本次回答失败；
     * 断连时流必然中断）——保留已渲染部分（翻 isStreaming=false）+ 追加提示行（D8）。
     * 提示行是 SDK 自造的 UI 状态行（协议无此帧），不计未读。
     */
    private func finalizeInterruptedStream() {
        stateLock.lock()
        guard let id = streamingId else {
            stateLock.unlock()
            return
        }
        streamingId = nil
        streamedContent = ""
        let index = history.lastIndex(where: { $0.id == id })
        var partial: ConversationMessage?
        if let index {
            let flipped = ConversationMessage(
                id: history[index].id,
                sessionId: history[index].sessionId,
                sender: history[index].sender,
                content: history[index].content,
                createdAt: history[index].createdAt,
                isAiResponse: history[index].isAiResponse,
                isStreaming: false,
                sources: history[index].sources,
                confidence: history[index].confidence,
                nextAction: history[index].nextAction
            )
            history[index] = flipped
            partial = flipped
        }
        stateLock.unlock()
        if let partial { _messages.emit(partial) }
        recordAndEmit(ConversationMessage(
            id: nextId(),
            sessionId: sessionId,
            sender: .system,
            content: "回答中断，请重试",
            createdAt: now()
        ))
    }

    // MARK: - 守卫/助手

    private func bumpUnreadIfHidden() {
        let hidden: Bool = {
            stateLock.lock()
            defer { stateLock.unlock() }
            return !sessionVisible
        }()
        if hidden { _unreadCount.set(_unreadCount.value + 1) }
    }

    private func cancelPendingReconnect() {
        stateLock.lock()
        let pending = reconnectTask
        reconnectTask = nil
        reconnectAttempt = 0
        stateLock.unlock()
        pending?.cancel()
    }

    private func currentTransportNow() -> WebSocketTransport? {
        stateLock.lock()
        defer { stateLock.unlock() }
        return currentTransport
    }

    private func setPendingEcho(_ gate: EchoGate?) {
        stateLock.lock()
        defer { stateLock.unlock() }
        pendingEcho = gate
    }

    private func clearPendingEcho(matching gate: EchoGate) {
        stateLock.lock()
        if pendingEcho === gate { pendingEcho = nil }
        stateLock.unlock()
    }

    fileprivate func isCurrent(_ transport: WebSocketTransport) -> Bool {
        stateLock.lock()
        defer { stateLock.unlock() }
        return currentTransport === transport
    }

    private func isDestroyed() -> Bool {
        stateLock.lock()
        defer { stateLock.unlock() }
        return destroyed
    }

    private func isEverConnected() -> Bool {
        stateLock.lock()
        defer { stateLock.unlock() }
        return everConnected
    }

    private func nextId() -> String {
        stateLock.lock()
        defer { stateLock.unlock() }
        return nextIdLocked()
    }

    /// 仅在持有 stateLock 时调用。
    private func nextIdLocked() -> String {
        localSeq += 1
        return "ws-\(localSeq)"
    }

    private func now() -> Int64 {
        Int64(Date().timeIntervalSince1970 * 1000)
    }
}

/// okhttp WebSocketListener 的镜像：过期套接字守卫（transport 身份比对）+
/// 回调转发到门面。Kotlin 侧以 inner class 捕获外部实例；Swift 嵌套类不捕获，
/// 显式持有 facade 引用与所服务 transport。
private final class WsListener: WebSocketTransportListener {
    private unowned let facade: ServifyChat
    private let transport: WebSocketTransport

    init(facade: ServifyChat, transport: WebSocketTransport) {
        self.facade = facade
        self.transport = transport
    }

    func onOpen() {
        guard facade.isCurrent(transport) else { return } // 过期套接字（已被新连接取代）
        facade.handleOpen()
    }

    func onMessage(_ text: String) {
        guard facade.isCurrent(transport) else { return }
        facade.handleFrame(text)
    }

    func onFailure(_ error: Error, httpStatus: Int?) {
        guard facade.isCurrent(transport) else { return }
        facade.handleFailure(error, httpStatus: httpStatus)
    }

    func onClosed(code: Int, reason: String) {
        guard facade.isCurrent(transport) else { return }
        facade.handleClosed()
    }
}
