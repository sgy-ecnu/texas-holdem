import { defineConfig, devices } from '@playwright/test'

// 独立于真实 Go E2E：用本地 fixture 服务器验证真人上桌的浏览器交互。
// 它不是真实链路验收，真实 Go 联调需另跑 playwright.config.ts + run_browser.py。
const PORT = 4181

export default defineConfig({
  testDir: './fixture/tests',
  fullyParallel: false,
  workers: 1,
  forbidOnly: true,
  retries: 0,
  timeout: 60_000,
  reporter: [['list']],
  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    actionTimeout: 15_000,
    navigationTimeout: 20_000,
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'node fixture/fixture-server.mjs',
    url: `http://127.0.0.1:${PORT}/api/v1/health`,
    reuseExistingServer: false,
    timeout: 30_000,
  },
})
