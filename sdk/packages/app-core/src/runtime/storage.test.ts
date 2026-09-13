import { describe, expect, it } from 'vitest';

import { MemoryMobileStorageAdapter, StorageSnapshotStore } from '../index';

describe('MemoryMobileStorageAdapter', () => {
  it('round-trips values and returns null for missing keys', async () => {
    const storage = new MemoryMobileStorageAdapter();

    expect(await storage.getItem('missing')).toBeNull();

    await storage.setItem('session', 'abc');
    expect(await storage.getItem('session')).toBe('abc');

    await storage.setItem('session', 'def');
    expect(await storage.getItem('session')).toBe('def');

    await storage.removeItem('session');
    expect(await storage.getItem('session')).toBeNull();
    await expect(storage.removeItem('session')).resolves.toBeUndefined();
  });
});

describe('StorageSnapshotStore', () => {
  const snapshot = {
    sessionId: 'sess-1',
    customerId: 'cust-9',
    lastMessageId: 'msg-3',
    savedAt: '2026-01-01T00:00:00.000Z',
  };

  it('persists and captures a snapshot through the storage adapter', async () => {
    const store = new StorageSnapshotStore(new MemoryMobileStorageAdapter());

    expect(await store.capture()).toBeNull();

    await store.persist(snapshot);
    expect(await store.capture()).toEqual(snapshot);
  });

  it('heals corrupted or malformed snapshots back to null', async () => {
    const storage = new MemoryMobileStorageAdapter();
    const store = new StorageSnapshotStore(storage);

    await storage.setItem(store.storageKey, '{not json');
    expect(await store.capture()).toBeNull();
    expect(await storage.getItem(store.storageKey)).toBeNull();

    await storage.setItem(store.storageKey, JSON.stringify({ nope: true }));
    expect(await store.capture()).toBeNull();
    expect(await storage.getItem(store.storageKey)).toBeNull();
  });

  it('honors the keyPrefix option when reading and writing', async () => {
    const storage = new MemoryMobileStorageAdapter();
    const store = new StorageSnapshotStore(storage, { keyPrefix: 'tenant-a:' });

    await store.persist(snapshot);

    expect(store.storageKey).toBe('tenant-a:servify:session-snapshot');
    expect(await storage.getItem('tenant-a:servify:session-snapshot')).toContain('sess-1');
  });

  it('defaults savedAt to now when persisting a snapshot without one', async () => {
    const store = new StorageSnapshotStore(new MemoryMobileStorageAdapter());

    await store.persist({ ...snapshot, savedAt: '' });

    const captured = await store.capture();
    expect(captured?.savedAt).not.toBe('');
  });

  it('clear removes only the persisted snapshot with a matching sessionId', async () => {
    const storage = new MemoryMobileStorageAdapter();
    const store = new StorageSnapshotStore(storage);

    await store.persist(snapshot);
    await store.clear('sess-other');

    const stored = await storage.getItem(store.storageKey);
    expect(stored).not.toBeNull();

    await store.clear('sess-1');
    expect(await storage.getItem(store.storageKey)).toBeNull();
  });

  it('restore re-persists the given snapshot', async () => {
    const store = new StorageSnapshotStore(new MemoryMobileStorageAdapter());

    await store.restore(snapshot);

    await expect(store.capture()).resolves.toEqual(snapshot);
  });
});
