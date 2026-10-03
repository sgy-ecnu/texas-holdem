# 配置工具链：YAML → 冻结 CLI → Python / Vite / Playwright

> 本文件是配置工具链的唯一说明文档：YAML schema、冻结 CLI 公共契约、优先级、env 安全、
> 场景映射与可复现步骤。端口/地址的默认值只在 Go `internal/conf` 定义一次，Python/Vite/
> Playwright 一律通过冻结 CLI 读取，禁止复制端口默认值。

## 0. 本轮状态（必读）

| 项 | 状态 |
|---|---|
| 冻结 CLI `go run ./cmd/config` | 已落地：严格 5 字段公共 JSON；失败/未匹配非零退出，消费侧不回退端口默认 |
| `config/config.yaml` | 已存在（默认 8081）；由 Go/config lane 维护，本 lane 不写 |
| Go 侧 `internal/conf` | 已存在：`Load/LoadPublic/LoadFile/LoadPublicFile`、`Public` 白名单、`POKER_CONFIG_FILE` 发现、白名单 env 覆盖 |
| Python 消费侧（`scripts/local.py`） | resolver + `service_env`；`probe_dev`/`verify_*` 全部按 CLI 派生，无 hardcode 活动逻辑 |
| `service_env` 配置注入 | 为所有 Go 子进程注入**绝对** `POKER_CONFIG_FILE`，支持非项目 cwd 运行嵌入资产 |
| Vite serve 解析（`web/vite.config.ts`） | serve 时解析 CLI；build 不触发（无 dist 环）；`web/` 已加 `@types/node`，无 `@ts-nocheck`，typecheck 0 error |
| 真实链路（默认 8081 + 临时仅改 YAML→8083） | e2e 9/9、Chromium 6/6、Vite dev proxy 4/4，两端口均通过；见 `CONFIG_REFACTOR_VERIFICATION.md` |
| `.env.local` | 未修改、未读取输出；仅注入 DSN/Redis 密码；非敏感旧值不盖过 YAML |

验收：**只改 YAML 的 `server.http.port`（8081→8083），不改任何代码**，Go 服务、Vite dev
proxy、E2E 与浏览器全部自动派生到 8083；已在 `docs/CONFIG_REFACTOR_VERIFICATION.md` 留证。

## 1. 唯一事实来源与冻结 CLI 契约

配置文件的解析与默认值全部由 Go `internal/conf` 负责；Python/Vite/Playwright 不加 YAML
库，只调用冻结 CLI 读取**白名单 JSON**：

```sh
go run ./cmd/config [-conf PATH]
```

- 默认 base：`config/config.yaml`；`POKER_CONFIG_FILE` 可指定；`-conf PATH` 显式指定。
- 不依赖 `web/dist`，不需要 DB 秘密，不输出任何 secret 或配置原文。
- 输出仅 5 个字段（严格白名单，禁止扩展）：

```json
{
  "http_base_url": "http://127.0.0.1:8081",
  "http_address": "127.0.0.1:8081",
  "http_host": "127.0.0.1",
  "http_port": 8081,
  "frontend_port": 5173
}
```

消费侧（`scripts/local.py`）对返回做严格校验：字段集合必须完全一致、host 仅允许
`127.0.0.1`/`localhost`、`http_address == host:port`、`http_base_url == http://http_address`。
任一不满足即 `SafeError`；CLI 缺失/非零退出即 `CONFIG_CLI_FAILED`，**绝不回退到硬编码默认端口**。

## 2. YAML schema（与 `internal/conf` 对齐）

```yaml
server:
  http:
    host: 127.0.0.1
    port: 8081
    timeout: 10s
frontend:
  dev_port: 5173
data:
  mysql:
    dsn_env: POKER_MYSQL_DSN
  redis:
    addr: 127.0.0.1:6380
    db: 15
    password_env: POKER_REDIS_PASSWORD
agents:
  catalog_path: auto
  request_timeout: 15s
  max_output_tokens: 256
  allow_paid_models: false
  max_live_calls: 1000
```

