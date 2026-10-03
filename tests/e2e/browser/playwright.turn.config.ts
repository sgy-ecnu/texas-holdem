import { defineConfig } from '@playwright/test'
import human from './playwright.human.config'

// 原真人回归与新轮转场景共享真实服务；保持 fixture 和原配置独立。
export default defineConfig({
  ...human,
  testDir: '.',
  testMatch: ['human/human.spec.ts', 'turn/turn.spec.ts'],
  timeout: 60_000,
  globalTimeout: 300_000,
  use: { ...human.use, viewport: { width: 1440, height: 1100 } },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
})
