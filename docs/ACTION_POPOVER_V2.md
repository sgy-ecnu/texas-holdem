# 动作大尺寸弹出特效 + 下注徽章就近避让（V2）

实现依据：`/Users/gaosper/.local/share/opencode/tool-output/tool_1077afb6e001ytFGi8utiPTdA6`（架构师冻结方案，475 行）。
本轮为前端实现 + fixture 浏览器验证；**未做真实 Go 链路联调**（见「未验证边界」）。

## 一、改动文件与落地位置

| 文件 | 状态 | 落地内容 |
|---|---|---|
| `web/src/components/ActionPopover.tsx` | 重写 | 四动作统一参数；`HINT_DURATION_MS=2200`（:15）、`HINT_FALLBACK_MS=2300`（:17）、`KIND_CLASS` 四色映射（:29-34）、`ActionPlacement` 展开方向类型（:41）、`onAnimationEnd→onDone`（:58） |
| `web/src/components/PokerTable.tsx` | 改 | 顶部动作区纵向预留 `TOP_BAND=150`（:127）；`RY` 依此压缩（:132）；`rimAt` 徽章就近避让算法（:145-174）；两行徽章 markup（:218-227）；`isBottomCenter` 展开方向（:267-277）；`onHintDone` 透传（:16,:306） |
| `web/src/components/SpectatorView.tsx` | 改 | `usePrefersReducedMotion`（:25-38）；每座位 FIFO（`activeRef/queueRef/timerRef`，:74-80）；`advance`（:83）、`pump`（:102）、`popHint` 去重入队（:125）；跨手不清空（:141-153） |
| `web/src/styles.css` | 重写相关段 | 层级（:518 `isolation`、:588 center 40、:709 turn-badge 50、:864 hole 40）；动作特效（:929-1041）；下注徽章（:1041-1085）；移动端（:2016/:2027/:2031）；reduced-motion |
| `tests/e2e/browser/fixture/tests/action-popover-scale.spec.ts` | 重写 | V2 动作特效 RED/验收 |
| `tests/e2e/browser/fixture/tests/action-popover.spec.ts` | 改 | 契约变化同步：四色类名、同座位 FIFO、跨手不清、reduced-motion 2200ms |
| `tests/e2e/browser/fixture/tests/bet-badge-v2.spec.ts` | 新增 | 徽章两行 + 就近避让 + 互不叠压 + 不遮保护区 |
| `web/src/components/Seat.tsx` | 未改 | 现有 `.seat` / `.hole-open` / `.turn-badge` / `.seat-row1` 已提供测量锚点，无需改动 |

未触碰：Go、`scripts/`、`config/`、`web/src/format.ts`、`web/src/types.ts`（action 字段不动）。

## 二、动作特效落地

- **统一参数**：`--pop-scale: 1.8`（styles.css:934），四种动作共用；动画 `action-pop 2200ms both`（:951）。
- **时间轴**（`@keyframes action-pop`，:1003 起，按 2200ms 百分比换算）：
  - `0%` opacity 0.35、scale 1.8，timing `cubic-bezier(0.2,0.8,0.3,1)`
  - `3.636%`(=80ms) opacity 1、scale 1.8
  - `14.545%`(=320ms) scale 1.8，timing `cubic-bezier(0.4,0,0.2,1)`
  - `41.818%`(=920ms) scale 1
  - `90%`(=1980ms) scale 1，timing `ease-in`
  - `100%`(=2200ms) opacity 0、scale 1
- **四色**（只区分动作，尺寸统一）：`is-fold` `#30383D/#F3F5F7/#AAB4BC`、`is-check` `#183D46/#E0F8FF/#82D5E8`、`is-call` `#594216/#FFF0B8/#E9C768`、`is-raise` `#F2CD67/#241B07/#FFF2BB`；均带黑色落影 `0 6px 16px 0.45` + 同色光晕 12px 0.25（:954-978）。字号 16/字重 700/内边距 7×12/边框 2px/圆角 12/最小宽 80（:934-951）。
- **展开方向**（仅缩放、固定锚点）：
  - `is-above-left`：`right:0; bottom:calc(100%+27px); transform-origin:right bottom`（左半桌，:980）
  - `is-above-right`：`left:0; bottom:...; origin:left bottom`（右半桌，:985）
  - `is-above-center`：`left:50%; translate:-50% 0; origin:center bottom`（正顶单座，:990）
  - `is-right`：`left:calc(100%+12px); top:50%; translate:0 -50%; origin:left center`（底部真人，:997）
- **文案**：沿用 `actionPopoverLabel`，长金额允许换行（`white-space:normal; overflow-wrap:anywhere`），不截断/省略/缩小。

