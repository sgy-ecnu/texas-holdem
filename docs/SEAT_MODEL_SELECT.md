# 按座位选模型：修复「选了真实模型不生效」验证证据

运行时间：2026-10-06 00:11:27 +0800

统一判定规则：**真实调用 = mode==live 且当前 AI 座位 model_ref != "mock"**。
复现命令（项目根目录）：`python3 tests/e2e/browser/run_seat_model.py`。
真实 Go 二进制 + 嵌入最新 web/dist + Chromium + 受控本地 provider（非真实模型）。
不读取 .env.local / config.local.yaml 的值；连接凭据仅由 local.py/Go 本地载入，不回传。

### 命令

```sh
npm --prefix web run typecheck
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

```

退出码：0

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
dist/assets/index-IpFMil0q.css   40.85 kB │ gzip:  9.60 kB
dist/assets/index-jADg2Ha1.js   283.20 kB │ gzip: 89.99 kB
✓ built in 692ms
```

退出码：0

### 命令

```sh
go test -mod=readonly -race -count=1 ./internal/service/
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text
ok  	texas-holdem/internal/service	6.327s
```

退出码：0

### 命令

```sh
go vet ./internal/service/
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

```

退出码：0

### 命令

```sh
go build -mod=readonly -o /Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/.seat-model-jh33e2et/server ./cmd/server
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

```

退出码：0

```text
health: {"mysql": "available", "redis": "available", "storage_degraded": false, "allow_paid_models": true, "max_live_calls": 1000}; models=["mock", "local/real"]
```

### 命令

```sh
npx --no-install playwright test --config=playwright.seatmodel.config.ts
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser`

```text

Running 4 tests using 1 worker

  ✓  1 [chromium] › seat-model/seat_model.spec.ts:69:1 › mock 模式：模型字段固定不可改，全 mock 局真实 provider 出站为 0 (2.0s)
  ✓  2 [chromium] › seat-model/seat_model.spec.ts:96:1 › live 全 mock：可开局且不产生真实模型调用 (1.7s)
  ✓  3 [chromium] › seat-model/seat_model.spec.ts:119:1 › live 混合座位：仅真实模型座位出站并消耗额度 (1.1s)
  ✓  4 [chromium] › seat-model/seat_model.spec.ts:145:1 › 多浏览器 context：Cookie 隔离且各自配置独立 (919ms)

  4 passed (6.2s)
```

退出码：0

```text
playwright: passed=4 failed=0 not_run=0 exit=0
```

截图目录：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/seat-model-artifacts/20261006-001138`。

```text
cleanup: own_server_stopped=True; provider_stopped=True; 8081_and_provider_released=True; Homebrew_3306_6379_retained=True
```

## 实现改动（file:line）

