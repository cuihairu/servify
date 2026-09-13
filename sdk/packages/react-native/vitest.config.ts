import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';

// 行为测试直接对 core/app-core 源码跑,不依赖先构建 dist
export default defineConfig({
  resolve: {
    alias: {
      '@servify/core': fileURLToPath(new URL('../core/src', import.meta.url)),
      '@servify/app-core': fileURLToPath(new URL('../app-core/src', import.meta.url)),
    },
  },
  test: {
    environment: 'node',
  },
});
