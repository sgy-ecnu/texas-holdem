# 修复：created（已创建未开始）状态缺少返回配置页入口

## 背景与根因

牌局创建后处于 `created`（已创建未开始）时，`web/src/components/SpectatorView.tsx` 的 controls 区只有「开始」按钮；
返回配置页的 `onExit` 入口（`App.exitToConfig`）只在 `terminal`（stopped / completed / failed）状态经 `isTerminal` 渲染。
于是用户建了牌桌、未开始、想改设置时无路可退。

根因位置（修复前）：`web/src/components/SpectatorView.tsx` controls 区，`terminal && (<button onClick={onExit}>新建牌局</button>)`。
`created` 分支内没有任何返回入口。

## 改动点（file:line）

写范围仅 `web/src/components/SpectatorView.tsx` 与新增本文件；未改 `styles.css`（复用既有 `.btn .btn-danger .btn-sm`），
未改 Go / scripts / config / docs/contracts.md / Makefile。

| 位置 | 改动 |
|---|---|
| `web/src/components/SpectatorView.tsx:68` | `control(command)` 由 `Promise<void>` 改为 `Promise<boolean>`：成功 `return true`，失败 `return false`（失败仍按原逻辑 `setActionError` 并在 409 时刷新对齐）。既有调用方 `onClick={() => control(...)}` 不受影响。 |
| `web/src/components/SpectatorView.tsx:97-101` | 新增 `discardAndExit()`：先 `await control('stop')`，仅当成功才 `onExit()`；失败则留在本页展示错误，不跳转。 |
| `web/src/components/SpectatorView.tsx:154-172` | `created` 分支由单个「开始」按钮改为 `<>…</>`：保留「开始」，新增「放弃并返回」按钮。 |

## 按钮文案与交互

- 文案：`放弃并返回`（`title="停止这个尚未开始的牌局并返回配置页"`）；pending 期间显示 `放弃中…`。
- 样式：`btn btn-danger btn-sm`，与运行中/暂停态的「停止」按钮同款视觉语言。
- 交互：
  1. 点击 → 调 `control('stop')` 停止这个 `created` 牌局（释放非终态锁，避免之后新建撞 409「已有非终态游戏」）。
  2. `stop` 成功后 → 调 `onExit()`（`App.exitToConfig`）回到配置页；`onExit` 内部 `bootstrap()` 调 `/games/current`，
     真实后端终态局 `active_slot != 1` 故返回 404，稳定落在配置页。
  3. `stop` 失败（网络/409 等）→ 不跳转，`actionError` 横幅展示原因。
- 防重复：按钮 `disabled={controlsBusy}`（`controlsBusy = pending !== null || acting`），pending 期间禁用。
- 后端合法性依据：`internal/service/manager.go:484-485` `case "stop": return "stopped", !terminal(v.Status)`，`created` 合法。

## 验证命令与完整输出

### 1) typecheck

```bash
npm --prefix web run typecheck
```

```
> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

EXIT=0
```

### 2) build

```bash
npm --prefix web run build
```

```
> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
transforming...
✓ 43 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-DUAoSD3x.css   26.37 kB │ gzip:  6.92 kB
dist/assets/index-B3rcq7mO.js   266.09 kB │ gzip: 83.72 kB
✓ built in 683ms
EXIT=0
```

### 3) 真实浏览器（Chromium / Playwright 1.63.0）交互验证

驱动方式：一次性脚本（位于仓库外临时目录）静态服务**刚构建的 `web/dist`**，API fixture 复刻契约，
关键点是终态牌局不再出现在 `/games/current`（对齐真实后端 `active_slot` 语义），从而 `exitToConfig → bootstrap → /current 404 → 配置页`。
脚本命令：`node <temp>/created-exit-verify.mjs`。**这是前端交互验证，不是真实 Go 链路证据。**

```
  [api] GET http://127.0.0.1:4182/api/v1/health 200
  [api] GET http://127.0.0.1:4182/api/v1/models 200
  [api] GET http://127.0.0.1:4182/api/v1/games/current 404
  [browser] error Failed to load resource: the server responded with a status of 404 (Not Found)
  [api] POST http://127.0.0.1:4182/api/v1/games 200
  [api] GET http://127.0.0.1:4182/api/v1/games/fixture-06f31b3a/player 200
PASS · created 状态出现「开始」按钮
PASS · created 状态出现「放弃并返回」按钮
PASS · created 状态不显示旧的「新建牌局」终态入口
PASS · 点击后 pending 期间按钮禁用
PASS · 点击后 pending 期间文案为「放弃中…」
  [api] POST http://127.0.0.1:4182/api/v1/games/fixture-06f31b3a/commands 200
  [api] GET http://127.0.0.1:4182/api/v1/games/fixture-06f31b3a/player 200
  [api] GET http://127.0.0.1:4182/api/v1/health 200
  [api] GET http://127.0.0.1:4182/api/v1/models 200
  [api] GET http://127.0.0.1:4182/api/v1/games/current 404
  [browser] error Failed to load resource: the server responded with a status of 404 (Not Found)
PASS · 停止成功后回到配置页
PASS · 仅发送一次 stop 命令 · commands=["stop"]
PASS · 后端牌局状态为 stopped · status=stopped

SUMMARY passed=8 failed=0 total=8
```

> 日志中的 `[browser] error … 404` 是 `/games/current` 无进行中牌局的预期响应（配置页初始态），非失败。

## 逐场景 → 检查 → 结果

| # | 场景 | 检查 | 结果 |
|---|---|---|---|
| 1 | `created` 状态出现返回按钮 | 「放弃并返回」可见、且不出现终态「新建牌局」 | PASS |
| 2 | pending 防重复 | 点击后按钮 disabled、文案变「放弃中…」 | PASS |
| 3 | 点击后回配置页 | 停牌后 `/current` 404 → 显示「新建牌局」配置页 | PASS |
| 4 | 牌局被 stop | 仅 1 次 `POST /commands {command:"stop"}`；fixture 状态 `stopped` | PASS |
| 5 | 静态回归 | `tsc --noEmit`、`vite build` 均退出 0 | PASS |

## 未验证 / 边界

- **真实 Go + 真实浏览器端到端**：未验证。8081 上运行的 Go 服务在构建期 `//go:embed dist`（`web/embed.go`），
  其内嵌的是**改动前**的前端产物；本轮未重启该服务（8081 当前另有进行中牌局，`/games/current` 返回 200，
  不打扰用户牌局）。因此浏览器验证走的是「新构建 `dist` + 契约 fixture」，真实 Go 链路待服务重建后另行联调。
- **真实后端 `stop` on `created` 的运行时行为**：未在真实服务上跑（同上，未重启/未创建）。合法性仅由源码
  `internal/service/manager.go:484-485` 与任务说明佐证；fixture 按契约模拟。
- **stop 失败分支**：未用浏览器实测（fixture 未注入失败）。代码路径为「失败不跳转 + 展示 `actionError`」，逻辑直读确认。
- 未做真机移动端 / 多浏览器验证（仅 Chromium）。

## 未提交

按要求未 `commit` / `push`，`git status` 仅 `M web/src/components/SpectatorView.tsx`；本文件为新增。
