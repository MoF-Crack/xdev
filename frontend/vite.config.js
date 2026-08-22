import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

export default defineConfig({
  plugins: [vue()],
  test: {
    environment: 'jsdom',
    include: ['test/**/*.{test,spec}.js'],
    setupFiles: ['./test/setup.js'],
  },
  resolve: {
    alias: {
      '@': '/src',
    },
  },
  build: {
    outDir: 'dist',
    target: 'esnext',
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/naive-ui') || id.includes('node_modules/vueuc') || id.includes('node_modules/@css-render')) {
            return 'naive';
          }
          if (id.includes('node_modules/vue/') || id.includes('node_modules/@vue/')) {
            return 'vue';
          }
        },
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
  },
});
