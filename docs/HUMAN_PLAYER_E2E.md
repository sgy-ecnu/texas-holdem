# 真人上桌真实联调证据

## 最终验收摘要

最后一轮真实 Chromium 测试：**4 passed、0 failed、0 not_run，退出码 0**（31.1s）。
`go test -mod=readonly -race -count=1 ./...`：**8 个测试包通过、0 失败**，web 无测试文件，退出码 0。
本轮自启 Go 服务退出、8081 释放，Homebrew MySQL 3306 / Redis 6379 保留运行。
以下保留全部历史轮次的原始输出；历史轮次中的 FAIL 是当时的测试断言问题，不能替代最后一轮结果。

复现（项目根目录）：

```sh
python3 tests/e2e/browser/run_human.py
```

该入口重新构建 Web 与 Go，使用 `local.py.service_env(e2e=True)` 的等价 dev 启动机制，
强制 `POKER_HUMAN_ACTION_TIMEOUT=5s`、`POKER_ALLOW_PAID_MODELS=false` 与 mock-only 目录，
通过 `http://127.0.0.1:8081` 验证真实嵌入前端；最后执行指定的全量 Go race。
证据追加写入本文，不覆盖失败输出。数据库测试记录保留，仅停止本轮创建的活动游戏，不删库、不 flush Redis。

| 用户场景 | 测试函数（human.spec.ts） | 最后一轮实测结果 |
|---|---|---|
| a 1 真人 + 1 AI，模型要求 | human_identity_privacy_refresh | PASS：创建 201，真人省略 model_ref；AI 缺模型 400 |
| b 下发 HttpOnly Cookie、浏览器自动携带 | human_identity_privacy_refresh | PASS：poker_player 下发，HttpOnly/Strict/游戏级 Path 均 true；未手工注入 Cookie，本人接口 200 |
| c 本人两张底牌与合法动作、匿名 403 | human_identity_privacy_refresh | PASS：own_hole_count=2、legal_actions=true；独立匿名 context 返回 403 PLAYER_NOT_AUTHORIZED |
| d 行动面板、合法按钮、倒计时 | human_actions | PASS：waiting_for=human_action；弃牌、过牌、跟注、加注、全押在各自合法状态可用；倒计时 1–5s |
| e 动作、非法输入、过期 revision | human_actions | PASS：call/check/raise_to/fold 全部 200 且 revision 增加；非法 check 400 ILLEGAL_ACTION 且不改变 revision/deadline；旧 revision 409 STALE_ACTION |
| f 5 秒无人操作后服务端弃牌且继续 | human_timeout / human_running_refresh_next_hand | PASS：fold/source=fallback/error_code=HUMAN_ACTION_TIMEOUT，即使能 check 也弃牌；浏览器自动动作 POST=0；首手结算并持久化，多手局 status=running/hand=2/hands_completed=1 |
| g 公共私牌隔离、完成后揭牌 | human_identity_privacy_refresh / human_actions / human_running_refresh_next_hand | PASS：进行中 player=null、无 hole/deck/session 字段，匿名及 AI 座位未打开私牌；未完成手牌详情 409，完成后 200 并揭示两座各两张牌；下一手公共接口仍 player=null |
| h 刷新 Cookie 恢复 | human_identity_privacy_refresh / human_running_refresh_next_hand | PASS：暂停与运行中均刷新恢复真人视角，两张本人牌不变；运行中 deadline 不变；手工 Cookie 注入=0 |

### 测试内容摘要

测试文件：`tests/e2e/browser/human/human.spec.ts`。关键断言如下（完整内容见源文件）：

```ts
expect(attrs).toEqual({ issued: true, httpOnly: true, strict: true, gamePath: true })
expect(denied.status).toBe(403)
expect(denied.data.error.code).toBe('PLAYER_NOT_AUTHORIZED')
expect(view.player.hole_cards).toHaveLength(2)
expect(view.player.legal_actions).not.toBeNull()
expect(view.player).toBeNull() // assertPublic 中的公共 DTO 检查
expect(request).toMatchObject({ expected_revision: before.revision, kind })
expect(after.revision).toBeGreaterThan(before.revision)
expect(final.last_action).toMatchObject({
  seat: 0, kind: 'fold', source: 'fallback', error_code: 'HUMAN_ACTION_TIMEOUT',
  input_tokens: null, output_tokens: null,
})
expect(postedActions).toBe(0)
expect(next.status).toBe('running')
expect(next.hands_completed).toBe(1)
expect(recovered.data.player.action_deadline).toBe(first.player.action_deadline)
```

**两处新增测试断言修正，非 Go/Web 实现契约冲突：**

1. 进行中手牌详情原有语义是 `409 INVALID_STATE`（`internal/service/manager.go:416–420`、`internal/server/http.go:391–392`），不是测试首版写的 404。
2. `input_tokens` / `output_tokens` 是合法公共用量统计字段（`internal/server/dto.go:49–50`），不是会话凭据；测试第三轮的泛化 `token` 字段匹配过宽，第四轮仅修正测试匹配规则。

