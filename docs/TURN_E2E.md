
# 轮转 UI 真实端到端证据

运行时间：2026-10-03 21:25:12 +0800

复现命令（项目根目录）：`python3 tests/e2e/browser/run_turn.py`。
真实 Go 新构建二进制/嵌入 Web + Chromium + Homebrew MySQL3306/Redis6379。mock-only、allow_paid_models=false、真人超时 5s、AI 步进 1.5s（显式固定默认值）。
不读取 .env.local；连接凭据仅由 local.py 连接助手/Go 在本地加载，受保护配置内容不回传。不读取用户模型目录，不打印 Cookie 凭据；Set-Cookie 回归仅检查属性布尔值。不启用 trace/HAR/video/storageState；截图只有渲染 UI，不含请求头。
本地无 Git 元数据，未 pull、commit、push，版本新旧未核验。

```text
preflight: MySQL3306=available Redis6379=available active_games=0 8081=free
```

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
✓ 42 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-BScmiY94.css   24.45 kB │ gzip:  6.62 kB
dist/assets/index-BcXB2nS0.js   262.62 kB │ gzip: 82.63 kB
✓ built in 935ms
```

退出码：0

### 命令

```sh
go build -mod=readonly -o /Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/.turn-runtime-bd64rt_v/server ./cmd/server
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

```

退出码：0

# 轮转 UI 真实端到端证据

运行时间：2026-10-03 21:27:43 +0800

复现命令（项目根目录）：`python3 tests/e2e/browser/run_turn.py`。
真实 Go 新构建二进制/嵌入 Web + Chromium + Homebrew MySQL3306/Redis6379。mock-only、allow_paid_models=false、真人超时 5s、AI 步进 800ms（显式短值，仅 mock 生效）。
不读取 .env.local；连接凭据仅由 local.py 连接助手/Go 在本地加载，受保护配置内容不回传。不读取用户模型目录，不打印 Cookie 凭据；Set-Cookie 回归仅检查属性布尔值。不启用 trace/HAR/video/storageState；截图只有渲染 UI，不含请求头。
本地无 Git 元数据，未 pull、commit、push，版本新旧未核验。

```text
preflight: MySQL3306=available Redis6379=available active_games=0 8081=free
```

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
✓ 42 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-BScmiY94.css   24.45 kB │ gzip:  6.62 kB
dist/assets/index-BcXB2nS0.js   262.62 kB │ gzip: 82.63 kB
✓ built in 706ms
```

退出码：0

### 命令

```sh
go build -mod=readonly -o /Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/.turn-runtime-br1y4v7a/server ./cmd/server
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

```

退出码：0

## 启动方式

使用 `local.service_env(e2e=True, explicit=...)` 的等价 dev 环境。显式覆盖 POKER_HTTP_HOST=127.0.0.1、POKER_HTTP_PORT=8081、POKER_HUMAN_ACTION_TIMEOUT=5s、POKER_AI_TURN_DELAY=800ms；E2E 强制 POKER_ALLOW_PAID_MODELS=false 与 scripts/mock-catalog.json。直接运行本轮二进制：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/.turn-runtime-br1y4v7a/server`，cwd 为项目根目录。服务原始日志丢弃，避免凭据出现在证据中。

```text
health: {"mysql": "available", "redis": "available", "storage_degraded": false, "allow_paid_models": false, "max_live_calls": 1000}; models=[mock]
```

### 命令

```sh
npx --no-install playwright test --config=playwright.turn.config.ts
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser`

```text

Running 7 tests using 1 worker

EVIDENCE a/b: {"create":201,"human_model_omitted":true,"ai_model_required":400,"cookie":{"name":"poker_player","issued":true,"httpOnly":true,"strict":true,"gamePath":true},"provider_calls":0}
EVIDENCE c/g: {"authenticated_player":200,"own_hole_count":2,"legal_actions":true,"anonymous_player":403,"anonymous_code":"PLAYER_NOT_AUTHORIZED","public_player":null,"public_private_keys":0,"unfinished_hand":409,"ai_open_holes":0,"anonymous_open_holes":0}
EVIDENCE h: {"page_reload":true,"recovered_player":200,"same_own_holes":true,"cookies_manually_added":0}
  ✓  1 [chromium] › human/human.spec.ts:123:1 › human_identity_privacy_refresh · a/b/c/d/g/h 真实创建、会话、私牌、匿名与刷新 (8.0s)
