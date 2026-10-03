# 浏览器验收（Playwright + Chromium）

真实浏览器验收德州扑克观战台：由 Python wrapper 安全启动真实 Go 子进程（经 `scripts/local.py dev`），
再用 Chromium 打开真实前端，通过真实 UI 按钮创建/开始/暂停/恢复/停止牌局。

> infra 的 `tests/e2e/run.py browser` 与 `make e2e-browser` 目前仍是占位（BLOCKED），
> 本目录是独立、可复现的浏览器验收入口，不改动 `run.py` / `Makefile` / `local.py`。

## 前置

- MySQL(3307) / Redis(6380) 已运行（保留，不停止；wrapper 只回收自己拉起的 8080）。
- `web/dist` 已存在（服务端 `go:embed` 依赖；改动前端后先 `npm --prefix web run build`）。
- 首次需安装依赖与 Chromium（下载到本机 cache `~/Library/Caches/ms-playwright`）：

```bash
cd tests/e2e/browser
npm install
npx playwright install chromium
```

## 运行

```bash
# 全部场景
python3 tests/e2e/browser/run_browser.py

# 单场景（透传给 playwright test 的参数，按标题 grep）
python3 tests/e2e/browser/run_browser.py --grep "scenario 4"
```

Wrapper 负责：预检 8080 空闲 → 启动 `python3 scripts/local.py dev` → 等待 `/api/v1/health` 且
`allow_paid_models=false` → 运行 `npx playwright test` → **强制回收自己启动的 8080**（依赖保留）。
凭据只在 `local.py` 进程内从受保护文件载入，wrapper 与测试全程不读取、不输出任何凭据。

## 场景 → 测试

| 场景 | 测试 | 断言要点 |
|---|---|---|
| 1 配置 2/9 人与每座不同模型 | `scenario 1 · 配置 2/9 人且每座选择不同模型` | 默认 2 座；扩到 9 座；9 座选择互不相同的模型（`Set` 去重）；SB/BB/max_hands 默认值 |
| 2 创建/开始至 max_hands 并核对详情数值 | `scenario 2 · 创建/开始至 max_hands 并核对结束详情数值数组` | UI 点「开始」到「已完成」、手数 3/3；详情弹层底牌 2 张/人；API 交叉核对 `pots/payouts/final_stacks` 为数组且 `final_stacks` 合计 20000 |
| 3 暂停/刷新找 current/恢复/停止 | `scenario 3 · 暂停 / 刷新找 current / 恢复 / 停止` | 暂停→真实 `page.reload()` 由 `/games/current` 找回→恢复→停止；终态出现「新建牌局」 |
| 4 live 禁用与费用确认 | `scenario 4 · live 禁用与费用确认门槛` | 真实服务 `allow_paid_models=false` 时 Live 按钮禁用；未确认文案。用拦截 health 呈现 live 可用态验证「确认勾选」UI 分支，未确认时客户端拦截（无创建请求），不发生付费调用 |
| 5 手机宽 390 无遮挡可操作 | `scenario 5 · 手机宽 390 可操作无遮挡 › 390 宽配置与运行控制可用` | 视口 390×844；无横向溢出；暂停按钮 boundingBox 在视口内并可点击；暂停/停止可用 |
| 6 运行无底牌 / 完成可揭示 | `scenario 6 · 运行中无底牌 / 完成后详情可揭示` | 运行中仅牌背（`.mini-card`）、无 `.hole-item`、无弹层；完成后详情揭示每人 2 张底牌 |

截图输出（被忽略目录，不入库）：
`web/node_modules/.cache/browser-verify/<时间戳>/`。

## 注意

- 后端同一时间只允许一个非终态牌局，测试串行（`workers: 1`），每个用例 `afterEach` 通过 API 兜底停止自己产生的非终态局。
- mock 极快：除场景 2 用小 `max_hands` 验证完成外，其余需要「暂停」的场景都用大 `max_hands`，避免在点击前就结束。
