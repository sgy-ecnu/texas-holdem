# 配置重构验证：默认 8081 与仅改 YAML 端口 8083

> 本文件由 `python3 scripts/local.py verify-config-refactor`（历史别名
> `verify-port8081` / `verify-final`）追加真实原始输出。历史文档
> `PORT_8081_VERIFICATION.md`、`FINAL_VERIFICATION.md` 的 raw 输出不改写。

## 目标

仅通过修改 YAML 的 `server.http.port`（不改任何代码），让 Go 服务、Vite dev proxy、
E2E 与浏览器全部自动派生到新端口并保持一致；地址/端口的唯一来源是冻结 CLI
`go run ./cmd/config` 的严格 5 字段公共 JSON。

## 场景映射

| 场景 | 目标 / 命令 | 断言 | 来源 |
|---|---|---|---|
| CLI 端口契约（默认） | `go run ./cmd/config` | 5 字段严格白名单；http_port=8081 | `verify_config_refactor` |
| 脚本单测 | `make test-scripts` | 含配置契约/env 安全用例全通过 | 同上 |
| 前端类型 | `npm --prefix web run typecheck` | `@types/node` 就位、无 `@ts-nocheck`、0 error | 同上 |
| 构建 | `make build` | npm ci + vite build + Go embedded | 同上 |
| 默认真实链路 | `make migrate` / `make e2e` / `make e2e-browser` | 8081 上真 MySQL/Redis/Go/浏览器 | 同上 |
| Go 契约单测 | `go test -run TestYAMLConfigWiringAndPublicWhitelist\|TestPublicDoesNotRequireStorageSecret\|TestPort8081SecurityContract\|TestDynamicListenerAndOriginWhitelist` | `--- PASS` 存在且非 `[no tests to run]`（防 `-run` 假通过） | 同上 |
| 仅改 YAML 端口 | 临时目录复制 `config/config.yaml`，只把 `server.http.port` 改 8083；`POKER_CONFIG_FILE` 指向临时文件 | 主 YAML/local 未改；CLI http_port=8083 | 同上 |
| 8083 真实链路 | `python3 tests/e2e/run.py all`（POKER_CONFIG_FILE=临时） | service/E2E 基址自动 8083 | 同上 |
| 8083 浏览器 | `python3 tests/e2e/browser/run_browser.py`（同上） | `POKER_BASE_URL` 同源 8083 | 同上 |
| 8083 Vite proxy | `python3 tests/e2e/vite_proxy.py`（同上） | 真实 proxy Host/Origin 改写，非 mock 配置 | 同上 |
| Go 全量 race | `go test -mod=readonly -race -count=1 ./...` | 9 个包全通过（干净环境，不注入配置） | `verify_config_refactor` |
| Go vet | `go vet ./...` | 0 报告 | 同上 |

### E2E 场景 → 测试函数（`tests/e2e/run.py` CASES）

| 场景键 | 测试函数 | 8081 | 8083 |
|---|---|---|---|
| mock | `test_mock_multihand` | PASS | PASS |
| privacy | `test_public_private_isolation` | PASS | PASS |
| controls | `test_pause_resume_stop` | PASS | PASS |
| recovery | `test_restart_recovery` | PASS | PASS |
| redis | `test_redis_runtime_degradation` | PASS | PASS |
| dependencies | `test_dependency_startup_failures` | PASS | PASS |
| embedded | `test_embedded_resources` | PASS | PASS |
| abort | `test_http_aborted_polling` | PASS | PASS |
| port | `test_http_host_origin_contract` | PASS | PASS |

`e2e: tests=9 passed=9 failed=0 blocked=0`（两端口各一次，见原始输出）。
Chromium：`browser-wrapper: passed=6 failed=0`（两端口各一次）。
Vite proxy：`checks=4 passed=4 failed=0`（health 200 / direct-origin 403 / proxied-origin 400 / index HTML）。

## 安全边界

- 临时 YAML 由默认 YAML 复制并只改一处端口；无秘密字段；内容不打印。
- 旧 `.env.local` 仅秘密（DSN/Redis 密码）注入；非敏感旧值不注入、不盖过 YAML。
- 普通 dev / E2E `allow_paid_models=false`；`dev-live` 唯显式 action。
- 旧已占用的陌生 8081/8083/5173 端口不 kill，按阻塞上报。
- 只启动本 lane 自己的 HTTP；结束释放；依赖保留。

## 原始输出

（以下由 `verify-config-refactor` 运行时追加。）

### make test-scripts