超时证据中的 `remaining_at_observation_ms` 取自等待点的服务端响应；`elapsed_ms` 从等待行动面板显示后才开始计时。
二者计时起点不同，不能直接相减认定提前超时。截止时间由服务端生成，本轮配置 5s；可 check 时仍由服务端提交超时弃牌，浏览器未发送该动作。

**未验证边界：** 全押按钮未实际点击；短全押/全押跟注、服务重启恢复、跨标签并发、Cookie 丢失/重复/伪造、Redis 缓存原文、存储故障注入、非 loopback 部署、live/付费模型、旧全 AI 浏览器全量回归。原 fixture 测试与原 run.py 未修改，本轮未重跑它们。

仅新增真实 E2E 测试/配置/入口与本文；未修改 Go、web/src、config.yaml 或 scripts/local.py 核心逻辑。Web dist 为重新构建的生产嵌入产物，无付费调用、commit 或 push。

## 按运行顺序保留的完整命令与原始输出

入口：`python3 tests/e2e/browser/run_human.py`。时间：2026-10-03 20:41:21 +0800

真实 Chromium → 同源 Go 嵌入 Web → Homebrew MySQL 3306 / Redis 6379。无 fixture/route mock；mock 模型目录、allow_paid_models=false、真人超时 5s。

不读取 .env.local，不输出 config.local.yaml 的内容或凭据；存储连接只由 local.py 连接助手/Go 进程在本地加载。不调用浏览器 Cookie/storageState API，不提取 Cookie 凭据值；Set-Cookie 只检查名称及 HttpOnly/Strict/Path 属性布尔值。禁用 trace/HAR/video。

当前目录无 Git 元数据，未 pull、commit、push；版本新旧未核验。

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
✓ 39 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.44 kB
dist/assets/index-C3qy8hek.css   21.17 kB │ gzip:  5.82 kB
dist/assets/index-CaKcuwmp.js   260.51 kB │ gzip: 81.94 kB
✓ built in 806ms
```

退出码：0

### 命令

```sh
go build -mod=readonly -o /Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/.human-runtime-78t6xtfp/server ./cmd/server
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

```

退出码：0

### 启动

等价于 local.py dev 的 `service_env(e2e=True, explicit=...)` 环境，直接运行本轮新构建二进制。显式非敏感覆盖：POKER_HTTP_HOST=127.0.0.1、POKER_HTTP_PORT=8081、POKER_HUMAN_ACTION_TIMEOUT=5s、POKER_CATALOG_PATH=scripts/mock-catalog.json、POKER_ALLOW_PAID_MODELS=false；POKER_CONFIG_FILE 为主 YAML 绝对路径。未使用 dev-live，不读取用户 opencode 配置。
服务原始日志不保存、不回传，避免凭据进入证据。

实测 health：`{"mysql": "available", "redis": "available", "storage_degraded": false, "allow_paid_models": false, "max_live_calls": 1000}`；models 仅 mock。

### 命令

```sh
npx --no-install playwright test --config=playwright.human.config.ts
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser`

```text

Running 3 tests using 1 worker

EVIDENCE a/b: {"create":201,"human_model_omitted":true,"ai_model_required":400,"cookie":{"name":"poker_player","issued":true,"httpOnly":true,"strict":true,"gamePath":true},"provider_calls":0}
  ✘  1 [chromium] › human/human.spec.ts:120:1 › human_identity_privacy_refresh · a/b/c/d/g/h 真实创建、会话、私牌、匿名与刷新 (3.6s)
