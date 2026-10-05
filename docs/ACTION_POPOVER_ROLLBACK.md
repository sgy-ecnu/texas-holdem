# 动作特效保留 · 座位与下注徽章位置回退到 HEAD

目标：座位环与下注徽章布局回退到 HEAD（`b58b53f`），只保留新增的 ActionPopover 动作特效
（1.8 倍缩放 + 四色 + 2200ms 动画 + 展开方向）。不 commit / 不 push。

基线：`git rev-parse HEAD` = `b58b53fa73afb5b7f93f1fb838e4526933f82211`
（工作区相对 HEAD 的改动均为本轮特效开发遗留，尚未提交。）

---

## 1. 回退了哪些位置改动（file:line）

### web/src/components/PokerTable.tsx

| 项 | 处置 | 位置（回退后行号） |
|---|---|---|
| `TOP_BAND = 150` + 纵向半径压缩 | 删除 | geometry 现为 117–136 |
| `RX` 公式 | 恢复 HEAD：`clamp(((w/2 - HALF_X + MARGIN_X)/w)*100, 30, 47)` | 125–126 |
| `RY` 公式 | 恢复 HEAD：`clamp(((h/2 - HALF_Y + MARGIN_Y)/h)*100, 28, 45)`，`MARGIN_Y=4`，无尺寸 fallback 44 | 124 / 127–128 |
| `rimAt(s, ownSeat)` 复杂锚定 | 删除，恢复 HEAD 的 `rimAt(s)`：`t = s.dy > 0.9 ? 0.62 : 0.85`，`{cx: 50 + s.dx*t*RX, cy: 50 + s.dy*t*RY}` | 130–134 |
| 徽章 JSX 锚定类 `anchor-below/own/left/right`、`is-own`、`data-seat` | 删除 | 170–186 |
| 徽章两行 `rim-line1` / `rim-line2` | 删除，恢复单行：`<span className="mini-chip"/>` + `rim-now` + 条件 `rim-cum` | 180–182 |

回退前（被删除）的实现：`TOP_BAND=150` 使 `RY` 上界降到 14、`HALF_Y` 基础上再减 150px，导致上方座位下沉、下方座位上移、整体挤到中央；徽章改为 `anchor-below/own/left/right` 的“贴卡”双行大块，跨座遮挡。

### web/src/styles.css

| 项 | 处置 | 位置（回退后行号） |
|---|---|---|
| `.rim-marks` z-index | 20 → 恢复 HEAD `1` | 1037–1042 |
| `.rim-mark` | 恢复 HEAD 单行胶囊：`transform: translate(-50%,-50%)`、`display:flex; align-items:center; gap:5px`、`border-radius:999px`、`padding:2px 9px`、`animation: badge-in` | 1043–1062 |
| `.rim-mark .rim-now` / `.rim-cum` | 恢复 HEAD | 1063–1069 |
| 双行 `.rim-line1/.rim-line2`、`.anchor-own/left/right`、`.rim-mark .mini-chip` 覆盖 | 删除 | — |
| `@keyframes rim-mark-in` | 删除（随双行样式一起废弃，恢复使用 `badge-in`） | — |

### 测试契约同步（tests/e2e/browser/fixture/tests/*）

删除两个编码了“被回退布局”契约的用例文件（均为此前特效工作新增、未提交）：

- `bet-badge-v2.spec.ts`：断言双行 `rim-line1/rim-line2`、`data-seat`、`anchor-own`、卡片下方/左侧贴卡定位。
- `bet-badge-clearance.spec.ts`：断言 `.rim-mark.is-own`、`data-seat`、双行徽章 clearance。

两者测试的正是本次删除的双行 + 锚定行为，回退后不再适用；徽章的“渲染存在且不压公共牌”仍由既有
`table-layout.spec.ts` 的同名用例覆盖（该用例为 HEAD 既有、未改动）。

### 未改动（按要求保留）

- `web/src/components/SpectatorView.tsx`：FIFO 生命周期、reduced-motion 检测、`hints/onHintDone` 挂载完整保留。
- `web/src/components/ActionPopover.tsx`：四色、`scale 1.8`、2200ms 时间轴、展开方向类型、`onDone` 未动。
- `PokerTable.tsx` 内：`hints`/`onHintDone` props、ActionPlacement 判定、ActionPopover 渲染保留。
- `styles.css` 内 `.action-popover` 特效段（933–1034：`--pop-scale:1.8`、四色、`@keyframes action-pop`、
  above-left/right/center/is-right 展开方向、`z-index:60`）以及移动端特效排布覆盖保留。

---

## 2. 特效遮挡如何校正

问题：左侧座位 1.8 倍“跟注”气泡右下被**更晚出现在 DOM 的相邻 `.seat-slot`**（牌背 / 座位卡 / 「思考中」标签）覆盖。

根因：`.seat-slot` 自带 `z-index`（`2`，行动者 `4`）形成独立层叠上下文；`.action-popover` 的 `z-index:60`
**只在自身 slot 的层叠上下文内**生效，无法压过排在后面的兄弟 slot。

