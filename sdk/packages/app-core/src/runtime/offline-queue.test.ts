import { describe, expect, it } from 'vitest';

import { MemoryOfflineQueueStore } from '../index';
import type { OfflineQueueEntry } from '../index';

function entry(id: string, type = 'chat.send'): OfflineQueueEntry<{ text: string }> {
  return {
    id,
    type,
    payload: { text: `payload-${id}` },
    createdAt: '2026-01-01T00:00:00.000Z',
    retryCount: 0,
  };
}

describe('MemoryOfflineQueueStore', () => {
  it('peeks entries in FIFO order up to the limit and isolates entry objects', async () => {
    const store = new MemoryOfflineQueueStore<{ text: string }>();
    await store.enqueue(entry('a'));
    await store.enqueue(entry('b'));
    await store.enqueue(entry('c'));

    const firstTwo = await store.peek(2);
    expect(firstTwo.map((e) => e.id)).toEqual(['a', 'b']);

    // 条目为浅拷贝:改返回条目的标量字段不影响队列;payload 为共享引用
    firstTwo[0].id = 'mutated';
    firstTwo[0].payload.text = 'mutated';
    const again = await store.peek();
    expect(again.map((e) => e.id)).toEqual(['a', 'b', 'c']);
    expect(again[0].payload.text).toBe('mutated');
  });

  it('acknowledge removes only the acknowledged entry', async () => {
    const store = new MemoryOfflineQueueStore<{ text: string }>();
    await store.enqueue(entry('a'));
    await store.enqueue(entry('b'));

    await store.acknowledge('a');
    expect((await store.peek()).map((e) => e.id)).toEqual(['b']);

    // 未知 id 静默忽略
    await store.acknowledge('missing');
    expect((await store.peek()).map((e) => e.id)).toEqual(['b']);
  });

  it('clear empties the queue', async () => {
    const store = new MemoryOfflineQueueStore<{ text: string }>();
    await store.enqueue(entry('a'));

    await store.clear();
    await expect(store.peek()).resolves.toEqual([]);
  });
});
