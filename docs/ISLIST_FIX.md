# 修复：6 人（及更少人）牌桌被误触发「移动端列表布局」

日期：2026-10-05
范围：`web/src/components/PokerTable.tsx`、`tests/e2e/browser/fixture/tests/table-layout.spec.ts`（契约）、`docs/islist-fix/*.png`（证据）。
未提交（不 commit / 不 push）。

## 1. 现象与根因

现象：6 人局（及更少）在桌面宽度下，座位没有围在椭圆桌沿，而是掉到桌面下方排成单列长条（`.stage.is-list` 形态），即被误判成移动端列表布局。用户截图 ui9/ui10 已证实。

根因（架构师 arc-11 定位）：`PokerTable.tsx` 的 `geometry` 用固定的「顶部 4 座相邻间距」常量判断是否切列表：

```js
// 旧逻辑（PokerTable.tsx 约 207-225 行）
const RXpx = (RX / 100) * W
const isList = W > 0 && (TOP_NEIGHBOR_DX * RXpx < TOP_GAP_MIN || W < LIST_MIN_STAGE_W)
// TOP_NEIGHBOR_DX = 0.37（对应 9 人 T4 四座密集间距），TOP_GAP_MIN = 160
```

问题：`TOP_NEIGHBOR_DX = 0.37` 是 9 人顶部四座的间距，但没有按人数区分。6 人模板 `TEMPLATES[6] = [B, LL, UL, T, UR, LR]` 顶部只有 UL/T/UR 三个槽位、间距本应很大；同时 `ENVELOPE_SIDE = 131` 把 `HALF_X` 撑到 `70 + 131 = 201`，压缩了 `RX`，使得 `0.37 × RXpx < 160` 在中等宽度下成立，6 人局被误判成列表布局。只有 8/9 人（顶部 T3/T4 三、四座密集）才真正存在该问题。

## 2. 修复方案（file:line + 新旧逻辑）

文件：`web/src/components/PokerTable.tsx`

### 2.1 顶部相邻间距改为「按当前人数实际槽位」计算

新增模块级 helper（`PokerTable.tsx:112-132`）：

```js
// 当前座位集里「上向」下注条相邻槽位的最小水平间距（归一化 dx）。
// 顶部只有 0/1 个上向座位（2/4 人）时返回 Infinity（永不触发顶部拥挤退化）；
// 6 人 UL/T/UR 间距 0.72、7 人 T2L/T2R 0.72、8 人 T3 三座 0.46、9 人 T4 四座 0.37。
function minTopNeighborDx(slots: Slot[]): number {
  const dxs = slots.filter((s) => barDir(s) === 'up').map((s) => s.dx).sort((a, b) => a - b)
  let min = Infinity
  for (let i = 1; i < dxs.length; i++) min = Math.min(min, dxs[i] - dxs[i - 1])
  return min
}
```

删除固定常量 `TOP_NEIGHBOR_DX = 0.37`，保留 `TOP_GAP_MIN = 160`、`LIST_MIN_STAGE_W = 520`（`PokerTable.tsx:95-101`）。

`isList` 判定（`PokerTable.tsx:247-252`）：

```js
// 旧
const isList = W > 0 && (TOP_NEIGHBOR_DX * RXpx < TOP_GAP_MIN || W < LIST_MIN_STAGE_W)
// 新
const RXpx = (RX / 100) * W
const isList = W > 0 && (minTopNeighborDx(slots) * RXpx < TOP_GAP_MIN || W < LIST_MIN_STAGE_W)
```

各人数实际顶部相邻间距（归一化 dx）：

| 人数 | 顶部上向座位 | 最小相邻间距 | 是否可能因「顶部间距」切列表 |
| --- | --- | --- | --- |
| 2 | T | ∞（仅 1 个） | 否 |
| 3 | UL/UR | 1.44 | 否 |
| 4 | T | ∞ | 否 |
| 5 | UL/UR | 1.44 | 否 |
| 6 | UL/T/UR | 0.72 | 否（本次修复核心） |
| 7 | T2L/T2R | 0.72 | 否 |
| 8 | T3L/T3M/T3R | 0.46 | 是（真正密集） |
| 9 | T4L/T4ML/T4MR/T4R | 0.37 | 是（真正密集） |

保留 `W < LIST_MIN_STAGE_W`（极窄退化）不变。

### 2.2 ENVELOPE_SIDE 仅在存在侧向座位时计入

