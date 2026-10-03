# 操作区侧栏布局（参考 PokerTH）

> 目标：把「轮到谁 + 倒计时 + 操作按钮」从页面最底部移到牌桌旁边，让操作按钮不用下滑就可见。
> 范围：仅前端布局（`web/src/**`）。未改 Go、scripts、config、contracts、Makefile。
> 未 commit / push。

## 一、PokerTH 布局分析结论

看了 PokerTH 的两张截图（`mile_high_club.png`、`default.png`）：

1. **固定游戏视口，整屏不滚动**。牌桌是画面主体，所有控件都在同一屏内，因此操作按钮天然可见。
2. **操作条在牌桌正下方、水平居中**：`Fold / Check / Bet`（或 `Fold / Call / Raise`）一行大按钮，
   其上方紧贴一行快捷注（`1/3 · 1/2 · Pot · All-In · Manual`）与状态指示。
3. **操作条不压在牌桌上，也不遮座位**：牌桌椭圆与座位在上，操作条独占牌桌下方的一条横带。
4. **行动反馈贴座位显示**（座位旁的 `Fold / Check / Raise / Bet` 小标签），
   没有单独的滚动「行动历史」大面板；牌桌右上角是街名（Flop/Turn）与手数等极简信息。
5. 次要信息（Credits、Chips、街名）放在四角，不抢视觉焦点。

**可借鉴的核心思路**：操作区与牌桌同屏、紧邻牌桌、绝不与座位重叠；次要信息弱化、外移。
本项目原实现的差异在于它不是固定视口，而是一张长页面——操作区 `sticky bottom` 被包含块
（`.lower-deck`）约束在牌桌下方，页面初始在顶部时它还在视口外，必须下滑才出现。这正是用户抱怨的点。

## 二、新布局结构

宽屏（≥1200px）采用左右分栏：**牌桌居中偏左 + 右侧常驻操作/历史栏**；窄屏回落为纵向堆叠（保留原
「操作区不遮盖底部座位」的 sticky 行为）。

```
.container.container-play           ← 牌局阶段容器加宽到 1360px
├─ banner(s) / statusbar
├─ .play-layout                     ← 新增分栏容器
│  ├─ .table-wrap                   ← 牌桌（左列）
│  │  └─ .stage(PokerTable)
│  └─ aside.side-rail               ← 操作/历史栏（右列，宽屏 sticky 顶部）
│     ├─ ActionDock (.hand-dock)    ← 轮到谁提示 + 倒计时/进度 + 操作按钮（主）
│     └─ .panel 行动历史 (ActionFeed)
└─ .panel.info-deck                 ← 运行信息（次要，整幅放分栏下方）
```

关键点：

- `ActionDock` 从「页面底部 sticky」移入 `.side-rail`。宽屏下它是普通整卡（完整圆角/四边框），
  随侧栏 `position: sticky; top: 14px` 常驻，操作按钮始终在视口内。
- 宽屏时操作区在**独立列**中，与牌桌并排，天然不可能遮盖牌桌底部座位。
- 窄屏（<1200px）`.play-layout` 回落为 `display:block`，`.side-rail` 在牌桌下方；
  其 sticky 底部操作区被 `.side-rail`（包含块）约束在牌桌下方，**不会盖住底部座位**（沿用原约束）。
- 「行动历史」随操作区进侧栏（次要面板）；「运行信息」整幅放到分栏下方，进一步弱化。
- 组件逻辑零改动：倒计时、快捷键、快捷注、`controlsBusy` 互斥语义全部保留，只改容器与样式。

## 三、关键 CSS（节选）

