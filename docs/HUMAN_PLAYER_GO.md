# 真人上桌（Go lane）实现与验证报告

依据冻结设计契约（架构师完整设计报告）实现「1 个真人座位 + 其余 AI、Web 实时点击、超时 60s 自动弃牌、
游戏级 HttpOnly Cookie 会话」。本文档只覆盖 Go lane；Web lane 并行，未改动 `web/` 下其他文件。

> 环境：本机未启动真实 HTTP/MySQL/Redis；未做真实浏览器、真实 Cookie、真实超时链路联调。
> 本文所有「通过」均来自 `go test` 本地真实执行输出（原始输出见文末），不包含任何凭据。

## 1. 改动文件清单

| 文件 | 改动 |
|---|---|
| `internal/poker/rules.go` | 新增只读窄接口 `PrivateHole` |
| `internal/poker/private_test.go` | 新增 `TestPrivateHoleAccess` |
| `internal/service/contracts.go` | `SeatKind`、`SeatConfig/SeatView.Kind`、`PlayerSessionHash`、`PlayerView/PlayerGameView/HumanAction/HumanWait`、`GameView.HumanSeat`、`HandView.WaitingFor`、`Record` 私有字段、`Options.HumanActionTimeout`、Manager `GetPlayer/Act`、错误码 |
| `internal/service/manager.go` | 座位校验、Create 私有摘要/超时、`humanSeat`、`GetPlayer/Act/playerView/authorized`、Control 暂停冻结/恢复续期/截止前置超时、New 恢复冻结 |
| `internal/service/worker.go` | 真人等待分支（先于 live/模型）、`submit` 统一提交链路、超时弃牌 |
| `internal/service/human_test.go` | 新增 12 个真人场景测试 + 2 个补充测试 |
| `internal/service/manager_test.go` | 测试夹具新增 `noCurrent` |
| `internal/server/dto.go` | `SeatDTO.Kind`、`HandDTO.WaitingFor`、`GameDTO.HumanSeat/Player`、`PlayerDTOView`、`LegalActionsDTO`、`PlayerGame`、`CreateSeatDTO` kind/model_ref 严格解析、`HumanActionDTO.HumanInput` |
| `internal/server/http.go` | Cookie（生成/下发/校验）、`GET /player`、`POST /action`、loopback 限制、错误映射 |
| `internal/server/http_test.go` | `managerStub` 实现 `GetPlayer/Act` |
| `internal/server/human_test.go` | 隐私与 HTTP 边界测试 |
| `internal/conf/config.go` | `Config.HumanActionTimeout`、`gameplay.human_action_timeout`、默认 60s、校验、环境变量 |
| `internal/conf/config_test.go` | `TestHumanActionTimeoutConfig` + 隔离环境变量 |
| `internal/data/repository_test.go` | 断言私有字段不进入公共缓存 |
| `cmd/server/main.go` | 透传 `HumanActionTimeout` |
| `docs/contracts.md` | 更新 PrivateHole、seat kind、真人接口/DTO/错误码/Cookie/超时语义 |

未改动：`web/` 其他文件（Web lane）、`scripts/`、`config.yaml`（在禁改写范围内；`gameplay.human_action_timeout`
缺省即 60s）、`go.mod`/`go.sum`。

## 2. 核心实现点与位置（file:line）

1. **poker 窄接口** `PrivateHole`：`internal/poker/rules.go:140`（只读自己已发牌；非 actor/弃牌/全押/完成可读，未发牌/非法拒绝）。
2. **service 契约**：`internal/service/contracts.go`
   - `SeatKind`/常量 `:14-19`；`SeatConfig.Kind` `:27`；`SeatView.Kind` `:44`
   - `CreateGame.PlayerSessionHash` `:38`；`HandView.WaitingFor` `:50`；`GameView.HumanSeat` `:72`
   - `PlayerSessionHash` `:79`；`HumanAction` `:82`；`PlayerView` `:88`；`PlayerGameView` `:97`；`HumanWait` `:104`
   - Manager `GetPlayer/Act` `:132-133`；`Record.HumanSessionHash/HumanActionTimeout/HumanWait` `:144-146`；`Options.HumanActionTimeout` `:169`
   - 错误码 `:179` 起（`PLAYER_NOT_AUTHORIZED`/`HUMAN_LOCAL_ONLY`/`ILLEGAL_ACTION`/`STALE_ACTION`/`ACTION_EXPIRED`）
