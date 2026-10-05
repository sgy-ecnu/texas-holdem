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

---

# 追加轮：8081 残留 server 自动清理（2026-10-03）

## 问题根因

`make run` = 前端编译 + `go run ./cmd/server`。旧 `go run` 编译出的 server 二进制进程
若仍占用 8081，新 `go run` 会因端口被占启动失败；用户看到的仍是旧进程返回的嵌入 bundle
（`go:embed dist` 编译期嵌入，旧进程永远返回旧样式）。本轮在启动服务前自动检测并**安全**
清理本项目残留 server。

## 改动点

- `scripts/local.py`
  - 新增 `stop_stale_server(port=STALE_SERVER_PORT)`（`STALE_SERVER_PORT = 8081`）：
    `port_open` 空闲则跳过；占用时用 `lsof -nP -iTCP:8081 -sTCP:LISTEN -t` 取监听 PID，
    经 `server_listener_identity()` 核验身份后 SIGTERM，等待 20s 超时 SIGKILL。
  - 新增 `server_listener_identity(pid)`：要求进程存在且归属当前用户、原生 argv 可读
    （`process_arguments` 的 Darwin KERN_PROCARGS2，不信任 ps 文本）、可执行文件在
    `go run` 的 `GOCACHE` 下、cwd 为本项目根目录、argv 含 `cmd/server`/`texas-holdem`；
    采集前后启动时间一致。任一不满足 → `STALE_SERVER_UNKNOWN_PROCESS_MANUAL`，不杀。
  - 新增 `go_run_parent_identity(pid)`：沿 ppid 核验父进程为 `go run ./cmd/server`；
    无法确认返回 None（不杀）。辅助：`go_cache`、`listener_pids`、`project_process_cwd`、`_stop_pid`。
  - `run()`：`frontend()` 后、`go_task("dev")` 前调用 `stop_stale_server()`。
  - `go_task(action)`：`action in {"dev","dev-live"}` 时启动前调用 `stop_stale_server()`。
- `tests/e2e/test_local.py`：新增 12 项（清理跳过/清理顺序/拒绝陌生进程/监听不可识别/
  身份核验接受与拒绝/父进程核验/go_task 调用点与 build 不调用），并更新 2 项既有 run 测试。

## 清理顺序（go run 父进程 vs server 子进程）

监听 8081 的是 `go run` 编译出的 **server 子进程**；其父为 `go run` 进程。**必须先生命
令子进程退出，再兜底处理父进程**：若先杀父进程，子进程被 reparent 后仍会继续占用 8081。
子进程退出后 `go run` 自行结束，父进程通常已不存在（`_stop_pid` 检测到不存在即跳过）。
实测父子链：`python3 scripts/local.py dev`(14769) → `go run ./cmd/server`(14771) →
GOCACHE server(14775，监听 8081)。

## `make test-scripts`（全量，原始输出）

```text
python3 tests/e2e/test_local.py

build: passed; exit=0
config-local: already-present; not overwritten
brew-service: mysql@8.4 start requested
brew-service: redis start requested
init: dedicated database/user created; scoped grants; no root changes
stale-server: port 8081 free; nothing to stop
stale-server: stopping project server pid=14775
stale-server: port 8081 cleared
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
test_go_run_parent_identity_accepts_project_go_run: PASS
test_go_run_parent_identity_rejects_unrelated_parent: PASS
test_go_task_build_does_not_clean_stale_server: PASS
test_go_task_dev_cleans_stale_server_before_start: PASS
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
test_server_listener_identity_accepts_go_build_cache_server: PASS
test_server_listener_identity_rejects_foreign_cwd: PASS
test_server_listener_identity_rejects_non_go_run_executable: PASS
test_server_listener_identity_unavailable_argv_refuses: PASS
test_service_env_dev_forwards_whitelist_and_ignores_ambient_paid: PASS
test_service_env_dev_live_only_explicit_action_enables_paid: PASS
test_service_env_e2e_forces_mock_and_false_and_drops_legacy_nonsensitive: PASS
test_service_env_injects_absolute_config_file: PASS
test_setup_preserves_existing_env: PASS
test_setup_preserves_existing_local_config: PASS
test_setup_writes_generated_credentials_to_local_config: PASS
test_stop_rechecks_identity_before_signal: PASS
test_stop_stale_server_refuses_unknown_process_without_signal: PASS
test_stop_stale_server_skips_when_port_free: PASS
test_stop_stale_server_stops_listener_then_go_run_parent: PASS
test_stop_stale_server_unidentified_listener_refuses: PASS
test_storage_config_absent_password_falls_back_to_env: PASS
test_storage_config_explicit_empty_password_ignores_env: PASS
test_storage_config_merges_direct_fields_over_env: PASS
test_storage_config_null_password_falls_back_to_env: PASS
test_unknown_key_rejected: PASS
test_validate_tolerates_empty_or_near_empty_env: PASS
scripts: tests=70 passed=70 failed=0
```

