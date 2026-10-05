# 前端验收：街进度指示器 + 谁赢了结算（SETTLEMENT_FRONTEND）

范围：`web/src/**`（除 `web/embed.go`）、`tests/e2e/browser/fixture/**`。未改 Go / scripts / config / docs/contracts.md / Makefile。
未 commit / 未 push。

> 本文所有「通过」结论均来自下方可复现命令的完整输出；截图落盘在 `docs/settlement/`。
> 真实 Go 联调未做（见文末「未验证边界」）。

---

## 一、改动文件

| 文件 | 改动 |
| --- | --- |
| `web/src/types.ts` | 新增 `ShowdownDTO`（`seat`/`hand_rank`/`best_five`）并在 `CompletedHandDTO` 增加 `showdown: ShowdownDTO[]`（与 `internal/server/dto.go` 一致） |
| `web/src/format.ts` | 新增 `SETTLEMENT_TITLE`、`potName(index)`（0→主池） |
| `web/src/settlement.ts` | 新增（纯逻辑）：`SettlementPhase`、`SettlementView`、`settlementSchedule`、`showdownParticipantSeats`、`payoutTotalBySeat`、`winnerSeats`、`handRankBySeat`、`finalStackBySeat`、`potRows`、`humanSidePotIndex`、`bestFiveBreakdown`、`settlementAnnouncement` |
| `web/src/hooks/usePrefersReducedMotion.ts` | 新增（从 SpectatorView 抽出，供结算与动作标签共用） |
| `web/src/hooks/useHandSettlement.ts` | 新增：观察 `hands_completed` 状态机 |
| `web/src/components/StreetTrack.tsx` | 新增：5 节点轨道 + 状态行 |
| `web/src/components/WinnerTag.tsx` | 新增：赢家标签 |
| `web/src/components/SettlementBar.tsx` | 新增：桌下结算条 |
| `web/src/components/Seat.tsx` | 新增结算 props（翻牌/角色/最终筹码），结算期隐藏牌背、下注行、行动态 |
| `web/src/components/PokerTable.tsx` | 接入 `StreetTrack`；新增 `settlement` prop 与独立结算模式；赢家标签、筹码飞行、快照数据源 |
| `web/src/components/SpectatorView.tsx` | 接入 `useHandSettlement` + `SettlementBar`；`HandDetail` 手数改为受控 |
| `web/src/styles.css` | 新增街轨道、结算座位/翻牌/筹码/标签/结算条样式与 reduced-motion 覆盖 |
| `tests/e2e/browser/fixture/tests/street-track.spec.ts` | 新增（RED→GREEN）：5 用例 |
| `tests/e2e/browser/fixture/tests/settlement.spec.ts` | 新增（RED→GREEN）：6 用例 |

后端字段核对结论：`ShowdownDTO{seat,hand_rank,best_five}`、`CompletedHandDTO.showdown`、`reason`（`showdown`/`uncontested`）、`pots[].payouts`、`payouts`、`refunds`、`final_stacks` 与设计一致，**无字段名差异**，未硬编兜底字段。

---

## 二、Task 1：街轨道组件 / 状态行实现

组件 `StreetTrack.tsx`，放在原街名位置（`.center` 纵向：公共牌 → 底池 → `.street-label`（轨道 + 状态行）），不遮挡公共牌/底池。

- **五节点**：翻牌前 / 翻牌 / 转牌 / 河牌 / 结束，横向等宽排列，节点 `data-node` + `data-state` 供测试与无障碍定位。
- **节点状态** `data-state`：
  - `done`（已到达）：黄铜描边 + 小勾「✓」+ 低亮度黄铜文字；
  - `current`（当前街）：黄铜金实心圆 + 光晕 + 最亮文字；
  - `future`（未到达）：空心圆、低饱和文字、细暗线；
  - `skipped`（提前结束未进行的街）：短横、弱化；
  - `ended`（本手结束）：第五节点高亮。
