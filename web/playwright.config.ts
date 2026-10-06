import { defineConfig, devices } from '@playwright/test'

// Smoke tests against a production build served by `vite preview`, with the
// API mocked by MSW from the captured fixtures (VITE_MOCK=1), so they need no
// backend. Run: npm run e2e (first time: npx playwright install chromium).
const PORT = 4179

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    timezoneId: 'America/Mexico_City',
    locale: 'en-US',
  },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } },
    { name: 'mobile', use: { ...devices['Pixel 7'] } },
  ],
  webServer: {
    command: `VITE_MOCK=1 npx vite build --outDir dist-e2e --logLevel warn && npx vite preview --outDir dist-e2e --port ${PORT} --strictPort`,
    url: `http://127.0.0.1:${PORT}`,
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
})
