# 配置系统（YAML + 环境变量）

本文档定义服务配置的 YAML schema、加载优先级、CLI 公共契约与边界行为。字段为跨 lane 冻结约定。

## 1. 文件与解析顺序

解析顺序（后者覆盖前者）：

1. base YAML：由 `POKER_CONFIG_FILE` 指定；未设置则默认 `config/config.yaml`。
   `cmd/config` 也可用 `-conf PATH` 显式指定。
2. 同目录 `config.local.yaml`（可选，缺失不报错；路径固定为 base 同目录的 `config.local.yaml`）。
3. 白名单环境变量（仅非空值生效，作为显式覆盖）。

- 合并按「字段是否显式出现」进行，因此 base / local 中显式的 `false`、`0` 不会被丢弃。
- base `config/config.yaml` 进 git：**禁止写入任何明文密码/DSN**，只声明环境变量名。
- 本机私有 `config.local.yaml` 已被 gitignore，可写**直连**地址与密码（`data.mysql.dsn` /
  `data.redis.password`）；直连非空时优先于对应 `*_env` 引用。秘密只在进程内解析，不落盘、不输出。

`-conf` 优先于 `POKER_CONFIG_FILE`；两者都优先于默认 `config/config.yaml`。

### 1.1 本地覆盖示例

- 示例文件：`config/config.local.yaml.example`（提交到仓库）。
- 使用：`cp config/config.local.yaml.example config/config.local.yaml`，按需取消注释。
- 它只演示**最小安全的部分覆盖**，不重复完整默认 schema；`config.local.yaml` 缺失时按无覆盖处理。
- 覆盖语义：显式写出的 `false` / `0` 会覆盖 base 的 `true` / 非 0（存在即覆盖）。示例中以注释形式给出 `agents.allow_paid_models: false` 与 `data.redis.db: 0`。
- 本文件已被 gitignore，**可写本机真实直连秘密**：`data.mysql.dsn`（完整 DSN）与
  `data.redis.password`（密码）。base 则只保留 `dsn_env` / `password_env` 引用，禁止写明文。
- 直连与 env 引用二选一即可：直连非空优先；`dsn: ""` 等同未设置（回退 env），
  `password: ""` 等同无密码（不回退 env）。
- **本机所有服务地址（监听地址）只通过 `server.http.port` 变更**，`host` 保持 loopback。
- 修改配置后需**重启服务**才生效。
- `config.local.yaml` 是本地私有文件，必须被 git 忽略；`config.local.yaml.example` 必须提交（`.gitignore` 规则由另一 lane 维护）。

## 2. YAML Schema

```yaml
server:
  http:
    host: 127.0.0.1      # 监听主机
    port: 8081           # 监听端口
    timeout: 10s         # HTTP 超时（Go duration 字符串）
frontend:
  dev_port: 5173         # 前端开发端口（公共契约字段）
data:
  mysql:
    dsn: ""                          # 可选：完整直连 DSN（含密码），非空优先于 dsn_env
    dsn_env: POKER_MYSQL_DSN         # 存放 DSN 的环境变量名（默认即旧键）
  redis:
    addr: 127.0.0.1:6379             # Homebrew 默认端口
    db: 0                            # 本地默认 DB
    password: ""                     # 可选：直连密码，非空优先于 password_env；"" = 无密码
    password_env: POKER_REDIS_PASSWORD
agents:
  catalog_path: auto     # auto = 自动发现 opencode.jsonc / opencode.json
  request_timeout: 15s   # agent 请求超时
  max_output_tokens: 256 # agent 请求 max_tokens 限额
  allow_paid_models: false
  max_live_calls: 1000
```

严格解码规则（在 decode 之前先校验 YAML Node）：

