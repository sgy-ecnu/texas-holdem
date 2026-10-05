# 座位动作动效收尾：下注徽章遮挡修复 + 特效分级收敛

状态：前端实现完成，fixture 浏览器验证通过；**真实 Go 联调未做**，实际观感需 `make run` 后目视确认。未提交、未推送。

范围：仅前端视觉（`web/src/components/ActionPopover.tsx`、`web/src/components/PokerTable.tsx`、`web/src/components/Seat.tsx`、`web/src/styles.css`）；
不动交互逻辑 / 去重 / 生命周期（`SpectatorView.tsx` 未改）、`format.ts`、`types.ts`、Go、scripts、config。

---

## 改动 1：下注徽章遮挡真人底牌 / 横穿座位卡（独立布局 bug）

### 根因（已定位）

`.rim-mark`（"本轮 X · 累计 Y"）用 `transform: translate(-50%, -50%)` 做居中，但它的入场动画 `badge-in`
（`fill-mode: both`）也动画 `transform`，末帧为 `translate(-50%, 0) scale(1)`，**覆盖了居中位移**。
结果徽章以「顶边」而非「中心」落在座位坐标上，整体下坠约半个自身高度，正好压到底牌/座位卡。
加之桌面定位用固定内收系数（`t=0.62/0.85`）不随 stage 尺寸缩放，矮桌面下重叠更严重。

实测（注入本街下注、真人局，量测徽章底边 vs 真人底牌顶边的间距）：

| 视口 | 间距 gap（正=不重叠） |
| --- | --- |
| 1536×1024 | +7.6px（贴边） |
| 1440×900 | **−15.9px（重叠）** |
| 1280×720 | **−50.2px（重叠）** |
| 1280×680 | **−57.8px（重叠）** |

### 方案

1. **修复居中**：`.rim-mark` 改用自带的 `@keyframes rim-mark-in`，起止保留 `translate(-50%, -50%)`，
   不再被 `badge-in` 覆盖（`badge-in` 仍留给 `.turn-badge`）。
2. **按像素保证间距（不靠 z-index 硬盖）**：徽章沿「卡心 → 桌心」方向内移，
   位移量 = 座位卡半尺寸 + 徽章半尺寸 + 间隙，随 stage 尺寸自适应：
   `off = |ux|·(70+68) + |uy|·(29+12) + 8`，再换算成 stage 百分比。
3. **本人座位特例**：本人（真人）底牌在其座位卡正上方，属垂直方向唯一冲突区。
   把本人座位的徽章改放**座位卡左侧、与卡垂直居中**（`seatX −146px`，即座位半宽 70 + 徽章半宽留量 68 + 间隙 8），
   彻底避开底牌与座位卡第一行；与右侧的动作标签形成左右对称。

新增 `data-seat` 便于逐座位判定，`is-own` 标记本人徽章。

### 验证结果（fixture，非真实 Go 链路）

- 桌面（真人局，注入 street_bet/committed）：1536×1024 / 1440×900 / 1280×720 三视口下，
  徽章与「本人底牌 / 座位卡第一行 / 座位卡 / 任意底牌」零相交。
- 桌面 9 座密集顶排：每个徽章不压自身座位卡。
- 移动端 390：桌沿层本就隐藏，座位卡内 `.bet` 行不遮底牌/第一行、横向不越出卡、无横向溢出。

截图：`docs/action-popover/final-desktop-human-badge.png`、`final-desktop-9seat-badge.png`、`final-mobile-bet.png`。

---

## 改动 2：动作特效分级收敛（回归「轻量提示」）

取消上一版过大的 scale（桌面 1.30/1.55/1.75）与回弹；按动作强度分档，只保留极轻的运动过程。

### 分级参数