3. **座位校验**：`internal/service/manager.go:199`（0/1 真人、真人 `model_ref==""`、`Stack>0`、摘要非零/全 AI 不得提供）。
4. **Create**：`manager.go:288`（`SeatView.Kind`、`HumanSeat`、AI 才建 Decider、摘要入私有 Record、`HumanActionTimeout` 落库）。
5. **GetPlayer/Act**：`manager.go:587` / `:607`；`authorized` 恒定时间比较 `:549`；`playerView` 私牌/合法动作/截止时间投影 `:558`。
6. **worker 真人等待分支**：`internal/service/worker.go:109`（先于 live 额度/模型；未截止不推进；到点弃牌）。
7. **统一提交链路**：`worker.go:200` `submit`（Apply + revision/快照/动作/结算原子 CAS + 清等待）。
8. **超时弃牌语义**：`worker.go:113` / `manager.go:607` Act 截止前置（`source=fallback`、`error_code=HUMAN_ACTION_TIMEOUT`、`kind=fold`）。
9. **pause/resume/stop 人类语义**：`manager.go:476` Control；冻结 `Remaining`、恢复续期、stop 清等待、`humanWaitExpired` `:471`。
10. **恢复冻结**：`manager.go` `New` 内 `running→paused/recovery` 时 `Remaining=max(0, deadline-now)`。
11. **conf**：`internal/conf/config.go:68`（字段）、`:375`（schema）、`:258`（默认 60s）、`:607`（env）、`:670`（校验）。
12. **data 私有持久化**：`internal/data/repository.go`（`Record` 已在 MySQL JSON 内；Redis 仅 `GameView`，见 §5 证据）。
13. **server Cookie/REST**：`internal/server/http.go:82/89/100/108/120`（loopback/hasHuman/session/cookie/parse）、`:227`（GET player）、`:248`（POST action）、错误映射 `:396-404`。
14. **server DTO**：`internal/server/dto.go:67`（HumanSeat/Player）、`:72`（PlayerDTOView）、`:81`（LegalActionsDTO）、`:201`（PlayerGame）、`:290/333`（输入解析）。

## 3. 场景 → 测试函数 → 结果

| 设计报告场景 | 测试函数 | 位置 | 结果 |
|---|---|---|---|
| 全 AI 旧请求与旧记录兼容 | `TestHumanSeatBackwardCompatibility` | service/human_test.go | PASS |
| 最多一个真人、模型引用校验 | `TestHumanSeatValidation` | service/human_test.go | PASS（6 子用例） |
| 非 actor/弃牌/全押时读取自己牌 | `TestPrivateHoleAccess` | poker/private_test.go | PASS（3 子用例） |
| 本人授权与其他浏览器拒绝 | `TestPlayerSessionAuthorization` | service/human_test.go | PASS |
| 公共 HTTP/Redis 不含私牌 | `TestHumanPublicDTOPrivacy` + data 缓存断言 | server/human_test.go、data/repository_test.go | PASS |
| 真人视角绝无其他 AI 底牌 | `TestHumanPlayerDTOPrivacy` | server/human_test.go | PASS |
| 四种引擎动作 | `TestHumanActions` | service/human_test.go | PASS（fold/check/call/raise_to） |
| 非法动作不推进、不重置时限 | `TestHumanIllegalAction` | service/human_test.go | PASS |
| 能 check 时超时仍 fold | `TestHumanTimeoutAlwaysFolds` | service/human_test.go | PASS |
| 截止、双击、跨手、动作/超时竞争 | `TestHumanActionConcurrency` | service/human_test.go | PASS（3 子用例） |
| 暂停冻结、恢复、停止、晚请求 | `TestHumanPauseResumeStop` + `TestHumanPauseAfterExpiryFolds` | service/human_test.go | PASS |
| 重启恢复与过期等待 | `TestHumanRecovery` | service/human_test.go | PASS（2 子用例） |
| 真人不消耗 AI 调用额度 | `TestHumanLiveBudgetIsolation` | service/human_test.go | PASS |
| 动作/快照/结果 CAS 原子提交 | `TestHumanCommitAtomicity` | service/human_test.go | PASS |
| （补充）超时配置 | `TestHumanActionTimeoutConfig` | conf/config_test.go | PASS |
| （补充）HTTP Cookie 会话与动作边界 | `TestHumanHTTPPlayerAndAction`、`TestHumanCreateSetsCookieAndLocalOnly` | server/human_test.go | PASS |