- 每一层必须是 mapping；非 mapping（标量/序列）拒绝。
- 类型按 YAML tag 精确匹配：integer 仅 `!!int`，bool 仅 `!!bool`，string / duration 仅 `!!str`。`8083.9`、`"8083"`、`true`（期望 string）等一律拒绝。
- 显式 `null`（含 root null、section null、字段 null）拒绝，绝不当作 missing 落默认。
- 未知字段、重复键、多文档流一律拒绝，固定返回 `CONFIG_INVALID`，error 文本不回显输入原文。
- YAML 别名（`*alias`）与合并键（`<<`）明确不支持（避免 Node 循环），一律拒绝。
- base 空文件 / 全注释 / 空 root mapping 拒绝 `CONFIG_INVALID`；可选 `config.local.yaml` 缺失 / 全注释 / 空 mapping 视为 noop。

`host` 只接受裸合法 IP 或合法主机名，拒绝 path/scheme/userinfo/端口/空白（不修剪猜测）。
`dsn` / `password` 同样只接受 `!!str`：数字、布尔、序列、mapping 与显式 `null` 一律 `CONFIG_INVALID`。
`dsn: ""` 是合法的「未设置」语义（回退 `dsn_env`）；`password: ""` 是合法的「无密码」语义（不回退 `password_env`），二者均不报错。
`dsn_env` / `password_env` 必须匹配 `^[A-Za-z_][A-Za-z0-9_]*$`，值仅从对应环境变量在进程内读取。
`catalog_path` 显式空字符串拒绝；未填写才按 `auto` 自动发现。
`POKER_CONFIG_FILE` / `-conf` 显式空字符串拒绝，不回退环境或默认。

## 3. 环境变量白名单

| 变量 | 作用 |
| --- | --- |
| `POKER_CONFIG_FILE` | 指定 base 配置文件 |
| `POKER_HTTP_HOST` / `POKER_HTTP_PORT` / `POKER_HTTP_TIMEOUT` | 覆盖 `server.http.*` |
| `POKER_FRONTEND_PORT` | 覆盖 `frontend.dev_port` |
| `POKER_MYSQL_DSN` | MySQL DSN（默认 `data.mysql.dsn_env`，秘密） |
| `POKER_REDIS_ADDR` / `POKER_REDIS_DB` | 覆盖 `data.redis.addr` / `db` |
| `POKER_REDIS_PASSWORD` | Redis 密码（默认 `data.redis.password_env`，秘密） |
| `POKER_CATALOG_PATH` | 覆盖 `agents.catalog_path` |
| `POKER_ALLOW_PAID_MODELS` / `POKER_MAX_LIVE_CALLS` | 覆盖同名 agents 项 |
| `POKER_DECISION_TIMEOUT` / `POKER_MAX_OUTPUT_TOKENS` | 覆盖 `agents.request_timeout` / `max_output_tokens` |

`POKER_REDIS_ADDR/DB`、`POKER_CATALOG_PATH`、`POKER_ALLOW_PAID_MODELS`、`POKER_MAX_LIVE_CALLS` 为兼容旧键保留。

## 4. 安全错误码

| 码 | 含义 |
| --- | --- |
| `CONFIG_READ_FAILED` | base 文件缺失或不可读 |
| `CONFIG_INVALID` | YAML 未知字段 / 重复键 / 多文档 / 类型错误；环境变量非法 |
| `CONFIG_HOME_UNAVAILABLE` | 无法定位用户主目录（catalog `auto`） |
| `CONFIG_STORAGE_REQUIRED` | 服务路径缺少 MySQL DSN 或 Redis 地址 |

CLI 公共路径 `LoadPublic*` 不要求存储秘密。

## 5. CLI 公共契约（`cmd/config`）

```
go run ./cmd/config [-conf PATH]
```

只输出严格白名单 JSON（固定 5 键，stdout 一行 + 换行）：

```json
{"http_base_url":"http://127.0.0.1:8081","http_address":"127.0.0.1:8081","http_host":"127.0.0.1","http_port":8081,"frontend_port":5173}
```

- 不 import `web`/`server`，无 dist 构建产物依赖。
- 与服务使用同一 resolver；不要求 DB 秘密。
- 不输出 Redis / MySQL / catalog / 凭据；即使 `config.local.yaml` 写了 `dsn` / `password`，
  公共 JSON 也仍严格 5 键，绝不新增或泄露任何直连秘密字段。
