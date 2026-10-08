# 阶段 2：用户字段预留 + 牌桌配置模板（后端）

按架构师 arc-14 已确认方案实现。本文记录数据模型/迁移改动、接口与实现、REST 端点、
user 字段预留位置、单测场景→结果、真实 MySQL 迁移验证证据、命令原始输出与未验证边界。

不涉及 web 前端（阶段 3）。所有改动未 commit/push。

---

## 1. 数据模型改动

### 1.1 迁移机制改造（`internal/data/repository.go`）

| 项 | 改动前 | 改动后 |
|---|---|---|
| SQL 加载 | `//go:embed migrations/001_init.sql` 单文件字符串 | `//go:embed migrations/*.sql` + `loadMigrations()` 按文件名前缀版本号升序 |
| 版本校验 | `validateSchema` 硬编码 `version != 1` | `currentSchemaVersion = 2`，并校验新增表/字段/索引 |
| 执行 | 直接把唯一 SQL 按 `;` 执行 | `runMigrations` 先建 `schema_version`，只执行 `version > MAX(version)` 的迁移，成功后 `INSERT IGNORE` 记录版本 |
| 注释处理 | 无 | `stripLineComments` 先剥离 `--` 行注释再按 `;` 切分（避免注释内分号破坏切分） |

新增 `internal/data/migrations/002_table_templates.sql`：
- `CREATE TABLE IF NOT EXISTS table_templates`（`id CHAR(36) PK`、`owner_user_id CHAR(36) NULL`、
  `name VARCHAR(128) NOT NULL`、`revision BIGINT UNSIGNED`、`config JSON NULL`、
  `created_at/updated_at DATETIME(6)`、`KEY templates_updated (updated_at,id)`、`utf8mb4`）。
- 对 `games` 幂等加列：`owner_user_id`、`template_id`、`template_revision`。
  用 `information_schema` 守卫 + `PREPARE/EXECUTE`，可重复运行。
- `INSERT IGNORE INTO schema_version (version) VALUES (2)`。

`games` INSERT（`Repository.Create`）新增三列；`openMySQL` 增加 `cfg.ParseTime = true`
以正确扫描 `DATETIME(6)`。

### 1.2 user 字段预留（最小侵入，全部允许 NULL）

| 位置 | 字段 | 说明 |
|---|---|---|
| `games` 列 + `service.Record` | `owner_user_id` / `OwnerUserID *string` | 牌局创建者 |
| `table_templates` 列 + `service.Table` | `owner_user_id` / `OwnerUserID *string` | 模板所有者 |
| `service.SeatConfig` | `UserID *string` | 真人座位参与用户；AI 座位必须为空 |

规则落实：
- **AI 座位 UserID 必须为空**：`Engine.validate` 对 `SeatAI` 且 `UserID != nil` 返回 `ErrInvalid`。
- **不允许请求自行传入**：HTTP DTO 无 user 字段，严格解码会因未知字段返回 400（见单测）。
- **注入点预留**：`registerTableRoutes` 里 `tables.Create(..., nil)` 的 owner 恒为 nil；
  `CreateGame.OwnerUserID` 只由可信适配层注入，`Engine.Create` 会把它移入私有 `Record`，不写入 `Config`。
- **不进公共输出**：`SeatView`/`SeatDTO`/`TableDTO`/`TableSummaryDTO` 均无 user/owner 字段；
  `server.MarshalGame` 仍只编码 `GameView`。
- **模板不保存用户**：纯 `TableConfig`/`TableSeat` 无任何 user 字段；
  `TableConfig.GameInput()` 生成的 `SeatConfig.UserID` 恒为 nil。
- **HumanSessionHash 含义不变**：仍是游戏级操作凭据，不是 user 身份。

### 1.3 牌局来源字段

`games` 列与 `Record` 新增 `template_id` / `template_revision`（`*string` / `*uint64`）。
从模板开局时由 HTTP 层写入快照；worker 之后只读牌局 `Config`，不读模板；删除模板不影响历史局。

