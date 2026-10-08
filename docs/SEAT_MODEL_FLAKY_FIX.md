# 时序 flaky 修复：TestSeatModelFallbackKeepsBudget

阶段 1「按座位选模型」遗留的未提交测试修复。范围仅限
`internal/service/seat_model_test.go`；**未改动** `internal/service/worker.go`。

## 1. 根因判定：测试问题，非 worker 并发计数 bug

`TestSeatModelFallbackKeepsBudget` 原实现只等「首个动作落库」就断言
`LiveCallsUsed == 1`：

```go
eventually(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.actions) >= 1 })
v, _ := e.Get(context.Background(), "mixed")
if v.LiveCallsUsed != 1 { ... }
```

但该局 `d` 的 `Decide` 恒定返回错误，真实请求失败后按阶段 1 语义**保留预扣额度**并继续推进
游戏；后台 worker（`loop` → `step`，25ms tick + wake）在首个动作落库后会继续对真实座位发起
请求，每次真实调用都预扣一次额度，因此读取到的 `LiveCallsUsed` 是中间值，并随调度时机变化
（非 -race 更快、稳定读到更大值；-race 更慢、偶发读到 1）。

关键点：测试写库时 `Config.MaxLiveCalls` 仍是 `mixedLiveConfig()` 的 **100**
（`seedMixed` 只把第 4 个参数写进 `View.MaxLiveCalls`），而 worker 额度判定读取的是
`r.Config.MaxLiveCalls`（`worker.go:150`），所以真实调用可持续到 100 才暂停，暴露竞态。

计数逻辑本身无 bug：

- `worker.go:188` 在**派发请求前**对真实调用预扣一次 `r.View.LiveCallsUsed++`；
- `commitDecision`（含失败 fallback 分支）**从不**再自增/回退 `LiveCallsUsed`。

即：一次真实请求恰好计一次，失败不退还 —— 与阶段 1 已确认语义一致。故仅修测试。

## 2. 修复方式

文件：`internal/service/seat_model_test.go:322-355`

- 将持久化额度上限显式对齐：`c.MaxLiveCalls = 1` + `seedMixed(t, r, c, 0, 1, false)`；
- 采用与 `manager_test.go` 的 `failed_call_precharged_and_limit_pauses` 相同的同步等待：
  先 `eventually` 等到稳定终态 `paused` + `reason == "cost_limit"`，再断言首个动作
  `Source == "fallback"`（且 `ErrorCode != nil`）、`LiveCallsUsed == 1`。

这样在首个真实请求预扣后，轮到真实座位再次行动时即稳定停在 `cost_limit`，读取终态无竞态。

## 3. 复现证据（修复前，原始输出）

```
$ for i in 1 2 3 4 5; do go test -mod=readonly -count=1 -run TestSeatModelFallbackKeepsBudget ./internal/service/; done
=== run 1 ===
--- FAIL: TestSeatModelFallbackKeepsBudget (0.00s)
    seat_model_test.go:341: failed real request must retain budget: 4
FAIL
FAIL	texas-holdem/internal/service	3.035s
FAIL
=== run 2 ===
--- FAIL: TestSeatModelFallbackKeepsBudget (0.00s)
    seat_model_test.go:341: failed real request must retain budget: 4
FAIL
FAIL	texas-holdem/internal/service	0.139s
FAIL
=== run 3 ===
--- FAIL: TestSeatModelFallbackKeepsBudget (0.00s)
    seat_model_test.go:341: failed real request must retain budget: 4
FAIL
FAIL	texas-holdem/internal/service	0.140s
FAIL
=== run 4 ===
--- FAIL: TestSeatModelFallbackKeepsBudget (0.00s)
    seat_model_test.go:341: failed real request must retain budget: 4
FAIL
FAIL	texas-holdem/internal/service	0.143s
FAIL
=== run 5 ===
--- FAIL: TestSeatModelFallbackKeepsBudget (0.00s)
    seat_model_test.go:341: failed real request must retain budget: 4
FAIL
FAIL	texas-holdem/internal/service	0.141s
FAIL
```

