import { defineConfig, devices } from '@playwright/test'

// 下注徽章遮挡修复 · 真实链路验收专用配置（独立于既有观战/真人/fixture 配置）。
// 只允许打在真实服务的本地 8081，绝不回退到硬编码地址或 fixture。
if (process.env.POKER_BASE_URL !== 'http://127.0.0.1:8081') {
  throw new Error('CHIP_BADGE_E2E_REQUIRES_LOCAL_8081')
}

export default defineConfig({
  testDir: './chip-badge',
  fullyParallel: false,
  workers: 1,
  forbidOnly: true,
  retries: 0,
  maxFailures: 1,
  timeout: 120_000,
  globalTimeout: 300_000,
  reporter: [['list']],
  outputDir: process.env.CHIP_OUTPUT_DIR,
  use: {
    baseURL: process.env.POKER_BASE_URL,
    // 真实测试禁用 trace/HAR/storageState/video，避免凭据落盘。
    trace: 'off',
    screenshot: 'off',
    video: 'off',
    actionTimeout: 15_000,
    navigationTimeout: 20_000,
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
