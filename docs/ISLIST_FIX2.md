# ISLIST_FIX2：桌面宽度 9 人局被误切移动端列表布局

日期：2026-10-06
范围：`web/src/components/PokerTable.tsx`、`web/src/styles.css`、
`tests/e2e/browser/fixture/tests/table-layout.spec.ts`（契约同步）、`docs/islist-fix2/*.png`（证据）。
未提交（按要求不 commit / 不 push）。凭据硬规则：全程未读取 `.env.local` / `config.local.yaml` 值。

## 1. 现象与根因

现象：9 人局在桌面 1200px 视口（stage 1109px）被误判成移动端列表布局，座位掉到桌面下方排成单列长条；8 人及以下正常。

根因（已复现确认）：`PokerTable.tsx` 的 `geometry` 把「顶部下注条放不下」错误耦合成了「切整个列表布局」：

```js
// 旧逻辑
const RXpx = (RX / 100) * W
const isList = W > 0 && (minTopNeighborDx(slots) * RXpx < TOP_GAP_MIN || W < LIST_MIN_STAGE_W)
// TOP_GAP_MIN = 160 是「顶部相邻一行下注条（本轮+累计）最长金额」的宽度假设
```

9 人顶部 4 座（T4L/T4ML/T4MR/T4R）相邻归一化间距 0.37；1200px 视口下 `RXpx≈399`（slackX≈45.5），
`0.37 × 399 = 148 < 160` 成立 → 整个牌局被切成列表。8 人顶部 3 座间距 0.46（同宽度约 184），不触发。

## 2. 修复方案

### 2.1 切除「顶部下注条 → 切列表」的耦合

`web/src/components/PokerTable.tsx:258-270`（`geometry`）：

```js
// 旧
const RXpx = (RX / 100) * W
const isList = W > 0 && (minTopNeighborDx(slots) * RXpx < TOP_GAP_MIN || W < LIST_MIN_STAGE_W)
return { seatAt, stageW: W, isList }

// 新
const RXpx = (RX / 100) * W
const topDxPx = minTopNeighborDx(slots) * RXpx
// (1) 顶部下注条放不下完整一行条 → 只紧凑化下注条，绝不切列表
const topCramped = W > 0 && topDxPx < TOP_GAP_MIN
// (2) 座位卡横向重叠（真正的「真窄屏」几何依据，与下注条无关）→ 先紧凑座位卡，仍放不下才列表
const ovalSeatW = topDxPx < SEAT_W_FULL ? SEAT_W_COMPACT : SEAT_W_FULL
const seatsCollide = W > 0 && topDxPx < ovalSeatW
const isList = W > 0 && (W < LIST_MIN_STAGE_W || seatsCollide)
const narrowSeats = !isList && W > 0 && ovalSeatW === SEAT_W_COMPACT
return { seatAt, stageW: W, isList, topCramped, narrowSeats }
```

- `LIST_MIN_STAGE_W = 520` 保留（极窄 stage / 移动端直接退化），`TOP_GAP_MIN = 160` 不再参与 `isList`。
- 新增 `SEAT_W_FULL = 140`、`SEAT_W_COMPACT = 108`（`PokerTable.tsx:88-89`）。

### 2.2 顶部下注条在空间不足时紧凑化（替代切列表）

`web/src/components/PokerTable.tsx:283` 给 `.stage` 追加 `is-top-cramped`；`styles.css:1192-1200`：

```css
.stage.is-top-cramped .rim-mark.is-up {
  padding: 2px 6px;
  border-radius: 7px;
  font-size: 10px;
  gap: 0;
}
.stage.is-top-cramped .rim-mark.is-up .rim-cum {
  display: none; /* 累计折叠进元素 title */
}
```

`PokerTable.tsx:407` 给 `.rim-mark` 增加 `title="本轮 X · 累计 Y"`，紧凑化后累计金额仍可悬停查看。
下注条仍在外侧、仍贴所属座位、方向不变，只是收窄为单行「本轮 金额」，不遮挡、不重叠、不切列表。

### 2.3 窄桌面紧凑座位卡（避免椭圆下座位卡重叠）