```text
python3 tests/e2e/test_local.py
test_child_env_removes_admin_and_inherited_config: PASS
test_config_cli_env_only_forwards_whitelist: PASS
test_config_cli_failure_is_blocker_not_default: PASS
test_config_public_derives_endpoint_from_cli_not_constant: PASS
test_config_public_honors_explicit_env_override: PASS
test_config_public_rejects_non_loopback_host: PASS
test_config_public_rejects_non_whitelist_and_secret_fields: PASS
test_config_public_requires_consistent_address: PASS
test_dev_catalog_allowed_paid_still_disabled: PASS
test_dev_live_only_explicit_action_changes_backend_switch: PASS
test_duplicate_rejected: PASS
test_e2e_ambient_paid_and_catalog_cannot_override: PASS
test_e2e_catalog_override_does_not_read_user_config: PASS
test_existing_runtime_permissions_not_changed: PASS
test_fallback_ports_allowed: PASS
test_go_contract_check_rejects_run_without_match: PASS
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
test_probe_resolved_port_in_use_refuses_without_signal: PASS
test_real_catalog_rejected: PASS
test_redaction_does_not_consume_next_test_line: PASS
test_safe_output_preserves_failure_and_redacts_credentials: PASS
test_service_env_dev_forwards_whitelist_and_ignores_ambient_paid: PASS
test_service_env_dev_live_only_explicit_action_enables_paid: PASS
test_service_env_e2e_forces_mock_and_false_and_drops_legacy_nonsensitive: PASS
test_service_env_injects_absolute_config_file: PASS
test_setup_preserves_existing_env: PASS
test_stop_rechecks_identity_before_signal: PASS
test_unknown_key_rejected: PASS
scripts: tests=45 passed=45 failed=0
```

退出码：0；判定：PASS

### web typecheck

```text

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

```

退出码：0；判定：PASS

### make build

```text
python3 scripts/local.py frontend

added 71 packages, and audited 72 packages in 6s

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
✓ built in 675ms
stage=frontend-build; exit=0; output=credential-redacted-only
python3 scripts/local.py build

build: passed; exit=0
```

退出码：0；判定：PASS

默认 CLI：字段=frontend_port,http_address,http_base_url,http_host,http_port；http_port=8081；strict5=True

### default 8081: make migrate

```text
python3 scripts/local.py migrate

migrate: passed; exit=0
```

退出码：0；判定：PASS

### default 8081: make e2e

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
  host-origin-contract: resolved=127.0.0.1:8081; neighbor/other-origin Host and Origin rejected
port -> test_http_host_origin_contract: PASS
e2e: tests=9 passed=9 failed=0 blocked=0
```

退出码：0；判定：PASS

### default 8081: make e2e-browser

```text
python3 tests/e2e/browser/run_browser.py

Running 6 tests using 1 worker

  ✓  1 [chromium] › tests/scenarios.spec.ts:75:1 › scenario 1 · 配置 2/9 人且每座选择不同模型 (711ms)
  ✓  2 [chromium] › tests/scenarios.spec.ts:108:1 › scenario 2 · 创建/开始至 max_hands 并核对结束详情数值数组 (2.1s)
  ✓  3 [chromium] › tests/scenarios.spec.ts:142:1 › scenario 3 · 暂停 / 刷新找 current / 恢复 / 停止 (1.3s)
  ✓  4 [chromium] › tests/scenarios.spec.ts:167:1 › scenario 4 · live 禁用与费用确认门槛 (628ms)
  ✓  5 [chromium] › tests/scenarios.spec.ts:210:1 › scenario 6 · 运行中无底牌 / 完成后详情可揭示 (2.5s)
  ✓  6 [chromium] › tests/scenarios.spec.ts:251:3 › scenario 5 · 手机宽 390 可操作无遮挡 › 390 宽配置与运行控制可用 (693ms)

  6 passed (8.5s)
browser-wrapper: passed=6 failed=0 exit=0
browser-wrapper: screenshots=/Users/gaosper/Documents/mycode/texas-holdem/web/node_modules/.cache/browser-verify/20261003-191556
browser-wrapper: note=infra run.py 'browser' 仍为占位，本目录为独立可复现入口
browser-wrapper: own HTTP process stopped; dependencies retained
```

退出码：0；判定：PASS

### default 8081: go contract tests

```text
=== RUN   TestYAMLConfigWiringAndPublicWhitelist
--- PASS: TestYAMLConfigWiringAndPublicWhitelist (0.00s)
=== RUN   TestPublicDoesNotRequireStorageSecret
--- PASS: TestPublicDoesNotRequireStorageSecret (0.00s)
PASS
ok  	texas-holdem/internal/conf	0.235s
=== RUN   TestPort8081SecurityContract
=== RUN   TestPort8081SecurityContract/loopback_8081_same_origin
=== RUN   TestPort8081SecurityContract/localhost_8081_same_origin
=== RUN   TestPort8081SecurityContract/old_loopback_host_8080
=== RUN   TestPort8081SecurityContract/old_localhost_host_8080
=== RUN   TestPort8081SecurityContract/neighbor_host_8082
=== RUN   TestPort8081SecurityContract/old_origin_8080
=== RUN   TestPort8081SecurityContract/neighbor_origin_8082
=== RUN   TestPort8081SecurityContract/hostname_mismatch
--- PASS: TestPort8081SecurityContract (0.00s)
    --- PASS: TestPort8081SecurityContract/loopback_8081_same_origin (0.00s)
    --- PASS: TestPort8081SecurityContract/localhost_8081_same_origin (0.00s)
    --- PASS: TestPort8081SecurityContract/old_loopback_host_8080 (0.00s)
    --- PASS: TestPort8081SecurityContract/old_localhost_host_8080 (0.00s)
    --- PASS: TestPort8081SecurityContract/neighbor_host_8082 (0.00s)
    --- PASS: TestPort8081SecurityContract/old_origin_8080 (0.00s)
    --- PASS: TestPort8081SecurityContract/neighbor_origin_8082 (0.00s)
    --- PASS: TestPort8081SecurityContract/hostname_mismatch (0.00s)
