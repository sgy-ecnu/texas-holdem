# BUGFIX_GO_LIFECYCLE

修复范围：仅 `internal/service/**`（含 `*_test.go`）与 `internal/data/**`（错误分类）。
未改动 `poker/conf/agent/server/web/scripts/go.mod/config.yaml`；未启动真实 HTTP/MySQL/Redis，未付费，未 commit/push。
凭据安全：未读取 `.env.local` / `config.local.yaml` 的值、未读取用户 opencode 配置；诊断只写固定安全码、阶段与 uuid 级 gameID，绝不携带 provider 原始错误或凭据。

## 一、修复点

### Bug 3：关付费开关后旧 live 局仍可 start/resume，触发全局 fault

- `internal/service/manager.go:420-424`：`Control` 在通过状态校验后、执行状态变更前，重新校验运行授权。live 局且 `!Options.AllowPaidModels` 时直接返回 `ErrForbidden`，不改状态、不锁存 fault。
- `internal/service/worker.go:122-135`：live 分支把「授权关闭」与「factory 缺失」拆开。授权关闭时记录 `BG_AUTH_DENIED/authorization` 并把局置为 `paused`（reason `unauthorized`），不再调用 `fail()`；仅 `factory == nil` 这类真实构造缺陷才 `failWith(BG_MODEL_FAILED)`。
- 保留 paused 语义：恢复开关后可正常 resume（`unauthorized` 不属于 `cost_limit` 拒绝项）。

### Bug 4：后台错误静默且统一伪装成存储故障

- `internal/service/manager.go:74-141`：新增 `diag func(code, stage, gameID)`、固定安全码常量、`reportDiag` / `failWith` / `txnStage` / `safeIdent` / `defaultDiag`。`failWith` 在同一故障锁存周期内只记录一次；默认落到 stderr 固定格式 `POKER_BACKGROUND_FAILURE code=... stage=... game=...`。
- `internal/service/manager.go:143-152`：`save` 提交失败按 `txnStage(err)` 记录事务阶段（`txn` / `instance_lock`）。
- `internal/service/manager.go:263-269`：`Create` 落库失败记录事务阶段。
- `internal/service/worker.go`：随机源、引擎构造/观察、快照编解码、模型构造、uuid、提交/读取等后台边界分别记录 `BG_RANDOM_FAILED/random`、`BG_ENGINE_FAILED/engine`、`BG_SNAPSHOT_FAILED/snapshot`、`BG_MODEL_FAILED/model`、`BG_TXN_FAILED/...`、`BG_STORAGE_FAILED`。
- `internal/service/contracts.go:115-118`：新增 `ErrInstanceLock`；与 `ErrStorage` 共同包装，`errors.Is(err, ErrStorage)` 仍成立，fail-closed 语义不变。
- `internal/data/repository.go:218,221`：`guard` 实例锁失效错误改为 `fmt.Errorf("%w: %w", service.ErrStorage, service.ErrInstanceLock)`，使 service 层可区分「实例锁」与普通事务失败。
- 诊断不含 provider 原始错误：`modelErrorCode` 现有 allowlist 逻辑未变，`safeIdent` 还会清洗 gameID。

### Bug 6：终态 live 局 decider 永久驻留（内存泄漏）

- `internal/service/manager.go:143-148`：`save` 在提交成功后，若记录进入终态（`stopped/completed/failed`）则 `delete(e.deciders, r.View.ID)`。所有终态转换（worker 结算完成、Control stop）均经由 `save`，无需新增资源管理层。
- 结合现有 request identity：非终态提交不清理；终态后 worker `step` 因状态非 running 不再调度，`decide` 亦校验 `activeRequest==id` 与状态/修订号，故删除后的对象只会被 GC 回收，不会复用。

## 二、新增测试函数（命名体现行为）

| 文件 | 测试函数 | 覆盖行为 |
| --- | --- | --- |
| `internal/service/lifecycle_bugfix_test.go` | `TestLiveAuthorizationRecheckedAtRuntime` | start/resume 未授权返回 forbidden；运行中撤权暂停且不锁存 fault |
| `internal/service/lifecycle_bugfix_test.go` | `TestTerminalGameReleasesDeciders` | stop/completed 终态后释放 gameID 的 decider |
| `internal/service/diagnostics_bugfix_test.go` | `TestBackgroundFailureDiagnostics` | 快照/模型构造/实例锁事务诊断码；授权拒绝不算 fault |