- **结束街判定**：由该手 `board.length` 反推最后进行到的街（0→翻牌前、3→翻牌、4→转牌、5→河牌），其后的下注街标 `skipped`，**不伪造未进行的街**。
- **第五节点命名**：默认「结束」；仅当传入的该手 `CompletedHandDTO.reason === 'showdown'` 时显示「摊牌」。**不做 `complete → 摊牌` 直接映射**。
- **状态行文案优先级**：
  1. `status === stopped/failed` → 「牌局已停止」/「牌局失败」（**不画成正常结束**，`.street-node.is-ended` 不出现，第五节点回落 `future`）；
  2. `status === paused` → 「牌局已暂停 · 本街尚未结束」；
  3. 无手牌 → 「等待开始」；
  4. `street === complete` → 「本手已结束」；
  5. 切街（同一手、恰好前进一条下注街）→ 临时 1.5s「翻牌下注已结束 · 转牌下注进行中」，随后恢复常态；
  6. 常态 → `current_bet > 0` 时「下注进行中 · 本街当前注 X」，否则「下注进行中 · 本街尚无人下注」。
- **跳街不伪造**：仅当新街索引 = 旧街索引 + 1 时才弹切街提示；`preflop → river` 这类跨多街直接显示最新街常态，不连续弹「下注结束」。
- **不做**：不显示「还有 N 人待行动」（数据不可靠推断）；不逐街补动画。

无障碍：`.street-status` 带 `role="status" aria-live="polite"`；轨道本身 `aria-hidden`。

---

## 三、Task 2：结算状态机（idle→loading→reveal→award→hold→finished）

`useHandSettlement(game)`（`web/src/hooks/useHandSettlement.ts`）：

- **触发**：观察 `game.hands_completed` 从 N 增到 M；为 `N+1..M` 依次调用 `api.hand(gameId, handNo)`；去重键 `gameId:handNo`。
- **首次进入**只记录基线，不重播历史手；**切视角**（`Stage` 以 `mode` 为 key 重建）同样以当前值为基线，不重播同一手。
- **失败不伪造**：`api.hand` 拉取失败则跳过该手、继续队列，不展示虚构结算。
- 队列：多手同时完成时按序逐手展示。
- **相位时间（摊牌，约 6.5s）**：`revealEnd=1300`、`awardEnd=2800`、`holdEnd=6200`、`finishEnd=6500`；落在设计分段的 0-1300（收束/翻牌/揭晓）、1300-2800（派发）、2800-6200（停留）、6200-6500（收尾）。
- **相位时间（弃牌，约 3.8s）**：`revealEnd=900`、`awardEnd=1800`、`holdEnd=3300`、`finishEnd=3800`。
- `settlementSchedule(reason)` 按 `reason` 分支；`SpectatorView` 将 `{hand, phase, reduced}` 传给 `PokerTable`。

结算期间牌桌进入**独立模式**（`.stage.is-settling`，`data-settlement`）：公共牌 / 底牌 / 获奖 / 最终筹码全部取自该手 `CompletedHandDTO`，抑制动作标签与桌沿下注徽章，杜绝「上一手赢家标签 + 下一手公共牌」混显。

---

## 四、赢家标签 / 翻牌 / 筹码动画参数

**赢家标签**（`WinnerTag.tsx` + CSS `.winner-tag`）：
- 实色深黄铜底 + 亮金边；固定锚点、`transform` 缩放与 `translate` 定位分离、`pointer-events:none`（复用 ActionPopover 同族规则）。
- 金额 21px（真人 24px），牌型 12px；入场 100ms 淡入到 `--win-scale`（1.35 / 真人 1.45）、保持 220ms、再 450ms 缩回 1（`winner-in` 770ms）；`finished` 相位加 `.is-leaving` 退出 250ms（`winner-out`）。
- 文案：`赢 +X`（X = 该座位总 `payouts` 之和，不含 `refunds`）；真人「你赢了 +X」；牌型行 `hand_rank · bestFive 归并`（如「葫芦 · A三条/K一对」，由 `bestFiveBreakdown` 从 `best_five` 归并，无成组时只显示 `hand_rank`）；弃牌结束显示「其余玩家弃牌」，真人只赢边池显示「赢得边池N」。
- **不复用** ActionKind 枚举 / 2.2s 生命周期 / 1.8 倍缩放 / 动作 FIFO。

**摊牌翻牌**（`Seat.tsx` + `.hole.hole-reveal`）：仅 `reason==="showdown"` 的**摊牌参与者**（`pots[].eligible` 并集 ∩ `hole_cards`）翻牌；弃牌者不翻。`perspective:800px`，单张 `rotateY 0→180`（视觉上牌背转正面）360ms ease-out，按座位顺序错峰（`250ms + 序号×60ms`）。

**赢家/输家座位**：赢家 `.seat.is-winner` 2px 金描边 + 18px 光晕 0.3，`winner-seat` 450ms 单次增强后保持静态；赢家底牌金边；输家 `.hole-reveal.is-loser .card` opacity 0.55（姓名/筹码仍可读）。

