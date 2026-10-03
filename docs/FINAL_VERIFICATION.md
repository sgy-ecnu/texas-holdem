# 最终集成验证（2026-10-03）

> 历史端口说明：本文原始命令输出属于 HTTP 8080 的历史验证，原样保留，不代表当前
> 监听地址。当前 HTTP 已按用户要求迁移到 8081；最新操作与证据见 PORT_8081_VERIFICATION.md。

本轮按当前磁盘重建并完成两次串行集成验证，8 个命令阶段均退出 0。第二次为最终
证据：HTTP E2E 8/8、真实 Chromium UI 6/6、Go 顶层测试 62 PASS + 1 SKIP、脚本安全
32/32、开发模式只读探测 1/1。没有失败阶段；全部安全输出原样保存在下文，未截断。
浏览器真实创建/控制的牌局均为 mock，不用 fixtures 替代运行链路。

验证顺序：`make build → make migrate → make e2e → make e2e-browser → make test → make test-scripts → make probe-dev → make status`。
所有 provider 验证仅限本地 HTTP stub；未运行 dev-live，未调用真实 provider。

## contracts §7：24 场景逐项映射

### 协调者最终独立复跑

执行目录：`/Users/gaosper/Documents/mycode/texas-holdem`。

```sh
AGENT_CATALOG_PATH= go test -mod=readonly -race -count=1 ./... && go vet ./... && make test-scripts
```

退出码 0；6 个含测试的 Go 包通过 race，0 个包失败，2 个包无测试；`go vet` 无输出且退出 0；脚本测试 32 PASS、0 FAIL。以下为完整原始输出：

```text
?   	texas-holdem/cmd/server	[no test files]
ok  	texas-holdem/internal/agent	4.413s
ok  	texas-holdem/internal/conf	4.314s
ok  	texas-holdem/internal/data	4.249s
ok  	texas-holdem/internal/poker	4.939s
ok  	texas-holdem/internal/server	4.452s
ok  	texas-holdem/internal/service	4.723s
?   	texas-holdem/web	[no test files]
python3 tests/e2e/test_local.py
test_child_env_removes_admin_and_inherited_config: PASS
test_dev_catalog_allowed_paid_still_disabled: PASS
test_dev_live_only_explicit_action_changes_backend_switch: PASS
test_duplicate_rejected: PASS
test_e2e_ambient_paid_and_catalog_cannot_override: PASS
test_e2e_catalog_override_does_not_read_user_config: PASS
test_existing_runtime_permissions_not_changed: PASS
test_fallback_ports_allowed: PASS
test_identity_forged_signature_rejected: PASS
test_identity_kernel_executable_spoof_rejected: PASS
test_identity_legal_path_with_spaces_accepted: PASS
test_identity_mysql_path_suffix_rejected: PASS
test_identity_pid_birth_metadata_reused_rejected: PASS
test_identity_redis_similar_config_argument_rejected: PASS
test_identity_redis_similar_config_path_rejected: PASS
test_identity_reused_pid_real_executable_rejected: PASS
test_identity_unavailable_argv_rejected_no_signal: PASS
test_identity_zombie_is_not_live: PASS
test_json_env_roundtrip: PASS
test_mysql_password_only_in_child_environment: PASS
test_no_shell_expansion: PASS
test_nonlocal_mysql_rejected: PASS
test_nonlocal_redis_rejected: PASS
test_paid_models_rejected: PASS
test_permissions_and_symlink: PASS
test_pid_identity_mismatch_rejected: PASS
test_real_catalog_rejected: PASS
test_redaction_does_not_consume_next_test_line: PASS
test_safe_output_preserves_failure_and_redacts_credentials: PASS
test_setup_preserves_existing_env: PASS
test_stop_rechecks_identity_before_signal: PASS
test_unknown_key_rejected: PASS
scripts: tests=32 passed=32 failed=0
```

真实 provider 调用及 `dev-live` 实际启动仍未验证；上述独立复跑不改变该边界。

`make test` 实际执行 `go test -count=1 -v ./...`，无测试缓存替代本轮执行。下表 PASS
注明单元/stub/真实链路边界，不将 mocked repository 或本地协议 stub 冒充真实 provider。

| # | 契约场景 | 实际测试函数 / 路径 | 证据命令 | 结果与边界 |
|---|---|---|---|---|
| 1 | 2人与9人位置 | poker.TestBlindAndActionOrder/heads_up、nine_seats | make test | PASS：纯规则单元；盲注、翻前/翻后 actor |
| 2 | minraise | poker.TestMinimumRaise | make test | PASS：纯规则单元 |
| 3 | 短全押 | poker.TestShortAllInReopening | make test | PASS：单次/累计重开及欠注响应 |
| 4 | 禁止无对手加注 | poker.TestNoRaiseWithoutResponder | make test | PASS：纯规则单元 |
| 5 | 边池/退款 | poker.TestSidePotsAndRefunds | make test | PASS：纯规则单元 |
| 6 | 平分余数 | poker.TestSplitPotOddChip | make test | PASS：纯规则单元 |
| 7 | 牌力 | poker.TestHandRanking | make test | PASS：wheel、踢脚与平局 |
| 8 | 自动结算 | poker.TestRunoutAndUncontested | make test | PASS：跑完/立即结束 |
| 9 | 守恒 | poker.TestChipConservationRandomSequences；test_mock_multihand | make test；make e2e | PASS：单元每步守恒 + 真 MySQL 三手终局守恒 |
| 10 | 私牌隔离 | poker/agent.TestObservationPrivacy；test_public_private_isolation | make test；make e2e | PASS：单元 observation + 真实 HTTP/Redis 公共 DTO |
| 11 | loader安全 | agent.TestConfigLoaderRedaction | make test | PASS：受控测试配置/env/file；不读取真实用户配置 |
| 12 | 协议适配 | agent.TestProviderAdapters/openai、anthropic | make test | PASS：本地 HTTP stub，两类协议；非真实 provider |
| 13 | 降级 | agent/service.TestDecisionFallback | make test | PASS：超时/畸形/非法动作 check/fold，受控 decider |
| 14 | 成本门槛 | service.TestLiveGateAndBudget；test_dev_live_only_explicit_action_changes_backend_switch；浏览器 scenario 4 | make test；make test-scripts；make e2e-browser | PASS：后端单元双授权/预算；真实未授权 health/UI。费用确认分支使用拦截 health，不创建 live 局，不证明 live-provider 链路 |
| 15 | 生命周期 | service.TestPauseResumeStop；test_pause_resume_stop；浏览器 scenario 3 | make test；make e2e；make e2e-browser | PASS：受控取消/晚结果单元 + 真 HTTP/UI 控制 |
| 16 | 原子持久化 | data/service.TestRepositoryCommitAtomicity；test_mock_multihand | make test；make e2e | PASS：原子失败/CAS 为受控 repository/sql-driver 单元；真 MySQL 验证三手持久化。未额外注入真实 MySQL 事务故障 |
| 17 | 崩溃恢复 | service.TestRecoveryPausesRunningGame；test_restart_recovery | make test；make e2e | PASS：单元预算/结算 + 真进程 SIGKILL、同库 paused/recovery |
| 18 | Redis故障 | data.TestRedisDegradation；test_redis_runtime_degradation | make test；make e2e | PASS：真 Redis 前测试代理断开，MySQL 仍推进、degraded；不停止用户 Redis |
| 19 | 公共DTO | server.TestPublicDTOPrivacy、TestPublicDTOPrivacyHTTP；浏览器 scenario 6 | make test；make e2e-browser | PASS：单元公共字段/未完成409/完成揭示 + 真实 UI 无底牌/详情揭示 |
| 20 | 本机真实链路 | test_mock_multihand；浏览器 scenario 2 | make build；make migrate；make e2e；make e2e-browser | PASS：真 MySQL/Redis/Go 与 Chromium UI 连续三手至上限 |
| 21 | 存储启动失败 | test_dependency_startup_failures（mysql、redis） | make e2e | PASS：分别注入错误 loopback 端口，存储错误/非零退出/HTTP关闭，无 memory 替代；按授权不真正停用户服务 |
| 22 | 浏览器恢复 | tests/e2e/browser/tests/scenarios.spec.ts: scenario 3 · 暂停 / 刷新找 current / 恢复 / 停止 | make e2e-browser | PASS：真实 Chromium reload/current/control，不使用状态 fixture |
| 23 | 生产静态资源 | test_embedded_resources；浏览器 scenarios 1–6 | make build；make e2e；make e2e-browser | PASS：空 cwd 无 sidecar 的 Go 产物 HTTP assets + Go embedded 真实 UI；无 Vite 服务 |
| 24 | 实际provider | live_provider_smoke | 未执行 | **未验证**：未获本轮付费调用授权；本地协议 stub、模型清单及 dev-live 单元不能替代 |

## 本轮新增验证与使用边界

- `tests/e2e/run.py::test_http_aborted_polling`：150 次原始 socket 完整 GET 后立即断开，
  game/current/health 各 50 次，随后 HTTP 200、同局 worker 推进、可暂停稳定并恢复。
  **不声称每次断开都触发 SQL 取消，也不声称统计到了实际 SQL 取消次数。** 确定性
  cancel/deadline 由 data.TestRepositoryReadCancellation、TestGuardPollingCancellationDoesNotCancelLockQuery、
  service.TestReadCancellationKeepsWorker、server.TestAbortedPollingDoesNotFaultManager 等
  本轮通过的单元证明；TestRepositoryRealFailureWinsOverCancellation、
  TestReadStorageFailureStillLatches、TestHealthReadFailureStillLatches 与
  TestUncertainBudgetWriteStillFaults 证明真实依赖错误/不确定写入不被读取消绕过。
