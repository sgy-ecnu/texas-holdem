# 座位旁动作标签「缩放回弹」醒目动效

状态：前端实现完成，fixture 浏览器验证通过；**真实 Go 联调未做**，实际观感需 `make run` 后目视确认。
范围：仅前端（组件 / CSS），不动后端、不动去重与生命周期逻辑（`SpectatorView.tsx` 未改）。未提交、未推送。

## 一、需求与方案

用户反馈旧动效「不明显」（12px 标签 + 6px 上浮 + 150ms 淡入）。按架构师方案改为：
**弹出较大尺寸 → 缩放回正常并轻微回弹 → 停留 → 只淡出**，总时长保持 1600ms；
并按动作类型分档放大与配色，提升可辨识度。

## 二、改动文件

修改：
- `web/src/components/ActionPopover.tsx`：`ActionHint` 增加 `kind: ActionKind`；按 `kind` 映射强度分档 class；更新注释。
- `web/src/styles.css`：新增 `--pop-scale`、三档强度样式、缩放回弹 keyframes、`transform-origin` 分派、移动端收敛、`padding-bottom` 34px。

未改（按要求）：
- `web/src/components/SpectatorView.tsx`（去重 / timer / `HINT_DURATION_MS` 均不动）
- `web/src/components/PokerTable.tsx`（继续传 `hint` + `placement`，缩放原点交由 CSS 按 `is-above`/`is-right` 分派）
- `web/src/format.ts`、`web/src/types.ts`（文案与契约不变）

新增（验证用）：
- `tests/e2e/browser/fixture/tests/action-popover-scale.spec.ts`
- `docs/ACTION_POPOVER_SCALE.md` + `docs/action-popover/scale-*.png`

## 三、缩放回弹 keyframes（`styles.css`）

```css
@keyframes action-pop {
  0% {
    opacity: 0.2;
    transform: scale(var(--pop-scale));
    animation-timing-function: linear;
  }
  3.75% {
    opacity: 1;
    transform: scale(var(--pop-scale));
    animation-timing-function: cubic-bezier(0.2, 0.8, 0.25, 1.12);
  }
  18.75% {
    opacity: 1;
    transform: scale(1);
    animation-timing-function: linear;
  }
  87.5% {
    opacity: 1;
    transform: scale(1);
    animation-timing-function: linear;
  }
  100% {
    opacity: 0;
    transform: scale(1);
  }
}
```

相位换算（总 1600ms）：

| 时间 | 百分比 | 行为 |
| --- | --- | --- |
| 0–60ms | 0–3.75% | 大尺寸（`scale(var(--pop-scale))`）显现，`opacity 0.2→1`（回弹前已可见） |
| 60–300ms | 3.75–18.75% | 缩放回 1，`cubic-bezier(0.2, 0.8, 0.25, 1.12)` 先略超调再落回（一次轻微回弹，非先放大） |
| 300–1400ms | 18.75–87.5% | `scale 1`、`opacity 1` 停留 |
| 1400–1600ms | 87.5–100% | 只淡出，不再缩放/位移 |

要点：大尺寸阶段 `opacity` 从 0.2 快速升到 1，**在缩回开始前已完全可见**；结束后段不再缩放，避免「淡出时还在动」。

动画声明：`.action-popover { animation: action-pop 1600ms linear both; }`（区间缓动由 keyframe 内的 `animation-timing-function` 控制）。

## 四、动作强度分档

按动作类型（非解析中文文案）映射 class 与初始缩放：

| kind | 强度 class | 初始 scale | 颜色/边框 |
| --- | --- | --- | --- |
| `fold` / `check` | `is-neutral` | **1.20** | 中性高对比文字（`--ink`）、弱边框 |
| `call` | `is-call` | **1.40** | 金色文字（`--gold-soft`）、边框稍清晰 |
| `raise_to` | `is-raise` | **1.55** | 更亮金色文字（`#f6e7b8`）、清晰边框 + 静态轻光晕（`box-shadow`，不参与动画） |

