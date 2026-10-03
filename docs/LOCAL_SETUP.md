# 本地环境与真实链路验证

> 端口迁移说明：当前操作使用 **http://127.0.0.1:8081/**。下方 8080 相关原始输出是
> 历史证据，未改写；最新端口契约、安全测试与真实链路见 PORT_8081_VERIFICATION.md。

## 存储切换（当前，2026-10-03）

- 存储已从项目隔离实例（MySQL 3307 / Redis 6380）切换到 **Homebrew 默认实例**
  （MySQL `127.0.0.1:3306`、Redis `127.0.0.1:6379`）。
- 真实地址与密码写在本地私有 `config/config.local.yaml`（mode 600、gitignored）的直连字段
  `data.mysql.dsn` / `data.redis.password`；**直连优先于 `*_env` 引用**，无需再从
  `.env.local` 提供 DSN/密码。
- `.env.local` 已清空旧隔离凭据（不再含 `POKER_MYSQL_DSN` / `POKER_REDIS_ADDR` /
  `POKER_REDIS_DB` 等），仅需文件存在；脚本对近空文件容错。
- 隔离实例已 `python3 scripts/local.py stop` 停止（3307/6380 关闭），`.local` 数据目录保留；
  `runtime_start/stop/status` 仅作历史隔离能力保留。
- 下方"隔离与配置安全"等章节的 3307/6380 描述属历史实现说明，当前有效路径以本节、
  [配置系统](CONFIGURATION.md) 与 [配置工具链](CONFIG_TOOLING.md) 为准。
- 存储切换后的真实验证（真 MySQL 3306 / Redis 6379）：`make test-scripts` 48/48、
  `make e2e` 9/9；原始安全输出见 [CONFIG_REFACTOR_VERIFICATION.md](CONFIG_REFACTOR_VERIFICATION.md)。

## 最终集成轮结果

当前磁盘重建、迁移、HTTP E2E 8/8（含 150 次断开）、真实 Chromium UI 6/6、脚本安全
32/32、Go 顶层测试 62 PASS / 1 SKIP、开发探测 1/1。最终 8080 已释放，隔离依赖保持
运行。完整两轮安全原始输出、contracts §7 24 场景逐条映射及未验证项均在
[FINAL_VERIFICATION.md](FINAL_VERIFICATION.md)。下方原轮次记录保留为历史，不冒充最新重跑。

`make dev-live` / `local.py dev-live` 仅在显式 action 内、load/validate 文件 false 后，
将当前子进程后端付费开关设为 true；不改 `.env.local`，普通 dev 与 E2E 不能被 ambient
付费环境绕过。Web 仍须 mode=live、正预算与费用确认；用户自行决定费用。本轮没有
启动 dev-live、没有真实 provider 调用。`make e2e-abort` 运行断开压力，不保证每次
触发 SQL 取消；确定性取消与真实存储错误不被吞掉的结论来自本轮 Go 单元。

现在日志保留完整上下文，仅脱敏凭据/DSN，不整行隐藏普通错误；最终输出全部保存在
FINAL_VERIFICATION.md，历史输出中的旧日志策略不代表当前行为。

## 当前进程身份修复轮（2026-10-03）

本轮先以 mock ps/file 运行 RED：22 项中 18 通过、4 项按预期失败，验证旧实现误接受
相似路径、伪造 signature 和伪装 executable。修正后最终 29/29 通过；真实隔离依赖
status/stop/start/status 通过，两个服务保持运行。本轮未读取 `.env.local` 值，未启动
HTTP、Go E2E 或浏览器。`make e2e-browser` 仅 dry-run，已指向已有 browser wrapper。
下方新章节保存本轮完整安全原始输出、逐项映射与剩余 TOCTOU 限制。

## 上一轮真实 HTTP 验证记录（非本轮重跑）

