# 下注徽章移至座位外侧（arc-8 方案）— 实现与验证

> 纯前端改动。回退点 `85080f6`。不 commit / push（由用户看效果决定是否保留）。
> **本文档所有断言均来自 fixture 浏览器验证；未做真实 Go 联调（fixture 不能作为真实链路证据）。**

## 1. 目标

把「本轮 X / 累计 Y」下注徽章从牌桌内部（旧 `.rim-marks > .rim-mark` 层）移到**座位外侧**，
成为每个座位自己的「附属条」：与 `<Seat>` 同级挂在 `.seat-slot` 下，紧贴座位、方向表达归属。

## 2. 改动文件

| 文件 | 说明 |
|---|---|
| `web/src/components/PokerTable.tsx` | 方向分派、几何/包络、把徽章渲染进 `.seat-slot`、移除旧 `.rim-marks` 层 |
| `web/src/styles.css` | `.rim-mark` 附属条新样式（4 方向）、`.rail` 外围带、`.stage.is-list` 提前退化 |
| `tests/e2e/browser/fixture/tests/rim-badge.spec.ts` | 新增：方向分派/桌沿外/不遮挡/相邻不叠压/最长金额/窄桌面/移动端 |
| `tests/e2e/browser/fixture/tests/table-layout.spec.ts` | 同步契约：牌桌宽度阈值、徽章标题/注释、移动端 `.rim-mark` 隐藏 |

## 3. 方向分派（按槽位 dx/dy，不按像素角度）

`web/src/components/PokerTable.tsx:105`

```ts
type BarDir = 'up' | 'down' | 'left' | 'right'
function barDir(s: Slot): BarDir {
  if (Math.abs(s.dx) > Math.abs(s.dy) * 1.25) return s.dx < 0 ? 'left' : 'right'
  return s.dy < 0 ? 'up' : 'down'
}
```

- 规则：`|dx| > 1.25×|dy|` → 左/右（取 dx 符号）；否则 → 上/下（取 dy 符号）；**临界优先上下**。
- 正上/左上/右上 → 上；正左/左 → 左；正右/右 → 右；左下/右下/正下 → 下。
- 方向**只由槽位决定**（`PokerTable.tsx:269` `const dir = barDir(slot)`），不随金额/行动状态变化。
- 9 人（真人 #4，ring 从 #4 顺时针 B/L2L/L2U/T4L/T4ML/T4MR/T4R/R2U/R2L）实测方向：
  `4:down, 5:left, 6:left, 7:up, 8:up, 0:up, 1:up, 2:right, 3:right`（见第 7 节测试输出）。

## 4. 徽章新定位算法

- 结构：`.seat-slot`（`position:absolute; transform:translate(-50%,-50%)`）内，徽章与 `<Seat>` 同级，
  `PokerTable.tsx:367`：
  ```tsx
  <div className={`rim-mark is-${dir}`} data-seat={seat.seat} data-dir={dir} aria-hidden="true">
  ```
  不放进 `.seat`，因此不被 `is-dimmed`/`is-folded` 的透明度影响。
- 定位纯 CSS（相对座位卡盒，`padding-box = 140×58`），不依赖像素角度、不做动态左右挪移：
  - `styles.css:1070` `is-up`：`left:50%; bottom:calc(100% + 19px); translateX(-50%)`
  - `styles.css:1075` `is-down`：`left:50%; top:calc(100% + 21px); translateX(-50%)`
  - `styles.css:1097` `is-left`：`top:50%; right:calc(100% + 21px); translateY(-50%)`
  - `styles.css:1100` `is-right`：`top:50%; left:calc(100% + 21px); translateY(-50%)`
  - 19/21px 间隙分别越过「行动状态标签」上(`-15px`)/下(`-17px`)的独立位置；左右 21px = 牌背外探 ~14px + 余量。

## 5. 样式改动（附属条，不再是黑色胶囊）

`styles.css:1041`：浅圆角弱边框、无筹码图标、无金边胶囊。

```css
.rim-mark { position:absolute; z-index:5; display:inline-flex; align-items:baseline; gap:5px;
  padding:3px 9px; border-radius:9px; background:rgba(10,16,14,0.72);
  border:1px solid rgba(201,164,76,0.26); ... animation: rim-bar-in .28s ... both; }
.rim-mark .rim-now { color:var(--gold-soft); font-weight:600; }   /* 本轮：一级强调 */
.rim-mark .rim-cum { color:var(--muted); font-size:10px; }         /* 累计：次级但可读 */
```

