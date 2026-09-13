import { StaticCapabilitySet } from '../contracts/capability';

/**
 * 移动端能力集：headless 绑定（React Native 等）不带屏幕共享与 WebRTC 语音面，
 * negotiate 对 remote_assist / voice 请求返回 disabled 拒绝。
 */
export function createMobileCapabilitySet(): StaticCapabilitySet {
  return new StaticCapabilitySet([
    { name: 'chat', enabled: true, version: '1' },
    { name: 'realtime', enabled: true, version: '1' },
    { name: 'knowledge', enabled: true, version: '1' },
    { name: 'remote_assist', enabled: false, version: 'reserved' },
    { name: 'voice', enabled: false, version: 'reserved' },
  ]);
}