- 出错时仅向 stderr 输出安全错误码，退出码非零，stdout 无内容。

## 6. 服务接线

- 监听地址：`config/server/http{host,port}` → `server.Options` → `khttp.Address`，真正用于绑定。
- 同源策略：仅允许配置端口的 `host:port`；当 host 为 loopback 时额外允许 `127.0.0.1:port` / `localhost:port` 别名。Origin 必须与请求 Host 完全一致，跨源仍拒绝。
- `server.http.timeout` 接入 Kratos HTTP 超时与读写超时。
- `agents.request_timeout` / `agents.max_output_tokens` 接入 agent 的请求超时与 `max_tokens`；`NewFactory` 保持旧签名，新增 `NewFactoryWithOptions`。
- `poker` / `service` 契约不变。

## 附录 A：验证证据（原始输出）

### A.1 构建 / 静态检查

```
$ go build ./...
BUILD_OK
$ go vet ./...
VET_OK
```

### A.2 全量单元测试

```
$ go test ./... -count=1
ok  	texas-holdem/cmd/config	0.173s
?   	texas-holdem/cmd/server	[no test files]
ok  	texas-holdem/internal/agent	0.458s
ok  	texas-holdem/internal/conf	0.403s
ok  	texas-holdem/internal/data	0.939s
ok  	texas-holdem/internal/poker	0.623s
ok  	texas-holdem/internal/server	0.395s
ok  	texas-holdem/internal/service	0.806s
?   	texas-holdem/web	[no test files]
```

### A.3 竞态检测

```
$ go test -race ./... -count=1
ok  	texas-holdem/cmd/config	1.184s
?   	texas-holdem/cmd/server	[no test files]
ok  	texas-holdem/internal/agent	1.554s
ok  	texas-holdem/internal/conf	1.481s
ok  	texas-holdem/internal/data	1.595s
ok  	texas-holdem/internal/poker	1.666s
ok  	texas-holdem/internal/server	1.788s
ok  	texas-holdem/internal/service	2.087s
?   	texas-holdem/web	[no test files]
```

### A.4 定向详细测试

```
$ go test ./internal/conf/ ./internal/server/ ./internal/agent/ ./cmd/config/ -v -count=1
# 结果：132 个 PASS 子测试，0 个 FAIL
```

### A.5 CLI 真实调用

```
$ go run ./cmd/config
{"http_base_url":"http://127.0.0.1:8081","http_address":"127.0.0.1:8081","http_host":"127.0.0.1","http_port":8081,"frontend_port":5173}
exit=0

$ go run ./cmd/config -conf config/config.yaml
{"http_base_url":"http://127.0.0.1:8081","http_address":"127.0.0.1:8081","http_host":"127.0.0.1","http_port":8081,"frontend_port":5173}
exit=0

$ POKER_HTTP_PORT=8282 go run ./cmd/config
{"http_base_url":"http://127.0.0.1:8282","http_address":"127.0.0.1:8282","http_host":"127.0.0.1","http_port":8282,"frontend_port":5173}
exit=0

$ go run ./cmd/config -conf /tmp/definitely-absent-config.yaml
CONFIG_READ_FAILED
exit status 1
```

### A.6 场景 → 测试函数 → 结果