- 上/下方向：一行（`styles.css:1081` 用 `·` 分隔）；左/右方向：两行、`min-width:84px`（趋近 80-100px）。
- 入场动画 `@keyframes rim-bar-in` 只动 opacity（`styles.css:1539` 区），不再改 `transform`，避免破坏方向锚定。
- 层级：`.rim-mark` z-index 5 < 行动状态标签 `turn-badge` 50 < 动作特效 `action-popover` 60，
  故下注条**不会遮住**行动标签/动作特效。

## 6. 外围留白预算（实测，非硬编码死值）

- 常量（`PokerTable.tsx:88-98`）：`ENVELOPE_SIDE=131`、`ENVELOPE_VERT=54`、
  `SLACK_X_MAX=110`、`TOP_GAP_MIN=160`。
- 椭圆半径把「座位卡+底牌+下注条+行动标签」组合包络算进去（`PokerTable.tsx:208-225`）：
  ```ts
  const HALF_X = SEAT_HALF_W + ENVELOPE_SIDE   // 自座位中心到外侧条外缘
  const HALF_Y = SEAT_HALF_H + ENVELOPE_VERT
  const slackX = Math.min(stageSize.slackX, SLACK_X_MAX) // 实测 stage→视口左右余量
  const RX = clamp(((W/2 - HALF_X + slackX) / W) * 100, 26, 47)
  const RY = clamp(((H/2 - HALF_Y + MARGIN_Y) / H) * 100, 26, 45)
  ```
- `slackX` 由 `getBoundingClientRect` 实测（`PokerTable.tsx:116-131`），把外侧条放进容器留白，
  而不是把 100px 写死。
- 牌桌（`.rail`）内收为 `inset: 12% 11%`（`styles.css:526`），让出上/下/左/右外围带承载座位+条；
  实测 1536×1024：stage 1334×734、felt 1012×528（旧版 felt≈1187 宽），牌桌按预期收窄以换取外侧空间。

## 7. 移动端 / 窄桌面退化

- **移动端 ≤720px**：行为不变——`.rim-mark` 隐藏，下注信息回落到 `Seat` 卡内 `.bet`（单列网格）。
- **窄桌面退化**：由实测 stage 宽度判定，给 `.stage` 加 `.is-list`（`PokerTable.tsx:223`）：
  ```ts
  const isList = W > 0 && (TOP_NEIGHBOR_DX * RXpx < TOP_GAP_MIN || W < LIST_MIN_STAGE_W)
  ```
  即顶部 4 座相邻间距放不下一行条（含最长金额）或 stage 过窄时，**提前切列表布局**（不再靠缩字号/动态挪徽章）。
  同时 `is-tight`（stage<1000）仅收内边距/圆角，不缩字号（`styles.css:1104`）。
- 列表布局规则改为 `.stage.is-list` 作用域（`styles.css:1997`），stage 外层的操作条/次要面板用
  `.play-layout:has(.stage.is-list)` / `.container:has(.stage.is-list)` 关联。
- **重要修复**：判定不能反过来改变 stage 宽度，否则「列表判定→宽度变化→判定反转」会抖动；
  已移除 `.play-layout` 的 `max-width:none` 覆盖，实测 1280×720 稳定为列表、不再振荡。

## 8. 验证结果

### 8.1 typecheck

```
$ npm --prefix web run typecheck

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

```

### 8.2 build

```
$ npm --prefix web run build

> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
transforming...
✓ 50 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-CKHRXGU8.css   39.19 kB │ gzip:  9.29 kB
dist/assets/index-klyLNgpZ.js   281.56 kB │ gzip: 89.40 kB
✓ built in 719ms
```

### 8.3 外侧徽章专项（fixture）