仅 `Public` 中的 `server.http.host/port` 与 `frontend.dev_port` 通过 CLI 暴露；其余字段
（Redis/DB/catalog/paid/token 限额）留在服务进程内，不进入公共 JSON。

## 3. 优先级（后者覆盖前者）

1. 内置默认值（Go `internal/conf.defaults()`）。
2. base YAML：`POKER_CONFIG_FILE` 或 `config/config.yaml`（`-conf` 可指定）。
3. 同目录可选 `config.local.yaml`（仅覆盖显式出现的键，显式 `false`/`0` 保留）。
4. 白名单环境变量（显式覆盖）。

白名单 env：`POKER_CONFIG_FILE`、`POKER_HTTP_HOST`、`POKER_HTTP_PORT`、
`POKER_HTTP_TIMEOUT`、`POKER_FRONTEND_PORT`（Go 侧另有 agent/限额键，Python 消费侧仅透传
上述 HTTP/前端/config 相关键）。

旧 `.env.local` 的**非敏感**键（`POKER_REDIS_ADDR`/`POKER_REDIS_DB`/`POKER_CATALOG_PATH`/
`POKER_ALLOW_PAID_MODELS`）**不再默认注入**服务子进程，避免盖过 YAML；Redis 因此走
`internal/conf` 默认 `127.0.0.1:6380` db15。仅保留凭据 `POKER_MYSQL_DSN` 与
`POKER_REDIS_PASSWORD` 注入。

付费门（`POKER_ALLOW_PAID_MODELS`）：
- E2E 强制 `false` + mock catalog（最高优先）。
- 普通 dev 强制 `false`，ambient `POKER_ALLOW_PAID_MODELS=true` 不能打开。
- 仅显式 `dev-live` action 将当前子进程设为 `true`，不改 `.env.local`。

## 4. 消费方

| 消费方 | 读取方式 | 变更点 |
|---|---|---|
| Python `scripts/local.py` | `config_public()` / `config_endpoint()` / `service_env()` | 新增 resolver；服务子进程环境改为 `service_env` 优先级 |
| E2E `tests/e2e/run.py` | `resolved_config()` 派生 base/host/port | 移除硬编码 8081；Host/Origin 契约测试按解析地址派生（函数改名 `test_http_host_origin_contract`，CASES 键仍为 `port`） |
| 浏览器 `tests/e2e/browser/run_browser.py` | `base_url()` / `http_port()` | 移除硬编码 8081；`POKER_BASE_URL` 由 resolver 注入 |
| Playwright `tests/e2e/browser/playwright.config.ts` | 必须收到 `POKER_BASE_URL` | 删除硬编码回退，缺失即明确失败 |
| Vite `web/vite.config.ts` | serve 时 `execFileSync('go', ['run','./cmd/config'])` | 只机械接线 proxy/Origin/devPort；build 不触发 CLI |
| Vite proxy 联调 `tests/e2e/vite_proxy.py` | `config_public()` + 真实 dev server | 真实 Host/Origin 转发（直连 403 vs 经 proxy 400），非 mock 配置 |

`POKER_BASE_URL` 与服务必须同源：wrapper 用 resolver 结果填 `POKER_BASE_URL`，不允许二者
指向不同地址。

### Vite 说明

- `defineConfig(({ command }) => ...)`：`command !== 'serve'` 时返回纯 build 配置，
  不执行 CLI，避免"CLI 依赖 dist / dist 依赖 CLI"的构建环。
- `command === 'serve'`（含直接 `npm run dev`）时解析 YAML：host ← `http_host`，
  `server.port` ← `frontend_port`，proxy target/Origin ← `http_base_url`，行为与视觉不变。
- 仅透传白名单 env + `PATH/HOME/GOCACHE/GOPATH/GOROOT/TMPDIR`，不带任何凭据。
- `web/package.json` 已加 `@types/node` devDependency，`vite.config.ts` 用 `PublicConfig`
  接口、`NodeJS.ProcessEnv` 与 Vite `ProxyOptions` 事件类型完整标注；**无** `@ts-nocheck` /
  `any`，`tsc --noEmit` 0 error。

