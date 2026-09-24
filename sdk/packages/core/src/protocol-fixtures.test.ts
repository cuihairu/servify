import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { WebSocketManager } from './websocket';
import type { Message, TransferAssignmentUpdate, TransferWaitingUpdate } from './types';

/**
 * 契约回放测试：用 sdk/protocol-fixtures/ 同一套样例喂 core 的 WS 分发，
 * 断言 core 对每类帧的实际行为与 PROTOCOL.md 契约一致。
 *
 * Android/iOS 探针消费同一套样例（M0 验收①"双端同一套样例断言一致"）；
 * 端级偏差在样例 expectations 内显式声明（如 webrtc 帧两端分叉），
 * 转人工两帧（transfer_notification / waiting_notification）经 transfer:* 独立
 * 事件面三端闭环（移动端 agentAssigned/waitingInQueue 同构），见 PROTOCOL.md §4.2。
 */

interface FixtureExpectations {
  kind: string;
  assert: Record<string, unknown>;
  state?: Array<{ from: string; to: string }>;
}

interface Fixture {
  name: string;
  direction: string;
  frame?: Record<string, unknown>;
  frames?: Array<Record<string, unknown>>;
  expectations: FixtureExpectations;
}

const FIXTURES_DIR = join(__dirname, '..', '..', '..', 'protocol-fixtures');

const KNOWN_KINDS = [
  'visitor-echo',
  'agent-message',
  'ai-final',
  'ai-stream-complete',
  'ai-stream-interrupted',
  'transfer',
  'waiting',
  'unknown-ignored',
  'webrtc-ignored-by-mobile',
];

function loadFixtures(): Fixture[] {
  return readdirSync(FIXTURES_DIR)
    .filter((f) => f.endsWith('.json'))
    .sort()
    .map((f) => JSON.parse(readFileSync(join(FIXTURES_DIR, f), 'utf-8')) as Fixture);
}

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  static OPEN = 1;

  readonly url: string;
  readyState = FakeWebSocket.OPEN;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: ((event: { code: number; reason: string }) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }

  send(): void {}
  close(): void {
    this.onclose?.({ code: 1000, reason: 'closed' });
  }
  open(): void {
    this.onopen?.();
  }
  feed(frame: Record<string, unknown>): void {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }
}

async function connectManager(): Promise<{ manager: WebSocketManager; socket: FakeWebSocket }> {
  vi.stubGlobal('WebSocket', FakeWebSocket);
  const manager = new WebSocketManager({
    url: 'ws://localhost:8080/api/v1/ws?session_id=fixture-session',
  });
  const connectPromise = manager.connect();
  await vi.waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
  FakeWebSocket.instances[0].open();
  await connectPromise;
  return { manager, socket: FakeWebSocket.instances[0] };
}