校正（不改座位环、不降 scale）：
- `PokerTable.tsx:235` 给携带特效的 slot 追加 `has-hint` 类。
- `styles.css:703–706` 新增：
  ```css
  .seat-slot.has-hint {
    z-index: 25;
  }
  ```
  使其临时高于其它座位 slot（仍低于 `.center` 保护区：`.seats` 层整体 `z-index:30` < `.center` `40`，
  故特效不会盖住公共牌 / 底池）。特效层仍为 `z-index:60`，展开方向沿用按半桌分派（左半 `above-left`、
  右半 `above-right`、顶部 `above-center`、底部真人 `right`），保证放大后向外展开。

程序化验证（临时探针，验证后已删除）：在左侧座位 `跟注` 气泡的底部左/中/右取点，
`elementFromPoint` 最上层元素均为该 `.action-popover` 自身：

```
[occlusion] {"out":[
  {"name":"右下","topClass":"SPAN.action-popover is-call is-above-left","inside":true},
  {"name":"中心","topClass":"SPAN.action-popover is-call is-above-left","inside":true},
  {"name":"左下","topClass":"SPAN.action-popover is-call is-above-left","inside":true}
],"slotZ":"25","popZ":"60"}
1 passed (3.5s)
```

---

## 3. 命令与完整输出

### 3.1 typecheck

```
$ npm --prefix web run typecheck

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

TYPECHECK_EXIT=0
```

### 3.2 build

```
$ npm --prefix web run build

> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
transforming...
✓ 44 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-RzVafJlC.css   31.08 kB │ gzip:  7.87 kB
dist/assets/index-XWFIrc9D.js   270.52 kB │ gzip: 85.46 kB
✓ built in 718ms
BUILD_EXIT=0
```

### 3.3 fixture 全量（`playwright.fixture.config.ts`）

```
$ npx playwright test --config playwright.fixture.config.ts

Running 28 tests using 1 worker
...
✓  24 [chromium] › fixture/tests/table-layout.spec.ts:94:1 › 人数模板 2-9（真人局）：不重叠、不侵入中央、真人底部居中、两行固定 (26.0s)
[layout] stage=1334x734 felt=1158x624
✓  25 [chromium] › fixture/tests/table-layout.spec.ts:137:1 › 桌面 · 真人 9 座：底部居中、中央清爽、座位卡两行、牌桌更大 (3.6s)
✓  26 [chromium] › fixture/tests/table-layout.spec.ts:163:1 › 桌面 · 全 AI 9 座观战：环绕不重叠、中央清爽 (672ms)
✓  27 [chromium] › fixture/tests/table-layout.spec.ts:177:1 › 桌沿徽章：本街下注（本轮 + 累计）落在护栏内侧，不压中央 (1.9s)
✓  28 [chromium] › fixture/tests/table-layout.spec.ts:210:1 › 移动端 390：座位可点、不溢出、操作条在视口内 (3.2s)

  28 passed (2.1m)
FIXTURE_EXIT=0
```

关键用例结果（逐场景）：

| 场景 | 用例 | 结果 |
|---|---|---|
| 座位环回退（环绕桌沿） | `table-layout.spec.ts`「人数模板 2-9（真人局）」「全 AI 9 座观战」 | pass |
| 桌沿徽章单行回退 | `table-layout.spec.ts`「桌沿徽章：本街下注…不压中央」 | pass |
| 特效 scale 1.8 / 2200ms / 四色 | `action-popover-scale.spec.ts`（4 用例） | pass |
| 特效展开方向 origin | `action-popover-scale.spec.ts`「展开方向 transform-origin」 | pass |
| 特效 FIFO / 不遮挡 / 模板 2/6/9 / 移动端 | `action-popover.spec.ts`（4 用例） | pass |
| reduced-motion 静态展示 | `action-popover*.spec.ts` | pass |
| 其他 | `action-dock` / `quick-bet-keys` / `human-player` | pass |

### 3.4 截图路径

fixture 用例落盘目录：`docs/action-popover/`（默认 `SCREEN_DIR`）。本次运行产出/刷新：

- 桌面 9 座座位位置恢复：`desktop-allai-9.png`、`desktop-rim-bets.png`、`v2-desktop-four-actions.png`
- 特效可见：`desktop-human-3-popover.png`、`count-2-popover.png`、`count-6-popover.png`、`count-9-popover.png`、
  `scale-desktop-call.png`、`scale-desktop-raise.png`
- 移动端：`mobile-390-popover.png`、`scale-mobile-raise.png`
- reduced-motion：`reduced-motion-static.png`、`scale-reduced-motion.png`

（本 agent 运行环境不支持读图，截图仅作落盘留证，未做像素级目视复核；遮挡以 3.2 的几何断言 +
第 2 节的 `elementFromPoint` 探针为准。）

---

## 4. 未验证边界 / 说明

- **未读图**：无法目视截图；“特效不侵入邻座”以几何断言（`.action-popover` 与 `.board/.pot/.hole/.turn-badge`
  不重叠、特效互相不重叠）与 `elementFromPoint` 层叠探针为证据。
- **top 座位 1.8 倍空间**：回退后座位回到桌沿环绕，顶部座位特效沿用 `above-center` 向上展开；
  当前 1536×1024 下 `count-9` 用例（含顶部/左右/底部）未出现与中央或彼此重叠。更矮视口若将来报冲突，
  按要求只调特效内收偏移、不压缩座位环、不降 scale。
- **未跑真实 Go E2E**：本任务只跑 fixture（前端契约），不含真实服务链路。
- **未提交**：所有改动停在未提交状态；未 commit / 未 push。
- **凭据**：全程未读取 `.env.local` / `config.local.yaml` 的值。