```
$ cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts rim-badge.spec.ts

Running 5 tests using 1 worker

[rim] stage=1334.40625x733.921875 felt=1012x528 bars=9
[rim] seat=0 dir=up box=857,164 129x23
[rim] seat=1 dir=up box=1067,193 129x23
[rim] seat=2 dir=right box=1369,383 84x37
[rim] seat=3 dir=right box=1415,625 84x37
[rim] seat=4 dir=down box=698,861 141x23
[rim] seat=5 dir=left box=37,625 84x37
[rim] seat=6 dir=left box=83,383 84x37
[rim] seat=7 dir=up box=335,193 141x23
[rim] seat=8 dir=up box=545,164 141x23
  ✓  1 [chromium] › fixture/tests/rim-badge.spec.ts:108:1 › 9 人（真人 #4）：方向分派正确、外侧条在桌沿外、不遮中央/底牌/座位卡/行动标签、相邻不叠压 (3.9s)
  ✓  2 [chromium] › fixture/tests/rim-badge.spec.ts:141:1 › 9 人（真人 #0）：顶部 4 座方向同上、外侧条不叠压、不溢出 (836ms)
  ✓  3 [chromium] › fixture/tests/rim-badge.spec.ts:155:1 › 最长金额：整条仍在桌沿外、不产生横向滚动 (837ms)
  ✓  4 [chromium] › fixture/tests/rim-badge.spec.ts:169:1 › 窄桌面 900x800：空间不足提前切列表布局，外侧条隐藏、下注信息回卡内 .bet、无横向滚动 (486ms)
  ✓  5 [chromium] › fixture/tests/rim-badge.spec.ts:186:1 › 移动端 390：外侧条隐藏，下注信息在座位卡内 .bet，无横向溢出 (371ms)

  5 passed (7.0s)
```

场景 → 测试函数 → 结果：

| 场景 | 测试 | 结果 |
|---|---|---|
| 9 人方向分派 + 桌沿外 + 不遮中央/底牌/座位卡/行动标签 + 相邻不叠压（真人 #4） | `rim-badge.spec.ts:108 9 人（真人 #4）…` | ✓ |
| 9 人顶部 4 座（真人 #0）不叠压/不溢出 | `rim-badge.spec.ts:141 9 人（真人 #0）…` | ✓ |
| 最长金额（9,999,999 / 10,000,000）仍桌沿外、无横向滚动 | `rim-badge.spec.ts:155 最长金额…` | ✓ |
| 窄桌面 900×800 提前切列表、外侧条隐藏、`.bet` 可见、无横滚 | `rim-badge.spec.ts:169 窄桌面 900x800…` | ✓ |
| 移动端 390 外侧条隐藏、`.bet` 可见、无横滚 | `rim-badge.spec.ts:186 移动端 390…` | ✓ |

### 8.4 完整 fixture 套件（回归）

