# ActionPopover 动作标签归属/贴边修复（arc-12 方案）证据

入口：纯前端实现（React + 原生 CSS）。范围仅：
`web/src/components/ActionPopover.tsx`、`web/src/components/PokerTable.tsx`、`web/src/styles.css`、
`tests/e2e/browser/fixture/tests/**`（契约同步）。
不碰 Go / scripts / config / Seat.tsx / SpectatorView.tsx / settlement / types.ts / format.ts。
未读取 `.env.local` / `config.local.yaml` 的值；本轮全程未出现任何凭据。

## 1. 根因（arc-12 已定位）

动作标签数据归属本来就是对的（`hints.get(seat.seat)` 渲染进对应 `.seat-slot`），
缺的是**视觉归属**：标签没有指向、没有尖角、位置浮在座位间隙。
旧规则里 AI-4（右上 UR，`is-above-right` 向上/右展开）和 AI-5（正右 R，`is-above-right`）
会落进同一片空隙，看起来像一组、分不清是谁。

## 2. 实现

### 2.1 标签内侧固定贴边：位置分派

标签锚定边由座位槽位方向决定（`PokerTable.tsx:362`）：

```ts
const anchor: ActionAnchor = dir === 'up' ? 'below' : slot.dx > 0.001 ? 'left' : 'right'
```

- `dir` 为下注附属条方向（`barDir`，`PokerTable.tsx:107-110`），标签落在其**相反（朝桌心）一侧**。
- 顶部座位（`dir==='up'`）→ `anchor-below`：贴卡片下沿居中，向下朝桌内展开。
- 正右/右下座位（`slot.dx > 0`）→ `anchor-left`：贴卡片左侧、垂直居中。
- 正左/左下/正下座位（`slot.dx <= 0`）→ `anchor-right`：贴卡片右侧、垂直居中（底部座位避开上方底牌）。

分派接线（file:line）：

| 位置 | 文件:行 |
|---|---|
| 锚定边计算 | `web/src/components/PokerTable.tsx:362` |
| 锚定边类型定义 | `web/src/components/ActionPopover.tsx:48` |
| 渲染 class `anchor-${anchor}` | `web/src/components/ActionPopover.tsx:64` |
| 传入 `anchor` | `web/src/components/PokerTable.tsx:432` |

CSS 位置（`web/src/styles.css`）：

| 锚定边 | 规则 | 行为 |
|---|---|---|
| `anchor-below` | `styles.css:997-1004` | `top: calc(100% + 6px)`、`left:50%`、`translate:-50% 0` |
| `anchor-right` | `styles.css:1005-1012` | `left: calc(100% + 6px)`、`top:50%`、`translate:0 -50%` |
| `anchor-left` | `styles.css:1013-1020` | `right: calc(100% + 6px)`、`top:50%`、`translate:0 -50%` |

间距变量 `--pop-gap: 6px`（`styles.css:938`），标签贴边后不再浮在座位间隙。

### 2.2 短尖角（CSS `::after` 三角形，真正指向对应卡片）

尖角长度 `--pop-arrow: 6px`（`styles.css:939`，与 `--pop-gap` 相等，正常尺寸下顶点恰好触到卡片）。
尖角颜色用 `--pop-accent`（与标签描边同色，四种动作各自取色：`styles.css:962/969/976/983`）。

| 锚定边 | 规则 | 三角着色边 | 朝向 |
|---|---|---|---|
| `anchor-below` | `styles.css:1036-1043` | `border-bottom` | 朝上，指向卡片下沿 |
| `anchor-right` | `styles.css:1044-1051` | `border-right` | 朝左，指向卡片右侧 |
| `anchor-left` | `styles.css:1052-1059` | `border-left` | 朝右，指向卡片左侧 |

列表布局（移动端/窄桌面）隐藏尖角，避免指向不存在的侧向卡片：`styles.css:2112-2114`。

### 2.3 transform-origin（贴边一侧固定，缩放时尖角不脱离卡片）

缩放原点固定在标签的贴边一侧，放大时向**远离卡片**方向展开，尖角连接线固定不动：

| 锚定边 | transform-origin | 展开方向 |
|---|---|---|
| `anchor-below` | `center top`（`styles.css:1003`） | 向下 |
| `anchor-right` | `left center`（`styles.css:1011`） | 向右 |
| `anchor-left` | `right center`（`styles.css:1019`） | 向左 |