---

## 2. TableService / TableRepository

新增 `internal/service/table.go`：

- `TableSeat` / `TableConfig`：可复用的**纯配置**（seats/button/blinds/max_hands/mode/max_live_calls），
  不含 `PlayerSessionHash`、不含 user。
- `Table`：领域对象（ID/owner/name/revision/config/created_at/updated_at），`Config==nil` 表示空态。
- `validateTableConfig`：**保存时**只检查结构、数值范围、座位关系（mode/blinds/max_hands/seats 2–9/
  ≤1 真人/真人空 model_ref/AI 非空 model_ref/live 预算范围），**不要求真人会话、不检查模型目录、不构造 decider**。
- `TableService`：`Create/Get/List/Update/Clear/Delete`，全部走 revision CAS。
- `TableConfig.HumanSeat()` / `GameInput()`：开局配置读取；`TableConfigFromGame` 反向投影。

新增 `internal/data/table_repository.go`：`TableRepository` 的 MySQL 实现（直接读写 MySQL，不加 Redis 缓存）。
- `GetTable/ListTables/UpdateTable/ClearTable/DeleteTable` 走 `guard(ctx)` 单实例锁校验。
- 列表按 `updated_at DESC, id DESC` keyset 分页，游标为 `unix_micro:id`（不含用户信息）。
- CAS 更新/删除影响 0 行时，用 `SELECT COUNT(*)` 区分 `ErrNotFound` 与 `ErrConflict`。

**保存校验与开局校验分离**：保存只做纯校验；开局复用 `Engine.Create` 复核授权/预算/模型可构造性并注入新会话。
模型目录变化或付费授权关闭后模板仍可保存/编辑，但可能暂时不能开局。

---

## 3. REST 端点（revision CAS，冲突 409）

| 方法 | 路径 | 请求 | 响应 |
|---|---|---|---|
| POST | `/api/v1/tables` | `name` + `config`（允许 null/缺省） | 201 `TableDTO` |
| GET | `/api/v1/tables` | `limit`（0–100）、`cursor` | 200 `{tables,next_cursor}` |
| GET | `/api/v1/tables/{id}` | — | 200 `TableDTO` |
| PUT | `/api/v1/tables/{id}` | `expected_revision` + `name` + 完整 `config` | 200 `TableDTO` |
| POST | `/api/v1/tables/{id}/clear` | `expected_revision` | 200 `TableDTO`（`config=null`） |
| DELETE | `/api/v1/tables/{id}` | `expected_revision` | 204 |
| POST | `/api/v1/tables/{id}/games` | `expected_revision` | 201 `GameDTO` |

- `TableDTO`：`id/name/revision/config/created_at/updated_at`；列表返回不含完整 seats 的摘要，
  不暴露 owner/会话。
- 清空 = `config:null` 的未配置空态，不是默认 2 人局；空模板开局返回 400。
- 从模板开局：`expected_revision` 需等于当前模板 revision（否则 409）；复用 `Engine.Create`
  与数据库单活约束（存在非终态局返回 409）。
- 真人模板开局复用现有 loopback 检查 + 生成新会话 + 设置 HttpOnly Cookie；会话值不写入模板/响应。
- 构造函数：保留 `server.New/Handler`（tables=nil，不注册模板路由）；新增
  `server.NewWithTables/HandlerWithTables`。`cmd/server/main.go` 用 `service.NewTableService(repo)` 注入。

---

## 4. 单测场景 → 结果

