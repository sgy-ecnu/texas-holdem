# HTTP 8081 迁移验证

当前任务仅迁移 HTTP 为 `127.0.0.1:8081`；MySQL 3307 / Redis 6380 不变。端口占用预检：
```text
port8081: available
```
本轮仅访问已有本地依赖，未调用付费 provider。先执行端口契约 RED，再迁移生产配置并
执行 GREEN：端口安全 8/8 子场景通过、默认配置 1/1；指定 readonly race 全仓测试与
vet 退出 0（6 个含测试包 PASS、2 个无测试包）；脚本安全 33/33、HTTP E2E 9/9、真实
Chromium UI 6/6。预期 RED 失败完整保留，GREEN 后无失败阶段。

当前网址：**http://127.0.0.1:8081/**。在项目根目录运行 `make dev`，Ctrl-C 退出。
测试启动的 HTTP 已退出，最终 8081 空闲；项目 MySQL 3307 / Redis 6380 保留运行。

## 变更与边界

- `internal/conf/config.go`、`config_test.go`：默认地址 8081 与回归。
- `internal/server/http.go`、`http_test.go`、`read_cancel_test.go`：监听地址/Host、请求
  Origin 同源契约、旧端口与邻端口负例；读取消测试的请求来源机械迁移。
- `scripts/local.py`：8081 preflight/probe/退出检查；新增 `verify-port8081` 保存完整安全输出。
- `tests/e2e/run.py`、`test_local.py`：8081 原始 socket/HTTP 基址，新增真实 Host/Origin
  场景，以及占用 8081 不启动、不发送 signal 的 mock 回归。
- `tests/e2e/browser/run_browser.py`、`playwright.config.ts`：wrapper/baseURL/清理端口 8081。
- `web/vite.config.ts`：仅机械修改 target/转发 Origin 与注释，未修改 UI 或设计。
- `README.md`、`docs/contracts.md`：当前地址及拒绝旧端口的契约。
- `docs/FINAL_VERIFICATION.md`、`BROWSER_VERIFICATION.md`、`LOCAL_SETUP.md`：只新增
  旧端口历史说明，原始输出未改写；LOCAL_SETUP 的当前操作 URL 更新为 8081。
- 新增本文；没有改业务逻辑/依赖、`.env.local`、用户模型配置、数据库/Redis 端口；
  不 commit/push，不 kill 陌生进程，不删除数据。

## 定向扫描与端口安全场景映射

最终定向核对活动 cmd/internal Go、scripts Python、E2E/browser Python/TS、Vite config、README
及 contracts，AST glob 明确排除 node_modules、.local、.env、日志、bin/dist。初次目录 grep
意外列出了 node_modules 的端口示例声明，随后按排除规则重新核对，未修改这些文件。
最终活动默认监听/基址中
无 8080，剩余 8080/8082 引用为明确的拒绝负例；历史文档原始输出中的旧地址不替换。