## 三、场景 → 测试 → 结果

| 场景 | 测试（子用例） | 结果 |
| --- | --- | --- |
| 关开关后对 live created 局 start → forbidden，状态不变、不锁存 fault | `TestLiveAuthorizationRecheckedAtRuntime/start_forbidden` | PASS |
| 关开关后对 live paused 局 resume → forbidden，保持 paused、不锁存 fault | `TestLiveAuthorizationRecheckedAtRuntime/resume_forbidden` | PASS |
| 运行中撤权：worker 暂停为 `unauthorized`，`fault=false`，Health 正常 | `TestLiveAuthorizationRecheckedAtRuntime/running_worker_pauses_without_fault` | PASS |
| stop 终态后释放 decider | `TestTerminalGameReleasesDeciders/stop_releases` | PASS |
| completed 终态后释放 decider | `TestTerminalGameReleasesDeciders/completion_releases` | PASS |
| 坏快照后台失败 → `BG_SNAPSHOT_FAILED/snapshot/seed` 且仍 fail-closed | `TestBackgroundFailureDiagnostics/snapshot_decode` | PASS |
| 模型构造失败 → `BG_MODEL_FAILED/model/seed` 且仍 fail-closed | `TestBackgroundFailureDiagnostics/model_construction` | PASS |
| 实例锁事务失败 → `BG_TXN_FAILED/instance_lock/seed` 且仍 fail-closed | `TestBackgroundFailureDiagnostics/instance_lock_txn` | PASS |
| 授权拒绝 → `BG_AUTH_DENIED/authorization/seed` 且不锁存 fault | `TestBackgroundFailureDiagnostics/auth_denied_is_not_fault` | PASS |

## 四、RED 原始输出（修复前）

命令：

```text
$ go test -mod=readonly -race -count=1 ./internal/service/ -run 'TestLiveAuthorizationRecheckedAtRuntime|TestTerminalGameReleasesDeciders'
```

输出（bug 3、bug 6 测试在旧代码上失败）：

```text
--- FAIL: TestLiveAuthorizationRecheckedAtRuntime (0.00s)
    --- FAIL: TestLiveAuthorizationRecheckedAtRuntime/start_forbidden (0.00s)
        lifecycle_bugfix_test.go:28: start got <nil> want forbidden
    --- FAIL: TestLiveAuthorizationRecheckedAtRuntime/resume_forbidden (0.00s)
        lifecycle_bugfix_test.go:44: resume got <nil> want forbidden
    --- FAIL: TestLiveAuthorizationRecheckedAtRuntime/running_worker_pauses_without_fault (0.00s)
        lifecycle_bugfix_test.go:67: revoked live game not paused: {ID:seed Revision:1 Status:running Reason:<nil> Mode:live MaxHands:3 HandsCompleted:0 LiveCallsUsed:0 MaxLiveCalls:1 StorageDegraded:false Seats:[{Seat:0 Name: ModelRef: Stack:1000} {Seat:1 Name: ModelRef: Stack:1000}] Hand:0xc0001b6230 LastAction:<nil>}
    --- FAIL: TestTerminalGameReleasesDeciders (0.03s)
        --- FAIL: TestTerminalGameReleasesDeciders/stop_releases (0.00s)
            lifecycle_bugfix_test.go:106: decider retained after stop
        --- FAIL: TestTerminalGameReleasesDeciders/completion_releases (0.03s)
            lifecycle_bugfix_test.go:132: decider retained after completion
FAIL
FAIL	texas-holdem/internal/service	0.277s
```

bug 4 的 `TestBackgroundFailureDiagnostics` 在符号存在前为编译期 RED（构建即失败），原始诊断：

```text
internal/service/diagnostics_bugfix_test.go:30:42: undefined: ErrInstanceLock
internal/service/diagnostics_bugfix_test.go:47:5: e.diag undefined (type *Engine has no field or method diag)
internal/service/diagnostics_bugfix_test.go:67:5: e.diag undefined (type *Engine has no field or method diag)
internal/service/diagnostics_bugfix_test.go:88:5: e.diag undefined (type *Engine has no field or method diag)
internal/service/diagnostics_bugfix_test.go:109:5: e.diag undefined (type *Engine has no field or method diag)
```

