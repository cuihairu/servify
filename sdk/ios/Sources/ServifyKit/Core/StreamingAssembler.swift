import Foundation

/// ai-response 三段流式契约的拼接器（PROTOCOL.md §4.1）：
///
/// ① 若干 done=false 增量即到即拼；② 终末增量 content_delta="" + done=true；
/// ③ 完整 ai-response 终帧，内容与拼接结果一致（整体替换是幂等收口）。
///
/// 流中断语义：终末增量已到但无 ai-response 终帧 = 本次回答失败——保留已渲染
/// 部分 + 提示重试，不自动重发（`interrupted`）。
/// Kotlin 镜像：shared/core StreamingAssembler.kt；终末增量非空在 Kotlin 侧为 require 抛
/// IllegalArgumentException，Swift 侧同型为 throws。
public final class StreamingAssembler {

    private var parts: String = ""

    /// 已收到的增量分片（fixtures 回放按序断言 content_parts）。
    public private(set) var contentParts: [String] = []

    public private(set) var terminalDeltaSeen = false

    public private(set) var finalSeen = false

    /// 终帧与拼接结果的一致性；终帧未到时为 nil。
    public private(set) var finalMatchesConcatenation: Bool?

    public var rendered: String { parts }

    public init() {}

    /// 消费一条增量帧；终末增量后到达的普通增量按违反契约处理（返回 false）。
    @discardableResult
    public func onDelta(_ contentDelta: String, done: Bool) throws -> Bool {
        if terminalDeltaSeen || finalSeen { return false }
        if done {
            guard contentDelta.isEmpty else {
                throw StreamingContractError.terminalDeltaMustBeEmpty
            }
            terminalDeltaSeen = true
            return true
        }
        parts += contentDelta
        contentParts.append(contentDelta)
        return true
    }

    /// 消费 ai-response 终帧。增量流式与单发两种模式都合法：
    /// 有增量时校验拼接一致；无增量时直接以终帧为完整内容。
    @discardableResult
    public func onFinal(_ content: String) -> Bool {
        if finalSeen { return false }
        finalSeen = true
        finalMatchesConcatenation = terminalDeltaSeen ? (parts == content) : nil
        if !terminalDeltaSeen { parts += content }
        return true
    }

    /// 本次回答失败：增量流已收口但没有终帧。
    public var interrupted: Bool { terminalDeltaSeen && !finalSeen }
}

public enum StreamingContractError: Error {
    case terminalDeltaMustBeEmpty
}