=== RUN   TestDynamicListenerAndOriginWhitelist
=== RUN   TestDynamicListenerAndOriginWhitelist/new_port_same_origin
=== RUN   TestDynamicListenerAndOriginWhitelist/new_port_localhost_same_origin
=== RUN   TestDynamicListenerAndOriginWhitelist/old_port_8081_host_rejected
=== RUN   TestDynamicListenerAndOriginWhitelist/old_port_8081_origin_rejected
=== RUN   TestDynamicListenerAndOriginWhitelist/cross_origin_rejected
--- PASS: TestDynamicListenerAndOriginWhitelist (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/new_port_same_origin (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/new_port_localhost_same_origin (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/old_port_8081_host_rejected (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/old_port_8081_origin_rejected (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/cross_origin_rejected (0.00s)
PASS
ok  	texas-holdem/internal/server	0.326s
```

退出码：0；判定：PASS

临时 8083 CLI：字段=frontend_port,http_address,http_base_url,http_host,http_port；http_port=8083；strict5=True

### temp 8083: e2e all

```text
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
  host-origin-contract: resolved=127.0.0.1:8083; neighbor/other-origin Host and Origin rejected
port -> test_http_host_origin_contract: PASS
e2e: tests=9 passed=9 failed=0 blocked=0
```

退出码：0；判定：PASS

### temp 8083: browser

```text

Running 6 tests using 1 worker

  ✓  1 [chromium] › tests/scenarios.spec.ts:75:1 › scenario 1 · 配置 2/9 人且每座选择不同模型 (680ms)
  ✓  2 [chromium] › tests/scenarios.spec.ts:108:1 › scenario 2 · 创建/开始至 max_hands 并核对结束详情数值数组 (2.0s)
  ✓  3 [chromium] › tests/scenarios.spec.ts:142:1 › scenario 3 · 暂停 / 刷新找 current / 恢复 / 停止 (1.3s)
  ✓  4 [chromium] › tests/scenarios.spec.ts:167:1 › scenario 4 · live 禁用与费用确认门槛 (593ms)
  ✓  5 [chromium] › tests/scenarios.spec.ts:210:1 › scenario 6 · 运行中无底牌 / 完成后详情可揭示 (2.5s)
  ✓  6 [chromium] › tests/scenarios.spec.ts:251:3 › scenario 5 · 手机宽 390 可操作无遮挡 › 390 宽配置与运行控制可用 (721ms)

  6 passed (8.3s)
browser-wrapper: passed=6 failed=0 exit=0
browser-wrapper: screenshots=/Users/gaosper/Documents/mycode/texas-holdem/web/node_modules/.cache/browser-verify/20261003-191626
browser-wrapper: note=infra run.py 'browser' 仍为占位，本目录为独立可复现入口
browser-wrapper: own HTTP process stopped; dependencies retained
```

退出码：0；判定：PASS

### temp 8083: vite proxy -> Go

```text
vite-proxy: own processes stopped; ports-released=True
VITE_PROXY_HTTP_STATUS_MISMATCH
```

退出码：1；判定：FAIL

主 YAML 端口仍为 8081：True
自身 HTTP 8081/8083 已释放：True

失败阶段数：1

### make test-scripts

```text
python3 tests/e2e/test_local.py
test_child_env_removes_admin_and_inherited_config: PASS
test_config_cli_env_only_forwards_whitelist: PASS
test_config_cli_failure_is_blocker_not_default: PASS
test_config_public_derives_endpoint_from_cli_not_constant: PASS
test_config_public_honors_explicit_env_override: PASS
test_config_public_rejects_non_loopback_host: PASS
test_config_public_rejects_non_whitelist_and_secret_fields: PASS
test_config_public_requires_consistent_address: PASS
test_dev_catalog_allowed_paid_still_disabled: PASS
test_dev_live_only_explicit_action_changes_backend_switch: PASS
test_duplicate_rejected: PASS
test_e2e_ambient_paid_and_catalog_cannot_override: PASS
test_e2e_catalog_override_does_not_read_user_config: PASS
test_existing_runtime_permissions_not_changed: PASS
test_fallback_ports_allowed: PASS
test_go_contract_check_rejects_run_without_match: PASS
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
test_probe_resolved_port_in_use_refuses_without_signal: PASS
test_real_catalog_rejected: PASS
test_redaction_does_not_consume_next_test_line: PASS
test_safe_output_preserves_failure_and_redacts_credentials: PASS
test_service_env_dev_forwards_whitelist_and_ignores_ambient_paid: PASS
test_service_env_dev_live_only_explicit_action_enables_paid: PASS
test_service_env_e2e_forces_mock_and_false_and_drops_legacy_nonsensitive: PASS
test_service_env_injects_absolute_config_file: PASS
test_setup_preserves_existing_env: PASS
test_stop_rechecks_identity_before_signal: PASS
test_unknown_key_rejected: PASS
scripts: tests=45 passed=45 failed=0
```

退出码：0；判定：PASS

### web typecheck

```text

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

```

退出码：0；判定：PASS

### make build

```text
python3 scripts/local.py frontend

added 71 packages, and audited 72 packages in 6s

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
✓ built in 1.80s
stage=frontend-build; exit=0; output=credential-redacted-only
python3 scripts/local.py build

build: passed; exit=0
```

退出码：0；判定：PASS

默认 CLI：字段=frontend_port,http_address,http_base_url,http_host,http_port；http_port=8081；strict5=True

### default 8081: make migrate

```text
python3 scripts/local.py migrate

migrate: passed; exit=0
```

退出码：0；判定：PASS

### default 8081: make e2e

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
  host-origin-contract: resolved=127.0.0.1:8081; neighbor/other-origin Host and Origin rejected
port -> test_http_host_origin_contract: PASS
e2e: tests=9 passed=9 failed=0 blocked=0
```

退出码：0；判定：PASS

### default 8081: make e2e-browser

```text
python3 tests/e2e/browser/run_browser.py

Running 6 tests using 1 worker

  ✓  1 [chromium] › tests/scenarios.spec.ts:75:1 › scenario 1 · 配置 2/9 人且每座选择不同模型 (858ms)
  ✓  2 [chromium] › tests/scenarios.spec.ts:108:1 › scenario 2 · 创建/开始至 max_hands 并核对结束详情数值数组 (2.0s)
  ✓  3 [chromium] › tests/scenarios.spec.ts:142:1 › scenario 3 · 暂停 / 刷新找 current / 恢复 / 停止 (1.3s)
  ✓  4 [chromium] › tests/scenarios.spec.ts:167:1 › scenario 4 · live 禁用与费用确认门槛 (696ms)
  ✓  5 [chromium] › tests/scenarios.spec.ts:210:1 › scenario 6 · 运行中无底牌 / 完成后详情可揭示 (3.1s)
  ✓  6 [chromium] › tests/scenarios.spec.ts:251:3 › scenario 5 · 手机宽 390 可操作无遮挡 › 390 宽配置与运行控制可用 (922ms)

  6 passed (9.5s)
browser-wrapper: passed=6 failed=0 exit=0
browser-wrapper: screenshots=/Users/gaosper/Documents/mycode/texas-holdem/web/node_modules/.cache/browser-verify/20261003-191834
browser-wrapper: note=infra run.py 'browser' 仍为占位，本目录为独立可复现入口
browser-wrapper: own HTTP process stopped; dependencies retained
```

退出码：0；判定：PASS

### default 8081: go contract tests

```text
=== RUN   TestYAMLConfigWiringAndPublicWhitelist
--- PASS: TestYAMLConfigWiringAndPublicWhitelist (0.00s)
=== RUN   TestPublicDoesNotRequireStorageSecret
--- PASS: TestPublicDoesNotRequireStorageSecret (0.00s)
PASS
ok  	texas-holdem/internal/conf	3.076s
=== RUN   TestPort8081SecurityContract
=== RUN   TestPort8081SecurityContract/loopback_8081_same_origin
=== RUN   TestPort8081SecurityContract/localhost_8081_same_origin
=== RUN   TestPort8081SecurityContract/old_loopback_host_8080
=== RUN   TestPort8081SecurityContract/old_localhost_host_8080
=== RUN   TestPort8081SecurityContract/neighbor_host_8082
=== RUN   TestPort8081SecurityContract/old_origin_8080
=== RUN   TestPort8081SecurityContract/neighbor_origin_8082
=== RUN   TestPort8081SecurityContract/hostname_mismatch
--- PASS: TestPort8081SecurityContract (0.00s)
    --- PASS: TestPort8081SecurityContract/loopback_8081_same_origin (0.00s)
    --- PASS: TestPort8081SecurityContract/localhost_8081_same_origin (0.00s)
    --- PASS: TestPort8081SecurityContract/old_loopback_host_8080 (0.00s)
    --- PASS: TestPort8081SecurityContract/old_localhost_host_8080 (0.00s)
    --- PASS: TestPort8081SecurityContract/neighbor_host_8082 (0.00s)
    --- PASS: TestPort8081SecurityContract/old_origin_8080 (0.00s)
    --- PASS: TestPort8081SecurityContract/neighbor_origin_8082 (0.00s)
    --- PASS: TestPort8081SecurityContract/hostname_mismatch (0.00s)
=== RUN   TestDynamicListenerAndOriginWhitelist
=== RUN   TestDynamicListenerAndOriginWhitelist/new_port_same_origin
=== RUN   TestDynamicListenerAndOriginWhitelist/new_port_localhost_same_origin
=== RUN   TestDynamicListenerAndOriginWhitelist/old_port_8081_host_rejected
=== RUN   TestDynamicListenerAndOriginWhitelist/old_port_8081_origin_rejected
=== RUN   TestDynamicListenerAndOriginWhitelist/cross_origin_rejected
--- PASS: TestDynamicListenerAndOriginWhitelist (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/new_port_same_origin (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/new_port_localhost_same_origin (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/old_port_8081_host_rejected (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/old_port_8081_origin_rejected (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/cross_origin_rejected (0.00s)
PASS
ok  	texas-holdem/internal/server	2.801s
```

退出码：0；判定：PASS

临时 8083 CLI：字段=frontend_port,http_address,http_base_url,http_host,http_port；http_port=8083；strict5=True

### temp 8083: e2e all

```text
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
  host-origin-contract: resolved=127.0.0.1:8083; neighbor/other-origin Host and Origin rejected
port -> test_http_host_origin_contract: PASS
e2e: tests=9 passed=9 failed=0 blocked=0
```

退出码：0；判定：PASS

### temp 8083: browser

```text

Running 6 tests using 1 worker

  ✓  1 [chromium] › tests/scenarios.spec.ts:75:1 › scenario 1 · 配置 2/9 人且每座选择不同模型 (742ms)
  ✓  2 [chromium] › tests/scenarios.spec.ts:108:1 › scenario 2 · 创建/开始至 max_hands 并核对结束详情数值数组 (2.1s)
  ✓  3 [chromium] › tests/scenarios.spec.ts:142:1 › scenario 3 · 暂停 / 刷新找 current / 恢复 / 停止 (1.3s)
  ✓  4 [chromium] › tests/scenarios.spec.ts:167:1 › scenario 4 · live 禁用与费用确认门槛 (665ms)
  ✓  5 [chromium] › tests/scenarios.spec.ts:210:1 › scenario 6 · 运行中无底牌 / 完成后详情可揭示 (2.6s)
  ✓  6 [chromium] › tests/scenarios.spec.ts:251:3 › scenario 5 · 手机宽 390 可操作无遮挡 › 390 宽配置与运行控制可用 (750ms)

  6 passed (8.8s)
browser-wrapper: passed=6 failed=0 exit=0
browser-wrapper: screenshots=/Users/gaosper/Documents/mycode/texas-holdem/web/node_modules/.cache/browser-verify/20261003-191908
browser-wrapper: note=infra run.py 'browser' 仍为占位，本目录为独立可复现入口
browser-wrapper: own HTTP process stopped; dependencies retained
```

退出码：0；判定：PASS

### temp 8083: vite proxy -> Go

```text
vite-proxy: backend=http://127.0.0.1:8083; frontend=http://127.0.0.1:5173; health=200; direct-origin=403; proxied-origin=400; index=html
vite-proxy: checks=4 passed=4 failed=0
vite-proxy: own processes stopped; ports-released=True
```

退出码：0；判定：PASS

主 YAML 端口仍为 8081：True
自身 HTTP 8081/8083 已释放：True

失败阶段数：0

### make test-scripts

```text
python3 tests/e2e/test_local.py
test_child_env_removes_admin_and_inherited_config: PASS
test_config_cli_env_only_forwards_whitelist: PASS
test_config_cli_failure_is_blocker_not_default: PASS
test_config_public_derives_endpoint_from_cli_not_constant: PASS
test_config_public_honors_explicit_env_override: PASS
test_config_public_rejects_non_loopback_host: PASS
test_config_public_rejects_non_whitelist_and_secret_fields: PASS
test_config_public_requires_consistent_address: PASS
test_dev_catalog_allowed_paid_still_disabled: PASS
test_dev_live_only_explicit_action_changes_backend_switch: PASS
test_duplicate_rejected: PASS
test_e2e_ambient_paid_and_catalog_cannot_override: PASS
test_e2e_catalog_override_does_not_read_user_config: PASS
test_existing_runtime_permissions_not_changed: PASS
test_fallback_ports_allowed: PASS
test_go_contract_check_rejects_run_without_match: PASS
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
test_probe_resolved_port_in_use_refuses_without_signal: PASS
test_real_catalog_rejected: PASS
test_redaction_does_not_consume_next_test_line: PASS
test_safe_output_preserves_failure_and_redacts_credentials: PASS
test_service_env_dev_forwards_whitelist_and_ignores_ambient_paid: PASS
test_service_env_dev_live_only_explicit_action_enables_paid: PASS
test_service_env_e2e_forces_mock_and_false_and_drops_legacy_nonsensitive: PASS
test_service_env_injects_absolute_config_file: PASS
test_setup_preserves_existing_env: PASS
test_stop_rechecks_identity_before_signal: PASS
test_unknown_key_rejected: PASS
scripts: tests=45 passed=45 failed=0
```

退出码：0；判定：PASS

### web typecheck

```text

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

```

退出码：0；判定：PASS

### make build

```text
python3 scripts/local.py frontend

added 71 packages, and audited 72 packages in 6s

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
✓ built in 690ms
stage=frontend-build; exit=0; output=credential-redacted-only
python3 scripts/local.py build

build: passed; exit=0
```

退出码：0；判定：PASS

默认 CLI：字段=frontend_port,http_address,http_base_url,http_host,http_port；http_port=8081；strict5=True

### default 8081: make migrate

```text
python3 scripts/local.py migrate

migrate: passed; exit=0
```

退出码：0；判定：PASS

### default 8081: make e2e

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
  host-origin-contract: resolved=127.0.0.1:8081; neighbor/other-origin Host and Origin rejected
port -> test_http_host_origin_contract: PASS
e2e: tests=9 passed=9 failed=0 blocked=0
```

退出码：0；判定：PASS

### default 8081: make e2e-browser

```text
python3 tests/e2e/browser/run_browser.py

Running 6 tests using 1 worker

  ✓  1 [chromium] › tests/scenarios.spec.ts:75:1 › scenario 1 · 配置 2/9 人且每座选择不同模型 (724ms)
  ✓  2 [chromium] › tests/scenarios.spec.ts:108:1 › scenario 2 · 创建/开始至 max_hands 并核对结束详情数值数组 (2.2s)
  ✓  3 [chromium] › tests/scenarios.spec.ts:142:1 › scenario 3 · 暂停 / 刷新找 current / 恢复 / 停止 (1.3s)
  ✓  4 [chromium] › tests/scenarios.spec.ts:167:1 › scenario 4 · live 禁用与费用确认门槛 (627ms)
  ✓  5 [chromium] › tests/scenarios.spec.ts:210:1 › scenario 6 · 运行中无底牌 / 完成后详情可揭示 (2.5s)
  ✓  6 [chromium] › tests/scenarios.spec.ts:251:3 › scenario 5 · 手机宽 390 可操作无遮挡 › 390 宽配置与运行控制可用 (698ms)

  6 passed (8.5s)
browser-wrapper: passed=6 failed=0 exit=0
browser-wrapper: screenshots=/Users/gaosper/Documents/mycode/texas-holdem/web/node_modules/.cache/browser-verify/20261003-192533
browser-wrapper: note=infra run.py 'browser' 仍为占位，本目录为独立可复现入口
browser-wrapper: own HTTP process stopped; dependencies retained
```

退出码：0；判定：PASS

### default 8081: go contract tests

```text
=== RUN   TestYAMLConfigWiringAndPublicWhitelist
--- PASS: TestYAMLConfigWiringAndPublicWhitelist (0.00s)
=== RUN   TestPublicDoesNotRequireStorageSecret
--- PASS: TestPublicDoesNotRequireStorageSecret (0.00s)
PASS
ok  	texas-holdem/internal/conf	0.167s
=== RUN   TestPort8081SecurityContract
=== RUN   TestPort8081SecurityContract/loopback_8081_same_origin
=== RUN   TestPort8081SecurityContract/localhost_8081_same_origin
=== RUN   TestPort8081SecurityContract/old_loopback_host_8080
=== RUN   TestPort8081SecurityContract/old_localhost_host_8080
=== RUN   TestPort8081SecurityContract/neighbor_host_8082
=== RUN   TestPort8081SecurityContract/old_origin_8080
=== RUN   TestPort8081SecurityContract/neighbor_origin_8082
=== RUN   TestPort8081SecurityContract/hostname_mismatch
--- PASS: TestPort8081SecurityContract (0.00s)
    --- PASS: TestPort8081SecurityContract/loopback_8081_same_origin (0.00s)
    --- PASS: TestPort8081SecurityContract/localhost_8081_same_origin (0.00s)
    --- PASS: TestPort8081SecurityContract/old_loopback_host_8080 (0.00s)
    --- PASS: TestPort8081SecurityContract/old_localhost_host_8080 (0.00s)
    --- PASS: TestPort8081SecurityContract/neighbor_host_8082 (0.00s)
    --- PASS: TestPort8081SecurityContract/old_origin_8080 (0.00s)
    --- PASS: TestPort8081SecurityContract/neighbor_origin_8082 (0.00s)
    --- PASS: TestPort8081SecurityContract/hostname_mismatch (0.00s)
=== RUN   TestDynamicListenerAndOriginWhitelist
=== RUN   TestDynamicListenerAndOriginWhitelist/new_port_same_origin
=== RUN   TestDynamicListenerAndOriginWhitelist/new_port_localhost_same_origin
=== RUN   TestDynamicListenerAndOriginWhitelist/old_port_8081_host_rejected
=== RUN   TestDynamicListenerAndOriginWhitelist/old_port_8081_origin_rejected
=== RUN   TestDynamicListenerAndOriginWhitelist/cross_origin_rejected
--- PASS: TestDynamicListenerAndOriginWhitelist (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/new_port_same_origin (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/new_port_localhost_same_origin (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/old_port_8081_host_rejected (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/old_port_8081_origin_rejected (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/cross_origin_rejected (0.00s)
PASS
ok  	texas-holdem/internal/server	0.262s
```

退出码：0；判定：PASS

### go test -mod=readonly -race -count=1 ./...

```text
ok  	texas-holdem/cmd/config	1.181s
ok  	texas-holdem/cmd/server	1.365s
ok  	texas-holdem/internal/agent	1.835s
ok  	texas-holdem/internal/conf	1.239s
ok  	texas-holdem/internal/data	1.948s
ok  	texas-holdem/internal/poker	2.017s
ok  	texas-holdem/internal/server	2.111s
ok  	texas-holdem/internal/service	2.410s
?   	texas-holdem/web	[no test files]
```

退出码：0；判定：PASS

### go vet ./...

```text

```

退出码：0；判定：PASS

临时 8083 CLI：字段=frontend_port,http_address,http_base_url,http_host,http_port；http_port=8083；strict5=True

### temp 8083: e2e all

```text
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
  host-origin-contract: resolved=127.0.0.1:8083; neighbor/other-origin Host and Origin rejected
port -> test_http_host_origin_contract: PASS
e2e: tests=9 passed=9 failed=0 blocked=0
```

退出码：0；判定：PASS

### temp 8083: browser

```text

Running 6 tests using 1 worker

  ✓  1 [chromium] › tests/scenarios.spec.ts:75:1 › scenario 1 · 配置 2/9 人且每座选择不同模型 (725ms)
  ✓  2 [chromium] › tests/scenarios.spec.ts:108:1 › scenario 2 · 创建/开始至 max_hands 并核对结束详情数值数组 (2.0s)
  ✓  3 [chromium] › tests/scenarios.spec.ts:142:1 › scenario 3 · 暂停 / 刷新找 current / 恢复 / 停止 (1.2s)
  ✓  4 [chromium] › tests/scenarios.spec.ts:167:1 › scenario 4 · live 禁用与费用确认门槛 (598ms)
  ✓  5 [chromium] › tests/scenarios.spec.ts:210:1 › scenario 6 · 运行中无底牌 / 完成后详情可揭示 (2.6s)
  ✓  6 [chromium] › tests/scenarios.spec.ts:251:3 › scenario 5 · 手机宽 390 可操作无遮挡 › 390 宽配置与运行控制可用 (838ms)

  6 passed (8.5s)
browser-wrapper: passed=6 failed=0 exit=0
browser-wrapper: screenshots=/Users/gaosper/Documents/mycode/texas-holdem/web/node_modules/.cache/browser-verify/20261003-192609
browser-wrapper: note=infra run.py 'browser' 仍为占位，本目录为独立可复现入口
browser-wrapper: own HTTP process stopped; dependencies retained
```

退出码：0；判定：PASS

### temp 8083: vite proxy -> Go

```text
vite-proxy: backend=http://127.0.0.1:8083; frontend=http://127.0.0.1:5173; health=200; direct-origin=403; proxied-origin=400; index=html
vite-proxy: checks=4 passed=4 failed=0
vite-proxy: own processes stopped; ports-released=True
```

退出码：0；判定：PASS

主 YAML 端口仍为 8081：True
自身 HTTP 8081/8083 已释放：True

失败阶段数：0

## 存储切换验证：Homebrew 3306/6379 + config.local.yaml 直连

```text
env: present; owner=current-user; mode=600
mysql: available; loopback:3306; db-configured
redis: available; loopback:6379; db=0; no writes
frontend-dist: present
checks: passed=3 failed=0
harness: FAIL START_OR_MODELS_CHECK_REDACTED
Traceback (most recent call last):
  File "/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/run.py", line 478, in main
    h.start()
  File "/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/run.py", line 175, in start
    wait_for(ready, "SERVER_START_TIMEOUT")
  File "/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/run.py", line 60, in wait_for
    value = fn()
  File "/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/run.py", line 169, in ready
    require(self.process.poll() is None, "SERVER_START_FAILED_LOGS_REDACTED")
  File "/Users/gaosper/Documents/mycode/texas-holdem/scripts/local.py", line 43, in require
    raise SafeError(code)
local.SafeError: SERVER_START_FAILED_LOGS_REDACTED
server logs (credential-redacted only):
CONFIG_INVALID
e2e: tests=1 passed=0 failed=1 blocked=8
```

退出码：1

### 重建二进制后复跑：Homebrew 3306/6379 + config.local.yaml 直连

```text
env: present; owner=current-user; mode=600
mysql: available; loopback:3306; db-configured
redis: available; loopback:6379; db=0; no writes
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
  host-origin-contract: resolved=127.0.0.1:8081; neighbor/other-origin Host and Origin rejected
port -> test_http_host_origin_contract: PASS
e2e: tests=9 passed=9 failed=0 blocked=0
```

退出码：0

### 存储切换：make test-scripts（Homebrew 直连后）

```text
python3 tests/e2e/test_local.py
test_child_env_removes_admin_and_inherited_config: PASS
test_config_cli_env_only_forwards_whitelist: PASS
test_config_cli_failure_is_blocker_not_default: PASS
test_config_public_derives_endpoint_from_cli_not_constant: PASS
test_config_public_honors_explicit_env_override: PASS
test_config_public_rejects_non_loopback_host: PASS
test_config_public_rejects_non_whitelist_and_secret_fields: PASS
test_config_public_requires_consistent_address: PASS
test_dev_catalog_allowed_paid_still_disabled: PASS
test_dev_live_only_explicit_action_changes_backend_switch: PASS
test_duplicate_rejected: PASS
test_e2e_ambient_paid_and_catalog_cannot_override: PASS
test_e2e_catalog_override_does_not_read_user_config: PASS
test_existing_runtime_permissions_not_changed: PASS
test_fallback_ports_allowed: PASS
test_go_contract_check_rejects_run_without_match: PASS
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
test_load_env_accepts_near_empty_file: FAIL
test_mysql_password_only_in_child_environment: PASS
test_no_shell_expansion: PASS
test_nonlocal_mysql_rejected: PASS
test_nonlocal_redis_rejected: PASS
test_paid_models_rejected: PASS
test_permissions_and_symlink: PASS
test_pid_identity_mismatch_rejected: PASS
test_probe_resolved_port_in_use_refuses_without_signal: PASS
test_real_catalog_rejected: PASS
test_redaction_does_not_consume_next_test_line: PASS
test_safe_output_preserves_failure_and_redacts_credentials: PASS
test_service_env_dev_forwards_whitelist_and_ignores_ambient_paid: PASS
test_service_env_dev_live_only_explicit_action_enables_paid: PASS
test_service_env_e2e_forces_mock_and_false_and_drops_legacy_nonsensitive: PASS
test_service_env_injects_absolute_config_file: PASS
test_setup_preserves_existing_env: PASS
test_stop_rechecks_identity_before_signal: PASS
test_storage_config_merges_direct_fields_over_env: PASS
test_unknown_key_rejected: PASS
test_validate_tolerates_empty_or_near_empty_env: PASS
Traceback (most recent call last):
  File "/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/test_local.py", line 440, in test_load_env_accepts_near_empty_file
    self.assertEqual(local.load_env(), {})
AssertionError: {'POKER_CATALOG_PATH': '/Users/gaosper/Doc[49 chars]son'} != {}
- {'POKER_CATALOG_PATH': '/Users/gaosper/Documents/mycode/texas-holdem/scripts/mock-catalog.json'}
+ {}
scripts: tests=48 passed=47 failed=1
make: *** [test-scripts] Error 1
```

退出码：2

### 存储切换：make test-scripts 复跑（修正环境敏感断言后）

```text
python3 tests/e2e/test_local.py
test_child_env_removes_admin_and_inherited_config: PASS
test_config_cli_env_only_forwards_whitelist: PASS
test_config_cli_failure_is_blocker_not_default: PASS
test_config_public_derives_endpoint_from_cli_not_constant: PASS
test_config_public_honors_explicit_env_override: PASS
test_config_public_rejects_non_loopback_host: PASS
test_config_public_rejects_non_whitelist_and_secret_fields: PASS
test_config_public_requires_consistent_address: PASS
test_dev_catalog_allowed_paid_still_disabled: PASS
test_dev_live_only_explicit_action_changes_backend_switch: PASS
test_duplicate_rejected: PASS
test_e2e_ambient_paid_and_catalog_cannot_override: PASS
test_e2e_catalog_override_does_not_read_user_config: PASS
test_existing_runtime_permissions_not_changed: PASS
test_fallback_ports_allowed: PASS
test_go_contract_check_rejects_run_without_match: PASS
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
test_load_env_accepts_near_empty_file: PASS
test_mysql_password_only_in_child_environment: PASS
test_no_shell_expansion: PASS
test_nonlocal_mysql_rejected: PASS
test_nonlocal_redis_rejected: PASS
test_paid_models_rejected: PASS
test_permissions_and_symlink: PASS
test_pid_identity_mismatch_rejected: PASS
test_probe_resolved_port_in_use_refuses_without_signal: PASS
test_real_catalog_rejected: PASS
test_redaction_does_not_consume_next_test_line: PASS
test_safe_output_preserves_failure_and_redacts_credentials: PASS
test_service_env_dev_forwards_whitelist_and_ignores_ambient_paid: PASS
test_service_env_dev_live_only_explicit_action_enables_paid: PASS
test_service_env_e2e_forces_mock_and_false_and_drops_legacy_nonsensitive: PASS
test_service_env_injects_absolute_config_file: PASS
test_setup_preserves_existing_env: PASS
test_stop_rechecks_identity_before_signal: PASS
test_storage_config_merges_direct_fields_over_env: PASS
test_unknown_key_rejected: PASS
test_validate_tolerates_empty_or_near_empty_env: PASS
scripts: tests=48 passed=48 failed=0
```

退出码：0

## 存储切换：场景 → 结果（Homebrew 3306/6379 + config.local.yaml 直连）

| 场景 | 目标 / 函数 | 结果 |
|---|---|---|
| `.env.local` 清理后可解析 | `parse_env` / `load_env` / `validate`（近空文件） | PASS（`test_load_env_accepts_near_empty_file`、`test_validate_tolerates_empty_or_near_empty_env`） |
| setup 已存在分支不 KeyError | `setup()` already-present | PASS（`test_setup_preserves_existing_env`） |
| service_env 不再注入旧 DSN/密码 | `service_env` | PASS（`test_service_env_e2e_forces_mock_and_false_and_drops_legacy_nonsensitive`） |
| YAML 直连优先于 env | `storage_config()` | PASS（`test_storage_config_merges_direct_fields_over_env`） |
| doctor 真连 MySQL 3306 / Redis 6379 | `doctor()` | `mysql: available; loopback:3306`；`redis: available; loopback:6379; db=0` |
| E2E 9 场景（真 3306/6379） | `run.py all` | `tests=9 passed=9 failed=0 blocked=0` |
| 存储启动失败注入 | `test_dependency_startup_failures` | mysql/redis 各自 `expected failure; HTTP closed` |
| 脚本安全 | `make test-scripts` | `tests=48 passed=48 failed=0` |

- 历史失败保留：首次 `make e2e` 因 **旧二进制未重建**（早于 Go 直连字段支持）报 `CONFIG_INVALID` /
  `SERVER_START_FAILED_LOGS_REDACTED`；重建 `tmp/texas-holdem` 后复跑 9/9。该失败输出已如实保留在本文件。
- 端口：验证期间自启 HTTP 8081 已退出；未改动 Homebrew 3306/6379；未 kill 陌生进程。