- `make dev-live` / `python3 scripts/local.py dev-live` 是**显式费用风险入口**，本轮未启动。
  先安全 load/validate 文件仍为 paid=false，之后仅将该子进程后端开关改为 true；
  不修改 `.env.local`，不改变 Web mode/budget/确认状态，不自动创建游戏或调用 provider。
  用户仍需 Web `mode=live`、正预算、勾选费用确认，且自行决定是否承担费用。
  普通 dev 不受 ambient paid=true 影响；E2E 永远 paid=false + 独立 mock catalog。
  对应单元：test_dev_live_only_explicit_action_changes_backend_switch、
  test_e2e_ambient_paid_and_catalog_cannot_override，均 PASS。
- 开发只读探测仅展示清单计数：24 个非 mock 配置引用不代表 provider 已验证可调用。
  `TestLocalCatalogModels` SKIP：未传 AGENT_CATALOG_PATH，不自动读取用户配置。普通 dev
  的服务端只读目录展示由 probe-dev 单独证明；两者都没有模型调用。
- 浏览器 6 个命名测试均运行在真实 Chromium + Go HTTP；scenario 4 的第二段只用
  health 拦截检查费用确认 UI，明确不是 live 后端/provider 验证。其余创建、控制、
  刷新、详情均用真 HTTP 和真存储，牌局 mode=mock。
- 两次串行链路均 exit=0，后一次增加日志脱敏回归，脚本计数从 31 增为 32。
  第一轮日志脱敏规则曾误匹配测试名后的换行，遮蔽非敏感 RUN 行；已收紧为单行
  字段匹配并补 test_redaction_does_not_consume_next_test_line。第二轮原始输出为最终
  证据；第一轮也保留，不用其遮蔽行作为场景证明。没有运行失败需隐去。
- npm 输出的 allow-scripts warning 已完整保留，退出 0、真实 frontend build 与 browser
  均通过；没有擅自批准额外安装脚本或安装新工具。
- 所有本轮启动的 HTTP 已退出，最终 8080 空闲；隔离 MySQL 3307 / Redis 6380 持续
  运行且身份 verified。不删库、不 flush Redis、不改 Homebrew 原数据、不 commit/push。
- 本轮未重跑 race detector、未验证真实 provider、未实际启动 dev-live，不宣称这些
  项目完成。PID 身份核验与 signal 的剩余 Darwin TOCTOU 边界仍见 LOCAL_SETUP.md。

完整复现（不含 live）：`python3 scripts/local.py verify-final`。该命令将全部安全输出
追加到本文件，严格串行，凭据/DSN 在本地脱敏后写入；失败 traceback 和服务日志
不截断。失败的前置构建/迁移/HTTP E2E 会停止后续依赖链，不自行修改 Go 或兜底。

## 本轮完整安全原始输出

### `make build`

```text
python3 scripts/local.py frontend

added 69 packages, and audited 70 packages in 6s

9 packages are looking for funding
  run `npm fund` for details

found 0 vulnerabilities
npm warn allow-scripts 2 packages have install scripts not yet covered by allowScripts:
npm warn allow-scripts   esbuild@0.28.2 (postinstall: node install.js)
npm warn allow-scripts   fsevents@2.3.3 (install: (install scripts present))
npm warn allow-scripts
npm warn allow-scripts Run `npm install-scripts ls` to review, or `npm install-scripts approve <pkg>` to allow.
stage=npm-ci; exit=0; output=not-forwarded

> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
transforming...
✓ 38 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-B41XS3CE.css   19.01 kB │ gzip:  5.41 kB
dist/assets/index-D9a1tCE2.js   253.20 kB │ gzip: 79.58 kB
✓ built in 693ms
stage=frontend-build; exit=0; output=not-forwarded
python3 scripts/local.py build

build: passed; exit=0
```

退出码：0

### `make migrate`

```text
python3 scripts/local.py migrate

migrate: passed; exit=0
```

退出码：0

### `make e2e`

```text
python3 tests/e2e/run.py all
env: present; owner=current-user; mode=600; local-only; paid-disabled
mysql: available; dedicated-user; dedicated-database
redis: available; loopback; db=15; no writes
frontend-dist: present
checks: passed=3 failed=0
mock -> test_mock_multihand: PASS
privacy -> test_public_private_isolation: PASS
controls -> test_pause_resume_stop: PASS
recovery -> test_restart_recovery: PASS
redis -> test_redis_runtime_degradation: PASS
  dependency=mysql: expected failure; HTTP closed
  dependency=redis: expected failure; HTTP closed
dependencies -> test_dependency_startup_failures: PASS
embedded -> test_embedded_resources: PASS
  abort-stress: disconnects=150; per-route=50; followup-http=200; same-game-worker=advancing
  abort-stress: SQL cancellation occurrence is nondeterministic; unit tests prove deterministic cancellation
abort -> test_http_aborted_polling: PASS
e2e: tests=8 passed=8 failed=0 blocked=0
```

退出码：0

### `make e2e-browser`

```text
python3 tests/e2e/browser/run_browser.py

Running 6 tests using 1 worker

  ✓  1 [chromium] › tests/scenarios.spec.ts:75:1 › scenario 1 · 配置 2/9 人且每座选择不同模型 (761ms)
  ✓  2 [chromium] › tests/scenarios.spec.ts:108:1 › scenario 2 · 创建/开始至 max_hands 并核对结束详情数值数组 (2.1s)
  ✓  3 [chromium] › tests/scenarios.spec.ts:142:1 › scenario 3 · 暂停 / 刷新找 current / 恢复 / 停止 (1.2s)
  ✓  4 [chromium] › tests/scenarios.spec.ts:167:1 › scenario 4 · live 禁用与费用确认门槛 (594ms)
  ✓  5 [chromium] › tests/scenarios.spec.ts:210:1 › scenario 6 · 运行中无底牌 / 完成后详情可揭示 (2.7s)
  ✓  6 [chromium] › tests/scenarios.spec.ts:251:3 › scenario 5 · 手机宽 390 可操作无遮挡 › 390 宽配置与运行控制可用 (1.0s)

  6 passed (8.9s)
browser-wrapper: passed=6 failed=0 exit=0
browser-wrapper: screenshots=/Users/gaosper/Documents/mycode/texas-holdem/web/node_modules/.cache/browser-verify/20261003-181642
browser-wrapper: note=infra run.py 'browser' 仍为占位，本目录为独立可复现入口
browser-wrapper: own HTTP process stopped; dependencies retained
```

退出码：0

### `make test`