## 三、下注徽章两行 + 避让定位算法

- **两行结构**（PokerTable.tsx:223-227）：`.rim-line1`「● 本轮 X」（12px/700/`#F3D98B`，筹码图标 10px）+ `.rim-line2`「累计 Y」（11px/500/`#B9C4BD`）；行高 16、间距 2、内边距 5×9、圆角 10、背景 `#0B1712`、边框 1px `#526044`、`max-width:160px`（≈`min(180, 1.15×140)`）。
- **避让算法**（`PokerTable.tsx:145-174`，纯解析、随实测 stage 尺寸换算像素）：
  - 顶部/左右侧座位：`badge.centerX = seat.centerX`，`badge.top = 卡片底边 + 8px`（卡片下方附属区），CSS `translate(-50%,0)`。
  - 本人（底部真人）座位：`badge.right = 卡片左边缘 − 12px`、纵向居中，CSS `translate(-100%,-50%)`；宽度自适应。
  - 左右侧座位若卡片下方将越出 stage 底边：左半 → 卡片右侧、右半 → 卡片左侧，最近边缘仍 8px（`anchor-right/anchor-left`）。
  - 徽章与动作均 `pointer-events:none`；靠实际定位让开，不靠 z-index 硬盖。
- **层级**（styles.css）：牌桌背景 0 / 下注徽章 20（:1041 区 `.rim-marks z-index:20`）/ 座位卡 30（`.seats`）/ 底牌·公共牌·底池 40（`.hole`、`.center`）/ 行动状态标签 50（`.turn-badge`）/ 动作特效 60（`.action-popover`）；`.stage { isolation:isolate }` 建立独立 stacking context。

## 四、生命周期 FIFO

`SpectatorView.tsx`：
- 每座位一个 FIFO：`activeRef`（正在展示）+ `queueRef`（等待）+ `timerRef`（兜底）。
- 入队 `popHint`：同一动作 ID（game.id+hand_no+sequence）已在展示/队列则忽略；否则入队，若该座位空闲则 `pump`。
- `pump`：取队首展示，动画 2200ms；`animationend`（ActionPopover.onDone→advance）正常清理，`HINT_FALLBACK_MS=2300` 兜底并校验动作 ID。
- 同座位新动作在旧动作展示期间只排队，不截断；不同座位并行。
- 跨手：`hand_no` 递增不再清空，已开始标签完成剩余展示；仅在组件卸载时清 timer。
- reduced-motion：`usePrefersReducedMotion` 命中时 `fallbackMs=HINT_DURATION_MS=2200`，CSS `animation:none`、`transform:none`、`opacity:1` 静态高对比展示。

## 五、验证证据

### typecheck（`npm --prefix web run typecheck`）
```text

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

```
退出码 0。

### build（`npm --prefix web run build`）
```text

> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
transforming...
✓ 44 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-CH_hVpkC.css   31.46 kB │ gzip:  7.96 kB
dist/assets/index-DAkPFPCr.js   270.98 kB │ gzip: 85.67 kB
✓ built in 681ms
```
退出码 0。