EVIDENCE e/errors: {"illegal":400,"illegal_code":"ILLEGAL_ACTION","unchanged_revision":true,"unchanged_deadline":true,"stale":409,"stale_code":"STALE_ACTION"}
EVIDENCE e/call: {"status":200,"request":{"expected_revision":4,"kind":"call"},"before_revision":4,"after_revision":5,"source":"human"}
EVIDENCE d: {"waiting_for":"human_action","fold":true,"call":true,"check":true,"raise":true,"all_in":true,"countdown_max_seconds":5,"buttons_follow_legal_actions":true}
EVIDENCE e/check: {"status":200,"request":{"expected_revision":10,"kind":"check"},"before_revision":10,"after_revision":11,"source":"human"}
EVIDENCE e/raise_to: {"status":200,"request":{"expected_revision":14,"kind":"raise_to","to":10},"before_revision":14,"after_revision":15,"source":"human"}
EVIDENCE e/fold: {"status":200,"request":{"expected_revision":20,"kind":"fold"},"before_revision":20,"after_revision":21,"source":"human"}
EVIDENCE g/completed: {"completed_hand":200,"revealed_seats":2,"revealed_cards_per_seat":2}
  ✓  2 [chromium] › human/human.spec.ts:194:1 › human_actions · d/e/g 真实 UI call/check/raise_to/fold、400、409 与完成揭牌 (8.2s)
EVIDENCE e/call: {"status":200,"request":{"expected_revision":4,"kind":"call"},"before_revision":4,"after_revision":5,"source":"human"}
EVIDENCE f: {"configured_timeout_seconds":5,"remaining_at_observation_ms":4952,"elapsed_ms":4272,"can_check_before_timeout":true,"action":{"hand_no":1,"sequence":4,"seat":0,"kind":"fold","to":0,"source":"fallback","error_code":"HUMAN_ACTION_TIMEOUT","input_tokens":null,"output_tokens":null},"browser_timeout_action_posts":0,"status":"completed","hands_completed":1,"before_revision":10,"after_revision":11,"completed_hand":200,"live_calls_used":0}
  ✓  3 [chromium] › human/human.spec.ts:244:1 › human_timeout · f 5 秒无人操作，即使可 check 也由服务端 fallback 弃牌并结算 (9.9s)
EVIDENCE h/running: {"status":"running","player_http":200,"same_own_holes":true,"same_deadline":true,"manually_added_cookies":0}
EVIDENCE f/next-hand: {"status":"running","next_hand_number":2,"hands_completed":1,"first_hand_persisted":200,"browser_action_posts":0,"live_calls_used":0,"public_player_in_next_hand":null}
  ✓  4 [chromium] › human/human.spec.ts:276:1 › human_running_refresh_next_hand · f/h 运行中刷新恢复、超时后继续下一手 (8.2s)
  ✘  5 [chromium] › turn/turn.spec.ts:77:1 › turn_mock_cadence_feed · 纯 AI 光圈轮转、思考提示、逐条历史与自动滚动 (11.5s)
