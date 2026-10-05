
# 谁赢了结算 / 街进度 / 模型短名 真实链路验收证据

入口：`python3 tests/e2e/browser/run_settlement.py`。时间：2026-10-05 11:17:59 +0800

真实 Chromium → 同源 Go 嵌入 Web → Homebrew MySQL 3306 / Redis 6379。无 fixture/route mock；mock 模型目录、allow_paid_models=false。

启动方式等价于 `make run`：先 `npm --prefix web run build` 生成最新 web/dist，再 `go build -mod=readonly -o <tmp>/server ./cmd/server` 产出含 showdown 字段与手间窗口的最新二进制，随后前台运行该二进制。启动前经 `scripts/local.py:stop_stale_server`清理 8081 残留 server。显式非敏感覆盖：POKER_HTTP_HOST=127.0.0.1、POKER_HTTP_PORT=8081、POKER_HUMAN_ACTION_TIMEOUT=5s、POKER_CATALOG_PATH=scripts/mock-catalog.json、POKER_ALLOW_PAID_MODELS=false；AI 步进沿用 1.5s、手间窗口默认摊牌 12s/弃牌 8s。不读取 .env.local / config.local.yaml 的值，不提取 Cookie 凭据；存储连接只由 local.py 连接助手/Go 进程在本地加载。禁用 trace/HAR/video。

截图证据目录：`tests/e2e/browser/settlement/artifacts/20261005-111759`。


预检：MySQL 3306 / Redis 6379 连接成功；active_games=0；8081 空闲。

### 命令

```sh
npm --prefix web run build
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
transforming...
✓ 50 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index--6ohzaJu.css   37.97 kB │ gzip:  9.12 kB
dist/assets/index-CYkoxh88.js   282.27 kB │ gzip: 89.68 kB
✓ built in 672ms
```

退出码：0

### 命令

```sh
go build -mod=readonly -o /Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/.settle-runtime-0al9fbp_/server ./cmd/server
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

```

退出码：0

### 启动

等价于 local.py dev 的 `service_env(e2e=True, explicit=...)` 环境，直接运行本轮新构建二进制。服务原始日志不保存、不回传，避免凭据进入证据。

实测 health：`{"mysql": "available", "redis": "available", "storage_degraded": false, "allow_paid_models": false, "max_live_calls": 1000}`；models 仅 mock。

### 命令

```sh
npx --no-install playwright test --config=playwright.settlement.config.ts
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser`

```text

Running 2 tests using 1 worker

EVIDENCE showdown: {"models":[{"ref":"mock","label":"Mock"}],"seat_facts":[{"title":"AI-1 · Mock","row1":"0AI-1DSB","row2":"10,010剩余","model_short":null,"row2_white_space":"nowrap","stack_height":24},{"title":"AI-2 · Mock","row1":"1AI-2BB","row2":"9,990剩余","model_short":null,"row2_white_space":"nowrap","stack_height":24}],"short_names":[],"street_nodes":[{"node":"preflop","state":"current","text":"翻牌前"},{"node":"flop","state":"future","text":"翻牌"},{"node":"turn","state":"future","text":"转牌"},{"node":"river","state":"future","text":"河牌"},{"node":"complete","state":"future","text":"结束"}],"transition_text":"翻牌下注已结束 · 转牌下注进行中","settlement_head":"第 1 手 · 摊牌结算","settlement_pots":["主池20AI-1 20 · 两对"],"settlement_refunds":0,"winner_tag":"赢 +20两对 · K一对/Q一对/7单张","winner_rank":"两对 · K一对/Q一对/7单张","transition_runs_ms":[1442,1358,1442],"fly_chip_elements":3,"reveal_count":2,"winner_dom_seats":[0],"api_showdown":[{"seat":0,"hand_rank":"两对","best_five":["Qc","7d","Kh","Qd","Kd"]},{"seat":1,"hand_rank":"两对","best_five":["Qc","7d","Kh","7s","Qs"]}],"api_payout_seats":[0],"api_final_stacks":[{"seat":0,"stack":10010},{"seat":1,"stack":9990}],"settlement_window_ms":6401,"hand_gap_ms":11150,"screenshots":["street-preflop.png","street-flop.png","street-transition.png","settlement-showdown.png","settlement-bar-showdown.png","winner-tag-crop.png"]}
  ✓  1 [chromium] › settlement/settlement.spec.ts:234:1 › settlement_showdown · 模型短名 + 5 节点街轨道 + 摊牌结算与手间窗口 (23.1s)
EVIDENCE uncontested: {"settlement_head":"第 1 手 · 弃牌结算","settlement_pots":["主池10AI-2 10"],"winner_tag":"赢 +10其余玩家弃牌","reveal_count":0,"fly_chip_elements":3,"api_reason":"uncontested","api_showdown":[],"api_hole_cards":[],"api_board":[],"hand_gap_ms":8025,"screenshots":["settlement-uncontested.png","settlement-bar-uncontested.png"]}
  ✓  2 [chromium] › settlement/settlement.spec.ts:435:1 › settlement_uncontested · 真人弃牌：弃牌者不翻牌 + hole_cards 不含他人底牌 (11.1s)

  2 passed (34.8s)
```

