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
    /// coverage-exempt（Darwin 分支）：Linux 编译面仅 fatalError 守卫路径，
    /// 生产路径由 ios-macos job 的 CreateFactoryTests 覆盖（M2 豁免口径）。
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
    /// 测试注入：工单创建端点同款（生产路径恒为 apiUrl + /api/v1/tickets）。
    private let ticketUrlOverride: String?
    /// 测试注入：推送注册端点（生产路径恒为 apiUrl + /api/v1/push/register）。
    private let pushUrlOverride: String?
    /// 测试注入：断线补拉端点（生产路径恒为 apiUrl + /api/v1/sessions/{id}/messages）。
    private let messagesUrlOverride: String?
    /// REST 出站 HTTP 通道（工单创建/推送注册共用；nil 时 Darwin 用 URLSession
    /// 生产实现，Linux 测试面须注入 mock）。
    private let ticketHTTP: TicketHTTPPosting?
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

    /// 服务端消息游标（§10 #1 增量拉取端点）：仅由补拉结果推进（WS 帧不带服务端
    /// 消息 ID）；nil = 尚未拉过（下次补拉从会话头全量）。
    private var lastServerMessageId: Int64?

    /// 补拉去重指纹（"sender组|content"）：游标确立后 WS 渲染过的消息。服务端对
    /// 客户/坐席消息先落库后广播，故 WS 渲染的消息必然已在补拉窗口内——重连补拉
    /// 若把它们拉回（id > 游标，因上次补拉后渲染），按指纹跳过渲染与未读、游标照
    /// 推进。AI 回答不落库、系统提示不走 WS，天然无重复面。不做收尾清空（对账
    /// 在途时到达的帧会被清空抹掉、制造重复），由容量环形淘汰回收。
    private var cursorFingerprints: [String] = []

    /// 补拉分页参数（Kotlin 镜像同值）：单页 100 条、至多 10 页、指纹容量 200。
    static let reconcilePageLimit = 100
    static let maxReconcilePages = 10
    static let fingerprintCapacity = 200

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
        ticketUrlOverride: String? = nil,
        pushUrlOverride: String? = nil,
        messagesUrlOverride: String? = nil,
        ticketHTTP: TicketHTTPPosting? = nil,
        transportFactory: @escaping () -> WebSocketTransport
    ) {
        self.config = config
        self.sessionId = sessionId
        self.policy = policy
        self.echoTimeoutMs = echoTimeoutMs
        self.wsUrlOverride = wsUrlOverride
        self.ticketUrlOverride = ticketUrlOverride
        self.pushUrlOverride = pushUrlOverride
        self.messagesUrlOverride = messagesUrlOverride
        self.ticketHTTP = ticketHTTP
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

    /**
     * 会话页工单创建（M3，§10 #4：POST {apiUrl}/api/v1/tickets，免认证访客端点；
     * Kotlin 镜像：ServifyChat.createTicket）。ai_summary 由会话历史自动组装
     * （TicketSummary，坐席侧直接可读），无对话则不携带该字段。请求-响应语义：
     * 成功返回回执、失败返回 nil（错误经错误流同步发出，与 sendMessage 同风格）
     * ——调用方（UI 刀）按 nil 在表单上反馈。
     */
    /// REST 出站通道解析（工单创建/推送注册共用）：Darwin 用 URLSession 生产
    /// 实现（可注入覆盖），Linux 测试面必注入 mock。
    private func resolveHTTP() -> TicketHTTPPosting {
        #if canImport(Darwin)
        return ticketHTTP ?? URLSessionTicketHTTP()
        #else
        guard let injected = ticketHTTP else {
            // coverage-exempt（Darwin 分支）：Linux 测试面必注入 mock，守卫不可达；
            // 生产路径由 macos 构建面覆盖（M2 豁免口径）。
            fatalError("TicketHTTP 仅在 Darwin 生产面可用；Linux 测试须注入 mock")
        }
        return injected
        #endif
    }

    /// REST JSON body 编码（工单创建/推送注册共用）：payload 为 [String: String]
    /// （property-list 类型），JSONSerialization 对它不可失败——catch 面无法经
    /// 任何输入触达，纯 API 契约兜底（coverage-exempt 防御行锚定于此一处）。
    private static func encodeRestBody(_ payload: [String: String]) -> Data {
        do {
            return try JSONSerialization.data(withJSONObject: payload)
        } catch {
            // coverage-exempt（防御行）：[String: String] 编码不可失败，见上注释。
            return Data()
        }
    }

    public func createTicket(title: String, description: String? = nil) async -> TicketReceipt? {
        let http = resolveHTTP()

        let trimmedBase = ticketUrlOverride ?? (trimTrailingSlash(config.apiUrl) + "/api/v1/tickets")
        var payload: [String: String] = [
            "session_id": sessionId,
            "title": title,
        ]
        if let description, !description.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            payload["description"] = description
        }
        if let summary = TicketSummary.build(messages: historySnapshot()) {
            payload["ai_summary"] = summary
        }
        let body = Self.encodeRestBody(payload)

        let result: Result<(status: Int, data: Data), Error>
        do {
            result = .success(try await http.post(url: trimmedBase, body: body))
        } catch {
            result = .failure(error)
        }

        switch result {
        case let .failure(error):
            _errors.emit(.network(message: "ticket request failed: \(error.localizedDescription)"))
            return nil
        case let .success((status, data)):
            if !(200..<300).contains(status) {
                _errors.emit(.ticketFailed(message: "ticket http \(status): \(String(data: data.prefix(200), encoding: .utf8) ?? "")"))
                return nil
            }
            guard let ticketId = Self.parseTicketId(from: data) else {
                _errors.emit(.ticketFailed(message: "ticket response missing id: \(String(data: data.prefix(200), encoding: .utf8) ?? "")"))
                return nil
            }
            return TicketReceipt(ticketId: ticketId)
        }
    }

    /// Kotlin String.trimEnd('/') 同口径（逐字符去尾）。
    private func trimTrailingSlash(_ value: String) -> String {
        var result = value
        while result.hasSuffix("/") { result.removeLast() }
        return result
    }

    /// 服务端 models.Ticket JSON 仅取 id（畸形/缺 id 由调用方兜底 ticketFailed）。
    private static func parseTicketId(from data: Data) -> Int64? {
        guard
            let root = try? JSONSerialization.jsonObject(with: data),
            let object = root as? [String: Any],
            let id = object["id"] as? Int64 ?? (object["id"] as? NSNumber)?.int64Value
        else { return nil }
        return id
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

    /**
     * 追加 SDK 自造的系统提示行（M3 工单创建成功提示等 UI 刀接线用；协议无此帧，
     * 不计未读；Kotlin 镜像：appendSystemHint）——进 history，面板 hide/重开后随
     * 快照回放。
     */
    func appendSystemHint(_ text: String) {
        recordAndEmit(ConversationMessage(
            id: nextId(),
            sessionId: sessionId,
            sender: .system,
            content: text,
            createdAt: now()
        ))
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

    /**
     * 推送注册口（M3，平台规格 §4 registerPushToken；D7 可选性；Kotlin 镜像）：
     * - pushTokenProvider 未配置 → unsupported 错误（能力未启用）；
     * - 配置但 provider 返回 nil（宿主未授权/无 token）→ 静默返回 false（非错误）；
     * - 取到 token → POST {apiUrl}/api/v1/push/register（§10 #5，免认证访客
     *   端点；session_id/platform=ios/token，同 session+platform 服务端幂等）。
     *   成功 true；IO/HTTP 失败经错误流发 network（七码冻结面无 push 专用码，
     *   network 可重试语义与"注册可重报"一致）并返回 false。
     */
    public func registerPushToken() async -> Bool {
        guard let provider = config.pushTokenProvider else {
            _errors.emit(.unsupported(message: "push not configured"))
            return false
        }
        guard let token = await provider(), !token.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            return false
        }

        let http = resolveHTTP()
        let url = pushUrlOverride ?? (trimTrailingSlash(config.apiUrl) + "/api/v1/push/register")
        let body = Self.encodeRestBody([
            "session_id": sessionId,
            "platform": "ios",
            "token": token,
        ])

        let result: Result<(status: Int, data: Data), Error>
        do {
            result = .success(try await http.post(url: url, body: body))
        } catch {
            result = .failure(error)
        }

        switch result {
        case let .failure(error):
            _errors.emit(.network(message: "push register failed: \(error.localizedDescription)"))
            return false
        case let .success((status, data)):
            if !(200..<300).contains(status) {
                _errors.emit(.network(message: "push register http \(status): \(String(data: data.prefix(200), encoding: .utf8) ?? "")"))
                return false
            }
            return true
        }
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
        Task { [weak self] in await self?.reconcileMessages() }
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
            // 顺序契约：先定终态再发 error——错误流消费与状态读取在并发面无
            // happens-before，先 error 后 state 会让「收到 error 即读状态」拿到
            // 旧值（与 Kotlin ServifyChat 镜像，35940978983 负载窗口复现）。
            _connectionState.set(.disconnected)
            _errors.emit(err)
            notifyOfflineHint()
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
            _connectionState.set(.disconnected)
            _errors.emit(.network(message: "reconnect exhausted after \(policy.maxAttempts) attempts"))
            notifyOfflineHint()
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
            trackFingerprint(sender: "customer", content: content)
            stateLock.lock()
            let gate = pendingEcho
            stateLock.unlock()
            gate?.completeIfExpected(content)

        case let .agentMessage(_, _, content, _):
            emitMessage(.agent, content)
            trackFingerprint(sender: "agent", content: content)
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
            trackFingerprint(sender: "ai", content: content)
            bumpUnreadIfHidden()

        case let .transferNotification(_, _, message, agentId):
            _agentAssigned.emit(AgentAssignment(agentId: agentId, message: message))

        case let .waitingNotification(_, _, message):
            _waitingInQueue.emit(message)

        case .webRtcSignal, .unknown:
            break // 移动端契约显式忽略
        }
    }

    // MARK: - 断线补拉（D7 流程 3；§10 #1 端点；Kotlin 镜像：reconcileMessages）

    /**
     * 增量补拉：连接成功（onOpen）后对账断连窗口内错过的消息。逐页 GET
     * /api/v1/sessions/{id}/messages?after_id=<游标>（升序，has_more 续拉），
     * 合并进 history 并发事件流；agent/ai 来源在面板不可见时计未读（与 WS 帧
     * 口径同构；customer 回显与 system 提示不计）。
     *
     * 全失败面静默：会话行未建过（404，首连/未发过消息）与 IO/HTTP 错误都不
     * 影响 WS 使用，游标不动、下次连接重新对账。游标与服务端 ID 仅在此链内
     * 自持（WS 帧无服务端 ID）；指纹去重见 cursorFingerprints。
     */
    /// 补拉的 HTTP 通道（同 resolveHTTP 的 Darwin/注入解析，但 Linux 测试面未
    /// 注入时返回 nil 静默跳过——补拉挂在 onOpen 自动触发，是增强面而非显式
    /// 调用面，不能要求未搭 REST 面的测试/宿主先配置通道）。
    /// coverage-exempt（Darwin 分支）：Linux 编译为空隙 region 计 0，生产路径由
    /// ios-macos job 覆盖（M2 豁免口径）。
    private func reconcileHTTP() -> TicketHTTPPosting? {
        #if canImport(Darwin)
        return ticketHTTP ?? URLSessionTicketHTTP()
        #else
        return ticketHTTP
        #endif
    }

    func reconcileMessages() async {
        guard let http = reconcileHTTP() else { return }
        var rounds = 0
        while rounds < Self.maxReconcilePages {
            rounds += 1
            let after = currentCursor()
            let base = messagesUrlOverride ?? (trimTrailingSlash(config.apiUrl)
                + "/api/v1/sessions/" + Self.formEncode(sessionId) + "/messages")
            var url = base + "?limit=\(Self.reconcilePageLimit)"
            if let after { url += "&after_id=\(after)" }

            let result: Result<(status: Int, data: Data), Error>
            do {
                result = .success(try await http.get(url: url))
            } catch {
                return
            }
            guard case let .success((status, data)) = result else { return }
            // 404 = 会话行未建过（行在首条消息持久化时建），无历史可拉——正常态。
            guard (200..<300).contains(status) else { return }
            guard
                let root = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                let entries = root["messages"] as? [Any]
            else { return }

            for entry in entries {
                // 元素级跳过（畸形条目不打死整页，Kotlin 镜像同语义）。
                guard let message = entry as? [String: Any] else { continue }
                guard
                    let idText = message["id"] as? String,
                    let id = Int64(idText.trimmingCharacters(in: .whitespaces))
                else { continue }
                let sender = message["sender"] as? String ?? ""
                let content = message["content"] as? String ?? ""
                // 游标只前进：指纹命中（本地已渲染）也推进——服务端落库事实已确认。
                if advanceCursorAndCheckFingerprint(id: id, key: Self.fingerprint(sender: sender, content: content)) {
                    continue
                }
                switch sender {
                case "agent":
                    emitMessage(.agent, content)
                    bumpUnreadIfHidden()
                case "ai":
                    recordAndEmit(ConversationMessage(
                        id: "srv-\(id)",
                        sessionId: sessionId,
                        sender: .system,
                        content: content,
                        createdAt: now(),
                        isAiResponse: true
                    ))
                    bumpUnreadIfHidden()
                case "system":
                    recordAndEmit(ConversationMessage(
                        id: "srv-\(id)",
                        sessionId: sessionId,
                        sender: .system,
                        content: content,
                        createdAt: now()
                    ))
                default:
                    // customer：首连全量回放会话历史时要进 history（重连增量的本地
                    // 已渲染份由指纹跳过）；customer 来源不计未读。
                    recordAndEmit(ConversationMessage(
                        id: "srv-\(id)",
                        sessionId: sessionId,
                        sender: .customer,
                        content: content,
                        createdAt: now()
                    ))
                }
            }
            if (root["has_more"] as? Bool) != true { break }
        }
        // 不在收尾清空指纹表：对账在途期间到达的 WS 帧（渲染+入表）会被清空抹掉，
        // 随后的重连补拉即重复渲染（Kotlin 侧实测可复现的竞态窗口）。容量环形
        // 淘汰足够——游标未确立窗口的渲染量远小于容量；补拉渲染与后续补拉之间
        // 由游标保护，指纹表只需覆盖「游标确立前的 WS 渲染」。
    }

    /// 补拉去重键（与 cursorFingerprints 同构；服务端 sender 直接入键）。
    private static func fingerprint(sender: String, content: String) -> String {
        "\(sender)|\(content)"
    }

    /// 当前游标快照（同步临界区；async 上下文禁裸 lock/unlock——Swift 6 不可用）。
    private func currentCursor() -> Int64? {
        stateLock.lock()
        defer { stateLock.unlock() }
        return lastServerMessageId
    }

    /// 游标推进 + 指纹查重（同一临界区）：返回 true = 本地已渲染，调用方跳过。
    private func advanceCursorAndCheckFingerprint(id: Int64, key: String) -> Bool {
        stateLock.lock()
        defer { stateLock.unlock() }
        // 游标只前进：指纹命中（本地已渲染）也推进——服务端落库事实已确认。
        if lastServerMessageId == nil || id > lastServerMessageId! {
            lastServerMessageId = id
        }
        return cursorFingerprints.contains(key)
    }

    /// WS 渲染时积累指纹（仅历史性消息；SDK 自造提示行不入——服务端无对应行）。
    private func trackFingerprint(sender: String, content: String) {
        let value = Self.fingerprint(sender: sender, content: content)
        stateLock.lock()
        cursorFingerprints.append(value)
        // coverage-exempt（防御行）：淘汰分支只有指纹溢出（>200 条 WS 渲染）触达——
        // 指纹表仅由 WS 渲染积累（补拉渲染由游标保护不入表：入表会让补拉大页把
        // WS 渲染指纹挤出窗口、反破坏去重），测试面渲染量远小于容量；生产溢出
        // 由环形淘汰自然回收。Android 镜像行同语义（jacoco 行口径天然覆盖）。
        while cursorFingerprints.count > Self.fingerprintCapacity {
            cursorFingerprints.removeFirst()
        }
        stateLock.unlock()
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
    // coverage-exempt（llvm 计数脱节面）：主路径在 CI 上物理执行（提示行断言过、
    // 子块 counter=1）但线性段 counter 报 0（调用计数全记 guard-else 特化副本，
    // 35906115877/35910306629 诊断实锤）——Swift/Linux 插桩缺陷，非缺口。
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
        // coverage-exempt（llvm 计数脱节面）：同 finalize 头部锚定——后半段行
        stateLock.unlock()
        if let partial { _messages.emit(partial) }
        appendSystemHint("回答中断，请重试")
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

    /// 离线提示（Branding.offlineText）：disconnected 终态（握手失败/重连耗尽）时追加的
    /// 系统提示行——同流中断提示模式：SDK 自造的 UI 状态行（协议无此帧），不计未读。
    /// destroy 不提示——用户主动关闭不等于客服离线。
    private func notifyOfflineHint() {
        guard let text = config.branding.offlineText else { return }
        appendSystemHint(text)
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