## 5. env 安全

- `.env.local`：不修改、不 read 输出、不 source；仅本地 Python 进程读取，保留 DSN /
  RedisPassword 凭据。
- 调 CLI 的环境 `config_cli_env()`：先剥离全部 `POKER_*`/`MYSQL*`，再仅回填白名单非敏感键。
- 服务子进程环境 `service_env()`：只注入 DSN + RedisPassword；白名单 env 为显式覆盖；
  E2E 强制 mock/false；dev-live 唯显式 action。
- 不输出 env 或配置原文；不在错误/日志中回传 secret。

## 6. .gitignore

仅新增 `/config/config.local.yaml`。`config/config.yaml` 与 `config/*.example` 保持可提交，
不忽略；不动历史 raw 证据。

## 7. 场景映射（配置工具链）

| 场景 | 目标 / 测试函数 | 断言 | 本轮结果 |
|---|---|---|---|
| CLI 端口契约 | `test_config_public_derives_endpoint_from_cli_not_constant` | 8083 从 CLI 派生，非硬编码 | PASS |
| 非白名单/secret 字段拒绝 | `test_config_public_rejects_non_whitelist_and_secret_fields` | 额外字段（如 DSN）即拒绝 | PASS |
| 非 loopback 拒绝 | `test_config_public_rejects_non_loopback_host` | host 非 127.0.0.1/localhost 即拒绝 | PASS |
| 地址一致性 | `test_config_public_requires_consistent_address` | base_url 与 host:port 不一致即拒绝 | PASS |
| CLI 缺失即阻塞 | `test_config_cli_failure_is_blocker_not_default` | 非零退出 → SafeError，无默认回退 | PASS |
| CLI env 白名单 | `test_config_cli_env_only_forwards_whitelist` | 只透传白名单，剥离 DSN/密码/paid | PASS |
| E2E 强覆盖 | `test_service_env_e2e_forces_mock_and_false_and_drops_legacy_nonsensitive` | mock catalog + paid=false；旧 RedisAddr 不注入 | PASS |
| dev 优先级 | `test_service_env_dev_forwards_whitelist_and_ignores_ambient_paid` | 白名单透传；ambient paid=true 无效 | PASS |
| dev-live 显式 | `test_service_env_dev_live_only_explicit_action_enables_paid` | 仅显式 action 打开付费 | PASS |
| 绝对配置路径注入 | `test_service_env_injects_absolute_config_file` | 非项目 cwd 也能解析同一 base YAML | PASS |
| `-run` 假通过防护 | `test_go_contract_check_rejects_run_without_match` | 无匹配（`[no tests to run]`）判 FAIL | PASS |
| CLI env 显式覆盖 | `test_config_public_honors_explicit_env_override` | env 参数透传 POKER_CONFIG_FILE | PASS |
| 8081 Host/Origin（真实） | E2E `port` / `test_http_host_origin_contract` | 默认 8081 上 host/origin 契约 | PASS（e2e 9/9） |
| 可配置端口真实链路 | `verify-config-refactor` | 仅改临时 YAML→8083：e2e + browser + vite proxy | PASS（e2e 9/9、browser 6/6、proxy 4/4） |

E2E 场景键 `mock/privacy/controls/recovery/redis/dependencies/embedded/abort/port` 保留，
地址统一由 resolver 派生。

## 8. 可复现步骤

### 8.1 一键真实验证（推荐）

```sh
python3 scripts/local.py verify-config-refactor   # 历史别名 verify-port8081 / verify-final
# 或： make verify-config-refactor
```

依次执行：`make test-scripts` → `web typecheck` → `make build` → 默认 8081（migrate / e2e /
browser / go 契约单测）→ 临时 YAML 仅改端口 8083（e2e / browser / vite proxy）。全部原始
（脱敏）输出追加到 `docs/CONFIG_REFACTOR_VERIFICATION.md`。

