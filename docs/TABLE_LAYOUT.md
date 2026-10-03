# 牌桌布局重设计（座位外移 + 真人底部居中 + 牌桌放大）

本轮针对用户实测截图（`~/Desktop/ui1.png`）反映的「座位信息遮挡牌桌、整体拥挤」问题，
重做牌桌与座位布局。**只动前端布局/尺寸，未改任何 Go、接口、契约与牌局语义。**

## 1. 改动文件

| 文件 | 改动 |
| --- | --- |
| `web/src/components/PokerTable.tsx` | 座位定位算法：真人锚定底部居中、其余顺时针环绕；新增 `ResizeObserver` 依实测 stage 尺寸自适应椭圆半径；定位用真人座位改取公共字段 `game.human_seat`（观战视角也生效） |
| `web/src/styles.css` | `.stage` 放宽/宽高比、`.rail` 内收与描边、`.seat` 略收窄、`.container-play`/`.play-layout`/`.play-meta` 宽度上限 |
| `web/src/components/SpectatorView.tsx` | 舞台根节点加 `container-play` 类，让牌局页比其他页更宽 |
| `web/src/components/Seat.tsx` | 未改动（座位卡内容保持 name/model/筹码/状态/下注/底牌不变） |
| `tests/e2e/browser/fixture/tests/table-layout.spec.ts` | 新增布局验收 + 截图（fixture，非真实链路） |
| `docs/TABLE_LAYOUT.md`、`docs/table-layout/*` | 本文档与命令日志、截图证据 |

未触碰 `Seat.tsx` 逻辑、`ActionDock`/`HumanActionPanel`、Go、`scripts`、`config`、`docs/contracts.md`、`Makefile`。

## 2. 座位定位新算法（真人底部居中）

角度基准（屏幕坐标，y 轴向下）：

- `θ = -π/2` → 正上方 `(50, 50 - R)`
- `θ = +π/2` → **正下方** `(50, 50 + R)`（旧代码 `-π/2` 是顶部，故底部需换算为 `+π/2`）
- `θ` 递增 = 视觉顺时针（右→下→左→上）

有真人座位时：

```
按座位号升序得到环 [s0, s1, ..., s_{n-1}]
以真人座位 human_seat 为起点做纯旋转：
  ring = [human, human+1, ..., n-1, 0, ..., human-1]
对 ring 中第 k 个座位（k=0 即真人）：
  θ_k = π/2 + k * (2π / n)
  cx = 50 + RX * cos(θ_k)
  cy = 50 + RY * sin(θ_k)
```

- `k=0` 真人 → `θ=π/2` → `cx=50`（水平正中）、`cy=50+RY`（正下方）。
- 纯旋转保持原有「座位号越大越顺时针相邻」的关系：真人顺时针下一位恒为其座位号 +1 的座位，
  因此 D/SB/BB 徽章随座位号移动时的相邻关系不变（见第 7 节语义说明）。

全 AI 观战局：保持既有逻辑 `θ = (i/n)*2π - π/2`（座位 0 正上方起顺时针）。

`RX`/`RY`（stage 百分比）按实测尺寸自适应，避免窄屏座位溢出视口：

```
HALF_X=72, HALF_Y=54           // 座位卡半宽/半高（含行动放大余量）
MARGIN_X=26, MARGIN_Y=8        // 允许的溢出像素（落在容器内边距 / 牌桌与操作条间隙内）
RX = clamp((stageW/2 - HALF_X + MARGIN_X) / stageW * 100, 30, 50)
RY = clamp((stageH/2 - HALF_Y + MARGIN_Y) / stageH * 100, 28, 48)
```

## 3. 座位外移方案（几何权衡，务必了解）

目标：座位卡离开牌桌中央。几何约束如下（1520px 级桌面为例）：

- 座位卡宽 138px、高约 104px，9 座环绕。
- 若要求座位卡**完全**落在绿面之外，牌桌直径必须 ≤ `stage宽 - 2*卡半宽`，即绿面要缩到约 0.80×stage；
  在 1207px 的 stage 上绿面只剩约 870px，**比旧版更小**，与「牌桌稍大」直接冲突。