```text
python3 scripts/local.py test
?   	texas-holdem/cmd/server	[no test files]
=== RUN   TestConfigLoaderRedaction
=== RUN   TestConfigLoaderRedaction/literal
=== RUN   TestConfigLoaderRedaction/env
=== RUN   TestConfigLoaderRedaction/file
--- PASS: TestConfigLoaderRedaction (0.00s)
    --- PASS: TestConfigLoaderRedaction/literal (0.00s)
    --- PASS: TestConfigLoaderRedaction/env (0.00s)
    --- PASS: TestConfigLoaderRedaction/file (0.00s)
=== RUN   TestJSONCLexerAndMerge
--- PASS: TestJSONCLexerAndMerge (0.00s)
=== RUN   TestModelListAndMock
--- PASS: TestModelListAndMock (0.00s)
=== RUN   TestProviderAdapters
=== RUN   TestProviderAdapters/openai
=== RUN   TestProviderAdapters/anthropic
--- PASS: TestProviderAdapters (0.00s)
    --- PASS: TestProviderAdapters/openai (0.00s)
    --- PASS: TestProviderAdapters/anthropic (0.00s)
=== RUN   TestStrictActionJSON
--- PASS: TestStrictActionJSON (0.00s)
=== RUN   TestDecisionFallback
=== RUN   TestDecisionFallback/timeout/check=true
=== RUN   TestDecisionFallback/timeout/check=false
=== RUN   TestDecisionFallback/malformed/check=true
=== RUN   TestDecisionFallback/malformed/check=false
=== RUN   TestDecisionFallback/illegal/check=true
=== RUN   TestDecisionFallback/illegal/check=false
=== RUN   TestDecisionFallback/auth/check=true
=== RUN   TestDecisionFallback/auth/check=false
=== RUN   TestDecisionFallback/oversize/check=true
=== RUN   TestDecisionFallback/oversize/check=false
=== RUN   TestDecisionFallback/protocol/check=true
=== RUN   TestDecisionFallback/protocol/check=false
--- PASS: TestDecisionFallback (0.22s)
    --- PASS: TestDecisionFallback/timeout/check=true (0.10s)
    --- PASS: TestDecisionFallback/timeout/check=false (0.10s)
    --- PASS: TestDecisionFallback/malformed/check=true (0.00s)
    --- PASS: TestDecisionFallback/malformed/check=false (0.00s)
    --- PASS: TestDecisionFallback/illegal/check=true (0.00s)
    --- PASS: TestDecisionFallback/illegal/check=false (0.00s)
    --- PASS: TestDecisionFallback/auth/check=true (0.00s)
    --- PASS: TestDecisionFallback/auth/check=false (0.00s)
    --- PASS: TestDecisionFallback/oversize/check=true (0.00s)
    --- PASS: TestDecisionFallback/oversize/check=false (0.00s)
    --- PASS: TestDecisionFallback/protocol/check=true (0.00s)
    --- PASS: TestDecisionFallback/protocol/check=false (0.00s)
=== RUN   TestObservationPrivacy
--- PASS: TestObservationPrivacy (0.00s)
=== RUN   TestAvailabilityConcurrent
--- PASS: TestAvailabilityConcurrent (0.00s)
=== RUN   TestLocalCatalogModels
    agent_test.go:363: 未提供显式只读配置路径；不自动读取用户配置
--- SKIP: TestLocalCatalogModels (0.00s)
=== RUN   TestConfigLoaderFailures
=== RUN   TestConfigLoaderFailures/duplicate-key
=== RUN   TestConfigLoaderFailures/bad-model-id
=== RUN   TestConfigLoaderFailures/unsafe-endpoint
=== RUN   TestConfigLoaderFailures/credential-newline
=== RUN   TestConfigLoaderFailures/bad-env-reference
=== RUN   TestConfigLoaderFailures/missing-file
=== RUN   TestConfigLoaderFailures/reserved-header
=== RUN   TestConfigLoaderFailures/header-collision
--- PASS: TestConfigLoaderFailures (0.00s)
    --- PASS: TestConfigLoaderFailures/duplicate-key (0.00s)
    --- PASS: TestConfigLoaderFailures/bad-model-id (0.00s)
    --- PASS: TestConfigLoaderFailures/unsafe-endpoint (0.00s)
    --- PASS: TestConfigLoaderFailures/credential-newline (0.00s)
    --- PASS: TestConfigLoaderFailures/bad-env-reference (0.00s)
    --- PASS: TestConfigLoaderFailures/missing-file (0.00s)
    --- PASS: TestConfigLoaderFailures/reserved-header (0.00s)
    --- PASS: TestConfigLoaderFailures/header-collision (0.00s)
=== RUN   TestLegalActionBoundaries
--- PASS: TestLegalActionBoundaries (0.00s)
=== RUN   TestHTTPPrivacyRedirects
--- PASS: TestHTTPPrivacyRedirects (0.00s)
=== RUN   TestCancellationAndLimits
--- PASS: TestCancellationAndLimits (0.00s)
=== RUN   TestProviderResponseErrors
=== RUN   TestProviderResponseErrors/openai
=== RUN   TestProviderResponseErrors/anthropic
--- PASS: TestProviderResponseErrors (0.00s)
    --- PASS: TestProviderResponseErrors/openai (0.00s)
    --- PASS: TestProviderResponseErrors/anthropic (0.00s)
PASS
ok  	texas-holdem/internal/agent	0.481s
=== RUN   TestDefaultCatalogDiscoveryAndLoader
=== RUN   TestDefaultCatalogDiscoveryAndLoader/json_only
=== RUN   TestDefaultCatalogDiscoveryAndLoader/jsonc_only
=== RUN   TestDefaultCatalogDiscoveryAndLoader/both
=== RUN   TestDefaultCatalogDiscoveryAndLoader/neither
--- PASS: TestDefaultCatalogDiscoveryAndLoader (0.00s)
    --- PASS: TestDefaultCatalogDiscoveryAndLoader/json_only (0.00s)
    --- PASS: TestDefaultCatalogDiscoveryAndLoader/jsonc_only (0.00s)
    --- PASS: TestDefaultCatalogDiscoveryAndLoader/both (0.00s)
    --- PASS: TestDefaultCatalogDiscoveryAndLoader/neither (0.00s)
=== RUN   TestExplicitMissingCatalogPath
=== RUN   TestExplicitMissingCatalogPath/missing.json
=== RUN   TestExplicitMissingCatalogPath/opencode.json
=== RUN   TestExplicitMissingCatalogPath/opencode.jsonc
--- PASS: TestExplicitMissingCatalogPath (0.00s)
    --- PASS: TestExplicitMissingCatalogPath/missing.json (0.00s)
    --- PASS: TestExplicitMissingCatalogPath/opencode.json (0.00s)
    --- PASS: TestExplicitMissingCatalogPath/opencode.jsonc (0.00s)
=== RUN   TestDefaultsAndSafeConfiguration
--- PASS: TestDefaultsAndSafeConfiguration (0.00s)
=== RUN   TestConfigurationErrorPriority
=== RUN   TestConfigurationErrorPriority/storage
=== RUN   TestConfigurationErrorPriority/paid_models
=== RUN   TestConfigurationErrorPriority/live_calls
=== RUN   TestConfigurationErrorPriority/redis_db
--- PASS: TestConfigurationErrorPriority (0.00s)
    --- PASS: TestConfigurationErrorPriority/storage (0.00s)
    --- PASS: TestConfigurationErrorPriority/paid_models (0.00s)
    --- PASS: TestConfigurationErrorPriority/live_calls (0.00s)
    --- PASS: TestConfigurationErrorPriority/redis_db (0.00s)
PASS
ok  	texas-holdem/internal/conf	0.472s
=== RUN   TestRepositoryReadCancellation
=== RUN   TestRepositoryReadCancellation/context_canceled/guard
=== RUN   TestRepositoryReadCancellation/context_canceled/get
=== RUN   TestRepositoryReadCancellation/context_canceled/current
=== RUN   TestRepositoryReadCancellation/context_canceled/hand
=== RUN   TestRepositoryReadCancellation/context_canceled/health_guard
=== RUN   TestRepositoryReadCancellation/context_canceled/health_mysql
=== RUN   TestRepositoryReadCancellation/context_canceled/health_redis
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/guard
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/get
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/current
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/hand
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/health_guard
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/health_mysql
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/health_redis
--- PASS: TestRepositoryReadCancellation (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/guard (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/get (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/current (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/hand (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/health_guard (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/health_mysql (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/health_redis (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/guard (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/get (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/current (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/hand (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/health_guard (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/health_mysql (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/health_redis (0.00s)
=== RUN   TestRepositoryCanceledContext
=== RUN   TestRepositoryCanceledContext/cancel
=== RUN   TestRepositoryCanceledContext/deadline
--- PASS: TestRepositoryCanceledContext (0.00s)
    --- PASS: TestRepositoryCanceledContext/cancel (0.00s)
    --- PASS: TestRepositoryCanceledContext/deadline (0.00s)
=== RUN   TestRepositoryRealFailureWinsOverCancellation
=== RUN   TestRepositoryRealFailureWinsOverCancellation/lock_lost
=== RUN   TestRepositoryRealFailureWinsOverCancellation/guard_database
=== RUN   TestRepositoryRealFailureWinsOverCancellation/load_database
=== RUN   TestRepositoryRealFailureWinsOverCancellation/mysql_then_cache_cancel
--- PASS: TestRepositoryRealFailureWinsOverCancellation (0.00s)
    --- PASS: TestRepositoryRealFailureWinsOverCancellation/lock_lost (0.00s)
    --- PASS: TestRepositoryRealFailureWinsOverCancellation/guard_database (0.00s)
    --- PASS: TestRepositoryRealFailureWinsOverCancellation/load_database (0.00s)
    --- PASS: TestRepositoryRealFailureWinsOverCancellation/mysql_then_cache_cancel (0.00s)
=== RUN   TestRepositoryWriteCancellationRemainsConservative
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_canceled/create
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_canceled/begin
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_canceled/update
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_canceled/commit
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/create
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/begin
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/update
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/commit
--- PASS: TestRepositoryWriteCancellationRemainsConservative (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_canceled/create (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_canceled/begin (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_canceled/update (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_canceled/commit (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/create (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/begin (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/update (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/commit (0.00s)
=== RUN   TestGuardPollingCancellationDoesNotCancelLockQuery
=== RUN   TestGuardPollingCancellationDoesNotCancelLockQuery/cancel
=== RUN   TestGuardPollingCancellationDoesNotCancelLockQuery/deadline
--- PASS: TestGuardPollingCancellationDoesNotCancelLockQuery (0.01s)
    --- PASS: TestGuardPollingCancellationDoesNotCancelLockQuery/cancel (0.00s)
    --- PASS: TestGuardPollingCancellationDoesNotCancelLockQuery/deadline (0.01s)
=== RUN   TestRepositoryCommitAtomicity
=== RUN   TestRepositoryCommitAtomicity/commit
=== RUN   TestRepositoryCommitAtomicity/cas_conflict
=== RUN   TestRepositoryCommitAtomicity/hand_failure
=== RUN   TestRepositoryCommitAtomicity/commit_failure
--- PASS: TestRepositoryCommitAtomicity (0.00s)
    --- PASS: TestRepositoryCommitAtomicity/commit (0.00s)
    --- PASS: TestRepositoryCommitAtomicity/cas_conflict (0.00s)
    --- PASS: TestRepositoryCommitAtomicity/hand_failure (0.00s)
    --- PASS: TestRepositoryCommitAtomicity/commit_failure (0.00s)
=== RUN   TestRedisDegradation
--- PASS: TestRedisDegradation (0.00s)
=== RUN   TestSingleInstanceLockGuard
--- PASS: TestSingleInstanceLockGuard (0.00s)
=== RUN   TestSchemaFailsClosed
--- PASS: TestSchemaFailsClosed (0.00s)
PASS
ok  	texas-holdem/internal/data	0.641s
=== RUN   TestBlindAndActionOrder
=== RUN   TestBlindAndActionOrder/heads_up
=== RUN   TestBlindAndActionOrder/nine_seats
=== RUN   TestBlindAndActionOrder/sparse_and_out_seats
--- PASS: TestBlindAndActionOrder (0.00s)
    --- PASS: TestBlindAndActionOrder/heads_up (0.00s)
    --- PASS: TestBlindAndActionOrder/nine_seats (0.00s)
    --- PASS: TestBlindAndActionOrder/sparse_and_out_seats (0.00s)
=== RUN   TestMinimumRaise
--- PASS: TestMinimumRaise (0.00s)
=== RUN   TestShortAllInReopening
=== RUN   TestShortAllInReopening/single_does_not_reopen
=== RUN   TestShortAllInReopening/cumulative_reopens
=== RUN   TestShortAllInReopening/unacted_retains_right
--- PASS: TestShortAllInReopening (0.00s)
    --- PASS: TestShortAllInReopening/single_does_not_reopen (0.00s)
    --- PASS: TestShortAllInReopening/cumulative_reopens (0.00s)
    --- PASS: TestShortAllInReopening/unacted_retains_right (0.00s)
=== RUN   TestNoRaiseWithoutResponder
--- PASS: TestNoRaiseWithoutResponder (0.00s)
=== RUN   TestBlindShortStack
--- PASS: TestBlindShortStack (0.00s)
=== RUN   TestSidePotsAndRefunds
=== RUN   TestSidePotsAndRefunds/layers_folded_money_refund
=== RUN   TestSidePotsAndRefunds/actual_action_chain
--- PASS: TestSidePotsAndRefunds (0.00s)
    --- PASS: TestSidePotsAndRefunds/layers_folded_money_refund (0.00s)
    --- PASS: TestSidePotsAndRefunds/actual_action_chain (0.00s)
=== RUN   TestSplitPotOddChip
--- PASS: TestSplitPotOddChip (0.00s)
=== RUN   TestHandRanking
--- PASS: TestHandRanking (0.00s)
=== RUN   TestRunoutAndUncontested
--- PASS: TestRunoutAndUncontested (0.00s)
=== RUN   TestChipConservationRandomSequences
--- PASS: TestChipConservationRandomSequences (0.04s)
=== RUN   TestObservationPrivacy
--- PASS: TestObservationPrivacy (0.00s)
=== RUN   TestOutputIsolation
--- PASS: TestOutputIsolation (0.00s)
=== RUN   TestConcurrentBranchIsolation
--- PASS: TestConcurrentBranchIsolation (0.00s)
=== RUN   TestInitializationValidation
=== RUN   TestInitializationValidation/one
=== RUN   TestInitializationValidation/duplicate_seat
=== RUN   TestInitializationValidation/seat_range
=== RUN   TestInitializationValidation/negative
=== RUN   TestInitializationValidation/total_limit
=== RUN   TestInitializationValidation/overflow
=== RUN   TestInitializationValidation/only_one_funded
=== RUN   TestInitializationValidation/ineligible_button
--- PASS: TestInitializationValidation (0.00s)
    --- PASS: TestInitializationValidation/one (0.00s)
    --- PASS: TestInitializationValidation/duplicate_seat (0.00s)
    --- PASS: TestInitializationValidation/seat_range (0.00s)
    --- PASS: TestInitializationValidation/negative (0.00s)
    --- PASS: TestInitializationValidation/total_limit (0.00s)
    --- PASS: TestInitializationValidation/overflow (0.00s)
    --- PASS: TestInitializationValidation/only_one_funded (0.00s)
    --- PASS: TestInitializationValidation/ineligible_button (0.00s)
=== RUN   TestActionValidation
--- PASS: TestActionValidation (0.00s)
=== RUN   TestSnapshotValidationAndResume
--- PASS: TestSnapshotValidationAndResume (0.00s)
PASS
ok  	texas-holdem/internal/poker	1.096s
=== RUN   TestPublicDTOPrivacy
--- PASS: TestPublicDTOPrivacy (0.00s)
=== RUN   TestCompletedPotSeatArrays
=== RUN   TestCompletedPotSeatArrays/nonempty
=== RUN   TestCompletedPotSeatArrays/multiple_winners
=== RUN   TestCompletedPotSeatArrays/empty
=== RUN   TestCompletedPotSeatArrays/nil
--- PASS: TestCompletedPotSeatArrays (0.00s)
    --- PASS: TestCompletedPotSeatArrays/nonempty (0.00s)
    --- PASS: TestCompletedPotSeatArrays/multiple_winners (0.00s)
    --- PASS: TestCompletedPotSeatArrays/empty (0.00s)
    --- PASS: TestCompletedPotSeatArrays/nil (0.00s)
=== RUN   TestPublicDTOPrivacyHTTP
--- PASS: TestPublicDTOPrivacyHTTP (0.00s)
=== RUN   TestModelsWrapperAndHealth
--- PASS: TestModelsWrapperAndHealth (0.00s)
=== RUN   TestCompletedPotSeatArraysHTTP
=== RUN   TestCompletedPotSeatArraysHTTP/nonempty
=== RUN   TestCompletedPotSeatArraysHTTP/multiple_winners
=== RUN   TestCompletedPotSeatArraysHTTP/empty
=== RUN   TestCompletedPotSeatArraysHTTP/nil
--- PASS: TestCompletedPotSeatArraysHTTP (0.00s)
    --- PASS: TestCompletedPotSeatArraysHTTP/nonempty (0.00s)
    --- PASS: TestCompletedPotSeatArraysHTTP/multiple_winners (0.00s)
    --- PASS: TestCompletedPotSeatArraysHTTP/empty (0.00s)
    --- PASS: TestCompletedPotSeatArraysHTTP/nil (0.00s)
=== RUN   TestInputLimitsAndOrigins
=== RUN   TestInputLimitsAndOrigins/default_mock
=== RUN   TestInputLimitsAndOrigins/same_origin
=== RUN   TestInputLimitsAndOrigins/cross_origin
=== RUN   TestInputLimitsAndOrigins/dns_rebinding
=== RUN   TestInputLimitsAndOrigins/null_origin
=== RUN   TestInputLimitsAndOrigins/fetch_cross_site
=== RUN   TestInputLimitsAndOrigins/oversized_body
=== RUN   TestInputLimitsAndOrigins/oversized_headers
=== RUN   TestInputLimitsAndOrigins/unknown_field
=== RUN   TestInputLimitsAndOrigins/missing_required
=== RUN   TestInputLimitsAndOrigins/duplicate_key
=== RUN   TestInputLimitsAndOrigins/trailing_value
=== RUN   TestInputLimitsAndOrigins/not_json
=== RUN   TestInputLimitsAndOrigins/null_mode
--- PASS: TestInputLimitsAndOrigins (0.00s)
    --- PASS: TestInputLimitsAndOrigins/default_mock (0.00s)
    --- PASS: TestInputLimitsAndOrigins/same_origin (0.00s)
    --- PASS: TestInputLimitsAndOrigins/cross_origin (0.00s)
    --- PASS: TestInputLimitsAndOrigins/dns_rebinding (0.00s)
    --- PASS: TestInputLimitsAndOrigins/null_origin (0.00s)
    --- PASS: TestInputLimitsAndOrigins/fetch_cross_site (0.00s)
    --- PASS: TestInputLimitsAndOrigins/oversized_body (0.00s)
    --- PASS: TestInputLimitsAndOrigins/oversized_headers (0.00s)
    --- PASS: TestInputLimitsAndOrigins/unknown_field (0.00s)
    --- PASS: TestInputLimitsAndOrigins/missing_required (0.00s)
    --- PASS: TestInputLimitsAndOrigins/duplicate_key (0.00s)
    --- PASS: TestInputLimitsAndOrigins/trailing_value (0.00s)
    --- PASS: TestInputLimitsAndOrigins/not_json (0.00s)
    --- PASS: TestInputLimitsAndOrigins/null_mode (0.00s)
=== RUN   TestSafeErrorsAndCurrent
--- PASS: TestSafeErrorsAndCurrent (0.00s)
=== RUN   TestKratosHTTPConfigurationAndStatic
--- PASS: TestKratosHTTPConfigurationAndStatic (0.00s)
=== RUN   TestAbortedPollingDoesNotFaultManager
=== RUN   TestAbortedPollingDoesNotFaultManager/cancel/get
=== RUN   TestAbortedPollingDoesNotFaultManager/cancel/current
=== RUN   TestAbortedPollingDoesNotFaultManager/cancel/hand
=== RUN   TestAbortedPollingDoesNotFaultManager/cancel/health
=== RUN   TestAbortedPollingDoesNotFaultManager/deadline/get
=== RUN   TestAbortedPollingDoesNotFaultManager/deadline/current
=== RUN   TestAbortedPollingDoesNotFaultManager/deadline/hand
=== RUN   TestAbortedPollingDoesNotFaultManager/deadline/health
--- PASS: TestAbortedPollingDoesNotFaultManager (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/cancel/get (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/cancel/current (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/cancel/hand (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/cancel/health (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/deadline/get (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/deadline/current (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/deadline/hand (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/deadline/health (0.00s)
PASS
ok  	texas-holdem/internal/server	0.409s
=== RUN   TestDecisionFallback
=== RUN   TestDecisionFallback/timeout/fold
=== RUN   TestDecisionFallback/timeout/check
=== RUN   TestDecisionFallback/protocol/fold
=== RUN   TestDecisionFallback/protocol/check
=== RUN   TestDecisionFallback/illegal/fold
=== RUN   TestDecisionFallback/illegal/check
--- PASS: TestDecisionFallback (0.03s)
    --- PASS: TestDecisionFallback/timeout/fold (0.01s)
    --- PASS: TestDecisionFallback/timeout/check (0.01s)
    --- PASS: TestDecisionFallback/protocol/fold (0.00s)
    --- PASS: TestDecisionFallback/protocol/check (0.00s)
    --- PASS: TestDecisionFallback/illegal/fold (0.00s)
    --- PASS: TestDecisionFallback/illegal/check (0.00s)
=== RUN   TestLiveGateAndBudget
=== RUN   TestLiveGateAndBudget/default_zero_network
=== RUN   TestLiveGateAndBudget/dual_authorization
=[凭据已脱敏]
=== RUN   TestLiveGateAndBudget/failed_call_precharged_and_limit_pauses
--- PASS: TestLiveGateAndBudget (0.01s)
    --- PASS: TestLiveGateAndBudget/default_zero_network (0.00s)
    --- PASS: TestLiveGateAndBudget/dual_authorization (0.00s)
    --- PASS: TestLiveGateAndBudget/hard_cap_and_mock_budget (0.00s)
    --- PASS: TestLiveGateAndBudget/failed_call_precharged_and_limit_pauses (0.00s)
=== RUN   TestPauseResumeStop
--- PASS: TestPauseResumeStop (0.00s)
=== RUN   TestRecoveryPausesRunningGame
--- PASS: TestRecoveryPausesRunningGame (0.00s)
=== RUN   TestRepositoryCommitAtomicity
--- PASS: TestRepositoryCommitAtomicity (0.00s)
=== RUN   TestZeroStacksAndStorageFailure
--- PASS: TestZeroStacksAndStorageFailure (0.00s)
=== RUN   TestModelErrorCodeAllowlist
--- PASS: TestModelErrorCodeAllowlist (0.00s)
=== RUN   TestReadCancellationKeepsWorker
=== RUN   TestReadCancellationKeepsWorker/cancel/get
=== RUN   TestReadCancellationKeepsWorker/cancel/current
=== RUN   TestReadCancellationKeepsWorker/cancel/hand_lookup
=== RUN   TestReadCancellationKeepsWorker/cancel/hand
=== RUN   TestReadCancellationKeepsWorker/cancel/get_health
=== RUN   TestReadCancellationKeepsWorker/cancel/health
=== RUN   TestReadCancellationKeepsWorker/deadline/get
=== RUN   TestReadCancellationKeepsWorker/deadline/current
=== RUN   TestReadCancellationKeepsWorker/deadline/hand_lookup
=== RUN   TestReadCancellationKeepsWorker/deadline/hand
=== RUN   TestReadCancellationKeepsWorker/deadline/get_health
=== RUN   TestReadCancellationKeepsWorker/deadline/health
--- PASS: TestReadCancellationKeepsWorker (0.02s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/get (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/current (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/hand_lookup (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/hand (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/get_health (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/health (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/get (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/current (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/hand_lookup (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/hand (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/get_health (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/health (0.00s)
=== RUN   TestReadStorageFailureStillLatches
=== RUN   TestReadStorageFailureStillLatches/STORAGE_UNAVAILABLE
=== RUN   TestReadStorageFailureStillLatches/TEST_DATABASE_ERROR
=== RUN   TestReadStorageFailureStillLatches/context_deadline_exceeded
--- PASS: TestReadStorageFailureStillLatches (0.00s)
    --- PASS: TestReadStorageFailureStillLatches/STORAGE_UNAVAILABLE (0.00s)
    --- PASS: TestReadStorageFailureStillLatches/TEST_DATABASE_ERROR (0.00s)
    --- PASS: TestReadStorageFailureStillLatches/context_deadline_exceeded (0.00s)
=== RUN   TestUncertainBudgetWriteStillFaults
=== RUN   TestUncertainBudgetWriteStillFaults/context_canceled
=== RUN   TestUncertainBudgetWriteStillFaults/context_deadline_exceeded
--- PASS: TestUncertainBudgetWriteStillFaults (0.00s)
    --- PASS: TestUncertainBudgetWriteStillFaults/context_canceled (0.00s)
    --- PASS: TestUncertainBudgetWriteStillFaults/context_deadline_exceeded (0.00s)
=== RUN   TestHealthReadFailureStillLatches
=== RUN   TestHealthReadFailureStillLatches/database/get_health
=== RUN   TestHealthReadFailureStillLatches/database/health
=== RUN   TestHealthReadFailureStillLatches/unmarked_database/get_health
=== RUN   TestHealthReadFailureStillLatches/unmarked_database/health
=== RUN   TestHealthReadFailureStillLatches/dependency_deadline/get_health
=== RUN   TestHealthReadFailureStillLatches/dependency_deadline/health
=== RUN   TestHealthReadFailureStillLatches/confirmed_mysql_failure_with_cancel/get_health
=== RUN   TestHealthReadFailureStillLatches/confirmed_mysql_failure_with_cancel/health
--- PASS: TestHealthReadFailureStillLatches (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/database/get_health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/database/health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/unmarked_database/get_health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/unmarked_database/health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/dependency_deadline/get_health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/dependency_deadline/health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/confirmed_mysql_failure_with_cancel/get_health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/confirmed_mysql_failure_with_cancel/health (0.00s)
PASS
ok  	texas-holdem/internal/service	0.891s
?   	texas-holdem/web	[no test files]
test: passed; exit=0
```