```css
/* 牌局容器加宽，给「牌桌 + 右栏」并排空间 */
.container-play { max-width: 1360px; }

/* 窄屏默认：纵向堆叠，side-rail 从牌桌下方开始（sticky 包含块，防遮盖） */
.play-layout { display: block; }
.side-rail { display: flex; flex-direction: column; gap: 14px; margin-top: 18px; min-width: 0; }

/* 宽屏：左右分栏，操作栏常驻视口 */
@media (min-width: 1200px) {
  .play-layout {
    display: grid;
    grid-template-columns: minmax(0, 1fr) clamp(320px, 30%, 400px);
    gap: 18px;
    align-items: start;
  }
  .side-rail { position: sticky; top: 14px; align-self: start; margin-top: 0; }
  /* 侧栏行动区改为整卡常驻样式：完整圆角与四边框，去掉底部吸附外观 */
  .side-rail .hand-dock {
    position: static;
    border-radius: var(--radius);
    border-bottom: 1px solid rgba(201, 164, 76, 0.22);
    box-shadow: 0 24px 50px -30px rgba(0, 0, 0, 1);
  }
  .side-rail .action-feed { max-height: min(42vh, 420px); }
}

/* 运行信息：牌桌分栏下方的次要面板 */
.info-deck { margin-top: 16px; }
.info-kv { flex-direction: column; gap: 8px; }
.info-models { margin-top: 12px; }
```

配色沿用现有深色绒面 + 黄铜变量（`--gold`、`--panel`、`--radius` 等），未引入新色系。

## 四、改动文件

| 文件 | 改动 |
|---|---|
| `web/src/components/SpectatorView.tsx` | 新增 `.play-layout` 分栏；`ActionDock` 与「行动历史」进 `.side-rail`；「运行信息」拆到 `.info-deck` 整幅面板；根容器加 `container-play` |
| `web/src/styles.css` | 新增 `.container-play`、`.play-layout`、`.side-rail`、`.info-deck/.info-kv/.info-models`；新增 ≥1200px 分栏媒体查询；删除废弃 `.play-grid` 规则 |

> `docs/CREATED_EXIT_FIX.md` 是本次之前就存在的未跟踪文件，未改动。

未修改任何组件逻辑（`ActionDock.tsx` / `HumanActionPanel.tsx` / `PokerTable.tsx` 等）。

## 五、`npm --prefix web run typecheck` 输出

```
$ npm --prefix web run typecheck

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

TYPECHECK_EXIT:0
```

## 六、`npm --prefix web run build` 输出

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
dist/assets/index-GwY-fn-t.css   26.98 kB │ gzip:  7.07 kB
dist/assets/index-BfHmuUL6.js   266.12 kB │ gzip: 83.75 kB
✓ built in 665ms
BUILD_EXIT:0
```

## 七、fixture 浏览器证据（`tests/e2e/browser/fixture`）

### 7.1 现有 fixture 套件回归（全部通过）

```
$ npx playwright test --config playwright.fixture.config.ts

Running 11 tests using 1 worker

  ✓   1 [chromium] › fixture/tests/human-player.spec.ts:19:1 › 全 AI 创建走公共观战，不出现真人行动面板 (392ms)
  ✓   2 [chromium] › fixture/tests/human-player.spec.ts:31:1 › 真人座位：配置页隐藏模型下拉，创建后进入真人视角 (3.1s)
  ✓   3 [chromium] › fixture/tests/human-player.spec.ts:44:1 › 轮到真人：座位光圈与「轮到我」、底部倒计时进度条与操作按钮组 (3.2s)
  ✓   4 [chromium] › fixture/tests/human-player.spec.ts:76:1 › AI 轮转：底部显示「思考中…」与不确定进度，行动历史高亮新动作 (3.2s)
  ✓   5 [chromium] › fixture/tests/human-player.spec.ts:96:1 › 无 Cookie 浏览器：人类牌局仅公共观战，看不到真人底牌与行动面板 (262ms)
  ✓   6 [chromium] › fixture/tests/human-player.spec.ts:123:1 › 移动端 390 宽：轮转提示与底部操作区可点无遮挡 (3.1s)
  ✓   7 [chromium] › fixture/tests/quick-bet-keys.spec.ts:26:1 › 快捷注：六个比例按钮渲染，目标值按基准池 clamp，点击填入输入框 (3.4s)
  ✓   8 [chromium] › fixture/tests/quick-bet-keys.spec.ts:57:1 › 快捷键 F：直接弃牌并收起操作面板 (3.2s)
  ✓   9 [chromium] › fixture/tests/quick-bet-keys.spec.ts:66:1 › 快捷键 R 聚焦加注输入框；输入框聚焦时 F/C/K 不误触 (3.3s)
  ✓  10 [chromium] › fixture/tests/quick-bet-keys.spec.ts:87:1 › 键盘 R + 输入 + 回车：提交加注动作 (3.3s)
  ✓  11 [chromium] › fixture/tests/quick-bet-keys.spec.ts:100:1 › 移动端 390 宽：快捷注按钮可见且不超出视口 (3.3s)

  11 passed (30.5s)
