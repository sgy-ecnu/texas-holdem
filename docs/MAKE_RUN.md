# make run 验证证据

本轮新增 `make run`：一条命令完成「编译前端（生成最新 `web/dist`）+ 前台启动后端」，
避免「改了前端但服务嵌入旧 dist」。以下为实际命令与原始输出（仅对凭据按脚本既有策略
脱敏；无凭据出现在输出中）。工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`。

## 改动点

- `Makefile`
  - `.PHONY` 与 `local.py $@` 分发列表新增 `run`。
  - 新增注释说明 `run`/`dev`/`build` 语义差别；`build: frontend` 语义不变。
- `scripts/local.py`
  - 新增 `run()`：`frontend()`（npm ci + npm run build）后 `return go_task("dev")`。
    服务 env 仍由 `action_env("dev")` → `service_env` 注入绝对 `POKER_CONFIG_FILE`
    并加载受保护凭据，不绕过安全机制。
  - `main()` 新增 `run` action 分发。
- `tests/e2e/test_local.py`：新增 3 项（`test_run_compiles_frontend_before_starting_dev`、
  `test_run_frontend_failure_stops_before_service`、`test_main_run_action_dispatches_to_run`）。
- `README.md`、`docs/LOCAL_SETUP.md`：说明 `make run` 与 `make dev` 差别。

## make run vs make dev

| 命令 | 前端编译 | web/dist 要求 | 服务启动 | 适用 |
|---|---|---|---|---|
| `make run` | 每次 `npm ci` + `npm run build` | 自动生成最新 | 前台 dev，等价 `make dev` | 改完代码一键跑，保证嵌入最新 dist |
| `make dev` | 不编译 | 必须已存在 | 前台 dev | 已构建后的快速重启 |
| `make build` | `npm ci` + `build` + `go build` 产二进制 | 生成 | 不启动 | 语义不变 |

两者服务启动、配置加载、端口行为一致；`make run` 只是在前端阶段串行前置编译。

## `make test-scripts`（脚本安全测试全量）

```text
python3 tests/e2e/test_local.py
config-local: already-present; not overwritten
brew-service: mysql@8.4 start requested
brew-service: redis start requested
init: dedicated database/user created; scoped grants; no root changes
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
test_doctor_rejects_env_file_without_private_mode: PASS
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
test_main_run_action_dispatches_to_run: PASS
test_mysql_password_only_in_child_environment: PASS
test_no_shell_expansion: PASS
test_nonlocal_mysql_rejected: PASS
test_nonlocal_redis_rejected: PASS
test_paid_models_rejected: PASS
test_permissions_and_symlink: PASS
test_pid_identity_mismatch_rejected: PASS
test_probe_resolved_port_in_use_refuses_without_signal: PASS
test_protected_stat_rejects_wrong_mode: PASS
test_real_catalog_rejected: PASS
test_redaction_does_not_consume_next_test_line: PASS
test_run_compiles_frontend_before_starting_dev: PASS
test_run_frontend_failure_stops_before_service: PASS
test_safe_output_preserves_failure_and_redacts_credentials: PASS
test_service_env_dev_forwards_whitelist_and_ignores_ambient_paid: PASS
test_service_env_dev_live_only_explicit_action_enables_paid: PASS
test_service_env_e2e_forces_mock_and_false_and_drops_legacy_nonsensitive: PASS
test_service_env_injects_absolute_config_file: PASS
test_setup_preserves_existing_env: PASS
test_setup_preserves_existing_local_config: PASS
test_setup_writes_generated_credentials_to_local_config: PASS
test_stop_rechecks_identity_before_signal: PASS
test_storage_config_absent_password_falls_back_to_env: PASS
test_storage_config_explicit_empty_password_ignores_env: PASS
test_storage_config_merges_direct_fields_over_env: PASS
test_storage_config_null_password_falls_back_to_env: PASS
test_unknown_key_rejected: PASS
test_validate_tolerates_empty_or_near_empty_env: PASS
scripts: tests=58 passed=58 failed=0
```

## 实际 `make run`（真实 MySQL 3306 / Redis 6379，付费关闭）

编排脚本启动 `make run`，轮询 HTTP health，比对「服务返回的 JS 资源」与「本地
`web/dist` 资源」的 sha256，再以 SIGINT 停止（等价 Ctrl-C），检查端口释放与依赖保留。

```text
== before: web/dist assets ==
total 568
-rw-r--r--@ 1 gaosper  staff   24488 Oct  3 22:25 index-DO-kHOcF.css
-rw-r--r--@ 1 gaosper  staff  262894 Oct  3 22:25 index-TfRB24FV.js
before index.html sha256=5d3f050fd8709d7f0707f212be03fdc34f2175b3ed32557887c021042689d6d9
== start: make run (background) ==
make pid=9035
== health ==
{"mysql":"available","redis":"available","storage_degraded":false,"allow_paid_models":false,"max_live_calls":1000}

