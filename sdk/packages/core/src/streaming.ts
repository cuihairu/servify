/**
 * ai-response 三段流式契约的拼接器（PROTOCOL.md §4.1，Android 门面流式状态同构）：
 *
 * ① 若干 done=false 增量即到即拼；② 终末增量 done=true（契约 content_delta=""，
 * 宽容面：非空也先追加再标记，内容不丢）；③ 完整 ai-response 终帧，内容与拼接
 * 结果一致（整体替换是幂等收口）。
 *
 * 流中断语义：终末增量已到但无 ai-response 终帧 = 本次回答失败——保留已渲染
 * 部分 + 提示重试，不自动重发。收口时机只有两个：断连（流必然中断）与终帧
 * 到达（正常收口）；无超时器（对齐 Android 门面）。终帧收口后再到的增量按
 * 新一轮回答的流处理（帧层面无从区分违约与新一轮）。
 */
export class StreamingAssembler {
  private parts: string[] = [];
  private streamId: string | null = null;
  private terminalDeltaSeen = false;
  private nextStreamId = 1;

  /** 是否存在活跃流（有过实质增量、尚未收口）。 */
  get active(): boolean {
    return this.streamId !== null;
  }

  /** 当前累计内容（测试断言用）。 */
  get content(): string {
    return this.parts.join('');
  }

  /** 当前流 id（无流时 null）。 */
  get currentId(): string | null {
    return this.streamId;
  }

  get terminalSeen(): boolean {
    return this.terminalDeltaSeen;
  }

  /** 消费一条增量帧。返回 'appended'（内容增量，调用方应发 delta 事件）、
   *  'terminal'（终末标记，无气泡更新）、'rejected'（终末后到达的违约增量，静默）。 */
  onDelta(contentDelta: string, done: boolean): 'appended' | 'terminal' | 'rejected' {
    if (this.terminalDeltaSeen) return 'rejected';
    if (done) {
      // 契约终末增量 content_delta=""；非空时按普通增量先追加（内容不丢）再标记
      if (contentDelta !== '') {
        this.append(contentDelta);
      }
      this.terminalDeltaSeen = true;
      return 'terminal';
    }
    this.append(contentDelta);
    return 'appended';
  }

  /**
   * 终帧收口：若存在流，返回其 id（调用方在终帧 message 之后发 end 事件）；
   * 无流（单发终帧）返回 null。收口后 assembles 器回到空白态。
   */
  closeOnFinal(): { id: string } | null {
    if (this.streamId === null) return null;
    const id = this.streamId;
    this.reset();
    return { id };
  }

  /**
   * 断连收口：若存在未收口的流，返回中断负载（UI 保留部分内容并提示重试）；
   * 从无实质内容（仅终末增量的异常形态等）不算中断——无气泡可保留。
   */
  closeOnDisconnect(): { id: string; content: string } | null {
    if (this.streamId === null) return null;
    const content = this.parts.join('');
    const id = this.streamId;
    this.reset();
    if (content === '') return null;
    return { id, content };
  }

  private append(contentDelta: string): void {
    if (this.streamId === null) {
      this.streamId = `ai-stream-${this.nextStreamId}`;
      this.nextStreamId += 1;
    }
    this.parts.push(contentDelta);
  }

  private reset(): void {
    this.parts = [];
    this.streamId = null;
    this.terminalDeltaSeen = false;
  }
}