项目隔离 MySQL/Redis 保持运行；构建、迁移、7 项真实存储/Go HTTP E2E 通过。
脚本安全测试 18/18；开发模式 HTTP 探测 1/1，模型清单包含 24 个非 mock 模型，
付费关闭，未创建 live 游戏或调用 provider。浏览器 Playwright 尚未验证，不能用
HTTP E2E 代替浏览器结论。所有结果的完整安全命令输出见下文。

原 Homebrew 服务仍不能保持运行，并非可用服务：
- `/opt/homebrew/var/mysql` 日志分类明确命中 `MYSQL_DATA_VERSION_DOWNGRADE_FORBIDDEN`，
  包括 `MY-014061`；即已有数据版本不允许当前二进制降级。没有更改这些数据。
- `/opt/homebrew/var/log/redis.log` 错误类别为 `dlopen,failed,load,module`，并命中
  `PATH_MISSING`，即模块动态加载时存在缺失路径。具体模块/库名称未输出或进一步
  核实；不输出日志原文或配置值，没有卸载/改写模块配置。
- 两者目录当前可读写，不能继续将退出原因笼统归为目录权限。

## 最简运行

首次在项目根目录执行，前置要求 `web/package.json`、`web/package-lock.json` 已生成。
Go embedded 编译需要真实 `web/dist/index.html`，迁移不能在 dist 缺失时运行。

```sh
make build       # npm ci + 真实前端构建 + Go embedded 二进制；生成 dist
make setup       # 独立存储准备；已有配置不覆盖，重复调用保持幂等
make migrate     # 显式 Go 迁移
make dev         # 前台 HTTP：http://127.0.0.1:8081/
```

`make dev` 使用受保护配置，不需要 source/export DSN。Ctrl-C 停止自己的 Go 进程组。
setup 可在前端未完成时先准备存储；它不要求 dist，但 migrate/dev/test 必须先 build。
重复 setup 不重新 initialize、不改已有库/用户、不覆盖 `.env.local`。
`make probe-dev` 临时启动开发模式二进制，只读 health/models，输出计数后停止自己的
HTTP 进程；不打印模型目录内容、模型名称、凭据或 DSN。

```sh
make status      # 项目隔离进程身份、端口；不打印凭据
make stop        # 仅 SIGTERM 已核验的项目隔离进程；数据保留
make start       # 仅重新启动项目隔离依赖
make doctor      # 专用账号 MySQL SELECT 1；Redis AUTH/SELECT/PING；无写入
make diagnose    # 固定错误分类 + 已知路径/权限；无日志原文
make test-scripts
make test
make e2e
```

## 隔离与配置安全

- 优先使用现有 `brew services`；若依赖不能运行，仅用已安装的 binaries 创建项目
  `.local` fallback。`.local` 必须是当前用户所有、mode 700、非符号链接；已有权限
  不符合要求就停止，不自动 chmod/chown。
- 独立 MySQL data 在 `.local/mysql-data`，只在空目录执行 `initialize-insecure`，
  独立 socket `.local/mysql.sock`、TCP `127.0.0.1:3307`；禁用 mysqlx、general/slow
  query logs、binlog。不碰 Homebrew 数据、库或 root 认证，不 sudo、不重置已有 root。
  initialize-insecure 的新隔离 root 按授权保留初始本机认证，仅限本机受信用户开发；
  不是生产部署或多用户主机安全配置。
- 独立 Redis `127.0.0.1:6380`、DB 15、protected-mode，目录 `.local/redis-data`；
  只缓存公共 DTO，不启用持久化，不 flush、扫描或删除用户数据。