### 未覆盖（Go 单测无法验证，需真实链路）

| 场景 | 状态 |
|---|---|
| `human_web_flow`（实浏览器点击、刷新、跨标签、匿名观战） | **未验证**（Web lane 未联调） |
| `human_lifecycle_e2e`（真 MySQL/Redis、超时、暂停、重启） | **未验证**（本机未启动存储/HTTP） |
| `human_embedded_web`（构建产物嵌入） | **未验证** |
| 真实 Cookie HttpOnly/SameSite 浏览器行为、真实 60s 倒计时链路 | **未验证** |
| 真 MySQL 旧 JSON 记录解码、Redis 缓存真实回源 | **未验证**（仅内存/JSON 往返与 sqlmock） |

## 4. 完整命令原始输出

### `go vet ./...`

```text
$ go vet ./...
exit=0
```

### `go test -mod=readonly -race -count=1 ./...`

```text
$ go test -mod=readonly -race -count=1 ./...
ok  	texas-holdem/cmd/config	1.277s
ok  	texas-holdem/cmd/server	1.965s
ok  	texas-holdem/internal/agent	2.024s
ok  	texas-holdem/internal/conf	2.189s
ok  	texas-holdem/internal/data	2.672s
ok  	texas-holdem/internal/poker	2.744s
ok  	texas-holdem/internal/server	2.355s
ok  	texas-holdem/internal/service	3.062s
?   	texas-holdem/web	[no test files]
exit=0
```

失败计数：**0**（全部 8 个包 ok，`web` 无测试文件）。

### `go test -mod=readonly -count=1 -v -run 'TestHuman|TestPrivateHole|TestPlayerSession' ./internal/...`