```ts
type ActionIntensity = 'neutral' | 'call' | 'raise'
const INTENSITY: Record<ActionKind, ActionIntensity> = {
  fold: 'neutral', check: 'neutral', call: 'call', raise_to: 'raise',
}
```

`ActionHint` 现为 `{ id, seat, text, kind }`；`kind` 由 `hintFromAction(gameId, action)` 直接取自 `ActionDTO.kind`，
组件按 `kind` 选择 class，不解析文案。初始缩放通过 class 上的 `--pop-scale` 注入 keyframes 的 `scale(var(--pop-scale))`。

> 说明：全押（all-in）目前无独立动作类型（仍归 `raise_to`/`call`）；若要单独档位需后端额外字段，第一版不做。

## 五、transform-origin 分派

- 普通座位 `.is-above`：`transform-origin: center bottom`（向上、向两侧扩展，不向下压座位）。
- 底部真人座位 `.is-right`：`transform-origin: left center`（向右扩展，不侵入上方底牌）。
- 移动端 `.is-above, .is-right`：`transform-origin: center top`（向下/两侧扩展在预留行内）。

定位仍用独立 `translate`（`-50% 0` / `0 -50%`），缩放只用 `transform`，二者互不覆盖。

## 六、移动端收敛

- `≤720px`：`.seat-slot` 预留 `padding-bottom: 34px`；初始缩放收敛到 `neutral 1.20 / call 1.25 / raise 1.30`；
  `transform-origin: center top`；`max-width: 100%` 防越出网格列。
- 移动端长金额（如「加注到 123,456」）验证不横向溢出。

## 七、reduced-motion 降级

保持静态降级（`.action-popover { animation: none !important; opacity: 1; transform: none; }`）：
无缩放、无位移、正常大小立即显示、**保留动作颜色/光晕**，仍由 React timer 在 1600ms 后移除；
不因动画被压到 0.01ms 而落到 `opacity:0` 结束帧。

## 八、验证

### 1) 类型检查与构建

命令：`npm --prefix web run typecheck`、`npm --prefix web run build`

```
=== npm --prefix web run typecheck ===

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

EXIT=0

=== npm --prefix web run build ===

> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
transforming...
✓ 44 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-BnevG_Uv.css   30.56 kB │ gzip:  7.73 kB
dist/assets/index-C7tQMy6F.js   269.78 kB │ gzip: 85.15 kB
✓ built in 996ms
EXIT=0
```

### 2) fixture 浏览器（Playwright，非真实 Go 链路）

命令：`cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts action-popover-scale`

```
Running 3 tests using 1 worker

  ✓  1 [chromium] › fixture/tests/action-popover-scale.spec.ts:139:1 › 桌面：动作分档 scale + 缩放回弹 + transform-origin (6.4s)
  ✓  2 [chromium] › fixture/tests/action-popover-scale.spec.ts:209:1 › 移动端 390：scale 收敛 1.20–1.30、origin center top、不溢出 (3.8s)
  ✓  3 [chromium] › fixture/tests/action-popover-scale.spec.ts:244:1 › reduced-motion：静态可见、无缩放、保留动作颜色 (3.0s)

  3 passed (14.3s)
```

### 3) 场景逐条对齐（场景 → 测试 → 结果）

| 需求场景 | 测试 | 关键断言 | 结果 |
| --- | --- | --- | --- |
| fold/check scale 1.20 | 「桌面：动作分档…」 | `is-neutral`、`--pop-scale≈1.20`；30ms 帧 scale≈1.2 | ✅ |
| call scale 1.40 | 同上 | `is-call`、`--pop-scale≈1.40`；30ms 帧 scale≈1.4 | ✅ |
| raise_to scale 1.55 | 同上 | `is-raise`、`--pop-scale≈1.55`；静态光晕 `box-shadow ≠ none` | ✅ |
| 缩放回弹动画 | 同上 | keyframes 含 `scale(var(--pop-scale))` 与 `cubic-bezier(0.2, 0.8, 0.25, 1.12)`；`animationName=action-pop`、`duration=1.6s`；30ms 大尺寸、800ms 回到 1 | ✅ |
| 底部真人 transform-origin | 同上 | `is-right` 且 `originX≈0`、`originY≈height/2`（left center） | ✅ |
| 普通座位 transform-origin | 同上 | `is-above` 且 `originX≈width/2`、`originY≈height`（center bottom） | ✅ |
| 移动端 scale 收敛 + 不溢出 | 「移动端 390…」 | `1.20≤scale≤1.30`；`origin=(width/2, 0)`（center top）；`scrollWidth-innerWidth ≤ 1`；长金额「加注到 123,456」不越列 | ✅ |
| reduced-motion 静态可见 | 「reduced-motion…」 | `animationName=none`、`transform=none`、`opacity>0.9`、保留 `is-raise` 颜色 `rgb(246,231,184)`；800ms 仍在、随后移除 | ✅ |

