
# ActionPopover 真实链路验收证据

入口：`python3 tests/e2e/browser/run_action_popover.py`。时间：2026-10-04 09:54:56 +0800

真实 Chromium → 同源 Go 嵌入 Web → Homebrew MySQL 3306 / Redis 6379。无 fixture/route mock；mock 模型目录、allow_paid_models=false。

启动方式等价于 `make run`：先 `npm --prefix web run build` 生成最新 web/dist，再 `go build ./cmd/server` 产出含 amount 字段的最新二进制，随后前台运行该二进制。显式非敏感覆盖：POKER_HTTP_HOST=127.0.0.1、POKER_HTTP_PORT=8081、POKER_HUMAN_ACTION_TIMEOUT=5s、POKER_CATALOG_PATH=scripts/mock-catalog.json、POKER_ALLOW_PAID_MODELS=false；AI 步进沿用默认 1.5s。不读取 .env.local / config.local.yaml 的值，不提取 Cookie 凭据；存储连接只由 local.py 连接助手/Go 进程在本地加载。禁用 trace/HAR/video。


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
✓ 44 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-ZWGcZmHF.css   29.82 kB │ gzip:  7.58 kB
dist/assets/index-DlS0Rw-R.js   269.68 kB │ gzip: 85.11 kB
✓ built in 668ms
```

退出码：0

### 命令

```sh
go build -mod=readonly -o /Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/.ap-runtime-48f4xhga/server ./cmd/server
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
npx --no-install playwright test --config=playwright.actionpopover.config.ts
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser`

```text

Running 2 tests using 1 worker

EVIDENCE api/all_ai: {"observed_actions":9,"kinds":["call","check"],"calls":[{"hand":1,"seq":1,"to":0,"amount":5},{"hand":2,"seq":9,"to":0,"amount":5}],"checks_count":7,"reconstructed":[{"hand":1,"seq":1,"seat":0,"kind":"call","to":0,"amount":5,"expected":5},{"hand":1,"seq":2,"seat":1,"kind":"check","to":0,"amount":0,"expected":0},{"hand":1,"seq":3,"seat":1,"kind":"check","to":0,"amount":0,"expected":0},{"hand":1,"seq":4,"seat":0,"kind":"check","to":0,"amount":0,"expected":0},{"hand":1,"seq":5,"seat":1,"kind":"check","to":0,"amount":0,"expected":0},{"hand":1,"seq":6,"seat":0,"kind":"check","to":0,"amount":0,"expected":0},{"hand":1,"seq":7,"seat":1,"kind":"check","to":0,"amount":0,"expected":0},{"hand":1,"seq":8,"seat":0,"kind":"check","to":0,"amount":0,"expected":0}]}
EVIDENCE browser/all_ai: {"adds":9,"texts":["过牌","跟注 5"],"call_texts":["跟注 5"],"add_remove_durations_ms":[1002.4000000953674,1601.6999998092651,1601.5999999046326,1602,1602.0999999046326,1601.0999999046326,1005.6999998092651,1601.6999998092651,1601.6999998092651],"dedup_adds_before_pause":9,"dedup_adds_after_9s":9}
  ✓  1 [chromium] › action-popover/action_popover.spec.ts:239:1 › action_popover_all_ai · amount 真实下发 + 标签弹出/约1.6s消失/去重 (27.3s)
EVIDENCE browser/human: {"actions":[{"kind":"call","to":0,"amount":5,"expected":5,"text":"跟注 5"},{"kind":"check","to":0,"amount":0,"expected":0,"text":"过牌"},{"kind":"raise_to","to":10,"amount":10,"expected":10,"text":"加注到 10"},{"kind":"fold","to":0,"amount":0,"expected":0,"text":"弃牌"}],"seat1_popover_count":4,"seat1_texts":["跟注 5","过牌","加注到 10","弃牌"],"seat1_durations_ms":[1600.6999998092651,1600.7000002861023,1600.5,1600.8000001907349]}
  ✓  2 [chromium] › action-popover/action_popover.spec.ts:364:1 › action_popover_human · 真人四动作文案与金额 + 去重 (14.5s)

  2 passed (43.1s)
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
ok  	texas-holdem/cmd/config	4.189s
ok  	texas-holdem/cmd/server	4.237s
ok  	texas-holdem/internal/agent	4.290s
ok  	texas-holdem/internal/conf	4.465s
ok  	texas-holdem/internal/data	4.599s
ok  	texas-holdem/internal/poker	4.988s
ok  	texas-holdem/internal/server	4.899s
ok  	texas-holdem/internal/service	5.511s
?   	texas-holdem/web	[no test files]
```

退出码：0

```text
go-race: passed_packages=8 failed_packages=0 exit=0
```

## 场景与测试函数

| 场景 | 测试函数 | 结果来源 |
|---|---|---|
| 2a 全 AI mock API amount 语义 | action_popover_all_ai | api/all_ai EVIDENCE |
| 2b 全 AI 座位旁标签弹出/1.6s/去重 | action_popover_all_ai | browser/all_ai EVIDENCE |
| 3 真人 call/check/raise_to/fold 文案与金额 | action_popover_human | browser/human EVIDENCE |

测试源码：`tests/e2e/browser/action-popover/action_popover.spec.ts`。结果以未截断原始输出为准。

未验证边界：全 AI mock 决策器只会 check/call（`internal/agent/mock.go`），故 raise_to/fold 的 amount 只由真人场景覆盖；未覆盖多真人、跨标签并发、live/付费模型、全押按钮、非 loopback 部署、故障注入。

最终状态：PASS / 本轮真实链路与 Go race 均通过。