## 4. 修复后验证（原始输出）

### 4.1 全量 service（-race）

```
$ go test -mod=readonly -race -count=1 ./internal/service/
ok  	texas-holdem/internal/service	3.464s
```

### 4.2 目标测试连续 5 次

```
$ go test -mod=readonly -count=5 -run TestSeatModelFallbackKeepsBudget ./internal/service/
ok  	texas-holdem/internal/service	0.184s
```

### 4.3 seat_model 全部测试（-v，确认无回归）

```
$ go test -mod=readonly -count=1 -run 'TestSeatModel' -v ./internal/service/
=== RUN   TestSeatModelValidate
=== RUN   TestSeatModelValidate/live_mixed_seats_ok
=== RUN   TestSeatModelValidate/live_rejects_bad_refs
=== RUN   TestSeatModelValidate/live_rejects_bad_refs/unknown
=== RUN   TestSeatModelValidate/live_rejects_bad_refs/unsupported
=== RUN   TestSeatModelValidate/live_rejects_bad_refs/mock_provider
=== RUN   TestSeatModelValidate/live_real_ref_without_factory_rejected
=== RUN   TestSeatModelValidate/live_all_mock_without_factory_ok
=== RUN   TestSeatModelValidate/mock_mode_normalizes_refs
--- PASS: TestSeatModelValidate (0.00s)
    --- PASS: TestSeatModelValidate/live_mixed_seats_ok (0.00s)
    --- PASS: TestSeatModelValidate/live_rejects_bad_refs (0.00s)
        --- PASS: TestSeatModelValidate/live_rejects_bad_refs/unknown (0.00s)
        --- PASS: TestSeatModelValidate/live_rejects_bad_refs/unsupported (0.00s)
        --- PASS: TestSeatModelValidate/live_rejects_bad_refs/mock_provider (0.00s)
    --- PASS: TestSeatModelValidate/live_real_ref_without_factory_rejected (0.00s)
    --- PASS: TestSeatModelValidate/live_all_mock_without_factory_ok (0.00s)
    --- PASS: TestSeatModelValidate/mock_mode_normalizes_refs (0.00s)
=== RUN   TestSeatModelBudgetIsolation
=== RUN   TestSeatModelBudgetIsolation/live_all_mock_no_budget
=== RUN   TestSeatModelBudgetIsolation/live_mixed_only_real_consumes
--- PASS: TestSeatModelBudgetIsolation (0.01s)
    --- PASS: TestSeatModelBudgetIsolation/live_all_mock_no_budget (0.00s)
    --- PASS: TestSeatModelBudgetIsolation/live_mixed_only_real_consumes (0.00s)
=== RUN   TestSeatModelExhaustedBudget
=== RUN   TestSeatModelExhaustedBudget/real_seat_pauses
=== RUN   TestSeatModelExhaustedBudget/mock_seat_continues
--- PASS: TestSeatModelExhaustedBudget (0.00s)
    --- PASS: TestSeatModelExhaustedBudget/real_seat_pauses (0.00s)
    --- PASS: TestSeatModelExhaustedBudget/mock_seat_continues (0.00s)
=== RUN   TestSeatModelFallbackKeepsBudget
--- PASS: TestSeatModelFallbackKeepsBudget (0.00s)
PASS
ok  	texas-holdem/internal/service	0.179s
```

### 4.4 go vet

```
$ go vet ./internal/service/
（无输出，exit=0）
```

## 5. 未验证边界

- `-count=5` 仅覆盖非 -race 的连续重复；-race 全量为单次（`-count=1`）。
- 未在真实 MySQL/Redis 或真实模型 provider 下运行（本次为 service 层单测，测试仓为内存 repo）。
- 未 commit/push，按约定停在未提交状态。
