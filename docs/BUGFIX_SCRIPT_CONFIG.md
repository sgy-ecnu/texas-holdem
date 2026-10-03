# 脚本配置接线 bug 修复（setup 凭据交接 / Go 契约一致 / doctor 权限核验）

> 只改 `scripts/local.py`、`tests/e2e/test_local.py`、`tests/e2e/run.py`（仅测试适配）及文档；
> 未改 Go/conf schema/go.mod/config.yaml/config.local.yaml。凭据值全程不进入输出/日志。
> 本文件按 RED→GREEN 记录三处 bug 修复与完整脱敏原始输出（追加于文末）。

## 修复点

| bug | file:line | 改动 |
|---|---|---|
| 1 setup 凭据交接断裂 | `scripts/local.py` `setup()` @409、新增 `_ensure_env_file()` @381、`_write_local_config()` @392 | setup 改为把生成的随机密码写入 `config.local.yaml` 直连字段（`data.mysql.dsn` / `data.redis.password`，O_EXCL、mode 600），不再写入 `.env.local`；`.env.local` 仅确保存在（近空）。`doctor()`/`service_env()`/`storage_config()` 与 setup 读同一来源。已存在 `config.local.yaml` → 保留不覆盖（`config-local: already-present; not overwritten`）。 |
| 2 Python 存储解析与 Go 契约不一致 | `scripts/local.py` `_read_yaml_subset()` @150、`storage_config()` @215 | 空值语义与 Go 对齐：`password` 以「字段存在性」决定回退——显式 `password: ""` = 无密码、不回退 env；`password:`（null）/缺省才读 `password_env`。解析器区分 `key:`（None）与 `key: ""`（""）。 |
| 3 doctor 未校验就报权限结论 | `scripts/local.py` 新增 `protected_stat()` @57、`doctor()` @359 | doctor 先 `protected_stat(ENV_FILE)` 实际核验类型/owner/mode=600 才输出结论；`config.local.yaml` 存在时同样核验。不再只看存在性就谎报 `owner=current-user; mode=600`。 |

## 新增测试函数（RED→GREEN）

| bug | 测试函数 |
|---|---|
| 1 | `test_setup_writes_generated_credentials_to_local_config`、`test_setup_preserves_existing_local_config` |
| 2 | `test_storage_config_explicit_empty_password_ignores_env`、`test_storage_config_absent_password_falls_back_to_env`、`test_storage_config_null_password_falls_back_to_env` |
| 3 | `test_protected_stat_rejects_wrong_mode`、`test_doctor_rejects_env_file_without_private_mode` |

## RED（修复前）

```text
test_doctor_rejects_env_file_without_private_mode: FAIL
test_protected_stat_rejects_wrong_mode: FAIL
test_setup_preserves_existing_local_config: FAIL
test_setup_writes_generated_credentials_to_local_config: FAIL
test_storage_config_explicit_empty_password_ignores_env: FAIL
scripts: tests=55 passed=50 failed=5
```

## GREEN（修复后）

```text
scripts: tests=55 passed=55 failed=0
```

## 场景 → 结果

| 场景 | 命令/函数 | 结果 |
|---|---|---|
| 近空 `.env.local` 容忍 | `test_load_env_accepts_near_empty_file` | PASS |
| setup 生成凭据写入 config.local.yaml | `test_setup_writes_generated_credentials_to_local_config` | PASS |
| setup 保留已有 config.local.yaml | `test_setup_preserves_existing_local_config` | PASS |
| password 显式空不回退 env | `test_storage_config_explicit_empty_password_ignores_env` | PASS |
| password 缺省/null 回退 env | `test_storage_config_absent_password_falls_back_to_env` / `..._null_...` | PASS |
| doctor 核验权限 | `test_protected_stat_rejects_wrong_mode` / `test_doctor_rejects_env_file_without_private_mode` | PASS |

## 原始输出（运行时追加）

### `python3 scripts/local.py setup`

```text
git: unavailable; checked .gitignore rules only
config-local: already-present; not overwritten
env: exists; regular-file; owner=current-user; mode=600
config-local: exists; regular-file; owner=current-user; mode=600
mysql: available; loopback:3306; db-configured
redis: available; loopback:6379; db=0; no writes
frontend-dist: present
checks: passed=3 failed=0
```

退出码：0

### `make test-scripts`

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
scripts: tests=55 passed=55 failed=0
```

退出码：0

### `make e2e`

```text
python3 tests/e2e/run.py all
env: exists; regular-file; owner=current-user; mode=600
config-local: exists; regular-file; owner=current-user; mode=600
mysql: available; loopback:3306; db-configured
redis: available; loopback:6379; db=0; no writes
frontend-dist: present
checks: passed=3 failed=0
mock -> test_mock_multihand: BLOCKED HTTP_PORT_IN_USE_STOP_NO_KILL
privacy -> test_public_private_isolation: BLOCKED HTTP_PORT_IN_USE_STOP_NO_KILL
controls -> test_pause_resume_stop: BLOCKED HTTP_PORT_IN_USE_STOP_NO_KILL
recovery -> test_restart_recovery: BLOCKED HTTP_PORT_IN_USE_STOP_NO_KILL
redis -> test_redis_runtime_degradation: BLOCKED HTTP_PORT_IN_USE_STOP_NO_KILL
dependencies -> test_dependency_startup_failures: BLOCKED HTTP_PORT_IN_USE_STOP_NO_KILL
embedded -> test_embedded_resources: BLOCKED HTTP_PORT_IN_USE_STOP_NO_KILL
abort -> test_http_aborted_polling: BLOCKED HTTP_PORT_IN_USE_STOP_NO_KILL
port -> test_http_host_origin_contract: BLOCKED HTTP_PORT_IN_USE_STOP_NO_KILL
e2e: tests=0 passed=0 failed=0 blocked=9
make: *** [e2e] Error 2
```

退出码：2

### `make e2e` 复跑（清理自身残留 8081 后）

```text
python3 tests/e2e/run.py all
env: exists; regular-file; owner=current-user; mode=600
config-local: exists; regular-file; owner=current-user; mode=600
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
