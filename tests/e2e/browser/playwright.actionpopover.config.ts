import { defineConfig, devices } from '@playwright/test'

// ActionPopover 真实浏览器验收专用配置；与既有观战/真人/turn 配置互不影响。
// 只允许打在真实服务的本地 8081，绝不回退到硬编码地址或 fixture。
if (process.env.POKER_BASE_URL !== 'http://127.0.0.1:8081') {
  throw new Error('ACTION_POPOVER_E2E_REQUIRES_LOCAL_8081')
}

export default defineConfig({
  testDir: './action-popover',
  fullyParallel: false,
  workers: 1,
  forbidOnly: true,
  retries: 0,
  maxFailures: 1,
  timeout: 120_000,
  globalTimeout: 300_000,
  reporter: [['list']],
  outputDir: process.env.AP_OUTPUT_DIR,
  use: {
    baseURL: process.env.POKER_BASE_URL,
    // 真实测试禁用 trace/HAR/storageState/video，避免 HttpOnly 凭据落盘。
    trace: 'off',
    screenshot: 'off',
    video: 'off',
    actionTimeout: 10_000,
    navigationTimeout: 20_000,
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
