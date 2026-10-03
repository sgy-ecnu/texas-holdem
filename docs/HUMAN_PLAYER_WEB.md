# 真人上桌 · Web 实现与验证记录

> 范围：`web/**`（不含 `web/embed.go`）与 `tests/e2e/browser/**`。
> 契约依据：架构师冻结设计报告（座位 `kind`、`/player` 与 `/action`、DTO 字段、可见性矩阵、全押映射、倒计时规则）。
> 说明：本文件是任务验证要求指定的输出文件，按明确指示新建；`docs/` 在任务的独占写范围之外，仅新增本文件，未改动任何既有文档。

## 1. 交付物

### 改动/新增文件

| 文件 | 类型 | 说明 |
|---|---|---|
| `web/src/types.ts` | 改 | 冻结 TS 类型对齐报告 DTO |
| `web/src/api.ts` | 改 | 新增 `player` / `action`，全部请求带 `credentials:'same-origin'` |
| `web/src/hooks/useGamePolling.ts` | 改 | 支持 `public`/`player` 双视角轮询与 403 降级 |
| `web/src/components/HumanActionPanel.tsx` | 新增 | 真人行动面板 + 倒计时 + 全押映射 |
| `web/src/components/SpectatorView.tsx` | 改 | 视角选择（本人/观战）、集成行动面板、私牌下传 |
| `web/src/components/PokerTable.tsx` | 改 | 仅真人座位显示本人底牌 |
| `web/src/components/Seat.tsx` | 改 | 真人座位高亮、本人底牌面朝上 |
| `web/src/components/ConfigPanel.tsx` | 改 | 「真人座位」单选、真人隐藏模型、校验最多 1 个真人 |
| `web/src/App.tsx` | 改 | 顶部文案 |
| `web/src/styles.css` | 改 | 真人座位、底牌、行动面板、倒计时样式 |
| `tests/e2e/browser/fixture/fixture-server.mjs` | 新增 | 本地 fixture 服务器（静态 + API 模拟） |
| `tests/e2e/browser/playwright.fixture.config.ts` | 新增 | 独立 fixture 配置（不干扰真实 Go E2E） |
| `tests/e2e/browser/fixture/tests/human-player.spec.ts` | 新增 | 浏览器交互验收（fixture） |
| `docs/HUMAN_PLAYER_WEB.md` | 新增 | 本文件 |

### 新增 TS 类型（`web/src/types.ts`）

- `SeatKind = 'ai' | 'human'`；`SeatDTO.kind`
- `WaitingFor = 'human_action'`；`HandDTO.waiting_for: WaitingFor | null`
- `GameDTO.human_seat: number | null`、`GameDTO.player: PlayerDTOView | null`
- `LegalActionsDTO`（`can_fold/can_check/can_call/call_amount/can_raise/min_raise_to/max_raise_to/short_all_in_to`）
- `PlayerDTOView`（`seat/hand_no/hole_cards/legal_actions/action_deadline/server_time`）
- `HumanActionRequest`（`expected_revision/kind/to?`）
- `CreateSeatRequest.kind`，`model_ref?` 改为可选（真人省略）

### 新增 API（`web/src/api.ts`）

```ts
player(id, signal?)                       // GET  /api/v1/games/{id}/player
action(id, {expected_revision,kind,to?})  // POST /api/v1/games/{id}/action
```

两者均显式 `credentials:'same-origin'`；`request()` 与健康检查同样带同源凭据。前端全程不读取 Cookie 值。

### 新增组件

- `HumanActionPanel`：可行动条件、动作按钮、加注输入（本街累计目标值「加注到」）、全押、倒计时。

## 2. 交互要点

