# 座位旁动作标签（Action Popover）动效实现与验证

状态：前端实现完成，fixture 浏览器验证通过；**真实 Go 联调未做**（见文末「未验证边界」）。
范围：仅 `web/src/**`；另新增验证用 spec `tests/e2e/browser/fixture/tests/action-popover.spec.ts` 与本报告。未提交、未推送。

## 一、契约与文案映射

`ActionDTO` 新增 `amount: number`（语义＝本次实际投入额：fold/check=0、call=本次补差跟注额、raise_to=追加额）。
后端 `internal/server/dto.go` 已落地该字段；前端 `web/src/types.ts` 按冻结契约补齐。

座位旁标签文案（`web/src/format.ts` 的 `actionPopoverLabel`）：

| kind | 文案 | 取值字段 |
| --- | --- | --- |
| `fold` | `弃牌` | — |
| `check` | `过牌` | — |
| `call` | `跟注 {amount}` | `amount`（本次实际投入额） |
| `raise_to` | `加注到 {to}` | `to`（本街累计目标值） |

数值统一走 `chips()` 千分位格式化；`amount` 缺失时按 0 兜底，不显示 `NaN`。
`ActionFeed`（行动历史）沿用既有 `ACTION_LABEL`＋`chips(to)`，本次未改动其行为。

## 二、实现位置

| 关注点 | 位置 |
| --- | --- |
| 动效组件 + 提示数据结构 + 时长常量 | `web/src/components/ActionPopover.tsx`（新建）：`ActionPopover`、`ActionHint`、`HINT_DURATION_MS=1600`、`hintFromAction` |
| 去重触发 + 生命周期状态管理 | `web/src/components/SpectatorView.tsx`（`Stage`） |
| 座位内渲染与定位分派 | `web/src/components/PokerTable.tsx`（`.seat-slot` 内、与 `<Seat>` 同级） |
| 文案映射 | `web/src/format.ts` |
| 契约类型 | `web/src/types.ts` |
| 动画 / 定位 / reduced-motion / 移动端预留行 | `web/src/styles.css` |

### 去重触发逻辑（`Stage`）

- 动作唯一键 = `game.id + hand_no + sequence`，即 `ActionPopover.hintFromAction` 生成的 `id`。
- `seenActionRef` 记录已观察到的最后序号：
  - 首次（进入 / 切视角后 Stage 以 `key=mode` 重建）观察到的 `last_action` **只作基线，不弹**；
  - 仅当 `hand_no+sequence` **严格递增**（`handNo > seen.handNo` 或同手 `sequence > seen.sequence`）才弹；
  - 相等（重复轮询）直接忽略；不依赖 `revision`，故 revision 变化不会触发。
- 定位用 `action.seat`（不用 `hand.actor`，后者通常已轮到下一人）。
- 跨手：`hand.number` 变化或检测到 `hand_no` 递增时先 `clearHints()` 清空上一手，再弹本手新动作。

### 状态管理（`Stage`）

- `hints: Map<seat, ActionHint>`：按座位索引 → 不同座位并存、同座位替换不堆叠（`Map.set` 覆盖）。
- `hintTimersRef: Map<seat, timeoutId>`：每座位一个到期 timer；同座位替换时先清旧 timer。
- 到期移除前用动作 `id` 校验（`prev.get(seat)?.id !== hint.id` 则不动），防止旧 timer 误删同座位新提示。
- 卸载时清理全部未到期 timer。
- React 只负责挂载 / 替换 / 移除，动画交给 CSS；提示节点用 `key={hint.id}` 触发重播，**未给 `Seat` / `PokerTable` 换 key**。

### 动效与定位（`styles.css`）

- 总时长 1600ms：`@keyframes action-pop` 150ms 淡入上浮 → 停留 1200ms → 250ms 淡出上浮，只动画 `opacity + transform`，不缩放、不闪烁、不循环。
- 定位用独立的 `translate` 属性（不参与动画），动画只动 `transform`，二者互不冲突：
  - 桌面普通座位 `.is-above`：座位上沿外侧居中，间距 6px；
  - 底部真人座位 `.is-right`：座位右侧，间距 8px（避让本人上方底牌）。
