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
      // FOUNDRY_API points the dev server at another daemon (the default is the local one).
      '/api': { target: process.env.FOUNDRY_API ?? 'http://127.0.0.1:7777' },
    },
  },
  test: {
    include: ['src/**/*.test.ts'],
  },
});
