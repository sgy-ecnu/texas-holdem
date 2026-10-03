import { expect, test } from '@playwright/test'

// 快捷注 + 键盘快捷键的 fixture 浏览器验收（仅前端交互，不是真实 Go 链路）。
// fixture 在真人等待点时固定下发：
//   pot=150, current_bet=100, call_amount=100, min_raise_to=200, max_raise_to=10000
// 因此基准池 = 150 + 100 + 100 = 350。

test.beforeEach(async ({ request }) => {
  await request.post('/__fixture/reset')
})

async function createHumanGame(page: import('@playwright/test').Page) {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '新建牌局' })).toBeVisible()
  await page.getByLabel('真人座位').selectOption('0')
  await page.getByRole('button', { name: '创建牌局' }).click()
  await expect(page.getByText('真人视角')).toBeVisible()
}

async function waitMyTurn(page: import('@playwright/test').Page) {
  await page.getByRole('button', { name: '开始' }).click()
  await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('.action-panel.is-turn')).toBeVisible()
}

test('快捷注：六个比例按钮渲染，目标值按基准池 clamp，点击填入输入框', async ({ page }) => {
  await createHumanGame(page)
  await waitMyTurn(page)

  const quick = page.locator('.quick-bets')
  await expect(quick).toBeVisible()
  await expect(quick.locator('.quick-bet')).toHaveCount(6)

  // 基准池 350；1/3≈117 被 clamp 到 min 200；2/3≈233；3/4≈263；1×=350；全押=10000。
  const expectAmount = async (key: string, text: string) => {
    await expect(quick.locator(`[data-quick="${key}"] .qb-to`)).toHaveText(text)
  }
  await expectAmount('1/3', '200')
  await expectAmount('1/2', '200')
  await expectAmount('2/3', '233')
  await expectAmount('3/4', '263')
  await expectAmount('1', '350')
  await expectAmount('allin', '10,000')

  // 点击填入输入框（不自动提交），命中高亮
  await quick.locator('[data-quick="2/3"]').click()
  await expect(page.locator('#raise-to')).toHaveValue('233')
  await expect(quick.locator('[data-quick="2/3"]')).toHaveAttribute('aria-pressed', 'true')

  await quick.locator('[data-quick="1"]').click()
  await expect(page.locator('#raise-to')).toHaveValue('350')

  await quick.locator('[data-quick="allin"]').click()
  await expect(page.locator('#raise-to')).toHaveValue('10000')
})

test('快捷键 F：直接弃牌并收起操作面板', async ({ page }) => {
  await createHumanGame(page)
  await waitMyTurn(page)

  await page.keyboard.press('f')
  await expect(page.locator('.action-panel.is-turn')).toHaveCount(0, { timeout: 15_000 })
  await expect(page.locator('.action-feed .feed-row').last()).toContainText('弃牌')
})

test('快捷键 R 聚焦加注输入框；输入框聚焦时 F/C/K 不误触', async ({ page }) => {
  await createHumanGame(page)
  await waitMyTurn(page)

  await page.keyboard.press('r')
  await expect(page.locator('#raise-to')).toBeFocused()

  // 输入框聚焦：F 不得弃牌、K 不得过牌（fixture can_check=false），仍是本方行动点
  await page.keyboard.press('f')
  await page.keyboard.press('k')
  await expect(page.locator('.action-panel.is-turn')).toBeVisible()
  await expect(page.locator('.hand-dock[data-turn="me"]')).toBeVisible()

  // 失焦（Tab 到「加注」按钮）后按键重新生效：C 跟注
  await page.keyboard.press('Tab')
  await expect(page.locator('#raise-to')).not.toBeFocused()
  await page.keyboard.press('c')
  await expect(page.locator('.action-panel.is-turn')).toHaveCount(0, { timeout: 15_000 })
  await expect(page.locator('.action-feed .feed-row').last()).toContainText('跟注')
})

test('键盘 R + 输入 + 回车：提交加注动作', async ({ page }) => {
  await createHumanGame(page)
  await waitMyTurn(page)

  await page.keyboard.press('r')
  await expect(page.locator('#raise-to')).toBeFocused()
  await page.keyboard.type('250')
  await page.keyboard.press('Enter')

  await expect(page.locator('.action-panel.is-turn')).toHaveCount(0, { timeout: 15_000 })
  await expect(page.locator('.action-feed .feed-row').last()).toContainText('加注到')
})

test('移动端 390 宽：快捷注按钮可见且不超出视口', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await createHumanGame(page)
  await waitMyTurn(page)

  const quick = page.locator('.quick-bets')
  await expect(quick).toBeVisible()
  for (const key of ['1/3', '1/2', '2/3', '3/4', '1', 'allin']) {
    const box = await quick.locator(`[data-quick="${key}"]`).boundingBox()
    expect(box).not.toBeNull()
    expect(box!.x).toBeGreaterThanOrEqual(0)
    expect(box!.x + box!.width).toBeLessThanOrEqual(390)
  }
})
