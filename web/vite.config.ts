/// <reference types="vitest/config" />
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// The dev server proxies /api to ui-api (services/ui-api, 127.0.0.1:8090),
// so the browser only ever talks to one origin and no CORS is needed. The
// real-account canary's console (2026-10-22) is a second dev server:
// UI_API_URL=http://127.0.0.1:8091 UI_PORT=5174 npm run dev. The target is
// baked in as __UI_API__ so the REAL bar can name the ui-api it talks to.
const API = process.env.UI_API_URL ?? 'http://127.0.0.1:8090'
const PORT = Number(process.env.UI_PORT ?? 5173)

export default defineConfig({
  plugins: [react()],
  define: { __UI_API__: JSON.stringify(API) },
  server: {
    host: '127.0.0.1',
    port: PORT,
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