```text
testing: warning: no tests to run
PASS
ok  	texas-holdem/internal/agent	0.294s [no tests to run]
=== RUN   TestHumanActionTimeoutConfig
=== RUN   TestHumanActionTimeoutConfig/default_60s
=== RUN   TestHumanActionTimeoutConfig/yaml_and_env_override
=== RUN   TestHumanActionTimeoutConfig/explicit_zero_and_bad_type_rejected
--- PASS: TestHumanActionTimeoutConfig (0.00s)
    --- PASS: TestHumanActionTimeoutConfig/default_60s (0.00s)
    --- PASS: TestHumanActionTimeoutConfig/yaml_and_env_override (0.00s)
    --- PASS: TestHumanActionTimeoutConfig/explicit_zero_and_bad_type_rejected (0.00s)
PASS
ok  	texas-holdem/internal/conf	0.430s
testing: warning: no tests to run
PASS
ok  	texas-holdem/internal/data	0.359s [no tests to run]
=== RUN   TestPrivateHoleAccess
=== RUN   TestPrivateHoleAccess/actor_and_non_actor_read_own_cards
=== RUN   TestPrivateHoleAccess/folded_allin_complete_still_readable
=== RUN   TestPrivateHoleAccess/illegal_seat_and_undealt_refused
--- PASS: TestPrivateHoleAccess (0.00s)
    --- PASS: TestPrivateHoleAccess/actor_and_non_actor_read_own_cards (0.00s)
    --- PASS: TestPrivateHoleAccess/folded_allin_complete_still_readable (0.00s)
    --- PASS: TestPrivateHoleAccess/illegal_seat_and_undealt_refused (0.00s)
PASS
ok  	texas-holdem/internal/poker	0.572s
=== RUN   TestHumanPublicDTOPrivacy
--- PASS: TestHumanPublicDTOPrivacy (0.00s)
=== RUN   TestHumanPlayerDTOPrivacy
--- PASS: TestHumanPlayerDTOPrivacy (0.00s)
=== RUN   TestHumanHTTPPlayerAndAction
=== RUN   TestHumanHTTPPlayerAndAction/missing_cookie_403
=== RUN   TestHumanHTTPPlayerAndAction/duplicate_cookie_403
=== RUN   TestHumanHTTPPlayerAndAction/valid_cookie_200
=== RUN   TestHumanHTTPPlayerAndAction/action_valid_and_bad
=== RUN   TestHumanHTTPPlayerAndAction/error_mapping
--- PASS: TestHumanHTTPPlayerAndAction (0.00s)
    --- PASS: TestHumanHTTPPlayerAndAction/missing_cookie_403 (0.00s)
    --- PASS: TestHumanHTTPPlayerAndAction/duplicate_cookie_403 (0.00s)
    --- PASS: TestHumanHTTPPlayerAndAction/valid_cookie_200 (0.00s)
    --- PASS: TestHumanHTTPPlayerAndAction/action_valid_and_bad (0.00s)
    --- PASS: TestHumanHTTPPlayerAndAction/error_mapping (0.00s)
=== RUN   TestHumanCreateSetsCookieAndLocalOnly
=== RUN   TestHumanCreateSetsCookieAndLocalOnly/loopback_sets_cookie
=== RUN   TestHumanCreateSetsCookieAndLocalOnly/non_loopback_human_rejected
--- PASS: TestHumanCreateSetsCookieAndLocalOnly (0.00s)
    --- PASS: TestHumanCreateSetsCookieAndLocalOnly/loopback_sets_cookie (0.00s)
    --- PASS: TestHumanCreateSetsCookieAndLocalOnly/non_loopback_human_rejected (0.00s)
PASS
ok  	texas-holdem/internal/server	0.865s
=== RUN   TestHumanSeatBackwardCompatibility
--- PASS: TestHumanSeatBackwardCompatibility (0.00s)
=== RUN   TestHumanSeatValidation
=== RUN   TestHumanSeatValidation/two_humans
=== RUN   TestHumanSeatValidation/human_with_model_ref
=== RUN   TestHumanSeatValidation/human_zero_stack
=== RUN   TestHumanSeatValidation/human_without_session
=== RUN   TestHumanSeatValidation/ai_with_session
=== RUN   TestHumanSeatValidation/unknown_kind
--- PASS: TestHumanSeatValidation (0.00s)
    --- PASS: TestHumanSeatValidation/two_humans (0.00s)
    --- PASS: TestHumanSeatValidation/human_with_model_ref (0.00s)
    --- PASS: TestHumanSeatValidation/human_zero_stack (0.00s)
    --- PASS: TestHumanSeatValidation/human_without_session (0.00s)
    --- PASS: TestHumanSeatValidation/ai_with_session (0.00s)
    --- PASS: TestHumanSeatValidation/unknown_kind (0.00s)
=== RUN   TestPlayerSessionAuthorization
--- PASS: TestPlayerSessionAuthorization (0.00s)
=== RUN   TestHumanActions
=== RUN   TestHumanActions/check
=== RUN   TestHumanActions/fold
=== RUN   TestHumanActions/call
=== RUN   TestHumanActions/raise_to
--- PASS: TestHumanActions (0.00s)
    --- PASS: TestHumanActions/check (0.00s)
    --- PASS: TestHumanActions/fold (0.00s)
    --- PASS: TestHumanActions/call (0.00s)
    --- PASS: TestHumanActions/raise_to (0.00s)
=== RUN   TestHumanIllegalAction
--- PASS: TestHumanIllegalAction (0.00s)
=== RUN   TestHumanTimeoutAlwaysFolds
--- PASS: TestHumanTimeoutAlwaysFolds (0.00s)
=== RUN   TestHumanActionConcurrency
=== RUN   TestHumanActionConcurrency/expired_act_triggers_timeout_fold
=== RUN   TestHumanActionConcurrency/stale_and_double_submit
=== RUN   TestHumanActionConcurrency/concurrent_act_single_commit
--- PASS: TestHumanActionConcurrency (0.00s)
    --- PASS: TestHumanActionConcurrency/expired_act_triggers_timeout_fold (0.00s)
    --- PASS: TestHumanActionConcurrency/stale_and_double_submit (0.00s)
    --- PASS: TestHumanActionConcurrency/concurrent_act_single_commit (0.00s)
=== RUN   TestHumanPauseResumeStop
--- PASS: TestHumanPauseResumeStop (0.00s)
=== RUN   TestHumanPauseAfterExpiryFolds
--- PASS: TestHumanPauseAfterExpiryFolds (0.00s)
=== RUN   TestHumanRecovery
=== RUN   TestHumanRecovery/future_wait_frozen
=== RUN   TestHumanRecovery/expired_wait_zero_remaining
--- PASS: TestHumanRecovery (0.00s)
    --- PASS: TestHumanRecovery/future_wait_frozen (0.00s)
    --- PASS: TestHumanRecovery/expired_wait_zero_remaining (0.00s)
=== RUN   TestHumanLiveBudgetIsolation
--- PASS: TestHumanLiveBudgetIsolation (0.00s)
=== RUN   TestHumanCommitAtomicity
--- PASS: TestHumanCommitAtomicity (0.00s)
PASS
ok  	texas-holdem/internal/service	0.704s
```