**筹码飞行**（`.settle-chips/.settle-chip`）：`award/hold` 相位，每赢家 3 枚，起点 ≤--fly-x/--fly-y≥ = 牌桌中心相对座位的像素偏移，`chip-fly` 700ms ease-out，间隔 90ms。

---

## 五、桌下结算条（SettlementBar）

位置：牌桌之后、`ActionDock` 之前，正常文档流，不遮牌桌。
- 标题：`第 N 手 · 摊牌结算 | 弃牌结算`。
- 每池一行 `.settlement-pot`：第一池标「主池」（不是「边池1」），之后「边池1」「边池2」…；行内给出个人 `payout` 与牌型。
- 退回行 `.settlement-refunds`：`退回：你 600`。
- 操作：`查看本手详情`（打开对应手数 `HandDetail`）、`收起`（仅收起当前手，下一手自动重新展示）。
- 移动端可换行/展开。
- 可访问性：`.sr-only[role=status aria-live=polite]` 用 `settlementAnnouncement` 只宣读一次；视觉内容 `aria-hidden`，不重复播报。

**HandDetail 统一口径**：即时界面与详情都由 `pots[].payouts` 展示各池个人 payout，第一池标「主池」。

---

## 六、验证命令与完整输出

### 6.1 typecheck

```
$ npm --prefix web run typecheck

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit
```

（无 error，退出码 0。）

### 6.2 build

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
dist/assets/index--6ohzaJu.css   37.97 kB │ gzip:  9.12 kB
dist/assets/index-BM4adM2m.js   282.27 kB │ gzip: 89.69 kB
✓ built in 694ms
```

### 6.3 fixture 全量（41 用例）

命令：

```
$ cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts --reporter=list
```

完整输出：

```
Running 41 tests using 1 worker

[dock] desktop height=139
  ✓   1 [chromium] › fixture/tests/action-dock.spec.ts:41:1 › 桌面 · 三行紧凑结构、总高下降、无重复信息、按钮≥40px (3.5s)
[dock] thinking height=54
  ✓   2 [chromium] › fixture/tests/action-dock.spec.ts:94:1 › 桌面 · 非我行动（思考中）时操作区仅剩状态行 (3.6s)