- 管理脚本不信任 metadata.signature/binary；身份从固定 RUNTIME 路径生成。
  Darwin KERN_PROCARGS2 读取 NUL 分隔完整 argv，仅解析 executable/argv，不解析或
  回传环境区；ps 只采集 UID、stat、启动时间，不采集 command。真实 executable
  同时由原生 executable 路径与 lsof txt 核验，不能只相信 argv[0]。
  MySQL datadir/socket/pid-file 完整参数与规范路径、bind/port、cwd 必须精确匹配。
  Redis 原始完整参数禁止任意 config 路径；改写 argv 后必须是识别的完整标题，并
  精确匹配真实 executable、固定 cwd 和该 PID 的唯一 loopback listener。
  无法可靠采集身份就拒绝；停止前再核验一次完整身份。停止等待只检查同一启动
  时间，zombie 视为已退出，不在退出过程中读取失效 argv，不 force-kill 数据库。
- 专用库 `texas_holdem`、专用用户 `texas_holdem_app@localhost`，只授予该库
  `SELECT,INSERT,UPDATE,CREATE,REFERENCES`（后三者中的 CREATE/REFERENCES 是当前迁移
  所需）；无全局权限、无 DROP/DELETE/ALTER。遇同名库/用户但无配置就停止，不接管。
- 密码由 secrets 的密码学随机源在本地进程生成；创建用户 SQL 走 mysql stdin，
  认证经子进程环境。不通过 shell/argv/工具输出传递凭据或 DSN。
- `.env.local` 受 `.gitignore` 覆盖，O_EXCL 防覆盖、O_NOFOLLOW、防错权限，mode 600。
  格式为允许字段的 `KEY=JSON字符串`；不执行 shell、变量插值、source。不要读取、
  粘贴或提交该文件。本轮二次 setup 验证了已有配置不覆盖。
- 应用地址只允许 127.0.0.1、MySQL 3306/3307 与 Redis 6379/6380。管理员连接只允许
  已列出的本机 socket。brew 管理员密码如需提供，仅从受保护进程环境读取一次；
  认证失败即停止，不重置/猜测密码，不继承给 Go/npm。
- **dev 与 E2E 不同**：dev 允许 Go 服务只读加载用户模型目录，优先显式
  `POKER_CATALOG_PATH`，否则存在的用户 opencode JSONC/JSON；脚本本身只检查路径
  存在性，不读取目录内容。目录不存在时回退 mock。受保护文件中的 `POKER_ALLOW_PAID_MODELS`
  必须 false；仅显式 dev-live 在验证后开启当前子进程，模型清单读取不会调用模型。
  E2E 强制 `scripts/mock-catalog.json` 空 provider，
  所有游戏 `mode=mock`、`model_ref=mock`、live budget=0，绝不加载用户模型配置。
- 原始子进程输出在本地处理，当前普通构建/测试错误保留全部上下文，只脱敏凭据/DSN；
  OS 管理错误只回传 errno；依赖日志诊断只回传错误类别、代码、已知文件路径与权限。
  不能将完全脱敏的未知错误当作成功。
- 当前目录不是 Git 仓库，只能核验 `.gitignore` 规则；没有 commit/push。

## 逐场景映射

源码为 `tests/e2e/run.py`，实际使用真 MySQL/Redis、真 Go 二进制与 loopback HTTP。

| 场景 | 目标 / 函数 | 本轮结果与断言 |
|---|---|---|
| 多手 mock | e2e-mock / test_mock_multihand | PASS：3 手完成，0 live 调用，mock action，筹码守恒，MySQL hands/actions 只读计数 |
| 公共/私牌隔离 | e2e-privacy / test_public_private_isolation | PASS：game/current 与真实 Redis 公共 DTO 递归检查私牌/牌堆/快照/凭据字段 |
| 暂停恢复停止 | e2e-controls / test_pause_resume_stop | PASS：暂停后 revision 稳定，恢复推进，停止后稳定 |
| 崩溃重启恢复 | e2e-recovery / test_restart_recovery | PASS：仅 SIGKILL 自己 Go 进程，重启同库 paused/recovery，不自动推进，再恢复 |
| Redis 运行期降级 | e2e-redis / test_redis_runtime_degradation | PASS：仅关闭测试自建代理；health 503，MySQL 仍推进、game degraded，真实 Redis 仍可 PING |
| 依赖启动失败 | e2e-dependencies / test_dependency_startup_failures | PASS：分别注入错误本地端口，固定存储错误、非零退出、HTTP 不暴露；不关闭用户服务 |
| embedded 资源 | e2e-embedded / test_embedded_resources | PASS：空 cwd 启动 embedded 二进制，无 sidecar；读取真实 HTML/JS/资源及 SPA fallback |
| 真浏览器 | e2e-browser / tests/e2e/browser/run_browser.py | 入口已接已有 wrapper；本轮仅 dry-run，未运行浏览器 |