| 场景 | 测试函数 / 配置 | 证据命令 | 结果 |
|---|---|---|---|
| 默认监听 127.0.0.1:8081 | conf.TestDefaultsAndSafeConfiguration；server.TestPort8081SecurityContract | GREEN 定向 go test；全仓 readonly race | PASS（RED 先失败） |
| 127.0.0.1:8081 Host + 同源 Origin | TestPort8081SecurityContract/loopback_8081_same_origin | GREEN 定向 go test | PASS 201 |
| localhost:8081 Host + 同源 Origin | TestPort8081SecurityContract/localhost_8081_same_origin | GREEN 定向 go test | PASS 201 |
| 旧 loopback Host 8080 | TestPort8081SecurityContract/old_loopback_host_8080 | GREEN 定向 go test | PASS 403，不进入 service |
| 旧 localhost Host 8080 | TestPort8081SecurityContract/old_localhost_host_8080 | GREEN 定向 go test | PASS 403，不进入 service |
| 相邻 Host 8082 | TestPort8081SecurityContract/neighbor_host_8082 | GREEN 定向 go test | PASS 403 |
| 正确 Host + 旧 Origin 8080 | TestPort8081SecurityContract/old_origin_8080 | GREEN 定向 go test | PASS 403 |
| 正确 Host + 相邻 Origin 8082 | TestPort8081SecurityContract/neighbor_origin_8082 | GREEN 定向 go test | PASS 403 |
| Host/Origin hostname 不同 | TestPort8081SecurityContract/hostname_mismatch | GREEN 定向 go test | PASS 403 |
| 8081 已占用时拒绝，不启动/kill | test_probe_8081_in_use_refuses_without_signal | make test-scripts | PASS：mock port/Popen/kill |
| 真 HTTP 8081 上旧 Host/Origin 仍拒绝 | test_http_8081_host_origin | make e2e | PASS：9 个真实 HTTP 请求，200/403/400 边界 |
| 多手 mock、公共 DTO、暂停恢复停止 | test_mock_multihand、test_public_private_isolation、test_pause_resume_stop | make e2e | PASS：真 MySQL/Redis/Go HTTP 8081 |
| 崩溃恢复、Redis 降级、启动依赖失败 | test_restart_recovery、test_redis_runtime_degradation、test_dependency_startup_failures | make e2e | PASS：不停止用户依赖，只关闭自己的 Redis 代理/注入坏端口 |
| embedded 资源与断开轮询 | test_embedded_resources、test_http_aborted_polling | make e2e | PASS：150 次断开；不声称每次触发 SQL 取消 |
| 真 Chromium UI 配置与三手完成 | scenarios.spec.ts scenarios 1、2 | make e2e-browser | PASS，Go HTTP 8081，无 Vite/替代牌局 fixture |
| 真 UI 暂停/刷新/恢复/停止与隐私 | scenarios.spec.ts scenarios 3、6 | make e2e-browser | PASS |
| Live 禁用与费用 UI；手机宽控制 | scenarios.spec.ts scenarios 4、5 | make e2e-browser | PASS：全部牌局 mock；费用确认 UI 分支单独拦截 health，不证明付费调用 |
| Vite proxy target + Origin 为 8081 | web/vite.config.ts BACKEND_ORIGIN | make build + 活动配置定向核对 | PASS：机械修改/构建；Vite dev proxy 真请求未另行运行 |

## 未验证项与可复现步骤

未启动 dev-live、未调用真实 provider；未单独运行 Vite dev proxy 请求链路（只改 target /
Origin 并构建）。这些不作为本轮通过结论。Go race 不代替真实链路，实际 E2E/browser
证据单独保存在下方。npm allow-scripts warning 原样保留，未擅自批准脚本。

完整复现：`python3 scripts/local.py verify-port8081`。它先确认 8081 未占用，按下方
命令串行执行（含用户指定的原始 readonly race 命令），完整安全输出追加到本文。
日志仅凭据/DSN 脱敏，不截断失败；失败的依赖阶段会停止后续链路，不能算通过。
历史端口输出分别保存在原 FINAL/BROWSER/LOCAL_SETUP 中，不把 8080 成功伪造成 8081。

## RED：修改生产监听前

命令：`go test -mod=readonly -count=1 -v ./internal/conf ./internal/server -run 'TestDefaultsAndSafeConfiguration|TestPort8081SecurityContract'`

```text
=== RUN   TestDefaultsAndSafeConfiguration
    config_test.go:52: unsafe defaults
--- FAIL: TestDefaultsAndSafeConfiguration (0.00s)
FAIL
FAIL	texas-holdem/internal/conf	3.119s
=== RUN   TestPort8081SecurityContract
    http_test.go:53: listener address got 127.0.0.1:8080 want 127.0.0.1:8081
=== RUN   TestPort8081SecurityContract/loopback_8081_same_origin
    http_test.go:73: status got 403 want 201
=== RUN   TestPort8081SecurityContract/localhost_8081_same_origin
    http_test.go:73: status got 403 want 201
=== RUN   TestPort8081SecurityContract/old_loopback_host_8080
    http_test.go:73: status got 201 want 403
    http_test.go:74: rejected origin reached service
=== RUN   TestPort8081SecurityContract/old_localhost_host_8080
    http_test.go:73: status got 201 want 403
    http_test.go:74: rejected origin reached service
=== RUN   TestPort8081SecurityContract/neighbor_host_8082
=== RUN   TestPort8081SecurityContract/old_origin_8080
=== RUN   TestPort8081SecurityContract/neighbor_origin_8082
=== RUN   TestPort8081SecurityContract/hostname_mismatch
--- FAIL: TestPort8081SecurityContract (0.00s)
    --- FAIL: TestPort8081SecurityContract/loopback_8081_same_origin (0.00s)
    --- FAIL: TestPort8081SecurityContract/localhost_8081_same_origin (0.00s)
    --- FAIL: TestPort8081SecurityContract/old_loopback_host_8080 (0.00s)
    --- FAIL: TestPort8081SecurityContract/old_localhost_host_8080 (0.00s)
    --- PASS: TestPort8081SecurityContract/neighbor_host_8082 (0.00s)
    --- PASS: TestPort8081SecurityContract/old_origin_8080 (0.00s)
    --- PASS: TestPort8081SecurityContract/neighbor_origin_8082 (0.00s)
    --- PASS: TestPort8081SecurityContract/hostname_mismatch (0.00s)
FAIL
FAIL	texas-holdem/internal/server	3.141s
FAIL
```

