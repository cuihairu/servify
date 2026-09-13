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
      name: 'ServifyAppCore',
      fileName: (format) => `index.${format === 'es' ? 'esm.js' : 'js'}`,
      formats: ['es', 'umd']
    },
    rollupOptions: {
      external: [],
      output: {
        globals: {}
      }
    },
    sourcemap: true,
    minify: 'terser',
  }
});
