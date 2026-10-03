# 座位卡「筹码不减少」问题 · 真实浏览器复现与验证报告

结论先行：**用户报告的「下注后座位卡筹码数字不减少」在当前 HEAD 无法复现**。
座位卡早已渲染实时余额 `hand.players[].stack`，下注后数字确实会减少；
`seats[].stack`（结算余额）只在下注时的 `hand === null` 瞬时时序窗里短暂显示。
本次改动是针对任务要求的展示语义澄清（剩余 / 本轮 / 累计），不是数值修复。

- 仓库：`/Users/gaosper/Documents/mycode/texas-holdem`
- HEAD：`923828b`（2026-10-03 21:35:34 +0800）
- 工作区改动：`web/src/components/Seat.tsx`、`web/src/styles.css`（未提交）
- 真实链路：Chromium → 同源 Go（`go:embed web/dist`）→ MySQL 3306 / Redis 6379，无 fixture / route mock

---

## 一、真实浏览器复现

### 1.1 方法

临时 Playwright 用例（chromium，独立端口 8082，非 8081）：
1. 真人局：UI 创建（真人座位 0 + AI 座位 1，SB 5 / BB 10），UI 点「开始」，
   等待 `waiting_for === 'human_action'`；抓座位 0 与座位 1 的 `.seat-stack` DOM 文本与
   `GET /games/{id}/player` 的 `hand.players[].stack`；UI 填「加注到 20」并点「加注」；
   再次抓 DOM 与（含）动作响应体里的实时余额。
2. 全 AI 局：UI 创建（两座 AI），UI 点「开始」；先等页面自身轮询渲染出盲注下注行，
   抓座位 0 DOM 与 `GET /games/{id}`（公共视角）的实时/结算余额。

> 说明：临时用例文件在 `tests/e2e/browser/chip/` 与 `tests/e2e/browser/playwright.chip.config.ts`，
> 属本次排查临时产物，验证后已删除（写范围限定 `web/src/**` + 本报告）。

### 1.2 下注前后 DOM 文本证据

真人视角（修复前，标签仍是「筹码」）：

| 时点 | 座位0 DOM `.seat-stack` | 座位0 DOM `.bet` | API `hand.players[0].stack`（实时） | API `seats[0].stack`（结算） |
|---|---|---|---|---|
| 下注前（已下 SB 5） | `9,995 筹码` | `本轮 5` | `9995` | `10000` |
| 加注到 20 后 | `9,980 筹码` | `本轮 20` | `9980`（动作响应体同为 `9980`） | `10000` |

结论：DOM 数字 = 实时余额，且下注后 9,995 → 9,980 明确减少；结算余额 10,000 未变（设计如此）。
同一手内座位1（AI，已下 BB）DOM 为 `9,990 筹码`，同样是实时值。

全 AI 公共视角（`human_seat=null`，`game.player=null`）：

| 时点 | 座位0 DOM | API 实时 | API 结算 |
|---|---|---|---|
| preflop（等页面渲染出下注行后） | `9,900 筹码` | `9900` | `10000` |

结论：公共观战视角同样渲染实时余额。

### 1.3 曾出现的「10,000」及其解释

首轮 AI 公共视角抓取时曾读到 DOM `10,000 筹码`，而 API 已是 `9950`。
定位为**时序竞态**：点「开始」后页面先应用命令响应，若该帧 `hand === null`
（手牌尚未进入视图），`PokerTable.playerBySeat` 为空 → `Seat` 回退 `seat.stack`（结算余额 10000）；
下一次 1s 轮询到达后即切回实时值。用例加入「等待页面渲染出盲注下注行」后稳定读到 `9,900 筹码`，
即该现象非持久。`Seat.tsx:41-43` 的 `live = player !== null` 正是此回退分支。

### 1.4 截图

- `/var/folders/w2/bntp6mgj2nn81dbr0cc2nnw00000gn/T/opencode/chip-shot/01-before.png`（真人下注前，座位0 `9,995 剩余`）
- `/var/folders/w2/bntp6mgj2nn81dbr0cc2nnw00000gn/T/opencode/chip-shot/02-after.png`（真人加注到 20 后，座位0 `9,980 剩余`）
- `/var/folders/w2/bntp6mgj2nn81dbr0cc2nnw00000gn/T/opencode/chip-shot/03-ai-public.png`（全 AI 公共观战，座位0 `9,900 剩余`）
- DOM/API 原始记录：`/var/folders/w2/bntp6mgj2nn81dbr0cc2nnw00000gn/T/opencode/chip-shot/evidence.jsonl`

---

## 二、定位：前端渲染了哪个值，为什么

| 位置 | 代码 | 结论 |
|---|---|---|
| `web/src/components/PokerTable.tsx:23-27,71` | `playerBySeat` 由 `hand.players` 构建；`playerState = playerBySeat.get(seat.seat) ?? null` | 正确 |
| `web/src/components/Seat.tsx:41-43` | `live = player !== null`；`stack = live ? player.stack : seat.stack` | 正确，手中用实时余额 |
| `internal/server/dto.go:20-26,188-190` | `PlayerDTO.Stack` ← `p.Stack`（实时）；`SeatDTO.Stack` ← `v.Seats[].Stack`（结算） | 字段映射正确 |