退出码：0

```text
playwright: passed=2 failed=0 not_run=0 exit=0
```

```text
cleanup: own_server_stopped=True; 8081_released=True; Homebrew_3306_6379_retained=True
```

### 命令

```sh
go test -mod=readonly -race -count=1 ./...
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text
ok  	texas-holdem/cmd/config	4.070s
ok  	texas-holdem/cmd/server	3.967s
ok  	texas-holdem/internal/agent	4.347s
ok  	texas-holdem/internal/conf	4.294s
ok  	texas-holdem/internal/data	4.421s
ok  	texas-holdem/internal/poker	4.788s
ok  	texas-holdem/internal/server	4.819s
ok  	texas-holdem/internal/service	6.804s
?   	texas-holdem/web	[no test files]
```

退出码：0

```text
go-race: passed_packages=8 failed_packages=0 exit=0
```

## 结论与关键证据摘要

三项功能均在真实链路（真实 Go 服务 + Chromium，非 fixture/route mock）验证通过；前后端字段与行为一致，未发现契约不一致。

1. **模型名（座位卡第一行）**：实测服务仅提供 mock 模型（`ref=mock`、`label=Mock`）。`web/src/format.ts:71-78` 的 `MODEL_SHORT_LABEL` 只映射 `deepseek/deepseek-v4-pro`，mock 未收录，故座位卡首行**按设计省略短名**：`.seat-model` 节点不存在（`short_names: []`），无「(V4 Pro)」文本；完整 label 保留在座位卡 `title`（实测 `AI-1 · Mock`）。首行实际文本为 `0AI-1DSB` / `1AI-2BB`（座位号+名字+身份标记）。第二行「剩余」`white-space: nowrap` 且实测高度 24px，单行不换行（`row2: "10,010剩余"`）。**这是 mock 下的预期降级现象，不是缺陷；「AI-x (V4 Pro)」短名文本在真实链路不可出现，原因见 file:line。**

2. **街进度指示器**：牌桌中央出现 5 节点轨道，`data-node` 顺序 `preflop→flop→turn→river→complete`，标签「翻牌前/翻牌/转牌/河牌/结束」；开局翻牌前为 `current`，其余 `future`。状态行为「下注进行中 · 本街当前注 X」。切街提示实测文案「**翻牌下注已结束 · 转牌下注进行中**」，真实停留 `[1442, 1358, 1442] ms`（约 1.5s，断言区间 1000–2200ms）。证据：`street-preflop.png` / `street-flop.png` / `street-transition.png`。

