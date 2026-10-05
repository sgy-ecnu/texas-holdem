# RIM 徽章修复（Bug1 撞位 + Bug2 外观）

范围：纯前端。仅改 `web/src/styles.css`、`web/src/components/PokerTable.tsx`，并同步 fixture 契约
（`tests/e2e/browser/fixture/tests/rim-badge.spec.ts`、`tests/e2e/browser/fixture/tests/action-popover-scale.spec.ts`）。
未改动 Go / scripts / config / Seat.tsx / SpectatorView.tsx / settlement / ActionPopover.tsx / types.ts / format.ts。
未 commit / push。未读取 `.env.local` / `config.local.yaml` 的值。

---

## Bug1：顶部下注徽章与动作特效撞位

### 实测碰撞（修复前，fixture，1536×1024，4 座，顶部 seat2，动作 scale 1.8）

| 元素 | top(px) | bottom(px) | 说明 |
| --- | --- | --- | --- |
| `.stage` 顶 | 155.98 | — | 舞台顶 |
| 顶部座位卡顶 | 205.97 | — | 座位中心距 stage 顶仅 ~50px |
| `.rim-mark.is-up` | 164.13 | 186.97 | `bottom: calc(100% + 19px)`，常驻 |
| `.action-popover` @1.8× | 110.57 | 178.97 | `bottom: calc(100% + 27px)`，瞬态 |

重叠：动作特效底边 178.97 落在徽章 top 164.13 之下 → **重叠 14.84px**；且特效顶 110.57 已在 stage 顶
155.98 之上（向上溢出 ~45px）。

### 方案判断（为什么徽章不能放特效之上）

若把徽章 `bottom` 增大到「特效最大展开范围之上」，按基础高 38px×1.8=68.4，甚至长金额折行 ~104px 计，
徽章顶需到座位顶上方 `27 + 104 ≈ 131px`，而实测座位顶距 stage 顶**只有 50px** → 徽章会被 stage 顶裁切/溢出，
属任务明确禁止的「硬塞导致新溢出」。故采用任务允许的折中：**动作特效向下（朝桌内）展开**。

### 实现

- `PokerTable.tsx`：`.seat-slot` 增加 `data-dir={dir}`（方向分派 `BarDir` 原样透出，供 CSS 定位）。
- `styles.css` 新增定向规则：

```css
.seat-slot[data-dir="up"] .action-popover {
  top: calc(100% + 8px);   /* 座位卡下方 8px 起，向下展开 */
  bottom: auto;
  left: 50%;
  right: auto;
  translate: -50% 0;
  transform-origin: center top;
}
```

顶部动作特效锚点由「座位上方 27px 向上展开」改为「座位卡下方 8px 向下展开」，与常驻徽章
（座位上方 19px）在垂直方向上**完全分离**，不依赖 z-index 硬盖。徽章位置、方向分派（`barDir`）均未改。

### 修复后实测（同条件）

| 帧 (ms) | popTop | popBottom | barTop | barBottom | 是否重叠 | 是否越 stage 顶 |
| --- | --- | --- | --- | --- | --- | --- |
| 0 | 271.97 | 340.37 | 164.13 | 186.97 | 否 | 否 |
| 320 | 271.97 | 340.37 | 164.13 | 186.97 | 否 | 否 |
| 620 | 271.97 | 316.79 | 164.13 | 186.97 | 否 | 否 |
| 920 | 271.97 | 309.97 | 164.13 | 186.97 | 否 | 否 |
| 1980 | 271.97 | 309.97 | 164.13 | 186.97 | 否 | 否 |

放大→缩回全程：特效顶固定 271.97（= 座位卡底 + 8），徽章底 186.97，**最小间隙约 85px**，全程不压徽章；
特效不再越过 stage 顶部。

### 其他方向核查（读码 + 实测）

- `is-down`：徽章在座位下方，动作特效在座位上方 → 无撞位。
- `is-left` / `is-right`：徽章纵向居中挂在座位侧边，动作特效在座位上方 → 无撞位。
- 仅 `is-up` 存在同座撞位，已修；下/左/右不额外改（见回归测试）。

---

## Bug2：徽章外观（深色胶囊 → 浅色弱边框附属条）

`styles.css` `.rim-mark`：