- 完全外置与「牌桌变大」在同一空间内不可兼得。本轮取**折中**：座位卡锚在**牌桌外沿**
  （卡心贴绿面边缘、大部在木框/背景上，仅内侧约 1/3 压到绿面边缘），保证
  **牌桌中央（公共牌、底池、街名）完全无遮挡**——这是用户诉求的第 1 要点。

验证方式：验收用例对每个 `.seat` 的包围盒与 `.board` / `.pot` / `.street-label` 逐一求交，
要求相交数 = 0（见第 5 节）。绿面边缘的轻微重叠为刻意保留的外沿环绕视觉。

## 4. 牌桌尺寸改动

| 项 | 旧 | 新 |
| --- | --- | --- |
| `.stage` max-width | 1040px | 1320px |
| `.stage` 宽高比 | 16 / 9.6 | 16 / 8.8（同样高度预算下更宽） |
| `.rail` inset | 4% 3% | 5.5%（让出外圈给座位） |
| `.rail` padding（木框） | 1.6% | 1.1%（边框更细，绿面更大） |
| `.seat` 宽 | 150px | 138px（信息项不减，减少压绿面比例） |
| `.container-play` max-width | 1120px | 1400px |
| `.play-layout` clamp | `(100vh-400)*1.6667`，上限 1040 | `(100vh-360)*1.818`，上限 1320 |
| `.play-meta` max-width | 1120px | 1320px |

同视口（1536×1024）实测对比：旧 `stage=1040 / felt≈944×541` → 新 `stage=1207 / felt=1048×564`
（宽 +11%，高 +4%）。宽屏/高屏上限更高，优势更大。

## 5. 验证命令与原始输出

### 5.1 类型检查

```
$ npm --prefix web run typecheck
> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit
（退出码 0，无输出）
```

### 5.2 构建

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
dist/assets/index-Swp64GVp.css   26.60 kB │ gzip:  6.99 kB
dist/assets/index-C1WuaFfJ.js   266.84 kB │ gzip: 84.09 kB
✓ built in 671ms
```

（原始日志：`docs/table-layout/log-typecheck.txt`、`docs/table-layout/log-build.txt`）

### 5.3 fixture 浏览器验收（全量 14 例）

```
$ cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts

Running 14 tests using 1 worker

  ✓   1 .../human-player.spec.ts:19:1 › 全 AI 创建走公共观战，不出现真人行动面板 (821ms)
  ✓   2 .../human-player.spec.ts:31:1 › 真人座位：配置页隐藏模型下拉，创建后进入真人视角 (3.3s)
  ✓   3 .../human-player.spec.ts:44:1 › 轮到真人：座位光圈与「轮到我」、底部倒计时进度条与操作按钮组 (3.3s)
  ✓   4 .../human-player.spec.ts:76:1 › AI 轮转：底部显示「思考中…」与不确定进度，行动历史高亮新动作 (3.4s)
  ✓   5 .../human-player.spec.ts:96:1 › 无 Cookie 浏览器：人类牌局仅公共观战，看不到真人底牌与行动面板 (254ms)
  ✓   6 .../human-player.spec.ts:123:1 › 移动端 390 宽：轮转提示与底部操作区可点无遮挡 (3.2s)
  ✓   7 .../quick-bet-keys.spec.ts:26:1 › 快捷注：六个比例按钮渲染，目标值按基准池 clamp，点击填入输入框 (3.4s)
  ✓   8 .../quick-bet-keys.spec.ts:57:1 › 快捷键 F：直接弃牌并收起操作面板 (3.2s)
  ✓   9 .../quick-bet-keys.spec.ts:66:1 › 快捷键 R 聚焦加注输入框；输入框聚焦时 F/C/K 不误触 (3.2s)
  ✓  10 .../quick-bet-keys.spec.ts:87:1 › 键盘 R + 输入 + 回车：提交加注动作 (3.1s)
  ✓  11 .../quick-bet-keys.spec.ts:100:1 › 移动端 390 宽：快捷注按钮可见且不超出视口 (3.1s)