| kind | 强度 class | 初始 scale | 可见时长（动画时长） | 运动 |
| --- | --- | --- | --- | --- |
| `fold` / `check` | `is-neutral` | **1（固定尺寸）** | **1000ms** | 只淡入淡出，不缩放、不回弹 |
| `call` | `is-call` | **1（固定尺寸）** | **1200ms** | 靠金色文字/边框区分，不缩放 |
| `raise_to` | `is-raise` | **1.08**（≤1.10） | **1500ms** | 前段轻微放大后收敛到 1，**无回弹** + 静态轻光晕 |

- `HINT_DURATION_MS`：由 2400 改为 **1600ms**，仅作移除兜底（须 ≥ 最长档 1500ms）；单个标签可见时长由 CSS `--pop-dur` 按 kind 决定。
  早结束的档位在移除前已淡出为不可见。**全押**无独立 `ActionKind`，第一版按 `raise_to` 处理（不新增字段）。
- 移动端（≤720px）：过牌/弃牌/跟注 `--pop-scale: 1`、加注 `1.05`，`transform-origin: center top`，不溢出。
- `transform-origin` 分派保留：普通座位 `center bottom`、底部真人座位 `left center`。
- reduced-motion 降级保留：`animation: none`、`opacity: 1`、`transform: none`，静态可见，由 timer 在 1600ms 后移除。
- 复用 action ID 作 React key 触发重播（未改）。

### 关键帧

```css
@keyframes action-pop {
  0%   { opacity: 0; transform: scale(var(--pop-scale)); animation-timing-function: ease-out; }
  12%  { opacity: 1; transform: scale(var(--pop-scale)); animation-timing-function: ease-out; }
  30%  { opacity: 1; transform: scale(1); animation-timing-function: linear; }
  78%  { opacity: 1; transform: scale(1); animation-timing-function: ease-in; }
  100% { opacity: 0; transform: scale(1); }
}
/* .action-popover { animation: action-pop var(--pop-dur) linear both; } */
```

过牌/弃牌/跟注 `--pop-scale=1` → 全程 `scale(1)`，仅透明度变化；加注 `1.08 → 1`（12–30%），全程 `scale ≥ 1`，无下冲回弹。

### 验证结果

- 分级正确：neutral/call `--pop-scale=1`、raise `1.08`；时长 1s / 1.2s / 1.5s；origins 符合分派。
- 无回弹：keyframes 不含 `scale(0.97)`；加注档定格多个时间点 scale 均 ≥ 1。
- 移动端收敛 ≤1.05、不溢出；reduced-motion 静态可见、保留颜色、1600ms 后移除。

截图：`final-desktop-call.png`、`final-desktop-raise.png`、`final-mobile-raise.png`、`final-reduced-motion.png`。

---

## 改动文件

实现（独占写范围）：
- `web/src/components/ActionPopover.tsx`：`HINT_DURATION_MS` 2400→1600；注释同步分级语义。
- `web/src/styles.css`：动作标签分档（`--pop-scale` / `--pop-dur`）、新 `@keyframes action-pop`（无回弹）、
  `@keyframes rim-mark-in`、`.rim-mark` 改用该动画、移动端收敛、reduced-motion 注释。
- `web/src/components/PokerTable.tsx`：桌沿徽章定位改为「径向按像素保证间距」+ 本人座位左侧特例；`data-seat` / `is-own`。
- `web/src/components/Seat.tsx`：本轮未改（`.bet` 已有结构满足要求；未改文件内容）。

验证用测试（新增 / 更新）：
- `tests/e2e/browser/fixture/tests/bet-badge-clearance.spec.ts`（新增）
- `tests/e2e/browser/fixture/tests/action-popover-scale.spec.ts`（更新为分级契约）

未改（按要求）：`SpectatorView.tsx`（去重 / timer / `HINT_DURATION_MS` 消费不动）、`format.ts`、`types.ts`、Go、scripts、config。

---

## 验证证据

### 1) 类型检查与构建