- 背景：`rgba(10,16,14,0.72)`（深色）→ `rgba(238,234,223,0.92)`（浅暖白）。
- 边框：金色 `rgba(201,164,76,0.26)` → 中性弱边框 `rgba(43,47,41,0.22)`（去掉金边/筹码图标感）。
- 文字：本轮 `.rim-now` → 近黑 `#171b12` + `font-weight:700`（一级强调）；累计 `.rim-cum` → 中灰 `#6a7167`、10px（次级但可读）。
- 上/下方向仍单行、`·` 分隔（分隔点色由 `var(--faint)` 调整为 `#9aa096`，适配浅底）；左/右仍两行。

---

## 验证

### 1) typecheck

```
$ npm --prefix web run typecheck

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

（exit 0，无输出）
```

### 2) build

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
dist/assets/index-BsyZcR7H.css   39.30 kB │ gzip:  9.35 kB
dist/assets/index-C-hqzINN.js   281.57 kB │ gzip: 89.40 kB
✓ built in 666ms
```

### 3) fixture 浏览器验证（Playwright，本地 fixture 服务器，非真实 Go 链路）

命令（工作目录 `tests/e2e/browser`）：

```
$ npx playwright test --config playwright.fixture.config.ts
```

完整输出：

```
Running 49 tests using 1 worker

[dock] desktop height=139
  ✓   1 [chromium] › fixture/tests/action-dock.spec.ts:41:1 › 桌面 · 三行紧凑结构、总高下降、无重复信息、按钮≥40px (3.6s)
[dock] thinking height=54
  ✓   2 [chromium] › fixture/tests/action-dock.spec.ts:94:1 › 桌面 · 非我行动（思考中）时操作区仅剩状态行 (3.5s)
