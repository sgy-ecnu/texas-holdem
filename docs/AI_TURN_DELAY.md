# AI 行动思考节奏（gameplay.ai_turn_delay）交付与验证

## 目标

给 AI 行动加「思考节奏」：AI 每次提交动作后、推进到下一个 actor 前插入一个可取消的
思考停顿，让轮转过程人眼可见（类似斗地主「轮到谁、谁在出牌」），而不是 mock 瞬间跑完。
停顿只作用于 AI 座位；真人座位仍由 `gameplay.human_action_timeout` 控制自身操作时间。

## 配置字段

| 层级 | 字段 | 默认值 | 说明 |
| --- | --- | --- | --- |
| YAML | `gameplay.ai_turn_delay` | `1.5s` | time.Duration；显式 `0`/负值/类型错误一律 `CONFIG_INVALID` |
| env | `POKER_AI_TURN_DELAY` | 覆盖 YAML | 非空则 `time.ParseDuration` 解析；`0s`/负值同样 `CONFIG_INVALID` |
| Go | `conf.Config.AITurnDelay time.Duration` | `1500ms`（`defaultAITurnDelay`） | 由 `finalize()` 校验 `> 0` |
| service | `service.Options.AITurnDelay time.Duration` | 零值=不等待 | 由 `cmd/server/main.go` 从 `c.AITurnDelay` 透传 |

`Options.AITurnDelay <= 0` 时直接跳过等待，因此既有测试（默认不设置该字段）不会变慢。

## 延迟实现位置

| 位置 | 说明 |
| --- | --- |
| `internal/service/worker.go:350` | `func thinkPause(ctx context.Context, delay time.Duration)` —— 实际停顿实现 |
| `internal/service/worker.go:248-266` | `decide()` 提交动作后调用停顿（`commitDecision` 返回 true 且 `AITurnDelay > 0`） |
| `internal/service/worker.go:269-344` | `commitDecision()` —— 原 `decide` 持锁落库部分抽出，返回是否落库成功 |
| `internal/service/worker.go:29` | `step()` 门闩：`... || e.pausing` 时不推进下一个 actor |
| `internal/service/manager.go:34` | `Engine.pausing bool` + `Engine.pauseCancel context.CancelFunc` |
| `internal/service/manager.go:109-115` | `cancelPauseLocked()` —— 取消停顿并清门闩（须持锁） |
| `internal/service/manager.go:536` | `Control()` 中 `e.cancelPauseLocked()`，pause/stop/resume/start 立即打断停顿 |
| `internal/service/manager.go:81-89` | `Close()` 中取消停顿，配合 `e.ctx` 取消 |

## 可打断机制

```
saved := commitDecision(...)                 // 持 e.mu 落库，返回是否提交
if saved && AITurnDelay > 0 {
    pauseCtx, e.pauseCancel = context.WithCancel(e.ctx)
    e.pausing = true                          // 持锁置门闩
}
e.mu.Unlock()                                // 停顿期间不持锁
if saved && pauseCtx != nil {
    thinkPause(pauseCtx, AITurnDelay)         // select { <-ctx.Done(); <-time.After(delay) }
    e.mu.Lock(); e.cancelPauseLocked(); e.mu.Unlock()
}
e.notify()                                   // 停顿结束才唤醒 loop 推进下一个 actor
```

- `thinkPause` 严格使用 `select` + `ctx.Done()` + `time.After`。
- `pauseCtx` 派生自 `e.ctx`：`Close()` 取消 `e.ctx` 即打断；`Control()` 调
  `cancelPauseLocked()` 取消 `pauseCtx`，pause/stop/resume/start 无需等停顿自然结束。
- 停顿期间不持有 `e.mu`，不阻塞任何控制命令；`step()` 通过 `e.pausing` 门闩避免 ticker
  在停顿窗口内提前推进 actor。
- 真人座位在 `step()` 中先于 AI 分支返回，从不进入 `decide()`，因此天然不加该停顿。
- mock 与 live 共用 `decide()`/`commitDecision()`，两种 AI 统一加停顿；live 的网络耗时
  之外再叠加此步进停顿。

## 新增测试

| 场景 | 测试函数 | 结果 |
| --- | --- | --- |
| ctx 已取消时停顿立即返回 | `internal/service.TestAITurnDelayInterruptibleByContext/pre_canceled` | PASS |
| 停顿进行中 cancel 立即打断 | `internal/service.TestAITurnDelayInterruptibleByContext/cancel_during_pause` | PASS |
| `delay<=0` 不停顿（不拖慢测试） | `internal/service.TestAITurnDelayInterruptibleByContext/zero_delay_is_noop` | PASS |
| AI 提交后停顿窗口内不推进下一 actor，之后继续轮转 | `internal/service.TestAITurnDelayPacesAIActions` | PASS |
| 配置默认 1.5s | `internal/conf.TestAITurnDelayConfig/default_1500ms` | PASS |
| YAML 2s 与 env 300ms 覆盖 | `internal/conf.TestAITurnDelayConfig/yaml_and_env_override` | PASS |
| 显式 0/负值/负类型、env 0/负值拒绝 | `internal/conf.TestAITurnDelayConfig/explicit_zero_negative_and_bad_type_rejected` | PASS |

## 验证命令与完整原始输出

### `go vet ./...`

```
$ go vet ./...
（无输出，退出码 0）
```

### `go test -mod=readonly -race -count=1 ./...`

```
$ go test -mod=readonly -race -count=1 ./...
ok  	texas-holdem/cmd/config	1.252s
ok  	texas-holdem/cmd/server	1.398s
ok  	texas-holdem/internal/agent	1.449s
ok  	texas-holdem/internal/conf	1.612s
ok  	texas-holdem/internal/data	1.757s
ok  	texas-holdem/internal/poker	2.365s
ok  	texas-holdem/internal/server	2.345s
ok  	texas-holdem/internal/service	2.568s
?   	texas-holdem/web	[no test files]
```