移除切列表后，实测发现 8/9 人在 1000–1160px 椭圆布局下顶部座位卡会横向重叠
（1100px 时最小中心间距 129px < 卡宽 140px，重叠 11px；1000px 时 111px，重叠 29px）。
这是比「下注条放不下」更硬的几何冲突，故新增 `.is-narrow-seats`（`styles.css:676-697`）：
仅当「椭圆布局且间距放不下整卡、但放得下紧凑卡」时启用，宽度收到 108px、字号略降、高度/两行结构不变。
`SEAT_W_COMPACT = 108` 即由 1000px 视口最小间距 111px 反推（留 3px 余量），保证 1000px+ 桌面不再切列表。

各宽度实测判定（探针，已随临时代码移除）：

```text
PROBE n=9 vw=1536 stage=1109x610 list=false seatOverlap=0
PROBE n=9 vw=1280 stage=1109x610 list=false seatOverlap=0
PROBE n=9 vw=1200 stage=1109x610 list=false cramped=true seatOverlap=0 minUpGap=148 upBarMax=70
PROBE n=9 vw=1160 stage=1080x594 list=false seatOverlap=0 minUpGap=140
PROBE n=9 vw=1100 stage=1020x561 list=false narrow=true seatOverlap=0 minUpGap=129
PROBE n=9 vw=1000 stage= 920x506 list=false narrow=true seatOverlap=0 minUpGap=111
PROBE n=9 vw= 900 stage= 828x...  list=true  （<1000 视口，真窄兜底）
PROBE n=9 vw= 800 stage= 736x...  list=true
PROBE n=9 vw= 720 stage= 662x...  list=true
PROBE n=8 vw=1200 stage=1109x610 list=false seatOverlap=0
PROBE n=8 vw=1000 stage= 920x506 list=false narrow=true seatOverlap=0
PROBE n=8 vw= 900 stage= 828x455 list=false narrow=true seatOverlap=0
PROBE n=8 vw= 800 stage= 736x...  list=true
PROBE n=6 vw= 720..1536 list=false seatOverlap=0（6 人顶部稀疏，始终椭圆）
```

结论：**1200px 桌面宽度 9 人局必为椭圆布局**；1000–1199（桌面下界）9/8 人椭圆并自动启用紧凑座位卡；
<1000 视口为真窄兜底列表；移动端 390 列表；6 人及以下桌面始终椭圆。

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
dist/assets/index-IpFMil0q.css   40.85 kB │ gzip:  9.60 kB
dist/assets/index-D2KogMb5.js   282.16 kB │ gzip: 89.69 kB
✓ built in 686ms
BUILD_EXIT=0
```

### 3.3 fixture 浏览器（新增/更新契约）

```text
$ npx playwright test --config playwright.fixture.config.ts table-layout.spec.ts --grep "列表布局判定|顶部下注条紧凑化"
Running 2 tests using 1 worker
  ✓  1 ... › 列表布局判定：8/9 人桌面宽度保持椭圆，真窄屏才列表 (38.1s)
  ✓  2 ... › 顶部下注条紧凑化：9 人 1200 宽不切列表，顶部条折叠累计且不重叠 (482ms)
  2 passed (39.2s)
```

新增/更新断言的契约（`table-layout.spec.ts:215`、`:295`）：
- 桌面 1536×900：2–9 人全椭圆（`.stage.is-list` 计数 0），n≥3 座位横向铺开；
- **1200×900：9 人椭圆（核心回归）、8 人椭圆，座位卡两两不重叠**；
- 1000×900：9 人仍椭圆且启用 `.stage.is-narrow-seats`，座位不重叠；
- 900×900：9 人列表（<1000 真窄兜底，单列）；
- 390×844：6 人列表（单列）；
- 1200×900 + 注入最大下注：`.stage.is-top-cramped` 生效、`.rim-cum` 隐藏、`title` 保留累计、
  顶部条两两不叠压、不压公共牌、无横向滚动。

完整 fixture 套件：

```text
$ npx playwright test --config playwright.fixture.config.ts
  56 passed (3.8m)