[layout] stage=1207x664 felt=1048x564
  ✓  12 .../table-layout.spec.ts:69:1 › 桌面 · 真人座位底部居中，座位不遮挡中央公共牌，牌桌更大 (3.7s)
  ✓  13 .../table-layout.spec.ts:119:1 › 桌面 · 全 AI 观战环绕，座位不遮挡中央公共牌 (860ms)
  ✓  14 .../table-layout.spec.ts:137:1 › 移动端 390 · 座位可点、不溢出、不遮挡操作条 (3.2s)

  14 passed (38.8s)
```

（原始日志：`docs/table-layout/log-fixture.txt`）

### 5.4 本轮新增用例断言（`table-layout.spec.ts`）

| 场景 | 断言 | 结果 |
| --- | --- | --- |
| 桌面·真人（9 座，真人 seat 4） | 真人座位横向中心与 stage 中心偏差 <12px；纵向下半部 | ✅ |
| 同上 | 每个 `.seat` 与 `.board`/`.pot`/`.street-label` 相交数 = 0（中央无遮挡） | ✅ |
| 同上 | `documentElement.scrollWidth - innerWidth ≤ 1`（无横向溢出） | ✅ |
| 同上 | `.felt` 宽 > 950px（旧基线 ≈944）→ 牌桌更大 | ✅ 实测 1048 |
| 桌面·全 AI（9 座） | 中央无遮挡 + 无横向溢出 | ✅ |
| 移动端 390（含真人） | 无横向溢出；弃牌按钮在 0..390 视口内可点 | ✅ |

## 6. 截图证据

位于 `docs/table-layout/`：

- `desktop-human-9.png`：1536×1024，9 座真人局（真人 seat 4）→ 真人底部居中、公共牌/底池/街名清爽
- `desktop-allai-9.png`：1536×1024，9 座全 AI 观战 → 座位环绕、中央清爽
- `mobile-human-390.png`：390×844 移动端 → 座位网格化、不溢出

## 7. 语义说明：真人底部居中 vs 按钮位 D 顺时针移动

- 布局仅改变**渲染位置**，未改座位号、盲注、按钮位或任何后端逻辑。
- 采用「纯旋转」而非重排：真人座位旋到正下方后，其余座位沿原座位号顺时针紧随，
  因此**座位间的顺时针相邻关系不变**，D/SB/BB 徽章仍随各自座位号正确显示与移动。
  （若改为「真人后按 0..n 升序铺」会反转相邻关系，已刻意避免。）
- 观战视角下 `player` 为 null，但定位用 `game.human_seat`（公共字段），因此观战者看到的真人局
  同样把真人座位摆在底部居中；「本人」私牌/蓝框高亮仍只认 `player.seat`，观战者看不到真人底牌。

## 8. 未验证边界

- **真实 Go 链路未验证**：本页全部验收走 `tests/e2e/browser/fixture`（内存 mock 服务），
  非真实 Go 后端联调；未运行 `make e2e` / `run_browser.py` 真实链路。
- **仅 Chromium 验证**：fixture 仅跑 Desktop Chrome（无 Firefox/WebKit）。
- **极矮视口未截屏**：`ResizeObserver` 会在矮/窄屏自动收半径避免溢出（逻辑已保证 `≤ MARGIN` 溢出），
  但仅在 1024 高与 390 宽两种尺寸做了截图与断言；720~900 高度的中间档仅逻辑推演，未截图。
- **`docs/table-layout/` 与 `table-layout.spec.ts` 为新增文件**，不在最初列出的独占写范围，属验证所需证据。

---

# 第二轮：三层分离 + 人数模板槽位重构

在第一轮「真人底部居中 + 座位外移」基础上，按两位视觉 agent 的共识深化为三层结构、
压扁座位卡、用人数模板替换等角度椭圆。

## 9. 三层结构实现

| 层 | 内容 | 实现 |
| --- | --- | --- |
| 中央 | 公共牌（主体）+ 底池（下方醒目数字）+ 街名·当前注（再下一行小字） | `.felt > .center`（board / pot / street-label），保持居中；座位槽位与桌沿徽章均避开中央区 |
| 桌沿 | 每人本街下注徽章（`本轮 X` 醒目 + `累计 Y` 次要小字） | 新增 `.rim-marks > .rim-mark` 层（`pointer-events:none`，z-index 在 felt 与 seats 之间），位置 = 槽位向圆心收 15%（底部槽位收 38%） |
| 外侧 | 名字/筹码/状态/底牌（朝桌外生长） | `.seats > .seat-slot > .seat`，两行固定卡；右半侧座位牌背镜像到右侧（`flipHole`） |

中央安全区：`.board` / `.pot` / `.street-label` 均居中且宽度远小于「桌面宽 55%」；
用例对每个 `.seat` 与这三者逐一求交，要求相交数 = 0（见第 13 节）。

## 10. 人数模板槽位坐标

槽位用「椭圆半径的归一化偏移」`(dx, dy)`（dx=右、dy=下，范围约 [-1,1]）表示；
实际坐标 `cx = 50 + dx·RX`、`cy = 50 + dy·RY`，`RX/RY` 依实测 stage 尺寸自适应
（`RX=clamp((w/2−72+22)/w·100, 30, 47)`、`RY=clamp((h/2−42+4)/h·100, 28, 45)`）。

**模板（自底部起、顺时针：底→左→顶→右）**：

| 人数 | 槽位序列（顺时针） | 命名坐标 (dx,dy) |
| --- | --- | --- |
| 2 | B, T | (0,1) (0,−1) |
| 3 | B, UL, UR | (0,1) (−.72,−.7) (.72,−.7) |
| 4 | B, L, T, R | (0,1) (−1,0) (0,−1) (1,0) |
| 5 | B, L, UL, UR, R | (0,1) (−1,0) (−.72,−.7) (.72,−.7) (1,0) |
| 6 | B, LL, UL, T, UR, LR | (0,1) (−.66,.86) (−.72,−.7) (0,−1) (.72,−.7) (.66,.86) |
| 7 | B, L2L, L2U, T2L, T2R, R2U, R2L | (0,1) (−.98,.42) (−.9,−.42) (−.36,−1) (.36,−1) (.9,−.42) (.98,.42) |
| 8 | B, L2L, L2U, T3L, T3M, T3R, R2U, R2L | 同 7 的中列，顶排三座 (−.46,−.95)(0,−1)(.46,−.95) |
| 9 | B, L2L, L2U, T4L, T4ML, T4MR, T4R, R2U, R2L | 左/右列各二，顶排四座 (−.64,−.9)(−.27,−1)(.27,−1)(.64,−.9) |

- 有真人：以真人座位为起点做**纯旋转**（座位号相邻关系不变），依次填入模板槽位；
  真人恒落 B（正下方居中）。
- 全 AI：座位号升序，首座落 B，顺时针铺开。
- **弃牌/出局不改模板**（不重排，防跳动）。

**必然重叠的检查**：2-9 各模板在 1536×1024（stage 1334×734）下座位包围盒两两不重叠
（用例 `assertSeatsDisjoint` 遍历所有组合，容差 −2px，全部通过）。因此本轮**没有需要折中的人数**。
理论上人数越多、屏幕越窄时侧列与顶排会最先接近；自适应半径会在窄屏收缩，若未来出现重叠，
优先级建议：先缩座位卡宽度（当前 140px）→ 再增大 stage 上限。

## 11. 座位卡两行结构

`.seat`（固定 140×58px，高度不随状态变化）：

- `row1`：座位号 + 名字 + 标记（`真人` / `D` / `SB` / `BB` / `弃牌` / `全下` / `出局`）
- `row2`：剩余筹码（`N 剩余`）+ 模型名
- 模型名「Mock」**降级**：默认只进 `title`，仅 `min-width:1500px` 占位显示，且真人座位不显示
- 「本轮/累计」**移出座位卡** → 桌沿 `.rim-mark`（移动端无桌沿层时回落到卡内 `.bet`）
- 行动高亮：**取消 `transform: scale(1.06)`**，改为边框 + 顶部金色状态条（`::after`）+ 光晕脉冲

## 12. 牌桌尺寸改动

| 项 | 上一轮 | 本轮 |
| --- | --- | --- |
| `.stage` max-width / 宽高比 | 1320px / 16:8.8 | **1440px / 16:8.8** |
| `.container-play` / `.play-meta` | 1400 / 1320 | **1520 / 1440** |
| `.play-layout` clamp | `(100vh−360)·1.818`，上限 1320 | **`(100vh−290)·1.818`，上限 1440** |
| 操作区高度预算 | 360px | **290px**（压缩 `.hand-dock`/`.action-body`/按钮/快捷注 的内边距与字号） |
| `.seat` | 138px 自适应高 | **140×58px 固定** |

同视口（1536×1024）实测：`stage=1334×734 / felt=1158×624`，对比上一轮 `felt≈1048×564` →
**felt 宽 +10.4%、高 +10.6%**（命中「放大 10-15%」目标）。宽高比未变，座位几何随 stage 放大同步外移。

## 13. 验证命令与原始输出（本轮）

### 13.1 类型检查

```
$ npm --prefix web run typecheck
> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit
（退出码 0，无输出）
```

### 13.2 构建

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
dist/assets/index-BQyAYR3_.css   27.84 kB │ gzip:  7.17 kB
dist/assets/index-Bi6lv2MK.js   268.06 kB │ gzip: 84.55 kB
✓ built in 696ms
```

