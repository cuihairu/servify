package servify.sdk.android.core

/**
 * ai-response 三段流式契约的拼接器（PROTOCOL.md §4.1）：
 *
 * ① 若干 done=false 增量即到即拼；② 终末增量 content_delta="" + done=true；
 * ③ 完整 ai-response 终帧，内容与拼接结果一致（整体替换是幂等收口）。
 *
 * 流中断语义：终末增量已到但无 ai-response 终帧 = 本次回答失败——保留已渲染
 * 部分 + 提示重试，不自动重发（[interrupted]）。
 */
class StreamingAssembler {

    private val parts = StringBuilder()

    /** 已收到的增量分片（fixtures 回放按序断言 content_parts）。 */
    val contentParts = mutableListOf<String>()

    var terminalDeltaSeen = false
        private set

    var finalSeen = false
        private set

    /** 终帧与拼接结果的一致性；终帧未到时为 null。 */
    var finalMatchesConcatenation: Boolean? = null
        private set

    val rendered: String get() = parts.toString()

    /** 消费一条增量帧；终末增量后到达的普通增量按违反契约处理（返回 false）。 */
    fun onDelta(contentDelta: String, done: Boolean): Boolean {
        if (terminalDeltaSeen || finalSeen) return false
        if (done) {
            require(contentDelta.isEmpty()) { "terminal delta must carry empty content_delta" }
            terminalDeltaSeen = true
            return true
        }
        parts.append(contentDelta)
        contentParts += contentDelta
        return true
    }

    /**
     * 消费 ai-response 终帧。增量流式与单发两种模式都合法：
     * 有增量时校验拼接一致；无增量时直接以终帧为完整内容。
     */
    fun onFinal(content: String): Boolean {
        if (finalSeen) return false
        finalSeen = true
        finalMatchesConcatenation = if (terminalDeltaSeen) parts.toString() == content else null
        if (!terminalDeltaSeen) parts.append(content)
        return true
    }

    /** 本次回答失败：增量流已收口但没有终帧。 */
    val interrupted: Boolean get() = terminalDeltaSeen && !finalSeen
}