`ENVELOPE_SIDE = 131` 是为「左/右向两行下注条」预留的横向外围包络。2/3/6 人模板没有正左/正右座位（UL/UR 的条方向为「上」、LL/LR 为「下」），此时横向不需要 131px，却仍被计入导致椭圆半径被无谓压缩、桌子偏小。

新增 `hasSideBars`（`PokerTable.tsx:125-132`）并在半径计算中条件计入（`PokerTable.tsx:237-239`）：

```js
// 旧
const HALF_X = SEAT_HALF_W + ENVELOPE_SIDE
// 新
const HALF_X = SEAT_HALF_W + (hasSideBars(slots) ? ENVELOPE_SIDE : ENVELOPE_SIDE_NONE)
// ENVELOPE_SIDE_NONE = 21（座位卡边 + AI 牌背外探/间隙）
```

判断依据是槽位方向（`barDir`），只与人数/模板有关，同一局内固定，不会随下注状态或折叠抖动。4/5/7/8/9 人仍有侧向座位，行为不变。

## 3. 命令输出

### 3.1 typecheck

```text
$ npm --prefix web run typecheck
> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit
TYPECHECK_EXIT=0
```

### 3.2 build

```text
$ npm --prefix web run build
> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build
vite v7.3.6 building client environment for production...
✓ 50 modules transformed.
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-BsyZcR7H.css   39.30 kB │ gzip:  9.35 kB
dist/assets/index-C7VMxsjI.js   281.84 kB │ gzip: 89.50 kB
✓ built in 748ms
BUILD_EXIT=0
```

### 3.3 fixture 浏览器（非真实链路）

完整 `table-layout.spec.ts`（含新增契约用例）：

```text
Running 6 tests using 1 worker
  ✓ 1 人数模板 2-9（真人局）：不重叠、不侵入中央、真人底部居中、两行固定 (26.1s)
[layout] stage=1334x734 felt=1012x528
  ✓ 2 桌面 · 真人 9 座：底部居中、中央清爽、座位卡两行、牌桌占据主要区域 (3.6s)
  ✓ 3 桌面 · 全 AI 9 座观战：环绕不重叠、中央清爽 (696ms)
  ✓ 4 座位外侧下注条：本街下注（本轮 + 累计）在桌沿外，不压中央 (680ms)
  ✓ 5 列表布局按人数区分：6 人及以下桌面不误触发，8/9 人顶部密集时按宽度切 (32.1s)
  ✓ 6 移动端 390：座位可点、不溢出、操作条在视口内 (3.2s)
  6 passed (1.1m)
```

完整 fixture 套件（全量）：

```text
  50 passed (3.4m)
```

新增契约用例断言（`tests/e2e/browser/fixture/tests/table-layout.spec.ts:213`）：
- 桌面 1536×900：2–9 人全部椭圆（`.stage.is-list` 计数 0），n≥3 座位横向铺开（x 分桶 ≥3）；
- 窄桌面 1000×900：6 人仍椭圆（本次修复核心回归），9 人顶部密集则切列表；
- 移动端 390×844：6 人切列表（单列，x 分桶 =1）。

各人数/宽度实际判定（临时探针，已移除）：

```text
PROBE vw=781  n=6 isList=false stage=719x395  uniqXY=5/4
PROBE vw=781  n=8 isList=true  stage=719x2586 uniqXY=1/8
PROBE vw=781  n=9 isList=true  stage=719x2837 uniqXY=1/9
PROBE vw=1000 n=6 isList=false stage=920x506  uniqXY=5/4
PROBE vw=1000 n=8 isList=true  stage=920x2750 uniqXY=1/8
PROBE vw=1000 n=9 isList=true  stage=920x3003 uniqXY=1/9
PROBE vw=1100 n=6 isList=false stage=1020x561 uniqXY=5/4
PROBE vw=1100 n=8 isList=false stage=1020x561 uniqXY=7/5
PROBE vw=1100 n=9 isList=true  stage=1020x3078 uniqXY=1/9
PROBE vw=1200 n=6 isList=false stage=1109x610 uniqXY=5/4
PROBE vw=1200 n=8 isList=false stage=1109x610 uniqXY=7/5
PROBE vw=1200 n=9 isList=true  stage=1109x3145 uniqXY=1/9
PROBE vw=1280 n=6 isList=false stage=1109x610 uniqXY=5/4
PROBE vw=1280 n=8 isList=false stage=1109x610 uniqXY=7/5
PROBE vw=1280 n=9 isList=false stage=1109x610 uniqXY=9/5
PROBE vw=1536 n=6 isList=false stage=1109x610 uniqXY=5/4
PROBE vw=1536 n=9 isList=false stage=1109x610 uniqXY=9/5
```

