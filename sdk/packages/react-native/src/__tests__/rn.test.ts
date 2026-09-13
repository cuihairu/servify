import { afterEach, describe, expect, it, vi } from 'vitest';

import { MemoryMobileStorageAdapter } from '@servify/app-core';

import { createRNServifySDK } from '../createRNServifySDK';

class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  static OPEN = 1;

  readonly url: string;
  readonly protocols?: string | string[];
  readyState = FakeWebSocket.OPEN;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: ((event: { code: number; reason: string }) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(url: string, protocols?: string | string[]) {
    this.url = url;
    this.protocols = protocols;
    FakeWebSocket.instances.push(this);
  }

  send(data: string): void {
    this.sent.push(data);
  }

  close(): void {
    this.onclose?.({ code: 1000, reason: 'closed' });
  }

  open(): void {
    this.onopen?.();
  }
}

function makeFactory() {
  return vi.fn((url: string, protocols?: string | string[]) => {
    return new FakeWebSocket(url, protocols) as unknown as WebSocket;
  });
}

describe('createRNServifySDK', () => {
  afterEach(() => {
    FakeWebSocket.instances = [];
  });

  it('requires a storage adapter', () => {
    expect(() =>
      createRNServifySDK(
        { apiUrl: 'https://api.example.com' },
        { storage: undefined as unknown as MemoryMobileStorageAdapter },
      ),
    ).toThrow('options.storage');
  });

  it('carries the mobile capability set: chat on, remote_assist/voice negotiated as disabled', () => {
    const instance = createRNServifySDK(
      { apiUrl: 'https://api.example.com' },
      { storage: new MemoryMobileStorageAdapter() },
    );

    expect(instance.sdk.capabilities.has('chat')).toBe(true);
    expect(instance.sdk.capabilities.has('realtime')).toBe(true);
    expect(instance.sdk.capabilities.has('remote_assist')).toBe(false);
    expect(instance.sdk.capabilities.has('voice')).toBe(false);

    const verdict = instance.sdk.capabilities.negotiate([
      { name: 'chat' },
      { name: 'remote_assist' },
      { name: 'voice' },
    ]);
    expect(verdict.granted.map((entry) => entry.name)).toEqual(['chat']);
    expect(verdict.rejected.map((entry) => entry.request.name)).toEqual(['remote_assist', 'voice']);
    expect(verdict.rejected[0]).toMatchObject({ reason: 'disabled' });
  });

  it('wires the snapshot store onto the injected storage with the key prefix', async () => {
    const storage = new MemoryMobileStorageAdapter();
    const instance = createRNServifySDK(
      { apiUrl: 'https://api.example.com' },
      { storage, snapshotKeyPrefix: 'tenant-a:' },
    );

    await instance.snapshots.persist({
      sessionId: 'sess-1',
      customerId: 'cust-9',
      savedAt: '2026-01-01T00:00:00.000Z',
    });

    expect(await storage.getItem('tenant-a:servify:session-snapshot')).toContain('sess-1');
    await expect(instance.snapshots.capture()).resolves.toMatchObject({ sessionId: 'sess-1' });
  });

  it('sends chat messages through the injected WebSocket factory without touching global WebSocket', async () => {
    const factory = makeFactory();
    const storage = new MemoryMobileStorageAdapter();
    const instance = createRNServifySDK(
      {
        apiUrl: 'https://api.example.com',
        customerId: '7',
        autoConnect: false,
        webSocketFactory: factory,
      },
      { storage },
    );

    await instance.sdk.initialize();

    const pending = instance.sdk.startChat();
    await vi.waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    expect(factory).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/ws?session_id='),
      [],
    );
    FakeWebSocket.instances[0].open();

    await pending;
    await instance.sdk.sendMessage('你好,需要帮助');

    const frames = FakeWebSocket.instances[0].sent.map((frame) => JSON.parse(frame) as {
      type: string;
      data: { content: string };
    });
    expect(frames.at(-1)).toMatchObject({
      type: 'text-message',
      data: { content: '你好,需要帮助' },
    });
  });
});