退出码：0

### `make test-scripts`

```text
python3 tests/e2e/test_local.py
test_child_env_removes_admin_and_inherited_config: PASS
test_dev_catalog_allowed_paid_still_disabled: PASS
test_dev_live_only_explicit_action_changes_backend_switch: PASS
test_duplicate_rejected: PASS
test_e2e_ambient_paid_and_catalog_cannot_override: PASS
test_e2e_catalog_override_does_not_read_user_config: PASS
test_existing_runtime_permissions_not_changed: PASS
test_fallback_ports_allowed: PASS
test_identity_forged_signature_rejected: PASS
test_identity_kernel_executable_spoof_rejected: PASS
test_identity_legal_path_with_spaces_accepted: PASS
test_identity_mysql_path_suffix_rejected: PASS
test_identity_pid_birth_metadata_reused_rejected: PASS
test_identity_redis_similar_config_argument_rejected: PASS
test_identity_redis_similar_config_path_rejected: PASS
test_identity_reused_pid_real_executable_rejected: PASS
test_identity_unavailable_argv_rejected_no_signal: PASS
test_identity_zombie_is_not_live: PASS
test_json_env_roundtrip: PASS
test_mysql_password_only_in_child_environment: PASS
test_no_shell_expansion: PASS
test_nonlocal_mysql_rejected: PASS
test_nonlocal_redis_rejected: PASS
test_paid_models_rejected: PASS
test_permissions_and_symlink: PASS
test_pid_identity_mismatch_rejected: PASS
test_real_catalog_rejected: PASS
test_safe_output_preserves_failure_and_redacts_credentials: PASS
test_setup_preserves_existing_env: PASS
test_stop_rechecks_identity_before_signal: PASS
test_unknown_key_rejected: PASS
scripts: tests=31 passed=31 failed=0
```