| 场景 | 测试函数 | 结果 |
| --- | --- | --- |
| YAML 端口 8083 驱动监听地址与公共契约 | `conf.TestYAMLConfigWiringAndPublicWhitelist` | PASS |
| local 显式 `false` / `0` 不被丢弃 | `conf.TestLocalOverlayPreservesExplicitFalseAndZero` | PASS |
| 环境变量覆盖（兼容旧键 + 新键） | `conf.TestEnvOverridePriority` | PASS |
| 指定文件路径 + 同目录 local | `conf.TestExplicitFilePathAndSiblingLocal` | PASS |
| 坏 YAML（未知字段/重复键/多文档/类型）安全且不回传原文 | `conf.TestSafeConfigurationErrors` | PASS |
| 缺文件 / 非法 env | `conf.TestMissingBaseAndInvalidEnv` | PASS |
| 公共路径不要求 DB 秘密且不泄露 | `conf.TestPublicDoesNotRequireStorageSecret` | PASS |
| catalog auto 发现 jsonc/json | `conf.TestDefaultCatalogDiscoveryAndLoader` | PASS |
| 显式缺失 catalog 路径不被兜底 | `conf.TestExplicitMissingCatalogPath` | PASS |
| 动态端口 8083 白名单变化、旧 8081 拒绝 | `server.TestDynamicListenerAndOriginWhitelist` | PASS |
| 8081 同源安全契约保持 | `server.TestPort8081SecurityContract` | PASS |
| HTTP 超时接线 | `server.TestDynamicListenerAndOriginWhitelist`（`ReadTimeout`/`WriteTimeout`） | PASS |
| agent 超时 / max_tokens 接线 | `agent.TestFactoryOptionsWireTimeoutAndTokens` | PASS |
| CLI 白名单 JSON | `cmd/config.TestRunPublicWhitelistJSON` | PASS |
| CLI env 优先 | `cmd/config.TestRunEnvPriority` | PASS |
| CLI 错误码 / 非零退出 / stdout 空 | `cmd/config.TestRunSafeErrors` | PASS |

### A.8 本地覆盖示例验证（合成 temp fixture）

不触碰用户 `config.local.yaml`、不读 `.env.local`、不发真实 API；使用 mktemp 临时目录构造 base + local。

```
$ ls -l config/config.local.yaml.example docs/CONFIGURATION.md
-rw-r--r--@ 1 gaosper  staff  1773 ... config/config.local.yaml.example
-rw-r--r--@ 1 gaosper  staff  8958 ... docs/CONFIGURATION.md

$ wc -l config/config.local.yaml.example
      39 config/config.local.yaml.example
# 有效（非注释、非空）行数：0（全注释，合法空文档）

# 用例1：example 原样复制为 temp local（全注释=无覆盖）
$ go run ./cmd/config -conf "$TMP/config.yaml"
{"http_base_url":"http://127.0.0.1:8081","http_address":"127.0.0.1:8081","http_host":"127.0.0.1","http_port":8081,"frontend_port":5173}
exit=0

# 用例2：合成激活覆盖（server.http.port:8083 / data.redis.db:0 / agents.allow_paid_models:false）
$ go run ./cmd/config -conf "$TMP/config.yaml"
{"http_base_url":"http://127.0.0.1:8083","http_address":"127.0.0.1:8083","http_host":"127.0.0.1","http_port":8083,"frontend_port":5173}
exit=0
```

计数：示例文件 39 行、0 行有效配置；strict resolver 两次调用均 exit 0（1/1 接受示例、1/1 应用覆盖）。

### A.7 未验证项

- 真实 `make dev` 端到端监听与前端联调：未验证（本 lane 不启动共享 HTTP 端口，留给工具集成 lane）。
- `config.local.yaml` 的 `.gitignore` 忽略：由另一 lane 负责，本 lane 未改动 `.gitignore`。
- 真实 MySQL/Redis 凭据链路：未验证（不读/改实际凭据文件，未启动付费模型）。

## 附录 B：严格 schema 校验修复（arc-2 阻断项）

修复 4 项阻断：`port` 小数被截断、`host` 布尔/整数被转 string、字段 `null` 落默认、`catalog_path: ""` 当 auto、显式 `-conf ""` 当未指定。

方案：decode 前先解析为 YAML Node 并做 schema/type-tag 校验（mapping 层、`!!int`/`!!bool`/`!!str`、拒绝 null 与 alias/合并键、拒绝空 base），再 decode 到结构体。显式 null 不再被当作 missing。

### B.1 RED 复现（修复前）