> 注：测试中 `stale-server: stopping project server pid=14775` 是被 mock 的 `_stop_pid`
> 打印，未真杀，且断言顺序为「先监听子进程、后 go run 父进程」。

## 真实闭环：制造旧 server 残留 → `make run` 自动清理

闭环前 8081 上已有旧 `go run` server（PID 14775，cwd=项目根、可执行文件在 GOCACHE，
父 14771=`go run ./cmd/server`）。编排脚本启动 `make run`，等待监听 PID 变化，比对服务
返回 JS 与 `web/dist` 的 sha256，再 SIGINT 停止，检查端口。

```text
== before: stale listener on 8081 ==
COMMAND   PID    USER   FD   TYPE             DEVICE SIZE/OFF NODE NAME
server  14775 gaosper    7u  IPv4 0x9bf52119350c5fcf      0t0  TCP 127.0.0.1:8081 (LISTEN)
old listener pid=14775
old go-run parent pid=14771
old listener start: Sat Oct  3 23:33:39 2026
== before: old process served assets ==
/assets/index-Bi6lv2MK.js
/assets/index-BQyAYR3_.css
old served js ref=/assets/index-Bi6lv2MK.js
old served js sha256=bbdbb71f582fd0086b262a03f6f4dd956b0c26cc5a2d3f63a6694d0baa97d4c3

== start: make run (background) ==
make pid=15023
python pid=15032

== wait for stale server to be replaced ==
new listener pid=15233
old listener still alive? no
old go-run parent still alive? no

== health (new process) ==
{"mysql":"available","redis":"available","storage_degraded":false,"allow_paid_models":false,"max_live_calls":1000}

== served index asset refs (new process) ==
/assets/index-Bi6lv2MK.js
/assets/index-BQyAYR3_.css
new served js ref=/assets/index-Bi6lv2MK.js
served js sha256=bbdbb71f582fd0086b262a03f6f4dd956b0c26cc5a2d3f63a6694d0baa97d4c3
local  js sha256=bbdbb71f582fd0086b262a03f6f4dd956b0c26cc5a2d3f63a6694d0baa97d4c3
bundle-match=yes

== stop: SIGINT to python local.py run ==
make exit=2
== after: port state ==
8081 closed
3306 open
6379 open
residual project server procs:
(none)
```

`make run` 自身原始日志（清理与新监听关键行）：

```text
stale-server: stopping project server pid=14775
stale-server: port 8081 cleared
stale-server: port 8081 free; nothing to stop
2026/10/03 23:40:02 INFO [HTTP] server listening addr=127.0.0.1:8081
```

结论：
- **旧进程被清**：旧监听 PID 14775 与其 `go run` 父 14771 均消失（`still alive? no`），
  新监听 PID 15233 出现；日志显示 `stopping project server pid=14775` → `port 8081 cleared`。