[dock] mobile height=202
  ✓   3 [chromium] › fixture/tests/action-dock.spec.ts:109:1 › 移动端 390 · 各行堆叠、触控目标≥40px、不溢出 (3.2s)
  ✓   4 [chromium] › fixture/tests/action-popover-scale.spec.ts:150:1 › 桌面：四种动作统一 scale 1.8 / 2200ms / 四色 (6.1s)
  ✓   5 [chromium] › fixture/tests/action-popover-scale.spec.ts:216:1 › 桌面：精确时间轴（80/320/620/920/1980ms） (2.7s)
  ✓   6 [chromium] › fixture/tests/action-popover-scale.spec.ts:247:1 › 桌面：展开方向 transform-origin（左半/右半/顶部/真人） (6.0s)
  ✓   7 [chromium] › fixture/tests/action-popover-scale.spec.ts:289:1 › 移动端 390：scale 仍 1.8、不横向溢出 (2.3s)
  ✓   8 [chromium] › fixture/tests/action-popover-scale.spec.ts:312:1 › reduced-motion：静态高对比展示约 2200ms (4.5s)
  ✓   9 [chromium] › fixture/tests/action-popover.spec.ts:131:1 › 去重触发 + 同座位 FIFO 不截断 + 不同座位并存 + 跨手不清 + 弃牌不变暗 + 标签定位 (11.7s)
  ✓  10 [chromium] › fixture/tests/action-popover.spec.ts:218:1 › reduced-motion：标签降级为静态可见，随后由 timer 移除 (5.4s)
  ✓  11 [chromium] › fixture/tests/action-popover.spec.ts:247:1 › 人数模板 2/6/9（全 AI）：标签不压中央、不互相重叠、无横向溢出 (7.4s)
  ✓  12 [chromium] › fixture/tests/action-popover.spec.ts:294:1 › 移动端 390：标签落在预留行、不压 .bet、不溢出 (3.6s)
  ✓  13 [chromium] › fixture/tests/human-player.spec.ts:19:1 › 全 AI 创建走公共观战，不出现真人行动面板 (353ms)
  ✓  14 [chromium] › fixture/tests/human-player.spec.ts:31:1 › 真人座位：配置页隐藏模型下拉，创建后进入真人视角 (3.1s)
  ✓  15 [chromium] › fixture/tests/human-player.spec.ts:44:1 › 轮到真人：座位光圈与「轮到我」、底部倒计时进度条与操作按钮组 (3.2s)
  ✓  16 [chromium] › fixture/tests/human-player.spec.ts:76:1 › AI 轮转：底部显示「思考中…」与不确定进度，行动历史高亮新动作 (3.2s)
  ✓  17 [chromium] › fixture/tests/human-player.spec.ts:96:1 › 无 Cookie 浏览器：人类牌局仅公共观战，看不到真人底牌与行动面板 (268ms)
  ✓  18 [chromium] › fixture/tests/human-player.spec.ts:123:1 › 移动端 390 宽：轮转提示与底部操作区可点无遮挡 (3.1s)
  ✓  19 [chromium] › fixture/tests/model-label.spec.ts:52:1 › 桌面：模型短名上移第一行、第二行剩余不换行、未知模型不留孤立括号、卡不增高 (603ms)
  ✓  20 [chromium] › fixture/tests/model-label.spec.ts:102:1 › 移动端 ≤720px：模型短名 11px / max-width 120px、不溢出 (3.2s)
  ✓  21 [chromium] › fixture/tests/quick-bet-keys.spec.ts:26:1 › 快捷注：六个比例按钮渲染，目标值按基准池 clamp，点击填入输入框 (3.2s)
  ✓  22 [chromium] › fixture/tests/quick-bet-keys.spec.ts:57:1 › 快捷键 F：直接弃牌并收起操作面板 (3.2s)
  ✓  23 [chromium] › fixture/tests/quick-bet-keys.spec.ts:66:1 › 快捷键 R 聚焦加注输入框；输入框聚焦时 F/C/K 不误触 (3.1s)
  ✓  24 [chromium] › fixture/tests/quick-bet-keys.spec.ts:87:1 › 键盘 R + 输入 + 回车：提交加注动作 (3.1s)
  ✓  25 [chromium] › fixture/tests/quick-bet-keys.spec.ts:100:1 › 移动端 390 宽：快捷注按钮可见且不超出视口 (3.1s)
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
  ✓  26 [chromium] › fixture/tests/rim-badge.spec.ts:108:1 › 9 人（真人 #4）：方向分派正确、外侧条在桌沿外、不遮中央/底牌/座位卡/行动标签、相邻不叠压 (3.7s)
  ✓  27 [chromium] › fixture/tests/rim-badge.spec.ts:141:1 › 9 人（真人 #0）：顶部 4 座方向同上、外侧条不叠压、不溢出 (791ms)
  ✓  28 [chromium] › fixture/tests/rim-badge.spec.ts:155:1 › 最长金额：整条仍在桌沿外、不产生横向滚动 (829ms)
  ✓  29 [chromium] › fixture/tests/rim-badge.spec.ts:169:1 › 窄桌面 900x800：空间不足提前切列表布局，外侧条隐藏、下注信息回卡内 .bet、无横向滚动 (481ms)
  ✓  30 [chromium] › fixture/tests/rim-badge.spec.ts:186:1 › 移动端 390：外侧条隐藏，下注信息在座位卡内 .bet，无横向溢出 (388ms)
  ✓  31 [chromium] › fixture/tests/rim-badge.spec.ts:241:1 › 顶部座位：动作特效 scale 1.8→缩回全程不压顶部下注条、不向上溢出 stage (3.8s)
  ✓  32 [chromium] › fixture/tests/rim-badge.spec.ts:305:1 › 徽章外观：浅色弱边框附属条（非深色胶囊），本轮强调、累计次级可读 (661ms)
  ✓  33 [chromium] › fixture/tests/rim-badge.spec.ts:341:1 › 下/左/右方向无同座徽章与动作特效撞位（不回归） (10.4s)
  ✓  34 [chromium] › fixture/tests/settlement.spec.ts:134:1 › 摊牌结算：参与者翻牌、弃牌者不翻、赢家金额=payouts、结算条主池命名、快照 (3.1s)
  ✓  35 [chromium] › fixture/tests/settlement.spec.ts:194:1 › 弃牌结束：不翻牌、不显示牌型、显示「其余玩家弃牌」、退回展示 (1.9s)
  ✓  36 [chromium] › fixture/tests/settlement.spec.ts:234:1 › 真人赢：标签「你赢了 +X」、只赢边池明确提示、冰蓝身份保留 (3.5s)
  ✓  37 [chromium] › fixture/tests/settlement.spec.ts:299:1 › 首次进入不重播历史手 (2.8s)
  ✓  38 [chromium] › fixture/tests/settlement.spec.ts:318:1 › reduced-motion：结算静态可见（金边/标签/牌面），不依赖动画 (1.8s)
  ✓  39 [chromium] › fixture/tests/settlement.spec.ts:354:1 › 移动端 390：结算条在牌桌下方、可展开、不遮挡操作区 (1.6s)
  ✓  40 [chromium] › fixture/tests/street-track.spec.ts:73:1 › 五节点轨道：当前街最亮、过去小勾、未来空心、当前注文案 (2.6s)
  ✓  41 [chromium] › fixture/tests/street-track.spec.ts:107:1 › 切街临时提示 1.5s（flop→turn），随后恢复常态 (4.2s)
  ✓  42 [chromium] › fixture/tests/street-track.spec.ts:126:1 › 跳街（preflop→river）不伪造中间切街提示 (3.5s)
  ✓  43 [chromium] › fixture/tests/street-track.spec.ts:146:1 › 暂停：状态行显示「牌局已暂停 · 本街尚未结束」 (501ms)
  ✓  44 [chromium] › fixture/tests/street-track.spec.ts:161:1 › 停止：不把中止画成正常结束 (468ms)
  ✓  45 [chromium] › fixture/tests/table-layout.spec.ts:94:1 › 人数模板 2-9（真人局）：不重叠、不侵入中央、真人底部居中、两行固定 (26.0s)