### 4) 回归

命令：`cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts`
结果：`26 passed (2.1m)`（含上一轮 4 条 popover 场景 + 本轮 3 条 scale 场景 + 既有 action-dock / human-player /
quick-bet-keys / table-layout 全过；`transform-origin`/缩放未破坏既有布局与不重叠断言）。

### 5) 截图证据（`docs/action-popover/`）

- `scale-desktop-raise.png`：桌面真人局，加注档 1.55 + 静态光晕，底部真人 `is-right`。
- `scale-mobile-raise.png`：移动端 390，收敛比例 + 长金额不越列。
- `scale-reduced-motion.png`：reduced-motion 静态可见、保留颜色。

## 九、未验证边界（如实说明）

- **真实 Go 联调未做**：均为本地 fixture + `page.route` 注入，实际观感/节奏需 `make run` 后目视确认。
- **缩放分档未覆盖全押独立档**：全押无独立 `ActionKind`，仍按 `raise_to`/`call` 处理；如需单独更醒目档位需后端新增字段（第一版不做）。
- **回弹观感**：仅用 Web Animations API 定格 30ms/800ms 帧验证 scale 数值轨迹，未逐帧比对主观「回弹幅度」；如需更强回弹可调 `cubic-bezier` 的 y 超调值。
- **`--pop-scale` 依赖 CSS 自定义属性**：现代 Chromium 已验证；未覆盖旧版浏览器。
- 顶层座位标签放大后向 stage 上方扩展的极端视觉叠压未逐一目视核对（不产生横向滚动，已验证 `overflow ≤ 1`）。

---

# 第二轮调整：大尺寸拉长 + 慢速缩回（2026-10-04）

状态：前端实现完成，fixture 浏览器验证通过；**真实 Go 联调未做**，实际观感需 `make run` 后目视确认。未提交、未推送。
用户反馈第一版「大尺寸只闪现一下就缩回、特效不明显、太快」。本轮目标：**开头大尺寸明显可见并停留，再慢速缩放回去**。

## 一、根因

第一版 keyframes 里大尺寸（`scale 1.40/1.55`）只在 `0–60ms`，`60–300ms` 就缩回 1：
大尺寸阶段太短、缩回太快，视觉上等同「弹一下就没」。

## 二、新时间轴（总 2400ms）

| 时间 | 百分比 | 行为 |
| --- | --- | --- |
| 0–100ms | 0–4.17% | `opacity 0.2→1`，`scale` 保持分档放大值（尽快可见） |
| 100–250ms | 4.17–10.42% | **大尺寸停留**（`scale` 分档值、`opacity 1`） |
| 250–720ms | 10.42–30% | **慢速缩回**：分档值 → `0.97`（约 470ms，`cubic-bezier(0.45, 0.05, 0.55, 0.95)`） |
| 720–800ms | 30–33.33% | **回弹落回**：`0.97 → 1`（约 80ms，`cubic-bezier(0.25, 0.6, 0.5, 1)`） |
| 800–2100ms | 33.33–87.5% | 停留 `scale 1` |
| 2100–2400ms | 87.5–100% | 只淡出，不再缩放/位移 |

