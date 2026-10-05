# 座位卡模型名修复（arc-5）

纯前端修复：把模型名从第二行上移到第一行，消除与「剩余筹码」的宽度竞争；对长模型名只显示有辨识度的短名，完整 label 保留在 title。

## 改动文件

| 文件 | 改动 |
|---|---|
| `web/src/format.ts` | 新增 `MODEL_SHORT_LABEL` 映射表 + `modelShortLabel()` 短名函数 |
| `web/src/components/Seat.tsx` | 模型名从 `seat-row2` 移到 `seat-row1`（座位号→名字→模型短名→标签）；第二行只留「筹码+剩余」 |
| `web/src/styles.css` | `.seat-model` 改为第一行内展示、max-width 56px/10px、可收缩到 0；`.seat-row2`/`.seat-stack` 禁止换行；移动端 ≤720px 短名 11px/120px |
| `tests/e2e/browser/fixture/tests/model-label.spec.ts` | 新增（新文件，不改共享 fixture server，用 route 注入长模型名） |

## 1. 短名映射函数与映射表

位置：`web/src/format.ts`

```ts
// 座位卡模型短名：只对已知模型做少量显式映射，不做任何通用字符串猜测/截断。
// key = ModelDTO.ref（形如 deepseek/deepseek-v4-pro）；完整 label 仍保留在座位卡 title。
// 未收录的模型返回 null，座位卡直接省略模型名（降级为只保留 title），避免再次挤坏第一行。
export const MODEL_SHORT_LABEL: Record<string, string> = {
  'deepseek/deepseek-v4-pro': 'V4 Pro',
}

export function modelShortLabel(modelRef: string | null | undefined): string | null {
  if (!modelRef) return null
  return MODEL_SHORT_LABEL[modelRef] ?? null
}
```

- 映射依据 `ModelDTO.ref`（后端 `config_loader.go` 里 `ref = providerID + "/" + modelKey`，`label` 为 model ID，见 `internal/agent/config_loader.go:187-198`）。
- 表内目前仅收录需求给定的 `deepseek/deepseek-v4-pro`。**未收录的模型一律返回 null、座位卡省略显示（降级），不做字符串猜测**；完整 label 仍通过座位卡 `title` 可见。

## 2. Seat 两行结构调整

- 第一行：`座位号 → 玩家名 → 模型短名 → 标签`（D/SB/BB/真人/弃牌…）。
- 第二行：只保留 `筹码 + 剩余`，移除 `.seat-model`。
- 短名节点整组条件渲染（含括号），未知模型不渲染 → 不会留孤立括号。
- 座位卡 `title` 仍为 `名字 · 完整 modelLabel`（`Seat.tsx:69`）。

关键 JSX：

```tsx
const modelLabel = isHuman ? '真人玩家' : model ? model.label : seat.model_ref || '未绑定模型'
const modelRef = model?.ref ?? seat.model_ref ?? ''
const shortModel = isHuman ? null : modelShortLabel(modelRef)
...
{shortModel && (
  <span className="seat-model" title={modelLabel}>({shortModel})</span>
)}
```

## 3. styles.css 改动要点

- `.seat-row2`：`white-space: nowrap`；第二行不再含模型节点。
- `.seat-stack`：`white-space: nowrap; flex: none`，禁止换行、不被压缩。
- `.seat-name`：`flex: 1 1 auto; min-width: 0`。
- `.seat-model`：`flex: 0 100 auto; max-width: 56px; min-width: 0; font-size: 10px`，允许收缩到 0，空间紧张时先于名字收缩（优先级 名字>标签>筹码>模型名）。
- 删除原「默认隐藏、≥1500px 才显示、真人隐藏」的旧规则（模型名现在作为短名常显）。
- `@media (max-width: 720px)`：`.seat .seat-model { font-size: 11px; max-width: 120px }`。
- 座位卡尺寸不变：`.seat` 仍 `width:140px; height:58px`。

## 4. 验证命令与输出

### 4.1 typecheck

```
$ npm --prefix web run typecheck

> texas-holdem-web@0.1.0 typecheck
> tsc --noEmit

EXIT=0
```

### 4.2 build

```
$ npm --prefix web run build

> texas-holdem-web@0.1.0 build
> tsc --noEmit && vite build

vite v7.3.6 building client environment for production...
✓ 44 modules transformed.
dist/index.html                   0.67 kB │ gzip:  0.45 kB
dist/assets/index-4j8RCVpB.css   31.23 kB │ gzip:  7.92 kB
dist/assets/index-COWU5Mlm.js   271.63 kB │ gzip: 86.05 kB
✓ built in 742ms
EXIT=0
```

### 4.3 fixture 浏览器验证

命令（复用现有 fixture 配置，只跑本任务 spec；route 注入 `deepseek/deepseek-v4-pro` 与超长未知模型，不改共享 fixture server / dist）：

```
$ cd tests/e2e/browser
$ npx playwright test --config playwright.fixture.config.ts model-label.spec.ts

Running 2 tests using 1 worker

  ✓  1 [chromium] › fixture/tests/model-label.spec.ts:52:1 › 桌面：模型短名上移第一行、第二行剩余不换行、未知模型不留孤立括号、卡不增高 (809ms)
  ✓  2 [chromium] › fixture/tests/model-label.spec.ts:102:1 › 移动端 ≤720px：模型短名 11px / max-width 120px、不溢出 (3.2s)

  2 passed (4.6s)
EXIT=0
```

### 4.4 截图证据

| 文件 | 场景 |
|---|---|
| `docs/model-label/desktop-long-model.png` | 桌面 1536×1024：长玩家名 + 已知模型 `(V4 Pro)` 在第一行；未知长模型座位不渲染模型名 |
| `docs/model-label/mobile-model.png` | 移动端 390×844：模型短名 11px / max-width 120px、无横向溢出 |

## 5. 场景 → 断言 → 结果

| 场景 | 测试项 | 结果 |
|---|---|---|
| 模型名不再挤坏剩余行 | `.seat-row2 .seat-model` 数量=0；`.seat-row2` 单行高 ≤26px | ✅ |
| 长模型名显示短名 + 省略号 | `.seat-row1 .seat-model` 文本 = `(V4 Pro)`（max-width 56px 下 ellipsis 兜底） | ✅ |
| 短名省略时不孤立括号 | 未知模型座位 `.seat-model` 数量=0，row1 文本不含 `(` | ✅ |
| 座位卡不增高 | `.seat` 宽 140±2、高 58±2 | ✅ |
| 名字优先级高于模型名 | 超长名字宽 >50px、同座模型宽 <20px（模型先收缩） | ✅ |
| 移动端正常 | 390 宽 computed font-size=11px、max-width=120px、无横向溢出 | ✅ |
| 完整信息保留 title | 座位卡 title 含完整 `deepseek-v4-pro` / 未知 ref | ✅ |

## 6. 未验证边界

- **真实 Go 联调未做**：本次仅前端 + fixture（非真实链路）。真实后端 `ModelDTO.label` 取自 `model` 配置键、`ref` 为 `provider/model`，映射以 `ref` 为键；真实链路下短名是否命中取决于实际配置的 ref。
- **未知模型降级**：映射表只含需求给定的一条，其他任意模型（含真实接入的其它模型）均走「省略显示、只保留 title」降级，不做字符串猜测；如需更多短名，后续在 `MODEL_SHORT_LABEL` 增补显式映射。
- fixture 中 mock 模型（label `Mock`）未收录 → 不显示短名，仅 title。

未执行 commit / push。