- **配置真人**：新建牌局页新增「真人座位：无 / 座位 N」单选，天然保证最多 1 个真人；真人座位隐藏模型下拉、不参与 live 模型校验；提交时真人 `kind:'human'` 且省略 `model_ref`，AI 输出 `kind:'ai'`。
- **视角切换**：`human_seat===null` → 公共观战；否则尝试 `/player`，收到 `403 PLAYER_NOT_AUTHORIZED` 退回公共观战；网络错误/503 保持真人视角并显示「状态未知」，不解释成「无权限」。切换视角以 `key={mode}` 重建轮询实例，避免旧视角晚到响应覆盖。
- **私有底牌**：仅 `player.seat` 对应座位面朝上显示本人两张牌；其余座位一律牌背，历史详情接口仍可揭示已完成手牌。
- **可行动判定**：`本人视角 && running && hand.actor==player.seat && waiting_for=='human_action' && legal_actions!=null && action_deadline!=null && 无读取故障 && 无动作/控制请求`。
- **动作提交**：携带 `expected_revision`；`409` 刷新对齐、`400` 展示安全错误、`5xx`/网络刷新，均不自动重发 POST；成功后按 revision 应用并清空加注草稿。
- **全押映射**：`can_raise` → `raise_to` `to=max_raise_to`；否则 `can_call && call_amount==本人剩余 stack` → `call`；其余禁用。
- **倒计时**：`server_time/action_deadline` 求偏移，本地时钟推进；归零禁用动作并刷新一次，**前端不发自动弃牌**（服务端超时弃牌）。
- **控制命令与动作互斥**：任一进行中时另一侧禁用，命令后真人视角重新 GET `/player`，不合并旧私牌。

## 3. 验证命令与完整输出

### 3.1 依赖安装（跳过）

`web/node_modules` 已存在，按要求跳过 `npm --prefix web install`。

### 3.2 typecheck

```
$ npm --prefix web run typecheck

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

（无输出，退出码 0）
```

### 3.3 build

```
$ npm --prefix web run build

> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
transforming...
✓ 39 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.44 kB
dist/assets/index-C3qy8hek.css   21.17 kB │ gzip:  5.82 kB
dist/assets/index-CaKcuwmp.js   260.51 kB │ gzip: 81.94 kB
✓ built in 690ms
```

### 3.4 浏览器交互验证（fixture）

```
$ npx playwright test --config=playwright.fixture.config.ts

Running 4 tests using 1 worker

  ✓  1 [chromium] › fixture/tests/human-player.spec.ts:10:1 › 全 AI 创建走公共观战，不出现真人行动面板 (679ms)
  ✓  2 [chromium] › fixture/tests/human-player.spec.ts:19:1 › 真人座位：配置页隐藏模型下拉，创建后进入真人视角 (3.2s)
  ✓  3 [chromium] › fixture/tests/human-player.spec.ts:32:1 › 轮到真人：显示底牌、倒计时与动作按钮，弃牌带 revision 提交后收起面板 (3.3s)
  ✓  4 [chromium] › fixture/tests/human-player.spec.ts:55:1 › 无 Cookie 浏览器：人类牌局仅公共观战，看不到真人底牌与行动面板 (298ms)

  4 passed (8.2s)
```

场景 → 测试 → 结果：

| 场景 | 测试 | 结果 |
|---|---|---|
| 全 AI 公共观战，无行动面板 | `全 AI 创建走公共观战…` | 通过 |
| 真人座位隐藏模型、进入真人视角 | `真人座位：配置页隐藏模型下拉…` | 通过 |
| 本人底牌/倒计时/动作按钮/弃牌提交 | `轮到真人：显示底牌、倒计时与动作按钮…` | 通过 |
| 非本人浏览器观战、无底牌与面板 | `无 Cookie 浏览器：人类牌局仅公共观战…` | 通过 |

## 4. 未验证边界（如实声明）

- **真实 Go 联调未做**：Go 后端并行实现中，`/player`、`/action`、超时弃牌、Cookie 会话均未与真实服务联调。
- **真实 Cookie 会话未验证**：仅用 fixture 验证了 `Set-Cookie`/`403` 的前端分支，未验证真实 HttpOnly/SameSite/Path 行为与凭据摘要。
- **真实倒计时/动作提交未验证**：`action_deadline`/`server_time` 的真实时间基准、服务端超时弃牌、`409 STALE_ACTION`/`400 ILLEGAL_ACTION` 的真实返回均未验证。
- **暂停/恢复/重启对真人等待的影响未验证**（属 Go lane）。
- **fixture 不是真实链路完成证据**：仅证明前端在契约 DTO 下可正确渲染与交互。
- 上一手真实数据、revision 竞争等边界依赖 Go 后端，未验证。