Testing stopped early after 1 maximum allowed failures.


  1) [chromium] › turn/turn.spec.ts:77:1 › turn_mock_cadence_feed · 纯 AI 光圈轮转、思考提示、逐条历史与自动滚动 ───────

    Error: expect(received).toEqual(expected) // deep equality

    - Expected  - 0
    + Received  + 1

      Set {
    +   "",
        "0",
        "1",
      }

      126 |     return { ui, actions, scroll }
      127 |   }, id)
    > 128 |   expect(new Set(observed.ui.map(sample => sample.actor))).toEqual(new Set(['0', '1']))
          |                                                            ^
      129 |   expect(observed.actions.length).toBeGreaterThanOrEqual(6)
      130 |   const gaps = observed.actions.slice(1).map((action, i) => action.atMs - observed.actions[i].atMs)
      131 |   // 首个 action 是启动观测时已存在的快照，其后的第一个间隔只是剩余窗口。
        at /Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/turn/turn.spec.ts:128:60

    Error Context: .turn-runtime-br1y4v7a/artifacts/turn-turn-turn-mock-cadence-feed-·-纯-AI-光圈轮转、思考提示、逐条历史与自动滚动-chromium/error-context.md

  1 failed
    [chromium] › turn/turn.spec.ts:77:1 › turn_mock_cadence_feed · 纯 AI 光圈轮转、思考提示、逐条历史与自动滚动 ────────
  2 did not run
  4 passed (46.5s)
  1 error was not a part of any test, see above for details
```

退出码：1

```text
playwright: passed=4 failed=1 not_run=2 exit=1
```

截图目录：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/turn-artifacts/20261003-212751`。

联调停止：`REAL_BROWSER_FAILED_STOP_IMPLEMENTATION_UNCHANGED`；不修改 Go/Web 实现。

```text
cleanup: own_server_stopped=True; 8081_released=True; Homebrew_3306_6379_retained=True
```

### 命令

```sh
go test -mod=readonly -race -count=1 ./...
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text
ok  	texas-holdem/cmd/config	4.181s
ok  	texas-holdem/cmd/server	3.824s
ok  	texas-holdem/internal/agent	4.384s
ok  	texas-holdem/internal/conf	3.707s
ok  	texas-holdem/internal/data	4.755s
ok  	texas-holdem/internal/poker	4.679s
ok  	texas-holdem/internal/server	4.567s
ok  	texas-holdem/internal/service	5.343s
?   	texas-holdem/web	[no test files]
```

退出码：0

```text
go-race: passed_packages=8 failed_packages=0 exit=0
```

## 场景 → 测试函数 → 结果证据

| 场景 | 测试函数 | 结果依据 |
|---|---|---|
| AI 光圈逐座轮转、思考提示、行动间隔 | turn_mock_cadence_feed | TURN_EVIDENCE mock-cadence |
| 历史逐条增加与自动滚到底部 | turn_mock_cadence_feed | ui_feed_additions + scroll + 截图 |
| 真人光圈、底部倒计时减少、合法按钮 | turn_human_countdown | TURN_EVIDENCE human-countdown |
| 停顿窗口 pause/stop 快速打断与稳定 | turn_interrupt | TURN_EVIDENCE interrupt |
| 既有真人真实回归 | human.spec.ts 的全部4个测试 | EVIDENCE + Playwright逐条结果 |

没有对应通过输出的场景不得认定通过。完整测试断言见 `tests/e2e/browser/turn/turn.spec.ts` 和 `tests/e2e/browser/human/human.spec.ts`。

未验证边界：**live 真实模型耗时节奏未联网验证**；未调用付费模型；仅桌面 Chromium，未验证移动端/其他浏览器、极慢网络/后台标签限流、动作多于60条时的历史截断、全押实际提交、跨标签竞争、服务重启和存储故障。AI 行动时间为浏览器观测时间，不是服务端数据库提交时钟。原 fixture 与旧观战场景未重跑，未改变其机制。

本轮结论：FAIL / 部分完成，见完整失败输出。

# 轮转 UI 真实端到端证据

运行时间：2026-10-03 21:28:55 +0800

复现命令（项目根目录）：`python3 tests/e2e/browser/run_turn.py`。
真实 Go 新构建二进制/嵌入 Web + Chromium + Homebrew MySQL3306/Redis6379。mock-only、allow_paid_models=false、真人超时 5s、AI 步进 800ms（显式短值，仅 mock 生效）。
不读取 .env.local；连接凭据仅由 local.py 连接助手/Go 在本地加载，受保护配置内容不回传。不读取用户模型目录，不打印 Cookie 凭据；Set-Cookie 回归仅检查属性布尔值。不启用 trace/HAR/video/storageState；截图只有渲染 UI，不含请求头。
本地无 Git 元数据，未 pull、commit、push，版本新旧未核验。

