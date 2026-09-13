import type { MobileStorageAdapter } from '../contracts/storage';
import type { SessionRestoreStrategy, SessionSnapshot } from '../contracts/session-restore';

export const DEFAULT_SNAPSHOT_KEY = 'servify:session-snapshot';

/**
 * MobileStorageAdapter 之上的 SessionSnapshot 读写件:
 * capture 读取已持久化的快照(损坏自愈为 null),persist/restore 写入,
 * clear 按 sessionId 匹配清理。持久化介质由注入的 storage 决定,
 * SDK 接线由上层绑定(RN 等)组合完成。
 */
export class StorageSnapshotStore implements SessionRestoreStrategy {
  private readonly storage: MobileStorageAdapter;
  readonly storageKey: string;

  constructor(storage: MobileStorageAdapter, options?: { keyPrefix?: string }) {
    this.storage = storage;
    this.storageKey = `${options?.keyPrefix ?? ''}${DEFAULT_SNAPSHOT_KEY}`;
  }

  async capture(): Promise<SessionSnapshot | null> {
    const raw = await this.storage.getItem(this.storageKey);
    if (raw === null) {
      return null;
    }

    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch {
      await this.storage.removeItem(this.storageKey);
      return null;
    }

    if (!this.isSessionSnapshot(parsed)) {
      await this.storage.removeItem(this.storageKey);
      return null;
    }
    return parsed;
  }

  async persist(snapshot: SessionSnapshot): Promise<void> {
    const normalized: SessionSnapshot = {
      ...snapshot,
      savedAt: snapshot.savedAt || new Date().toISOString(),
    };
    await this.storage.setItem(this.storageKey, JSON.stringify(normalized));
  }

  async restore(snapshot: SessionSnapshot): Promise<void> {
    await this.persist(snapshot);
  }

  async clear(sessionId: string): Promise<void> {
    const snapshot = await this.capture();
    if (snapshot === null || snapshot.sessionId === sessionId) {
      await this.storage.removeItem(this.storageKey);
    }
  }

  private isSessionSnapshot(value: unknown): value is SessionSnapshot {
    return (
      typeof value === 'object' &&
      value !== null &&
      typeof (value as SessionSnapshot).sessionId === 'string' &&
      typeof (value as SessionSnapshot).savedAt === 'string'
    );
  }
}
