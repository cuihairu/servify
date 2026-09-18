import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      // 示例消费 monorepo 内的发布物 dist（@servify/* 未发布到 npm registry，
      // 依赖声明走 npm 安装会 404）；构建前需先 `npm -C sdk run build`。
      '@servify/react': fileURLToPath(
        new URL('../../packages/react/dist/index.esm.js', import.meta.url),
      ),
    },
  },
  server: {
    port: 3000,
  },
});
