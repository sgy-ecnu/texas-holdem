# 牌桌配置模板：阶段 3 前端真实链路验证证据

运行时间：2026-10-08 10:33:21 +0800

复现命令（项目根目录）：`python3 tests/e2e/browser/run_table_template.py`。
真实 Go 二进制 + 嵌入最新 web/dist + Chromium + 真实 MySQL/Redis。
**scratch 库说明**：用户库 `texas_holdem` 仍为 schema v1（阶段 2 需 v2），真实服务会拒绝启动。本 wrapper 不改用户库与仓库 config，只在临时目录生成 E2E 专用配置，指向一次性 scratch 库 `texas_holdem_frontend_verify`（root 空密码，无密钥），结束后删除。
Redis 密码由 local.py 在本地进程内从受保护配置读入并注入子进程，全程不打印、不回传。
不改后端（阶段 2 已完成）。不 commit/push。

---

## 1. 页面 / 组件结构

首版不引入路由框架，`App` 用 `page: 'tables' | 'editor' | 'quick' | 'play'` 状态切页：

| 页面 | 组件 | 职责 | 提交目标 |
|---|---|---|---|
| 牌桌列表（默认入口） | `components/TableList.tsx` | 浏览模板卡片；开一局/编辑/清空配置/删除 | 列表/清空/删除 + 从模板开局 |
| 牌桌编辑 | `components/TableEditor.tsx` | 创建/编辑模板，全量替换 | `POST/PUT /api/v1/tables` |
| 快速开局 | `components/ConfigPanel.tsx` | 保留一次性配置入口 | `POST /api/v1/games` |
| 当前牌局 | `components/SpectatorView.tsx` | 沿用原观战/真人交互 | — |

列表卡片展示：名称、`rev`、人数、盲注、真人位置、`真实/mock` 模型数量、live 额度。
「未配置」`config=null` 卡片只显示名称 + 空态文案，`开一局` 置灰，可编辑/清空/删除。

## 2. 共享编辑器拆分

- 新增 `components/GameConfigEditor.tsx`：座位/盲注/按钮位/模式/额度表单与全部联动 + 校验，
  **不发起任何 API 请求**；产出一个 `TableConfigInput`（结构与 `CreateGameRequest` 同构）。
- `ConfigPanel`（快速开局）与 `TableEditor`（模板编辑）各自只负责提交，复用同一份表单，未复制第二份。
- 空态：`initial: TableConfigDTO | null`。`null` 时显示「该模板尚未配置：暂无座位与盲注」+
  「填入默认配置」按钮（座位数为 0、盲注为空），点击填入默认 2 人 mock 配置。
- mock/live 模型选择联动、费用确认门槛、真人座位不绑定模型等阶段 1 逻辑原样保留。

## 3. api.ts / types.ts 改动

`types.ts` 新增（与 `internal/server/dto.go` 逐字段对齐）：
`TableSeatDTO`、`TableConfigDTO`、`TableDTO`、`TableSummaryDTO`、`TableListDTO`、
`TableSeatInput`、`TableConfigInput`、`CreateTableRequest`、`UpdateTableRequest`。

`api.ts` 新增：`tables({limit,cursor})`、`table(id)`、`createTable`、`updateTable`、
`clearTable(id, expected_revision)`、`deleteTable(id, expected_revision)`（204）、
`openGameFromTable(id, expected_revision)`。全部同源相对路径 + `credentials: 'same-origin'`，
不读取 Cookie 值。

## 4. 导航逻辑

- `bootstrap` 只加载 `health/models/current`，**不再自动跳转当前牌局**；默认 `page='tables'`。
- 有当前牌局且不在观战页时，顶栏显示「返回当前牌局」入口，不强制阻止访问模板页。
- 离开观战页（返回列表/编辑）**不触发 stop**；`/games/current` 仍存在（场景 6 验证）。
- 模板修改/清空不影响已在展示的牌局（开局只读模板快照，见阶段 2）。
- 清空与删除使用**不同确认文案**：清空强调「变为未配置空态、名称保留」；
  删除强调「不可恢复、已开始的牌局不受影响」。

## 5. 一键开局（创建 + start 两步）与失败重试

`TableList.openGame`：先 `POST /tables/{id}/games` 创建（服务端用模板当前 revision 快照，
不接收座位/盲注覆盖），再 `POST /games/{game_id}/commands {command:'start'}`。
第二步失败时**保留已创建的 game_id**，进入该牌局并显示「牌局已创建，但启动失败：可在本页点
『开始』重试，不会重新创建牌局」——复用 `SpectatorView` 对 `status=created` 的「开始」按钮重试，
`openGame` 只在第一次创建，重试只发 start（场景 7 验证 `open 请求=1`）。
最小可行方案即此：不做补偿/复合事务，失败即可见地停在已创建态。

## 6. 真实浏览器测试场景 → 结果

真实 Go 二进制（scratch 库，schema v2）+ 嵌入 `web/dist` + Chromium。测试文件
`tests/e2e/browser/table-template/table_template.spec.ts`，配置 `playwright.tabletemplate.config.ts`。