因为原点在贴边一侧，`--pop-scale: 1.8` 放大时贴边线不移动，尖角只会向卡片方向加深重叠，
不会被拉离卡片。1.8 倍缩放、2200ms 时间轴、四色配色、动画曲线均未改动
（`@keyframes action-pop`，`styles.css:1062-1091`）。

### 2.4 顶部向下展开的保留与相邻侧边冲突

顶部座位仍向下（朝桌内）展开，避开座位上方常驻下注条；
把正右座位标签改贴卡片左侧（`anchor-left`）后，UR 与 R 不再共用同一空隙。

## 3. 侧边标签贴内向桌心展开的风险与折中

正右座位标签贴卡片左侧后，向桌心（公共牌方向）展开；长「加注到 1,234,567」在 1.8 倍峰值
横向伸展可能逼近公共牌。折中：侧边标签紧凑化，收窄 `max-width` 到 128px
（`styles.css:1022-1025`），使长金额折行增高、减少横向伸展；不缩字号、不降特效强度。
9 人局 + 最长金额在 1.8 倍峰值实测不遮挡 `.board` / `.pot` / `.street-label`
（见 `action-popover-anchor.spec.ts`「9 人密集 + 长金额峰值」）。

## 4. 命令与完整原始输出

### 4.1 typecheck + build

```text
$ npm --prefix web run typecheck

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

exit=0

$ npm --prefix web run build

> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
transforming...
✓ 50 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-uXRkLTzZ.css   40.32 kB │ gzip:  9.52 kB
dist/assets/index-BcFeZ6av.js   281.92 kB │ gzip: 89.53 kB
✓ built in 676ms
exit=0
```

### 4.2 动作标签定向 fixture（14 passed）

命令：`npx --no-install playwright test --config=playwright.fixture.config.ts action-popover.spec.ts action-popover-scale.spec.ts action-popover-anchor.spec.ts`

```text
Running 14 tests using 1 worker

  ✓   1 [chromium] › fixture/tests/action-popover-anchor.spec.ts:140:1 › 5 人：AI-4(UR 向下) 与 AI-5(R 贴左) 相邻过牌并存，各自贴座、尖角指向、互不重叠 (5.1s)
  ✓   2 [chromium] › fixture/tests/action-popover-anchor.spec.ts:209:1 › 4 座：四种锚定边贴边间距与尖角方向（below/right/left） (4.7s)
  ✓   3 [chromium] › fixture/tests/action-popover-anchor.spec.ts:247:1 › 9 人密集 + 长「加注到」1.8 倍峰值：侧边标签紧凑化不遮挡公共牌/底池 (1.8s)
  ✓   4 [chromium] › fixture/tests/action-popover-anchor.spec.ts:274:1 › 移动端 390 列表布局：不显示贴边尖角、不溢出 (1.4s)
  ✓   5 [chromium] › fixture/tests/action-popover-anchor.spec.ts:292:1 › reduced-motion：静态保留贴边与尖角，无动画 (3.0s)
  ✓   6 [chromium] › fixture/tests/action-popover-scale.spec.ts:150:1 › 桌面：四种动作统一 scale 1.8 / 2200ms / 四色 (6.0s)
  ✓   7 [chromium] › fixture/tests/action-popover-scale.spec.ts:216:1 › 桌面：精确时间轴（80/320/620/920/1980ms） (2.7s)
  ✓   8 [chromium] › fixture/tests/action-popover-scale.spec.ts:247:1 › 桌面：展开方向 transform-origin（左半/右半/顶部/真人） (6.0s)
  ✓   9 [chromium] › fixture/tests/action-popover-scale.spec.ts:292:1 › 移动端 390：scale 仍 1.8、不横向溢出 (2.3s)
  ✓  10 [chromium] › fixture/tests/action-popover-scale.spec.ts:315:1 › reduced-motion：静态高对比展示约 2200ms (4.5s)
  ✓  11 [chromium] › fixture/tests/action-popover.spec.ts:131:1 › 去重触发 + 同座位 FIFO 不截断 + 不同座位并存 + 跨手不清 + 弃牌不变暗 + 标签定位 (11.6s)
  ✓  12 [chromium] › fixture/tests/action-popover.spec.ts:218:1 › reduced-motion：标签降级为静态可见，随后由 timer 移除 (5.4s)
  ✓  13 [chromium] › fixture/tests/action-popover.spec.ts:247:1 › 人数模板 2/6/9（全 AI）：标签不压中央、不互相重叠、无横向溢出 (7.3s)
  ✓  14 [chromium] › fixture/tests/action-popover.spec.ts:294:1 › 移动端 390：标签落在预留行、不压 .bet、不溢出 (3.6s)

  14 passed (1.1m)
```