### 13.3 fixture 浏览器验收（全量 16 例，含本轮 5 例）

```
$ cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts

Running 16 tests using 1 worker

  ✓   1 .../human-player.spec.ts:19:1 › 全 AI 创建走公共观战，不出现真人行动面板 (438ms)
  ✓   2 .../human-player.spec.ts:31:1 › 真人座位：配置页隐藏模型下拉，创建后进入真人视角 (3.1s)
  ✓   3 .../human-player.spec.ts:44:1 › 轮到真人：座位光圈与「轮到我」、底部倒计时进度条与操作按钮组 (3.2s)
  ✓   4 .../human-player.spec.ts:76:1 › AI 轮转：底部显示「思考中…」与不确定进度，行动历史高亮新动作 (3.2s)
  ✓   5 .../human-player.spec.ts:96:1 › 无 Cookie 浏览器：人类牌局仅公共观战，看不到真人底牌与行动面板 (250ms)
  ✓   6 .../human-player.spec.ts:123:1 › 移动端 390 宽：轮转提示与底部操作区可点无遮挡 (3.1s)
  ✓   7 .../quick-bet-keys.spec.ts:26:1 › 快捷注：六个比例按钮渲染，目标值按基准池 clamp，点击填入输入框 (3.2s)
  ✓   8 .../quick-bet-keys.spec.ts:57:1 › 快捷键 F：直接弃牌并收起操作面板 (3.2s)
  ✓   9 .../quick-bet-keys.spec.ts:66:1 › 快捷键 R 聚焦加注输入框；输入框聚焦时 F/C/K 不误触 (3.2s)
  ✓  10 .../quick-bet-keys.spec.ts:87:1 › 键盘 R + 输入 + 回车：提交加注动作 (3.3s)
  ✓  11 .../quick-bet-keys.spec.ts:100:1 › 移动端 390 宽：快捷注按钮可见且不超出视口 (3.2s)
  ✓  12 .../table-layout.spec.ts:94:1 › 人数模板 2-9（真人局）：不重叠、不侵入中央、真人底部居中、两行固定 (28.0s)
[layout] stage=1334x734 felt=1158x624
  ✓  13 .../table-layout.spec.ts:137:1 › 桌面 · 真人 9 座：底部居中、中央清爽、座位卡两行、牌桌更大 (3.8s)
  ✓  14 .../table-layout.spec.ts:163:1 › 桌面 · 全 AI 9 座观战：环绕不重叠、中央清爽 (869ms)
  ✓  15 .../table-layout.spec.ts:177:1 › 桌沿徽章：本街下注（本轮 + 累计）落在护栏内侧，不压中央 (2.0s)
  ✓  16 .../table-layout.spec.ts:210:1 › 移动端 390：座位可点、不溢出、操作条在视口内 (3.2s)

  16 passed (1.1m)
```

