# 底部操作区精致化（Action Dock Refine）

用户反馈：`~/Desktop/ui3.png` 显示底部操作区约占可视高度 **31%**，过大。按两位视觉 agent 的共识，
把桌面操作区压缩成紧凑三行、删除重复信息、精致化配色与圆角；移动端回落为各行堆叠且不缩小触控目标。
**只改前端布局/样式，交互语义与后端契约不变。**

## 1. 改动文件

| 文件 | 改动 |
| --- | --- |
| `web/src/components/ActionDock.tsx` | 状态行合并（去掉冗余「你」）；进度条并入状态行底部（不再独占一行） |
| `web/src/components/HumanActionPanel.tsx` | 删除「真人 · 座位 X」行；直接动作 + 加注组合并到同一「操作行」；删除与占位重复的「范围」文案，改为常驻短提示 `.raise-range`；快捷注移出加注组、自成一行 |
| `web/src/styles.css` | 三行结构尺寸、容器/按钮圆角、配色（金只留确认按钮）、细进度线、快捷键降权、移动端堆叠 |
| `web/src/hooks/useCountdown.ts` | **未改动**（倒计时逻辑/格式保持原样，等宽与去抖动由 CSS 承担） |
| `tests/e2e/browser/fixture/tests/action-dock.spec.ts` | 新增操作区验收 + 截图 |
| `docs/ACTION_DOCK_REFINE.md`、`docs/action-dock/*` | 本文档与命令日志、截图 |

未触碰 Go、scripts、config、`docs/contracts.md`、Makefile、`PokerTable.tsx`、`Seat.tsx`。

## 2. 三行结构 DOM

`ActionDock`（状态行，含进度线）+ `HumanActionPanel`（操作行 + 快捷注行）：

```
section.hand-dock[data-turn]
├─ div.dock-turn                     ← 状态行（~24px + 4px 给进度线）
│  ├─ span.turn-orb                  ← 小金点（me）
│  ├─ span.turn-who                  ← 仅非本人行动时显示行动者名（本人行动时省略，避免「你」冗余）
│  ├─ span.dock-state                ← 文本不变：'轮到你出牌' / '思考中…' / '牌局已暂停'（既有测试依赖）
│  ├─ span.countdown                 ← 右侧等宽数字，min-width 固定防跳变
│  └─ div.dock-timer(role=progressbar) └─ div.dock-timer-fill   ← 3px 细线，绝对定位并入状态行底部
└─ HumanActionPanel
   └─ section.action-panel.is-turn
      └─ div.action-body
         ├─ div.action-main           ← 操作行（40px）
         │  ├─ div.action-buttons     ← 弃牌 / 过牌 / 跟注金额 /（无加注权时）全押
         │  └─ div.raise-control      ← input#raise-to + 加注按钮 + span.raise-range（常驻短提示）
         └─ div.quick-bets            ← 快捷注行（40px）：6 个 .quick-bet[data-quick]，比例 + 金额两层
```

- `action-main` 用 `justify-content: space-between` + `gap:14px`，直接动作与加注组之间保证 ≥14px 安全间距防误点。
- 进度条从「独立一行」改为 `.dock-turn` 内 3px 绝对定位细线。

## 3. 尺寸改动

| 项 | 旧 | 新（桌面） |
| --- | --- | --- |
| `.hand-dock` padding / radius | 10/14/12，`var(--radius)` 16 | **7/12/8，14px** |
| `.dock-turn` min-height / padding-bottom | 无（flex wrap） | **24px / 4px**（容纳 3px 进度线） |
| `.dock-timer` | margin-top 8、height 6、独立行 | **绝对定位、height 3、并入状态行** |
| `.countdown` | 22px / min-width 64 | **17px / min-width 52 / `tabular-nums`** |
| `.action-body` gap | 8 | **5** |
| `.action-buttons .btn` | `flex:1 1 auto` 各占半屏、min-height 40 | **`flex:0 0 auto` 内容定宽、min-height 40、padding 0 16px** |
| `.raise-control .input` | width 150 | **width 112、右对齐等宽、min-height 40** |
| `.quick-bet` | min-width 52 / min-height 36 | **min-width 56 / min-height 40** |
| `.hand-dock .btn` radius | 999（胶囊） | **11px**（容器 14px） |

**实测总高（1536×1024，真人行动点）**：`desktop height=139px`（目标 96-140px ✅）。
非本人行动（思考中）仅状态行：`height=54px`。移动端 390：`height=202px`（目标 168-204px ✅）。

