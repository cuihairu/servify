import type { MobileStorageAdapter } from '../contracts/storage';

/**
 * 内存版 MobileStorageAdapter:单测、SSR 与无持久化宿主的默认实现。
 * 生命周期与持有者一致,进程结束即丢弃。
 */
export class MemoryMobileStorageAdapter implements MobileStorageAdapter {
  private readonly entries = new Map<string, string>();

  async getItem(key: string): Promise<string | null> {
    return this.entries.get(key) ?? null;
  }

  async setItem(key: string, value: string): Promise<void> {
    this.entries.set(key, value);
  }

  async removeItem(key: string): Promise<void> {
    this.entries.delete(key);
  }
}