PW_EXIT:0
```

### 7.2 「操作按钮在视口内、无需下滑」专项断言

独立脚本（`/tmp/pokerth-layout-verify.cjs`，只在本地跑、不写入仓库）启动 fixture 服务器，
用真实 Chromium 在 `scrollY=0`（未滚动）时测操作按钮位置与牌桌/侧栏关系：

```
{
  "desktop": {
    "viewport": "1440x900",
    "scrollY": 0,
    "foldInViewportAtTop": true,
    "foldBox": { "x": 993, "y": 267.56, "width": 153.02, "height": 46 },
    "dockBox": { "x": 976, "y": 155.98, "width": 384, "height": 382.97 },
    "tableBox": { "x": 80, "y": 155.98, "width": 878, "height": 526.80 },
    "sideBySide": true,
    "overlapX": 0
  },
  "desktopSmall": {
    "viewport": "1280x800",
    "scrollY": 0,
    "foldInViewportAtTop": true,
    "foldBox": { "x": 897, "y": 267.56, "width": 141.02, "height": 46 },
    "sideBySide": true
  },
  "mobile": {
    "viewport": "390x844",
    "horizontalOverflowPx": 0,
    "foldBox": { "x": 28.59, "y": 399.05, "width": 162.41, "height": 46 },
    "foldWithinWidth": true,
    "clickable": true,
    "actionPanelClosedAfterClick": true
  }
}
```

解读：
- 桌面 1440×900 / 1280×800：**未滚动（scrollY=0）时「弃牌」按钮已在视口内**（y≈268 < 900/800）。
- `dockBox.x (976) > tableBox.right (958)`，`overlapX: 0`：操作区在牌桌右侧独立列，**不重叠牌桌**。
- 移动 390×844：横向溢出 0px；滚动到操作区后按钮完整落在 390 宽度内且可点击、点击后等待点正确收起。

### 7.3 截图（桌面 + 移动端）

保存在被 git 忽略的目录：
`web/node_modules/.cache/browser-verify/1791038966403/`

- `layout-desktop-1440x900.png`：牌桌在左，右侧栏为「你 · 轮到你出牌 · 1:00」+ 倒计时条 + 弃牌/跟注/加注/快捷注/全押，下方「行动历史」；操作按钮无需滚动即可见。
- `layout-desktop-1280x800.png`：较小桌面同样分栏，按钮在视口内。
- `layout-mobile-390x844.png`：牌桌与座位在上，操作区紧随其下，横向不溢出。

## 八、与「座位环绕牌桌」椭圆定位的关系（冲突与折中）

- **宽屏（≥1200px）无冲突**：操作区在牌桌的独立列里，座位仍按椭圆围绕牌桌，二者水平分离。
- **窄屏（<1200px）不能并排**，否则牌桌列过窄会让 150px 座位互相挤压/溢出椭圆。折中：
  这些宽度回落为纵向堆叠，操作区仍在牌桌下方，并用 `.side-rail` 作为 sticky 包含块，
  保证**操作区永不遮盖底部座位**（保留既有约束）。代价是「全程无需下滑」只在 ≥1200px 保证；
  1200px 以下需要在牌桌上略作滚动才看到操作区（与移动端一致）。
- 说明：本项目座位几何是硬编码椭圆（座位 0 在正上方），与 PokerTH「真人座位在正下方」不同；
  本次只改布局不动座位几何，避免引入与椭圆定位的冲突。

## 九、未验证边界

- **真实 Go 联调未做**：以上均为本地 fixture（内存态）验证，非真实后端链路。
  真实链路需另跑 `playwright.config.ts` + `run_browser.py`（依赖 MySQL/Redis），本次未执行。
- 1200/1400px 之间的极端缩放、超宽屏（>1920）下的牌桌留白未逐一截图核对；
  已核对 1280×800、1440×900、390×844 三档。
- 未在 Safari/Firefox 实机验证（仅 Chromium）。
