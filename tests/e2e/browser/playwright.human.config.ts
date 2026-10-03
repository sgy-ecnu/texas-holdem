import { defineConfig, devices } from '@playwright/test'

// 独立真实服务入口；不加载 fixture，不影响原有观战/fixture 测试。
if (process.env.POKER_BASE_URL !== 'http://127.0.0.1:8081') {
  throw new Error('HUMAN_E2E_REQUIRES_LOCAL_8081')
}

export default defineConfig({
  testDir: './human',
  fullyParallel: false,
  workers: 1,
  forbidOnly: true,
  retries: 0,
  maxFailures: 1,
  timeout: 45_000,
  globalTimeout: 180_000,
  reporter: [['list']],
  outputDir: process.env.HUMAN_OUTPUT_DIR,
  use: {
    baseURL: process.env.POKER_BASE_URL,
    // trace/HAR/storageState 都可能保存 HttpOnly 凭据，真实测试明确禁用。
    trace: 'off',
    screenshot: 'off',
    video: 'off',
    actionTimeout: 5_000,
    navigationTimeout: 10_000,
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