### fixture 全量（`npx playwright test --config=playwright.fixture.config.ts`，SCREEN_DIR=docs/action-popover-v2）
```text
Running 33 tests using 1 worker

[dock] desktop height=139
  ✓   1 action-dock.spec.ts:41:1 › 桌面 · 三行紧凑结构、总高下降、无重复信息、按钮≥40px
[dock] thinking height=54
  ✓   2 action-dock.spec.ts:94:1 › 桌面 · 非我行动（思考中）时操作区仅剩状态行
[dock] mobile height=202
  ✓   3 action-dock.spec.ts:109:1 › 移动端 390 · 各行堆叠、触控目标≥40px、不溢出
  ✓   4 action-popover-scale.spec.ts:150:1 › 桌面：四种动作统一 scale 1.8 / 2200ms / 四色
  ✓   5 action-popover-scale.spec.ts:216:1 › 桌面：精确时间轴（80/320/620/920/1980ms）
  ✓   6 action-popover-scale.spec.ts:247:1 › 桌面：展开方向 transform-origin（左半/右半/顶部/真人）
  ✓   7 action-popover-scale.spec.ts:288:1 › 移动端 390：scale 仍 1.8、不横向溢出
  ✓   8 action-popover-scale.spec.ts:311:1 › reduced-motion：静态高对比展示约 2200ms
  ✓   9 action-popover.spec.ts:131:1 › 去重触发 + 同座位 FIFO 不截断 + 不同座位并存 + 跨手不清 + 弃牌不变暗 + 标签定位
  ✓  10 action-popover.spec.ts:218:1 › reduced-motion：标签降级为静态可见，随后由 timer 移除
  ✓  11 action-popover.spec.ts:247:1 › 人数模板 2/6/9（全 AI）：标签不压中央、不互相重叠、无横向溢出
  ✓  12 action-popover.spec.ts:294:1 › 移动端 390：标签落在预留行、不压 .bet、不溢出
  ✓  13 bet-badge-clearance.spec.ts:93:1 › 桌面：下注徽章不遮真人底牌/座位卡（1536/1440/1280 多视口）
  ✓  14 bet-badge-clearance.spec.ts:118:1 › 桌面 9 座密集：每个下注徽章不压自身座位卡
  ✓  15 bet-badge-clearance.spec.ts:132:1 › 移动端 390：座位卡内 .bet 不遮底牌/第一行、无横向溢出
  ✓  16 bet-badge-v2.spec.ts:50:1 › 桌面 9 座：徽章两行结构、互不叠压、不遮座位卡/底牌/中央
  ✓  17 bet-badge-v2.spec.ts:148:1 › 桌面：本人座位徽章在卡片左侧、离卡片 8~16px、纵向居中
  ✓  18 human-player.spec.ts:19:1 › 全 AI 创建走公共观战，不出现真人行动面板
  ✓  19 human-player.spec.ts:31:1 › 真人座位：配置页隐藏模型下拉，创建后进入真人视角
  ✓  20 human-player.spec.ts:44:1 › 轮到真人：座位光圈与「轮到我」、底部倒计时进度条与操作按钮组
  ✓  21 human-player.spec.ts:76:1 › AI 轮转：底部显示「思考中…」与不确定进度，行动历史高亮新动作
  ✓  22 human-player.spec.ts:96:1 › 无 Cookie 浏览器：人类牌局仅公共观战，看不到真人底牌与行动面板
  ✓  23 human-player.spec.ts:123:1 › 移动端 390 宽：轮转提示与底部操作区可点无遮挡
  ✓  24 quick-bet-keys.spec.ts:26:1 › 快捷注：六个比例按钮渲染，目标值按基准池 clamp，点击填入输入框
  ✓  25 quick-bet-keys.spec.ts:57:1 › 快捷键 F：直接弃牌并收起操作面板
  ✓  26 quick-bet-keys.spec.ts:66:1 › 快捷键 R 聚焦加注输入框；输入框聚焦时 F/C/K 不误触
  ✓  27 quick-bet-keys.spec.ts:87:1 › 键盘 R + 输入 + 回车：提交加注动作
  ✓  28 quick-bet-keys.spec.ts:100:1 › 移动端 390 宽：快捷注按钮可见且不超出视口
  ✓  29 table-layout.spec.ts:94:1 › 人数模板 2-9（真人局）：不重叠、不侵入中央、真人底部居中、两行固定
[layout] stage=1334x734 felt=1158x624
  ✓  30 table-layout.spec.ts:137:1 › 桌面 · 真人 9 座：底部居中、中央清爽、座位卡两行、牌桌更大
  ✓  31 table-layout.spec.ts:163:1 › 桌面 · 全 AI 9 座观战：环绕不重叠、中央清爽
  ✓  32 table-layout.spec.ts:177:1 › 桌沿徽章：本街下注（本轮 + 累计）落在护栏内侧，不压中央
  ✓  33 table-layout.spec.ts:210:1 › 移动端 390：座位可点、不溢出、操作条在视口内

  33 passed (2.2m)
```

### 截图证据（`docs/action-popover-v2/`）
- 四种动作 / 四色：`v2-desktop-four-actions.png`
- 时间轴：`v2-desktop-timeline.png`
- 展开方向：`v2-desktop-origins.png`
- 真人加注（右侧展开）：`v2-desktop-four-actions.png`（seat0 `加注到 300`）
- 9 人顶部四座下注：`v2-desktop-9seat-badge.png`
- 本人下注左移：`v2-desktop-own-badge.png`
- 移动端：`v2-mobile-raise.png`
- reduced-motion：`v2-reduced-motion.png`
- 2/6/9 人动作共存：`count-2-popover.png` / `count-6-popover.png` / `count-9-popover.png`

## 六、逐条验收场景对齐

