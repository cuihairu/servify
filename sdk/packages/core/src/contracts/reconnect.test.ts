import { describe, expect, it } from 'vitest';

import {
  computeReconnectDelay,
  normalizeReconnectPolicy,
  shouldReconnect,
} from './reconnect';

describe('reconnect helpers', () => {
  it('normalizes reconnect policy with defaults and overrides', () => {
    expect(normalizeReconnectPolicy()).toEqual({
      maxAttempts: 5,
      baseDelayMs: 1000,
      backoffFactor: 2,
      maxDelayMs: 30000,
    });

    expect(normalizeReconnectPolicy(
      { maxAttempts: 8, baseDelayMs: 250, maxDelayMs: 5000 },
    )).toEqual({
      maxAttempts: 8,
      baseDelayMs: 250,
      backoffFactor: 2,
      maxDelayMs: 5000,
    });
  });

  it('computes exponential reconnect delay with cap', () => {
    const policy = normalizeReconnectPolicy({
      baseDelayMs: 100,
      backoffFactor: 3,
      maxDelayMs: 500,
    });

    expect(computeReconnectDelay(policy, 1)).toBe(100);
    expect(computeReconnectDelay(policy, 2)).toBe(300);
    expect(computeReconnectDelay(policy, 3)).toBe(500);
  });

  it('stops reconnecting after manual close or max attempts', () => {
    const policy = normalizeReconnectPolicy({ maxAttempts: 2 });

    expect(shouldReconnect({ attempt: 0, isManualClose: false }, policy)).toBe(true);
    expect(shouldReconnect({ attempt: 2, isManualClose: false }, policy)).toBe(false);
    expect(shouldReconnect({ attempt: 0, isManualClose: true }, policy)).toBe(false);
  });
});