[dock] mobile height=202
  ✓   3 [chromium] › fixture/tests/action-dock.spec.ts:109:1 › 移动端 390 · 各行堆叠、触控目标≥40px、不溢出 (3.2s)
  ✓   4 [chromium] › fixture/tests/action-popover-scale.spec.ts:150:1 › 桌面：四种动作统一 scale 1.8 / 2200ms / 四色 (6.1s)
  ✓   5 [chromium] › fixture/tests/action-popover-scale.spec.ts:216:1 › 桌面：精确时间轴（80/320/620/920/1980ms） (2.7s)
  ✓   6 [chromium] › fixture/tests/action-popover-scale.spec.ts:247:1 › 桌面：展开方向 transform-origin（左半/右半/顶部/真人） (6.1s)
  ✓   7 [chromium] › fixture/tests/action-popover-scale.spec.ts:288:1 › 移动端 390：scale 仍 1.8、不横向溢出 (2.3s)
  ✓   8 [chromium] › fixture/tests/action-popover-scale.spec.ts:311:1 › reduced-motion：静态高对比展示约 2200ms (4.5s)
  ✓   9 [chromium] › fixture/tests/action-popover.spec.ts:131:1 › 去重触发 + 同座位 FIFO 不截断 + 不同座位并存 + 跨手不清 + 弃牌不变暗 + 标签定位 (11.7s)
  ✓  10 [chromium] › fixture/tests/action-popover.spec.ts:218:1 › reduced-motion：标签降级为静态可见，随后由 timer 移除 (5.4s)
  ✓  11 [chromium] › fixture/tests/action-popover.spec.ts:247:1 › 人数模板 2/6/9（全 AI）：标签不压中央、不互相重叠、无横向溢出 (7.4s)
  ✓  12 [chromium] › fixture/tests/action-popover.spec.ts:294:1 › 移动端 390：标签落在预留行、不压 .bet、不溢出 (3.6s)
  ✓  13 [chromium] › fixture/tests/human-player.spec.ts:19:1 › 全 AI 创建走公共观战，不出现真人行动面板 (355ms)
  ✓  14 [chromium] › fixture/tests/human-player.spec.ts:31:1 › 真人座位：配置页隐藏模型下拉，创建后进入真人视角 (3.1s)
  ✓  15 [chromium] › fixture/tests/human-player.spec.ts:44:1 › 轮到真人：座位光圈与「轮到我」、底部倒计时进度条与操作按钮组 (3.2s)
  ✓  16 [chromium] › fixture/tests/human-player.spec.ts:76:1 › AI 轮转：底部显示「思考中…」与不确定进度，行动历史高亮新动作 (3.2s)
  ✓  17 [chromium] › fixture/tests/human-player.spec.ts:96:1 › 无 Cookie 浏览器：人类牌局仅公共观战，看不到真人底牌与行动面板 (247ms)
  ✓  18 [chromium] › fixture/tests/human-player.spec.ts:123:1 › 移动端 390 宽：轮转提示与底部操作区可点无遮挡 (3.1s)
  ✓  19 [chromium] › fixture/tests/model-label.spec.ts:52:1 › 桌面：模型短名上移第一行、第二行剩余不换行、未知模型不留孤立括号、卡不增高 (648ms)
  ✓  20 [chromium] › fixture/tests/model-label.spec.ts:102:1 › 移动端 ≤720px：模型短名 11px / max-width 120px、不溢出 (3.2s)
  ✓  21 [chromium] › fixture/tests/quick-bet-keys.spec.ts:26:1 › 快捷注：六个比例按钮渲染，目标值按基准池 clamp，点击填入输入框 (3.3s)
  ✓  22 [chromium] › fixture/tests/quick-bet-keys.spec.ts:57:1 › 快捷键 F：直接弃牌并收起操作面板 (3.2s)
  ✓  23 [chromium] › fixture/tests/quick-bet-keys.spec.ts:66:1 › 快捷键 R 聚焦加注输入框；输入框聚焦时 F/C/K 不误触 (3.2s)
  ✓  24 [chromium] › fixture/tests/quick-bet-keys.spec.ts:87:1 › 键盘 R + 输入 + 回车：提交加注动作 (3.2s)
  ✓  25 [chromium] › fixture/tests/quick-bet-keys.spec.ts:100:1 › 移动端 390 宽：快捷注按钮可见且不超出视口 (3.1s)
  ✓  26 [chromium] › fixture/tests/settlement.spec.ts:134:1 › 摊牌结算：参与者翻牌、弃牌者不翻、赢家金额=payouts、结算条主池命名、快照 (3.1s)
  ✓  27 [chromium] › fixture/tests/settlement.spec.ts:194:1 › 弃牌结束：不翻牌、不显示牌型、显示「其余玩家弃牌」、退回展示 (1.9s)
  ✓  28 [chromium] › fixture/tests/settlement.spec.ts:234:1 › 真人赢：标签「你赢了 +X」、只赢边池明确提示、冰蓝身份保留 (3.5s)
  ✓  29 [chromium] › fixture/tests/settlement.spec.ts:299:1 › 首次进入不重播历史手 (2.8s)
  ✓  30 [chromium] › fixture/tests/settlement.spec.ts:318:1 › reduced-motion：结算静态可见（金边/标签/牌面），不依赖动画 (1.9s)
  ✓  31 [chromium] › fixture/tests/settlement.spec.ts:354:1 › 移动端 390：结算条在牌桌下方、可展开、不遮挡操作区 (1.6s)
  ✓  32 [chromium] › fixture/tests/street-track.spec.ts:73:1 › 五节点轨道：当前街最亮、过去小勾、未来空心、当前注文案 (2.6s)
  ✓  33 [chromium] › fixture/tests/street-track.spec.ts:107:1 › 切街临时提示 1.5s（flop→turn），随后恢复常态 (4.3s)
  ✓  34 [chromium] › fixture/tests/street-track.spec.ts:126:1 › 跳街（preflop→river）不伪造中间切街提示 (3.5s)
  ✓  35 [chromium] › fixture/tests/street-track.spec.ts:146:1 › 暂停：状态行显示「牌局已暂停 · 本街尚未结束」 (516ms)
  ✓  36 [chromium] › fixture/tests/street-track.spec.ts:161:1 › 停止：不把中止画成正常结束 (507ms)
  ✓  37 [chromium] › fixture/tests/table-layout.spec.ts:94:1 › 人数模板 2-9（真人局）：不重叠、不侵入中央、真人底部居中、两行固定 (26.2s)