== served index asset refs ==
/assets/index-TfRB24FV.js
/assets/index-DO-kHOcF.css
served js asset=/assets/index-TfRB24FV.js
served asset sha256=21c56ff55c515d153537e0e8edcfab285eb71506c7da6dbcbb4ace35a117a1e3
local  asset sha256=21c56ff55c515d153537e0e8edcfab285eb71506c7da6dbcbb4ace35a117a1e3
== after: web/dist assets (should be freshly rebuilt) ==
total 568
-rw-r--r--@ 1 gaosper  staff   24488 Oct  3 22:29 index-DO-kHOcF.css
-rw-r--r--@ 1 gaosper  staff  262894 Oct  3 22:29 index-TfRB24FV.js
after index.html sha256=5d3f050fd8709d7f0707f212be03fdc34f2175b3ed32557887c021042689d6d9
== stop: SIGINT to scripts/local.py run ==
python pid=9044
make exit=2
== port state after stop ==
8081 closed
3306 open
6379 open
```

`make run` 自身原始输出：

```text
python3 scripts/local.py run

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
✓ 42 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-DO-kHOcF.css   24.49 kB │ gzip:  6.63 kB
dist/assets/index-TfRB24FV.js   262.89 kB │ gzip: 82.75 kB
✓ built in 977ms
stage=frontend-build; exit=0; output=credential-redacted-only
2026/10/03 22:29:55 INFO [HTTP] server listening addr=127.0.0.1:8081
make: *** [run] Error 241
```

结论：
- 前端每次重编译：`web/dist` 资源 mtime 由 `22:25` 变为 `22:29`（重新 `npm ci` + build）。
- 嵌入最新 dist：服务返回的 `/assets/index-TfRB24FV.js` 与本地 `web/dist` 同资源
  sha256 完全一致（`21c56ff5…`）。
- 服务起得来：`GET /api/v1/health` → 200，`mysql/redis=available`、`allow_paid_models=false`。
- 8081 释放：停止后 `8081 closed`；`pgrep` 无 `cmd/server`/`local.py run` 残留。
- 依赖保留：停止后 `3306 open`、`6379 open`。

## 未验证 / 边界

- `make run` 以 SIGINT（Ctrl-C）停止时，`go_task("dev")` 对子进程发 SIGTERM，`p.wait()`
  返回 `-15` → `sys.exit` 得到 241，`make` 报 `Error 241`。这是既有 `make dev` 的
  Ctrl-C 退出码行为，本轮未改动；服务与监听端口确实已释放。非「启动失败」，但退出码
  非零，脚本化调用方需知悉。
- 未验证：真实 provider / `make dev-live`（不付费）；未 commit/push；未改 Go、`web/src`、
  config、`docs/contracts.md`。
- `make run` 每次执行 `npm ci`，比 `make dev` 慢；若只重启后端继续用 `make dev`。
