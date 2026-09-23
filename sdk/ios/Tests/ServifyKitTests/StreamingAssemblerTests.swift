import XCTest
@testable import ServifyKit

/// ai-response 三段流式契约拼接器穷举。Kotlin 镜像：StateMachineTest.kt
/// StreamingAssemblerTest，用例名逐一同名。
final class StreamingAssemblerTests: XCTestCase {

    func testCompleteThreePartStreamConcatenatesAndVerifiesFinal() throws {
        let asm = StreamingAssembler()
        XCTAssertTrue(try asm.onDelta("你好", done: false))
        XCTAssertTrue(try asm.onDelta("，客服为您服务", done: false))
        XCTAssertTrue(try asm.onDelta("", done: true))
        XCTAssertTrue(asm.onFinal("你好，客服为您服务"))
        XCTAssertEqual(["你好", "，客服为您服务"], asm.contentParts)
        XCTAssertEqual("你好，客服为您服务", asm.rendered)
        XCTAssertEqual(true, asm.finalMatchesConcatenation)
        XCTAssertFalse(asm.interrupted)
    }

    func testMismatchedFinalIsRecordedNotSwallowed() throws {
        let asm = StreamingAssembler()
        try asm.onDelta("增量内容", done: false)
        try asm.onDelta("", done: true)
        XCTAssertTrue(asm.onFinal("另一段内容"))
        XCTAssertEqual(false, asm.finalMatchesConcatenation)
    }

    func testSingleShotFinalWithoutDeltasIsLegal() {
        let asm = StreamingAssembler()
        XCTAssertTrue(asm.onFinal("单发回答"))
        XCTAssertEqual("单发回答", asm.rendered)
        XCTAssertNil(asm.finalMatchesConcatenation)
    }

    func testInterruptedStreamIsFlagged() throws {
        let asm = StreamingAssembler()
        try asm.onDelta("渲染了一半", done: false)
        try asm.onDelta("", done: true)
        XCTAssertTrue(asm.interrupted)
        XCTAssertEqual("渲染了一半", asm.rendered)
    }

    func testDeltasAfterTerminalViolateContract() throws {
        let asm = StreamingAssembler()
        XCTAssertTrue(try asm.onDelta("正常增量", done: false))
        XCTAssertTrue(try asm.onDelta("", done: true))
        // 终末增量之后的普通增量违反三段顺序（PROTOCOL.md §4.1）；终帧则仍是合法收尾
        XCTAssertFalse(try asm.onDelta("迟到增量", done: false))
        XCTAssertEqual("正常增量", asm.rendered)
        XCTAssertTrue(asm.onFinal("正常增量"))
        XCTAssertFalse(asm.interrupted)
    }

    func testTerminalDeltaMustCarryEmptyContent() {
        let asm = StreamingAssembler()
        XCTAssertThrowsError(try asm.onDelta("非空", done: true))
    }
}