## 4. 删除的重复信息

1. **「真人 · 座位 0」整行**（`.action-who`）：本人座位卡已有「真人」身份标记，删除。
2. **加注「范围 100-9,367」文案**：与输入框 placeholder 重复。改为一个**常驻短提示** `.raise-range`
   （`200–10,000`，紧邻输入框/按钮），并移除输入框内重复的 range placeholder；输入后提示仍可见（不依赖 placeholder）。
3. 状态行冗余的「你」：本人行动时 `.dock-state` 本身即第二人称「轮到你出牌」，不再另起 `.turn-who`。

> 兼容性：`.dock-state` 文本、`.countdown`、`.dock-timer`(progressbar，name=「真人行动剩余时间」)、
> `.quick-bets`/`.quick-bet`/`[data-quick]`、`#raise-to`、以及可访问名「加注到（本街累计）」全部保留，
> 既有 fixture 与真实 human/turn E2E 的定位不受影响（见第 6 节回归）。

## 5. 精致化

- **金色只做重点**：确认按钮 `跟注/加注` 保留黄铜金渐变；普通按钮（过牌）深绿黑底 + 细黄铜边；
  弃牌/全押为低饱和红文字 + 细红边。全部限定在 `.hand-dock .btn` 作用域，不影响其他页面按钮。
- **轮到自己**：取消整圈金色光晕，改为轻微边框变化（`border-color: rgba(201,164,76,.5)`）+ 状态行小金色圆点。
- **圆角收敛**：容器 14px、按钮 11px（不再胶囊）。
- **数字等宽去抖**：倒计时、筹码、range 均 `--font-num`，倒计时用 `tabular-nums` + 固定 `min-width`。
- **快捷键降权**：`.hand-dock .keyhint` 降到 9.5px、15px 高、`opacity:.6`，作辅助信息。

## 6. 验证命令与原始输出

### 6.1 类型检查

```
$ npm --prefix web run typecheck
> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit
（退出码 0，无输出）
```

### 6.2 构建

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
dist/assets/index-Deg0C6L6.css   28.99 kB │ gzip:  7.41 kB
dist/assets/index-Dd6UHxoq.js   268.05 kB │ gzip: 84.55 kB
✓ built in 1.02s
```

### 6.3 fixture 浏览器验收（全量 19 例，含本轮新增 3 例）

```
$ cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts

Running 19 tests using 1 worker

[dock] desktop height=139
  ✓   1 .../action-dock.spec.ts:41:1 › 桌面 · 三行紧凑结构、总高下降、无重复信息、按钮≥40px (4.1s)
[dock] thinking height=54
  ✓   2 .../action-dock.spec.ts:94:1 › 桌面 · 非我行动（思考中）时操作区仅剩状态行 (3.6s)
[dock] mobile height=202
  ✓   3 .../action-dock.spec.ts:109:1 › 移动端 390 · 各行堆叠、触控目标≥40px、不溢出 (3.3s)
  ✓   4 .../human-player.spec.ts:19:1 › 全 AI 创建走公共观战，不出现真人行动面板 (391ms)
  ✓   5 .../human-player.spec.ts:31:1 › 真人座位：配置页隐藏模型下拉，创建后进入真人视角 (3.2s)
  ✓   6 .../human-player.spec.ts:44:1 › 轮到真人：座位光圈与「轮到我」、底部倒计时进度条与操作按钮组 (3.5s)
  ✓   7 .../human-player.spec.ts:76:1 › AI 轮转：底部显示「思考中…」与不确定进度，行动历史高亮新动作 (3.2s)
  ✓   8 .../human-player.spec.ts:96:1 › 无 Cookie 浏览器：人类牌局仅公共观战，看不到真人底牌与行动面板 (264ms)
  ✓   9 .../human-player.spec.ts:123:1 › 移动端 390 宽：轮转提示与底部操作区可点无遮挡 (3.1s)
  ✓  10 .../quick-bet-keys.spec.ts:26:1 › 快捷注：六个比例按钮渲染，目标值按基准池 clamp，点击填入输入框 (3.3s)
  ✓  11 .../quick-bet-keys.spec.ts:57:1 › 快捷键 F：直接弃牌并收起操作面板 (3.3s)
  ✓  12 .../quick-bet-keys.spec.ts:66:1 › 快捷键 R 聚焦加注输入框；输入框聚焦时 F/C/K 不误触 (3.2s)
  ✓  13 .../quick-bet-keys.spec.ts:87:1 › 键盘 R + 输入 + 回车：提交加注动作 (3.2s)
  ✓  14 .../quick-bet-keys.spec.ts:100:1 › 移动端 390 宽：快捷注按钮可见且不超出视口 (3.1s)
  ✓  15 .../table-layout.spec.ts:94:1 › 人数模板 2-9（真人局）：不重叠、不侵入中央、真人底部居中、两行固定 (26.7s)