```
$ cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts

Running 46 tests using 1 worker

[dock] desktop height=139
  ✓   1 … action-dock.spec.ts:41:1 › 桌面 · 三行紧凑结构、总高下降、无重复信息、按钮≥40px (3.6s)
[dock] thinking height=54
  ✓   2 … action-dock.spec.ts:94:1 › 桌面 · 非我行动（思考中）时操作区仅剩状态行 (3.5s)
[dock] mobile height=202
  ✓   3 … action-dock.spec.ts:109:1 › 移动端 390 · 各行堆叠、触控目标≥40px、不溢出 (3.2s)
  ✓   4 … action-popover-scale.spec.ts:150:1 › 桌面：四种动作统一 scale 1.8 / 2200ms / 四色 (6.0s)
  ✓   5 … action-popover-scale.spec.ts:216:1 › 桌面：精确时间轴（80/320/620/920/1980ms） (2.7s)
  ✓   6 … action-popover-scale.spec.ts:247:1 › 桌面：展开方向 transform-origin（左半/右半/顶部/真人） (6.0s)
  ✓   7 … action-popover-scale.spec.ts:288:1 › 移动端 390：scale 仍 1.8、不横向溢出 (2.3s)
  ✓   8 … action-popover-scale.spec.ts:311:1 › reduced-motion：静态高对比展示约 2200ms (4.5s)
  ✓   9 … action-popover.spec.ts:131:1 › 去重触发 + 同座位 FIFO 不截断 + 不同座位并存 + 跨手不清 + 弃牌不变暗 + 标签定位 (11.7s)
  ✓  10 … action-popover.spec.ts:218:1 › reduced-motion：标签降级为静态可见，随后由 timer 移除 (5.4s)
  ✓  11 … action-popover.spec.ts:247:1 › 人数模板 2/6/9（全 AI）：标签不压中央、不互相重叠、无横向溢出 (7.3s)
  ✓  12 … action-popover.spec.ts:294:1 › 移动端 390：标签落在预留行、不压 .bet、不溢出 (3.6s)
  ✓  13 … human-player.spec.ts:19:1 › 全 AI 创建走公共观战，不出现真人行动面板 (351ms)
  ✓  14 … human-player.spec.ts:31:1 › 真人座位：配置页隐藏模型下拉，创建后进入真人视角 (3.1s)
  ✓  15 … human-player.spec.ts:44:1 › 轮到真人：座位光圈与「轮到我」、底部倒计时进度条与操作按钮组 (3.2s)
  ✓  16 … human-player.spec.ts:76:1 › AI 轮转：底部显示「思考中…」与不确定进度，行动历史高亮新动作 (3.2s)
  ✓  17 … human-player.spec.ts:96:1 › 无 Cookie 浏览器：人类牌局仅公共观战，看不到真人底牌与行动面板 (267ms)
  ✓  18 … human-player.spec.ts:123:1 › 移动端 390 宽：轮转提示与底部操作区可点无遮挡 (3.2s)
  ✓  19 … model-label.spec.ts:52:1 › 桌面：模型短名上移第一行、第二行剩余不换行、未知模型不留孤立括号、卡不增高 (582ms)
  ✓  20 … model-label.spec.ts:102:1 › 移动端 ≤720px：模型短名 11px / max-width 120px、不溢出 (3.2s)
  ✓  21 … quick-bet-keys.spec.ts:26:1 › 快捷注：六个比例按钮渲染，目标值按基准池 clamp，点击填入输入框 (3.2s)
  ✓  22 … quick-bet-keys.spec.ts:57:1 › 快捷键 F：直接弃牌并收起操作面板 (3.2s)
  ✓  23 … quick-bet-keys.spec.ts:66:1 › 快捷键 R 聚焦加注输入框；输入框聚焦时 F/C/K 不误触 (3.1s)
  ✓  24 … quick-bet-keys.spec.ts:87:1 › 键盘 R + 输入 + 回车：提交加注动作 (3.1s)
  ✓  25 … quick-bet-keys.spec.ts:100:1 › 移动端 390 宽：快捷注按钮可见且不超出视口 (3.1s)
[rim] stage=1334.40625x733.921875 felt=1012x528 bars=9
[rim] seat=0 dir=up box=857,164 129x23
[rim] seat=1 dir=up box=1067,193 129x23
[rim] seat=2 dir=right box=1369,383 84x37
[rim] seat=3 dir=right box=1415,625 84x37
[rim] seat=4 dir=down box=698,861 141x23
[rim] seat=5 dir=left box=37,625 84x37
[rim] seat=6 dir=left box=83,383 84x37
[rim] seat=7 dir=up box=335,193 141x23
[rim] seat=8 dir=up box=545,164 141x23
  ✓  26 … rim-badge.spec.ts:108:1 › 9 人（真人 #4）：方向分派正确、外侧条在桌沿外、不遮中央/底牌/座位卡/行动标签、相邻不叠压 (3.6s)
  ✓  27 … rim-badge.spec.ts:141:1 › 9 人（真人 #0）：顶部 4 座方向同上、外侧条不叠压、不溢出 (815ms)
  ✓  28 … rim-badge.spec.ts:155:1 › 最长金额：整条仍在桌沿外、不产生横向滚动 (784ms)
  ✓  29 … rim-badge.spec.ts:169:1 › 窄桌面 900x800：空间不足提前切列表布局，外侧条隐藏、下注信息回卡内 .bet、无横向滚动 (464ms)
  ✓  30 … rim-badge.spec.ts:186:1 › 移动端 390：外侧条隐藏，下注信息在座位卡内 .bet、无横向溢出 (385ms)
  ✓  31 … settlement.spec.ts:134:1 › 摊牌结算：参与者翻牌、弃牌者不翻、赢家金额=payouts、结算条主池命名、快照 (3.0s)
  ✓  32 … settlement.spec.ts:194:1 › 弃牌结束：不翻牌、不显示牌型、显示「其余玩家弃牌」、退回展示 (1.8s)
  ✓  33 … settlement.spec.ts:234:1 › 真人赢：标签「你赢了 +X」、只赢边池明确提示、冰蓝身份保留 (3.5s)
  ✓  34 … settlement.spec.ts:299:1 › 首次进入不重播历史手 (2.8s)
  ✓  35 … settlement.spec.ts:318:1 › reduced-motion：结算静态可见（金边/标签/牌面），不依赖动画 (1.8s)
  ✓  36 … settlement.spec.ts:354:1 › 移动端 390：结算条在牌桌下方、可展开、不遮挡操作区 (1.6s)
  ✓  37 … street-track.spec.ts:73:1 › 五节点轨道：当前街最亮、过去小勾、未来空心、当前注文案 (2.6s)
  ✓  38 … street-track.spec.ts:107:1 › 切街临时提示 1.5s（flop→turn），随后恢复常态 (4.2s)
  ✓  39 … street-track.spec.ts:126:1 › 跳街（preflop→river）不伪造中间切街提示 (3.5s)
  ✓  40 … street-track.spec.ts:146:1 › 暂停：状态行显示「牌局已暂停 · 本街尚未结束」 (488ms)
  ✓  41 … street-track.spec.ts:161:1 › 停止：不把中止画成正常结束 (503ms)
  ✓  42 … table-layout.spec.ts:94:1 › 人数模板 2-9（真人局）：不重叠、不侵入中央、真人底部居中、两行固定 (26.0s)
[layout] stage=1334x734 felt=1012x528
  ✓  43 … table-layout.spec.ts:137:1 › 桌面 · 真人 9 座：底部居中、中央清爽、座位卡两行、牌桌占据主要区域 (3.5s)
  ✓  44 … table-layout.spec.ts:164:1 › 桌面 · 全 AI 9 座观战：环绕不重叠、中央清爽 (677ms)
  ✓  45 … table-layout.spec.ts:178:1 › 座位外侧下注条：本街下注（本轮 + 累计）在桌沿外，不压中央 (661ms)
  ✓  46 … table-layout.spec.ts:211:1 › 移动端 390：座位可点、不溢出、操作条在视口内 (3.2s)

  46 passed (2.7m)
```