真人相关测试：**30 PASS / 0 FAIL**（含子测试）。

## 5. 隐私与安全证据（代码级）

- 公共 `Game()` 恒 `Player=nil`，`MarshalGame`/Redis 只调用公共转换（`internal/server/dto.go:180` 起；`internal/data/repository.go:363` `publish` 仅编码 `GameView`）。
- `Record` 私有字段（`HumanSessionHash`/`HumanWait`/`HumanActionTimeout`）只存在于 MySQL JSON；`internal/data/repository_test.go` 断言公共缓存 JSON 不含这些字段及底牌。
- Cookie：32 字节 `crypto/rand`，服务端仅存 `sha256` 摘要并以 `subtle.ConstantTimeCompare` 比较；原值只写入 `HttpOnly`+`SameSite=Strict` Cookie；重复/格式错误 Cookie → 403。
- 私牌只经 `PrivateHole` 从引擎快照投影给配置中固定真人座位，浏览器提交的座位号不被采用。

## 6. 已知偏差 / 待确认

- `config.yaml` 未新增 `gameplay:` 段（在禁改写范围内）；运行时缺省即为 60s，可被 `POKER_HUMAN_ACTION_TIMEOUT` 或后续配置覆盖。
- 设计报告要求「显式 null `kind`/`model_ref` 拒绝」：通过 `json.RawMessage` 解析已实现。
- `HumanActionTimeout` 按设计在创建时写入私有 `Record`；`service.Options` 负值拒绝、零值规范化为 60s。
- 待联调：真实 Cookie 生命周期、真实 60s 倒计时、Web 点击链路、真实存储重启恢复。

**未完成/未验证项已在上表明确标注，不视为完成。**