退出码：0

### `make probe-dev`

```text
python3 scripts/local.py probe-dev
dev-probe: HTTP health=200; paid-disabled; model-count=25; real-model-count=24; provider-calls=0
dev-probe: catalog=user-catalog-read-by-server
dev-probe: tests=1 passed=1 failed=0
dev-probe: own HTTP process stopped; dependencies retained
```

退出码：0

### `make status`

```text
python3 scripts/local.py status
runtime: mysql; identity=verified; port=3307; open=True
runtime: redis; identity=verified; port=6380; open=True
```

退出码：0

最终 HTTP 8080 释放：True

## 本轮完整安全原始输出

### `make build`

```text
python3 scripts/local.py frontend

added 69 packages, and audited 70 packages in 6s

9 packages are looking for funding
  run `npm fund` for details

found 0 vulnerabilities
npm warn allow-scripts 2 packages have install scripts not yet covered by allowScripts:
npm warn allow-scripts   esbuild@0.28.2 (postinstall: node install.js)
npm warn allow-scripts   fsevents@2.3.3 (install: (install scripts present))
npm warn allow-scripts
npm warn allow-scripts Run `npm install-scripts ls` to review, or `npm install-scripts approve <pkg>` to allow.
stage=npm-ci; exit=0; output=credential-redacted-only

> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
transforming...
✓ 38 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-B41XS3CE.css   19.01 kB │ gzip:  5.41 kB
dist/assets/index-D9a1tCE2.js   253.20 kB │ gzip: 79.58 kB
✓ built in 672ms
stage=frontend-build; exit=0; output=credential-redacted-only
python3 scripts/local.py build

build: passed; exit=0
```

退出码：0

### `make migrate`

```text
python3 scripts/local.py migrate

migrate: passed; exit=0
```

退出码：0

### `make e2e`

```text
python3 tests/e2e/run.py all
env: present; owner=current-user; mode=600; local-only; paid-disabled
mysql: available; dedicated-user; dedicated-database
redis: available; loopback; db=15; no writes
frontend-dist: present
checks: passed=3 failed=0
mock -> test_mock_multihand: PASS
privacy -> test_public_private_isolation: PASS
controls -> test_pause_resume_stop: PASS
recovery -> test_restart_recovery: PASS
redis -> test_redis_runtime_degradation: PASS
  dependency=mysql: expected failure; HTTP closed
  dependency=redis: expected failure; HTTP closed
dependencies -> test_dependency_startup_failures: PASS
embedded -> test_embedded_resources: PASS
  abort-stress: disconnects=150; per-route=50; followup-http=200; same-game-worker=advancing
  abort-stress: SQL cancellation occurrence is nondeterministic; unit tests prove deterministic cancellation
abort -> test_http_aborted_polling: PASS
e2e: tests=8 passed=8 failed=0 blocked=0
```

退出码：0

### `make e2e-browser`

```text
python3 tests/e2e/browser/run_browser.py

Running 6 tests using 1 worker

  ✓  1 [chromium] › tests/scenarios.spec.ts:75:1 › scenario 1 · 配置 2/9 人且每座选择不同模型 (682ms)
  ✓  2 [chromium] › tests/scenarios.spec.ts:108:1 › scenario 2 · 创建/开始至 max_hands 并核对结束详情数值数组 (2.0s)
  ✓  3 [chromium] › tests/scenarios.spec.ts:142:1 › scenario 3 · 暂停 / 刷新找 current / 恢复 / 停止 (1.3s)
  ✓  4 [chromium] › tests/scenarios.spec.ts:167:1 › scenario 4 · live 禁用与费用确认门槛 (644ms)
  ✓  5 [chromium] › tests/scenarios.spec.ts:210:1 › scenario 6 · 运行中无底牌 / 完成后详情可揭示 (3.0s)
  ✓  6 [chromium] › tests/scenarios.spec.ts:251:3 › scenario 5 · 手机宽 390 可操作无遮挡 › 390 宽配置与运行控制可用 (753ms)

  6 passed (9.0s)
browser-wrapper: passed=6 failed=0 exit=0
browser-wrapper: screenshots=/Users/gaosper/Documents/mycode/texas-holdem/web/node_modules/.cache/browser-verify/20261003-181832
browser-wrapper: note=infra run.py 'browser' 仍为占位，本目录为独立可复现入口
browser-wrapper: own HTTP process stopped; dependencies retained
```

