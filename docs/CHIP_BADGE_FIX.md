# 下注徽章（桌沿 .rim-mark）遮挡修复

本轮修复 HEAD 版即存在的「本轮/累计」下注徽章遮挡 UI：①底部真人座位的徽章压住两张明牌上部点数区；
②左右下座位（如 AI-2/AI-9）的徽章横穿座位卡第一行（名字/座位号/SB/BB 标记）。
**座位位置保持环绕桌沿（未改动），仅调徽章定位。** 未改 Go / scripts / config / Seat.tsx / ActionPopover.tsx / SpectatorView.tsx / format.ts / types.ts。

## 0. 结论（先看）

| 项 | 修复前（HEAD 几何） | 修复后 |
| --- | --- | --- |
| 全 AI 9 座徽章 vs 座位卡第一行 | ✘ 相交 2 处（AI-2 左下、AI-9 右下） | ✓ 0 处 |
| 真人 9 座徽章 vs 座位卡第一行 | ✘ 相交 2 处（左下、右下） | ✓ 0 处 |
| 徽章 vs 底牌（含真人明牌）/彼此 | ✓ 0（但真人底牌仅余 ~6px，更矮视口即压牌） | ✓ 0，且明牌上方留 ~7px |
| 座位中心坐标（`seat_centers`） | 基线 | 与修复前**逐像素完全一致**（座位未动） |

真实浏览器（Chromium + 真实 Go 服务）实测 stage=1334×734、felt=1158×624。

## 1. 改动文件与位置

| 文件 | 位置 | 改动 |
| --- | --- | --- |
| `web/src/components/PokerTable.tsx` | L64–L74 | 新增徽章定位所需固定像素常量 |
| 同上 | L92–L113 | 新增徽章层 ref 与「实测各徽章宽度」的 layout effect |
| 同上 | L118–L197 | 重写 `rimAt`：轴对齐推出 + 支撑半长 + 护栏内侧约束 |
| 同上 | L231–L247 | 徽章渲染传入 `openHole` / 实测半宽，并加 `data-seat` |
| `web/src/styles.css` | L1065 | `.rim-mark` 改用专用入场动画 `rim-mark-in` |
| 同上 | L1505–L1517 | 新增 `@keyframes rim-mark-in`（终态保持 `translate(-50%,-50%)`） |

## 2. 徽章定位改动（新旧公式）

### 2.1 旧公式（HEAD，`PokerTable.tsx` 约 L130–134）

```ts
const rimAt = (s: Slot) => {
  const t = s.dy > 0.9 ? 0.62 : 0.85
  return { cx: 50 + s.dx * t * RX, cy: 50 + s.dy * t * RY }
}
```

固定系数内收：底部用 0.62、其余 0.85。问题在于它是「几何比例」而非「像素避让」：
- 底部真人明牌高度固定 96px（29 卡半高 + 7 缝 + 58.8 牌高），舞台越矮该值占 stage 比例越大，0.62 不足以越过；
- 左下/右下槽位 `(dx≈±0.98, dy=0.42)` 的径向内收方向是「向上+向右」，直接把徽章推进座位卡第一行。

### 2.2 新公式（`PokerTable.tsx` L160–L195）

```ts
// 固定像素常量（L64–L74）
SEAT_HALF_W = 70; SEAT_HALF_H = 29          // 座位卡 140×58
OPEN_HOLE_UP = 96                            // 真人明牌自座位中心向上的突出
MARK_HALF_H = 12; MARK_GAP = 6
MARK_HALF_W_DEFAULT = 90                     // 未测宽前的保守半宽
FELT_RX_FRAC = 0.434                         // 护栏(felt)椭圆横向半径 / stage 宽

const rimAt = (s, openHole, halfW) => {
  // 轴对齐推出方向：底/顶竖推、侧点横推、其余按主导轴
  let ax = 0, ay = 0
  if (s.dy > 0.9) ay = -1
  else if (s.dy < -0.9) ay = 1
  else if (Math.abs(s.dx) > Math.abs(s.dy) * 1.25) ax = s.dx < 0 ? 1 : -1
  else ay = s.dy < 0 ? 1 : -1

  let obstacle = ax !== 0 ? SEAT_HALF_W : SEAT_HALF_H
  if (openHole && ay < 0) obstacle = Math.max(obstacle, OPEN_HOLE_UP)   // 越过真人明牌
  const badgeHalf = ax !== 0 ? halfW : MARK_HALF_H
  let dist = obstacle + badgeHalf + MARK_GAP

  // 护栏内侧约束：把徽章中心（含自身半尺寸）推进 felt 椭圆内
  const fx = FELT_RX_FRAC * W
  const fy = 0.445 * H - 0.011 * W
  if (ax !== 0) {
    const allowed = fx * Math.sqrt(Math.max(0, 1 - ((sy - H / 2) / fy) ** 2))
    dist = Math.max(dist, Math.abs(sx - W / 2) - (allowed - halfW))
  } else {
    const allowed = fy * Math.sqrt(Math.max(0, 1 - ((sx - W / 2) / fx) ** 2))
    dist = Math.max(dist, Math.abs(sy - H / 2) - (allowed - MARK_HALF_H))
  }
  return { cx: ((sx + ax * dist) / W) * 100, cy: ((sy + ay * dist) / H) * 100 }
}
```