```
$ go test ./internal/conf/ -run 'TestStrictSchemaRejection|TestHostValidation|TestDiscoverPathExplicitEmptyRejected' -count=1
# texas-holdem/internal/conf [texas-holdem/internal/conf.test]
internal/conf/strict_test.go:116:14: undefined: validHost
internal/conf/strict_test.go:217:15: assignment mismatch: 2 variables but discoverPath returns 1 value
internal/conf/strict_test.go:221:15: assignment mismatch: 2 variables but discoverPath returns 1 value
internal/conf/strict_test.go:225:15: assignment mismatch: 2 variables but discoverPath returns 1 value
FAIL	texas-holdem/internal/conf [build failed]
FAIL

$ go test ./cmd/config/ -run TestRunExplicitEmptyConfRejected -count=1 -v
=== RUN   TestRunExplicitEmptyConfRejected
    main_test.go:97: args [-conf ] got <nil> want CONFIG_READ_FAILED (must not fall back)
--- FAIL: TestRunExplicitEmptyConfRejected (0.00s)
FAIL	texas-holdem/cmd/config	3.056s
FAIL
```

### B.2 GREEN 完整验证

```
$ go vet -mod=readonly ./...
VET_EXIT=0

$ go test -mod=readonly -race -count=1 ./...
ok  	texas-holdem/cmd/config	1.174s
ok  	texas-holdem/cmd/server	4.300s
ok  	texas-holdem/internal/agent	4.341s
ok  	texas-holdem/internal/conf	5.010s
ok  	texas-holdem/internal/data	4.764s
ok  	texas-holdem/internal/poker	5.456s
ok  	texas-holdem/internal/server	5.415s
ok  	texas-holdem/internal/service	4.681s
?   	texas-holdem/web	[no test files]
```

定向详细（`internal/conf` + `cmd/config` + `cmd/server`，`-v -count=1`）：

```
EXIT=0
PASS_lines=111
FAIL_lines=0
```

### B.3 场景 → 测试函数 → 结果

| 场景 | 测试函数 | 结果 |
| --- | --- | --- |
| 小数 port / 引号 int / 类型不符拒绝 | `conf.TestStrictSchemaRejection/{float_port,quoted_port,string_int,float_int,int_timeout}` | PASS |
| 字段 null / section null / root null 拒绝 | `conf.TestStrictSchemaRejection/{null_port,null_host,null_timeout,null_section,null_root}` | PASS |
| host 布尔/整数转 string 拒绝 | `conf.TestStrictSchemaRejection/{bool_host,int_host}` | PASS |
| 非 mapping 层拒绝 | `conf.TestStrictSchemaRejection/{non_mapping_section,non_mapping_nested,non_mapping_data}` | PASS |
| root 标量/序列/空 mapping 拒绝 | `conf.TestStrictSchemaRejection/{scalar_root,sequence_root,empty_mapping}` | PASS |
| base 空文件/全注释拒绝 | `conf.TestStrictSchemaRejection/{empty_file,comments_only}` | PASS |
| 未知字段/重复键/多文档拒绝且不回显 | `conf.TestStrictSchemaRejection/{unknown_field,unknown_section,duplicate_key,multi_document}` | PASS |
| YAML 别名/合并键拒绝 | `conf.TestStrictSchemaRejection/{merge_key,alias_value}` | PASS |
| 合法标量（db:0 / false / 10s / 8083）接受 | `conf.TestValidScalarTypesAccepted` | PASS |
| host：合法 IP/hostname 通过，path/scheme/port/userinfo/空白拒绝 | `conf.TestHostValidation` | PASS |
| dsn_env 坏名字拒绝、合法自定义名从对应 env 读取 | `conf.TestSecretEnvNameValidation` | PASS |
| catalog_path 显式空拒绝 | `conf.TestCatalogPathExplicitEmptyRejected` | PASS |
| local 全注释/空 mapping noop；root null/字段 null/未知字段/alias 拒绝 | `conf.TestLocalOverlaySemantics` | PASS |
| 显式 POKER_CONFIG_FILE 空拒绝、不回退默认 | `conf.TestDiscoverPathExplicitEmptyRejected` | PASS |
| LoadFile/LoadPublicFile 空拒绝 | `conf.TestLoadFileEmptyRejected` | PASS |
| CLI 显式 `-conf ""` / `-conf=` 拒绝，不回退 | `cmd/config.TestRunExplicitEmptyConfRejected` | PASS |
| server `-conf` 解析与显式空拒绝 | `cmd/server.TestParseArgs` / `cmd/server.TestResolveConfigExplicitEmptyRejected` | PASS |