退出码：0

### `make test`

```text
python3 scripts/local.py test
?   	texas-holdem/cmd/server	[no test files]
=== RUN   TestConfigLoaderRedaction
=== RUN   TestConfigLoaderRedaction/literal
=== RUN   TestConfigLoaderRedaction/env
=== RUN   TestConfigLoaderRedaction/file
--- PASS: TestConfigLoaderRedaction (0.00s)
    --- PASS: TestConfigLoaderRedaction/literal (0.00s)
    --- PASS: TestConfigLoaderRedaction/env (0.00s)
    --- PASS: TestConfigLoaderRedaction/file (0.00s)
=== RUN   TestJSONCLexerAndMerge
--- PASS: TestJSONCLexerAndMerge (0.00s)
=== RUN   TestModelListAndMock
--- PASS: TestModelListAndMock (0.00s)
=== RUN   TestProviderAdapters
=== RUN   TestProviderAdapters/openai
=== RUN   TestProviderAdapters/anthropic
--- PASS: TestProviderAdapters (0.00s)
    --- PASS: TestProviderAdapters/openai (0.00s)
    --- PASS: TestProviderAdapters/anthropic (0.00s)
=== RUN   TestStrictActionJSON
--- PASS: TestStrictActionJSON (0.00s)
=== RUN   TestDecisionFallback
=== RUN   TestDecisionFallback/timeout/check=true
=== RUN   TestDecisionFallback/timeout/check=false
=== RUN   TestDecisionFallback/malformed/check=true
=== RUN   TestDecisionFallback/malformed/check=false
=== RUN   TestDecisionFallback/illegal/check=true
=== RUN   TestDecisionFallback/illegal/check=false
=== RUN   TestDecisionFallback/auth/check=true
=== RUN   TestDecisionFallback/auth/check=false
=== RUN   TestDecisionFallback/oversize/check=true
=== RUN   TestDecisionFallback/oversize/check=false
=== RUN   TestDecisionFallback/protocol/check=true
=== RUN   TestDecisionFallback/protocol/check=false
--- PASS: TestDecisionFallback (0.21s)
    --- PASS: TestDecisionFallback/timeout/check=true (0.10s)
    --- PASS: TestDecisionFallback/timeout/check=false (0.10s)
    --- PASS: TestDecisionFallback/malformed/check=true (0.00s)
    --- PASS: TestDecisionFallback/malformed/check=false (0.00s)
    --- PASS: TestDecisionFallback/illegal/check=true (0.00s)
    --- PASS: TestDecisionFallback/illegal/check=false (0.00s)
    --- PASS: TestDecisionFallback/auth/check=true (0.00s)
    --- PASS: TestDecisionFallback/auth/check=false (0.00s)
    --- PASS: TestDecisionFallback/oversize/check=true (0.00s)
    --- PASS: TestDecisionFallback/oversize/check=false (0.00s)
    --- PASS: TestDecisionFallback/protocol/check=true (0.00s)
    --- PASS: TestDecisionFallback/protocol/check=false (0.00s)
=== RUN   TestObservationPrivacy
--- PASS: TestObservationPrivacy (0.00s)
=== RUN   TestAvailabilityConcurrent
--- PASS: TestAvailabilityConcurrent (0.00s)
=== RUN   TestLocalCatalogModels
    agent_test.go:363: 未提供显式只读配置路径；不自动读取用户配置
--- SKIP: TestLocalCatalogModels (0.00s)
=== RUN   TestConfigLoaderFailures
=== RUN   TestConfigLoaderFailures/duplicate-key
=== RUN   TestConfigLoaderFailures/bad-model-id
=== RUN   TestConfigLoaderFailures/unsafe-endpoint
=== RUN   TestConfigLoaderFailures/credential-newline
=== RUN   TestConfigLoaderFailures/bad-env-reference
=== RUN   TestConfigLoaderFailures/missing-file
=== RUN   TestConfigLoaderFailures/reserved-header
=== RUN   TestConfigLoaderFailures/header-collision
--- PASS: TestConfigLoaderFailures (0.00s)
    --- PASS: TestConfigLoaderFailures/duplicate-key (0.00s)
    --- PASS: TestConfigLoaderFailures/bad-model-id (0.00s)
    --- PASS: TestConfigLoaderFailures/unsafe-endpoint (0.00s)
    --- PASS: TestConfigLoaderFailures/credential-newline (0.00s)
    --- PASS: TestConfigLoaderFailures/bad-env-reference (0.00s)
    --- PASS: TestConfigLoaderFailures/missing-file (0.00s)
    --- PASS: TestConfigLoaderFailures/reserved-header (0.00s)
    --- PASS: TestConfigLoaderFailures/header-collision (0.00s)
=== RUN   TestLegalActionBoundaries
--- PASS: TestLegalActionBoundaries (0.00s)
=== RUN   TestHTTPPrivacyRedirects
--- PASS: TestHTTPPrivacyRedirects (0.00s)
=== RUN   TestCancellationAndLimits
--- PASS: TestCancellationAndLimits (0.00s)
=== RUN   TestProviderResponseErrors
=== RUN   TestProviderResponseErrors/openai
=== RUN   TestProviderResponseErrors/anthropic
--- PASS: TestProviderResponseErrors (0.00s)
    --- PASS: TestProviderResponseErrors/openai (0.00s)
    --- PASS: TestProviderResponseErrors/anthropic (0.00s)
PASS
ok  	texas-holdem/internal/agent	0.427s
=== RUN   TestDefaultCatalogDiscoveryAndLoader
=== RUN   TestDefaultCatalogDiscoveryAndLoader/json_only
=== RUN   TestDefaultCatalogDiscoveryAndLoader/jsonc_only
=== RUN   TestDefaultCatalogDiscoveryAndLoader/both
=== RUN   TestDefaultCatalogDiscoveryAndLoader/neither
--- PASS: TestDefaultCatalogDiscoveryAndLoader (0.00s)
    --- PASS: TestDefaultCatalogDiscoveryAndLoader/json_only (0.00s)
    --- PASS: TestDefaultCatalogDiscoveryAndLoader/jsonc_only (0.00s)
    --- PASS: TestDefaultCatalogDiscoveryAndLoader/both (0.00s)
    --- PASS: TestDefaultCatalogDiscoveryAndLoader/neither (0.00s)
=== RUN   TestExplicitMissingCatalogPath
=== RUN   TestExplicitMissingCatalogPath/missing.json
=== RUN   TestExplicitMissingCatalogPath/opencode.json
=== RUN   TestExplicitMissingCatalogPath/opencode.jsonc
--- PASS: TestExplicitMissingCatalogPath (0.00s)
    --- PASS: TestExplicitMissingCatalogPath/missing.json (0.00s)
    --- PASS: TestExplicitMissingCatalogPath/opencode.json (0.00s)
    --- PASS: TestExplicitMissingCatalogPath/opencode.jsonc (0.00s)
=== RUN   TestDefaultsAndSafeConfiguration
--- PASS: TestDefaultsAndSafeConfiguration (0.00s)
=== RUN   TestConfigurationErrorPriority
=== RUN   TestConfigurationErrorPriority/storage
=== RUN   TestConfigurationErrorPriority/paid_models
=== RUN   TestConfigurationErrorPriority/live_calls
=== RUN   TestConfigurationErrorPriority/redis_db
--- PASS: TestConfigurationErrorPriority (0.00s)
    --- PASS: TestConfigurationErrorPriority/storage (0.00s)
    --- PASS: TestConfigurationErrorPriority/paid_models (0.00s)
    --- PASS: TestConfigurationErrorPriority/live_calls (0.00s)
    --- PASS: TestConfigurationErrorPriority/redis_db (0.00s)
PASS
ok  	texas-holdem/internal/conf	0.359s
=== RUN   TestRepositoryReadCancellation
=== RUN   TestRepositoryReadCancellation/context_canceled/guard
=== RUN   TestRepositoryReadCancellation/context_canceled/get
=== RUN   TestRepositoryReadCancellation/context_canceled/current
=== RUN   TestRepositoryReadCancellation/context_canceled/hand
=== RUN   TestRepositoryReadCancellation/context_canceled/health_guard
=== RUN   TestRepositoryReadCancellation/context_canceled/health_mysql
=== RUN   TestRepositoryReadCancellation/context_canceled/health_redis
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/guard
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/get
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/current
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/hand
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/health_guard
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/health_mysql
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/health_redis
--- PASS: TestRepositoryReadCancellation (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/guard (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/get (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/current (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/hand (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/health_guard (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/health_mysql (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/health_redis (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/guard (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/get (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/current (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/hand (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/health_guard (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/health_mysql (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/health_redis (0.00s)
=== RUN   TestRepositoryCanceledContext
=== RUN   TestRepositoryCanceledContext/cancel
=== RUN   TestRepositoryCanceledContext/deadline
--- PASS: TestRepositoryCanceledContext (0.00s)
    --- PASS: TestRepositoryCanceledContext/cancel (0.00s)
    --- PASS: TestRepositoryCanceledContext/deadline (0.00s)
=== RUN   TestRepositoryRealFailureWinsOverCancellation
=== RUN   TestRepositoryRealFailureWinsOverCancellation/lock_lost
=== RUN   TestRepositoryRealFailureWinsOverCancellation/guard_database
=== RUN   TestRepositoryRealFailureWinsOverCancellation/load_database
=== RUN   TestRepositoryRealFailureWinsOverCancellation/mysql_then_cache_cancel
--- PASS: TestRepositoryRealFailureWinsOverCancellation (0.00s)
    --- PASS: TestRepositoryRealFailureWinsOverCancellation/lock_lost (0.00s)
    --- PASS: TestRepositoryRealFailureWinsOverCancellation/guard_database (0.00s)
    --- PASS: TestRepositoryRealFailureWinsOverCancellation/load_database (0.00s)
    --- PASS: TestRepositoryRealFailureWinsOverCancellation/mysql_then_cache_cancel (0.00s)
=== RUN   TestRepositoryWriteCancellationRemainsConservative
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_canceled/create
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_canceled/begin
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_canceled/update
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_canceled/commit
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/create
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/begin
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/update
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/commit
--- PASS: TestRepositoryWriteCancellationRemainsConservative (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_canceled/create (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_canceled/begin (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_canceled/update (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_canceled/commit (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/create (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/begin (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/update (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/commit (0.00s)
=== RUN   TestGuardPollingCancellationDoesNotCancelLockQuery
=== RUN   TestGuardPollingCancellationDoesNotCancelLockQuery/cancel
=== RUN   TestGuardPollingCancellationDoesNotCancelLockQuery/deadline
--- PASS: TestGuardPollingCancellationDoesNotCancelLockQuery (0.01s)
    --- PASS: TestGuardPollingCancellationDoesNotCancelLockQuery/cancel (0.00s)
    --- PASS: TestGuardPollingCancellationDoesNotCancelLockQuery/deadline (0.01s)
=== RUN   TestRepositoryCommitAtomicity
=== RUN   TestRepositoryCommitAtomicity/commit
=== RUN   TestRepositoryCommitAtomicity/cas_conflict
=== RUN   TestRepositoryCommitAtomicity/hand_failure
=== RUN   TestRepositoryCommitAtomicity/commit_failure
--- PASS: TestRepositoryCommitAtomicity (0.00s)
    --- PASS: TestRepositoryCommitAtomicity/commit (0.00s)
    --- PASS: TestRepositoryCommitAtomicity/cas_conflict (0.00s)
    --- PASS: TestRepositoryCommitAtomicity/hand_failure (0.00s)
    --- PASS: TestRepositoryCommitAtomicity/commit_failure (0.00s)
=== RUN   TestRedisDegradation
--- PASS: TestRedisDegradation (0.00s)
=== RUN   TestSingleInstanceLockGuard
--- PASS: TestSingleInstanceLockGuard (0.00s)
=== RUN   TestSchemaFailsClosed
--- PASS: TestSchemaFailsClosed (0.00s)
PASS
ok  	texas-holdem/internal/data	0.342s
=== RUN   TestBlindAndActionOrder
=== RUN   TestBlindAndActionOrder/heads_up
=== RUN   TestBlindAndActionOrder/nine_seats
=== RUN   TestBlindAndActionOrder/sparse_and_out_seats
--- PASS: TestBlindAndActionOrder (0.00s)
    --- PASS: TestBlindAndActionOrder/heads_up (0.00s)
    --- PASS: TestBlindAndActionOrder/nine_seats (0.00s)
    --- PASS: TestBlindAndActionOrder/sparse_and_out_seats (0.00s)
=== RUN   TestMinimumRaise
--- PASS: TestMinimumRaise (0.00s)
=== RUN   TestShortAllInReopening
=== RUN   TestShortAllInReopening/single_does_not_reopen
=== RUN   TestShortAllInReopening/cumulative_reopens
=== RUN   TestShortAllInReopening/unacted_retains_right
--- PASS: TestShortAllInReopening (0.00s)
    --- PASS: TestShortAllInReopening/single_does_not_reopen (0.00s)
    --- PASS: TestShortAllInReopening/cumulative_reopens (0.00s)
    --- PASS: TestShortAllInReopening/unacted_retains_right (0.00s)
=== RUN   TestNoRaiseWithoutResponder
--- PASS: TestNoRaiseWithoutResponder (0.00s)
=== RUN   TestBlindShortStack
--- PASS: TestBlindShortStack (0.00s)
=== RUN   TestSidePotsAndRefunds
=== RUN   TestSidePotsAndRefunds/layers_folded_money_refund
=== RUN   TestSidePotsAndRefunds/actual_action_chain
--- PASS: TestSidePotsAndRefunds (0.00s)
    --- PASS: TestSidePotsAndRefunds/layers_folded_money_refund (0.00s)
    --- PASS: TestSidePotsAndRefunds/actual_action_chain (0.00s)
=== RUN   TestSplitPotOddChip
--- PASS: TestSplitPotOddChip (0.00s)
=== RUN   TestHandRanking
--- PASS: TestHandRanking (0.00s)
=== RUN   TestRunoutAndUncontested
--- PASS: TestRunoutAndUncontested (0.00s)
=== RUN   TestChipConservationRandomSequences
--- PASS: TestChipConservationRandomSequences (0.03s)
=== RUN   TestObservationPrivacy
--- PASS: TestObservationPrivacy (0.00s)
=== RUN   TestOutputIsolation
--- PASS: TestOutputIsolation (0.00s)
=== RUN   TestConcurrentBranchIsolation
--- PASS: TestConcurrentBranchIsolation (0.00s)
=== RUN   TestInitializationValidation
=== RUN   TestInitializationValidation/one
=== RUN   TestInitializationValidation/duplicate_seat
=== RUN   TestInitializationValidation/seat_range
=== RUN   TestInitializationValidation/negative
=== RUN   TestInitializationValidation/total_limit
=== RUN   TestInitializationValidation/overflow
=== RUN   TestInitializationValidation/only_one_funded
=== RUN   TestInitializationValidation/ineligible_button
--- PASS: TestInitializationValidation (0.00s)
    --- PASS: TestInitializationValidation/one (0.00s)
    --- PASS: TestInitializationValidation/duplicate_seat (0.00s)
    --- PASS: TestInitializationValidation/seat_range (0.00s)
    --- PASS: TestInitializationValidation/negative (0.00s)
    --- PASS: TestInitializationValidation/total_limit (0.00s)
    --- PASS: TestInitializationValidation/overflow (0.00s)
    --- PASS: TestInitializationValidation/only_one_funded (0.00s)
    --- PASS: TestInitializationValidation/ineligible_button (0.00s)
=== RUN   TestActionValidation
--- PASS: TestActionValidation (0.00s)
=== RUN   TestSnapshotValidationAndResume
--- PASS: TestSnapshotValidationAndResume (0.00s)
PASS
ok  	texas-holdem/internal/poker	0.505s
=== RUN   TestPublicDTOPrivacy
--- PASS: TestPublicDTOPrivacy (0.00s)
=== RUN   TestCompletedPotSeatArrays
=== RUN   TestCompletedPotSeatArrays/nonempty
=== RUN   TestCompletedPotSeatArrays/multiple_winners
=== RUN   TestCompletedPotSeatArrays/empty
=== RUN   TestCompletedPotSeatArrays/nil
--- PASS: TestCompletedPotSeatArrays (0.00s)
    --- PASS: TestCompletedPotSeatArrays/nonempty (0.00s)
    --- PASS: TestCompletedPotSeatArrays/multiple_winners (0.00s)
    --- PASS: TestCompletedPotSeatArrays/empty (0.00s)
    --- PASS: TestCompletedPotSeatArrays/nil (0.00s)
=== RUN   TestPublicDTOPrivacyHTTP
--- PASS: TestPublicDTOPrivacyHTTP (0.00s)
=== RUN   TestModelsWrapperAndHealth
--- PASS: TestModelsWrapperAndHealth (0.00s)
=== RUN   TestCompletedPotSeatArraysHTTP
=== RUN   TestCompletedPotSeatArraysHTTP/nonempty
=== RUN   TestCompletedPotSeatArraysHTTP/multiple_winners
=== RUN   TestCompletedPotSeatArraysHTTP/empty
=== RUN   TestCompletedPotSeatArraysHTTP/nil
--- PASS: TestCompletedPotSeatArraysHTTP (0.00s)
    --- PASS: TestCompletedPotSeatArraysHTTP/nonempty (0.00s)
    --- PASS: TestCompletedPotSeatArraysHTTP/multiple_winners (0.00s)
    --- PASS: TestCompletedPotSeatArraysHTTP/empty (0.00s)
    --- PASS: TestCompletedPotSeatArraysHTTP/nil (0.00s)
=== RUN   TestInputLimitsAndOrigins
=== RUN   TestInputLimitsAndOrigins/default_mock
=== RUN   TestInputLimitsAndOrigins/same_origin
=== RUN   TestInputLimitsAndOrigins/cross_origin
=== RUN   TestInputLimitsAndOrigins/dns_rebinding
=== RUN   TestInputLimitsAndOrigins/null_origin
=== RUN   TestInputLimitsAndOrigins/fetch_cross_site
=== RUN   TestInputLimitsAndOrigins/oversized_body
=== RUN   TestInputLimitsAndOrigins/oversized_headers
=== RUN   TestInputLimitsAndOrigins/unknown_field
=== RUN   TestInputLimitsAndOrigins/missing_required
=== RUN   TestInputLimitsAndOrigins/duplicate_key
=== RUN   TestInputLimitsAndOrigins/trailing_value
=== RUN   TestInputLimitsAndOrigins/not_json
=== RUN   TestInputLimitsAndOrigins/null_mode
--- PASS: TestInputLimitsAndOrigins (0.00s)
    --- PASS: TestInputLimitsAndOrigins/default_mock (0.00s)
    --- PASS: TestInputLimitsAndOrigins/same_origin (0.00s)
    --- PASS: TestInputLimitsAndOrigins/cross_origin (0.00s)
    --- PASS: TestInputLimitsAndOrigins/dns_rebinding (0.00s)
    --- PASS: TestInputLimitsAndOrigins/null_origin (0.00s)
    --- PASS: TestInputLimitsAndOrigins/fetch_cross_site (0.00s)
    --- PASS: TestInputLimitsAndOrigins/oversized_body (0.00s)
    --- PASS: TestInputLimitsAndOrigins/oversized_headers (0.00s)
    --- PASS: TestInputLimitsAndOrigins/unknown_field (0.00s)
    --- PASS: TestInputLimitsAndOrigins/missing_required (0.00s)
    --- PASS: TestInputLimitsAndOrigins/duplicate_key (0.00s)
    --- PASS: TestInputLimitsAndOrigins/trailing_value (0.00s)
    --- PASS: TestInputLimitsAndOrigins/not_json (0.00s)
    --- PASS: TestInputLimitsAndOrigins/null_mode (0.00s)
=== RUN   TestSafeErrorsAndCurrent
--- PASS: TestSafeErrorsAndCurrent (0.00s)
=== RUN   TestKratosHTTPConfigurationAndStatic
--- PASS: TestKratosHTTPConfigurationAndStatic (0.00s)
=== RUN   TestAbortedPollingDoesNotFaultManager
=== RUN   TestAbortedPollingDoesNotFaultManager/cancel/get
=== RUN   TestAbortedPollingDoesNotFaultManager/cancel/current
=== RUN   TestAbortedPollingDoesNotFaultManager/cancel/hand
=== RUN   TestAbortedPollingDoesNotFaultManager/cancel/health
=== RUN   TestAbortedPollingDoesNotFaultManager/deadline/get
=== RUN   TestAbortedPollingDoesNotFaultManager/deadline/current
=== RUN   TestAbortedPollingDoesNotFaultManager/deadline/hand
=== RUN   TestAbortedPollingDoesNotFaultManager/deadline/health
--- PASS: TestAbortedPollingDoesNotFaultManager (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/cancel/get (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/cancel/current (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/cancel/hand (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/cancel/health (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/deadline/get (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/deadline/current (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/deadline/hand (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/deadline/health (0.00s)
PASS
ok  	texas-holdem/internal/server	0.646s
=== RUN   TestDecisionFallback
=== RUN   TestDecisionFallback/timeout/fold
=== RUN   TestDecisionFallback/timeout/check
=== RUN   TestDecisionFallback/protocol/fold
=== RUN   TestDecisionFallback/protocol/check
=== RUN   TestDecisionFallback/illegal/fold
=== RUN   TestDecisionFallback/illegal/check
--- PASS: TestDecisionFallback (0.03s)
    --- PASS: TestDecisionFallback/timeout/fold (0.01s)
    --- PASS: TestDecisionFallback/timeout/check (0.01s)
    --- PASS: TestDecisionFallback/protocol/fold (0.00s)
    --- PASS: TestDecisionFallback/protocol/check (0.00s)
    --- PASS: TestDecisionFallback/illegal/fold (0.00s)
    --- PASS: TestDecisionFallback/illegal/check (0.00s)
=== RUN   TestLiveGateAndBudget
=== RUN   TestLiveGateAndBudget/default_zero_network
=== RUN   TestLiveGateAndBudget/dual_authorization
=== RUN   TestLiveGateAndBudget/hard_cap_and_mock_budget
=== RUN   TestLiveGateAndBudget/failed_call_precharged_and_limit_pauses
--- PASS: TestLiveGateAndBudget (0.01s)
    --- PASS: TestLiveGateAndBudget/default_zero_network (0.01s)
    --- PASS: TestLiveGateAndBudget/dual_authorization (0.00s)
    --- PASS: TestLiveGateAndBudget/hard_cap_and_mock_budget (0.00s)
    --- PASS: TestLiveGateAndBudget/failed_call_precharged_and_limit_pauses (0.00s)
=== RUN   TestPauseResumeStop
--- PASS: TestPauseResumeStop (0.00s)
=== RUN   TestRecoveryPausesRunningGame
--- PASS: TestRecoveryPausesRunningGame (0.00s)
=== RUN   TestRepositoryCommitAtomicity
--- PASS: TestRepositoryCommitAtomicity (0.00s)
=== RUN   TestZeroStacksAndStorageFailure
--- PASS: TestZeroStacksAndStorageFailure (0.00s)
=== RUN   TestModelErrorCodeAllowlist
--- PASS: TestModelErrorCodeAllowlist (0.00s)
=== RUN   TestReadCancellationKeepsWorker
=== RUN   TestReadCancellationKeepsWorker/cancel/get
=== RUN   TestReadCancellationKeepsWorker/cancel/current
=== RUN   TestReadCancellationKeepsWorker/cancel/hand_lookup
=== RUN   TestReadCancellationKeepsWorker/cancel/hand
=== RUN   TestReadCancellationKeepsWorker/cancel/get_health
=== RUN   TestReadCancellationKeepsWorker/cancel/health
=== RUN   TestReadCancellationKeepsWorker/deadline/get
=== RUN   TestReadCancellationKeepsWorker/deadline/current
=== RUN   TestReadCancellationKeepsWorker/deadline/hand_lookup
=== RUN   TestReadCancellationKeepsWorker/deadline/hand
=== RUN   TestReadCancellationKeepsWorker/deadline/get_health
=== RUN   TestReadCancellationKeepsWorker/deadline/health
--- PASS: TestReadCancellationKeepsWorker (0.02s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/get (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/current (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/hand_lookup (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/hand (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/get_health (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/health (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/get (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/current (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/hand_lookup (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/hand (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/get_health (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/health (0.00s)
=== RUN   TestReadStorageFailureStillLatches
=== RUN   TestReadStorageFailureStillLatches/STORAGE_UNAVAILABLE
=== RUN   TestReadStorageFailureStillLatches/TEST_DATABASE_ERROR
=== RUN   TestReadStorageFailureStillLatches/context_deadline_exceeded
--- PASS: TestReadStorageFailureStillLatches (0.00s)
    --- PASS: TestReadStorageFailureStillLatches/STORAGE_UNAVAILABLE (0.00s)
    --- PASS: TestReadStorageFailureStillLatches/TEST_DATABASE_ERROR (0.00s)
    --- PASS: TestReadStorageFailureStillLatches/context_deadline_exceeded (0.00s)
=== RUN   TestUncertainBudgetWriteStillFaults
=== RUN   TestUncertainBudgetWriteStillFaults/context_canceled
=== RUN   TestUncertainBudgetWriteStillFaults/context_deadline_exceeded
--- PASS: TestUncertainBudgetWriteStillFaults (0.00s)
    --- PASS: TestUncertainBudgetWriteStillFaults/context_canceled (0.00s)
    --- PASS: TestUncertainBudgetWriteStillFaults/context_deadline_exceeded (0.00s)
=== RUN   TestHealthReadFailureStillLatches
=== RUN   TestHealthReadFailureStillLatches/database/get_health
=== RUN   TestHealthReadFailureStillLatches/database/health
=== RUN   TestHealthReadFailureStillLatches/unmarked_database/get_health
=== RUN   TestHealthReadFailureStillLatches/unmarked_database/health
=== RUN   TestHealthReadFailureStillLatches/dependency_deadline/get_health
=== RUN   TestHealthReadFailureStillLatches/dependency_deadline/health
=== RUN   TestHealthReadFailureStillLatches/confirmed_mysql_failure_with_cancel/get_health
=== RUN   TestHealthReadFailureStillLatches/confirmed_mysql_failure_with_cancel/health
--- PASS: TestHealthReadFailureStillLatches (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/database/get_health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/database/health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/unmarked_database/get_health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/unmarked_database/health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/dependency_deadline/get_health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/dependency_deadline/health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/confirmed_mysql_failure_with_cancel/get_health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/confirmed_mysql_failure_with_cancel/health (0.00s)
PASS
ok  	texas-holdem/internal/service	0.853s
?   	texas-holdem/web	[no test files]
test: passed; exit=0
```