| 场景 | 测试函数 | 实际结果 |
|---|---|---|
| 四种动作分别触发：1.8 起、320ms 后缩回、920ms 正常、2200ms 结束 | `action-popover-scale.spec.ts › 桌面：精确时间轴` + `四种动作统一 scale 1.8` | 通过（40/80/200/320=1.8；620∈(1.02,1.8)；920/1500/1980=1.0） |
| 四个顶部座位同时触发：动作互不覆盖、下注互不叠压、归属明确 | `bet-badge-v2.spec.ts › 桌面 9 座`；`action-popover.spec.ts › 人数模板` | 通过（徽章互不叠压、同轴、不遮保护区） |
| 真人加注 2,400/累计 2,800：下注在左距卡片 12px、动作在右、底牌完整 | `bet-badge-v2.spec.ts › 本人座位徽章`；`action-popover-scale.spec.ts`（seat0 is-right） | 通过（gap 6~18px、纵向居中、不压底牌） |
| 长金额、双行动作：字号/scale 不变、不省略、最大范围不碰其他元素 | `action-popover-scale.spec.ts › 移动端`（`加注到 123,456`） | 通过（scale 仍 1.8、无横向溢出）；桌面长金额换行为设计允许，未做极长金额×顶部四座穷举 |
| 下一座位在上一起未结束时行动：两标签完整共存 | `action-popover.spec.ts › 不同座位并存`（count=2） | 通过 |
| 同座位连续动作：每个完整展示、不被替换截断 | `action-popover.spec.ts › 同座位 FIFO 不截断` | 通过（旧文案保持，之后接新文案，count 始终 1） |
| 弃牌后卡片弱化：特效仍高对比、不继承透明度 | `action-popover.spec.ts › 弃牌不变暗` | 通过（opacity>0.9，标签与 `.seat` 同级） |
| 2–9 人桌、真人与观战视角：顺序稳定、真人一致、无保护区碰撞 | `table-layout.spec.ts` 全量 + `action-popover.spec.ts › 2/6/9` | 通过 |
| stage 宽度 1536/1440/1280/390：空间不足改排布不缩小特效、不裁切、不挡操作条 | `bet-badge-clearance`（多视口）、`action-popover-scale › 移动端`、`table-layout › 移动端` | 通过（移动端一列排布、scale 仍 1.8、无横向溢出） |
| 动画中缩放窗口、跨手：布局仍有效、已开始动画不无故截断 | `action-popover.spec.ts › 跨手不清` | 通过（跨手不清空） |
| 连续动作 sequence 存在跳号：验证补取机制 | 无 | **未验证**：数据链路仅 `last_action`，无增量补取来源 |
| reduced-motion：静态可读 2200ms、不瞬间消失 | `action-popover-scale › reduced-motion`；`action-popover › reduced-motion` | 通过（animation none、1900ms 仍可见、约 2200ms 移除） |

## 七、未验证边界与折中

1. **真实 Go 链路未联调**：本轮仅 fixture + Chromium（`web/dist` 构建产物），未运行 `tests/e2e/browser/run_action_popover.py`（真实 Go 嵌入 Web + MySQL/Redis）。因此「真实宿主链路」未验证。
2. **动作补取机制未验证**：后端只下发 `last_action`，轮询间隔内多个动作可能只拿到最后一个；方案 7.3 要求的「按 sequence 连续补取」依赖数据链路，本轮未核验/未实现，**不能宣称不漏动作**。
3. **顶部四座「1.8×最大双行标签」不越出单元**：`TOP_BAND=150` 只保证单行/常见双行在 stage 内不越顶、不压保护区的实测；顶部动作区按 5.3 公式（`actionWidth ≤ (L−24)/1.8`）尚未做成严格单元隔离，极长双行金额仍可能触及相邻单元。属最小可行折中，特效强度未改弱。
4. **底部正中座位动作方向折中**：全 AI 局的底部正中座位（无真人）上方即中央保护区，若按 `above-center` 放大 1.8 倍会压到 `.street-label`（已由 `2/6/9` 用例暴露）。折中：底部正中座位改走右侧展开（`is-right`），与底部真人同规则；这样牺牲了「底部普通座位向上展开」的字面描述，但保证不侵入保护区且特效强度不变。n=6 场景中左右下角座位与底部正中动作在**极长金额**下可能水平相邻，未做连线级碰撞穷举。
5. **徽章长金额宽度**：`max-width` 用 `min(180,1.15×座位卡宽)` 近似，未按 5.4 的「所属可用区域宽度」做逐座位单元预算；顶部四座在金额长度接近上限时是否保持 12px 间隔未穷举（现用例用 2,400/2,800 实测通过）。
6. **未做**：录屏核对慢缩回观感、80/320/620/920/1980ms 的像素级包围盒采样（现用 Web Animations 定格 scale 断言）、1280/1180/1024/768 的逐档几何检查；`run_action_popover.py` 未执行。

不 commit / 不 push（按规则停在未提交状态）。
