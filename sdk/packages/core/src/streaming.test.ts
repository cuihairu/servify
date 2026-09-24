import { describe, expect, it } from 'vitest';

import { StreamingAssembler } from './streaming';

/**
 * StreamingAssembler 单测：ai-response 三段流式契约（PROTOCOL.md §4.1）
 * 的拼接语义，与 fixtures 05/06 回放测试互补（此处覆盖全分支，回放覆盖
 * 帧序列端到端行为）。
 */
describe('StreamingAssembler', () => {
  it('appends deltas, allocating a stable stream id on the first chunk', () => {
    const a = new StreamingAssembler();
    expect(a.active).toBe(false);
    expect(a.currentId).toBeNull();

    expect(a.onDelta('根据', false)).toBe('appended');
    expect(a.onDelta('退货政策', false)).toBe('appended');

    expect(a.active).toBe(true);
    expect(a.currentId).toBe('ai-stream-1');
    expect(a.content).toBe('根据退货政策');
  });

  it('marks the terminal delta without appending content, then closes on the final frame', () => {
    const a = new StreamingAssembler();
    a.onDelta('根据退货政策，', false);
    expect(a.onDelta('', true)).toBe('terminal');
    expect(a.terminalSeen).toBe(true);
    expect(a.content).toBe('根据退货政策，');

    // 终帧收口：返回流 id 并回到空白态
    expect(a.closeOnFinal()).toEqual({ id: 'ai-stream-1' });
    expect(a.active).toBe(false);
    expect(a.content).toBe('');
    expect(a.terminalSeen).toBe(false);
  });

  it('tolerates a non-empty terminal delta by appending before marking (content is not lost)', () => {
    const a = new StreamingAssembler();
    a.onDelta('根据退货政策，', false);
    // 违约形态：终末增量携带内容——追加不丢，再标记
    expect(a.onDelta('7 天内可退。', true)).toBe('terminal');
    expect(a.content).toBe('根据退货政策，7 天内可退。');
    expect(a.terminalSeen).toBe(true);
  });

  it('rejects deltas arriving after the terminal delta', () => {
    const a = new StreamingAssembler();
    a.onDelta('部分', false);
    a.onDelta('', true);
    expect(a.onDelta('违约增量', false)).toBe('rejected');
    expect(a.onDelta('', true)).toBe('rejected');
    expect(a.content).toBe('部分');
  });

  it('treats deltas after a finalized stream as a new stream with a fresh id', () => {
    const a = new StreamingAssembler();
    a.onDelta('第一轮', false);
    a.closeOnFinal();

    expect(a.onDelta('第二轮', false)).toBe('appended');
    expect(a.currentId).toBe('ai-stream-2');
    expect(a.content).toBe('第二轮');
  });

  it('closeOnFinal returns null for a single-shot final (no stream), keeping the assembler idle', () => {
    const a = new StreamingAssembler();
    expect(a.closeOnFinal()).toBeNull();
    expect(a.active).toBe(false);
  });

  it('closeOnDisconnect yields the partial content and resets; empty streams are not interruptions', () => {
    const a = new StreamingAssembler();
    expect(a.closeOnDisconnect()).toBeNull();

    a.onDelta('部分回答', false);
    a.onDelta('', true); // 终末增量后断连：interrupted 语义成立
    expect(a.closeOnDisconnect()).toEqual({ id: 'ai-stream-1', content: '部分回答' });
    expect(a.active).toBe(false);
  });

  it('closeOnDisconnect ignores a stream that never carried content', () => {
    const a = new StreamingAssembler();
    // 异常形态：仅终末增量首帧，无任何实质增量——无气泡可保留，不算中断
    a.onDelta('', true);
    expect(a.closeOnDisconnect()).toBeNull();
    expect(a.active).toBe(false);
  });
});