| 场景 | 测试（spec 行号） | 结果 |
|---|---|---|
| UI 创建模板（含真人座位）+ 列表展示人数/盲注/真人位置/模型数量 + 无 owner 泄漏 | 场景1 (`:126`) | PASS |
| 编辑 PUT 全量替换 + 版本冲突 409 提示刷新 | 场景2 (`:167`) | PASS |
| 清空 = `config=null` 未配置空态（确认文案区别于删除）+ 未配置不可开局 + 空态「填入默认配置」 | 场景3 (`:203`) | PASS |
| 删除模板（独立确认文案）+ GET 404 | 场景4 (`:231`) | PASS |
| 从模板一键开局（创建 + start 两步） | 场景5 (`:254`) | PASS |
| 单活冲突 409 + 离开观战页不等于 stop | 场景6 (`:271`) | PASS |
| start 失败：保留 game_id、重试 start、不重新创建 | 场景7 (`:297`) | PASS |
| 多浏览器 context：模板列表不泄漏 owner + 真人 Cookie 隔离 | 场景8 (`:341`) | PASS |

`playwright: passed=8 failed=0 not_run=0 exit=0`。截图目录见文末。

## 7. typecheck / build 输出

见下方原始输出：`npm --prefix web run typecheck` 退出码 0；`npm --prefix web run build`
`✓ 53 modules transformed`、`✓ built`、退出码 0；`go build ./cmd/server` 退出码 0。

## 8. 期间修复的产品缺陷（由场景 6 暴露）

`TableList.openGame` 在 409 时先 `setError` 再 `void load()`，而 `load()` 开头 `setError(null)`，
导致冲突提示被刷新覆盖、用户看不到。改为：冲突时先 `await load()` 刷新 revision，完成后再 `setError(message)`。

## 9. 未验证边界

- **未跑**：live/付费真实模型链路（本验收全程 mock，`allow_paid_models=false`）；真实 provider 出站。
- **未跑**：模板列表「加载更多」分页（真实数据量未触发 `next_cursor`）；移动端窄屏布局。
- **未跑**：真人模板开局后完整真人行动/私牌权限全流程（场景 8 只验证 Cookie 隔离与 `/player` 200/403）。
- **未覆盖**：用户库 `texas_holdem` 仍为 schema v1，本次用 scratch 库验证；用户库迁移仍需用户停服执行。
- **回归风险（重要）**：默认入口从「快速开局配置页」改为「牌桌列表」，仓库内既有 fixture 浏览器用例
  （`tests/e2e/browser/tests/*.spec.ts`，均假设 `goto('/')` 后是「新建牌局」）**未同步更新、未复跑**；
  这些用例预计会失败，需后续补一步「进入快速开局」导航或调整断言。本轮按任务范围只新增真实浏览器用例。
- 未 commit/push。

### 命令

```sh
npm --prefix web run typecheck
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

```

退出码：0

### 命令

```sh
npm --prefix web run build
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
transforming...
✓ 53 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-K1UOmJRe.css   42.64 kB │ gzip:  9.90 kB
dist/assets/index-BOgKeb-b.js   297.19 kB │ gzip: 93.75 kB
✓ built in 698ms
```

退出码：0

### 命令

```sh
go build -mod=readonly -o /Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/.table-template-je69jhlv/server ./cmd/server
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

```

退出码：0

### 命令

```sh
/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/.table-template-je69jhlv/server -conf /Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/.table-template-je69jhlv/config.yaml migrate
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem`

```text

```

退出码：0

```text
health: {"mysql": "available", "redis": "available", "storage_degraded": false, "allow_paid_models": false, "max_live_calls": 1000}; scratch_schema_version=2
```

### 命令

```sh
npx --no-install playwright test --config=playwright.tabletemplate.config.ts
```

工作目录：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser`

```text

Running 8 tests using 1 worker

  ✓  1 [chromium] › table-template/table_template.spec.ts:126:1 › 场景1 UI 创建模板（含真人座位）并列表展示人数/盲注/真人位置/模型数量 (788ms)
  ✓  2 [chromium] › table-template/table_template.spec.ts:167:1 › 场景2 编辑模板 PUT 全量替换，版本冲突 409 提示刷新 (859ms)
  ✓  3 [chromium] › table-template/table_template.spec.ts:203:1 › 场景3 清空 = 未配置空态（确认文案区别于删除），未配置不可开局 (1.1s)
  ✓  4 [chromium] › table-template/table_template.spec.ts:231:1 › 场景4 删除模板（独立确认文案）后列表移除且 GET 404 (665ms)
  ✓  5 [chromium] › table-template/table_template.spec.ts:254:1 › 场景5 从模板一键开局（创建 + start 两步） (689ms)
  ✓  6 [chromium] › table-template/table_template.spec.ts:271:1 › 场景6 单活冲突：已有进行中牌局时再开局 409，且离开观战页不等于 stop (1.2s)
  ✓  7 [chromium] › table-template/table_template.spec.ts:297:1 › 场景7 start 失败：保留已创建 game_id 并允许重试 start，不重新创建 (756ms)
  ✓  8 [chromium] › table-template/table_template.spec.ts:341:1 › 场景8 多浏览器 context：模板列表不泄漏 owner、真人 Cookie 隔离 (2.1s)

  8 passed (8.8s)
```

退出码：0

```text
playwright: passed=8 failed=0 not_run=0 exit=0
```

截图目录：`/Users/gaosper/Documents/mycode/texas-holdem/tests/e2e/browser/table-template-artifacts/20261008-103326`。

```text
cleanup: own_server_stopped=True; scratch_db_dropped=True; 8081_released=True; Homebrew_3306_6379_retained=True
```

## 结论

PASS：8 个真实浏览器场景 + typecheck/build/Go 构建全部通过。