```text
preflight: MySQL3306=available Redis6379=available active_games=0 8081=free
```

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
✓ 42 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-BScmiY94.css   24.45 kB │ gzip:  6.62 kB
dist/assets/index-BcXB2nS0.js   262.62 kB │ gzip: 82.63 kB
✓ built in 685ms
```

退出码：0

### 命令

```sh
go build -mod=readonly -o /Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/.turn-runtime-1lrhf4x8/server ./cmd/server
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

```

退出码：0

## 启动方式

使用 `local.service_env(e2e=True, explicit=...)` 的等价 dev 环境。显式覆盖 POKER_HTTP_HOST=127.0.0.1、POKER_HTTP_PORT=8081、POKER_HUMAN_ACTION_TIMEOUT=5s、POKER_AI_TURN_DELAY=800ms；E2E 强制 POKER_ALLOW_PAID_MODELS=false 与 scripts/mock-catalog.json。直接运行本轮二进制：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/.turn-runtime-1lrhf4x8/server`，cwd 为项目根目录。服务原始日志丢弃，避免凭据出现在证据中。

```text
health: {"mysql": "available", "redis": "available", "storage_degraded": false, "allow_paid_models": false, "max_live_calls": 1000}; models=[mock]
```

### 命令

```sh
npx --no-install playwright test --config=playwright.turn.config.ts
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser`