| 文件 | 位置 | 改动 |
|---|---|---|
| `internal/service/contracts.go` | 208-225 | 新增 `effectiveModelRef`（旧记录缺失引用按 `"mock"` 解释）与 `isRealCall`（唯一判定：live 且 ref != "mock"） |
| `internal/service/manager.go` | 262-279 | `validate`：live 座位 `model_ref=="mock"` 直接接受；其他引用须在目录、非 mock provider、非 unsupported；mock 模式统一规范化为 `"mock"` |
| `internal/service/manager.go` | 330-352 | `Create`：live 局 mock 座位直接 `agent.NewMockDecider()`，真实座位才走 `factory.NewDecider`；factory 为 nil 且存在真实引用则拒绝 |
| `internal/service/worker.go` | 146-188 | 解析当前座位 `realCall`；预算耗尽只对真实请求暂停；live 授权复核保留；mock 引用用 MockDecider，真实引用走 factory 并请求前预扣 |
| `internal/service/worker.go` | 321-331 | `commitDecision`：`ActionView.Source` 按实际执行器判定（mock→`"mock"`，真实→`"model"`，失败回退→`"fallback"`） |
| `internal/service/worker.go` | 279-299, 381 | 停顿按实际执行器（`saved && !isRealCall`），mock 才加 `ai_turn_delay` |
| `web/src/components/ConfigPanel.tsx` | 35-126 | `MOCK_REF`、`makeDraft` 默认 mock、`unavailableReason`、`ModelPicker`（locked 固定「Mock（免费）」；live 可选 mock/真实；不存在/unsupported 等显示原因） |
| `web/src/components/ConfigPanel.tsx` | 158-224 | `hasRealModels`；live 校验跳过真人座位（修复原 173-178 误拒 bug）；费用确认只针对真实模型 |
| `web/src/components/ConfigPanel.tsx` | 405-445 | live→mock 重置 AI 引用为 mock；切回 live 不自动恢复付费选择；文案改为「允许真实模型」 |
| `web/src/components/ConfigPanel.tsx` | 447-478 | 全 mock 的 live 配置显示「本配置不产生真实模型调用」；仅真实模型才显示费用确认 |
| `tests/e2e/browser/tests/scenarios.spec.ts` | scenario 1/4 | mock 字段固定；live 确认门槛需先选真实模型（拦截 health+models） |
| `tests/e2e/browser/human/human.spec.ts` | 36-39 | mock 模式 AI 模型字段固定断言 |
| `tests/e2e/browser/seat-model/**`、`playwright.seatmodel.config.ts`、`run_seat_model.py` | 新增 | 受控 provider + 真实浏览器验收 |

## 额度计数边界（唯一规则：真实调用 = live 且当前座位 ref != "mock"）

| 全局 mode | 座位 model_ref | 执行器 | 消耗额度 | 额度耗尽时 |
|---|---|---|---|---|
| mock | 任意（规范化后存 "mock"） | MockDecider | 否 | 不暂停，继续决策 |
| live | mock | MockDecider | 否 | 不暂停，继续决策 |
| live | 已注册真实模型 | liveDecider | 是（请求前预扣） | `cost_limit` 暂停，不降级为 mock |
| 任意 | human | 真人链路 | 否 | 继续（真人分支优先） |

- 预扣时机：真实请求进入 `decide` 前 `LiveCallsUsed++`，失败/取消/超时均不退还。
- 真实请求失败：`Source="fallback"` 且已消耗额度保留。
- 授权复核：live 局在运行中 `AllowPaidModels` 关闭一律 `unauthorized` 暂停（与 Control 的 start/resume 一致）。

## 单元测试场景 → 测试函数 → 结果

命令：`go test -mod=readonly -race -count=1 -v -run 'TestSeatModel' ./internal/service/`

| 场景 | 测试函数 | 结果 |
|---|---|---|
| live 混合座位（部分 mock 部分真实）validate 通过 | `TestSeatModelValidate/live_mixed_seats_ok` | PASS |
| live 拒绝不存在/unsupported/mock-provider 引用 | `TestSeatModelValidate/live_rejects_bad_refs/{unknown,unsupported,mock_provider}` | PASS |
| live 真实引用但无 factory 拒绝 | `TestSeatModelValidate/live_real_ref_without_factory_rejected` | PASS |
| live 全 mock 无 factory 可创建 | `TestSeatModelValidate/live_all_mock_without_factory_ok` | PASS |
| mock 模式 model_ref 规范化（含持久化） | `TestSeatModelValidate/mock_mode_normalizes_refs` | PASS |
| live 全 mock 不消耗额度并跑完 | `TestSeatModelBudgetIsolation/live_all_mock_no_budget` | PASS |
| live 混合：额度==真实决策数；真实座位 source=model、mock 座位 source=mock | `TestSeatModelBudgetIsolation/live_mixed_only_real_consumes` | PASS |
| 额度耗尽、轮到真实座位 → cost_limit 暂停且无请求 | `TestSeatModelExhaustedBudget/real_seat_pauses` | PASS |
| 额度耗尽、轮到 mock 座位 → 继续决策、额度不变、source=mock | `TestSeatModelExhaustedBudget/mock_seat_continues` | PASS |
| 真实请求失败 → source=fallback 且保留已消耗额度 | `TestSeatModelFallbackKeepsBudget` | PASS |