- `≤720px` 网格：`.seat-slot` 增加 `padding-bottom: 24px` 预留提示行，标签贴 slot 底部居中，不挤占 `.bet`。
- 与 `<Seat>` 同级挂载，故不受 `.seat.is-dimmed` / `.seat.is-folded` 的透明度影响。
- 可访问性/性能：`aria-hidden="true"`（宣读由 `ActionFeed` 的 `role=log` 承担）、`pointer-events: none`、不抢焦点。
- `prefers-reduced-motion: reduce`：覆盖为 `animation: none; opacity: 1` 静态可见，仍由 React timer 在 1.6s 后移除（避免动画被压到 0.01ms 后在结束帧 `opacity:0` 瞬间消失）。

### 改动文件

新增：
- `web/src/components/ActionPopover.tsx`
- `tests/e2e/browser/fixture/tests/action-popover.spec.ts`
- `docs/ACTION_POPOVER.md`（本文件）+ `docs/action-popover/*.png`（截图）

修改：
- `web/src/types.ts`（`ActionDTO.amount`）
- `web/src/format.ts`（`actionPopoverLabel`）
- `web/src/components/PokerTable.tsx`（`hints` prop、`data-seat`、渲染 `ActionPopover`）
- `web/src/components/SpectatorView.tsx`（去重触发与生命周期）
- `web/src/styles.css`（动画/定位/reduced-motion/移动端预留行）

## 三、验证

### 1) 类型检查与构建

命令：`npm --prefix web run typecheck`、`npm --prefix web run build`

```
=== npm --prefix web run typecheck ===

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

EXIT=0

=== npm --prefix web run build ===

> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
transforming...
✓ 44 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-ZWGcZmHF.css   29.82 kB │ gzip:  7.58 kB
dist/assets/index-DlS0Rw-R.js   269.68 kB │ gzip: 85.11 kB
✓ built in 701ms
EXIT=0
```

### 2) fixture 浏览器（Playwright，非真实 Go 链路）

命令：
`cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts action-popover`

```
Running 4 tests using 1 worker

  ✓  1 [chromium] › fixture/tests/action-popover.spec.ts:131:1 › 去重触发 + 同座位替换 + 不同座位并存 + 跨手清空 + 弃牌不变暗 + 标签定位 (13.4s)
  ✓  2 [chromium] › fixture/tests/action-popover.spec.ts:219:1 › reduced-motion：标签降级为静态可见，随后由 timer 移除 (4.5s)
  ✓  3 [chromium] › fixture/tests/action-popover.spec.ts:248:1 › 人数模板 2/6/9（全 AI）：标签不压中央、不互相重叠、无横向溢出 (7.7s)
  ✓  4 [chromium] › fixture/tests/action-popover.spec.ts:295:1 › 移动端 390：标签落在预留行、不压 .bet、不溢出 (3.7s)

  4 passed (30.0s)
```

> 说明：`ActionDTO.amount` 在 `tests/e2e/browser/fixture/fixture-server.mjs` 中尚未下发，故 spec 用 `page.route`
> 脚本化 `last_action`（含 `amount`）以验证文案与触发时序，未改 fixture 服务器。真实 Go 已下发 `amount`（`internal/server/dto.go`）。

### 3) 场景逐条对齐（场景 → 测试 → 结果）