[layout] stage=1334x734 felt=1158x624
  ✓  38 [chromium] › fixture/tests/table-layout.spec.ts:137:1 › 桌面 · 真人 9 座：底部居中、中央清爽、座位卡两行、牌桌更大 (3.6s)
  ✓  39 [chromium] › fixture/tests/table-layout.spec.ts:163:1 › 桌面 · 全 AI 9 座观战：环绕不重叠、中央清爽 (735ms)
  ✓  40 [chromium] › fixture/tests/table-layout.spec.ts:177:1 › 桌沿徽章：本街下注（本轮 + 累计）落在护栏内侧，不压中央 (1.9s)
  ✓  41 [chromium] › fixture/tests/table-layout.spec.ts:210:1 › 移动端 390：座位可点、不溢出、操作条在视口内 (3.2s)

  41 passed (2.6m)
```

### 6.4 截图（docs/settlement/）

| 文件 | 场景 |
| --- | --- |
| `docs/settlement/showdown-winner.png` | 摊牌赢家：翻牌 + 金边 + 标签 + 结算条 |
| `docs/settlement/uncontested-winner.png` | 弃牌结束：不翻牌、无牌型、其余玩家弃牌、退回 |
| `docs/settlement/human-winner.png` | 真人赢：你赢了 + 只赢边池 + 多池结算条 |
| `docs/settlement/street-track-states.png` | 街轨道各节点状态 |
| `docs/settlement/mobile-settlement.png` | 移动端 390 结算条 |
| `docs/settlement/reduced-motion-static.png` | reduced-motion 静态展示 |

---

## 七、逐条验收对齐（场景 → 测试 → 结果）

### Task 1
| 要求 | 测试 | 结果 |
| --- | --- | --- |
| 街轨道节点状态（done/current/future/skipped/ended） | `street-track.spec.ts:73`、`settlement.spec.ts:194`（skipped） | 通过 |
| 切街文字提示 1.5s | `street-track.spec.ts:107` | 通过 |
| 跳街不伪造 | `street-track.spec.ts:126` | 通过 |
| 暂停文案 | `street-track.spec.ts:146` | 通过 |
| 停止不画成正常结束 | `street-track.spec.ts:161` | 通过 |
| 第五节点 showdown→「摊牌」、complete 默认「结束」 | `settlement.spec.ts:134`（摊牌）、`settlement.spec.ts:194`（结束） | 通过 |

### Task 2
| 要求 | 测试 | 结果 |
| --- | --- | --- |
| 摊牌翻牌、弃牌者不翻 | `settlement.spec.ts:134` | 通过 |
| 赢家标签金额 = 该座位 payouts 之和 | `settlement.spec.ts:134` | 通过 |
| 真人「你赢了」/ 只赢边池 | `settlement.spec.ts:234` | 通过 |
| 弃牌不翻牌、不显示牌型 | `settlement.spec.ts:194` | 通过 |
| 结算条主池命名（主池/边池1） | `settlement.spec.ts:134`、`settlement.spec.ts:234` | 通过 |
| 首次进入不重播历史手 | `settlement.spec.ts:299` | 通过 |
| 结算快照（公共牌/最终筹码来自该手） | `settlement.spec.ts:134` | 通过 |
| reduced-motion 静态 | `settlement.spec.ts:318` | 通过 |
| 桌下结算条位于牌桌之后、不遮挡 | `settlement.spec.ts:354` | 通过 |

### 既有测试不回归
`action-popover`、`action-popover-scale`、`table-layout`、`action-dock`、`quick-bet-keys`、`human-player`、`model-label` 全绿（见 6.3，共 41 passed）。

---

## 八、未验证边界

- **真实 Go 联调未做**：以上为本地 fixture（内存 mock），不能作为真实链路证据。真实 `hands_completed` 递增、`CompletedHandDTO.showdown` 内容、手间窗口与服务端时序需 `make run`（或真实 Go + 浏览器 E2E）验证。
- **实际观感未目视**：翻牌错峰节奏、筹码飞行方向、标签与相邻座位/中央区的视觉关系、移动端多池换行，需人工在真实窗口目视确认。
- **`bestFiveBreakdown` 文案**：从 `best_five` 归并，未成组时只显示 `hand_rank`；与设计示例「A三条/K一对」同构，但特殊牌型的措辞未穷举。
- **手间窗口时长**：前端时间轴（摊牌 6.5s / 弃牌 3.8s）短于服务端窗口（12s / 8s），但若服务端窗口被配置调短到小于前端动画，可能截断；未在真实配置下验证。