要点：
- 大尺寸阶段从 60ms 拉长到 **250ms**，且 `opacity` 在约 100ms 内到 1，保证「放大特效」看得清。
- 缩回从 240ms 拉长到 **550ms**，且拆成两段：先平缓缩到略小于 1（`0.97`）再回弹落回 1，既有「慢」又有「回弹感」。
  - 说明：单条 `cubic-bezier` 在 `y1` 较小时无法同时做到「中点≈50% + 明显回弹」（回弹量在 x→1 处被压到约 1.004），故改用两段 keyframe 显式制造回弹。
- 淡出仍是最后 300ms 只改 `opacity`。

新 keyframes：

```css
@keyframes action-pop {
  0% { opacity: 0.2; transform: scale(var(--pop-scale)); animation-timing-function: linear; }
  4.17% { opacity: 1; transform: scale(var(--pop-scale)); animation-timing-function: linear; }
  10.42% { opacity: 1; transform: scale(var(--pop-scale)); animation-timing-function: cubic-bezier(0.45, 0.05, 0.55, 0.95); }
  30% { opacity: 1; transform: scale(0.97); animation-timing-function: cubic-bezier(0.25, 0.6, 0.5, 1); }
  33.33% { opacity: 1; transform: scale(1); animation-timing-function: linear; }
  87.5% { opacity: 1; transform: scale(1); animation-timing-function: linear; }
  100% { opacity: 0; transform: scale(1); }
}
```

动画声明：`.action-popover { animation: action-pop 2400ms linear both; }`（区间缓动由 keyframe 内的 `animation-timing-function` 控制）。

## 三、调整后的 scale 分档与时长常量

| kind | 强度 class | 桌面初始 scale（原→新） | 移动端 scale |
| --- | --- | --- | --- |
| `fold` / `check` | `is-neutral` | 1.20 → **1.30** | 1.20（不变） |
| `call` | `is-call` | 1.40 → **1.55** | 1.25（不变） |
| `raise_to` | `is-raise` | 1.55 → **1.75** | 1.30（不变） |

- `HINT_DURATION_MS`：**1600 → 2400**（`web/src/components/ActionPopover.tsx`），与 CSS `animation` 时长同步（改一个必须改另一个）。
- 移动端（≤720px）仍收敛到 1.20–1.30，`transform-origin: center top`，不溢出。
- reduced-motion 降级不变：`animation: none`、`opacity: 1`、`transform: none`，静态可见，由 React timer 在 2400ms 后移除。

## 四、改动文件（本轮）

修改实现（独占写范围）：
- `web/src/components/ActionPopover.tsx`：`HINT_DURATION_MS` 1600→2400；更新注释。
- `web/src/styles.css`：`--pop-scale` 分档 1.30/1.55/1.75；`animation` 2400ms；`@keyframes action-pop` 改为四段两段缩回；注释同步。

修改测试（经用户确认后允许）：
- `tests/e2e/browser/fixture/tests/action-popover-scale.spec.ts`：断言更新到新契约（2.4s、1.30/1.55/1.75、`scale(0.97)` 回弹、250ms 保持大尺寸、500ms 缩回中途、720ms 回弹、800ms 回 1），并新增 call 档截图。

未改（按要求）：`SpectatorView.tsx`（去重/timer 逻辑不动，仅消费 `HINT_DURATION_MS`）、`PokerTable.tsx`、`format.ts`、`types.ts`、Go、scripts、config。

## 五、验证

前置：fixture 服务的是 `web/dist` 构建产物，故每次改 CSS 后先 `npm --prefix web run build` 再跑 fixture。

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
transforming...
✓ 44 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-D7a4QoCv.css   30.73 kB │ gzip:  7.76 kB
dist/assets/index-Bmh9bsBR.js   269.78 kB │ gzip: 85.15 kB
✓ built in 678ms
EXIT=0
```

### 2) fixture 浏览器（Playwright，非真实 Go 链路）

命令：`cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts action-popover-scale`

```
Running 3 tests using 1 worker

  ✓  1 [chromium] › fixture/tests/action-popover-scale.spec.ts:139:1 › 桌面：动作分档 scale + 缩放回弹 + transform-origin (6.1s)
  ✓  2 [chromium] › fixture/tests/action-popover-scale.spec.ts:221:1 › 移动端 390：scale 收敛 1.20–1.30、origin center top、不溢出 (3.6s)
  ✓  3 [chromium] › fixture/tests/action-popover-scale.spec.ts:256:1 › reduced-motion：静态可见、无缩放、保留动作颜色 (3.9s)

  3 passed (14.2s)