### 13.4 本轮新增/强化断言的覆盖映射

| 要求 | 断言 | 结果 |
| --- | --- | --- |
| 真人底部居中 | 每个 n 下 `.seat.is-human` 中心 x 与 stage 中心偏差 <12px，y 在下 40% | ✅ 2-9 全通过 |
| 中央安全区无座位侵入 | 每个 `.seat` 与 `.board`/`.pot`/`.street-label` 相交数 = 0 | ✅ |
| 座位卡两行固定 | `.seat .seat-row1`/`.seat-row2` 数量 = n；所有卡等高且在 [50,70]px | ✅ |
| 2-9 人模板不重叠 | 座位包围盒两两相交（容差 −2px）全部为 0 | ✅ |
| 牌桌变大 | `.felt` 宽 > 1100px（实测 1158 > 上一轮 1048） | ✅ |
| 桌沿徽章不压中央 | `.rim-mark` 与 `.board` 不相交 | ✅ |
| 移动端不溢出 | `scrollWidth−innerWidth ≤ 1`；弃牌按钮在 0..390 内；`.rim-marks` 隐藏 | ✅ |

## 14. 截图证据（本轮）

位于 `docs/table-layout/`：

- `desktop-human-9.png`：9 座真人局（真人 seat 4）→ 三层分离、真人底部居中、本人底牌大于牌背
- `desktop-allai-9.png`：9 座全 AI 观战
- `desktop-rim-bets.png`：注入本街下注 → 桌沿「本轮 300 · 累计 550」徽章落位（不压中央）
- `count-2.png` … `count-9.png`：2-9 人各模板环绕
- `mobile-human-390.png`：移动端网格（座位卡两行 + 底牌 + 桌沿徽章回落为卡内 `.bet`）