- **只清本项目 server**：身份核验依据为 GOCACHE 可执行路径 + 项目根 cwd + 原生 argv，
  未触及 MySQL 3306 / Redis 6379（`3306 open`、`6379 open`）。
- **新 bundle 一致**：新进程返回的 `/assets/index-Bi6lv2MK.js` 与本地 `web/dist` 同资源
  sha256 完全一致（`bbdbb71f…`）；health 200，`allow_paid_models=false`。
- **幂等**：`run()` 调用后 `go_task("dev")` 再次调用，第二次 `port 8081 free; nothing to stop`。
- **退出后**：8081 释放，无 `cmd/server` 残留；`make` 仍报既有 Error 241（Ctrl-C 退出码）。

## 未验证 / 边界

- 未制造「旧进程嵌入旧 bundle、新进程嵌入新 bundle 且二者不同」的对比：本机 `web/dist`
  自旧进程启动前未变，两次 bundle hash 相同；已验证的是「新进程返回的 bundle 与
  `make run` 新构建的 `web/dist` 一致」。
- 陌生进程场景仅由 mock 单测覆盖（拒绝且不发信号），未真实验证「他人进程占 8081」的
  线上表现；`lsof` 对非本用户进程可能不回 PID，此时 `listener_pids` 为空 → 同样拒绝。
- `stop_stale_server` 固定检测 8081；若通过 `POKER_HTTP_PORT`/YAML 改端口，本次不清理该
  端口的残留（本次 bug 固定为 8081）。不触碰 runtime 隔离的 mysql/redis。
- 未 commit/push；未改 Go、`web/src`、config、`docs/contracts.md`。

---

# 追加轮：修复 go run 产物在 TMPDIR `go-build<随机数>` 下的身份核验（2026-10-05）

## 问题根因

`go run` 在部分环境把编译产物放在 `$TMPDIR/go-build<随机数>/b001/exe/server`
（实测 `/var/folders/.../T/go-build195384386/b001/exe/server`，realpath 前缀
`/private/var/...`），目录名是 `go-build<随机数>` 而非精确的 `go-build`。
旧 `server_listener_identity()` 用精确匹配：

```python
require(cache == exe or cache in exe.parents or "go-build" in exe.parts,
        "STALE_SERVER_UNKNOWN_PROCESS_MANUAL")
```

`"go-build" in exe.parts` 对 `go-build195384386` 为 False → 抛
`STALE_SERVER_UNKNOWN_PROCESS_MANUAL` → `make run`/`make dev` 启动前中止
（`make: *** [run] Error 1`）。

## 改动点

- `scripts/local.py` `server_listener_identity()`：
  `"go-build" in exe.parts` → `any(p.startswith("go-build") for p in exe.parts)`。
  其余安全约束全部不变（cwd=项目根、argv 含 `cmd/server`/`texas-holdem`、进程归属当前
  用户、采集前后启动时间一致）；仍有 `cache == exe or cache in exe.parents` 这两条
  GOCACHE 路径约束，前缀匹配只放宽「go run 临时产物目录名」这一条，不会误杀陌生进程。
- `tests/e2e/test_local.py` 新增 2 项（mock，不真杀进程）：
  `test_server_listener_identity_accepts_tmpdir_go_build_random_server`、
  `test_server_listener_identity_rejects_non_go_build_prefix`。
- `go_run_parent_identity()` 核实结论：**无同类问题，未改**。它沿 ppid 校验的是
  `go` 父进程本身（`exe.name == "go"` + `argv[:2] == ["go","run"]` + argv 含
  `cmd/server`/`texas-holdem` + cwd=项目根），不做 go-build 目录名匹配，
  不涉及 `TMPDIR/go-build<随机数>` 判定。

## `make test-scripts`（全量，原始输出尾部）

```text
test_server_listener_identity_accepts_tmpdir_go_build_random_server: PASS
test_server_listener_identity_rejects_non_go_build_prefix: PASS
test_server_listener_identity_rejects_non_go_run_executable: PASS
...
scripts: tests=72 passed=72 failed=0
```

