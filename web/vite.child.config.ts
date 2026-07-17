import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist/child',
    rollupOptions: {
      input: 'child.html',
    },
  },
  server: {
    port: 5174,
    proxy: {
      '/api': { target: 'http://localhost:9001', changeOrigin: true },
      '/healthz': { target: 'http://localhost:9001', changeOrigin: true },
    },
  },
});