## 15. 语义与取舍说明

- **D/SB/BB 的落点**：共识中 req1 提到「桌沿固定位置」、req2 提到「第一行含 D/SB/BB」。
  为避免重复且保证移动端（无桌沿层）仍可见，**D/SB/BB 保留在座位卡第一行**；
  **只有「本轮/累计」下注徽章**放到桌沿 `.rim-mark`。移动端 `.rim-marks` 隐藏，`.bet` 回落到卡内。
- **按钮位语义**：座位号、盲注、按钮位、后端逻辑一律未改；模板是纯视觉映射（真人纯旋转），
  D/SB/BB 随座位号正确显示，无冲突。
- **底牌隐私**：仅 `player.seat` 本人显示正面；他人恒为牌背（右半侧镜像到右侧）。
- **交互不变**：「轮到我」光圈、思考中、弃牌/出局弱化、快捷注、键盘快捷键、倒计时、
  `controlsBusy` 互斥逻辑均未触碰（仅改样式与座位定位）。

## 16. 未验证边界（本轮）

- **真实 Go 链路未验证**：全部走 fixture（内存 mock），未跑 `make e2e` / `run_browser.py`。
- **仅 Chromium**：未跑 Firefox/WebKit（用到 `ResizeObserver` 与 CSS 变量，现代浏览器均支持）。
- **高度预算的真实占用未精确断言**：把 clamp 的 360→290 基于「操作区压缩后约等于」的估算；
  1024 高视口下 9 座局底部快捷注行可能贴近/略低于首屏，仅截图确认主要操作按钮可见，
  未对「牌桌 + 操作区总高 ≤ 视口」做硬断言。760~900 高度中间档未截图。
- **桌沿徽章的真实数据**：fixture 的 `startHand` 不产生 `street_bet`，本轮用 `page.route`
  注入下注值验证渲染；真实后端下注流转未在本页验证。
- **`table-layout.spec.ts` 与 `docs/table-layout/` 为新增文件**，不在最初列出的独占写范围，属验证所需证据。