退出码：1（预期 RED）。顶层 2 FAIL；端口安全子场景 4 FAIL / 4 PASS。

## `GREEN port contract`

```text
=== RUN   TestDefaultsAndSafeConfiguration
--- PASS: TestDefaultsAndSafeConfiguration (0.00s)
PASS
ok  	texas-holdem/internal/conf	3.066s
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
PASS
ok  	texas-holdem/internal/server	3.182s
```

退出码：0

## `go test -mod=readonly -race -count=1 ./...`

```text
?   	texas-holdem/cmd/server	[no test files]
ok  	texas-holdem/internal/agent	1.586s
ok  	texas-holdem/internal/conf	1.204s
ok  	texas-holdem/internal/data	1.275s
ok  	texas-holdem/internal/poker	1.775s
ok  	texas-holdem/internal/server	1.763s
ok  	texas-holdem/internal/service	1.762s
?   	texas-holdem/web	[no test files]
```

退出码：0

## `go vet ./...`

```text

```

退出码：0

## `make test-scripts`

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
test_probe_8081_in_use_refuses_without_signal: PASS
test_real_catalog_rejected: PASS
test_redaction_does_not_consume_next_test_line: PASS
test_safe_output_preserves_failure_and_redacts_credentials: PASS
test_setup_preserves_existing_env: PASS
test_stop_rechecks_identity_before_signal: PASS
test_unknown_key_rejected: PASS
scripts: tests=33 passed=33 failed=0
```

退出码：0

## `make build`

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
✓ built in 661ms
stage=frontend-build; exit=0; output=credential-redacted-only
python3 scripts/local.py build

build: passed; exit=0
```

退出码：0

## `make e2e`

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
  port-contract: requests=9; HTTP=8081; 8080/8082 Host and Origin rejected
port -> test_http_8081_host_origin: PASS
e2e: tests=9 passed=9 failed=0 blocked=0
```

退出码：0

## `make e2e-browser`

```text
python3 tests/e2e/browser/run_browser.py

Running 6 tests using 1 worker

  ✓  1 [chromium] › tests/scenarios.spec.ts:75:1 › scenario 1 · 配置 2/9 人且每座选择不同模型 (862ms)
  ✓  2 [chromium] › tests/scenarios.spec.ts:108:1 › scenario 2 · 创建/开始至 max_hands 并核对结束详情数值数组 (2.0s)
  ✓  3 [chromium] › tests/scenarios.spec.ts:142:1 › scenario 3 · 暂停 / 刷新找 current / 恢复 / 停止 (1.2s)
  ✓  4 [chromium] › tests/scenarios.spec.ts:167:1 › scenario 4 · live 禁用与费用确认门槛 (594ms)
  ✓  5 [chromium] › tests/scenarios.spec.ts:210:1 › scenario 6 · 运行中无底牌 / 完成后详情可揭示 (2.5s)
  ✓  6 [chromium] › tests/scenarios.spec.ts:251:3 › scenario 5 · 手机宽 390 可操作无遮挡 › 390 宽配置与运行控制可用 (745ms)

  6 passed (8.5s)
browser-wrapper: passed=6 failed=0 exit=0
browser-wrapper: screenshots=/Users/gaosper/Documents/mycode/texas-holdem/web/node_modules/.cache/browser-verify/20261003-185945
browser-wrapper: note=infra run.py 'browser' 仍为占位，本目录为独立可复现入口
browser-wrapper: own HTTP process stopped; dependencies retained
```

退出码：0

## `make status`

```text
python3 scripts/local.py status
runtime: mysql; identity=verified; port=3307; open=True
runtime: redis; identity=verified; port=6380; open=True
```

退出码：0

最终自己启动的 HTTP 8081 已退出：True