后端 HTTP 实测（curl 直连，AI 局）：盲注投入后 `hand.players[].stack` `10000→9990`，
`seats[].stack` 保持 `10000`。前端 `Seat.tsx` 取的是前者。**根因不在 DTO/后端，也不在前端逻辑——该数值 bug 在 HEAD 不存在。**

用户看到「不减少」的两种可能来源：
1. 观察窗口恰好落在 `hand === null` 的瞬时时序（开始/两手之间），此时座位卡显示结算余额；
2. 运行中的 Go 进程嵌入的是**旧 `web/dist`**（`dist` 被 gitignore，需 `npm run build` 后重启服务才生效）。

---

## 三、改动：明确区分「剩余 / 本轮 / 累计」

`hand.players[].street_bet` 进入下一街会被清零、`committed`（整手累计）不变，
此前座位卡只显示「本轮」且街切换后整行消失，容易让人误判筹码去向。改动（仅前端）：

- `web/src/components/Seat.tsx`
  - `.seat-stack` 单位由「筹码」改为「剩余」，并加 `data-live` 与 `title`（`剩余筹码 = 初始筹码 − 累计投入`；非手内为「最近结算筹码」）。
  - `.bet` 行改为一并显示「本轮 {street_bet} · 累计 {committed}」，绘制条件放宽为 `street_bet > 0 || committed > 0`（街切换后仍展示累计）。
- `web/src/styles.css`：新增 `.seat .bet .bet-sep` 分隔符样式。

不变量：`剩余(stack) + 累计(committed) = 本手初始筹码`；`本轮 ⊆ 累计`。

改动后 DOM 证据（真实浏览器）：

| 时点 | 座位0 DOM | 座位0 `.bet` |
|---|---|---|
| 下注前 | `9,995 剩余` | `本轮 5 · 累计 5` |
| 加注到 20 后 | `9,980 剩余` | `本轮 20 · 累计 20` |

---

## 四、验证命令与完整输出

### 4.1 真实浏览器（修复后）

```text
$ npx --no-install playwright test --config=playwright.chip.config.ts
Running 2 tests using 1 worker

CHIP_EVIDENCE before: {"seat0_dom_stack":"9,995 剩余","seat0_dom_bet":"本轮 5 · 累计 5","seat0_api_live_stack":9995,"seat0_api_seat_stack":10000,"seat1_dom_stack":"9,990 剩余","actor":0}
CHIP_EVIDENCE after: {"seat0_dom_stack":"9,980 剩余","seat0_dom_bet":"本轮 20 · 累计 20","seat0_api_live_stack":9980,"seat0_action_response_stack":9980,"seat0_api_seat_stack":10000,"seat1_dom_stack":"9,990 剩余","raise_to":20}
  ✓  1 [chromium] › chip/chip.spec.ts:47:1 › chip_after_bet · 真人下注前后座位卡筹码与 API 对照 (1.1s)
CHIP_EVIDENCE ai_public: {"dom_stack":"9,900 剩余","api_live_stack":9900,"api_seat_stack":10000,"street":"preflop"}
  ✓  2 [chromium] › chip/chip.spec.ts:125:1 › chip_ai_public · 全 AI 公共观战：座位卡显示实时余额而非结算余额 (1.8s)

  2 passed (3.4s)
```

### 4.2 Go race 全量

```text
$ go test -mod=readonly -race -count=1 ./...
ok  	texas-holdem/cmd/config	1.201s
ok  	texas-holdem/cmd/server	1.569s
ok  	texas-holdem/internal/agent	1.634s
ok  	texas-holdem/internal/conf	1.346s
ok  	texas-holdem/internal/data	2.223s
ok  	texas-holdem/internal/poker	1.980s
ok  	texas-holdem/internal/server	1.900s
ok  	texas-holdem/internal/service	2.488s
?   	texas-holdem/web	[no test files]
```

### 4.3 前端 typecheck + build

```text
$ npm --prefix web run typecheck
> tsc --noEmit
（无错误）

$ npm --prefix web run build
> tsc --noEmit && vite build
✓ 42 modules transformed.
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-DO-kHOcF.css   24.49 kB │ gzip:  6.63 kB
dist/assets/index-TfRB24FV.js   262.89 kB │ gzip: 82.75 kB
✓ built in 710ms
```

---

## 五、未验证边界

- 未覆盖 3–9 人桌、全押/短全押跟注、多手牌连续结算后的座位卡；
- 未覆盖 live/付费模型、失败注入、热重载、移动端 390px 布局（新增 `· 累计` 文案后宽度未做 390 宽实测）；
- 未覆盖 `hand === null` 瞬时时序的持续时长量化（仅确认 1s 轮询内自愈）；
- 未在用户实际运行的进程上核对其 `web/dist` 构建时间（旧产物假设未直接验证）。
