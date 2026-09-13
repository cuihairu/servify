import { describe, expect, it } from 'vitest';

import { ServifySDK } from '../sdk';
import { createMobileCapabilitySet } from './mobile';

describe('mobile bindings', () => {
  it('enables chat/realtime/knowledge on mobile', () => {
    const capabilities = createMobileCapabilitySet();

    expect(capabilities.has('chat')).toBe(true);
    expect(capabilities.has('realtime')).toBe(true);
    expect(capabilities.has('knowledge')).toBe(true);
  });

  it('rejects remote_assist and voice requests as disabled', () => {
    const capabilities = createMobileCapabilitySet();

    const result = capabilities.negotiate([
      { name: 'chat' },
      { name: 'remote_assist' },
      { name: 'voice' },
    ]);

    expect(result.granted.map((entry) => entry.name)).toEqual(['chat']);
    expect(result.rejected).toHaveLength(2);
    expect(result.rejected[0]).toMatchObject({ request: { name: 'remote_assist' }, reason: 'disabled' });
    expect(result.rejected[1]).toMatchObject({ request: { name: 'voice' }, reason: 'disabled' });
  });

  it('plugs into ServifySDK through config.capabilities while the default stays web', () => {
    const mobile = new ServifySDK({
      apiUrl: 'https://api.example.com',
      capabilities: createMobileCapabilitySet(),
    });
    expect(mobile.capabilities.has('chat')).toBe(true);
    expect(mobile.capabilities.has('remote_assist')).toBe(false);

    const web = new ServifySDK({ apiUrl: 'https://api.example.com' });
    expect(web.capabilities.has('remote_assist')).toBe(true);
  });
});