```
=== npm --prefix web run typecheck ===

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

EXIT=0

=== npm --prefix web run build ===

> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
✓ 44 modules transformed.
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-DXkw1z17.css   30.72 kB │ gzip:  7.78 kB
dist/assets/index-CGpkiNEs.js   270.11 kB │ gzip: 85.35 kB
✓ built in 673ms
EXIT=0
```

### 2) fixture 浏览器（Playwright，非真实 Go 链路）

```
cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts bet-badge-clearance
  ✓ 1 桌面：下注徽章不遮真人底牌/座位卡（1536/1440/1280 多视口） (3.4s)
  ✓ 2 桌面 9 座密集：每个下注徽章不压自身座位卡 (631ms)
  ✓ 3 移动端 390：座位卡内 .bet 不遮底牌/第一行、无横向溢出 (1.5s)
  3 passed (6.1s)

cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts action-popover-scale
  ✓ 1 桌面：分级 scale/时长 + 无回弹 + transform-origin (5.9s)
  ✓ 2 移动端 390：分级 scale 收敛（<=1.05）、origin center top、不溢出 (3.6s)
  ✓ 3 reduced-motion：静态可见、无缩放、保留动作颜色 (3.4s)
  3 passed (13.3s)

全量回归：npx playwright test -c playwright.fixture.config.ts
  29 passed (2.1m)
```

### 3) 场景逐条对齐（场景 → 测试 → 结果）

| 需求场景 | 测试 | 结果 |
| --- | --- | --- |
| 徽章不遮真人底牌（桌面多视口） | bet-badge-clearance「桌面…多视口」 | ✅ |
| 徽章不横穿座位卡第一行（桌面多视口） | 同上（徽章 ∩ 第一行 = ∅） | ✅ |
| 9 座密集顶排不压座位卡 | bet-badge-clearance「9 座密集」 | ✅ |
| 移动端 `.bet` 不遮底牌/第一行、不溢出 | bet-badge-clearance「移动端 390」 | ✅ |
| 过牌/弃牌固定尺寸、不缩放不回弹 | action-popover-scale「桌面…」 | ✅ |
| 跟注固定尺寸（颜色/边框区分） | 同上 | ✅ |
| 加注 1.08、无回弹 | 同上（定格帧 scale≥1） | ✅ |
| 分级时长 1s/1.2s/1.5s、兜底 1600ms | 同上（animationDuration） | ✅ |
| transform-origin 分派 | 同上 | ✅ |
| 移动端收敛 ≤1.05、不溢出 | 「移动端 390」 | ✅ |
| reduced-motion 静态可见、保留颜色 | 「reduced-motion」 | ✅ |

---

## 未验证边界（如实说明）

- **真实 Go 联调未做**：均为本地 fixture + `page.route` 注入；实际观感/节奏需 `make run` 后目视确认。
- **徽章定位按像素保证间距**：位移量覆盖「座位卡半尺寸 + 徽章半尺寸 + 8px」，数学上与 stage 尺寸无关，理论任意桌面尺寸都不压座位卡；
  仅当同一座位徽章文案极长（远超预留半宽）时右侧可能逼近本人座位卡——当前 `本轮 X · 累计 Y` 文案不会触发，未做超长强制换行。
- **全押档**：无独立 `ActionKind`，按 `raise_to` 处理；如需独立更醒目档位需后端新增字段（第一版不做）。
- **reduced-motion 时长**：因移除 timer 用固定 `HINT_DURATION_MS=1600`，降级下各档均静态可见到 1600ms（不存在分级时长）；不影响正常动画档。
- **`--pop-scale` / `--pop-dur` 依赖 CSS 自定义属性**：现代 Chromium 已验证；未覆盖旧版浏览器。
- **无折中降级**：本轮桌面（含 1280×720 矮视口）已完全解决遮挡，未采用「移动端回落卡内」以外的降级；移动端沿用既有「卡内 .bet」方案。
