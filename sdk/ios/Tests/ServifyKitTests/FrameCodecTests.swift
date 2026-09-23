import Testing

@testable import ServifyKit

/// FrameCodec 解码边界（fixtures 之外的变体输入，容错语义见 PROTOCOL §3）。
/// Kotlin 镜像：FixtureReplayTest.kt 的"FrameCodec 解码边界"节。
struct FrameCodecTests {

    @Test func textMessageBareStringDataDecodesToVisitorEcho() {
        // data 双形态之裸字符串（PROTOCOL §3，服务端两种都接受）
        let frame = FrameCodec.decode(
            #"{"type":"text-message","data":"直接文本","session_id":"s1"}"#
        )
        guard case let .visitorEcho(sessionId, _, content) = frame else {
            Issue.record("expected visitorEcho, got \(frame)")
            return
        }
        #expect(sessionId == "s1")
        #expect(content == "直接文本")
    }
}