| 需求场景 | 测试用例 | 断言位置 | 结果 |
| --- | --- | --- | --- |
| 新动作弹标签 | 「去重触发 + …」 | `state.action=act(1,2,1,'check')` 后 `toHaveCount(1)` / `toHaveText('过牌')` | ✅ |
| 重复轮询不重播 | 同上 | 重复轮询仍 `toHaveCount(1)`（不堆叠）；到期移除后持续轮询仍 `toHaveCount(0)` | ✅ |
| 切视角不重播 | 同上 | 基线断言：首次 `last_action` 不弹（切视角 Stage 以 `key=mode` 重建，等价重新基线） | ✅ |
| 同座位替换不堆叠 | 同上 | `act(1,3,1,'raise_to',600)` 后仍 `toHaveCount(1)` 且文本变为「加注到 600」 | ✅ |
| 不同座位并存 | 同上 | 连续弹 seat1、seat2 后 `toHaveCount(2)`，各自 slot 文本正确 | ✅ |
| 跨手清理 | 同上 | `handNo=2` 新动作后：seat0 弹出，seat1/seat2 标签 `toHaveCount(0)` | ✅ |
| 弃牌后标签不变暗 | 同上 | `.seat.is-human.is-folded` 存在，同 slot 标签 `opacity > 0.9`，class 含 `is-right` | ✅ |
| 文案映射 | 同上 | check→过牌、raise_to→加注到 600、call→跟注 75 / 跟注 50、fold→弃牌 | ✅ |
| 不遮挡 turn-badge / 底牌 / 中央 | 同上 | 标签 boundingBox 与 `.turn-badge` / `.hole` / `.board` / `.pot` 均不重叠 | ✅ |
| 2/6/9 人无重叠/不溢出 | 「人数模板 2/6/9…」 | 两个标签彼此不重叠、不压 `.board`/`.pot`/`.street-label`，`scrollWidth-innerWidth ≤ 1` | ✅ |
| 移动端无重叠/不溢出 | 「移动端 390…」 | 标签不压 `.seat .bet`，无横向溢出 | ✅ |
| reduced-motion 静态可见 | 「reduced-motion…」 | `animationName === 'none'`、`opacity > 0.9`、800ms 后仍在，随后由 timer 移除 `toHaveCount(0)` | ✅ |

### 4) 回归

命令：`cd tests/e2e/browser && npx playwright test -c playwright.fixture.config.ts`
结果：`23 passed (1.8m)`（含新增 4 条 + 既有 `action-dock` / `human-player` / `quick-bet-keys` / `table-layout` 全部通过；
`.seat-slot` 移动端 `padding-bottom` 未破坏既有移动端布局与溢出断言）。

### 5) 截图证据（`docs/action-popover/`）

- `desktop-human-3-popover.png`：桌面真人局，AI 座位 `.is-above`、真人弃牌 `.is-right` 且不变暗。
- `count-2-popover.png` / `count-6-popover.png` / `count-9-popover.png`：2/6/9 人两标签并存、不压中央。
- `mobile-390-popover.png`：移动端标签落在预留行、不压 `.bet`。
- `reduced-motion-static.png`：reduced-motion 下静态可见。

## 四、未验证边界（如实说明，未完成项）

- **真实 Go 端到端联调未做**：以上均为本地 fixture + `page.route` 注入，不能作为真实链路证据。真实链路需另跑
  `playwright.config.ts` + `run_browser.py`（真实 Go 服务），本次未执行。
- **fixture-server 未下发 `amount`**：spec 用路由注入；若后续在 fixture 中补齐 `amount`，可去掉注入直接验证。
- **多座位同帧动作**：后端 `last_action` 每次只暴露最新一条，客户端按轮询逐个观察；「同帧多动作」时只弹最新一条（符合
  「只弹客户端观察到的新动作」）。该行为未做真实多动作并发压测。
- **标签溢出 stage 顶部**：顶部座位标签按方案向上弹出，理论上可能伸到 stage 上方（不产生横向滚动，已验证 `overflow ≤ 1`）；
  未在极小高度视口下逐一目视核对与状态栏的视觉叠压。
- **未验证 reduced-motion 在真实系统偏好下的表现**：用 `page.emulateMedia({ reducedMotion: 'reduce' })` 模拟，非操作系统级验证。