退出码 0；通过包计数：8 个 `ok`，1 个 `[no test files]`，0 个 FAIL。
另单独运行新增用例：

```
$ go test -mod=readonly -race -count=1 -run 'TestAITurnDelay' ./internal/service/
ok  	texas-holdem/internal/service	4.280s
$ go test -mod=readonly -race -count=1 -run 'TestAITurnDelayConfig' ./internal/conf/
ok  	texas-holdem/internal/conf	1.443s
```

## 改动文件

- `internal/conf/config.go`：新增字段/默认值/schema/合并/应用/env/校验。
- `internal/conf/config_test.go`：新增 `TestAITurnDelayConfig`；`envKeys` 隔离列表加入 `POKER_AI_TURN_DELAY`。
- `internal/service/contracts.go`：`Options` 新增 `AITurnDelay`。
- `internal/service/worker.go`：抽出 `commitDecision`、新增 `thinkPause`、`step` 门闩、提交后停顿。
- `internal/service/manager.go`：`Engine` 新增 `pausing`/`pauseCancel`、`cancelPauseLocked`，`Control`/`Close` 取消停顿。
- `internal/service/ai_turn_delay_test.go`：新增打断与节奏测试（新建）。
- `config/config.yaml`：新增 `gameplay.ai_turn_delay: 1.5s`。
- `cmd/server/main.go`：透传 `AITurnDelay: c.AITurnDelay`。
- `docs/AI_TURN_DELAY.md`：本文件。

## 未验证边界

- 未启动真实 HTTP/MySQL/Redis，未做端到端浏览器轮转观察；「人眼可见节奏」由
  `TestAITurnDelayPacesAIActions` 的时序断言（停顿窗口内动作数不增长）间接证明。
- 未对真实模型 provider 发起付费调用；live 与 mock 共享同一 `decide()` 路径，停顿逻辑
  一致，但真实网络耗时叠加下的观感未实测。
- 停顿时长在并发多局（同一 Engine 单 worker 串行调度）下按局串行叠加，未做多局并发压测。
- 未验证生产配置从 `config.local.yaml` 覆盖 `ai_turn_delay` 的行为（按凭据规则未读本地文件）。

---

# 补充：仅 mock 加人工停顿，live 用真实模型耗时

## 需求
live 模式使用真实模型自身的思考/网络耗时，不再叠加 `ai_turn_delay` 人工停顿；
只有 mock 机器人保留 1.5~2s 的模拟停顿。

## 改动点（file:line）
- `internal/service/worker.go:269` `commitDecision` 签名改为 `(bool, bool)`：
  - 全部提前返回由 `return false` 改为 `return false, false`（`worker.go:272/277/281/286/317/335`）。
  - 末尾改为 `saved := e.save(...) == nil; return saved, saved && r.Config.Mode == "mock"`（`worker.go:343-345`）。
- `internal/service/worker.go:249` `decide()` 改为 `saved, isMock := e.commitDecision(...)`。
- `internal/service/worker.go:252` 停顿条件改为 `if saved && isMock && e.opts.AITurnDelay > 0`。

即 `isMock = saved && r.Config.Mode == "mock"`；live 的 `saved` 为 true 但 `isMock` 为 false，
不创建 `pauseCtx`、不置 `e.pausing`，直接 `e.notify()` 推进下一个 actor。

## 新增测试函数名
- `internal/service.TestAITurnDelaySkippedForLiveMode`（新建，RED→GREEN）
- `internal/service.TestAITurnDelayPacesAIActions`（既有，mock 节奏，回归仍通过）
- `internal/service.TestAITurnDelayInterruptibleByContext`（既有，回归仍通过）

## 场景 → 结果
| 场景 | 测试 | 结果 |
| --- | --- | --- |
| live 模式提交动作后不进入步进停顿（`e.pausing` 恒为 false） | `TestAITurnDelaySkippedForLiveMode` | PASS |
| live 模式第二个动作立即推进，不等待 300ms delay | `TestAITurnDelaySkippedForLiveMode` | PASS |
| mock 模式仍按 delay 停顿、窗口内不推进 | `TestAITurnDelayPacesAIActions` | PASS |
| 停顿可被 ctx 取消打断（回归） | `TestAITurnDelayInterruptibleByContext` | PASS |

### RED（改前，live 当前被错误停顿）
```
$ go test -mod=readonly -race -count=1 -run 'TestAITurnDelaySkippedForLiveMode' ./internal/service/
--- FAIL: TestAITurnDelaySkippedForLiveMode (0.06s)
    ai_turn_delay_test.go:102: live mode entered think pause; should use real model latency only
FAIL
FAIL	texas-holdem/internal/service	3.173s
```

## 验证命令与完整原始输出（GREEN）

```
$ go vet ./...
（无输出，退出码 0）

$ go test -mod=readonly -race -count=1 ./internal/service ./internal/conf
ok  	texas-holdem/internal/service	1.658s
ok  	texas-holdem/internal/conf	1.381s
```

退出码 0；通过包计数：2 个 `ok`，0 个 FAIL。
聚焦运行：

```
$ go test -mod=readonly -race -count=1 -run 'TestAITurnDelay' ./internal/service/
ok  	texas-holdem/internal/service	1.417s
```

## 本轮未验证边界
- live 测试用立即返回的 decider 模拟「真实模型」，验证的是「不再叠加人工停顿」；真实模型
  网络耗时下的实际节奏未做付费/联网实测。
- 未启动真实 HTTP/MySQL/Redis，未做端到端观察。