退出码：0

### `make test-scripts`

```text
python3 tests/e2e/test_local.py
test_child_env_removes_admin_and_inherited_config: PASS
test_dev_catalog_allowed_paid_still_disabled: PASS
test_dev_live_only_explicit_action_changes_backend_switch: PASS
test_duplicate_rejected: PASS
test_e2e_ambient_paid_and_catalog_cannot_override: PASS
test_e2e_catalog_override_does_not_read_user_config: PASS
test_existing_runtime_permissions_not_changed: PASS
test_fallback_ports_allowed: PASS
test_identity_forged_signature_rejected: PASS
test_identity_kernel_executable_spoof_rejected: PASS
test_identity_legal_path_with_spaces_accepted: PASS
test_identity_mysql_path_suffix_rejected: PASS
test_identity_pid_birth_metadata_reused_rejected: PASS
test_identity_redis_similar_config_argument_rejected: PASS
test_identity_redis_similar_config_path_rejected: PASS
test_identity_reused_pid_real_executable_rejected: PASS
test_identity_unavailable_argv_rejected_no_signal: PASS
test_identity_zombie_is_not_live: PASS
test_json_env_roundtrip: PASS
test_mysql_password_only_in_child_environment: PASS
test_no_shell_expansion: PASS
test_nonlocal_mysql_rejected: PASS
test_nonlocal_redis_rejected: PASS
test_paid_models_rejected: PASS
test_permissions_and_symlink: PASS
test_pid_identity_mismatch_rejected: PASS
test_real_catalog_rejected: PASS
test_redaction_does_not_consume_next_test_line: PASS
test_safe_output_preserves_failure_and_redacts_credentials: PASS
test_setup_preserves_existing_env: PASS
test_stop_rechecks_identity_before_signal: PASS
test_unknown_key_rejected: PASS
scripts: tests=32 passed=32 failed=0
```

退出码：0

### `make probe-dev`

```text
python3 scripts/local.py probe-dev
dev-probe: HTTP health=200; paid-disabled; model-count=25; real-model-count=24; provider-calls=0
dev-probe: catalog=user-catalog-read-by-server
dev-probe: tests=1 passed=1 failed=0
dev-probe: own HTTP process stopped; dependencies retained
```

退出码：0

### `make status`

```text
python3 scripts/local.py status
runtime: mysql; identity=verified; port=3307; open=True
runtime: redis; identity=verified; port=6380; open=True
```

退出码：0

最终 HTTP 8080 释放：True