E2E 发现已有活动游戏或 8080 被占用就停止，不接管用户进程/游戏。只通过公开 API
停止本轮创建的游戏；不删库、记录或缓存。断言失败后停止后续场景，清理失败单独报告。
脚本安全测试源码及内容为 `tests/e2e/test_local.py`，18 个命名函数覆盖解析、端口、
目录权限、PID 身份、开发目录/E2E 分离、付费关闭、已有 env 不覆盖及凭据不进 argv。

## 完整安全验证输出

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`。以下为实际命令输出。

### 首次 `python3 scripts/local.py setup`
```text
git: unavailable; checked .gitignore rules only
brew-service: mysql@8.4 launch failed
brew-service: redis launch failed
setup: brew dependencies unavailable; using isolated project runtime
runtime: isolated mysql initialized; existing brew data untouched
runtime: mysql running; loopback-port=3307
runtime: redis running; loopback-port=6380
init: dedicated database/user created; scoped grants; no root changes
env: present; owner=current-user; mode=600; local-only; paid-disabled
mysql: available; dedicated-user; dedicated-database
redis: available; loopback; db=15; no writes
frontend-dist: present
checks: passed=3 failed=0
```

### `make stop && make start && make status && make setup && make probe-dev`
```text
python3 scripts/local.py stop
runtime: redis stopped; data retained
runtime: mysql stopped; data retained
python3 scripts/local.py start
runtime: mysql running; loopback-port=3307
runtime: redis running; loopback-port=6380
python3 scripts/local.py status
runtime: mysql; identity=verified; port=3307; open=True
runtime: redis; identity=verified; port=6380; open=True
python3 scripts/local.py setup
git: unavailable; checked .gitignore rules only
env: already-present; not overwritten
runtime: mysql already-running; identity verified
runtime: redis already-running; identity verified
env: present; owner=current-user; mode=600; local-only; paid-disabled
mysql: available; dedicated-user; dedicated-database
redis: available; loopback; db=15; no writes
frontend-dist: present
checks: passed=3 failed=0
python3 scripts/local.py probe-dev
dev-probe: HTTP health=200; paid-disabled; model-count=25; real-model-count=24; provider-calls=0
dev-probe: catalog=user-catalog-read-by-server
dev-probe: tests=1 passed=1 failed=0
dev-probe: own HTTP process stopped; dependencies retained
```

### `make build && make test && make test-scripts`
```text
python3 scripts/local.py frontend
stage=npm-ci; exit=0; output=not-forwarded
stage=frontend-build; exit=0; output=not-forwarded
python3 scripts/local.py build
build: passed; exit=0
python3 scripts/local.py test
?   	texas-holdem/cmd/server	[no test files]
ok  	texas-holdem/internal/agent	0.445s
ok  	texas-holdem/internal/conf	0.304s
ok  	texas-holdem/internal/data	0.529s
ok  	texas-holdem/internal/poker	0.655s
ok  	texas-holdem/internal/server	0.369s
ok  	texas-holdem/internal/service	0.227s
?   	texas-holdem/web	[no test files]
test: passed; exit=0
python3 tests/e2e/test_local.py
test_child_env_removes_admin_and_inherited_config: PASS
test_dev_catalog_allowed_paid_still_disabled: PASS
test_duplicate_rejected: PASS
test_e2e_catalog_override_does_not_read_user_config: PASS
test_existing_runtime_permissions_not_changed: PASS
test_fallback_ports_allowed: PASS
test_json_env_roundtrip: PASS
test_mysql_password_only_in_child_environment: PASS
test_no_shell_expansion: PASS
test_nonlocal_mysql_rejected: PASS
test_nonlocal_redis_rejected: PASS
test_paid_models_rejected: PASS
test_permissions_and_symlink: PASS
test_pid_identity_mismatch_rejected: PASS
test_real_catalog_rejected: PASS
test_setup_preserves_existing_env: PASS
test_unknown_key_rejected: PASS
test_unknown_output_completely_redacted: PASS
scripts: tests=18 passed=18 failed=0
```

### `make migrate && make e2e && make status && make doctor`
```text
python3 scripts/local.py migrate
migrate: passed; exit=0
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
e2e: tests=7 passed=7 failed=0 blocked=0
python3 scripts/local.py status
runtime: mysql; identity=verified; port=3307; open=True
runtime: redis; identity=verified; port=6380; open=True
python3 scripts/local.py doctor
env: present; owner=current-user; mode=600; local-only; paid-disabled
mysql: available; dedicated-user; dedicated-database
redis: available; loopback; db=15; no writes
frontend-dist: present
checks: passed=3 failed=0
```

### 中途脚本验证失败与修正

首次更新 setup 后，旧单测没有 mock 新增的安全 load_env 阶段，因此记录为
`scripts: tests=13 passed=12 failed=1`（test_setup_preserves_existing_env）。修正测试
夹具后最终 18/18 通过。首次 status 只比较 Redis argv，因 Redis 改写进程标题而返回
`RUNTIME_PID_IDENTITY_MISMATCH_STOP`；没有杀进程。补充 executable/cwd 核验后，
上方真实 stop/start/status 通过。不得把中途失败当作通过证据。

### `make diagnose`（最终安全分类输出）
```text
python3 scripts/local.py diagnose
mysql: loopback-port-closed
redis: loopback-port-closed
path=/opt/homebrew; mode=0o755; owner-current=True; readable=True; writable=True
path=/opt/homebrew/var; mode=0o775; owner-current=True; readable=True; writable=True
path=/opt/homebrew/var/mysql; mode=0o755; owner-current=True; readable=True; writable=True
path=/opt/homebrew/var/log; mode=0o755; owner-current=True; readable=True; writable=True
path=/opt/homebrew/etc/redis.conf; mode=0o600; owner-current=True; readable=True; writable=True
mysql@8.4: launch-executable-exists=True
mysql@8.4: WorkingDirectory-parent-exists=True
redis: launch-executable-exists=True
redis: WorkingDirectory-parent-exists=True
redis: StandardErrorPath-parent-exists=True
redis: StandardOutPath-parent-exists=True
mysql: data-directory-exists=True
log-path=/opt/homebrew/var/mysql/GaosperdeMacBook-Pro.local.err
log-error-code=ERRNO: 0
log-error-code=MY-010020
log-error-code=MY-010116
log-error-code=MY-010119
log-error-code=MY-010159
log-error-code=MY-010334
log-error-code=MY-010910
log-error-code=MY-013576
log-error-code=MY-014061
log-error-code=MY-015015
log-error-code=MY-015016
log-classification: MYSQL_DATA_VERSION_DOWNGRADE_FORBIDDEN
log-classification: MYSQL_DATA_VERSION_DOWNGRADE_FORBIDDEN
log-classification: CANNOT_OPEN_FILE
log-classification: MYSQL_ABORTING
log-path=/opt/homebrew/var/log/redis.log
redis-error-categories=dlopen,failed,load,module
redis-error-categories=cannot
redis-error-categories=dlopen,failed,load,module
redis-error-categories=cannot
redis-error-categories=dlopen,failed,load,module
log-classification: MODULE_STAGE_SEEN
log-classification: PATH_MISSING
log-classification: MYSQL_ABORTING
log-path=/Users/gaosper/Documents/mycode/texas-holdem/.local/mysql-error.log
log-error-code=MY-010068
log-error-code=MY-010116
log-error-code=MY-010159
log-error-code=MY-010453
log-error-code=MY-010910
log-error-code=MY-010931
log-error-code=MY-013169
log-error-code=MY-013172
log-error-code=MY-013576
log-error-code=MY-013577
log-error-code=MY-013602
log-error-code=MY-015015
log-error-code=MY-015016
log-error-code=MY-015017
log-error-code=MY-015018
log-classification: MYSQL_READY_SEEN
log-path=/Users/gaosper/Documents/mycode/texas-holdem/.local/redis-error.log
log-classification: REDIS_READY_SEEN
```

## 本轮进程身份回归：场景、证据和限制

测试文件：`tests/e2e/test_local.py`。所有攻击/竞态场景使用 mock ps、原生 argv、lsof
文件路径与 os.kill；回归单测不向真实进程发送 signal。原 18 项测试保留，本轮新增
11 项，最终合计 29 项。

| 场景 | 测试函数 | 结果 |
|---|---|---|
| 同 binary、mysql-data-unrelated 后缀 | test_identity_mysql_path_suffix_rejected | RED 失败 → GREEN PASS |
| 伪造 metadata.signature 为陌生路径 | test_identity_forged_signature_rejected | RED 失败 → GREEN PASS |
| 陈旧 PID 指向不同真实 executable，argv 伪装合法 | test_identity_reused_pid_real_executable_rejected | RED 失败 → GREEN PASS |
| Redis 相似数据/config 路径与伪造元数据 | test_identity_redis_similar_config_path_rejected | RED 失败 → GREEN PASS |
| kernel executable 与 argv[0] 不一致 | test_identity_kernel_executable_spoof_rejected | GREEN PASS |
| 合法路径含空格，完整 argv 不拆错 | test_identity_legal_path_with_spaces_accepted | GREEN PASS |
| metadata 启动时间对应旧 PID 实例 | test_identity_pid_birth_metadata_reused_rejected | GREEN PASS |
| 无法采集原生 argv 时拒绝，不 signal | test_identity_unavailable_argv_rejected_no_signal | GREEN PASS |
| Redis 额外相似 config 文件参数，即使 cwd 正确 | test_identity_redis_similar_config_argument_rejected | GREEN PASS |
| 首次身份核验后 PID 身份变化，第二次拒绝 | test_stop_rechecks_identity_before_signal | GREEN PASS |
| zombie 已退出，不能视为可 signal 的活进程 | test_identity_zombie_is_not_live | GREEN PASS |

关键回归内容（完整代码在上述测试文件）：
```python
def test_identity_mysql_path_suffix_rejected(self):
    self.assert_identity_rejected(suffix="-unrelated")