3. **谁赢了结算**：
   - **摊牌**：`reason=showdown`，翻开 2 家底牌（`.hole-reveal=2`，共 4 张牌面），赢家座位亮金（`.seat.is-winner`），出现「赢 +20 两对 · K一对/Q一对/7单张」标签（含牌型），筹码飞向赢家（`.settle-chip=3`），桌下结算条标「第 1 手 · 摊牌结算」+「主池 20 AI-1 20 · 两对」。实拍：`settlement-showdown.png` / `settlement-bar-showdown.png` / `winner-tag-crop.png`。
   - **弃牌结束**：真人弃牌触发 `reason=uncontested`，**弃牌者不翻牌**（`.hole-reveal=0`），标签「赢 +10 其余玩家弃牌」（无牌型），结算条「第 1 手 · 弃牌结算」+「主池 10 AI-2 10」；筹码仍飞向赢家（3 枚）。实拍：`settlement-uncontested.png` / `settlement-bar-uncontested.png`。
   - **手间窗口**：结算动画期间公共牌不被重置、不开下一手（采样 `board>=5` 全程成立）；结算展示窗口实测约 6400ms；完成到下一手实测 11150ms（摊牌，≈12s）与 8025ms（弃牌，≈8s），与 `internal/service/worker.go:434-439` 的 12s/8s 一致。
   - **口径说明**：结算条本身展示主池/边池/退回；「最终筹码」在结算期间由**座位卡** `.seat-stack` 承载（`web/src/components/Seat.tsx:63` 用 `final_stacks` 覆盖），测试已断言 DOM 座位筹码逐座位等于 API `final_stacks`，两者一致，非缺失。

4. **API 层（真实 fetch，同源）**：
   - 摊牌：`GET /api/v1/games/{id}/hands/1` 的 `CompletedHandDTO.showdown` 为 2 项，字段恰为 `seat`/`hand_rank`/`best_five`，`hand_rank` 为中文牌型、`best_five` 5 张；`hole_cards` 2 家、`board` 5 张、`final_stacks` 2 项。赢家座位与 DOM 亮金座位、API `payouts>0` 三者一致。
   - 弃牌结束（uncontested）：`showdown=[]`、`hole_cards=[]`、`board=[]` —— 弃牌者与他人底牌均不下发，保密成立。

5. **完整命令输出**：见上文各 `### 命令` 代码块的未截断原始输出（前端构建 / Go 构建 / health / Playwright 全量含 2 passed / Go race 8 包 ok）。

6. **未验证边界**：mock 短名未收录（原因见上）；付费/真实模型、多真人、边池与退款非零、非 loopback 部署、故障注入、移动端布局未覆盖。

## 场景与测试函数

| 场景 | 测试函数 | 证据 |
|---|---|---|
| 1a 模型短名上移第一行 / 第二行剩余不换行 | settlement_showdown | seat_facts EVIDENCE + street-preflop.png |
| 1b 街轨道 5 节点 + 状态行 + 切街 1.5s 提示 | settlement_showdown | street-*.png + transition_text EVIDENCE |
| 1c 摊牌结算（翻牌/亮金/赢+X/筹码飞/结算条） | settlement_showdown | settlement-*.png + EVIDENCE |
| 1d API CompletedHandDTO.showdown 字段 | settlement_showdown | api_showdown EVIDENCE |
| 2 弃牌结束：弃牌者不翻 + hole_cards 保密 | settlement_uncontested | settlement-uncontested.png + EVIDENCE |

测试源码：`tests/e2e/browser/settlement/settlement.spec.ts`。结果以未截断原始输出为准。

未验证边界：mock 模型短名未收录（`web/src/format.ts:MODEL_SHORT_LABEL` 只映射 `deepseek/deepseek-v4-pro`），故「AI-x (V4 Pro)」短名文本无法在真实链路出现，只记录「mock 省略短名 + title 保留 label」的实际现象；付费/真实模型、多真人、边池/退款非零、非 loopback 部署、故障注入未覆盖。

最终状态：PASS / 本轮真实链路与 Go race 均通过。
