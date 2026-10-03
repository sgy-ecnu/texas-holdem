# Texas Hold'em 本地运行

Go 服务使用 MySQL 作为权威存储，Redis 仅缓存公共 DTO。HTTP 默认监听
`127.0.0.1:8081`，端口/地址统一由 YAML 配置经冻结 CLI 派生（改 YAML 即可换端口，
不改代码）。默认开发与全部 E2E 仅运行内置 mock，不调用真实模型。
配置 schema、优先级、env 安全与可复现步骤见
[配置工具链](docs/CONFIG_TOOLING.md)。

## 命令

首次在项目根目录运行：先确保前端 `web/package.json` 与 `web/package-lock.json`
存在，再执行 `make build → make setup → make migrate → make run`。
`make build` 生成真实 `web/dist`；Go embedded 使 migrate/dev/test 都依赖该产物。
setup 本身可先准备存储，不依赖 dist；重复 setup 不覆盖配置或重建数据库。

`make run` 是「改完代码一键跑」入口：每次先 `npm ci` + 前端构建生成**最新**
`web/dist`，再以前台 dev 方式启动服务，避免服务嵌入旧 dist。
`make dev` 保持快速启动语义，**要求 `web/dist` 已存在**，不重新编译前端。
两者在启动服务前都会自动检测并**安全清理 8081 上残留的本项目 server 进程**
（旧 `go run` 留下的监听进程会让新服务绑定 8081 失败、用户只能看到旧进程返回的旧
嵌入 bundle）；只清理「可执行文件在 Go build cache 且 cwd 指向本项目」的本项目
server 及其 `go run` 父进程，核验不通过的陌生进程一律拒绝并提示手动处理，绝不误杀。

```sh
make setup         # brew services + 独立数据库/用户 + 受保护本地配置
make doctor        # 不打印配置值的本机连接检测
make status        # 核验项目隔离依赖的 PID / executable / 路径
make stop          # 只停止项目隔离依赖，保留数据，不停止 brew 服务
make start         # 重启项目隔离依赖
make build         # npm ci、真实前端构建、Go embedded 二进制
make migrate       # 显式 Go 数据库迁移（依赖真实前端 dist）
make run           # 编译前端生成最新 dist，清理 8081 残留 server，再启动（一键，前台）
make dev           # 安全加载配置、清理 8081 残留 server 后启动（快速启动，要求 dist 已存在）
make dev-live      # 显式开启本次后端付费开关；可能产生费用，详见下方
make test-scripts  # 无数据库的脚本安全测试
make test          # Go 测试（依赖真实 dist）
make e2e             # 真 MySQL/Redis/Go HTTP，全部 mock
make e2e-abort       # 150 次真实 HTTP 断开后验证同局继续运行
make e2e-browser     # 真实 Chromium UI，wrapper 自管 HTTP，全部牌局 mock
make e2e-vite-proxy  # 真实 Vite dev proxy → Go，校验 Host/Origin 转发
make verify-config-refactor  # 默认 8081 与仅改 YAML→8083 的真实验证并留证
```

启动后访问 **http://127.0.0.1:8081/**（默认值，实际由 YAML 决定）。`make dev` 前台运行，
Ctrl-C 退出。当前本机存储使用 **Homebrew 默认实例**（MySQL `127.0.0.1:3306`、
Redis `127.0.0.1:6379`）；真实地址与密码写在本地私有 `config/config.local.yaml`
（mode 600、gitignored）的直连字段 `data.mysql.dsn` / `data.redis.password`，
直连优先于 `*_env` 引用。项目隔离实例（MySQL 3307 / Redis 6380）已弃用并停止，
`.local` 数据目录保留。`config.local.yaml` 与 `.env.local` 均不得读取/粘贴/提交其值。

开发模式允许 Go 服务只读加载用户模型目录，展示模型清单，但付费默认关闭、
不会自动调用真实模型。`make e2e` 始终强制独立 mock 目录。

`make dev-live` 是显式费用风险入口：仅改变当前子进程后端开关，不修改 `.env.local`
中的 false。仍需在 Web 选择 Live、正调用预算并勾选费用确认，才可能调用 provider。
使用者自行决定费用；本轮**未启动 dev-live、未验证真实 provider**。ambient 付费变量
不能绕过普通 dev，E2E 永远 false + mock。

本机实测：默认 8081 与"仅改 YAML 端口→8083"两条链路均为 HTTP E2E 9/9、Chromium UI 6/6、
Vite dev proxy 4/4、脚本安全 45/45；`@types/node` 就位后 `tsc --noEmit` 0 error。
端口/地址由 YAML 经冻结 CLI 派生，改 YAML 即可换端口、不改代码。
逐场景映射与完整（脱敏）输出见 [配置重构验证](docs/CONFIG_REFACTOR_VERIFICATION.md)；
配置契约与优先级见 [配置工具链](docs/CONFIG_TOOLING.md)。
[8081 验证](docs/PORT_8081_VERIFICATION.md) 与 [最终验证](docs/FINAL_VERIFICATION.md) 的
原始输出保留为历史证据。

需要 Python 3、Homebrew、Go、Node/npm、`mysql@8.4`、`redis`。
不自动安装缺失软件、不复制用户 opencode 配置、不生成占位 dist。
`.env.local` 仅需存在（脚本有 `ENV_LOCAL_MISSING` 检查）；存储凭据已迁至
`config/config.local.yaml`，该文件不再存放旧隔离实例（3307/6380）凭据。
**不要 source、cat、粘贴或提交** `.env.local` / `config.local.yaml`。初始化不覆盖已有文件，
不删库、不清空 Redis。

运行步骤、安全边界、E2E 矩阵和当前阻塞见 [本地环境说明](docs/LOCAL_SETUP.md)。

进程管理不信任 PID 元数据中的 signature/binary；从固定运行路径核验原生 argv、
真实 executable、cwd 与启动时间，停止前再次核验。Darwin 的检查与 SIGTERM 之间
仍有非原子 TOCTOU 窗口；不能宣称完全消除 PID 复用竞态。
