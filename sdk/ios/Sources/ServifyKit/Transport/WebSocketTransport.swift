import Foundation

/// WS 传输抽象（测试接缝）：Kotlin 侧直接硬绑 OkHttp，Swift 侧 Linux 无 URLSession
/// WebSocket 实现，传输必须是可注入接口——生产走 URLSessionWebSocketTransport（Darwin），
/// 测试注入 MockTransport。回调语义逐条对齐 okhttp WebSocketListener。
protocol WebSocketTransportListener: AnyObject {
    func onOpen()
    func onMessage(_ text: String)
    /// httpStatus：握手阶段失败时服务端返回码（对齐 okhttp onFailure 的 response?.code）。
    func onFailure(_ error: Error, httpStatus: Int?)
    func onClosed(code: Int, reason: String)
}

protocol WebSocketTransport: AnyObject {
    func connect(to url: String, listener: WebSocketTransportListener)
    /// 发送文本帧；连接正在关闭/已关闭时返回 false（对齐 okhttp WebSocket.send）。
    @discardableResult
    func send(_ text: String) -> Bool
    func cancel()
}

#if canImport(Darwin)
/// 生产传输（Darwin 平台）：URLSessionWebSocketTask 承载（D4：系统库零依赖）。
/// Linux corelibs 无此实现——SDK 的 Linux CI 测试面全部走注入的 mock 传输。
final class URLSessionWebSocketTransport: NSObject, WebSocketTransport, URLSessionWebSocketDelegate {
    private let session: URLSession
    private var task: URLSessionWebSocketTask?
    private weak var listener: WebSocketTransportListener?

    override init() {
        session = URLSession(configuration: .default)
        super.init()
    }

    func connect(to url: String, listener: WebSocketTransportListener) {
        guard let wsURL = URL(string: url) else {
            listener.onFailure(URLError(.badURL), httpStatus: nil)
            return
        }
        self.listener = listener
        let task = session.webSocketTask(with: wsURL)
        self.task = task
        task.resume()
        pump(task)
    }

    @discardableResult
    func send(_ text: String) -> Bool {
        guard let task else { return false }
        var accepted = false
        let semaphore = DispatchSemaphore(value: 0)
        task.send(.string(text)) { error in
            accepted = (error == nil)
            semaphore.signal()
        }
        _ = semaphore.wait(timeout: .now() + 5)
        return accepted
    }

    func cancel() {
        task?.cancel(with: .goingAway, reason: nil)
    }

    /// 接收循环：逐帧转发给 listener，错误时收口（正常关闭经 didCloseWith 代理回调）。
    private func pump(_ task: URLSessionWebSocketTask) {
        task.receive { [weak self] result in
            guard let self else { return }
            switch result {
            case let .success(.string(text)):
                self.listener?.onMessage(text)
                self.pump(task)
            case .success:
                self.pump(task) // V1 仅文本帧；忽略二进制
            case let .failure(error):
                self.listener?.onFailure(error, httpStatus: nil)
            }
        }
    }

    func urlSession(_: URLSession, webSocketTask _: URLSessionWebSocketTask, didOpenWithProtocol _: String?) {
        listener?.onOpen()
    }

    func urlSession(_: URLSession, webSocketTask _: URLSessionWebSocketTask, didCloseWith closeCode: URLSessionWebSocketTask.CloseCode, reason _: Data?) {
        listener?.onClosed(code: Int(closeCode.rawValue), reason: "")
    }

    func urlSession(_: URLSession, task _: URLSessionTask, didCompleteWithError error: Error?) {
        guard let error else { return } // 正常关闭走 didCloseWith
        listener?.onFailure(error, httpStatus: nil)
    }
}
#endif
