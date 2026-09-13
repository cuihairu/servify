import { defineConfig } from 'vite';
import dts from 'vite-plugin-dts';

export default defineConfig({
  plugins: [
    dts({
      insertTypesEntry: true,
      outDir: 'dist',
      include: ['src/**/*'],
    }),
  ],
  build: {
    lib: {
      entry: 'src/index.ts',
      name: 'ServifyReactNative',
      fileName: (format) => `index.${format === 'es' ? 'esm.js' : 'js'}`,
      formats: ['es', 'cjs']
    },
    rollupOptions: {
      // headless 绑定只依赖 react;core/app-core 随包打包(与 @servify/react 先例一致)
      external: ['react'],
      output: {
        globals: {
          react: 'React'
        }
      }
    },
    sourcemap: true,
    minify: 'terser',
  }
});