### B.4 新增源码函数 / 测试函数

源码（`internal/conf/config.go`）：`readRoot`、`loadFile(path, required)`、`validateNode`、`expectScalar`、`validHost`、`envNamePattern`、`configSchema`、`discoverPath() (string,error)`。
源码（`cmd/config/main.go`）：`run` 使用 `flag.Visit` 区分显式空。
源码（`cmd/server/main.go`）：`parseArgs`、`resolveConfig`。
测试：`internal/conf/strict_test.go`（`TestStrictSchemaRejection`、`TestValidScalarTypesAccepted`、`TestHostValidation`、`TestSecretEnvNameValidation`、`TestCatalogPathExplicitEmptyRejected`、`TestLocalOverlaySemantics`、`TestDiscoverPathExplicitEmptyRejected`、`TestLoadFileEmptyRejected`）；`cmd/config/main_test.go`（`TestRunExplicitEmptyConfRejected`）；`cmd/server/main_test.go`（`TestParseArgs`、`TestResolveConfigExplicitEmptyRejected`）。

> 说明：以上为追加内容，附录 A 的历史输出未改动。YAML 别名与合并键均明确不支持。

## 附录 C：本地直连秘密字段（`data.mysql.dsn` / `data.redis.password`）

### C.1 背景与语义

- 本地开发允许在 **被 gitignore 的** `config/config.local.yaml` 里直连 MySQL/Redis 真实地址与密码：
  - `data.mysql.dsn`：完整 DSN（含密码），非空时优先于 `data.mysql.dsn_env`。
  - `data.redis.password`：直连密码，非空时优先于 `data.redis.password_env`。
- `config/config.yaml` 进 git，保持无密码：仍用 `dsn_env: POKER_MYSQL_DSN` / `password_env: POKER_REDIS_PASSWORD`。
- 显式空串语义（不报错）：`dsn: ""` 等同未设置（回退 env）；`password: ""` 等同无密码（不回退 env）。
- 严格校验保留：`dsn` / `password` 只接受 `!!str`，数字/布尔/序列/mapping/显式 `null` 一律 `CONFIG_INVALID`。
- 公共白名单 CLI 仍严格 5 键，`Public` 结构未扩展，密码/DSN 不进入任何日志、错误或公共 JSON。
- base `config/config.yaml` 的 `data.redis.addr` 由 `127.0.0.1:6380` → `127.0.0.1:6379`、`db` 由 `15` → `0`（Homebrew 默认）。默认常量同步为 `127.0.0.1:6379` / `0`。

### C.2 RED（实现前，先写测试）

```
$ go test -mod=readonly -count=1 ./internal/conf ./cmd/config ./cmd/server
--- FAIL: TestDirectSecretOverridesEnv (0.00s)
    direct_secret_test.go:59: CONFIG_INVALID
--- FAIL: TestDirectSecretEmptyStringSemantics (0.00s)
    direct_secret_test.go:86: CONFIG_INVALID
--- FAIL: TestDirectSecretEmptyStringAccepted (0.00s)
    direct_secret_test.go:165: empty direct secret rejected: CONFIG_INVALID
--- FAIL: TestPublicWhitelistExcludesDirectSecrets (0.00s)
    direct_secret_test.go:185: CONFIG_INVALID
FAIL
FAIL	texas-holdem/internal/conf	3.364s
--- FAIL: TestRunPublicWhitelistExcludesDirectSecrets (0.02s)
    main_test.go:154: CONFIG_INVALID
FAIL
FAIL	texas-holdem/cmd/config	3.210s
--- FAIL: TestResolveConfigLoadsDirectSecrets (0.00s)
    main_test.go:73: CONFIG_INVALID
FAIL
FAIL	texas-holdem/cmd/server	3.241s
FAIL
```

失败原因：实现前 schema 未声明 `dsn` / `password`，作为未知字段被严格拒绝（`CONFIG_INVALID`）。

### C.3 GREEN（实现后完整原始输出）

