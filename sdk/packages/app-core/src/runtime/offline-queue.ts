import type { OfflineQueueEntry, OfflineQueueStore } from '../contracts/offline-queue';

const DEFAULT_PEEK_LIMIT = 25;

/**
 * 内存版 OfflineQueueStore:FIFO 顺序,peek 返回条目浅拷贝
 * (payload 为共享引用),acknowledge 出队,进程结束即丢弃。
 * 持久化宿主可按同一契约另实现。
 */
export class MemoryOfflineQueueStore<TPayload = unknown> implements OfflineQueueStore<TPayload> {
  private readonly entries: OfflineQueueEntry<TPayload>[] = [];

  async enqueue(entry: OfflineQueueEntry<TPayload>): Promise<void> {
    this.entries.push(entry);
  }

  async peek(limit?: number): Promise<OfflineQueueEntry<TPayload>[]> {
    const capped = Math.max(limit ?? DEFAULT_PEEK_LIMIT, 0);
    return this.entries.slice(0, capped).map((entry) => ({ ...entry }));
  }

  async acknowledge(entryId: string): Promise<void> {
    const index = this.entries.findIndex((entry) => entry.id === entryId);
    if (index >= 0) {
      this.entries.splice(index, 1);
    }
  }

  async clear(): Promise<void> {
    this.entries.length = 0;
  }
}