## 五、GREEN 原始输出（修复后）

命令：

```text
$ go test -mod=readonly -race -count=1 -v ./internal/service/ -run 'TestLiveAuthorizationRecheckedAtRuntime|TestTerminalGameReleasesDeciders|TestBackgroundFailureDiagnostics'
```

输出：

```text
=== RUN   TestBackgroundFailureDiagnostics
=== RUN   TestBackgroundFailureDiagnostics/snapshot_decode
=== RUN   TestBackgroundFailureDiagnostics/model_construction
=== RUN   TestBackgroundFailureDiagnostics/instance_lock_txn
=== RUN   TestBackgroundFailureDiagnostics/auth_denied_is_not_fault
--- PASS: TestBackgroundFailureDiagnostics (0.01s)
    --- PASS: TestBackgroundFailureDiagnostics/snapshot_decode (0.00s)
    --- PASS: TestBackgroundFailureDiagnostics/model_construction (0.00s)
    --- PASS: TestBackgroundFailureDiagnostics/instance_lock_txn (0.00s)
    --- PASS: TestBackgroundFailureDiagnostics/auth_denied_is_not_fault (0.00s)
=== RUN   TestLiveAuthorizationRecheckedAtRuntime
=== RUN   TestLiveAuthorizationRecheckedAtRuntime/start_forbidden
=== RUN   TestLiveAuthorizationRecheckedAtRuntime/resume_forbidden
=== RUN   TestLiveAuthorizationRecheckedAtRuntime/running_worker_pauses_without_fault
--- PASS: TestLiveAuthorizationRecheckedAtRuntime (0.00s)
    --- PASS: TestLiveAuthorizationRecheckedAtRuntime/start_forbidden (0.00s)
    --- PASS: TestLiveAuthorizationRecheckedAtRuntime/resume_forbidden (0.00s)
    --- PASS: TestLiveAuthorizationRecheckedAtRuntime/running_worker_pauses_without_fault (0.00s)
=== RUN   TestTerminalGameReleasesDeciders
=== RUN   TestTerminalGameReleasesDeciders/stop_releases
=== RUN   TestTerminalGameReleasesDeciders/completion_releases
--- PASS: TestTerminalGameReleasesDeciders (0.03s)
    --- PASS: TestTerminalGameReleasesDeciders/stop_releases (0.00s)
    --- PASS: TestTerminalGameReleasesDeciders/completion_releases (0.03s)
PASS
ok  	texas-holdem/internal/service	1.229s
```

## 六、全量验证原始输出

```text
$ go test -mod=readonly -race -count=1 ./...
ok  	texas-holdem/cmd/config	1.259s
ok  	texas-holdem/cmd/server	1.393s
ok  	texas-holdem/internal/agent	1.812s
ok  	texas-holdem/internal/conf	1.263s
ok  	texas-holdem/internal/data	1.920s
ok  	texas-holdem/internal/poker	2.021s
ok  	texas-holdem/internal/server	2.103s
ok  	texas-holdem/internal/service	2.425s
?   	texas-holdem/web	[no test files]
exit=0

$ go vet ./...
exit=0
```

全量 `go test` 无 FAIL；`go vet` 无告警。

## 七、未验证边界 / 说明

- 未启动真实 HTTP/MySQL/Redis，未做端到端联调；`guard` 的实例锁包装仅在 `internal/data` 的 sqlmock 单测层面验证，真实 MySQL 锁失效链路未验证。
- 未验证真实进程「运行时关闭付费开关」的外部配置热更新链路：本修复验证的是引擎侧在 `Options.AllowPaidModels` 变化后的边界行为；配置如何热更新到 Engine 不在本次范围。
- `internal/data/read_cancel_test.go` 中原 `err != service.ErrStorage` 改为 `errors.Is(err, service.ErrStorage)`，因为实例锁错误现在包装 `ErrStorage`；断言语义（真实故障必须返回存储错误）不变，且更精确。
- 后台诊断默认写 stderr，未接入结构化日志系统；`ReportDiag` 可选注入点以未导出 `Engine.diag` 提供给同包测试，未改公共 `Manager`/DTO 契约。
- `web/embed.go` 的 `pattern dist: no matching files found` 为既有构建前置条件，与本次改动无关；全量测试通过。
