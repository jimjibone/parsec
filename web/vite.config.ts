import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    strictPort: true,
    // Keep the browser's Host header: the server refuses writes whose Origin
    // doesn't match it. The string shorthand would set changeOrigin: true.
    proxy: { '/api': { target: 'http://127.0.0.1:7343', changeOrigin: false } },
  },
});
