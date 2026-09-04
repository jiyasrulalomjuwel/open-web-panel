import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist/admin',
    rollupOptions: {
      input: 'admin.html',
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:9000', changeOrigin: true },
      '/healthz': { target: 'http://localhost:9000', changeOrigin: true },
      '/pma': { target: 'http://localhost:9000', changeOrigin: true },
    },
  },
});
