/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  build: {
    // The Go binary embeds this directory (internal/api/webdist).
    outDir: '../internal/api/webdist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': { target: 'http://127.0.0.1:7777' },
    },
  },
  test: {
    include: ['src/**/*.test.ts'],
  },
});