def test_identity_forged_signature_rejected(self):
    self.assert_identity_rejected(suffix="-unrelated", forged=True)

def test_stop_rechecks_identity_before_signal(self):
    first = {"pid": 42, "started": ["fixture"], "identity": ("first",)}
    changed = {"pid": 42, "started": ["new-process"], "identity": ("different",)}
    with patch.object(local.Path, "is_dir", return_value=True), \
            patch.object(local, "private_directory"), \
            patch.object(local, "process_state", side_effect=[first, changed]), \
            patch.object(local.os, "kill") as kill:
        with self.assertRaises(local.SafeError):
            local.runtime_stop()
        kill.assert_not_called()
```

### RED：`make test-scripts`（修改身份实现前）
```text
python3 tests/e2e/test_local.py
test_child_env_removes_admin_and_inherited_config: PASS
test_dev_catalog_allowed_paid_still_disabled: PASS
test_duplicate_rejected: PASS
test_e2e_catalog_override_does_not_read_user_config: PASS
test_existing_runtime_permissions_not_changed: PASS
test_fallback_ports_allowed: PASS
test_identity_forged_signature_rejected: FAIL (details redacted)
test_identity_mysql_path_suffix_rejected: FAIL (details redacted)
test_identity_redis_similar_config_path_rejected: FAIL (details redacted)
test_identity_reused_pid_real_executable_rejected: FAIL (details redacted)
test_json_env_roundtrip: PASS
test_mysql_password_only_in_child_environment: PASS
test_no_shell_expansion: PASS
test_nonlocal_mysql_rejected: PASS
test_nonlocal_redis_rejected: PASS
test_paid_models_rejected: PASS
test_permissions_and_symlink: PASS
test_pid_identity_mismatch_rejected: PASS
test_real_catalog_rejected: PASS
test_setup_preserves_existing_env: PASS
test_unknown_key_rejected: PASS
test_unknown_output_completely_redacted: PASS
scripts: tests=22 passed=18 failed=4
make: *** [test-scripts] Error 1
```

### 最终 GREEN：`make test-scripts`
```text
python3 tests/e2e/test_local.py
test_child_env_removes_admin_and_inherited_config: PASS
test_dev_catalog_allowed_paid_still_disabled: PASS
test_duplicate_rejected: PASS
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
test_setup_preserves_existing_env: PASS
test_stop_rechecks_identity_before_signal: PASS
test_unknown_key_rejected: PASS
test_unknown_output_completely_redacted: PASS
scripts: tests=29 passed=29 failed=0
```

### 合法进程真实验证：`make status && make stop && make start && make status && make -n e2e-browser`
```text
python3 scripts/local.py status
runtime: mysql; identity=verified; port=3307; open=True
runtime: redis; identity=verified; port=6380; open=True
python3 scripts/local.py stop
runtime: redis stopped; data retained
runtime: mysql stopped; data retained
python3 scripts/local.py start
runtime: mysql running; loopback-port=3307
runtime: redis running; loopback-port=6380
python3 scripts/local.py status
runtime: mysql; identity=verified; port=3307; open=True
runtime: redis; identity=verified; port=6380; open=True
python3 tests/e2e/browser/run_browser.py
```

最后一行是 `make -n` dry-run 输出，**没有运行 browser wrapper**。

### 中途真实验证失败（未冒充通过）

初次 status 返回 `RUNTIME_CWD_MISMATCH_STOP`：mysqld 在启动后 chdir 到 datadir，
已将期望 cwd 修正为固定 mysql-data。随后 Redis status 返回
`RUNTIME_REDIS_REWRITTEN_ARGV_UNRECOGNIZED_STOP`：其标题使用 opt symlink 路径，
补充整段路径规范化比较，而不是放宽为子串匹配。首次真实 stop 在已验证并发送
Redis SIGTERM 后，退出中的 argv 失效，返回 `RUNTIME_ARGV_INSPECTION_FAILED`；
等待逻辑改为同一 PID 实例的 stat/启动时间，zombie 视为退出。修正后的两服务
完整 stop/start 输出如上，未删除数据、未修改 Homebrew 依赖。

### 剩余 TOCTOU 边界

Darwin 的 ps/sysctl/lsof 核验与 `os.kill` 不是同一个原子操作。本实现保存启动时间，
核验前后检查同一 UID/启动时间，并在 signal 前再次核验完整 executable/参数/路径，
任何已观察到的变化或采集失败均拒绝 signal。**最后一次核验后到 SIGTERM 前仍有
非零竞态窗口**，且 ps lstart 仅有秒级分辨率；不能宣称彻底消除同秒 PID 复用。
旧 metadata 没有 started 字段时，仅在完整固定路径身份全部匹配后接受；本轮实际
start 已将新 metadata 写成 PID + 启动时间。未采用 force-kill，也未对陌生 PID 发 signal。
