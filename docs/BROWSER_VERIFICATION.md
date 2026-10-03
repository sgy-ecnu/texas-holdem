# 浏览器验收报告（真实 Playwright + Chromium）

> 历史端口说明：本文原始验证输出对应旧 HTTP 8080，保持原样，不作为当前地址说明。
> 当前 HTTP 为 8081；新的真实浏览器验证见 PORT_8081_VERIFICATION.md。

范围：前端观战台的真实浏览器验收与视觉/互动校验。写范围仅 `web/**`（未改 `web/embed.go`）、
`tests/e2e/browser/**`（新增）、本文件；未改 `Makefile` / `tests/e2e/run.py` / `scripts/local.py` / Go / `.env`。

未被要求也不在本报告范围：真实付费模型调用（`allow_paid_models=false`，全程未触发任何 provider 请求）。

## 环境

| 项 | 值 |
|---|---|
| Playwright | `@playwright/test` 1.63.0 |
| 浏览器 | Chromium 153.0.8010.12（`chromium-1243`，cache `~/Library/Caches/ms-playwright`） |
| Node / npm | v26.6.0 / 11.18.0 |
| 前端构建 | Vite 7.3.6 + React 19 + TS 5.9.3 |
| 服务 | 真实 Go 子进程（`python3 scripts/local.py dev`），MySQL 3307 / Redis 6380 |
| 模型 | catalog 仅展示（25 项：1 mock + 24 real），牌局全部 `mode=mock`，`live_calls_used=0` |

## 可复现命令

```bash
# 前置：deps 已在跑；web/dist 已构建
cd tests/e2e/browser && npm install && npx playwright install chromium
# 运行（wrapper 自管真实服务生命周期）
python3 tests/e2e/browser/run_browser.py
```

wrapper 结束输出（本次最终运行，动画已禁用后重跑）：

```
Running 6 tests using 1 worker
  ✓ scenario 1 · 配置 2/9 人且每座选择不同模型 (747ms)
  ✓ scenario 2 · 创建/开始至 max_hands 并核对结束详情数值数组 (2.0s)
  ✓ scenario 3 · 暂停 / 刷新找 current / 恢复 / 停止 (1.4s)
  ✓ scenario 4 · live 禁用与费用确认门槛 (615ms)
  ✓ scenario 6 · 运行中无底牌 / 完成后详情可揭示 (2.6s)
  ✓ scenario 5 · 手机宽 390 可操作无遮挡 › 390 宽配置与运行控制可用 (794ms)
  6 passed (8.6s)
browser-wrapper: passed=6 failed=0 exit=0
browser-wrapper: own HTTP process stopped; dependencies retained
```

## 逐场景 → 测试 → 结果

| # | 场景 | 测试 | 结果 | 关键证据 |
|---|---|---|---|---|
| 1 | 配置 2/9 人与每座不同模型 | `scenario 1` | PASS | 9 座各自选到不同模型（`Set` 去重=9），`s1-config-9seats.png` |
| 2 | 创建/开始多手至 max_hands；结束详情数值数组 | `scenario 2` | PASS | 手数 3/3；详情底牌每人 2 张；API 交叉核对 `pots/payouts/final_stacks` 为数组、`final_stacks` 合计 20000，`s2-completed-detail.png` |
| 3 | 暂停 / 刷新找 current / 恢复 / 停止 | `scenario 3` | PASS | 真实 `page.reload()` 后由 `/games/current` 找回 paused 局，`s3-paused-after-reload.png`（另 `s3-stopped.png`） |
| 4 | live 禁用与费用确认 | `scenario 4` | PASS | 真实服务 Live 禁用 + 未授权文案；拦截 health 呈现 live 可用态，确认勾选项可见、未确认被客户端拦截（0 次创建请求），`s4-live-disabled.png` / `s4-live-confirm.png` |
| 5 | 手机宽 390 无遮挡可操作 | `scenario 5` | PASS | 无横向溢出，暂停按钮 boundingBox 在视口内且可点击，`s5-mobile-config.png` / `s5-mobile-controls.png` |
| 6 | 运行无底牌 / 完成详情可揭示 | `scenario 6` | PASS | 运行中仅牌背、无 `.hole-item`、无弹层；完成后详情揭示每人 2 张底牌，`s6-running-nohole.png` / `s6-detail-reveal.png` |

Playwright 汇总：`6 passed / 0 failed / 0 skipped`。

## 截图（被忽略目录，不入库）

`web/node_modules/.cache/browser-verify/20261003-180408/`

```
s1-config-9seats.png      s2-completed-detail.png   s3-paused-after-reload.png
s3-stopped.png            s4-live-disabled.png      s4-live-confirm.png
s5-mobile-config.png      s5-mobile-controls.png    s6-running-nohole.png
s6-detail-reveal.png
```

视觉/互动结论（依据截图与失败期无障碍快照逐张核对）：
- 深色墨绿绒面 + 黄铜描边牌桌，座位环绕、行动位高亮、盲注/庄家标记、公共牌与底池居中；中文标签与安全错误文案一致。
- 运行中只有牌背占位，公共牌正常显示；完成手牌详情才出现底牌，并标注获胜座位与边池分配。
- 390 宽为纵向堆叠 + 座位双列，控制按钮换行但均在视口内可点击。

## 本轮修复

- `web/src/components/ConfigPanel.tsx`：修正 mock 模式校验 bug——mock 下 `maxLiveCalls` 输入框默认仍为 `100`，
  旧校验误判「mock 模式下 live 额度必须为 0」而拦截创建。mock 分支不再校验该（未渲染的）字段；请求体对 mock 仍强制传 `0`。契约与设计意图不变。
- 测试截图助手启用 `animations: 'disabled'`，消除座位入场动画导致的全页截图拼接错位（假象，非真实布局缺陷）。

## 未验证 / 边界

- **真实付费模型**：未验证。服务 `allow_paid_models=false`，全程无任何 provider 调用；场景 4 的 live「费用确认」仅验证 UI 分支（拦截 health），确认勾选后的真实付费创建路径不可达，按设计未启用。
- infra 的 `tests/e2e/run.py browser` 与 `make e2e-browser` 仍为占位（BLOCKED），未改动；本轮以 `tests/e2e/browser/run_browser.py` 为独立入口。
- 未做真实多浏览器（仅 Chromium）与真机移动端验证。
- 依赖进程（MySQL/Redis）由用户环境保留；本轮仅回收自己拉起的 8080。