```text

Running 7 tests using 1 worker

EVIDENCE a/b: {"create":201,"human_model_omitted":true,"ai_model_required":400,"cookie":{"name":"poker_player","issued":true,"httpOnly":true,"strict":true,"gamePath":true},"provider_calls":0}
EVIDENCE c/g: {"authenticated_player":200,"own_hole_count":2,"legal_actions":true,"anonymous_player":403,"anonymous_code":"PLAYER_NOT_AUTHORIZED","public_player":null,"public_private_keys":0,"unfinished_hand":409,"ai_open_holes":0,"anonymous_open_holes":0}
EVIDENCE h: {"page_reload":true,"recovered_player":200,"same_own_holes":true,"cookies_manually_added":0}
  ✓  1 [chromium] › human/human.spec.ts:123:1 › human_identity_privacy_refresh · a/b/c/d/g/h 真实创建、会话、私牌、匿名与刷新 (8.0s)
EVIDENCE e/errors: {"illegal":400,"illegal_code":"ILLEGAL_ACTION","unchanged_revision":true,"unchanged_deadline":true,"stale":409,"stale_code":"STALE_ACTION"}
EVIDENCE e/call: {"status":200,"request":{"expected_revision":4,"kind":"call"},"before_revision":4,"after_revision":5,"source":"human"}
EVIDENCE d: {"waiting_for":"human_action","fold":true,"call":true,"check":true,"raise":true,"all_in":true,"countdown_max_seconds":5,"buttons_follow_legal_actions":true}
EVIDENCE e/check: {"status":200,"request":{"expected_revision":10,"kind":"check"},"before_revision":10,"after_revision":11,"source":"human"}
EVIDENCE e/raise_to: {"status":200,"request":{"expected_revision":14,"kind":"raise_to","to":10},"before_revision":14,"after_revision":15,"source":"human"}
EVIDENCE e/fold: {"status":200,"request":{"expected_revision":20,"kind":"fold"},"before_revision":20,"after_revision":21,"source":"human"}
EVIDENCE g/completed: {"completed_hand":200,"revealed_seats":2,"revealed_cards_per_seat":2}
  ✓  2 [chromium] › human/human.spec.ts:194:1 › human_actions · d/e/g 真实 UI call/check/raise_to/fold、400、409 与完成揭牌 (10.6s)
EVIDENCE e/call: {"status":200,"request":{"expected_revision":4,"kind":"call"},"before_revision":4,"after_revision":5,"source":"human"}
EVIDENCE f: {"configured_timeout_seconds":5,"remaining_at_observation_ms":4948,"elapsed_ms":4280,"can_check_before_timeout":true,"action":{"hand_no":1,"sequence":4,"seat":0,"kind":"fold","to":0,"source":"fallback","error_code":"HUMAN_ACTION_TIMEOUT","input_tokens":null,"output_tokens":null},"browser_timeout_action_posts":0,"status":"completed","hands_completed":1,"before_revision":10,"after_revision":11,"completed_hand":200,"live_calls_used":0}
  ✓  3 [chromium] › human/human.spec.ts:244:1 › human_timeout · f 5 秒无人操作，即使可 check 也由服务端 fallback 弃牌并结算 (9.8s)
EVIDENCE h/running: {"status":"running","player_http":200,"same_own_holes":true,"same_deadline":true,"manually_added_cookies":0}
EVIDENCE f/next-hand: {"status":"running","next_hand_number":2,"hands_completed":1,"first_hand_persisted":200,"browser_action_posts":0,"live_calls_used":0,"public_player_in_next_hand":null}
  ✓  4 [chromium] › human/human.spec.ts:276:1 › human_running_refresh_next_hand · f/h 运行中刷新恢复、超时后继续下一手 (8.4s)
TURN_EVIDENCE mock-cadence: {"ai_delay_ms":800,"first_actor":"1","next_actor":"0","observed":{"ui":[{"atMs":0,"actor":"0","rows":2},{"atMs":671,"actor":"1","rows":3},{"atMs":1699,"actor":"0","rows":4},{"atMs":2732,"actor":"0","rows":5},{"atMs":3718,"actor":"","rows":6},{"atMs":4698,"actor":"0","rows":7},{"atMs":5735,"actor":"0","rows":8},{"atMs":6731,"actor":"0","rows":9},{"atMs":7715,"actor":"1","rows":10},{"atMs":8746,"actor":"0","rows":11}],"actions":[{"atMs":4,"hand":1,"sequence":3,"seat":1},{"atMs":209,"hand":1,"sequence":4,"seat":0},{"atMs":1032,"hand":1,"sequence":5,"seat":1},{"atMs":1854,"hand":1,"sequence":6,"seat":0},{"atMs":2681,"hand":1,"sequence":7,"seat":1},{"atMs":3304,"hand":1,"sequence":8,"seat":0},{"atMs":4131,"hand":2,"sequence":9,"seat":1},{"atMs":4959,"hand":2,"sequence":10,"seat":0},{"atMs":5788,"hand":2,"sequence":11,"seat":0},{"atMs":6628,"hand":2,"sequence":12,"seat":1},{"atMs":7457,"hand":2,"sequence":13,"seat":0},{"atMs":8282,"hand":2,"sequence":14,"seat":1}],"scroll":{"scrollTop":219,"scrollHeight":560,"clientHeight":340,"bottomGap":1}},"action_gap_ms":[205,823,822,827,623,827,828,829,840,829,825],"ui_feed_additions":[{"atMs":0,"actor":"0","rows":2},{"atMs":671,"actor":"1","rows":3},{"atMs":1699,"actor":"0","rows":4},{"atMs":2732,"actor":"0","rows":5},{"atMs":3718,"actor":"","rows":6},{"atMs":4698,"actor":"0","rows":7},{"atMs":5735,"actor":"0","rows":8},{"atMs":6731,"actor":"0","rows":9},{"atMs":7715,"actor":"1","rows":10},{"atMs":8746,"actor":"0","rows":11}],"screenshots":["/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/turn-artifacts/20261003-212900/ai-first-actor.png","/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/turn-artifacts/20261003-212900/ai-next-actor.png","/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/turn-artifacts/20261003-212900/ai-feed-scrolled.png"],"ui_forced_refreshes":0}
  ✓  5 [chromium] › turn/turn.spec.ts:77:1 › turn_mock_cadence_feed · 纯 AI 光圈轮转、思考提示、逐条历史与自动滚动 (11.9s)
TURN_EVIDENCE human-countdown: {"before":{"text":"5s","percent":100},"after":{"text":"4s","percent":80,"elapsedMs":621},"halo":{"shadow":"rgba(201, 164, 76, 0.518) 0px 0px 0px 1px, rgba(201, 164, 76, 0.51) 0px 0px 22.5289px -5.73553px, rgba(0, 0, 0, 0.9) 0px 16px 30px -18px","animation":"actor-pulse"},"call_http":200,"check_button_on_next_turn":true,"dock_in_viewport":true,"buttons":["弃牌","过牌","跟注","加注","全押"],"screenshots":["/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/turn-artifacts/20261003-212900/human-countdown-before.png","/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/turn-artifacts/20261003-212900/human-countdown-after.png"]}
  ✓  6 [chromium] › turn/turn.spec.ts:161:1 › turn_human_countdown · 真人光圈、底部按钮组、倒计时数字与进度条减少 (7.4s)
TURN_EVIDENCE interrupt: {"ai_delay_ms":800,"pause_response_ms":52,"stop_response_ms":44,"pause_action_sequence":3,"stop_action_sequence":4,"stable_after_wait_ms":1000,"paused_revision":10,"stopped_revision":14,"no_action_advanced_during_control":true}
  ✓  7 [chromium] › turn/turn.spec.ts:218:1 › turn_interrupt · AI 停顿窗口中 pause/stop 快速响应且状态不再推进 (4.3s)

  7 passed (1.0m)
```

