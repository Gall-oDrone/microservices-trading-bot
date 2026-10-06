/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// The dev server proxies /api to ui-api (services/ui-api, 127.0.0.1:8090),
// so the browser only ever talks to one origin and no CORS is needed.
const API = process.env.UI_API_URL ?? 'http://127.0.0.1:8090'

export default defineConfig({
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    proxy: { '/api': { target: API, changeOrigin: false } },
  },
  preview: { host: '127.0.0.1', port: 4173 },
  build: {
    sourcemap: true,
    rollupOptions: {
      output: {
        manualChunks: {
          charts: ['lightweight-charts'],
          vendor: ['react', 'react-dom', 'react-router', '@tanstack/react-query', 'zod'],
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'], // e2e/ is Playwright's
    css: false,
    restoreMocks: true,
  },
})