describe('protocol fixtures replay (core)', () => {
  let fixtures: Fixture[];

  beforeEach(() => {
    fixtures = loadFixtures();
    FakeWebSocket.instances = [];
  });

  afterEach(() => {
    FakeWebSocket.instances = [];
    vi.unstubAllGlobals();
  });

  it('covers the full known kind vocabulary so new fixture kinds cannot skip dual-end assertion', () => {
    const kinds = new Set(fixtures.map((f) => f.expectations.kind));
    expect([...kinds].sort()).toEqual([...KNOWN_KINDS].sort());
  });

  it('replays visitor-echo into a customer message with the exact content', async () => {
    const fixture = fixtures.find((f) => f.expectations.kind === 'visitor-echo')!;
    const { manager, socket } = await connectManager();
    const messages: Message[] = [];
    manager.on('message', (m) => messages.push(m));

    socket.feed(fixture.frame!);

    expect(messages).toHaveLength(1);
    expect(messages[0].sender_type).toBe('customer');
    expect(messages[0].content).toBe(fixture.expectations.assert.content);
  });

  it('replays agent-message into an agent message with sender content', async () => {
    const fixture = fixtures.find((f) => f.expectations.kind === 'agent-message')!;
    const { manager, socket } = await connectManager();
    const messages: Message[] = [];
    manager.on('message', (m) => messages.push(m));

    socket.feed(fixture.frame!);

    expect(messages).toHaveLength(1);
    expect(messages[0].sender_type).toBe('agent');
    expect(messages[0].content).toBe(fixture.expectations.assert.content);
    expect(messages[0].metadata as Record<string, unknown>).toMatchObject({
      sender: fixture.expectations.assert.sender,
    });
  });

  it('replays ai-response full and minimal into is_ai_response messages with contract fields', async () => {
    const full = fixtures.find((f) => f.name === 'ai-response-full')!;
    const minimal = fixtures.find((f) => f.name === 'ai-response-minimal')!;
    const { manager, socket } = await connectManager();
    const messages: Message[] = [];
    manager.on('message', (m) => messages.push(m));

    socket.feed(full.frame!);
    socket.feed(minimal.frame!);

    expect(messages).toHaveLength(2);
    expect(messages.every((m) => m.is_ai_response === true)).toBe(true);

    // 全字段样例：sources/strategy/next_action/handoff_reason 均经 metadata 透出
    expect(messages[0].content).toBe(full.expectations.assert.content);
    expect(messages[0].metadata as Record<string, unknown>).toMatchObject({
      confidence: full.expectations.assert.confidence,
      source: full.expectations.assert.source,
      strategy: full.expectations.assert.strategy,
      next_action: full.expectations.assert.next_action,
      handoff_reason: full.expectations.assert.handoff_reason,
    });
    const sources = (messages[0].metadata as Record<string, unknown>).sources as Array<
      Record<string, unknown>
    >;
    expect(sources).toHaveLength(full.expectations.assert.sources_count as number);
    expect(sources[0]).toMatchObject(full.expectations.assert.sources_first as Record<string, unknown>);

    // 最小字段样例：编排附加输出零值省略（fields_absent）
    expect(messages[1].content).toBe(minimal.expectations.assert.content);
    const minMeta = messages[1].metadata as Record<string, unknown>;
    for (const field of minimal.expectations.assert.fields_absent as string[]) {
      expect(minMeta[field]).toBeUndefined();
    }
  });

  it('replays the complete delta stream: deltas assemble into ai-stream:delta updates, the final frame supersedes', async () => {
    // 三段契约（PROTOCOL.md §4.1）：① 增量即到即拼（ai-stream:delta content 为
    // 累计全量）；② 终末增量只做标记不发事件；③ ai-response 终帧照常走 message
    // （幂等收口），随发 ai-stream:end(interrupted=false) 让 UI 移除流式气泡。
    const fixture = fixtures.find((f) => f.expectations.kind === 'ai-stream-complete')!;
    const { manager, socket } = await connectManager();
    const messages: Message[] = [];
    const deltas: Array<{ id: string; content: string }> = [];
    const ends: Array<{ id: string; interrupted: boolean }> = [];
    manager.on('message', (m) => messages.push(m));
    manager.on('ai-stream:delta', (u) => deltas.push(u));
    manager.on('ai-stream:end', (u) => ends.push(u));

    for (const frame of fixture.frames!) {
      socket.feed(frame);
    }

    // 增量段：content_parts 逐条累计（同 id 稳定，UI upsert）
    const parts = fixture.expectations.assert.content_parts as string[];
    expect(deltas).toHaveLength(parts.length);
    expect(new Set(deltas.map((d) => d.id)).size).toBe(1);
    parts.forEach((_, i) => {
      expect(deltas[i].content).toBe(parts.slice(0, i + 1).join(''));
    });
    // 终末增量（content_delta="" + done=true）不产生事件
    // 终帧：message 幂等收口 + end(interrupted=false)
    expect(messages).toHaveLength(1);
    expect(messages[0].content).toBe(fixture.expectations.assert.final_content);
    expect(messages[0].is_ai_response).toBe(true);
    expect(ends).toEqual([{ id: deltas[0].id, interrupted: false }]);
  });

  it('replays the interrupted stream: partial content is kept on disconnect close, no fabricated final', async () => {
    // 流中断语义（PROTOCOL.md §4.1）：终末增量已到但无 ai-response 终帧 = 本次
    // 回答失败。core 无超时器（对齐 Android 门面），中断在断连时收口——
    // ai-stream:end(interrupted=true) 携带部分内容，UI 保留气泡并提示重试。
    const fixture = fixtures.find((f) => f.expectations.kind === 'ai-stream-interrupted')!;
    const { manager, socket } = await connectManager();
    const messages: Message[] = [];
    const deltas: Array<{ id: string; content: string }> = [];
    const ends: Array<{ id: string; interrupted: boolean; content?: string }> = [];
    manager.on('message', (m) => messages.push(m));
    manager.on('ai-stream:delta', (u) => deltas.push(u));
    manager.on('ai-stream:end', (u) => ends.push(u));

    for (const frame of fixture.frames!) {
      socket.feed(frame);
    }

    // 流式期：增量即到即拼，终末增量无事件，不 fabricate 终帧
    const parts = fixture.expectations.assert.content_parts as string[];
    expect(deltas).toHaveLength(parts.length);
    expect(deltas[deltas.length - 1].content).toBe(parts.join(''));
    expect(messages).toHaveLength(0);
    expect(ends).toHaveLength(0);

    // 断连收口：end(interrupted=true) 保留部分内容
    socket.close();
    expect(ends).toEqual([{ id: deltas[0].id, interrupted: true, content: parts.join('') }]);
  });

  it('replays transfer and waiting notifications: real handoff frames drive transfer events (three-end closure)', async () => {
    // transfer_notification / waiting_notification 是转人工状态机的真实驱动帧
    // （PROTOCOL.md §4.2）：core 经独立事件面 transfer:assigned / transfer:waiting
    // 透出（纯事件流，不渲染消息、不动会话状态——对齐移动端
    // agentAssigned/waitingInQueue），负载取自样例 assert 词汇。
    for (const kind of ['transfer', 'waiting'] as const) {
      FakeWebSocket.instances = [];
      vi.unstubAllGlobals();
      const fixture = fixtures.find((f) => f.expectations.kind === kind)!;
      const { manager, socket } = await connectManager();
      const messages: Message[] = [];
      const errors: unknown[] = [];
      const assigned: TransferAssignmentUpdate[] = [];
      const waiting: TransferWaitingUpdate[] = [];
      manager.on('message', (m) => messages.push(m));
      manager.on('error', (e) => errors.push(e));
      manager.on('transfer:assigned', (u) => assigned.push(u));
      manager.on('transfer:waiting', (u) => waiting.push(u));

      socket.feed(fixture.frame!);

      expect(messages).toHaveLength(0);
      expect(errors).toHaveLength(0);
      const asserted = fixture.expectations.assert as { message: string; agent_id?: number };
      if (kind === 'transfer') {
        expect(assigned).toEqual([{ agentId: asserted.agent_id, message: asserted.message }]);
        expect(waiting).toHaveLength(0);
      } else {
        expect(waiting).toEqual([{ message: asserted.message }]);
        expect(assigned).toHaveLength(0);
      }
    }
  });

  it('replays the dead-branch frame: ignored silently, never an error', async () => {
    const fixture = fixtures.find((f) => f.expectations.kind === 'unknown-ignored')!;
    const { manager, socket } = await connectManager();
    const messages: Message[] = [];
    const errors: unknown[] = [];
    manager.on('message', (m) => messages.push(m));
    manager.on('error', (e) => errors.push(e));

    expect(() => socket.feed(fixture.frame!)).not.toThrow();
    expect(messages).toHaveLength(0);
    expect(errors).toHaveLength(0);
  });

  it('replays the webrtc frame: the explicit dual-end fork — core consumes, mobile contract does not', async () => {
    const fixture = fixtures.find((f) => f.expectations.kind === 'webrtc-ignored-by-mobile')!;
    const { manager, socket } = await connectManager();
    const offers: unknown[] = [];
    manager.on('webrtc:offer', (o) => offers.push(o));

    socket.feed(fixture.frame!);

    // core 端消费（emit webrtc:offer）；移动端（Android 探针）断言同一帧被忽略——
    // 两端结论不同正是该样例的存在意义（PROTOCOL.md §8 端级偏差显式声明）。
    expect(offers).toHaveLength(1);
  });
});