## 9. 截图证据

`docs/rim-badge/`（由 `rim-badge.spec.ts` 落盘）：

| 文件 | 场景 |
|---|---|
| `desktop-9-bets.png` | 1536×1024 9 人（真人 #4），9 条外侧徽章 |
| `desktop-9-human0.png` | 1536×1024 9 人（真人 #0），顶部 4 座 |
| `desktop-max-amount.png` | 1536×1024 最长金额 |
| `narrow-900.png` | 900×800 窄桌面（已切列表布局） |
| `mobile-390.png` | 390×844 移动端（卡内 `.bet`） |

## 10. 未验证边界 / 已知冲突与折中

- **真实 Go 联调未做**：以上均为本地 fixture（内存服务 + `web/dist`），不是真实后端链路证据。
- **动作特效（action-popover）与上/下方向徽章的瞬态重叠**（不可避免）：
  动作特效是 z-index 60 的瞬态层、直接位于座位上/下方展开（如 `above-center` 在座位正上方），
  而上/下方向徽章也在座位正上/下方。两者几何上会在特效播放期（2200ms）重合；
  已通过层序保证**徽章在特效之下**（徽章 5 < 特效 60），即徽章不会遮挡特效，特效短暂盖住徽章属预期。
  未强行把徽章推离（会让外围预算失控、且违反「紧贴座位」）。
- **`turn-badge`（行动状态标签）已规避**：徽章与标签分别位于座位卡上/下两侧的独立间隙，测试断言零重叠。
- **窄桌面判定基于 stage 实测宽度**：极端「很宽但很矮」的窗口可能提前进入列表布局（空间确实不足），
  属预期退化；阈值 `TOP_GAP_MIN=160` 以常见最长金额为准，超长金额若在临界宽度可能更早退化。
- **`tests/e2e/browser/chip-badge/`（真实链路徽章验收）未同步**：其断言基于旧「护栏内侧」契约，
  不在本次独占写范围内，运行会失败；需后续单独同步（或由用户决定是否保留旧契约）。
- **`tests/e2e/browser/*`（真实 Go 链路）未运行**：未做真实宿主验证。
- 回退点 `85080f6` 可用；本次改动未 commit。
