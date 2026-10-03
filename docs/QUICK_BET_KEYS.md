# 真人加注体验增强：快捷注比例按钮 + 键盘快捷键

参考 PokerTH 的加注交互，为真人上桌的底部操作区增加「快捷注」按钮组与键盘快捷键。
保持现有深色扑克桌风格与斗地主式底部行动区，不新增后端字段、不改 DTO、不碰 Go。

## 改动文件

| 文件 | 改动 |
|---|---|
| `web/src/quickbet.ts` | 新增。快捷注比例定义与目标值计算（纯函数） |
| `web/src/components/HumanActionPanel.tsx` | 快捷注按钮组、键盘快捷键、按钮快捷键提示、回车提交 |
| `web/src/styles.css` | 快捷注按钮、`kbd` 快捷键徽标、移动端适配样式 |
| `tests/e2e/browser/fixture/tests/quick-bet-keys.spec.ts` | 新增。fixture 浏览器验收（本文件仅为验证，非产品产物） |
| `docs/QUICK_BET_KEYS.md` | 本文件 |

## 一、快捷注计算逻辑

`to` 语义：**本街累计投入目标值**（契约 Action.To），不是追加量。

公式（`web/src/quickbet.ts`）：

```
基准池 base = hand.pot + hand.current_bet + legal.call_amount
比例注 to_raw = round(ratio * base)
比例注 to     = clamp(to_raw, legal.min_raise_to, legal.max_raise_to)
全押   to     = legal.max_raise_to
```

规则：

- 比例集合：1/3、1/2、2/3、3/4、1× 底池，另加「全押」。
- **clamp 边界处理**：比例结果低于 `min_raise_to` 时上取到 `min_raise_to`，高于 `max_raise_to` 时下取到 `max_raise_to`（即全押上限）。
- `can_raise = false` → 整组快捷注禁用（目标值均为 `null`，按钮 `disabled`）。
- **仅短全押**（`max_raise_to < min_raise_to`，此时 `short_all_in_to = max_raise_to`）→ 比例注无意义返回 `null`（禁用）；「全押」仍可用并取 `max_raise_to`（即 `short_all_in_to`）。

交互选择：点击快捷注**把目标值填入加注输入框并聚焦，不自动提交**，玩家用「加注」按钮或回车确认。
（比直接提交更安全，避免一次误点即下重注；也符合 PokerTH「先设注额再确认」的手感。）

### fixture 数值校验（真人等待点）

fixture 固定下发 `pot=150, current_bet=100, call_amount=100, min_raise_to=200, max_raise_to=10000`，
故 `base = 150 + 100 + 100 = 350`：

| 比例 | 原始值 | clamp 后 `to` |
|---|---|---|
| 1/3 底池 | round(116.67)=117 | **200**（上取到 min） |
| 1/2 底池 | 175 | **200**（上取到 min） |
| 2/3 底池 | round(233.33)=233 | **233** |
| 3/4 底池 | round(262.5)=263 | **263** |
| 1× 底池 | 350 | **350** |
| 全押 | — | **10000** |

## 二、快捷键映射

| 键 | 英文语义 | 动作 | 显示 |
|---|---|---|---|
| `F` | Fold | 弃牌（直接提交） | 弃牌 `(F)` |
| `C` | Call | 跟注（直接提交） | 跟注 `(C)` |
| `K` | check | 过牌（直接提交） | 过牌 `(K)` |
| `R` | Raise | 聚焦加注输入框（不自动提交） | 加注 `(R)` |

生效条件与互斥：

- 仅当轮到自己且可行动时生效：等价于内部 `canAct = isMyTurn && !controlsBusy && !submitting && !expired`。
- **输入框聚焦时不误触**：焦点在 `input/textarea/select/contenteditable` 时不响应全局快捷键。
- 弹层打开（`.overlay`）时不响应，避免查看手牌详情时误提交。
- 带 `Ctrl/Alt/Meta` 修饰键或长按重复（`repeat`）不拦截。
- 每个按键只在对应合法动作存在时生效（如 `can_check=false` 时 `K` 无效果）。
- 全局监听一次注册（`useRef` 保存最新状态），组件卸载时清理。
- 移动端无物理键盘，不产生 `keydown`，天然不触发；软键盘输入聚焦在输入框时也被焦点保护拦截。
- 另支持在加注输入框内按 `Enter` 提交加注。

