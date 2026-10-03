# 观战 / 真人界面「斗地主式」轮转体验（TURN_UI）

本轮把观战 / 真人界面重做成「欢乐斗地主式」的轮转体验：轮到谁一眼可见、倒计时在屏幕下方、
行动历史清晰滚动、真人操作区集中在底部。仅前端视觉改动，不改 Go、scripts、config、
docs/contracts.md；不 commit / 不 push。

- 分支 / 版本：当前目录无 Git 元数据，未 pull/commit/push；版本新旧未核验。
- 后端并行改动（`internal/service/worker.go` 的 AI 步进延迟）本轮未触碰，前端不依赖其未落地字段。
- 凭据：未读取 `.env.local` / `config.local.yaml` 的值，未读取任何 Cookie 值。

## 一、改动文件

| 文件 | 类型 | 说明 |
|---|---|---|
| `web/src/hooks/useCountdown.ts` | 新增 | 抽出真人倒计时逻辑（服务端截止时间为权威，本地时钟推进，含进度总量）。 |
| `web/src/components/ActionDock.tsx` | 新增 | 底部行动区：当前行动者提示 + 倒计时/进度条 + 真人操作区容器。 |
| `web/src/components/ActionFeed.tsx` | 新增 | 行动历史滚动列表，新动作短暂高亮。 |
| `web/src/components/HumanActionPanel.tsx` | 改 | 精简为「合法动作按钮组」，标题/倒计时交由 ActionDock；沿用提交与超时刷新逻辑。 |
| `web/src/components/Seat.tsx` | 改 | 当前 actor 座位「轮到我 / 思考中 / 等待真人」标签；非行动座位弱化。 |
| `web/src/components/PokerTable.tsx` | 改 | 计算 actorLabel / isDimmed，行动座位置顶。 |
| `web/src/components/SpectatorView.tsx` | 改 | 累积行动历史；底部行动区 + 行动历史 + 运行信息重排。 |
| `web/src/styles.css` | 改 | 底部行动区、倒计时进度条、行动历史、座位光圈/弱化、响应式与降级。 |
| `tests/e2e/browser/fixture/fixture-server.mjs` | 改 | 真人动作后写入 `last_action`（复刻契约），供行动历史 / 思考态验证。 |
| `tests/e2e/browser/fixture/tests/human-player.spec.ts` | 改 | 覆盖轮转提示、倒计时进度、行动历史高亮、移动端无遮挡。 |
| `docs/TURN_UI.md` | 新增 | 本证据文档。 |

## 二、斗地主式交互要点与实现位置

### 1. 轮到谁（核心）
- `web/src/components/Seat.tsx`：当前 actor 在座位顶部渲染 `.turn-badge`（`轮到我` / `思考中` /
  `等待真人`），并加 `.is-actor`；非当前行动且仍在参与本手的座位加 `.is-dimmed`。
- `web/src/components/PokerTable.tsx`：由 `hand.actor` 计算 `isActor`、`actorLabel`、`isDimmed`；
  行动座位 `seat-slot` 加 `.is-actor-slot` 置顶，避免放大后被相邻座位遮挡。
- `web/src/styles.css`：`.seat.is-actor`（黄铜光圈 + `scale(1.06)` + `actor-pulse` 呼吸）、
  `.seat.is-dimmed`（透明度 0.6 + 降饱和）、`.turn-badge` + `badge-in`。
- `web/src/components/ActionDock.tsx`：`data-turn`（`me` / `ai` / `human` / `idle`）与
  `.dock-state` 文案，统一暴露「轮到谁」。

### 2. 倒计时在下方
- `web/src/hooks/useCountdown.ts`：以 `player.action_deadline` / `server_time` 为权威，
  换算本地剩余，并记录同一截止时间的首次剩余量作为进度总量；归零仅 `expired`，前端不自动弃牌。
- `web/src/components/ActionDock.tsx`：真人等待时 `.dock-timer` 显示确定进度条
  （`remaining/total`）+ `.countdown` 数字；AI 行动且未到真人等待点时显示 `.dock-timer.is-thinking`
  的不确定进度条与「思考中…」。
- `web/src/styles.css`：`.hand-dock { position: sticky; bottom: 0 }`、`.dock-timer-fill`、
  `thinking-sweep`、紧迫态 `.urgent` 变红。
- 归零刷新：`HumanActionPanel.tsx` 内 `expired` 触发一次 `onRefresh()`，等待服务端超时弃牌，浏览器不发弃牌。

### 3. 行动历史滚动
- `web/src/components/SpectatorView.tsx`：后端只下发 `last_action`，前端按 `(hand_no, sequence)`
  去重累积为 `history`（上限 60 条），纯展示、不改契约。
- `web/src/components/ActionFeed.tsx`：`role="log"` 滚动列表，新动作自动滚到底部；最新一条加 `.is-new`
  触发入场高亮；展示「座位名 + 弃牌/过牌/跟注/加注到 X + 来源」。
- `web/src/styles.css`：`.action-feed`（`max-height` + 纵向滚动）、`.feed-row`、`feed-pop` 高亮。