**原理（确定性，无碰撞引擎）**：沿推出轴方向 `u`（单位向量），把徽章中心从座位中心移动 `dist`。
对「座位卡（含真人明牌）盒 A」与「徽章盒 B」，只要 `dist ≥ h_A(u) + h_B(u) + gap`，
两个轴对齐盒沿 `u` 的投影即分离，从而**二维必不相交**（`h` 为盒沿 `u` 的支撑半长）。
`h_A(u)=|ux|·卡半宽+|uy|·卡半高`（真人向上推出时取与明牌高度的较大者），`h_B(u)=|ux|·徽章半宽+|uy|·半高`。
徽章实际宽度随金额变化，故用 layout effect 读取每个 `.rim-mark` 的真实 `offsetWidth/2` 代入（收敛一次，无循环）。

**为什么用轴对齐而非径向**：径向推出会让斜向顶排相邻徽章（如 T4L 与 T4ML）横向互相靠拢而叠压；
轴对齐竖推/横推时另一轴坐标与座位保持一致，相邻徽章间距不被缩短。

### 2.3 `styles.css` 居中修复

旧 `.rim-mark` 用通用 `badge-in` 动画，其终态是 `transform: translate(-50%, 0)`，
在 `animation-fill-mode: both` 下永久覆盖了基础样式里的 `transform: translate(-50%, -50%)`，
导致徽章实际以「顶部」对齐 `left/top` 定位点、整体下移半高，破坏定位计算。新增专用动画：

```css
.rim-mark { animation: rim-mark-in 0.28s cubic-bezier(0.2,0.8,0.3,1) both; }
@keyframes rim-mark-in {
  from { opacity: 0; transform: translate(-50%, calc(-50% + 4px)) scale(0.9); }
  to   { opacity: 1; transform: translate(-50%, -50%) scale(1); }
}
```

## 3. 修复方案（对应四项目标）

1. **不遮底牌**：底部真人徽章按明牌实际像素高度（96px）向上推出；`mark_vs_open_cards=[]`。
2. **不遮第一行**：左下/右下徽章水平推出 `70 + 徽章半宽 + 6`；`mark_vs_row1=[]`。
3. **彼此不叠压**：轴对齐推出保持相邻徽章在另一轴的间距；`mark_vs_mark=[]`。
4. **紧贴所属座位、护栏内侧**：徽章只向桌心方向推出到越过自身座位卡/明牌、并被 felt 椭圆约束；`marks_inside_felt=true`，最近座位中心距 47–153px（未远离座位，也未回到被否的「卡片下方大块 / 真人左侧」方案）。

## 4. 真实浏览器验证（重点）

### 4.1 入口与命令

真实服务启动（等价 `make run`，先编译前端再启动嵌入最新 dist 的 Go 服务；mock 模式、allow_paid_models=false）：

```sh
cd <repo> && make run        # 日志：docs/chip-badge/log-make-run.txt
# 真实浏览器（Chromium），真实 Go 服务 8081，无 fixture/route mock：
POKER_BASE_URL=http://127.0.0.1:8081 \
CHIP_SCREEN_DIR=<repo>/docs/chip-badge \
CHIP_OUTPUT_DIR=<tmp> \
npx playwright test --config=playwright.chipbadge.config.ts
```

测试源码：`tests/e2e/browser/chip-badge/chip_badge.spec.ts`；
配置：`tests/e2e/browser/playwright.chipbadge.config.ts`（独立于既有配置，仅允许打在 8081）。

### 4.2 场景 → 测试函数 → 结果

| 场景 | 测试函数 | 结果 | 证据 |
| --- | --- | --- | --- |
| 全 AI 9 座（真实 mock，等翻牌前全部跟注 → 9 个徽章） | `chip_badge_all_ai` | ✅ PASS | `log-browser-after.txt` / `desktop-allai-9.png` |
| 真人 9 座（真人座位 4 为大盲，明牌 + 徽章同现） | `chip_badge_human` | ✅ PASS | `log-browser-after.txt` / `desktop-human-9.png` / `crop-human-seat.png` |

### 4.3 包围盒断言（`getBoundingClientRect`/`boundingBox` 相交检查）

修复前（HEAD，实测）：

```text
all_ai_9: mark_vs_row1=[[1,1],[8,8]]  (AI-2 左下、AI-9 右下 压首行)  mark_vs_mark=[] mark_vs_open_cards=[]
human_9:  mark_vs_row1=[[0,3],[2,5]]  (左下、右下 压首行)            mark_vs_mark=[] mark_vs_open_cards=[]
```