快捷键提示通过 `<kbd class="keyhint">` 显示在按钮上，并加 `aria-keyshortcuts`；
按钮的 `aria-label` 保持纯动作名（如「弃牌」），避免影响既有无障碍定位。

「全押」按钮不重复：有加注权时以快捷注的「全押」呈现（同一 `<button>`，页面上始终只有一个名为「全押」的按钮）；
无加注权但跟注即全下时，仍在动作按钮行保留「全押」。

## 三、验证命令与完整输出

### 1. 类型检查

```
$ npm --prefix web run typecheck

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

```

（无错误输出，退出码 0）

### 2. 构建

```
$ npm --prefix web run build

> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
transforming...
✓ 43 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-DUAoSD3x.css   26.37 kB │ gzip:  6.92 kB
dist/assets/index-ZO5d6aY3.js   265.81 kB │ gzip: 83.65 kB
✓ built in 657ms
```

（退出码 0）

### 3. fixture 浏览器验收（Playwright + Chromium）

```
$ cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts

Running 11 tests using 1 worker

  ✓   1 [chromium] › fixture/tests/human-player.spec.ts:19:1 › 全 AI 创建走公共观战，不出现真人行动面板 (609ms)
  ✓   2 [chromium] › fixture/tests/human-player.spec.ts:31:1 › 真人座位：配置页隐藏模型下拉，创建后进入真人视角 (3.1s)
  ✓   3 [chromium] › fixture/tests/human-player.spec.ts:44:1 › 轮到真人：座位光圈与「轮到我」、底部倒计时进度条与操作按钮组 (3.2s)
  ✓   4 [chromium] › fixture/tests/human-player.spec.ts:76:1 › AI 轮转：底部显示「思考中…」与不确定进度，行动历史高亮新动作 (3.2s)
  ✓   5 [chromium] › fixture/tests/human-player.spec.ts:96:1 › 无 Cookie 浏览器：人类牌局仅公共观战，看不到真人底牌与行动面板 (275ms)
  ✓   6 [chromium] › fixture/tests/human-player.spec.ts:123:1 › 移动端 390 宽：轮转提示与底部操作区可点无遮挡 (3.2s)
  ✓   7 [chromium] › fixture/tests/quick-bet-keys.spec.ts:26:1 › 快捷注：六个比例按钮渲染，目标值按基准池 clamp，点击填入输入框 (3.2s)
  ✓   8 [chromium] › fixture/tests/quick-bet-keys.spec.ts:57:1 › 快捷键 F：直接弃牌并收起操作面板 (3.2s)
  ✓   9 [chromium] › fixture/tests/quick-bet-keys.spec.ts:66:1 › 快捷键 R 聚焦加注输入框；输入框聚焦时 F/C/K 不误触 (3.2s)
  ✓  10 [chromium] › fixture/tests/quick-bet-keys.spec.ts:87:1 › 键盘 R + 输入 + 回车：提交加注动作 (3.2s)
  ✓  11 [chromium] › fixture/tests/quick-bet-keys.spec.ts:100:1 › 移动端 390 宽：快捷注按钮可见且不超出视口 (3.2s)

  11 passed (30.2s)
```

（退出码 0；含既有 6 条真人交互用例未回归）

## 四、未验证边界

1. **真实 Go 联调未做**：以上均为本地 fixture（内存 mock DTO）浏览器验收，不是真实引擎/服务链路。
   真实链路（真实 `LegalActions`、真实 `pot/current_bet/call_amount`、409 冲突）需另起 Go 服务跑。
2. **`can_raise=false` 快捷注禁用态**：fixture 在真人等待点恒为 `can_raise=true`，该禁用分支仅由代码路径与类型检查覆盖，无浏览器实测。
3. **仅短全押（`max_raise_to < min_raise_to`）分支**：fixture 无此状态，`short_all_in_to` 场景未实测。
4. **`controlsBusy/submitting/expired` 对快捷键的互斥**：逻辑上收敛于 `canAct`，但未用 fixture 构造点击中的竞态实测。
5. **移动端「无物理键盘」语义**：仅验证了窄屏布局不溢出，未在触屏设备上验证软键盘行为。

## 五、契约字段核对

`LegalActionsDTO` 已含 `can_raise / min_raise_to / max_raise_to / short_all_in_to / call_amount`，
`HandDTO` 已含 `pot / current_bet`，快捷注计算所需字段齐全，**无契约缺口，未改后端 DTO**。