| 场景 | 测试函数 | 结果 |
|---|---|---|
| 模板创建（空/完整）+ 读取 + 名称校验 | `service.TestTableServiceCreateGetAndEmpty`、`TestTableServiceNameValidation` | PASS |
| revision CAS 冲突 409 | `service.TestTableServiceRevisionCAS` | PASS |
| 清空 = config null + revision 递增 | `service.TestTableServiceClearSemantics` | PASS |
| 删除 CAS + 删除后 404 | `service.TestTableServiceDelete` | PASS |
| 存储故障归一化 | `service.TestTableServiceStorageFailure` | PASS |
| 保存校验（mode/blinds/座位关系/预算） | `service.TestValidateTableConfig` | PASS |
| 模板不携带 user、GameInput 不注入来源 | `service.TestTableConfigGameInputReservesUser`、`TestTableConfigFromGameDropsUser` | PASS |
| AI 座位 UserID 必空（Engine.Create） | `service.TestCreateRejectsAIUserField` | PASS |
| 列表有界分页 | `service.TestTableServiceListBounds` | PASS |
| HTTP CRUD + 冲突 409 + 清空 + 删除 204 | `server.TestTableCRUDHTTP` | PASS |
| 空 config 允许 + 座位 user_id 严格拒绝 | `server.TestTableCreateEmptyAndRejectUnknownUser` | PASS |
| 从模板开局记录 template_id/revision | `server.TestTableOpenFromTemplate` | PASS |
| 开局 expected_revision 不符 409 | `server.TestTableOpenRevisionMismatch` | PASS |
| 单活冲突 409 | `server.TestTableOpenSingleActiveConflict` | PASS |
| 真人开局设 HttpOnly Cookie、会话不进响应 | `server.TestTableOpenHumanSetsSessionCookie` | PASS |
| 非 loopback 真人开局 403 | `server.TestTableOpenHumanLocalOnly` | PASS |
| 模板仓库 INSERT/SELECT/CAS/Clear/Delete/分页（sqlmock） | `data.TestCreateTablePersists`、`TestGetTableRoundTrip`、`TestUpdateTableRevisionCAS`、`TestClearTableSetsNullConfig`、`TestDeleteTableCAS`、`TestListTablesPagination` | PASS |
| 迁移版本 2 校验（version 3 拒绝） | `data.TestSchemaFailsClosed` | PASS |
| 真实 MySQL：新库安装/重复迁移/旧库升级/部分失败重跑/单活 | `data.TestMigrateIntegration`（env 门控） | PASS |
| 真实 MySQL：模板仓库 INSERT/SELECT/CAS/Clear/Delete/keyset 分页 | `data.TestTableRepositoryIntegration`（env 门控） | PASS |

---

## 5. 真实 MySQL 迁移验证

本地 MySQL（127.0.0.1:3306，本机 root 免密，运行于独立 scratch 库
`texas_holdem_tmpl_verify`，未触碰用户库 `texas_holdem`）。集成测试用 DSN 经环境变量传入，
命令与输出不含任何凭据。

命令：

```
POKER_TEST_MYSQL_DSN='root:@tcp(127.0.0.1:3306)/texas_holdem_tmpl_verify?parseTime=true' \
  go test -mod=readonly -count=1 -v -run 'Test(Migrate|TableRepository)Integration' ./internal/data/
```

原始输出：

```
=== RUN   TestMigrateIntegration
=== RUN   TestMigrateIntegration/fresh_install_and_repeat
=== RUN   TestMigrateIntegration/old_install_upgrade
=== RUN   TestMigrateIntegration/partial_failure_rerun
--- PASS: TestMigrateIntegration (0.14s)
    --- PASS: TestMigrateIntegration/fresh_install_and_repeat (0.06s)
    --- PASS: TestMigrateIntegration/old_install_upgrade (0.04s)
    --- PASS: TestMigrateIntegration/partial_failure_rerun (0.04s)
=== RUN   TestTableRepositoryIntegration
--- PASS: TestTableRepositoryIntegration (0.00s)
PASS
ok  	texas-holdem/internal/data	0.340s
```

`TestTableRepositoryIntegration` 在真实 MySQL 上验证了模板仓库 INSERT/Get、CAS 冲突、
Update、Clear（config 置 NULL）、keyset 两页分页与 Delete（含删除后 NotFound）。

迁移后 DDL 快照：