```

回归（全量 fixture）：`cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts`

```
Running 26 tests using 1 worker
...（含 action-dock / action-popover / action-popover-scale / human-player / quick-bet-keys / table-layout）
  26 passed (2.0m)
```

### 3) 场景逐条对齐（场景 → 测试 → 结果）

| 需求场景 | 测试 | 关键断言 | 结果 |
| --- | --- | --- | --- |
| 大尺寸阶段拉长（前 250ms 保持分档值） | 「桌面：动作分档…」 | `scaleAt(30/100/200ms) ≈ 分档值`；200ms 仍 `1.55`（call） | ✅ |
| 缩回变慢 | 同上 | `scaleAt(500ms)` 严格介于 1 与分档值之间（call≈1.23、neutral≈1.12） | ✅ |
| 回弹落回 | 同上 | `scaleAt(720ms) ≈ 0.97`，`scaleAt(800ms) ≈ 1` | ✅ |
| scale 分档 | 同上 | `--pop-scale` = 1.30 / 1.55 / 1.75 | ✅ |
| 总时长同步 | 同上 | `animationDuration = '2.4s'`（与 `HINT_DURATION_MS=2400` 一致） | ✅ |
| transform-origin 分派 | 同上 | 普通座位 center bottom、真人座位 left center | ✅ |
| 移动端收敛 + 不溢出 | 「移动端 390…」 | `1.20≤scale≤1.30`、`origin=(w/2, 0)`、`scrollWidth-innerWidth ≤ 1` | ✅ |
| reduced-motion 静态可见 | 「reduced-motion…」 | `animationName=none`、`transform=none`、`opacity>0.9`、保留 `is-raise` 颜色；2400ms 后移除 | ✅ |
| 多座位连续动作不遮挡 | 「人数模板 2/6/9…」（全量回归） | 2/6/9 座并存两标签：不压中央、互不重叠、无横向溢出 | ✅ |

### 4) 截图证据（`docs/action-popover/`）

- `scale-desktop-call.png`：桌面真人局，call 档（定格 200ms，`scale≈1.55`）。
- `scale-desktop-raise.png`：桌面真人局，raise 档（`scale≈1.75` + 静态光晕），底部真人 `is-right`。
- `scale-mobile-raise.png`：移动端 390，收敛比例（≤1.30）+ 长金额不越列。
- `scale-reduced-motion.png`：reduced-motion 静态可见、保留颜色。

## 六、未验证边界（如实说明）

- **真实 Go 联调未做**：均为本地 fixture + `page.route` 注入；「大尺寸停留 250ms、慢速缩回 550ms」的实际手感需 `make run` 后目视确认。
- **回弹幅度为主观参数**：`0.97` 的欠冲与两段贝塞尔由数值轨迹（`scaleAt` 定格帧）验证，未做逐帧主观比对；如需更强/更弱回弹，调 `30%` 的 `scale(0.97)` 与两段贝塞尔即可。
- **多座位快速连发遮挡**：真实牌局相邻动作间隔通常数秒，标签几乎不共存；fixture 已测 2/6/9 座并存两标签不重叠。若真实 9 人局极短间隔连发导致大尺寸阶段（250ms）邻座短暂叠压，可折中：把大尺寸停留略缩（如 250→200ms）但**缩回仍保持慢速**——本轮未观测到该情形，故未缩短。
- **`--pop-scale` 依赖 CSS 自定义属性**：现代 Chromium 已验证；未覆盖旧版浏览器。
- 顶层座位标签放大后向 stage 上方扩展的极端视觉叠压未逐一目视核对（不产生横向滚动，已验证 `overflow ≤ 1`）。