[layout] stage=1334x734 felt=1158x624
  ✓  16 .../table-layout.spec.ts:137:1 › 桌面 · 真人 9 座：底部居中、中央清爽、座位卡两行、牌桌更大 (3.7s)
  ✓  17 .../table-layout.spec.ts:163:1 › 桌面 · 全 AI 9 座观战：环绕不重叠、中央清爽 (901ms)
  ✓  18 .../table-layout.spec.ts:177:1 › 桌沿徽章：本街下注（本轮 + 累计）落在护栏内侧，不压中央 (2.0s)
  ✓  19 .../table-layout.spec.ts:210:1 › 移动端 390：座位可点、不溢出、操作条在视口内 (3.2s)

  19 passed (1.3m)
```

（原始日志：`docs/action-dock/log-typecheck.txt`、`log-build.txt`、`log-fixture.txt`）

### 6.4 本轮新增断言覆盖映射

| 要求 | 断言 | 结果 |
| --- | --- | --- |
| 操作区高度显著下降 | `.hand-dock` 高 ≤150 且 ≥96（实测 139；旧版 ~260） | ✅ |
| 三行结构 | `.dock-turn` / `.action-main` / `.quick-bets` 存在且 y 递增；`.dock-turn .dock-timer` = 1 | ✅ |
| 删除重复信息 | `.action-who` = 0；`.raise-hint` = 0；页面无「真人 · 座位」；`.raise-range` 可见 | ✅ |
| 按钮 ≥40px 可点 | `.action-buttons .btn`、`.raise-control .btn`、`.quick-bet` 高度 ≥40 | ✅ |
| 快捷注仍 6 个 | `.quick-bet` count = 6 | ✅ |
| 加注输入可访问名保留 | `getByLabel('加注到（本街累计）').fill('300')` → `#raise-to` = '300' | ✅ |
| 移动端不溢出 | `scrollWidth−innerWidth ≤ 1`；三类按钮均在 0..390 内且高 ≥40 | ✅ |

## 7. 截图证据

位于 `docs/action-dock/`：

- `desktop-my-turn.png`：桌面轮到本人 → 三行（状态行+细进度线 / 操作行 / 快捷注行），金只留跟注·加注
- `desktop-thinking.png`：非本人行动 → 操作区仅剩状态行「AI-1 思考中…」
- `mobile-my-turn.png`：移动端各行堆叠、触控目标不缩小

## 8. 取舍说明

- **桌面 139px**：目标 96-140px，实测 139px，**达成**（未牺牲按钮尺寸：主按钮 40px、快捷注 40px）。
  若还要更矮，只能压小文字/按钮到难读/难点，按「不硬压」原则不再降。
- **移动端 202px**：落在目标 168-204px 上沿。这是「状态行 + 直接动作行 + 加注组行 + 六快捷注行各自成行、
  且按钮 ≥40px」的必然结果（6 个快捷注在 390px 宽下用 `flex: 1 1 calc(16.666% - 5px)` 尽量排成一行）。
  再压缩就得减小触控目标，故保持现状。
- **D/SB/BB 与本轮改动无关**，未动。

## 9. 未验证边界

- **真实 Go 链路未验证**：全部走 `tests/e2e/browser/fixture`（内存 mock），未跑 `make e2e` / `run_browser.py`
  以及 `playwright.turn.config.ts` / `playwright.human.config.ts` 真实链路（它们依赖 8081 服务）。
- **仅 Chromium**，未跑 Firefox/WebKit。
- **移动端首屏**：390×844 下操作区在座位网格之后，快捷注行可能需轻微下滑才见；仅断言横向不溢出与按钮可点，
  未对「操作区整体进入首屏」做断言。
- **`action-dock.spec.ts` 与 `docs/action-dock/` 为新增文件**，不在最初列出的独占写范围，属验证所需证据。
