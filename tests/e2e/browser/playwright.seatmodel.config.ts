import { defineConfig, devices } from '@playwright/test'
import path from 'node:path'

// 「按座位选模型」真实链路验收配置：真实 Go 二进制 + 受控本地 provider server。
// 服务地址唯一来源为 wrapper 注入的 POKER_BASE_URL；直接运行必须显式注入。
const outDir =
  process.env.SCREEN_DIR ||
  path.resolve(process.cwd(), '../../web/node_modules/.cache/browser-verify')

const baseURL = process.env.POKER_BASE_URL
if (!baseURL) throw new Error('POKER_BASE_URL_REQUIRED_FROM_CONFIG_RESOLVER')

export default defineConfig({
  testDir: './seat-model',
  fullyParallel: false,
  workers: 1,
  forbidOnly: true,
  retries: 0,
  timeout: 120_000,
  globalTimeout: 12 * 60_000,
  reporter: [['list']],
  outputDir: path.join(outDir, 'artifacts'),
  use: {
    baseURL,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    actionTimeout: 15_000,
    navigationTimeout: 20_000,
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