退出码：0

```text
playwright: passed=7 failed=0 not_run=0 exit=0
```

截图目录：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/turn-artifacts/20261003-212900`。

```text
cleanup: own_server_stopped=True; 8081_released=True; Homebrew_3306_6379_retained=True
```

### 命令

```sh
go test -mod=readonly -race -count=1 ./...
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text
ok  	texas-holdem/cmd/config	4.112s
ok  	texas-holdem/cmd/server	4.123s
ok  	texas-holdem/internal/agent	4.514s
ok  	texas-holdem/internal/conf	4.027s
ok  	texas-holdem/internal/data	4.904s
ok  	texas-holdem/internal/poker	4.655s
ok  	texas-holdem/internal/server	4.579s
ok  	texas-holdem/internal/service	5.168s
?   	texas-holdem/web	[no test files]
```

退出码：0

```text
go-race: passed_packages=8 failed_packages=0 exit=0
```

## 场景 → 测试函数 → 结果证据

| 场景 | 测试函数 | 结果依据 |
|---|---|---|
| AI 光圈逐座轮转、思考提示、行动间隔 | turn_mock_cadence_feed | TURN_EVIDENCE mock-cadence |
| 历史逐条增加与自动滚到底部 | turn_mock_cadence_feed | ui_feed_additions + scroll + 截图 |
| 真人光圈、底部倒计时减少、合法按钮 | turn_human_countdown | TURN_EVIDENCE human-countdown |
| 停顿窗口 pause/stop 快速打断与稳定 | turn_interrupt | TURN_EVIDENCE interrupt |
| 既有真人真实回归 | human.spec.ts 的全部4个测试 | EVIDENCE + Playwright逐条结果 |

没有对应通过输出的场景不得认定通过。完整测试断言见 `tests/e2e/browser/turn/turn.spec.ts` 和 `tests/e2e/browser/human/human.spec.ts`。

未验证边界：**live 真实模型耗时节奏未联网验证**；未调用付费模型；仅桌面 Chromium，未验证移动端/其他浏览器、极慢网络/后台标签限流、动作多于60条时的历史截断、全押实际提交、跨标签竞争、服务重启和存储故障。AI 行动时间为浏览器观测时间，不是服务端数据库提交时钟。原 fixture 与旧观战场景未重跑，未改变其机制。

本轮结论：PASS / 3个轮转测试、4个真人回归、Go race均通过。
