import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: '../cmd/web-portal/static',
    emptyOutDir: true,
    sourcemap: true,
    // Stable asset names (no content hash) so generated index.html does not churn in git.
    // Entire cmd/web-portal/static/ is build output — see cmd/web-portal/STATIC_BUILD.md.
    rollupOptions: {
      output: {
        entryFileNames: 'assets/[name].js',
        chunkFileNames: 'assets/[name].js',
        assetFileNames: 'assets/[name][extname]',
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/stomp': { target: 'ws://localhost:8080', ws: true },
      '/events': 'http://localhost:8080',
      '/health': 'http://localhost:8080',
    },
  },
});