```
$ mysql -u root texas_holdem_tmpl_verify -e "SELECT version FROM schema_version;"
version
1
2

$ mysql -u root texas_holdem_tmpl_verify -e "SHOW CREATE TABLE table_templates\G"
*************************** 1. row ***************************
       Table: table_templates
Create Table: CREATE TABLE `table_templates` (
  `id` char(36) NOT NULL,
  `owner_user_id` char(36) DEFAULT NULL,
  `name` varchar(128) NOT NULL,
  `revision` bigint unsigned NOT NULL,
  `config` json DEFAULT NULL,
  `created_at` datetime(6) NOT NULL,
  `updated_at` datetime(6) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `templates_updated` (`updated_at`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

$ mysql -u root texas_holdem_tmpl_verify -e "SHOW COLUMNS FROM games;"
Field	Type	Null	Key	Default	Extra
id	char(36)	NO	PRI	NULL	
revision	bigint unsigned	NO		NULL	
status	varchar(16)	NO		NULL	
active_slot	tinyint	YES	UNI	NULL	
record	longblob	NO		NULL	
owner_user_id	char(36)	YES		NULL	
template_id	char(36)	YES		NULL	
template_revision	bigint unsigned	YES		NULL	
```

---

## 6. 完整命令输出摘要

```
$ go vet ./...
vet_exit=0

$ go test -mod=readonly -race -count=1 ./...
ok  	texas-holdem/cmd/config	4.063s
ok  	texas-holdem/cmd/server	3.900s
ok  	texas-holdem/internal/agent	4.396s
ok  	texas-holdem/internal/conf	4.097s
ok  	texas-holdem/internal/data	4.346s
ok  	texas-holdem/internal/poker	4.705s
ok  	texas-holdem/internal/server	4.639s
ok  	texas-holdem/internal/service	6.993s
?   	texas-holdem/web	[no test files]
test_exit=0
```

---

## 7. 迁移机制改造风险点

1. **旧 JSON 记录兼容**：`games.record` 里旧 `Record` 缺新字段，反序列化自然为 nil，无需批量重写历史局。
2. **部分失败可重跑**：002 的 `CREATE TABLE IF NOT EXISTS` 与加列守卫幂等；`schema_version` 在整文件成功后记录，
   中途失败重跑不会因“列已存在”报错。集成测试 `partial_failure_rerun` 覆盖。
3. **DDL 无事务**：迁移不承诺原子回滚，依赖幂等语句 + 版本记录；部署时须停服后显式执行（`go run ./cmd/server migrate` 或 `make migrate`）。
4. **线上库需显式迁移**：当前用户库 `texas_holdem` 仍为版本 1；程序启动要求版本 2，**未迁移前会以 `SCHEMA_INVALID` 拒绝启动**。
   本任务只在 scratch 库验证，未对线上库执行迁移（需用户自行停服迁移）。
5. **`schema_version` 会同时保留 1、2 两行**（001/002 各自 INSERT + runner 兜底），`MAX(version)` 语义不受影响。
6. **`ParseTime=true` 全局生效**：仅影响 DATETIME/TIMESTAMP 扫描，现有代码不扫描时间列，无行为回归。

## 8. 未验证边界

- 未对用户真实库 `texas_holdem` 执行迁移（未触碰）；线上升级需另行停服迁移并验证。
- 未跑前端/浏览器链路（阶段 3）；模板前端未实现。
- 未在真实 provider 下验证“从模板开局触发真实模型”；开局复用 `Engine.Create` 的既有校验/构造路径，其模型链路由阶段 1 覆盖。
- 未做多用户/权限校验（本期仅预留字段，非权限系统）。
- 已知与本阶段无关：`internal/service/seat_model_test.go::TestSeatModelFallbackKeepsBudget` 为**时序 flaky**——
  其在后台 worker 持续推进的情况下等待 `actions>=1` 后即断言 `LiveCallsUsed==1`，异步超出时读到 2–4；
  非 `-race` 下稳定失败、`-race` 下多数通过但偶发失败。属阶段 1 未提交改动（`worker.go`/`seat_model_test.go`），
  本阶段未修改 worker.go；最终一次全量 `-race` 连续 3 次通过。