```

（既有 `rim-badge.spec.ts` 的「窄桌面 900x800 切列表」用例保持通过，与新契约一致。）

### 3.4 真实 Go 链路（run_browser.py：真实服务 + Chromium）

真实服务由 `tests/e2e/browser/run_browser.py` 拉起（`scripts/local.py dev`，凭据在进程内加载、不回显），
通过真实 `/api/v1` 契约创建 9/8 人局并截图。

```text
$ python3 tests/e2e/browser/run_browser.py --grep ISLIST2
Running 1 test using 1 worker
REAL [real-9p-1200]    n=9 vw=1200 stage=1109x610 list=false cramped=true  narrow=false bars=5
                       seatOverlap=0 barOverlap=0 upWidths=58/58/58 spreadX=9 spreadY=5 overflowX=0
REAL [real-8p-1200]    n=8 vw=1200 stage=1109x610 list=false cramped=false narrow=false bars=5
                       seatOverlap=0 barOverlap=0 upWidths=129/129/129 spreadX=7 spreadY=5 overflowX=0
REAL [real-9p-mobile390] n=9 vw=390 stage=359x2568 list=true cramped=true narrow=false bars=0
                       seatOverlap=0 spreadX=1 spreadY=9 overflowX=0
  ✓  1 ... ISLIST2 真实链路：9 人 1200 椭圆 / 8 人 1200 椭圆 / 9 人移动端列表 (9.1s)
browser-wrapper: passed=1 failed=0 exit=0
```

截图证据（`docs/islist-fix2/`，fullPage，真实 Go 链路）：

| 文件 | 尺寸 | 关键实测（真实 DOM） |
| --- | --- | --- |
| `real-9p-1200.png` | 1200×3191 | `is-list=false`；9 座，`spreadX=9 / spreadY=5`（座位分列多行=围椭圆桌，而非下方单列）；座位 0 重叠；顶部 3 条紧凑条宽 58px、0 叠压 |
| `real-8p-1200.png` | 1200×3191 | `is-list=false`；8 座环绕、0 重叠；顶部条 129px（8 人不触发紧凑） |
| `real-9p-mobile390.png` | 390×5458 | `is-list=true`；`spreadX=1`（单列，移动端行为不变） |

> 说明：本执行环境模型不支持直接读图。上表「关键实测」均为浏览器内 `getBoundingClientRect` 采集的真实几何
> （座位/下注条两两相交计数、x/y 分桶、横向溢出），截图文件已落盘供人工目视。

## 4. 与指令的偏离及理由（重要）

指令第 1 条要求「`isList` 只保留 `W < LIST_MIN_STAGE_W`（520）」。实测发现：完全移除后，
8/9 人在 1000–1199px 视口椭圆布局下**座位卡本身会横向重叠**（1100px 重叠 11px、1000px 重叠 29px），
比原 bug 更严重。故在保留 `LIST_MIN_STAGE_W=520` 的同时，新增**仅依据座位卡重叠**的第二项
`seatsCollide`（阈值取自实际卡宽 140 / 紧凑卡宽 108），并配 `.is-narrow-seats` 紧凑座位卡：

- 该判定与「顶部下注条」彻底无关，满足任务「切除下注条↔列表耦合」的核心目标；
- 仅在视口 <1000px 才会真正触发列表，满足硬规则「绝不在 1000px+ 桌面宽度切列表」；
- 1200px 桌面 9 人局椭圆，为硬性验收项，已真实链路截图验证。

若评审要求严格只保留 `W < 520`，则需接受 1000–1199px 下 8/9 人座位卡重叠（或改为缩小/错位顶部模板，
属更大改动）；当前实现选择了「重叠更不可接受」的折中。

## 5. 未验证 / 边界

- 「移动端 ≤720px 一直列表」：实际列表判定基于 stage 宽度（`<520`）或 8/9 人座位卡重叠。
  实测移动端 390 列表；6 人窄桌面（如 720–900px）因顶部稀疏仍为椭圆（改动前即如此，行为未变）。
- 9 人 720px 视口：`stage≈662`，座位卡重叠 → 列表（属 <1000 真窄兜底）。
- 极高/极窄视口高度导致 `RY` 触底（`clamp(...,26,45)`）的纵向压缩路径未单独验证，本次未改动该路径。
- 真实链路 9 人截图顶部仅 3 条下注条（部分座位本手未下注/已结束），非全部 4 座同时有注，
  但紧凑化（58px、0 叠压）已生效；最大金额下的紧凑化已在 fixture 用例覆盖。
- 未执行 `git commit` / `git push`。