新增两项在真实路径上的谓词对照（旧 False / 新 True）：

```text
parts: ('/', 'private', 'var', 'folders', 'w2', 'bntp6mgj2nn81dbr0cc2nnw00000gn', 'T', 'go-build195384386', 'b001', 'exe', 'server')
OLD  'go-build' in parts        -> False
NEW  any startswith('go-build') -> True
OLD full identity = False
NEW full identity = True
```

## 真实闭环 A：`make run` 清理 GOCACHE 路径残留

在本机 Go 1.26 下，默认 GOCACHE 命中时 `go run` 直接执行缓存中的链接产物
（`~/Library/Caches/go-build/57/...-d/server`）。后台 `make dev` 制造残留
（pid=29688，父 `go run ./cmd/server` pid=29684），随后 `make run`：

```text
stage=npm-ci; exit=0; output=credential-redacted-only
stage=frontend-build; exit=0; output=credential-redacted-only
stale-server: stopping project server pid=29688
stale-server: port 8081 cleared
stale-server: port 8081 free; nothing to stop
2026/10/05 22:16:49 INFO [HTTP] server listening addr=127.0.0.1:8081
```

- health：`{"mysql":"available","redis":"available","storage_degraded":false,"allow_paid_models":false,"max_live_calls":1000}` → HTTP 200。
- bundle 一致：服务返回 `/` 与本地 `web/dist/index.html` 同为
  `sha256=48b3495de5eb4dba90e105b02e2d3b2c8e890ae22a82e8191d6e4c449a179189`；
  `/assets/index-C-hqzINN.js` HTTP 200。
- 停止后：`8081: 0 listener(s)`、`3306: 1 listener(s)`、`6379: 2 listener(s)`。
- 无 `STALE_SERVER_UNKNOWN_PROCESS_MANUAL`（日志仅上述 stale-server 行；`make: *** [run]
  Error 241` 为 SIGINT 退出码，既有行为，非启动失败）。

## 真实闭环 B：强制 TMPDIR `go-build<随机数>` 路径残留并清理（本次修复目标场景）

用全新 GOCACHE 启动 `local.py dev`，使 `go run` 把产物放到 TMPDIR 的
`go-build<随机数>`（`go_cache()` 返回的全新缓存目录不在其 parents 中，只能靠新前缀分支通过）：

```text
listener pid=30628
server  30628 gaosper  txt    REG   1,17 19380914 ... /private/var/folders/w2/bntp6mgj2nn81dbr0cc2nnw00000gn/T/go-build195384386/b001/exe/server
```

在同一 GOCACHE 下再跑一次 `python3 -u scripts/local.py dev`：

```text
stale-server: stopping project server pid=30628
stale-server: port 8081 cleared
2026/10/05 22:19:06 INFO [HTTP] server listening addr=127.0.0.1:8081
30628 gone
```

- 真实 TMPDIR `go-build195384386` 目录下的 server 进程被识别并清理，**未**报
  `STALE_SERVER_UNKNOWN_PROCESS_MANUAL`。
- 清理后端口：`8081: 0 listener(s)`、`3306: 1 listener(s)`、`6379: 2 listener(s)`，
  无 `scripts/local.py`/`go run ./cmd/server`/`exe/server` 残留。

## 未验证 / 边界

- 本机默认 GOCACHE 命中时 `go run` 走 GOCACHE 链接产物路径（闭环 A），TMPDIR
  `go-build<随机数>` 路径需用全新 GOCACHE 才复现（闭环 B）；前缀匹配对两种路径均兼容。
- 陌生进程拒绝、TMPDIR 非 go-build 前缀拒绝仅由 mock 单测覆盖，未真实验证线上他人进程。
- `stop_stale_server` 固定检测 8081，改 `POKER_HTTP_PORT`/YAML 端口时不清理该端口残留。
- 未 commit/push；未改 Go、`web/src`、config。