```
$ go vet -mod=readonly ./...
VET_EXIT=0

$ go test -mod=readonly -race -count=1 ./internal/conf ./cmd/config ./cmd/server
ok  	texas-holdem/internal/conf	1.430s
ok  	texas-holdem/cmd/config	1.362s
ok  	texas-holdem/cmd/server	1.259s
TEST_EXIT=0
```

定向详细（`-v`）：

```
$ go test -mod=readonly -race -count=1 -v ./internal/conf ./cmd/config ./cmd/server
PASS_lines=32
FAIL_lines=0
ok  	texas-holdem/internal/conf	1.403s
ok  	texas-holdem/cmd/config	1.191s
ok  	texas-holdem/cmd/server	1.354s
```

### C.4 场景 → 测试函数 → 结果

| 场景 | 测试函数 | 结果 |
| --- | --- | --- |
| local 直连 dsn/password 覆盖 base 的 env 引用 | `conf.TestDirectSecretOverridesEnv` | PASS |
| 显式空串语义：`dsn:""` 回退 env、`password:""` 无密码 | `conf.TestDirectSecretEmptyStringSemantics` | PASS |
| base 仅 env 引用（无直连）正常解析 | `conf.TestEnvOnlyFallbackWithoutDirect` | PASS |
| 直连与 env 都无：缺 DSN 报 `CONFIG_STORAGE_REQUIRED`，解析器层为空 | `conf.TestSecretsAbsentBehaveClearly` | PASS |
| 新字段 strict 类型/null（null/int/bool/seq/mapping 拒绝） | `conf.TestDirectSecretStrictTypes`（8 子用例） | PASS |
| 空串是合法 string（不因此报错） | `conf.TestDirectSecretEmptyStringAccepted`（2 子用例） | PASS |
| 公共路径严格 5 键、不泄露 dsn/password | `conf.TestPublicWhitelistExcludesDirectSecrets` | PASS |
| CLI JSON 严格 5 键、不回显直连秘密 | `cmd/config.TestRunPublicWhitelistExcludesDirectSecrets` | PASS |
| 服务路径加载直连 dsn/password（不启 HTTP、不连库） | `cmd/server.TestResolveConfigLoadsDirectSecrets` | PASS |

### C.5 变更清单

- `internal/conf/config.go`：`fileConfig.Data.MySQL.DSN`（`yaml:"dsn"`）、`fileConfig.Data.Redis.Password`（`yaml:"password"`）；`Config.dsnDirect` / `passwordDirect` / `passwordPresent`；`configSchema` 增加 `dsn` / `password`；`mergeFile` / `applyFile` / `finalize` 处理优先级；默认 Redis 常量 `127.0.0.1:6379` / `0`。导出字段 `Config.MySQLDSN` / `Config.RedisPassword`、`Public` 结构均不变。
- `config/config.yaml`：`data.redis.addr: 127.0.0.1:6379`、`data.redis.db: 0`；`dsn_env` / `password_env` 不变。
- `config/config.local.yaml.example`：新增直连 `data.mysql.dsn` / `data.redis.password` 注释示例与占位符说明。
- `cmd/config/main.go`、`cmd/server/main.go`：无改动（白名单契约不变）。
- 测试：`internal/conf/direct_secret_test.go`（新增）；`cmd/config/main_test.go`、`cmd/server/main_test.go`（追加）。

`config.local.yaml` 中用户应填的确切字段名（占位，非真实值）：

```yaml
data:
  mysql:
    dsn: "<MYSQL_DSN_PLACEHOLDER>"     # 如 root:<PASSWORD>@tcp(127.0.0.1:3306)/poker?parseTime=true
  redis:
    addr: 127.0.0.1:6379
    db: 0
    password: "<REDIS_PASSWORD_PLACEHOLDER>"
```

### C.6 未验证项

- 真实 MySQL / Redis 连通（含密码鉴权）：未验证（未连接真实数据库、未启动 HTTP 服务）。
- `.gitignore` 对 `config.local.yaml` 的忽略规则：由另一 lane 维护，本次未改动。