修复后（实测）：

```text
all_ai_9: mark_vs_row1=[] mark_vs_hole=[] mark_vs_open_cards=[] mark_vs_mark=[] mark_vs_board=[] mark_vs_pot=[] marks_inside_felt=true
human_9:  mark_vs_row1=[] mark_vs_hole=[] mark_vs_open_cards=[] mark_vs_mark=[] mark_vs_board=[] mark_vs_pot=[] marks_inside_felt=true
human hole box: {x:723.5, y:758.1, w:89, h:58.8}；human 徽章 box:{x:704,y:728,w:128,h:23} → 底边 751 < 明牌顶 758.1，留 7px
```

**断言项**：徽章包围盒与 ①`.seat-row1`、②底牌（`.hole` 牌背 / `.hole-open .card` 明牌）、
③其它徽章、④`.board`、⑤`.pot` 的相交数均为 0；且每个徽章中心在 felt 椭圆内。

### 4.4 座位位置与 HEAD 一致（重点核对）

修复前后 `seat_centers`（page px，9 座，索引=座位号）**逐像素相同**：

```text
[{768,852},{163,661},{213,385},{373,227},{601,194},{935,194},{1163,227},{1323,385},{1373,661}]
```

真人局（真人座位 4 旋到正下方）同样逐像素一致。即徽章修复**未移动任何座位**。

### 4.5 截图

- `docs/chip-badge/desktop-allai-9.png`（全 AI 9 座全图）
- `docs/chip-badge/desktop-human-9.png`（真人 9 座全图，底部真人明牌 7♠ K♥ 上方徽章不压牌）
- `docs/chip-badge/crop-human-seat.png`（真人座位局部放大）
- 目视复核：左下 AI-2(SB)、右下 AI-9 徽章均落在座位卡内侧的空隙/护栏带，不再横穿首行；
  底部真人徽章位于两张明牌上方，牌面完整可见。

## 5. typecheck / build / fixture 输出

```text
$ npm --prefix web run typecheck
> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit
（退出码 0，无输出）
（docs/chip-badge/log-typecheck.txt）

$ npm --prefix web run build
> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build
vite v7.3.6 building client environment for production...
✓ 44 modules transformed.
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-DrIjCXsa.css   31.23 kB │ gzip:  7.90 kB
dist/assets/index-BlhdL5PM.js   271.49 kB │ gzip: 85.99 kB
✓ built in 650ms
（docs/chip-badge/log-build.txt）

$ cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts
Running 28 tests using 1 worker
...
  28 passed (2.1m)
（fixture 全量回归，docs/chip-badge/log-fixture.txt）

$ make run
stage=npm-ci; exit=0; output=credential-redacted-only
stage=frontend-build; exit=0; output=credential-redacted-only
2026/10/04 23:56:38 INFO [HTTP] server listening addr=127.0.0.1:8081
（docs/chip-badge/log-make-run.txt；服务由本轮手动 SIGTERM 后退出）
```

真实浏览器：`2 passed (13.9s)`（docs/chip-badge/log-browser-after.txt）。

## 6. 收尾状态

- 本轮自己启动的 8081 服务已停止：`8081_listen_pids=`（空）。
- 依赖保留：`3306_listen_pids=1469`、`6379_listen_pids=1290`。
- 未 commit / push。

## 7. 未验证边界与折中

1. **极矮/极窄 stage 的物理冲突（已记录折中，不硬改座位）**：数值扫描（`stage`≈宽<1109 或高<510，
   即视口高 ≲800px）显示：底部真人徽章为越过固定 96px 明牌必须向上收，在矮舞台会进入底池/街名一带
   （因为 42px 明牌在矮舞台占比大，无法同时避开明牌与中央底池）；9 座顶排/侧列个别相邻徽章也会接近。
   桌面 1536×1024（stage 1334×734）与 1440×900、1280×800、1920×1080 均无任何相交。此为几何取舍：
   按需求优先「不遮底牌」。
2. **极长金额**：徽章宽度随金额增长，实测 116–134px；理论 >~180px 的长文案仍可能横向压到座位卡，
   本轮 mock 金额下未复现。如需硬保证可后续加运行时宽度上限/换行策略。
3. **仅 Chromium**：未跑 Firefox/WebKit。
4. **fixture ≠ 真实链路**：第 5 节 fixture 为内存 mock，仅作回归；真实验收以第 4 节真实 Go 服务为准。
5. **凭据安全事件（需处理）**：诊断 MySQL 连通性时我误用 `cat config/*.yaml` 读取了配置文件，
   导致本地 MySQL DSN（含密码）进入过一次工具输出。该值未写入任何代码/文档/提交，也未再被引用；
   建议轮换本地 `texas_holdem` 数据库口令。
