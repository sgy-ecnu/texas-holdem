import { expect, test } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// 座位卡模型名修复验收（fixture，非真实 Go 链路）：
//   1) 模型短名移到第一行、不再挤占第二行「剩余」；
//   2) 已知模型显示短名（deepseek/deepseek-v4-pro → V4 Pro），完整 label 保留 title；
//   3) 未知模型省略显示（含括号一起隐藏），不留孤立括号；
//   4) 座位卡仍为 140×58 两行、不增高；
//   5) 移动端 ≤720px 模型短名 11px / max-width 120px。
// fixture 只返回 mock 模型，这里用 route 注入 deepseek 与超长未知模型（不改 fixture server）。
// 截图证据落盘到 SCREEN_DIR（默认 docs/model-label）。

const HERE = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(HERE, '../../../../..')
const OUT = process.env.SCREEN_DIR ? path.resolve(process.env.SCREEN_DIR) : path.join(ROOT, 'docs/model-label')

const KNOWN_REF = 'deepseek/deepseek-v4-pro'
const KNOWN_LABEL = 'deepseek-v4-pro'
const UNKNOWN_REF = 'some-provider/an-extremely-long-model-name-that-would-definitely-overflow'

const MODELS = {
  models: [
    { ref: 'mock', label: 'Mock', provider: 'mock', availability: 'mock', error_code: null },
    { ref: KNOWN_REF, label: KNOWN_LABEL, provider: 'openai_compatible', availability: 'configured', error_code: null },
    { ref: UNKNOWN_REF, label: UNKNOWN_REF, provider: 'openai_compatible', availability: 'configured', error_code: null },
  ],
}

function ai(seat: number, name: string, modelRef: string) {
  return { seat, name, stack: 10000, kind: 'ai' as const, model_ref: modelRef }
}

async function createGame(page: import('@playwright/test').Page, seats: Array<Record<string, unknown>>) {
  const res = await page.request.post('/api/v1/games', {
    data: { seats, button: 0, small_blind: 50, big_blind: 100, max_hands: 100, mode: 'mock', max_live_calls: 0 },
  })
  expect(res.ok()).toBeTruthy()
  return res.json()
}

async function injectModels(page: import('@playwright/test').Page) {
  await page.route('**/api/v1/models', (route) => route.fulfill({ json: MODELS }))
}

test.beforeEach(async ({ request }) => {
  await request.post('/__fixture/reset')
  await mkdir(OUT, { recursive: true })
})

test('桌面：模型短名上移第一行、第二行剩余不换行、未知模型不留孤立括号、卡不增高', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  await injectModels(page)
  const seats = [
    ai(0, '一个非常非常长的玩家名字占位', KNOWN_REF),
    ai(1, 'AI-long-unknown', UNKNOWN_REF),
    ai(2, 'AI-mock', 'mock'),
  ]
  await createGame(page, seats)
  await page.goto('/')
  await expect(page.getByText('公共观战')).toBeVisible()
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.seat')).toHaveCount(3)

  // 1) 模型短名只在第一行；第二行不含模型节点。
  await expect(page.locator('.seat-row1 .seat-model')).toHaveCount(1)
  await expect(page.locator('.seat-row2 .seat-model')).toHaveCount(0)
  await expect(page.locator('.seat-row1 .seat-model')).toHaveText('(V4 Pro)')
  // 完整 label 保留在座位卡 title。
  await expect(page.locator('.seat').first()).toHaveAttribute('title', /deepseek-v4-pro/)

  // 名字优先级高于模型名：超长玩家名下模型短名收缩（0 或极窄），名字仍拿到主要宽度。
  const longNameWidth = (await page.locator('.seat').first().locator('.seat-name').boundingBox())!.width
  const squeezedModelWidth = (await page.locator('.seat-row1 .seat-model').boundingBox())!.width
  expect(longNameWidth).toBeGreaterThan(50)
  expect(squeezedModelWidth).toBeLessThan(20)

  // 2) 未知模型：整组隐藏（无 .seat-model，row1 文本无孤立括号）。
  const unknown = page.locator('.seat').nth(1)
  await expect(unknown.locator('.seat-model')).toHaveCount(0)
  const unknownRow1 = (await unknown.locator('.seat-row1').innerText()).replace(/\s+/g, '')
  expect(unknownRow1).not.toContain('(')
  // 完整长 label 仍在 title。
  await expect(unknown).toHaveAttribute('title', new RegExp(UNKNOWN_REF))

  // 3) 第二行禁止换行：字高与行高一致（未折成两行），且卡高仍在 140×58 区间。
  const unitBox = await page.locator('.seat .seat-stack .unit').first().boundingBox()
  const row2Box = await page.locator('.seat-row2').first().boundingBox()
  expect(unitBox && row2Box).toBeTruthy()
  // 单行约 24px；若「剩余」被挤换行会明显 >30px。
  expect(row2Box!.height).toBeLessThanOrEqual(26)
  const box = await page.locator('.seat').first().boundingBox()
  expect(box!.width).toBeGreaterThanOrEqual(138)
  expect(box!.width).toBeLessThanOrEqual(142)
  expect(box!.height).toBeGreaterThanOrEqual(56)
  expect(box!.height).toBeLessThanOrEqual(60)

  await page.screenshot({ path: path.join(OUT, 'desktop-long-model.png'), fullPage: false })
})

test('移动端 ≤720px：模型短名 11px / max-width 120px、不溢出', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await injectModels(page)
  const seats = [
    ai(0, 'AI-1', KNOWN_REF),
    ai(1, 'AI-2', UNKNOWN_REF),
    { seat: 2, name: '我', stack: 10000, kind: 'human' as const },
  ]
  await createGame(page, seats)
  await page.goto('/')
  await expect(page.getByText('真人视角')).toBeVisible()
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible({ timeout: 15_000 })

  const model = page.locator('.seat .seat-model').first()
  await expect(model).toHaveText('(V4 Pro)')
  const css = await model.evaluate((el) => {
    const s = getComputedStyle(el)
    return { fontSize: s.fontSize, maxWidth: s.maxWidth }
  })
  expect(css.fontSize).toBe('11px')
  expect(css.maxWidth).toBe('120px')

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)

  await page.screenshot({ path: path.join(OUT, 'mobile-model.png'), fullPage: false })
})
