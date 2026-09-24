package servify.sdk.android

import java.util.ArrayDeque
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.RecordedRequest

/**
 * 补拉兜底 dispatcher（刀 10）：onOpen 自动补拉挂在所有 connect 类用例上——
 * 对账请求（GET /api/v1/sessions/...）不消费测试自己的响应队列，直接 404
 * （会话行未建过 = 补拉静默跳过的正常态），其余请求走 FIFO 响应队列。
 * 缺省时补拉要么阻塞在空队列（满等 readTimeout 10s）要么打真网
 * （messagesUrlOverride 缺省走 apiUrl 拼接面）。
 *
 * 队列等待带超时（3s 后 503）而非 QueueDispatcher 的无限 take()：断线重连的
 * 握手请求常落在用例未 enqueue 的空队列上（重连不是用例断言面），无限阻塞会让
 * tearDown 的 server.shutdown() 撞 5s 连接排空超时（"Gave up waiting for queue
 * to shut down"，release 变体实测复现）；超时 503 快速释放连接，客户端侧走既有
 * 握手失败路径，不影响任何用例断言。
 *
 * 断线补拉的显式对账用例（ReconcileMessagesTest）不用本类——它需要真实
 * 响应按请求顺序出队。
 */
internal class ReconcileBypassDispatcher(
    private val queueTimeoutMs: Long = 3_000,
) : Dispatcher() {
    private val lock = Object()
    private val responses = ArrayDeque<MockResponse>()

    /** 替代 server.enqueue（自定义 Dispatcher 后 MockWebServer.enqueue 会抛）。 */
    fun enqueue(response: MockResponse) {
        synchronized(lock) {
            responses.addLast(response)
            lock.notifyAll()
        }
    }

    override fun dispatch(request: RecordedRequest): MockResponse {
        if (request.method == "GET" && request.path.orEmpty().startsWith("/api/v1/sessions/")) {
            return MockResponse().setResponseCode(404)
        }
        val deadline = System.currentTimeMillis() + queueTimeoutMs
        while (true) {
            synchronized(lock) { responses.pollFirst() }?.let { return it }
            val wait = deadline - System.currentTimeMillis()
            if (wait <= 0) return MockResponse().setResponseCode(503)
            synchronized(lock) { lock.wait(wait) }
        }
    }
}
