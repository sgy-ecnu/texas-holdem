import { defineConfig, devices } from '@playwright/test'
import path from 'node:path'

// 截图/产物输出目录：默认落在被忽略的 web/node_modules/.cache 下，由 wrapper 通过 SCREEN_DIR 覆盖。
const outDir =
  process.env.SCREEN_DIR ||
  path.resolve(process.cwd(), '../../web/node_modules/.cache/browser-verify')

// 服务地址唯一来源为 wrapper 的配置 resolver（冻结 Go CLI）；这里不复制任何端口默认值。
// 直接运行 playwright 且未注入 POKER_BASE_URL 时明确失败，绝不回退到硬编码地址。
const baseURL = process.env.POKER_BASE_URL
if (!baseURL) throw new Error('POKER_BASE_URL_REQUIRED_FROM_CONFIG_RESOLVER')

export default defineConfig({
  testDir: './tests',
  // 后端同一时间只允许一个非终态牌局，必须串行执行，避免相互抢 409。
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