完整输出：`--- PASS: TestSeatModelValidate (0.00s)` … `--- PASS: TestSeatModelFallbackKeepsBudget (0.00s)`；`PASS`；`ok texas-holdem/internal/service 1.248s`。

## 真实浏览器测试场景 → 结果（受控本地 provider 出站计数）

服务：真实 Go 二进制 + `allow_paid_models=true` + 目录 `["mock","local/real"]`；provider 端口 4390 只统计出站。

| 场景 | 测试函数 | 结果依据 |
|---|---|---|
| mock 模式模型字段固定不可改 + 全 mock 局出站=0 | `seat_model.spec.ts:69` | `.f-model select` 计数 0、文案「Mock（免费）」、POST body `model_ref="mock"`；`provider count == 0`、`live_calls_used == 0` |
| live 全 mock 可开局、不产生真实调用 | `seat_model.spec.ts:96` | 文案「不产生真实模型调用」、无费用确认；`provider count == 0`、`live_calls_used == 0` |
| live 混合座位仅真实座位出站并消耗额度 | `seat_model.spec.ts:119` | 选 `local/real` 后出现费用确认；`provider count > 0`、`live_calls_used > 0`；座位 ref 同时含 `local/real` 与 `mock` |
| 多浏览器 context Cookie 隔离 + 配置独立 | `seat_model.spec.ts:145` | A 创建真人局：A `GET /player` 200、B 403；B 再创建：B 200、A 403；A 保持 mock、B 切 live 互不影响 |

**免费场景出站请求数=0 的直接证据**：场景 1、2 通过 `GET http://127.0.0.1:4390/__count` 读取，断言 `count === 0`；场景 3 同一 provider 上 `count > 0`，两者对比证明「只有选了真实模型座位才发出请求」。

Playwright 结果：`4 passed (6.2s)`；`playwright: passed=4 failed=0 not_run=0 exit=0`。

## typecheck / build / vet 输出

- `npm --prefix web run typecheck`：退出码 0（无错误）。
- `npm --prefix web run build`：`tsc --noEmit && vite build` 成功，`✓ 50 modules transformed`，`✓ built`（退出码 0）。
- `go test -mod=readonly -race -count=1 ./internal/service/`：`ok texas-holdem/internal/service 6.327s`。
- `go vet ./internal/service/`：退出码 0（无输出）。

完整原始输出见本文件上方「### 命令」各段。

## 未验证边界

- **真实 provider 未联网验证**：本地 provider 只验证了请求派发、额度语义与 source 判定；未调用任何真实付费模型，未联网，未验证真实 provider 的鉴权/限流/超时/返回协议差异。
- 仅桌面 Chromium；未验证 FireFox/WebKit、真实多用户并发（后端同一时间仅允许一个非终态局，多 context 场景为串行）。
- 未验证跨进程重启后的旧记录（mock 局持久化了真实 ref 的历史数据）UI 展示；`effectiveModelRef` 以「缺失按 mock」兜底，但历史非空真实 ref 在 mock 局仍会按 mock 执行（符合统一规则）。
- 未验证 live 授权在运行中被关闭后的 `unauthorized` 暂停的浏览器级表现（Go 单测覆盖语义）。
- 未改动 `internal/poker`、`internal/data`、`internal/server`、`internal/conf`、`config`、`docs/contracts.md`；未 commit/push。

## 结论

PASS：4 个真实浏览器场景 + 10 个单测子场景 + typecheck/build/vet 全部通过；真实 provider 未联网，仅本地受控 provider 验证出站派发与额度语义。