### 8.2 仅改 YAML 换端口的等价手工步骤

```sh
TMPD=$(mktemp -d); sed 's/port: 8081/port: 8083/' config/config.yaml > "$TMPD/config.yaml"
export POKER_CONFIG_FILE="$TMPD/config.yaml"     # 主 config/config.yaml 与 local 不改
go run ./cmd/config                              # 应输出 http_port=8083
python3 tests/e2e/run.py all                     # 基址自动 8083
python3 tests/e2e/browser/run_browser.py         # POKER_BASE_URL 同源 8083
python3 tests/e2e/vite_proxy.py                  # 真实 dev proxy Host/Origin 转发
```

断言：CLI、服务、Vite proxy、E2E、浏览器地址一致；邻居端口被 Host/Origin 拒绝；
`vite-proxy` 直连 403、经 proxy 400；`git status` 中主 YAML 未变。

## 9. 本轮证据

### 9.1 RED（新增配置契约测试、resolver 未实现时）

```text
AttributeError: module 'local' has no attribute 'config_public'
AttributeError: module 'local' has no attribute 'config_cli_env'
scripts: tests=39 passed=33 failed=6
```

### 9.2 GREEN（`python3 tests/e2e/test_local.py`）

```text
scripts: tests=45 passed=45 failed=0
```

（含配置契约、resolver、service_env、绝对配置路径、`-run` 假通过防护等；完整 45 项列表见
运行输出与 `docs/CONFIG_REFACTOR_VERIFICATION.md`。）

### 9.4 真实链路（本机 MySQL 3307 / Redis 6380 / Go / Chromium）

- 默认 8081：`make migrate` + `make e2e` 9/9 + `make e2e-browser` 6/6 + go 契约单测 PASS。
- 临时仅改 YAML→8083：`run.py all` 9/9 + browser 6/6 + `vite_proxy.py` 4/4 PASS。
- `vite_proxy`：直连后端带前端 Origin=403，经 proxy 同请求=400（证明 Host/Origin 被改写）；
  经 proxy 的 health=200、`/`=HTML。
- 完整脱敏输出：`docs/CONFIG_REFACTOR_VERIFICATION.md`（含一次早期 proxy 启动竞态 FAIL 与
  修正后 PASS，未截断）。

### 9.3 Vite

```text
> tsc --noEmit          # 通过（0 error）
> vite build
✓ 38 modules transformed.
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-B41XS3CE.css   19.01 kB │ gzip:  5.41 kB
dist/assets/index-D9a1tCE2.js   253.20 kB │ gzip: 79.58 kB
✓ built in ~0.7s
```

## 10. 偏差、限制与风险

1. **[已解决] Vite `@ts-nocheck`**：`web/package.json` 增加 `@types/node@^24`，`vite.config.ts`
   改用 `PublicConfig`/`NodeJS.ProcessEnv`/`ProxyOptions` 完整类型，无 `@ts-nocheck` / `any`，
   `tsc --noEmit` 0 error。
2. **[已解决] 动态端口派生**：`probe_dev`/`verify_*` 不再 hardcode 活动逻辑，全部经
   `config_public()`/`config_endpoint()`；历史命令别名保留。
3. **[已解决] `-run` 假通过**：`_go_contract_check` 要求 `--- PASS:` 且拒绝
   `[no tests to run]`；Go 契约单测改用真实存在的测试名。
4. **[限制] 端口占用**：若 8081/8083/5173 被陌生进程占用，脚本按 `*_IN_USE_STOP_NO_KILL`
   阻塞上报，绝不 kill 陌生进程。本轮开始前三个端口均空闲。
5. **[契约风险] Host/Origin 允许集合**：E2E 契约测试按解析出的 host + `localhost` 别名派生；
   若 Go 侧改为只接受配置 host、不接受 `localhost` 别名，需同步该断言（联调兜底）。
6. **[范围] captureproxy/付费**：全部验证使用 mock + `allow_paid_models=false`；未启动
   `dev-live`、未调用真实 provider、未产生费用。