Testing stopped early after 1 maximum allowed failures.


  1) [chromium] › human/human.spec.ts:120:1 › human_identity_privacy_refresh · a/b/c/d/g/h 真实创建、会话、私牌、匿名与刷新 

    Error: expect(received).toBe(expected) // Object.is equality

    Expected: 404
    Received: 409

      140 |   assertPublic(publicView.data)
      141 |   const unfinished = await http(page, `/api/v1/games/${id}/hands/1`)
    > 142 |   expect(unfinished.status).toBe(404)
          |                             ^
      143 |   const anonymous = await browser.newContext({ baseURL: process.env.POKER_BASE_URL })
      144 |   try {
      145 |     const spectator = await anonymous.newPage()
        at /Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/human/human.spec.ts:142:29

    Error Context: .human-runtime-78t6xtfp/artifacts/human-human-identity-priva-af2b4--b-c-d-g-h-真实创建、会话、私牌、匿名与刷新-chromium/error-context.md

  1 failed
    [chromium] › human/human.spec.ts:120:1 › human_identity_privacy_refresh · a/b/c/d/g/h 真实创建、会话、私牌、匿名与刷新 
  2 did not run
  1 error was not a part of any test, see above for details
(node:2199) Warning: The 'NO_COLOR' env is ignored due to the 'FORCE_COLOR' env being set.
(Use `node --trace-warnings ...` to show where the warning was created)
```

退出码：1

联调停止：`REAL_BROWSER_FAILED_STOP_IMPLEMENTATION_UNCHANGED`。未修改 Go/Web 实现。

```text
cleanup: own_server_stopped=True; 8081_released=True; Homebrew_3306_6379_retained=True
```

### 命令

```sh
go test -mod=readonly -race -count=1 ./...
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text
ok  	texas-holdem/cmd/config	1.272s
ok  	texas-holdem/cmd/server	1.487s
ok  	texas-holdem/internal/agent	1.515s
ok  	texas-holdem/internal/conf	1.794s
ok  	texas-holdem/internal/data	2.191s
ok  	texas-holdem/internal/poker	2.677s
ok  	texas-holdem/internal/server	1.968s
ok  	texas-holdem/internal/service	2.855s
?   	texas-holdem/web	[no test files]
```

退出码：0

```text
go-race: passed_packages=8 failed_packages=0 exit=0
```

## 场景与测试函数

| 场景 | 测试函数 | 结果来源 |
|---|---|---|
| a 创建真人+AI、模型校验 | human_identity_privacy_refresh | a/b EVIDENCE + Playwright |
| b Set-Cookie/HttpOnly、自动凭据 | human_identity_privacy_refresh | a/b、c/g EVIDENCE |
| c 本人两张牌/合法动作、匿名403 | human_identity_privacy_refresh | c/g EVIDENCE |
| d 行动面板与倒计时 | human_actions | d EVIDENCE |
| e call/check/raise_to/fold、400/409 | human_actions | e/* EVIDENCE |
| f 5秒服务端弃牌与继续结算 | human_timeout | f EVIDENCE |
| g 公共无私牌、完成后揭牌 | human_identity_privacy_refresh / human_actions | c/g、g/completed EVIDENCE |
| h 刷新 Cookie 恢复本人 | human_identity_privacy_refresh | h EVIDENCE |

测试源码：`tests/e2e/browser/human/human.spec.ts`（包含完整断言）。结果以以上未截断原始输出为准，缺少 EVIDENCE 的场景不得视为通过。

未验证边界：服务重启恢复、跨标签并发、Cookie 丢失/重复/伪造、非 loopback 部署、live/付费模型、短全押/全押跟注、Redis 缓存原文、故障注入、全 AI 旧浏览器全量回归。全押按钮在合法状态可用，但本轮未点击全押；刷新在冻结的真人等待点执行。超时验证一手上限后的继续推进及结算，未验证超时后下一手。

最终状态：FAIL / 部分完成，见上文失败证据。

## 首轮失败定位（测试断言问题，非实现契约冲突）

新增测试误把进行中手牌详情的状态写成 404；实测为 409。
`internal/service/manager.go:416–420` 明确区分：当前进行中手牌返回 `ErrConflict`，不存在的手牌返回 `ErrNotFound`；
`internal/server/http.go:391–392` 将 `ErrConflict` 映射为 `409 INVALID_STATE`。
权威真人设计契约只要求完成后才能揭牌，未规定未完成详情必须返回 404。
因此仅修正新增测试为 `409 INVALID_STATE`，没有修改 Go/Web 实现。下一轮证据追加如下，不覆盖失败信息。
# 真人上桌真实联调证据

入口：`python3 tests/e2e/browser/run_human.py`。时间：2026-10-03 20:42:04 +0800

真实 Chromium → 同源 Go 嵌入 Web → Homebrew MySQL 3306 / Redis 6379。无 fixture/route mock；mock 模型目录、allow_paid_models=false、真人超时 5s。

不读取 .env.local，不输出 config.local.yaml 的内容或凭据；存储连接只由 local.py 连接助手/Go 进程在本地加载。不调用浏览器 Cookie/storageState API，不提取 Cookie 凭据值；Set-Cookie 只检查名称及 HttpOnly/Strict/Path 属性布尔值。禁用 trace/HAR/video。

当前目录无 Git 元数据，未 pull、commit、push；版本新旧未核验。

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
✓ 39 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.44 kB
dist/assets/index-C3qy8hek.css   21.17 kB │ gzip:  5.82 kB
dist/assets/index-CaKcuwmp.js   260.51 kB │ gzip: 81.94 kB
✓ built in 732ms
```

退出码：0

### 命令

```sh
go build -mod=readonly -o /Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/.human-runtime-xibm6mdf/server ./cmd/server
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

```

退出码：0

### 启动

等价于 local.py dev 的 `service_env(e2e=True, explicit=...)` 环境，直接运行本轮新构建二进制。显式非敏感覆盖：POKER_HTTP_HOST=127.0.0.1、POKER_HTTP_PORT=8081、POKER_HUMAN_ACTION_TIMEOUT=5s、POKER_CATALOG_PATH=scripts/mock-catalog.json、POKER_ALLOW_PAID_MODELS=false；POKER_CONFIG_FILE 为主 YAML 绝对路径。未使用 dev-live，不读取用户 opencode 配置。
服务原始日志不保存、不回传，避免凭据进入证据。

实测 health：`{"mysql": "available", "redis": "available", "storage_degraded": false, "allow_paid_models": false, "max_live_calls": 1000}`；models 仅 mock。

### 命令

```sh
npx --no-install playwright test --config=playwright.human.config.ts
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser`

```text

Running 3 tests using 1 worker

EVIDENCE a/b: {"create":201,"human_model_omitted":true,"ai_model_required":400,"cookie":{"name":"poker_player","issued":true,"httpOnly":true,"strict":true,"gamePath":true},"provider_calls":0}
EVIDENCE c/g: {"authenticated_player":200,"own_hole_count":2,"legal_actions":true,"anonymous_player":403,"anonymous_code":"PLAYER_NOT_AUTHORIZED","public_player":null,"public_private_keys":0,"unfinished_hand":409,"ai_open_holes":0,"anonymous_open_holes":0}
EVIDENCE h: {"page_reload":true,"recovered_player":200,"same_own_holes":true,"cookies_manually_added":0}
  ✓  1 [chromium] › human/human.spec.ts:120:1 › human_identity_privacy_refresh · a/b/c/d/g/h 真实创建、会话、私牌、匿名与刷新 (8.0s)
EVIDENCE e/errors: {"illegal":400,"illegal_code":"ILLEGAL_ACTION","unchanged_revision":true,"unchanged_deadline":true,"stale":409,"stale_code":"STALE_ACTION"}
EVIDENCE e/call: {"status":200,"request":{"expected_revision":4,"kind":"call"},"before_revision":4,"after_revision":5,"source":"human"}
EVIDENCE d: {"waiting_for":"human_action","fold":true,"call":true,"check":true,"raise":true,"all_in":true,"countdown_max_seconds":5,"buttons_follow_legal_actions":true}
EVIDENCE e/check: {"status":200,"request":{"expected_revision":10,"kind":"check"},"before_revision":10,"after_revision":11,"source":"human"}
EVIDENCE e/raise_to: {"status":200,"request":{"expected_revision":14,"kind":"raise_to","to":10},"before_revision":14,"after_revision":15,"source":"human"}
EVIDENCE e/fold: {"status":200,"request":{"expected_revision":20,"kind":"fold"},"before_revision":20,"after_revision":21,"source":"human"}
EVIDENCE g/completed: {"completed_hand":200,"revealed_seats":2,"revealed_cards_per_seat":2}
  ✓  2 [chromium] › human/human.spec.ts:191:1 › human_actions · d/e/g 真实 UI call/check/raise_to/fold、400、409 与完成揭牌 (6.2s)
EVIDENCE e/call: {"status":200,"request":{"expected_revision":4,"kind":"call"},"before_revision":4,"after_revision":5,"source":"human"}
EVIDENCE f: {"configured_timeout_seconds":5,"remaining_at_observation_ms":4998,"elapsed_ms":3881,"can_check_before_timeout":true,"action":{"hand_no":1,"sequence":4,"seat":0,"kind":"fold","to":0,"source":"fallback","error_code":"HUMAN_ACTION_TIMEOUT","input_tokens":null,"output_tokens":null},"browser_timeout_action_posts":0,"status":"completed","hands_completed":1,"before_revision":10,"after_revision":11,"completed_hand":200,"live_calls_used":0}
  ✓  3 [chromium] › human/human.spec.ts:241:1 › human_timeout · f 5 秒无人操作，即使可 check 也由服务端 fallback 弃牌并结算 (8.3s)

  3 passed (23.1s)
```

退出码：0

```text
cleanup: own_server_stopped=True; 8081_released=True; Homebrew_3306_6379_retained=True
```

### 命令

```sh
go test -mod=readonly -race -count=1 ./...
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text
ok  	texas-holdem/cmd/config	1.165s
ok  	texas-holdem/cmd/server	1.223s
ok  	texas-holdem/internal/agent	1.606s
ok  	texas-holdem/internal/conf	1.542s
ok  	texas-holdem/internal/data	2.122s
ok  	texas-holdem/internal/poker	1.888s
ok  	texas-holdem/internal/server	1.808s
ok  	texas-holdem/internal/service	2.173s
?   	texas-holdem/web	[no test files]
```

退出码：0

```text
go-race: passed_packages=8 failed_packages=0 exit=0
```

## 场景与测试函数

| 场景 | 测试函数 | 结果来源 |
|---|---|---|
| a 创建真人+AI、模型校验 | human_identity_privacy_refresh | a/b EVIDENCE + Playwright |
| b Set-Cookie/HttpOnly、自动凭据 | human_identity_privacy_refresh | a/b、c/g EVIDENCE |
| c 本人两张牌/合法动作、匿名403 | human_identity_privacy_refresh | c/g EVIDENCE |
| d 行动面板与倒计时 | human_actions | d EVIDENCE |
| e call/check/raise_to/fold、400/409 | human_actions | e/* EVIDENCE |
| f 5秒服务端弃牌与继续结算 | human_timeout | f EVIDENCE |
| g 公共无私牌、完成后揭牌 | human_identity_privacy_refresh / human_actions | c/g、g/completed EVIDENCE |
| h 刷新 Cookie 恢复本人 | human_identity_privacy_refresh | h EVIDENCE |

测试源码：`tests/e2e/browser/human/human.spec.ts`（包含完整断言）。结果以以上未截断原始输出为准，缺少 EVIDENCE 的场景不得视为通过。

未验证边界：服务重启恢复、跨标签并发、Cookie 丢失/重复/伪造、非 loopback 部署、live/付费模型、短全押/全押跟注、Redis 缓存原文、故障注入、全 AI 旧浏览器全量回归。全押按钮在合法状态可用，但本轮未点击全押；刷新在冻结的真人等待点执行。超时验证一手上限后的继续推进及结算，未验证超时后下一手。

最终状态：PASS / 本轮真实链路与 Go race 均通过。
# 真人上桌真实联调证据

入口：`python3 tests/e2e/browser/run_human.py`。时间：2026-10-03 20:43:33 +0800

真实 Chromium → 同源 Go 嵌入 Web → Homebrew MySQL 3306 / Redis 6379。无 fixture/route mock；mock 模型目录、allow_paid_models=false、真人超时 5s。

不读取 .env.local，不输出 config.local.yaml 的内容或凭据；存储连接只由 local.py 连接助手/Go 进程在本地加载。不调用浏览器 Cookie/storageState API，不提取 Cookie 凭据值；Set-Cookie 只检查名称及 HttpOnly/Strict/Path 属性布尔值。禁用 trace/HAR/video。

当前目录无 Git 元数据，未 pull、commit、push；版本新旧未核验。

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
✓ 39 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.44 kB
dist/assets/index-C3qy8hek.css   21.17 kB │ gzip:  5.82 kB
dist/assets/index-CaKcuwmp.js   260.51 kB │ gzip: 81.94 kB
✓ built in 670ms
```

退出码：0

### 命令

```sh
go build -mod=readonly -o /Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/.human-runtime-7a79c3hw/server ./cmd/server
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

```

退出码：0

### 启动

等价于 local.py dev 的 `service_env(e2e=True, explicit=...)` 环境，直接运行本轮新构建二进制。显式非敏感覆盖：POKER_HTTP_HOST=127.0.0.1、POKER_HTTP_PORT=8081、POKER_HUMAN_ACTION_TIMEOUT=5s、POKER_CATALOG_PATH=scripts/mock-catalog.json、POKER_ALLOW_PAID_MODELS=false；POKER_CONFIG_FILE 为主 YAML 绝对路径。未使用 dev-live，不读取用户 opencode 配置。
服务原始日志不保存、不回传，避免凭据进入证据。

实测 health：`{"mysql": "available", "redis": "available", "storage_degraded": false, "allow_paid_models": false, "max_live_calls": 1000}`；models 仅 mock。

### 命令

```sh
npx --no-install playwright test --config=playwright.human.config.ts
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser`

```text

Running 4 tests using 1 worker

EVIDENCE a/b: {"create":201,"human_model_omitted":true,"ai_model_required":400,"cookie":{"name":"poker_player","issued":true,"httpOnly":true,"strict":true,"gamePath":true},"provider_calls":0}
EVIDENCE c/g: {"authenticated_player":200,"own_hole_count":2,"legal_actions":true,"anonymous_player":403,"anonymous_code":"PLAYER_NOT_AUTHORIZED","public_player":null,"public_private_keys":0,"unfinished_hand":409,"ai_open_holes":0,"anonymous_open_holes":0}
EVIDENCE h: {"page_reload":true,"recovered_player":200,"same_own_holes":true,"cookies_manually_added":0}
  ✓  1 [chromium] › human/human.spec.ts:120:1 › human_identity_privacy_refresh · a/b/c/d/g/h 真实创建、会话、私牌、匿名与刷新 (7.8s)
EVIDENCE e/errors: {"illegal":400,"illegal_code":"ILLEGAL_ACTION","unchanged_revision":true,"unchanged_deadline":true,"stale":409,"stale_code":"STALE_ACTION"}
EVIDENCE e/call: {"status":200,"request":{"expected_revision":4,"kind":"call"},"before_revision":4,"after_revision":5,"source":"human"}
EVIDENCE d: {"waiting_for":"human_action","fold":true,"call":true,"check":true,"raise":true,"all_in":true,"countdown_max_seconds":5,"buttons_follow_legal_actions":true}
EVIDENCE e/check: {"status":200,"request":{"expected_revision":10,"kind":"check"},"before_revision":10,"after_revision":11,"source":"human"}
EVIDENCE e/raise_to: {"status":200,"request":{"expected_revision":14,"kind":"raise_to","to":10},"before_revision":14,"after_revision":15,"source":"human"}
EVIDENCE e/fold: {"status":200,"request":{"expected_revision":20,"kind":"fold"},"before_revision":20,"after_revision":21,"source":"human"}
EVIDENCE g/completed: {"completed_hand":200,"revealed_seats":2,"revealed_cards_per_seat":2}
  ✓  2 [chromium] › human/human.spec.ts:191:1 › human_actions · d/e/g 真实 UI call/check/raise_to/fold、400、409 与完成揭牌 (6.2s)
EVIDENCE e/call: {"status":200,"request":{"expected_revision":4,"kind":"call"},"before_revision":4,"after_revision":5,"source":"human"}
EVIDENCE f: {"configured_timeout_seconds":5,"remaining_at_observation_ms":4994,"elapsed_ms":4283,"can_check_before_timeout":true,"action":{"hand_no":1,"sequence":4,"seat":0,"kind":"fold","to":0,"source":"fallback","error_code":"HUMAN_ACTION_TIMEOUT","input_tokens":null,"output_tokens":null},"browser_timeout_action_posts":0,"status":"completed","hands_completed":1,"before_revision":10,"after_revision":11,"completed_hand":200,"live_calls_used":0}
  ✓  3 [chromium] › human/human.spec.ts:241:1 › human_timeout · f 5 秒无人操作，即使可 check 也由服务端 fallback 弃牌并结算 (8.5s)
EVIDENCE h/running: {"status":"running","player_http":200,"same_own_holes":true,"same_deadline":true,"manually_added_cookies":0}
  ✘  4 [chromium] › human/human.spec.ts:273:1 › human_running_refresh_next_hand · f/h 运行中刷新恢复、超时后继续下一手 (8.3s)
Testing stopped early after 1 maximum allowed failures.


  1) [chromium] › human/human.spec.ts:273:1 › human_running_refresh_next_hand · f/h 运行中刷新恢复、超时后继续下一手 

    Error: expect(received).toBe(expected) // Object.is equality

    Expected: false
    Received: true

       99 |     else if (value && typeof value === 'object') {
      100 |       for (const [key, child] of Object.entries(value)) {
    > 101 |         expect(/hole|deck|snapshot|session|token|password|secret/i.test(key)).toBe(false)
          |                                                                               ^
      102 |         inspect(child)
      103 |       }
      104 |     }
        at inspect (/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/human/human.spec.ts:101:79)
        at inspect (/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/human/human.spec.ts:102:9)
        at assertPublic (/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/human/human.spec.ts:106:3)
        at /Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/human/human.spec.ts:302:3

    Error Context: .human-runtime-7a79c3hw/artifacts/human-human-running-refresh-next-hand-·-f-h-运行中刷新恢复、超时后继续下一手-chromium/error-context.md

  1 failed
    [chromium] › human/human.spec.ts:273:1 › human_running_refresh_next_hand · f/h 运行中刷新恢复、超时后继续下一手 
  3 passed (31.3s)
  1 error was not a part of any test, see above for details
```

退出码：1

```text
playwright: passed=3 failed=1 not_run=0 exit=1
```

联调停止：`REAL_BROWSER_FAILED_STOP_IMPLEMENTATION_UNCHANGED`。未修改 Go/Web 实现。

```text
cleanup: own_server_stopped=True; 8081_released=True; Homebrew_3306_6379_retained=True
```

### 命令

```sh
go test -mod=readonly -race -count=1 ./...
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text
ok  	texas-holdem/cmd/config	1.166s
ok  	texas-holdem/cmd/server	1.345s
ok  	texas-holdem/internal/agent	1.753s
ok  	texas-holdem/internal/conf	1.220s
ok  	texas-holdem/internal/data	1.719s
ok  	texas-holdem/internal/poker	2.125s
ok  	texas-holdem/internal/server	2.030s
ok  	texas-holdem/internal/service	2.406s
?   	texas-holdem/web	[no test files]
```

退出码：0

```text
go-race: passed_packages=8 failed_packages=0 exit=0
```

## 场景与测试函数

| 场景 | 测试函数 | 结果来源 |
|---|---|---|
| a 创建真人+AI、模型校验 | human_identity_privacy_refresh | a/b EVIDENCE + Playwright |
| b Set-Cookie/HttpOnly、自动凭据 | human_identity_privacy_refresh | a/b、c/g EVIDENCE |
| c 本人两张牌/合法动作、匿名403 | human_identity_privacy_refresh | c/g EVIDENCE |
| d 行动面板与倒计时 | human_actions | d EVIDENCE |
| e call/check/raise_to/fold、400/409 | human_actions | e/* EVIDENCE |
| f 5秒服务端弃牌、结算、下一手 | human_timeout / human_running_refresh_next_hand | f、f/next-hand EVIDENCE |
| g 公共无私牌、完成后揭牌 | human_identity_privacy_refresh / human_actions | c/g、g/completed EVIDENCE |
| h 暂停/运行中刷新 Cookie 恢复本人 | human_identity_privacy_refresh / human_running_refresh_next_hand | h、h/running EVIDENCE |

测试源码：`tests/e2e/browser/human/human.spec.ts`（包含完整断言）。结果以以上未截断原始输出为准，缺少 EVIDENCE 的场景不得视为通过。

未验证边界：服务重启恢复、跨标签并发、Cookie 丢失/重复/伪造、非 loopback 部署、live/付费模型、短全押/全押跟注、Redis 缓存原文、故障注入、全 AI 旧浏览器全量回归。全押按钮在合法状态可用，但本轮未点击全押。刷新同时验证暂停冻结点与运行中等待点；超时验证单手结算与多手牌局进入下一手。

最终状态：FAIL / 部分完成，见上文失败证据。
# 真人上桌真实联调证据

入口：`python3 tests/e2e/browser/run_human.py`。时间：2026-10-03 20:44:31 +0800

真实 Chromium → 同源 Go 嵌入 Web → Homebrew MySQL 3306 / Redis 6379。无 fixture/route mock；mock 模型目录、allow_paid_models=false、真人超时 5s。

不读取 .env.local，不输出 config.local.yaml 的内容或凭据；存储连接只由 local.py 连接助手/Go 进程在本地加载。不调用浏览器 Cookie/storageState API，不提取 Cookie 凭据值；Set-Cookie 只检查名称及 HttpOnly/Strict/Path 属性布尔值。禁用 trace/HAR/video。

当前目录无 Git 元数据，未 pull、commit、push；版本新旧未核验。

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
✓ 39 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.44 kB
dist/assets/index-C3qy8hek.css   21.17 kB │ gzip:  5.82 kB
dist/assets/index-CaKcuwmp.js   260.51 kB │ gzip: 81.94 kB
✓ built in 774ms
```

退出码：0

### 命令

```sh
go build -mod=readonly -o /Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/.human-runtime-zv8wloqr/server ./cmd/server
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

```

退出码：0

### 启动

等价于 local.py dev 的 `service_env(e2e=True, explicit=...)` 环境，直接运行本轮新构建二进制。显式非敏感覆盖：POKER_HTTP_HOST=127.0.0.1、POKER_HTTP_PORT=8081、POKER_HUMAN_ACTION_TIMEOUT=5s、POKER_CATALOG_PATH=scripts/mock-catalog.json、POKER_ALLOW_PAID_MODELS=false；POKER_CONFIG_FILE 为主 YAML 绝对路径。未使用 dev-live，不读取用户 opencode 配置。
服务原始日志不保存、不回传，避免凭据进入证据。

实测 health：`{"mysql": "available", "redis": "available", "storage_degraded": false, "allow_paid_models": false, "max_live_calls": 1000}`；models 仅 mock。

### 命令

```sh
npx --no-install playwright test --config=playwright.human.config.ts
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser`

```text

Running 4 tests using 1 worker

EVIDENCE a/b: {"create":201,"human_model_omitted":true,"ai_model_required":400,"cookie":{"name":"poker_player","issued":true,"httpOnly":true,"strict":true,"gamePath":true},"provider_calls":0}
EVIDENCE c/g: {"authenticated_player":200,"own_hole_count":2,"legal_actions":true,"anonymous_player":403,"anonymous_code":"PLAYER_NOT_AUTHORIZED","public_player":null,"public_private_keys":0,"unfinished_hand":409,"ai_open_holes":0,"anonymous_open_holes":0}
EVIDENCE h: {"page_reload":true,"recovered_player":200,"same_own_holes":true,"cookies_manually_added":0}
  ✓  1 [chromium] › human/human.spec.ts:122:1 › human_identity_privacy_refresh · a/b/c/d/g/h 真实创建、会话、私牌、匿名与刷新 (7.8s)
EVIDENCE e/errors: {"illegal":400,"illegal_code":"ILLEGAL_ACTION","unchanged_revision":true,"unchanged_deadline":true,"stale":409,"stale_code":"STALE_ACTION"}
EVIDENCE e/call: {"status":200,"request":{"expected_revision":4,"kind":"call"},"before_revision":4,"after_revision":5,"source":"human"}
EVIDENCE d: {"waiting_for":"human_action","fold":true,"call":true,"check":true,"raise":true,"all_in":true,"countdown_max_seconds":5,"buttons_follow_legal_actions":true}
EVIDENCE e/check: {"status":200,"request":{"expected_revision":10,"kind":"check"},"before_revision":10,"after_revision":11,"source":"human"}
EVIDENCE e/raise_to: {"status":200,"request":{"expected_revision":14,"kind":"raise_to","to":10},"before_revision":14,"after_revision":15,"source":"human"}
EVIDENCE e/fold: {"status":200,"request":{"expected_revision":20,"kind":"fold"},"before_revision":20,"after_revision":21,"source":"human"}
EVIDENCE g/completed: {"completed_hand":200,"revealed_seats":2,"revealed_cards_per_seat":2}
  ✓  2 [chromium] › human/human.spec.ts:193:1 › human_actions · d/e/g 真实 UI call/check/raise_to/fold、400、409 与完成揭牌 (6.2s)
EVIDENCE e/call: {"status":200,"request":{"expected_revision":4,"kind":"call"},"before_revision":4,"after_revision":5,"source":"human"}
EVIDENCE f: {"configured_timeout_seconds":5,"remaining_at_observation_ms":4996,"elapsed_ms":3840,"can_check_before_timeout":true,"action":{"hand_no":1,"sequence":4,"seat":0,"kind":"fold","to":0,"source":"fallback","error_code":"HUMAN_ACTION_TIMEOUT","input_tokens":null,"output_tokens":null},"browser_timeout_action_posts":0,"status":"completed","hands_completed":1,"before_revision":10,"after_revision":11,"completed_hand":200,"live_calls_used":0}
  ✓  3 [chromium] › human/human.spec.ts:243:1 › human_timeout · f 5 秒无人操作，即使可 check 也由服务端 fallback 弃牌并结算 (8.2s)
EVIDENCE h/running: {"status":"running","player_http":200,"same_own_holes":true,"same_deadline":true,"manually_added_cookies":0}
EVIDENCE f/next-hand: {"status":"running","next_hand_number":2,"hands_completed":1,"first_hand_persisted":200,"browser_action_posts":0,"live_calls_used":0,"public_player_in_next_hand":null}
  ✓  4 [chromium] › human/human.spec.ts:275:1 › human_running_refresh_next_hand · f/h 运行中刷新恢复、超时后继续下一手 (8.2s)

  4 passed (31.1s)
```

退出码：0

```text
playwright: passed=4 failed=0 not_run=0 exit=0
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
ok  	texas-holdem/cmd/config	4.164s
ok  	texas-holdem/cmd/server	4.346s
ok  	texas-holdem/internal/agent	4.407s
ok  	texas-holdem/internal/conf	4.086s
ok  	texas-holdem/internal/data	4.877s
ok  	texas-holdem/internal/poker	4.753s
ok  	texas-holdem/internal/server	4.673s
ok  	texas-holdem/internal/service	5.250s
?   	texas-holdem/web	[no test files]
```

退出码：0

```text
go-race: passed_packages=8 failed_packages=0 exit=0
```

## 场景与测试函数

| 场景 | 测试函数 | 结果来源 |
|---|---|---|
| a 创建真人+AI、模型校验 | human_identity_privacy_refresh | a/b EVIDENCE + Playwright |
| b Set-Cookie/HttpOnly、自动凭据 | human_identity_privacy_refresh | a/b、c/g EVIDENCE |
| c 本人两张牌/合法动作、匿名403 | human_identity_privacy_refresh | c/g EVIDENCE |
| d 行动面板与倒计时 | human_actions | d EVIDENCE |
| e call/check/raise_to/fold、400/409 | human_actions | e/* EVIDENCE |
| f 5秒服务端弃牌、结算、下一手 | human_timeout / human_running_refresh_next_hand | f、f/next-hand EVIDENCE |
| g 公共无私牌、完成后揭牌 | human_identity_privacy_refresh / human_actions | c/g、g/completed EVIDENCE |
| h 暂停/运行中刷新 Cookie 恢复本人 | human_identity_privacy_refresh / human_running_refresh_next_hand | h、h/running EVIDENCE |

测试源码：`tests/e2e/browser/human/human.spec.ts`（包含完整断言）。结果以以上未截断原始输出为准，缺少 EVIDENCE 的场景不得视为通过。

未验证边界：服务重启恢复、跨标签并发、Cookie 丢失/重复/伪造、非 loopback 部署、live/付费模型、短全押/全押跟注、Redis 缓存原文、故障注入、全 AI 旧浏览器全量回归。全押按钮在合法状态可用，但本轮未点击全押。刷新同时验证暂停冻结点与运行中等待点；超时验证单手结算与多手牌局进入下一手。

最终状态：PASS / 本轮真实链路与 Go race 均通过。