[layout] stage=1334x734 felt=1012x528
  ✓  46 [chromium] › fixture/tests/table-layout.spec.ts:137:1 › 桌面 · 真人 9 座：底部居中、中央清爽、座位卡两行、牌桌占据主要区域 (3.6s)
  ✓  47 [chromium] › fixture/tests/table-layout.spec.ts:164:1 › 桌面 · 全 AI 9 座观战：环绕不重叠、中央清爽 (732ms)
  ✓  48 [chromium] › fixture/tests/table-layout.spec.ts:178:1 › 座位外侧下注条：本街下注（本轮 + 累计）在桌沿外，不压中央 (662ms)
  ✓  49 [chromium] › fixture/tests/table-layout.spec.ts:211:1 › 移动端 390：座位可点、不溢出、操作条在视口内 (3.2s)

  49 passed (2.9m)
```

### 4) 场景 → 测试 → 结果

| 要求场景 | 测试函数 | 结果 |
| --- | --- | --- |
| 顶部动作标签从放大到缩回全程不压顶部下注徽章 | `rim-badge.spec.ts:241` | 通过（0/320/620/920/1980ms 无重叠，特效全程在徽章下方） |
| 徽章浅色弱边框附属条外观 | `rim-badge.spec.ts:305` | 通过（bg 三通道 >180；边框非金；本轮近黑、累计中灰） |
| 方向分派不变 | `rim-badge.spec.ts:108/141` | 通过（9 人 up/down/left/right 与预期一致） |
| 下/左/右不回归 | `rim-badge.spec.ts:341` | 通过（seat0 下、seat1 左、seat3 右 全程无同座撞位） |
| 2–9 人 | `table-layout.spec.ts:94` | 通过（不重叠、不侵入中央、真人底部居中） |
| 移动端 | `rim-badge.spec.ts:186`、`action-popover.spec.ts:294`、`table-layout.spec.ts:211` | 通过（外侧条隐藏、信息回卡内、无横向溢出） |
| reduced-motion | `action-popover-scale.spec.ts:312`、`action-popover.spec.ts:218` | 通过（静态可见、无缩放） |
| 顶部动作 origin 契约同步 | `action-popover-scale.spec.ts:247` | 通过（顶部 origin 改为 center top） |

### 5) 截图证据（`docs/rim-badge/`）

- `top-action-no-collision.png`：顶部座位动作发生（scale 1.8）时，特效在座位下方、徽章在上方不接触。
- `badge-light-appearance.png`：浅色弱边框徽章（本轮/累计）。
- `other-dirs-no-collision.png`：下/左/右方向无撞位。
- `desktop-9-bets.png` / `desktop-9-human0.png` / `desktop-max-amount.png`：2–9 人及最长金额。
- `mobile-390.png` / `narrow-900.png`：移动端/窄桌面（切列表布局、徽章隐藏）。

---

## 未验证边界 / 冲突记录

1. **真实 Go 端到端未跑**：上述为本地 fixture（内存模拟契约 + `web/dist`），不能作为真实 Go 联调完成证据。
   真实链路需 `tests/e2e/run.py` + `playwright.config.ts`（本次未运行，且不在允许改动范围）。
2. **顶部徽章不采用「放特效之上」**：实测座位顶距 stage 顶仅 ~50px（1536×1024），特效最大展开需 ~131px，
   放上方必然裁切/溢出，已按任务允许的折中改为特效向下展开，未硬塞。
3. **未在真实移动设备/多浏览器验证**：仅 Chromium（fixture 默认 project）。
4. **顶部特效长金额折行**：fixture 最长金额用例验证的是徽章条（不折行）；动作特效文本折行（2 行）时向下展开
   空间仍充足（实测到达 ~370px，未触公共牌 ~493px），但未对「2 行×1.8 倍」单独截图断言。