### 4. 真人操作区
- `web/src/components/HumanActionPanel.tsx`：底部呈现「弃牌 / 过牌 / 跟注 / 加注 / 全押 + 加注输入」，
  沿用 `expected_revision` 提交、409 对齐刷新、超时归零刷新、不自动弃牌。
- `web/src/components/ActionDock.tsx`：真人视角时把该面板嵌入底部行动区（`.action-panel` 保留）。
- `web/src/styles.css`：底部大按键（`min-height: 46px`、`flex: 1`）、`.raise-control`。

### 5. 视觉一致性 / 移动端 / 降级
- `web/src/components/SpectatorView.tsx` + `styles.css`：`.lower-deck` 作为底部行动区的包含块，
  sticky 不能越过其顶部，因此行动区**永不遮盖牌桌底部座位**（2 座底中座位已验证可见）。
- `styles.css`：`@media (max-width: 900px)` 结果区单列；`@media (max-width: 720px)` 行动区贴底、
  按钮两列、本人底牌不再负偏移避免裁切；全局 `@media (prefers-reduced-motion: reduce)` 关闭动画。

## 三、验证输出（完整，未截断）

### 3.1 `npm --prefix web run typecheck`
```text

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

typecheck_exit=0
```

### 3.2 `npm --prefix web run build`
```text

> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
transforming...
✓ 42 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.44 kB
dist/assets/index-BScmiY94.css   24.45 kB │ gzip:  6.62 kB
dist/assets/index-BcXB2nS0.js   262.62 kB │ gzip: 82.63 kB
✓ built in 705ms
build_exit=0
```

### 3.3 fixture 浏览器测试（`npx playwright test --config=playwright.fixture.config.ts`）
来源：`tests/e2e/browser/fixture/tests/human-player.spec.ts`，命令在 `tests/e2e/browser/` 下执行。
```text
Running 6 tests using 1 worker

  ✓  1 [chromium] › fixture/tests/human-player.spec.ts:19:1 › 全 AI 创建走公共观战，不出现真人行动面板 (407ms)
  ✓  2 [chromium] › fixture/tests/human-player.spec.ts:31:1 › 真人座位：配置页隐藏模型下拉，创建后进入真人视角 (3.1s)
  ✓  3 [chromium] › fixture/tests/human-player.spec.ts:44:1 › 轮到真人：座位光圈与「轮到我」、底部倒计时进度条与操作按钮组 (3.2s)
  ✓  4 [chromium] › fixture/tests/human-player.spec.ts:76:1 › AI 轮转：底部显示「思考中…」与不确定进度，行动历史高亮新动作 (3.2s)
  ✓  5 [chromium] › fixture/tests/human-player.spec.ts:96:1 › 无 Cookie 浏览器：人类牌局仅公共观战，看不到真人底牌与行动面板 (296ms)
  ✓  6 [chromium] › fixture/tests/human-player.spec.ts:123:1 › 移动端 390 宽：轮转提示与底部操作区可点无遮挡 (3.2s)

  6 passed (13.9s)
fixture_exit=0
```

场景覆盖：需求 5 条要点逐条对应到用例——
轮到提示（用例 3/4）、倒计时在下方（用例 3/6）、行动历史滚动（用例 4）、
真人操作区（用例 3/6）、移动端无遮挡（用例 6）、观战隔离（用例 1/5）。

## 四、未验证边界

- **真实 Go 联调未做**：本轮只跑 fixture 浏览器验证，fixture 不算真实链路。
  真实链路（同源 Go 嵌入 Web + MySQL 3306 / Redis 6379 + 8081）需另行运行
  `python3 tests/e2e/browser/run_human.py`，本轮未执行。
- `tests/e2e/browser/human/human.spec.ts`（真实 Go）保留了对 `.action-panel.is-turn`、
  `.countdown`、加注输入、各类按钮的选择器，代码未破坏，但**本轮未在真实服务上执行**。
- 后端并行在加「AI 步进延迟」（`internal/service/worker.go` 等当前存在编译错误），
  本轮未构建/运行后端；前端「思考中」是从已有 DTO 推导，未与真实 AI 停顿时长对齐验证。
- 真实超时（`human_action_timeout`）触发的服务端弃牌与倒计时归零刷新，仅由 fixture 行为间接覆盖，
  未在真实服务上验证。

## 五、DTO 缺口（记录，未改后端）

现有 DTO **没有**显式表达「当前 actor 是否处于思考/步进停顿」的字段
（`HandDTO` 只有 `actor` 与 `waiting_for`，`PlayerDTOView` 只有 `action_deadline`）。

- 前端处理：`ActionDock` 用 `hand.actor != null && hand.waiting_for != 'human_action' && status === 'running'`
  推断 AI「思考中…」，用 `player.action_deadline` 得到真人倒计时。无需新增字段即可实现本轮视觉需求。
- 缺口影响：若后端 AI 停顿时长需要可视化展示剩余时间（而不只是不确定进度条），
  需要后端新增类似「思考截止时间 / 剩余时长」的字段。本轮**未改** `internal/server/dto.go`，
  仅在此记录。