### 4.3 完整 fixture 套件（55 passed，确认无回归）

命令：`npx --no-install playwright test --config=playwright.fixture.config.ts`

```text
  55 passed (3.7m)
```

（含 table-layout 2-9 人、rim-badge 9 人方向分派、settlement、street-track、human-player、
quick-bet-keys、model-label、chip-badge 等；上表已列与本改动最相关的 action-popover 三组。）

## 5. 场景 → 测试函数 → 结果

| 场景 | 测试文件:行 | 断言要点 | 结果 |
|---|---|---|---|
| 5 人局 AI-4/AI-5 相邻过牌不再糊在一起 | `action-popover-anchor.spec.ts:140` | 两标签 boundingBox 不重叠 | PASS |
| AI-4(UR) 贴卡片下沿、尖角朝上 | 同上 | `anchor-below`、gap 0–10px、`border-bottom` 着色 | PASS |
| AI-5(R) 贴卡片左侧、尖角朝左 | 同上 | `anchor-left`、gap 0–10px、`border-left` 着色 | PASS |
| 底部真人贴右侧、尖角朝左 | 同上 | `anchor-right`、gap 0–10px、`border-right` 着色 | PASS |
| 正左/正右/顶部/真人四向 origin 与贴边 | `action-popover-scale.spec.ts:247` | origin 与 anchor class | PASS |
| 9 人密集 + 长金额 1.8 倍峰值不遮挡公共牌 | `action-popover-anchor.spec.ts:247` | 峰值帧与 `.board/.pot/.street-label` 不重叠 | PASS |
| 2/6/9 人标签不压中央、互不重叠、无横向溢出 | `action-popover.spec.ts:247` | boundingBox 交叉 | PASS |
| 移动端 390 列表布局无尖角、不溢出 | `action-popover-anchor.spec.ts:274` | `::after display:none`、scrollWidth | PASS |
| reduced-motion 静态保留贴边/尖角、无动画 | `action-popover-anchor.spec.ts:292`、`action-popover-scale.spec.ts:315` | `animation:none`、尖角 visible | PASS |
| 去重/FIFO/并存/弃牌不变暗 | `action-popover.spec.ts:131` | 数量、文案、opacity | PASS |

## 6. 截图证据

输出目录 `docs/action-popover-anchor/`（fixture 生成）：

| 文件 | 内容 |
|---|---|
| `five-seat-ai4-ai5-adjacent.png` | 5 人局 AI-4/AI-5 相邻过牌（重点场景） |
| `five-seat-bottom-human.png` | 5 人局底部真人动作标签 |
| `four-seat-anchors.png` | 四种锚定边（顶部/正左/正右/底部真人） |
| `nine-seat-long-amount-peak.png` | 9 人密集 + 最长金额 1.8 倍峰值 |
| `mobile-list-no-arrow.png` | 移动端列表布局（无尖角） |
| `reduced-motion-anchor.png` | reduced-motion 静态贴边+尖角 |

说明：上述截图由 Playwright 生成并随测试运行落盘；本轮的「不糊在一起/贴边/尖角朝向/不遮挡公共牌」
以测试中的**几何断言**（boundingBox 间距、交叠、`::after` 计算样式着色边）为准，可复现、非人眼自述。
截图的视觉复核可另行由子 agent 读图确认。

## 7. 未验证边界

- **真实 Go 端到端联调未执行**：本轮为 fixture（`fixture-server.mjs` + `page.route` 脚本化 `last_action`），
  不是真实 Go 链路验收。真实链路需跑 `python3 tests/e2e/browser/run_action_popover.py`
  （会重新 build web/dist + Go 二进制、起 8081、走真实 Chromium）。
- 未覆盖多真人并发、live/付费模型、故障注入、非 loopback 部署。
- 侧边紧凑化在极端窄桌面（>520px 且 <1000px 的 `is-tight` 非列表区间）未逐一截图，
  仅以 1536 桌面与 390 移动端两端 + 9 人密集峰值覆盖。
