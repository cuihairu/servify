// 导出主要的类和接口
export { ServifySDK } from './sdk';
export { ApiClient } from './api';
export { WebSocketManager } from './websocket';
export { VoiceChannel } from './voice';
export type { VoiceChannelOptions } from './voice';
export {
  MicCapture,
  VOICE_SAMPLE_RATE,
  floatToPcm16,
  resampleLinear,
  pcm16ToLeBytes,
} from './voice-capture';
export type { MicCaptureOptions, MicCapturePorts } from './voice-capture';
export { HttpTransport } from './transports/http';

// 导出所有类型
export * from './types';
export * from './contracts';
export * from './bindings';

// 导出默认实例创建函数
import { ServifySDK } from './sdk';
import { ServifyConfig } from './types';

/**
 * 创建 Servify SDK 实例的便捷函数
 *
 * @param config SDK 配置
 * @returns SDK 实例
 *
 * @example
 * ```typescript
 * import { createServifySDK } from '@servify/core';
 *
 * const sdk = createServifySDK({
 *   apiUrl: 'https://api.servify.com',
 *   customerName: 'John Doe',
 *   customerEmail: 'john@example.com',
 *   debug: true
 * });
 *
 * await sdk.initialize();
 * ```
 */
export function createServifySDK(config: ServifyConfig): ServifySDK {
  return new ServifySDK(config);
}

// 默认导出 SDK 类
export default ServifySDK;