结论：6 人在 781–1536 全程椭圆；8 人在 ≤1000 才切列表；9 人在 ≤1200 切列表、≥1280 椭圆——只有 8/9 密集顶部才会触发。

### 3.4 真实 Go 链路（make run + Chromium）

启动：`make run`（自动 `stop_stale_server` 清理 8081 残留），健康检查 200 后驱动真实 `/api/v1` 契约创建 6/9 人局并截图。

```text
HEALTH_OK ~15s
cleanup current game 8e74621c-... status=paused stop=200
REAL n=6 vw=1024 isList=false stage=944x519  uniqXY=5/4 -> docs/islist-fix/go-6p-1024.png
REAL n=6 vw=1536 isList=false stage=1109x610 uniqXY=5/4 -> docs/islist-fix/go-6p-1536.png
REAL n=6 vw=390  isList=true  stage=359x1893 uniqXY=1/6 -> docs/islist-fix/go-6p-mobile390.png
REAL n=9 vw=1280 isList=false stage=1109x610 uniqXY=9/5 -> docs/islist-fix/go-9p-1280.png
REAL n=9 vw=1000 isList=true  stage=920x3083 uniqXY=1/9 -> docs/islist-fix/go-9p-1000.png
script_exit=0
```

截图证据（`docs/islist-fix/`）：

| 文件 | 尺寸 | 目视结论 |
| --- | --- | --- |
| `go-6p-1024.png` | 1024×900 | 6 人（AI-1…AI-6）围椭圆桌沿，非下方单列（本次 bug 直接修复证据） |
| `go-6p-1536.png` | 1536×900 | 6 人椭圆环绕 |
| `go-6p-mobile390.png` | 390×844 | 移动端列表布局（4:3 圆角桌 + 座位下沉到桌面下方） |
| `go-9p-1280.png` | 1280×900 | 9 人椭圆环绕（顶部四座间距放得下，不切列表） |
| `go-9p-1000.png` | 1000×900 | 9 人顶部密集，正确切列表布局 |

- 6 人局直接复现宽度 1024：修复前（固定 `TOP_NEIGHBOR_DX=0.37`、`HALF_X=201`）该宽度 `0.37×RXpx < 160` 成立会切列表；修复后 `minTopNeighborDx=0.72 → 0.72×RXpx > 160` 保持椭圆。
- `.stage` 无 `is-list`，座位 x 分桶=5、y 分桶=4，证明环桌分布而非单列。

## 4. ENVELOPE_SIDE 是否调整及理由

调整：`ENVELOPE_SIDE` 仅在存在侧向（左/右向）下注条座位时计入；无侧向座位的 2/3/6 人改用 `ENVELOPE_SIDE_NONE = 21`。

理由：横向 131px 包络是为侧向两行下注条预留；2/3/6 人模板无正左/正右座位，其顶部/底部座位的条为「上/下」向，横向不需要该包络，仍计入会无谓压缩 `RX`、使椭圆桌偏小。判断只在游戏内固定的槽位方向上进行，不随下注/折叠变化，不会引起尺寸抖动。4/5/7/8/9 人仍含侧向座位，行为不变（fixture 2–9 人「不重叠/不侵入中央/无横向溢出」全通过）。

## 5. 未验证 / 边界说明

- 真实链路截图场景为「全 AI 观战」6/9 人（标题显示「公共观战」）；真人座位模板与人数一致，槽位方向相同，未单独对真人 6 人局截图（fixture 已覆盖真人 2–9 人并断言真人底部居中）。
- 8/9 人顶部密集在窄桌面切列表属保留的退化行为，仅影响 8/9 人密集顶部；在 1536×900 等常见桌面宽度下 9 人顶部间距 `0.37×RXpx≈210 > 160` 不切列表（已截图验证）。
- 未覆盖的极端：视口高度极小导致纵向 `RY` 触底（`clamp(...,26,45)`）时的座位纵向压缩未单独验证；该路径未改动。
- 未执行 `git commit` / `git push`（按要求保持未提交）。
