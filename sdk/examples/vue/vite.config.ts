import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      // 示例消费 monorepo 内的发布物 dist（@servify/* 未发布到 npm registry，
      // 依赖声明走 npm 安装会 404）；构建前需先 `npm -C sdk run build`。
      '@servify/vue': fileURLToPath(
        new URL('../../packages/vue/dist/index.esm.js', import.meta.url),
      ),
    },
  },
  server: {
    port: 3001,
  },
});
