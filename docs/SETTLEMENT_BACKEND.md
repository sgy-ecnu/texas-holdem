# 「谁赢了」结算展示：后端能力落地（arc-6 方案）

范围：仅后端 Go（`internal/`、`cmd/`）。未改动 `web/`、`scripts/`、`config/`。
未提交、未 push；未启动真实 HTTP/MySQL/Redis；未调用付费模型；未读取 `.env.local` / `config.local.yaml` 的值。

本次实现三件事：牌型字段、手间结算窗口、弃牌保密。

---

## 1. 改动文件

| 文件 | 改动 |
| --- | --- |
| `internal/poker/types.go` | `HandResult` 新增 `HandRanks map[Seat]string`、`BestFive map[Seat][]Card`；`copyResult` 深拷贝两字段 |
| `internal/poker/showdown.go` | 新增 `handCategoryNames` / `handCategoryName` / `rankBest` / `evalFive`；`rankCards`、`rankFive` 改为薄封装；`settle` 改为只公开摊牌参与者的底牌与牌型 |
| `internal/conf/config.go` | `Config` 新增 `HandIntervalShowdown/HandIntervalUncontested`；`fileConfig.Gameplay` 与严格 schema 新增 `hand_interval_showdown_sec` / `hand_interval_uncontested_sec`；默认 12s/8s；YAML/env 覆盖与校验 |
| `internal/service/contracts.go` | `Options` 新增两个手间窗口时长；`Record` 新增私有 `NextHandAt *time.Time` |
| `internal/service/worker.go` | `step` 新开手前增加窗口门闩；`settle` 记录/清除下一手最早开始时间；新增 `handInterval` |
| `internal/server/dto.go` | 新增 `ShowdownDTO`；`CompletedHandDTO` 新增 `showdown` 数组并在 `Completed()` 填充 |
| `cmd/server/main.go` | 把 config 的两个窗口时长注入 `service.Options` |
| `internal/poker/best_five_test.go`（新） | 牌型名 + 最佳五张 + 弃牌保密测试 |
| `internal/service/hand_interval_test.go`（新） | 手间窗口门闩 / 暂停恢复 / 重启 / 最后一手测试 |
| `internal/server/settlement_dto_test.go`（新） | 结算 DTO 的 `hand_rank`/`best_five` 测试 |
| `internal/conf/config_test.go` | env 隔离键新增两项；新增 `TestHandIntervalConfig` |

> 说明：任务要求「不碰 config/」，因此没有修改 `config/config.yaml`、`config.local.yaml.example`。两个参数以代码默认值 12s/8s 生效，运维可通过 YAML（`gameplay.hand_interval_*_sec`）或环境变量覆盖。

---

## 2. 牌型名与最佳五张

### 落地位置
- 算法与名称：`internal/poker/showdown.go`
  - `handCategoryNames = [9]string{"高牌","一对","两对","三条","顺子","同花","葫芦","四条","同花顺"}`（等级 0-8）
  - `handCategoryName(category int) string`
  - `rankBest(cards []Card) (uint64, int, [5]Card)`：枚举所有五张组合，返回最高分值、牌型等级、最佳五张
  - `evalFive(cards [5]Card) (uint64, int)`：五张牌分值 + 等级
  - `rankCards` / `rankFive` 保留为薄封装，分值语义不变
- 产物落到 `HandResult.HandRanks`（牌型中文名）与 `HandResult.BestFive`（最佳五张）。
- 传输：`internal/server/dto.go` 的 `ShowdownDTO{seat, hand_rank, best_five}`，通过 `CompletedHandDTO.showdown` 数组按座位升序输出。

### 算法与语义保持
- 等级与既有 `score(category, kickers...)` 的 `category` 完全一致，`evalFive` 在每个返回点同时给出 `category`，不做除法反推。
- `rankCards` 改为 `rankBest` 的薄封装后，`TestHandRanking`、`TestChipConservationRandomSequences` 等既有胜负判定测试全部通过，说明胜负语义未变。
- 平局（多个五张组合同为最高分）时保留枚举顺序中的第一组，组合按索引字典序稳定，结果可复现。
- **uncontested（弃牌结束）不产生任何牌型**：`settle` 只在 `reason=="showdown"` 时写入 `HandRanks`/`BestFive`。

JSON 形状示例（`GET /api/v1/games/{id}/hands/{hand_no}`）：

```json
{
  "reason": "showdown",
  "hole_cards": [{"seat": 1, "cards": ["2c", "2s"]}],
  "showdown": [
    {"seat": 0, "hand_rank": "高牌", "best_five": ["Qh", "Jd", "9c", "7s", "4h"]},
    {"seat": 1, "hand_rank": "一对", "best_five": ["2c", "2s", "Qh", "Jd", "9c"]}
  ]
}
```

---

## 3. 手间结算窗口

### 行为
- 每手结束（showdown / uncontested）后，`Engine.settle` 计算下一手最早开始时间并写入 `Record.NextHandAt`。
  - 摊牌默认 12s；弃牌结束默认 8s（`Options.HandIntervalShowdown/HandIntervalUncontested`，生产由 config 注入）。
  - 时长为 `<=0` 时不设置窗口（用于既有测试/快速部署；生产 config 拒绝 0/负值）。
- `worker.step` 在「新开一手」分支最前面判断：`NextHandAt != nil && now < NextHandAt` 时直接返回 —— 不新开手，也不进入后续真人等待分支，因此窗口内不会创建新手真人倒计时。
- 新手真正创建时清除 `NextHandAt`。

### 持久化与不绕过
- `NextHandAt` 存在 `service.Record`（私有字段），随 `games.record` JSON 落库；`GameView`/公共 DTO/Redis 均不含它，不泄露调度细节。
- **暂停/恢复不绕过**：窗口是绝对时刻，pause 不清除、resume 不重置；恢复时若仍在窗口内继续等待剩余时间。
- **服务重启不丢**：`Engine.New` 恢复运行中的局会转 paused(recovery)，但 `NextHandAt` 随记录保留；resume 后继续按剩余窗口等待。
- **最后一手不再开手**：`settle` 在判定终局（达到 MaxHands 或存活 < 2）时清空 `NextHandAt` 并置 `completed`。

### config 参数（严格白名单）
- `gameplay.hand_interval_showdown_sec`（int 秒，默认 12）
- `gameplay.hand_interval_uncontested_sec`（int 秒，默认 8）
- 白名单 schema 用 `kindInt`；显式 null / 非 int / 重复键等沿用既有严格校验。
- 覆盖优先级：默认 → base YAML → `config.local.yaml` → 白名单环境变量 `POKER_HAND_INTERVAL_SHOWDOWN_SEC` / `POKER_HAND_INTERVAL_UNCONTESTED_SEC`。
- 校验：`finalize` 与 env 解析均要求 `> 0`，否则 `CONFIG_INVALID`。

---

## 4. 弃牌保密

`internal/poker/showdown.go` 的 `settle`：

- 旧逻辑：对所有 `s.dealt[i]` 写入 `r.HoleCards`。
- 新逻辑：仅在 `reason=="showdown"` 且该座位 `Status` 为 `active`/`all_in`（即参与 `rankCards` 计算的那批）时，才写入 `HoleCards`，并同时写入 `HandRanks`/`BestFive`。
- `reason=="uncontested"`：不写入任何底牌，也不产生牌型。
- 真人本人已知底牌仍走 `poker.PrivateHole`（`GetPlayer` 私有链路），不依赖 `HandResult`；因此公共结算结果不含他人底牌。
- DTO 全链路一致：`CompletedHandDTO.HoleCards` 只含摊牌参与者；`showdown` 只含摊牌参与者；弃牌结束两者均为空。

---

## 5. 新增测试场景 → 结果

### 牌型名正确（`internal/poker/best_five_test.go`）
| 场景 | 测试函数 | 结果 |
| --- | --- | --- |
| 高牌/一对/两对/三条/顺子/同花/葫芦/四条/同花顺 各一例，断言中文名 + 最佳五张 + `rankCards` 语义不变 | `TestHandCategoryNamesAndBestFive`（9 子用例） | PASS |

### 弃牌保密（`internal/poker/best_five_test.go`）
| 场景 | 测试函数 | 结果 |
| --- | --- | --- |
| showdown 的 `HoleCards`/`HandRanks`/`BestFive` 不含弃牌者，只含 active/all_in | `TestSettleFoldSecrecy/showdown_hides_folded` | PASS |
| uncontested 的 `HoleCards`/`HandRanks`/`BestFive` 全为空 | `TestSettleFoldSecrecy/uncontested_hides_all` | PASS |

### 手间窗口（`internal/service/hand_interval_test.go`）
| 场景 | 测试函数 | 结果 |
| --- | --- | --- |
| 结算后窗口内不推进、窗口过后推进到第二手 | `TestHandIntervalGatesNextHand` | PASS |
| 暂停/恢复不绕过剩余窗口 | `TestHandIntervalNotBypassedByPauseResume` | PASS |
| 服务重启不丢窗口（NextHandAt 持久化，恢复后仍等待） | `TestHandIntervalSurvivesRestart` | PASS |
| 最后一手不再开手，且不残留窗口 | `TestHandIntervalLastHandStops` | PASS |

### config（`internal/conf/config_test.go`）
| 场景 | 测试函数 | 结果 |
| --- | --- | --- |
| 默认 12s/8s | `TestHandIntervalConfig/default_12s_8s` | PASS |
| YAML 与 env 覆盖 | `TestHandIntervalConfig/yaml_and_env_override` | PASS |
| 显式 0/负值/null/非 int 及 env 坏值拒绝 | `TestHandIntervalConfig/explicit_zero_negative_and_bad_type_rejected` | PASS |

### DTO（`internal/server/settlement_dto_test.go`）
| 场景 | 测试函数 | 结果 |
| --- | --- | --- |
| 摊牌 DTO 暴露 `hand_rank`/`best_five` 且按座位升序；uncontested 输出空数组 | `TestCompletedHandShowdownDTO` | PASS |

### 既有测试不回归
全量 `go test -race -count=1 ./...`：**130 个 PASS，0 个 FAIL，1 个 SKIP**（`TestLocalCatalogModels` 因未提供显式只读配置路径而跳过，与本次改动无关）。
覆盖 rules/showdown、dto、http、service、data、conf、agent 全部既有用例。

---

## 6. 验证命令与输出摘要

执行命令（完整原始输出见附录 A）：

```
go test -mod=readonly -race -count=1 ./...
go vet ./...
```

非 verbose 摘要：

```
ok  	texas-holdem/cmd/config	1.247s
ok  	texas-holdem/cmd/server	1.449s
ok  	texas-holdem/internal/agent	1.478s
ok  	texas-holdem/internal/conf	1.685s
ok  	texas-holdem/internal/data	1.840s
ok  	texas-holdem/internal/poker	2.310s
ok  	texas-holdem/internal/server	2.234s
ok  	texas-holdem/internal/service	4.536s
?   	texas-holdem/web	[no test files]
```

- `go vet ./...`：退出码 0，无输出（无警告）。
- `go test -race -count=1 ./...`：退出码 0；`--- PASS` 计数 130，`--- FAIL` 计数 0。
- `gofmt -l internal cmd`：无输出（全部已格式化）。

---

## 7. `rankFive` 重构点

- 原 `rankFive([5]Card) uint64` 只返回分值。为拿到「最佳五张」，新增 `evalFive([5]Card) (uint64, int)` 承载原逻辑并在每个返回点附带牌型等级；`rankFive` 保留为 `evalFive` 的薄封装。
- 原 `rankCards([]Card) uint64` 的枚举逻辑迁移到 `rankBest([]Card) (uint64, int, [5]Card)`，`rankCards` 保留为薄封装。
- 重构未改变任何分值计算分支与胜负判定；由 `TestHandRanking`、`TestSidePotsAndRefunds`、`TestChipConservationRandomSequences`（200 局随机守恒）等既有测试证明。

---

## 8. 未验证边界 / 待确认

1. **arc-6 原文未在本仓库检出**：仅按本任务描述实现；建议与架构师文档逐字段核对。
2. **DTO 形状为设计决策**：任务只给出「建议 JSON 字段 `hand_rank` / `best_five`」，未指定容器结构。本次实现为 `CompletedHandDTO.showdown` 数组（元素含 `seat`/`hand_rank`/`best_five`），与既有 `hole_cards` 数组风格一致。若前端约定不同容器名，需要微调 `dto.go`（纯映射改动，不影响规则层）。
3. **未做真实链路/端到端**：未启动真实 HTTP/MySQL/Redis，未跑浏览器；服务重启用内存 testRepo 验证 `NextHandAt` 的 JSON 持久化往返，未在真实 MySQL 上验证。属于「待验证」。
4. **窗口时长参数未写入 `config/config.yaml`**（任务禁止改 `config/`），线上如未显式配置将使用代码默认 12s/8s。
5. 真人座位「窗口内不创建新手倒计时」通过代码路径保证（窗口门闩在真人等待分支之前 return），未额外写真人+窗口的集成测试。

---

## 附录 A：完整原始输出（`go test -race -count=1 -v ./...`，逐条含计数）

```
=== RUN   TestRunPublicWhitelistJSON
--- PASS: TestRunPublicWhitelistJSON (0.00s)
=== RUN   TestRunEnvPriority
--- PASS: TestRunEnvPriority (0.00s)
=== RUN   TestRunExplicitEmptyConfRejected
--- PASS: TestRunExplicitEmptyConfRejected (0.00s)
=== RUN   TestRunSafeErrors
=== RUN   TestRunSafeErrors/missing_file
=== RUN   TestRunSafeErrors/unknown_flag
=== RUN   TestRunSafeErrors/extra_arg
--- PASS: TestRunSafeErrors (0.00s)
    --- PASS: TestRunSafeErrors/missing_file (0.00s)
    --- PASS: TestRunSafeErrors/unknown_flag (0.00s)
    --- PASS: TestRunSafeErrors/extra_arg (0.00s)
=== RUN   TestRunPublicWhitelistExcludesDirectSecrets
--- PASS: TestRunPublicWhitelistExcludesDirectSecrets (0.00s)
PASS
ok  	texas-holdem/cmd/config	1.171s
=== RUN   TestParseArgs
=== RUN   TestParseArgs/no_args
=== RUN   TestParseArgs/conf
=== RUN   TestParseArgs/conf_empty
=== RUN   TestParseArgs/conf_equals_empty
=== RUN   TestParseArgs/migrate
=== RUN   TestParseArgs/conf_migrate
=== RUN   TestParseArgs/bad_flag
=== RUN   TestParseArgs/bad_subcommand
=== RUN   TestParseArgs/extra_arg
--- PASS: TestParseArgs (0.00s)
    --- PASS: TestParseArgs/no_args (0.00s)
    --- PASS: TestParseArgs/conf (0.00s)
    --- PASS: TestParseArgs/conf_empty (0.00s)
    --- PASS: TestParseArgs/conf_equals_empty (0.00s)
    --- PASS: TestParseArgs/migrate (0.00s)
    --- PASS: TestParseArgs/conf_migrate (0.00s)
    --- PASS: TestParseArgs/bad_flag (0.00s)
    --- PASS: TestParseArgs/bad_subcommand (0.00s)
    --- PASS: TestParseArgs/extra_arg (0.00s)
=== RUN   TestResolveConfigExplicitEmptyRejected
--- PASS: TestResolveConfigExplicitEmptyRejected (0.00s)
=== RUN   TestResolveConfigLoadsDirectSecrets
--- PASS: TestResolveConfigLoadsDirectSecrets (0.00s)
PASS
ok  	texas-holdem/cmd/server	1.198s
=== RUN   TestConfigLoaderRedaction
=== RUN   TestConfigLoaderRedaction/literal
=== RUN   TestConfigLoaderRedaction/env
=== RUN   TestConfigLoaderRedaction/file
--- PASS: TestConfigLoaderRedaction (0.00s)
    --- PASS: TestConfigLoaderRedaction/literal (0.00s)
    --- PASS: TestConfigLoaderRedaction/env (0.00s)
    --- PASS: TestConfigLoaderRedaction/file (0.00s)
=== RUN   TestJSONCLexerAndMerge
--- PASS: TestJSONCLexerAndMerge (0.00s)
=== RUN   TestModelListAndMock
--- PASS: TestModelListAndMock (0.00s)
=== RUN   TestProviderAdapters
=== RUN   TestProviderAdapters/openai
=== RUN   TestProviderAdapters/anthropic
--- PASS: TestProviderAdapters (0.01s)
    --- PASS: TestProviderAdapters/openai (0.00s)
    --- PASS: TestProviderAdapters/anthropic (0.00s)
=== RUN   TestStrictActionJSON
--- PASS: TestStrictActionJSON (0.00s)
=== RUN   TestDecisionFallback
=== RUN   TestDecisionFallback/timeout/check=true
=== RUN   TestDecisionFallback/timeout/check=false
=== RUN   TestDecisionFallback/malformed/check=true
=== RUN   TestDecisionFallback/malformed/check=false
=== RUN   TestDecisionFallback/illegal/check=true
=== RUN   TestDecisionFallback/illegal/check=false
=== RUN   TestDecisionFallback/auth/check=true
=== RUN   TestDecisionFallback/auth/check=false
=== RUN   TestDecisionFallback/oversize/check=true
=== RUN   TestDecisionFallback/oversize/check=false
=== RUN   TestDecisionFallback/protocol/check=true
=== RUN   TestDecisionFallback/protocol/check=false
--- PASS: TestDecisionFallback (0.23s)
    --- PASS: TestDecisionFallback/timeout/check=true (0.10s)
    --- PASS: TestDecisionFallback/timeout/check=false (0.10s)
    --- PASS: TestDecisionFallback/malformed/check=true (0.00s)
    --- PASS: TestDecisionFallback/malformed/check=false (0.00s)
    --- PASS: TestDecisionFallback/illegal/check=true (0.00s)
    --- PASS: TestDecisionFallback/illegal/check=false (0.00s)
    --- PASS: TestDecisionFallback/auth/check=true (0.00s)
    --- PASS: TestDecisionFallback/auth/check=false (0.00s)
    --- PASS: TestDecisionFallback/oversize/check=true (0.01s)
    --- PASS: TestDecisionFallback/oversize/check=false (0.01s)
    --- PASS: TestDecisionFallback/protocol/check=true (0.00s)
    --- PASS: TestDecisionFallback/protocol/check=false (0.00s)
=== RUN   TestObservationPrivacy
--- PASS: TestObservationPrivacy (0.00s)
=== RUN   TestAvailabilityConcurrent
--- PASS: TestAvailabilityConcurrent (0.00s)
=== RUN   TestLocalCatalogModels
    agent_test.go:363: 未提供显式只读配置路径；不自动读取用户配置
--- SKIP: TestLocalCatalogModels (0.00s)
=== RUN   TestConfigLoaderFailures
=== RUN   TestConfigLoaderFailures/duplicate-key
=== RUN   TestConfigLoaderFailures/bad-model-id
=== RUN   TestConfigLoaderFailures/unsafe-endpoint
=== RUN   TestConfigLoaderFailures/credential-newline
=== RUN   TestConfigLoaderFailures/bad-env-reference
=== RUN   TestConfigLoaderFailures/missing-file
=== RUN   TestConfigLoaderFailures/reserved-header
=== RUN   TestConfigLoaderFailures/header-collision
--- PASS: TestConfigLoaderFailures (0.00s)
    --- PASS: TestConfigLoaderFailures/duplicate-key (0.00s)
    --- PASS: TestConfigLoaderFailures/bad-model-id (0.00s)
    --- PASS: TestConfigLoaderFailures/unsafe-endpoint (0.00s)
    --- PASS: TestConfigLoaderFailures/credential-newline (0.00s)
    --- PASS: TestConfigLoaderFailures/bad-env-reference (0.00s)
    --- PASS: TestConfigLoaderFailures/missing-file (0.00s)
    --- PASS: TestConfigLoaderFailures/reserved-header (0.00s)
    --- PASS: TestConfigLoaderFailures/header-collision (0.00s)
=== RUN   TestLegalActionBoundaries
--- PASS: TestLegalActionBoundaries (0.00s)
=== RUN   TestHTTPPrivacyRedirects
--- PASS: TestHTTPPrivacyRedirects (0.00s)
=== RUN   TestCancellationAndLimits
--- PASS: TestCancellationAndLimits (0.00s)
=== RUN   TestFactoryOptionsWireTimeoutAndTokens
--- PASS: TestFactoryOptionsWireTimeoutAndTokens (0.00s)
=== RUN   TestProviderResponseErrors
=== RUN   TestProviderResponseErrors/openai
=== RUN   TestProviderResponseErrors/anthropic
--- PASS: TestProviderResponseErrors (0.00s)
    --- PASS: TestProviderResponseErrors/openai (0.00s)
    --- PASS: TestProviderResponseErrors/anthropic (0.00s)
PASS
ok  	texas-holdem/internal/agent	1.744s
=== RUN   TestDefaultCatalogDiscoveryAndLoader
=== RUN   TestDefaultCatalogDiscoveryAndLoader/json_only
=== RUN   TestDefaultCatalogDiscoveryAndLoader/jsonc_only
=== RUN   TestDefaultCatalogDiscoveryAndLoader/both
=== RUN   TestDefaultCatalogDiscoveryAndLoader/neither
--- PASS: TestDefaultCatalogDiscoveryAndLoader (0.01s)
    --- PASS: TestDefaultCatalogDiscoveryAndLoader/json_only (0.00s)
    --- PASS: TestDefaultCatalogDiscoveryAndLoader/jsonc_only (0.00s)
    --- PASS: TestDefaultCatalogDiscoveryAndLoader/both (0.00s)
    --- PASS: TestDefaultCatalogDiscoveryAndLoader/neither (0.00s)
=== RUN   TestExplicitMissingCatalogPath
=== RUN   TestExplicitMissingCatalogPath/missing.json
=== RUN   TestExplicitMissingCatalogPath/opencode.json
=== RUN   TestExplicitMissingCatalogPath/opencode.jsonc
--- PASS: TestExplicitMissingCatalogPath (0.00s)
    --- PASS: TestExplicitMissingCatalogPath/missing.json (0.00s)
    --- PASS: TestExplicitMissingCatalogPath/opencode.json (0.00s)
    --- PASS: TestExplicitMissingCatalogPath/opencode.jsonc (0.00s)
=== RUN   TestYAMLConfigWiringAndPublicWhitelist
--- PASS: TestYAMLConfigWiringAndPublicWhitelist (0.00s)
=== RUN   TestLocalOverlayPreservesExplicitFalseAndZero
--- PASS: TestLocalOverlayPreservesExplicitFalseAndZero (0.00s)
=== RUN   TestEnvOverridePriority
--- PASS: TestEnvOverridePriority (0.00s)
=== RUN   TestExplicitFilePathAndSiblingLocal
--- PASS: TestExplicitFilePathAndSiblingLocal (0.00s)
=== RUN   TestSafeConfigurationErrors
=== RUN   TestSafeConfigurationErrors/unknown_field
=== RUN   TestSafeConfigurationErrors/unknown_section
=== RUN   TestSafeConfigurationErrors/duplicate_key
=== RUN   TestSafeConfigurationErrors/multi_document
=== RUN   TestSafeConfigurationErrors/malformed
=== RUN   TestSafeConfigurationErrors/bad_type
=== RUN   TestSafeConfigurationErrors/secret_in_bad_yaml
--- PASS: TestSafeConfigurationErrors (0.00s)
    --- PASS: TestSafeConfigurationErrors/unknown_field (0.00s)
    --- PASS: TestSafeConfigurationErrors/unknown_section (0.00s)
    --- PASS: TestSafeConfigurationErrors/duplicate_key (0.00s)
    --- PASS: TestSafeConfigurationErrors/multi_document (0.00s)
    --- PASS: TestSafeConfigurationErrors/malformed (0.00s)
    --- PASS: TestSafeConfigurationErrors/bad_type (0.00s)
    --- PASS: TestSafeConfigurationErrors/secret_in_bad_yaml (0.00s)
=== RUN   TestMissingBaseAndInvalidEnv
=== RUN   TestMissingBaseAndInvalidEnv/POKER_HTTP_PORT
=== RUN   TestMissingBaseAndInvalidEnv/POKER_HTTP_TIMEOUT
=== RUN   TestMissingBaseAndInvalidEnv/POKER_FRONTEND_PORT
=== RUN   TestMissingBaseAndInvalidEnv/POKER_REDIS_DB
=== RUN   TestMissingBaseAndInvalidEnv/POKER_ALLOW_PAID_MODELS
=== RUN   TestMissingBaseAndInvalidEnv/POKER_MAX_LIVE_CALLS
=== RUN   TestMissingBaseAndInvalidEnv/POKER_DECISION_TIMEOUT
=== RUN   TestMissingBaseAndInvalidEnv/POKER_MAX_OUTPUT_TOKENS
--- PASS: TestMissingBaseAndInvalidEnv (0.01s)
    --- PASS: TestMissingBaseAndInvalidEnv/POKER_HTTP_PORT (0.00s)
    --- PASS: TestMissingBaseAndInvalidEnv/POKER_HTTP_TIMEOUT (0.00s)
    --- PASS: TestMissingBaseAndInvalidEnv/POKER_FRONTEND_PORT (0.00s)
    --- PASS: TestMissingBaseAndInvalidEnv/POKER_REDIS_DB (0.00s)
    --- PASS: TestMissingBaseAndInvalidEnv/POKER_ALLOW_PAID_MODELS (0.00s)
    --- PASS: TestMissingBaseAndInvalidEnv/POKER_MAX_LIVE_CALLS (0.00s)
    --- PASS: TestMissingBaseAndInvalidEnv/POKER_DECISION_TIMEOUT (0.00s)
    --- PASS: TestMissingBaseAndInvalidEnv/POKER_MAX_OUTPUT_TOKENS (0.00s)
=== RUN   TestHumanActionTimeoutConfig
=== RUN   TestHumanActionTimeoutConfig/default_60s
=== RUN   TestHumanActionTimeoutConfig/yaml_and_env_override
=== RUN   TestHumanActionTimeoutConfig/explicit_zero_and_bad_type_rejected
--- PASS: TestHumanActionTimeoutConfig (0.01s)
    --- PASS: TestHumanActionTimeoutConfig/default_60s (0.00s)
    --- PASS: TestHumanActionTimeoutConfig/yaml_and_env_override (0.00s)
    --- PASS: TestHumanActionTimeoutConfig/explicit_zero_and_bad_type_rejected (0.00s)
=== RUN   TestAITurnDelayConfig
=== RUN   TestAITurnDelayConfig/default_1500ms
=== RUN   TestAITurnDelayConfig/yaml_and_env_override
=== RUN   TestAITurnDelayConfig/explicit_zero_negative_and_bad_type_rejected
--- PASS: TestAITurnDelayConfig (0.01s)
    --- PASS: TestAITurnDelayConfig/default_1500ms (0.00s)
    --- PASS: TestAITurnDelayConfig/yaml_and_env_override (0.00s)
    --- PASS: TestAITurnDelayConfig/explicit_zero_negative_and_bad_type_rejected (0.00s)
=== RUN   TestHandIntervalConfig
=== RUN   TestHandIntervalConfig/default_12s_8s
=== RUN   TestHandIntervalConfig/yaml_and_env_override
=== RUN   TestHandIntervalConfig/explicit_zero_negative_and_bad_type_rejected
--- PASS: TestHandIntervalConfig (0.01s)
    --- PASS: TestHandIntervalConfig/default_12s_8s (0.00s)
    --- PASS: TestHandIntervalConfig/yaml_and_env_override (0.00s)
    --- PASS: TestHandIntervalConfig/explicit_zero_negative_and_bad_type_rejected (0.01s)
=== RUN   TestPublicDoesNotRequireStorageSecret
--- PASS: TestPublicDoesNotRequireStorageSecret (0.00s)
=== RUN   TestDirectSecretOverridesEnv
--- PASS: TestDirectSecretOverridesEnv (0.00s)
=== RUN   TestDirectSecretEmptyStringSemantics
--- PASS: TestDirectSecretEmptyStringSemantics (0.00s)
=== RUN   TestEnvOnlyFallbackWithoutDirect
--- PASS: TestEnvOnlyFallbackWithoutDirect (0.00s)
=== RUN   TestSecretsAbsentBehaveClearly
--- PASS: TestSecretsAbsentBehaveClearly (0.00s)
=== RUN   TestDirectSecretStrictTypes
=== RUN   TestDirectSecretStrictTypes/dsn_null
=== RUN   TestDirectSecretStrictTypes/dsn_int
=== RUN   TestDirectSecretStrictTypes/dsn_bool
=== RUN   TestDirectSecretStrictTypes/dsn_sequence
=== RUN   TestDirectSecretStrictTypes/password_null
=== RUN   TestDirectSecretStrictTypes/password_int
=== RUN   TestDirectSecretStrictTypes/password_bool
=== RUN   TestDirectSecretStrictTypes/password_mapping
--- PASS: TestDirectSecretStrictTypes (0.01s)
    --- PASS: TestDirectSecretStrictTypes/dsn_null (0.00s)
    --- PASS: TestDirectSecretStrictTypes/dsn_int (0.00s)
    --- PASS: TestDirectSecretStrictTypes/dsn_bool (0.00s)
    --- PASS: TestDirectSecretStrictTypes/dsn_sequence (0.00s)
    --- PASS: TestDirectSecretStrictTypes/password_null (0.00s)
    --- PASS: TestDirectSecretStrictTypes/password_int (0.00s)
    --- PASS: TestDirectSecretStrictTypes/password_bool (0.00s)
    --- PASS: TestDirectSecretStrictTypes/password_mapping (0.00s)
=== RUN   TestDirectSecretEmptyStringAccepted
--- PASS: TestDirectSecretEmptyStringAccepted (0.00s)
=== RUN   TestPublicWhitelistExcludesDirectSecrets
--- PASS: TestPublicWhitelistExcludesDirectSecrets (0.00s)
=== RUN   TestStrictSchemaRejection
=== RUN   TestStrictSchemaRejection/float_port
=== RUN   TestStrictSchemaRejection/quoted_port
=== RUN   TestStrictSchemaRejection/null_port
=== RUN   TestStrictSchemaRejection/null_host
=== RUN   TestStrictSchemaRejection/null_timeout
=== RUN   TestStrictSchemaRejection/bool_host
=== RUN   TestStrictSchemaRejection/int_host
=== RUN   TestStrictSchemaRejection/int_timeout
=== RUN   TestStrictSchemaRejection/non_mapping_section
=== RUN   TestStrictSchemaRejection/non_mapping_nested
=== RUN   TestStrictSchemaRejection/non_mapping_data
=== RUN   TestStrictSchemaRejection/string_bool
=== RUN   TestStrictSchemaRejection/string_int
=== RUN   TestStrictSchemaRejection/float_int
=== RUN   TestStrictSchemaRejection/null_section
=== RUN   TestStrictSchemaRejection/null_root
=== RUN   TestStrictSchemaRejection/scalar_root
=== RUN   TestStrictSchemaRejection/sequence_root
=== RUN   TestStrictSchemaRejection/empty_mapping
=== RUN   TestStrictSchemaRejection/empty_file
=== RUN   TestStrictSchemaRejection/comments_only
=== RUN   TestStrictSchemaRejection/duplicate_key
=== RUN   TestStrictSchemaRejection/unknown_field
=== RUN   TestStrictSchemaRejection/unknown_section
=== RUN   TestStrictSchemaRejection/merge_key
=== RUN   TestStrictSchemaRejection/alias_value
=== RUN   TestStrictSchemaRejection/multi_document
--- PASS: TestStrictSchemaRejection (0.01s)
    --- PASS: TestStrictSchemaRejection/float_port (0.00s)
    --- PASS: TestStrictSchemaRejection/quoted_port (0.00s)
    --- PASS: TestStrictSchemaRejection/null_port (0.00s)
    --- PASS: TestStrictSchemaRejection/null_host (0.00s)
    --- PASS: TestStrictSchemaRejection/null_timeout (0.00s)
    --- PASS: TestStrictSchemaRejection/bool_host (0.00s)
    --- PASS: TestStrictSchemaRejection/int_host (0.00s)
    --- PASS: TestStrictSchemaRejection/int_timeout (0.00s)
    --- PASS: TestStrictSchemaRejection/non_mapping_section (0.00s)
    --- PASS: TestStrictSchemaRejection/non_mapping_nested (0.00s)
    --- PASS: TestStrictSchemaRejection/non_mapping_data (0.00s)
    --- PASS: TestStrictSchemaRejection/string_bool (0.00s)
    --- PASS: TestStrictSchemaRejection/string_int (0.00s)
    --- PASS: TestStrictSchemaRejection/float_int (0.00s)
    --- PASS: TestStrictSchemaRejection/null_section (0.00s)
    --- PASS: TestStrictSchemaRejection/null_root (0.00s)
    --- PASS: TestStrictSchemaRejection/scalar_root (0.00s)
    --- PASS: TestStrictSchemaRejection/sequence_root (0.00s)
    --- PASS: TestStrictSchemaRejection/empty_mapping (0.00s)
    --- PASS: TestStrictSchemaRejection/empty_file (0.00s)
    --- PASS: TestStrictSchemaRejection/comments_only (0.00s)
    --- PASS: TestStrictSchemaRejection/duplicate_key (0.00s)
    --- PASS: TestStrictSchemaRejection/unknown_field (0.00s)
    --- PASS: TestStrictSchemaRejection/unknown_section (0.00s)
    --- PASS: TestStrictSchemaRejection/merge_key (0.00s)
    --- PASS: TestStrictSchemaRejection/alias_value (0.00s)
    --- PASS: TestStrictSchemaRejection/multi_document (0.00s)
=== RUN   TestValidScalarTypesAccepted
--- PASS: TestValidScalarTypesAccepted (0.00s)
=== RUN   TestHostValidation
=== RUN   TestHostValidation/127.0.0.1
=== RUN   TestHostValidation/192.168.1.10
=== RUN   TestHostValidation/localhost
=== RUN   TestHostValidation/example.com
=== RUN   TestHostValidation/sub.example.com
=== RUN   TestHostValidation/localhost/path
=== RUN   TestHostValidation/http://127.0.0.1
=== RUN   TestHostValidation/127.0.0.1:8080
=== RUN   TestHostValidation/user@host
=== RUN   TestHostValidation/_127.0.0.1
=== RUN   TestHostValidation/127.0.0.1_
=== RUN   TestHostValidation/host_name
=== RUN   TestHostValidation//absolute
=== RUN   TestHostValidation/bad_host
=== RUN   TestHostValidation/#00
--- PASS: TestHostValidation (0.00s)
    --- PASS: TestHostValidation/127.0.0.1 (0.00s)
    --- PASS: TestHostValidation/192.168.1.10 (0.00s)
    --- PASS: TestHostValidation/localhost (0.00s)
    --- PASS: TestHostValidation/example.com (0.00s)
    --- PASS: TestHostValidation/sub.example.com (0.00s)
    --- PASS: TestHostValidation/localhost/path (0.00s)
    --- PASS: TestHostValidation/http://127.0.0.1 (0.00s)
    --- PASS: TestHostValidation/127.0.0.1:8080 (0.00s)
    --- PASS: TestHostValidation/user@host (0.00s)
    --- PASS: TestHostValidation/_127.0.0.1 (0.00s)
    --- PASS: TestHostValidation/127.0.0.1_ (0.00s)
    --- PASS: TestHostValidation/host_name (0.00s)
    --- PASS: TestHostValidation//absolute (0.00s)
    --- PASS: TestHostValidation/bad_host (0.00s)
    --- PASS: TestHostValidation/#00 (0.00s)
=== RUN   TestSecretEnvNameValidation
=== RUN   TestSecretEnvNameValidation/dsn_env_1BAD
=== RUN   TestSecretEnvNameValidation/dsn_env_BAD-NAME
=== RUN   TestSecretEnvNameValidation/dsn_env_has_space
=== RUN   TestSecretEnvNameValidation/dsn_env_
=== RUN   TestSecretEnvNameValidation/dsn_env_A.B
--- PASS: TestSecretEnvNameValidation (0.00s)
    --- PASS: TestSecretEnvNameValidation/dsn_env_1BAD (0.00s)
    --- PASS: TestSecretEnvNameValidation/dsn_env_BAD-NAME (0.00s)
    --- PASS: TestSecretEnvNameValidation/dsn_env_has_space (0.00s)
    --- PASS: TestSecretEnvNameValidation/dsn_env_ (0.00s)
    --- PASS: TestSecretEnvNameValidation/dsn_env_A.B (0.00s)
=== RUN   TestCatalogPathExplicitEmptyRejected
--- PASS: TestCatalogPathExplicitEmptyRejected (0.00s)
=== RUN   TestLocalOverlaySemantics
=== RUN   TestLocalOverlaySemantics/comments_noop
=== RUN   TestLocalOverlaySemantics/empty_mapping_noop
=== RUN   TestLocalOverlaySemantics/root_null
=== RUN   TestLocalOverlaySemantics/field_null
=== RUN   TestLocalOverlaySemantics/null_section
=== RUN   TestLocalOverlaySemantics/unknown_field
=== RUN   TestLocalOverlaySemantics/alias
--- PASS: TestLocalOverlaySemantics (0.01s)
    --- PASS: TestLocalOverlaySemantics/comments_noop (0.00s)
    --- PASS: TestLocalOverlaySemantics/empty_mapping_noop (0.00s)
    --- PASS: TestLocalOverlaySemantics/root_null (0.00s)
    --- PASS: TestLocalOverlaySemantics/field_null (0.00s)
    --- PASS: TestLocalOverlaySemantics/null_section (0.00s)
    --- PASS: TestLocalOverlaySemantics/unknown_field (0.00s)
    --- PASS: TestLocalOverlaySemantics/alias (0.00s)
=== RUN   TestDiscoverPathExplicitEmptyRejected
--- PASS: TestDiscoverPathExplicitEmptyRejected (0.00s)
=== RUN   TestLoadFileEmptyRejected
--- PASS: TestLoadFileEmptyRejected (0.00s)
PASS
ok  	texas-holdem/internal/conf	1.437s
=== RUN   TestRepositoryReadCancellation
=== RUN   TestRepositoryReadCancellation/context_canceled/guard
=== RUN   TestRepositoryReadCancellation/context_canceled/get
=== RUN   TestRepositoryReadCancellation/context_canceled/current
=== RUN   TestRepositoryReadCancellation/context_canceled/hand
=== RUN   TestRepositoryReadCancellation/context_canceled/health_guard
=== RUN   TestRepositoryReadCancellation/context_canceled/health_mysql
=== RUN   TestRepositoryReadCancellation/context_canceled/health_redis
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/guard
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/get
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/current
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/hand
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/health_guard
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/health_mysql
=== RUN   TestRepositoryReadCancellation/context_deadline_exceeded/health_redis
--- PASS: TestRepositoryReadCancellation (0.01s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/guard (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/get (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/current (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/hand (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/health_guard (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/health_mysql (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_canceled/health_redis (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/guard (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/get (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/current (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/hand (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/health_guard (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/health_mysql (0.00s)
    --- PASS: TestRepositoryReadCancellation/context_deadline_exceeded/health_redis (0.00s)
=== RUN   TestRepositoryCanceledContext
=== RUN   TestRepositoryCanceledContext/cancel
=== RUN   TestRepositoryCanceledContext/deadline
--- PASS: TestRepositoryCanceledContext (0.00s)
    --- PASS: TestRepositoryCanceledContext/cancel (0.00s)
    --- PASS: TestRepositoryCanceledContext/deadline (0.00s)
=== RUN   TestRepositoryRealFailureWinsOverCancellation
=== RUN   TestRepositoryRealFailureWinsOverCancellation/lock_lost
=== RUN   TestRepositoryRealFailureWinsOverCancellation/guard_database
=== RUN   TestRepositoryRealFailureWinsOverCancellation/load_database
=== RUN   TestRepositoryRealFailureWinsOverCancellation/mysql_then_cache_cancel
--- PASS: TestRepositoryRealFailureWinsOverCancellation (0.00s)
    --- PASS: TestRepositoryRealFailureWinsOverCancellation/lock_lost (0.00s)
    --- PASS: TestRepositoryRealFailureWinsOverCancellation/guard_database (0.00s)
    --- PASS: TestRepositoryRealFailureWinsOverCancellation/load_database (0.00s)
    --- PASS: TestRepositoryRealFailureWinsOverCancellation/mysql_then_cache_cancel (0.00s)
=== RUN   TestRepositoryWriteCancellationRemainsConservative
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_canceled/create
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_canceled/begin
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_canceled/update
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_canceled/commit
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/create
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/begin
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/update
=== RUN   TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/commit
--- PASS: TestRepositoryWriteCancellationRemainsConservative (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_canceled/create (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_canceled/begin (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_canceled/update (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_canceled/commit (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/create (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/begin (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/update (0.00s)
    --- PASS: TestRepositoryWriteCancellationRemainsConservative/context_deadline_exceeded/commit (0.00s)
=== RUN   TestGuardPollingCancellationDoesNotCancelLockQuery
=== RUN   TestGuardPollingCancellationDoesNotCancelLockQuery/cancel
=== RUN   TestGuardPollingCancellationDoesNotCancelLockQuery/deadline
--- PASS: TestGuardPollingCancellationDoesNotCancelLockQuery (0.01s)
    --- PASS: TestGuardPollingCancellationDoesNotCancelLockQuery/cancel (0.00s)
    --- PASS: TestGuardPollingCancellationDoesNotCancelLockQuery/deadline (0.01s)
=== RUN   TestRepositoryCommitAtomicity
=== RUN   TestRepositoryCommitAtomicity/commit
=== RUN   TestRepositoryCommitAtomicity/cas_conflict
=== RUN   TestRepositoryCommitAtomicity/hand_failure
=== RUN   TestRepositoryCommitAtomicity/commit_failure
--- PASS: TestRepositoryCommitAtomicity (0.00s)
    --- PASS: TestRepositoryCommitAtomicity/commit (0.00s)
    --- PASS: TestRepositoryCommitAtomicity/cas_conflict (0.00s)
    --- PASS: TestRepositoryCommitAtomicity/hand_failure (0.00s)
    --- PASS: TestRepositoryCommitAtomicity/commit_failure (0.00s)
=== RUN   TestRedisDegradation
--- PASS: TestRedisDegradation (0.00s)
=== RUN   TestSingleInstanceLockGuard
--- PASS: TestSingleInstanceLockGuard (0.00s)
=== RUN   TestSchemaFailsClosed
--- PASS: TestSchemaFailsClosed (0.00s)
PASS
ok  	texas-holdem/internal/data	2.006s
=== RUN   TestHandCategoryNamesAndBestFive
=== RUN   TestHandCategoryNamesAndBestFive/high_card
=== RUN   TestHandCategoryNamesAndBestFive/one_pair
=== RUN   TestHandCategoryNamesAndBestFive/two_pair
=== RUN   TestHandCategoryNamesAndBestFive/three_of_a_kind
=== RUN   TestHandCategoryNamesAndBestFive/straight
=== RUN   TestHandCategoryNamesAndBestFive/flush
=== RUN   TestHandCategoryNamesAndBestFive/full_house
=== RUN   TestHandCategoryNamesAndBestFive/four_of_a_kind
=== RUN   TestHandCategoryNamesAndBestFive/straight_flush
--- PASS: TestHandCategoryNamesAndBestFive (0.00s)
    --- PASS: TestHandCategoryNamesAndBestFive/high_card (0.00s)
    --- PASS: TestHandCategoryNamesAndBestFive/one_pair (0.00s)
    --- PASS: TestHandCategoryNamesAndBestFive/two_pair (0.00s)
    --- PASS: TestHandCategoryNamesAndBestFive/three_of_a_kind (0.00s)
    --- PASS: TestHandCategoryNamesAndBestFive/straight (0.00s)
    --- PASS: TestHandCategoryNamesAndBestFive/flush (0.00s)
    --- PASS: TestHandCategoryNamesAndBestFive/full_house (0.00s)
    --- PASS: TestHandCategoryNamesAndBestFive/four_of_a_kind (0.00s)
    --- PASS: TestHandCategoryNamesAndBestFive/straight_flush (0.00s)
=== RUN   TestSettleFoldSecrecy
=== RUN   TestSettleFoldSecrecy/showdown_hides_folded
=== RUN   TestSettleFoldSecrecy/uncontested_hides_all
--- PASS: TestSettleFoldSecrecy (0.00s)
    --- PASS: TestSettleFoldSecrecy/showdown_hides_folded (0.00s)
    --- PASS: TestSettleFoldSecrecy/uncontested_hides_all (0.00s)
=== RUN   TestPrivateHoleAccess
=== RUN   TestPrivateHoleAccess/actor_and_non_actor_read_own_cards
=== RUN   TestPrivateHoleAccess/folded_allin_complete_still_readable
=== RUN   TestPrivateHoleAccess/illegal_seat_and_undealt_refused
--- PASS: TestPrivateHoleAccess (0.00s)
    --- PASS: TestPrivateHoleAccess/actor_and_non_actor_read_own_cards (0.00s)
    --- PASS: TestPrivateHoleAccess/folded_allin_complete_still_readable (0.00s)
    --- PASS: TestPrivateHoleAccess/illegal_seat_and_undealt_refused (0.00s)
=== RUN   TestBlindAndActionOrder
=== RUN   TestBlindAndActionOrder/heads_up
=== RUN   TestBlindAndActionOrder/nine_seats
=== RUN   TestBlindAndActionOrder/sparse_and_out_seats
--- PASS: TestBlindAndActionOrder (0.00s)
    --- PASS: TestBlindAndActionOrder/heads_up (0.00s)
    --- PASS: TestBlindAndActionOrder/nine_seats (0.00s)
    --- PASS: TestBlindAndActionOrder/sparse_and_out_seats (0.00s)
=== RUN   TestMinimumRaise
--- PASS: TestMinimumRaise (0.00s)
=== RUN   TestShortAllInReopening
=== RUN   TestShortAllInReopening/single_does_not_reopen
=== RUN   TestShortAllInReopening/cumulative_reopens
=== RUN   TestShortAllInReopening/unacted_retains_right
--- PASS: TestShortAllInReopening (0.00s)
    --- PASS: TestShortAllInReopening/single_does_not_reopen (0.00s)
    --- PASS: TestShortAllInReopening/cumulative_reopens (0.00s)
    --- PASS: TestShortAllInReopening/unacted_retains_right (0.00s)
=== RUN   TestNoRaiseWithoutResponder
--- PASS: TestNoRaiseWithoutResponder (0.00s)
=== RUN   TestBlindShortStack
--- PASS: TestBlindShortStack (0.00s)
=== RUN   TestSidePotsAndRefunds
=== RUN   TestSidePotsAndRefunds/layers_folded_money_refund
=== RUN   TestSidePotsAndRefunds/actual_action_chain
--- PASS: TestSidePotsAndRefunds (0.00s)
    --- PASS: TestSidePotsAndRefunds/layers_folded_money_refund (0.00s)
    --- PASS: TestSidePotsAndRefunds/actual_action_chain (0.00s)
=== RUN   TestSplitPotOddChip
--- PASS: TestSplitPotOddChip (0.00s)
=== RUN   TestHandRanking
--- PASS: TestHandRanking (0.00s)
=== RUN   TestRunoutAndUncontested
--- PASS: TestRunoutAndUncontested (0.00s)
=== RUN   TestChipConservationRandomSequences
--- PASS: TestChipConservationRandomSequences (0.28s)
=== RUN   TestObservationPrivacy
--- PASS: TestObservationPrivacy (0.00s)
=== RUN   TestOutputIsolation
--- PASS: TestOutputIsolation (0.00s)
=== RUN   TestConcurrentBranchIsolation
--- PASS: TestConcurrentBranchIsolation (0.00s)
=== RUN   TestInitializationValidation
=== RUN   TestInitializationValidation/one
=== RUN   TestInitializationValidation/duplicate_seat
=== RUN   TestInitializationValidation/seat_range
=== RUN   TestInitializationValidation/negative
=== RUN   TestInitializationValidation/total_limit
=== RUN   TestInitializationValidation/overflow
=== RUN   TestInitializationValidation/only_one_funded
=== RUN   TestInitializationValidation/ineligible_button
--- PASS: TestInitializationValidation (0.00s)
    --- PASS: TestInitializationValidation/one (0.00s)
    --- PASS: TestInitializationValidation/duplicate_seat (0.00s)
    --- PASS: TestInitializationValidation/seat_range (0.00s)
    --- PASS: TestInitializationValidation/negative (0.00s)
    --- PASS: TestInitializationValidation/total_limit (0.00s)
    --- PASS: TestInitializationValidation/overflow (0.00s)
    --- PASS: TestInitializationValidation/only_one_funded (0.00s)
    --- PASS: TestInitializationValidation/ineligible_button (0.00s)
=== RUN   TestActionValidation
--- PASS: TestActionValidation (0.00s)
=== RUN   TestSnapshotValidationAndResume
--- PASS: TestSnapshotValidationAndResume (0.00s)
PASS
ok  	texas-holdem/internal/poker	1.924s
=== RUN   TestPublicDTOPrivacy
--- PASS: TestPublicDTOPrivacy (0.00s)
=== RUN   TestActionDTOCarriesAmount
=== RUN   TestActionDTOCarriesAmount/call
=== RUN   TestActionDTOCarriesAmount/raise_to
=== RUN   TestActionDTOCarriesAmount/fold
=== RUN   TestActionDTOCarriesAmount/check
--- PASS: TestActionDTOCarriesAmount (0.00s)
    --- PASS: TestActionDTOCarriesAmount/call (0.00s)
    --- PASS: TestActionDTOCarriesAmount/raise_to (0.00s)
    --- PASS: TestActionDTOCarriesAmount/fold (0.00s)
    --- PASS: TestActionDTOCarriesAmount/check (0.00s)
=== RUN   TestCompletedPotSeatArrays
=== RUN   TestCompletedPotSeatArrays/nonempty
=== RUN   TestCompletedPotSeatArrays/multiple_winners
=== RUN   TestCompletedPotSeatArrays/empty
=== RUN   TestCompletedPotSeatArrays/nil
--- PASS: TestCompletedPotSeatArrays (0.00s)
    --- PASS: TestCompletedPotSeatArrays/nonempty (0.00s)
    --- PASS: TestCompletedPotSeatArrays/multiple_winners (0.00s)
    --- PASS: TestCompletedPotSeatArrays/empty (0.00s)
    --- PASS: TestCompletedPotSeatArrays/nil (0.00s)
=== RUN   TestPort8081SecurityContract
=== RUN   TestPort8081SecurityContract/loopback_8081_same_origin
=== RUN   TestPort8081SecurityContract/localhost_8081_same_origin
=== RUN   TestPort8081SecurityContract/old_loopback_host_8080
=== RUN   TestPort8081SecurityContract/old_localhost_host_8080
=== RUN   TestPort8081SecurityContract/neighbor_host_8082
=== RUN   TestPort8081SecurityContract/old_origin_8080
=== RUN   TestPort8081SecurityContract/neighbor_origin_8082
=== RUN   TestPort8081SecurityContract/hostname_mismatch
--- PASS: TestPort8081SecurityContract (0.00s)
    --- PASS: TestPort8081SecurityContract/loopback_8081_same_origin (0.00s)
    --- PASS: TestPort8081SecurityContract/localhost_8081_same_origin (0.00s)
    --- PASS: TestPort8081SecurityContract/old_loopback_host_8080 (0.00s)
    --- PASS: TestPort8081SecurityContract/old_localhost_host_8080 (0.00s)
    --- PASS: TestPort8081SecurityContract/neighbor_host_8082 (0.00s)
    --- PASS: TestPort8081SecurityContract/old_origin_8080 (0.00s)
    --- PASS: TestPort8081SecurityContract/neighbor_origin_8082 (0.00s)
    --- PASS: TestPort8081SecurityContract/hostname_mismatch (0.00s)
=== RUN   TestDynamicListenerAndOriginWhitelist
=== RUN   TestDynamicListenerAndOriginWhitelist/new_port_same_origin
=== RUN   TestDynamicListenerAndOriginWhitelist/new_port_localhost_same_origin
=== RUN   TestDynamicListenerAndOriginWhitelist/old_port_8081_host_rejected
=== RUN   TestDynamicListenerAndOriginWhitelist/old_port_8081_origin_rejected
=== RUN   TestDynamicListenerAndOriginWhitelist/cross_origin_rejected
--- PASS: TestDynamicListenerAndOriginWhitelist (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/new_port_same_origin (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/new_port_localhost_same_origin (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/old_port_8081_host_rejected (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/old_port_8081_origin_rejected (0.00s)
    --- PASS: TestDynamicListenerAndOriginWhitelist/cross_origin_rejected (0.00s)
=== RUN   TestPublicDTOPrivacyHTTP
--- PASS: TestPublicDTOPrivacyHTTP (0.00s)
=== RUN   TestModelsWrapperAndHealth
--- PASS: TestModelsWrapperAndHealth (0.00s)
=== RUN   TestCompletedPotSeatArraysHTTP
=== RUN   TestCompletedPotSeatArraysHTTP/nonempty
=== RUN   TestCompletedPotSeatArraysHTTP/multiple_winners
=== RUN   TestCompletedPotSeatArraysHTTP/empty
=== RUN   TestCompletedPotSeatArraysHTTP/nil
--- PASS: TestCompletedPotSeatArraysHTTP (0.00s)
    --- PASS: TestCompletedPotSeatArraysHTTP/nonempty (0.00s)
    --- PASS: TestCompletedPotSeatArraysHTTP/multiple_winners (0.00s)
    --- PASS: TestCompletedPotSeatArraysHTTP/empty (0.00s)
    --- PASS: TestCompletedPotSeatArraysHTTP/nil (0.00s)
=== RUN   TestInputLimitsAndOrigins
=== RUN   TestInputLimitsAndOrigins/default_mock
=== RUN   TestInputLimitsAndOrigins/same_origin
=== RUN   TestInputLimitsAndOrigins/cross_origin
=== RUN   TestInputLimitsAndOrigins/dns_rebinding
=== RUN   TestInputLimitsAndOrigins/null_origin
=== RUN   TestInputLimitsAndOrigins/fetch_cross_site
=== RUN   TestInputLimitsAndOrigins/oversized_body
=== RUN   TestInputLimitsAndOrigins/oversized_headers
=== RUN   TestInputLimitsAndOrigins/unknown_field
=== RUN   TestInputLimitsAndOrigins/missing_required
=== RUN   TestInputLimitsAndOrigins/duplicate_key
=== RUN   TestInputLimitsAndOrigins/trailing_value
=== RUN   TestInputLimitsAndOrigins/not_json
=== RUN   TestInputLimitsAndOrigins/null_mode
--- PASS: TestInputLimitsAndOrigins (0.00s)
    --- PASS: TestInputLimitsAndOrigins/default_mock (0.00s)
    --- PASS: TestInputLimitsAndOrigins/same_origin (0.00s)
    --- PASS: TestInputLimitsAndOrigins/cross_origin (0.00s)
    --- PASS: TestInputLimitsAndOrigins/dns_rebinding (0.00s)
    --- PASS: TestInputLimitsAndOrigins/null_origin (0.00s)
    --- PASS: TestInputLimitsAndOrigins/fetch_cross_site (0.00s)
    --- PASS: TestInputLimitsAndOrigins/oversized_body (0.00s)
    --- PASS: TestInputLimitsAndOrigins/oversized_headers (0.00s)
    --- PASS: TestInputLimitsAndOrigins/unknown_field (0.00s)
    --- PASS: TestInputLimitsAndOrigins/missing_required (0.00s)
    --- PASS: TestInputLimitsAndOrigins/duplicate_key (0.00s)
    --- PASS: TestInputLimitsAndOrigins/trailing_value (0.00s)
    --- PASS: TestInputLimitsAndOrigins/not_json (0.00s)
    --- PASS: TestInputLimitsAndOrigins/null_mode (0.00s)
=== RUN   TestSafeErrorsAndCurrent
--- PASS: TestSafeErrorsAndCurrent (0.00s)
=== RUN   TestKratosHTTPConfigurationAndStatic
--- PASS: TestKratosHTTPConfigurationAndStatic (0.01s)
=== RUN   TestHumanPublicDTOPrivacy
--- PASS: TestHumanPublicDTOPrivacy (0.00s)
=== RUN   TestHumanPlayerDTOPrivacy
--- PASS: TestHumanPlayerDTOPrivacy (0.00s)
=== RUN   TestHumanHTTPPlayerAndAction
=== RUN   TestHumanHTTPPlayerAndAction/missing_cookie_403
=== RUN   TestHumanHTTPPlayerAndAction/duplicate_cookie_403
=== RUN   TestHumanHTTPPlayerAndAction/valid_cookie_200
=== RUN   TestHumanHTTPPlayerAndAction/action_valid_and_bad
=== RUN   TestHumanHTTPPlayerAndAction/error_mapping
--- PASS: TestHumanHTTPPlayerAndAction (0.00s)
    --- PASS: TestHumanHTTPPlayerAndAction/missing_cookie_403 (0.00s)
    --- PASS: TestHumanHTTPPlayerAndAction/duplicate_cookie_403 (0.00s)
    --- PASS: TestHumanHTTPPlayerAndAction/valid_cookie_200 (0.00s)
    --- PASS: TestHumanHTTPPlayerAndAction/action_valid_and_bad (0.00s)
    --- PASS: TestHumanHTTPPlayerAndAction/error_mapping (0.00s)
=== RUN   TestHumanCreateSetsCookieAndLocalOnly
=== RUN   TestHumanCreateSetsCookieAndLocalOnly/loopback_sets_cookie
=== RUN   TestHumanCreateSetsCookieAndLocalOnly/non_loopback_human_rejected
--- PASS: TestHumanCreateSetsCookieAndLocalOnly (0.00s)
    --- PASS: TestHumanCreateSetsCookieAndLocalOnly/loopback_sets_cookie (0.00s)
    --- PASS: TestHumanCreateSetsCookieAndLocalOnly/non_loopback_human_rejected (0.00s)
=== RUN   TestAbortedPollingDoesNotFaultManager
=== RUN   TestAbortedPollingDoesNotFaultManager/cancel/get
=== RUN   TestAbortedPollingDoesNotFaultManager/cancel/current
=== RUN   TestAbortedPollingDoesNotFaultManager/cancel/hand
=== RUN   TestAbortedPollingDoesNotFaultManager/cancel/health
=== RUN   TestAbortedPollingDoesNotFaultManager/deadline/get
=== RUN   TestAbortedPollingDoesNotFaultManager/deadline/current
=== RUN   TestAbortedPollingDoesNotFaultManager/deadline/hand
=== RUN   TestAbortedPollingDoesNotFaultManager/deadline/health
--- PASS: TestAbortedPollingDoesNotFaultManager (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/cancel/get (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/cancel/current (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/cancel/hand (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/cancel/health (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/deadline/get (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/deadline/current (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/deadline/hand (0.00s)
    --- PASS: TestAbortedPollingDoesNotFaultManager/deadline/health (0.00s)
=== RUN   TestCompletedHandShowdownDTO
--- PASS: TestCompletedHandShowdownDTO (0.00s)
PASS
ok  	texas-holdem/internal/server	2.178s
=== RUN   TestAITurnDelayInterruptibleByContext
=== RUN   TestAITurnDelayInterruptibleByContext/pre_canceled
=== RUN   TestAITurnDelayInterruptibleByContext/cancel_during_pause
=== RUN   TestAITurnDelayInterruptibleByContext/zero_delay_is_noop
--- PASS: TestAITurnDelayInterruptibleByContext (0.02s)
    --- PASS: TestAITurnDelayInterruptibleByContext/pre_canceled (0.00s)
    --- PASS: TestAITurnDelayInterruptibleByContext/cancel_during_pause (0.02s)
    --- PASS: TestAITurnDelayInterruptibleByContext/zero_delay_is_noop (0.00s)
=== RUN   TestAITurnDelayPacesAIActions
--- PASS: TestAITurnDelayPacesAIActions (0.16s)
=== RUN   TestAITurnDelaySkippedForLiveMode
--- PASS: TestAITurnDelaySkippedForLiveMode (0.05s)
=== RUN   TestBackgroundFailureDiagnostics
=== RUN   TestBackgroundFailureDiagnostics/snapshot_decode
=== RUN   TestBackgroundFailureDiagnostics/model_construction
=== RUN   TestBackgroundFailureDiagnostics/instance_lock_txn
=== RUN   TestBackgroundFailureDiagnostics/auth_denied_is_not_fault
--- PASS: TestBackgroundFailureDiagnostics (0.00s)
    --- PASS: TestBackgroundFailureDiagnostics/snapshot_decode (0.00s)
    --- PASS: TestBackgroundFailureDiagnostics/model_construction (0.00s)
    --- PASS: TestBackgroundFailureDiagnostics/instance_lock_txn (0.00s)
    --- PASS: TestBackgroundFailureDiagnostics/auth_denied_is_not_fault (0.00s)
=== RUN   TestHandIntervalGatesNextHand
--- PASS: TestHandIntervalGatesNextHand (0.33s)
=== RUN   TestHandIntervalNotBypassedByPauseResume
--- PASS: TestHandIntervalNotBypassedByPauseResume (0.63s)
=== RUN   TestHandIntervalSurvivesRestart
--- PASS: TestHandIntervalSurvivesRestart (0.70s)
=== RUN   TestHandIntervalLastHandStops
--- PASS: TestHandIntervalLastHandStops (0.02s)
=== RUN   TestHumanSeatBackwardCompatibility
--- PASS: TestHumanSeatBackwardCompatibility (0.00s)
=== RUN   TestHumanSeatValidation
=== RUN   TestHumanSeatValidation/two_humans
=== RUN   TestHumanSeatValidation/human_with_model_ref
=== RUN   TestHumanSeatValidation/human_zero_stack
=== RUN   TestHumanSeatValidation/human_without_session
=== RUN   TestHumanSeatValidation/ai_with_session
=== RUN   TestHumanSeatValidation/unknown_kind
--- PASS: TestHumanSeatValidation (0.00s)
    --- PASS: TestHumanSeatValidation/two_humans (0.00s)
    --- PASS: TestHumanSeatValidation/human_with_model_ref (0.00s)
    --- PASS: TestHumanSeatValidation/human_zero_stack (0.00s)
    --- PASS: TestHumanSeatValidation/human_without_session (0.00s)
    --- PASS: TestHumanSeatValidation/ai_with_session (0.00s)
    --- PASS: TestHumanSeatValidation/unknown_kind (0.00s)
=== RUN   TestPlayerSessionAuthorization
--- PASS: TestPlayerSessionAuthorization (0.00s)
=== RUN   TestHumanActions
=== RUN   TestHumanActions/check
=== RUN   TestHumanActions/fold
=== RUN   TestHumanActions/call
=== RUN   TestHumanActions/raise_to
--- PASS: TestHumanActions (0.01s)
    --- PASS: TestHumanActions/check (0.00s)
    --- PASS: TestHumanActions/fold (0.00s)
    --- PASS: TestHumanActions/call (0.00s)
    --- PASS: TestHumanActions/raise_to (0.00s)
=== RUN   TestActionAmount
=== RUN   TestActionAmount/fold
=== RUN   TestActionAmount/check
=== RUN   TestActionAmount/call
=== RUN   TestActionAmount/raise_to
--- PASS: TestActionAmount (0.00s)
    --- PASS: TestActionAmount/fold (0.00s)
    --- PASS: TestActionAmount/check (0.00s)
    --- PASS: TestActionAmount/call (0.00s)
    --- PASS: TestActionAmount/raise_to (0.00s)
=== RUN   TestHumanActionAmount
=== RUN   TestHumanActionAmount/check
=== RUN   TestHumanActionAmount/fold
=== RUN   TestHumanActionAmount/call
=== RUN   TestHumanActionAmount/raise_to
--- PASS: TestHumanActionAmount (0.01s)
    --- PASS: TestHumanActionAmount/check (0.00s)
    --- PASS: TestHumanActionAmount/fold (0.00s)
    --- PASS: TestHumanActionAmount/call (0.00s)
    --- PASS: TestHumanActionAmount/raise_to (0.00s)
=== RUN   TestHumanIllegalAction
--- PASS: TestHumanIllegalAction (0.00s)
=== RUN   TestHumanTimeoutAlwaysFolds
--- PASS: TestHumanTimeoutAlwaysFolds (0.00s)
=== RUN   TestHumanActionConcurrency
=== RUN   TestHumanActionConcurrency/expired_act_triggers_timeout_fold
=== RUN   TestHumanActionConcurrency/stale_and_double_submit
=== RUN   TestHumanActionConcurrency/concurrent_act_single_commit
--- PASS: TestHumanActionConcurrency (0.01s)
    --- PASS: TestHumanActionConcurrency/expired_act_triggers_timeout_fold (0.00s)
    --- PASS: TestHumanActionConcurrency/stale_and_double_submit (0.00s)
    --- PASS: TestHumanActionConcurrency/concurrent_act_single_commit (0.00s)
=== RUN   TestHumanPauseResumeStop
--- PASS: TestHumanPauseResumeStop (0.00s)
=== RUN   TestHumanPauseAfterExpiryFolds
--- PASS: TestHumanPauseAfterExpiryFolds (0.00s)
=== RUN   TestHumanRecovery
=== RUN   TestHumanRecovery/future_wait_frozen
=== RUN   TestHumanRecovery/expired_wait_zero_remaining
--- PASS: TestHumanRecovery (0.00s)
    --- PASS: TestHumanRecovery/future_wait_frozen (0.00s)
    --- PASS: TestHumanRecovery/expired_wait_zero_remaining (0.00s)
=== RUN   TestHumanLiveBudgetIsolation
--- PASS: TestHumanLiveBudgetIsolation (0.00s)
=== RUN   TestHumanCommitAtomicity
--- PASS: TestHumanCommitAtomicity (0.00s)
=== RUN   TestLiveAuthorizationRecheckedAtRuntime
=== RUN   TestLiveAuthorizationRecheckedAtRuntime/start_forbidden
=== RUN   TestLiveAuthorizationRecheckedAtRuntime/resume_forbidden
=== RUN   TestLiveAuthorizationRecheckedAtRuntime/running_worker_pauses_without_fault
--- PASS: TestLiveAuthorizationRecheckedAtRuntime (0.00s)
    --- PASS: TestLiveAuthorizationRecheckedAtRuntime/start_forbidden (0.00s)
    --- PASS: TestLiveAuthorizationRecheckedAtRuntime/resume_forbidden (0.00s)
    --- PASS: TestLiveAuthorizationRecheckedAtRuntime/running_worker_pauses_without_fault (0.00s)
=== RUN   TestTerminalGameReleasesDeciders
=== RUN   TestTerminalGameReleasesDeciders/stop_releases
=== RUN   TestTerminalGameReleasesDeciders/completion_releases
--- PASS: TestTerminalGameReleasesDeciders (0.03s)
    --- PASS: TestTerminalGameReleasesDeciders/stop_releases (0.00s)
    --- PASS: TestTerminalGameReleasesDeciders/completion_releases (0.03s)
=== RUN   TestDecisionFallback
=== RUN   TestDecisionFallback/timeout/fold
=== RUN   TestDecisionFallback/timeout/check
=== RUN   TestDecisionFallback/protocol/fold
=== RUN   TestDecisionFallback/protocol/check
=== RUN   TestDecisionFallback/illegal/fold
=== RUN   TestDecisionFallback/illegal/check
--- PASS: TestDecisionFallback (0.04s)
    --- PASS: TestDecisionFallback/timeout/fold (0.01s)
    --- PASS: TestDecisionFallback/timeout/check (0.01s)
    --- PASS: TestDecisionFallback/protocol/fold (0.00s)
    --- PASS: TestDecisionFallback/protocol/check (0.00s)
    --- PASS: TestDecisionFallback/illegal/fold (0.00s)
    --- PASS: TestDecisionFallback/illegal/check (0.00s)
=== RUN   TestAIActionAmount
=== RUN   TestAIActionAmount/call
=== RUN   TestAIActionAmount/raise_to
--- PASS: TestAIActionAmount (0.01s)
    --- PASS: TestAIActionAmount/call (0.00s)
    --- PASS: TestAIActionAmount/raise_to (0.00s)
=== RUN   TestLiveGateAndBudget
=== RUN   TestLiveGateAndBudget/default_zero_network
=== RUN   TestLiveGateAndBudget/dual_authorization
=== RUN   TestLiveGateAndBudget/hard_cap_and_mock_budget
=== RUN   TestLiveGateAndBudget/failed_call_precharged_and_limit_pauses
--- PASS: TestLiveGateAndBudget (0.06s)
    --- PASS: TestLiveGateAndBudget/default_zero_network (0.05s)
    --- PASS: TestLiveGateAndBudget/dual_authorization (0.00s)
    --- PASS: TestLiveGateAndBudget/hard_cap_and_mock_budget (0.00s)
    --- PASS: TestLiveGateAndBudget/failed_call_precharged_and_limit_pauses (0.00s)
=== RUN   TestPauseResumeStop
--- PASS: TestPauseResumeStop (0.00s)
=== RUN   TestRecoveryPausesRunningGame
--- PASS: TestRecoveryPausesRunningGame (0.00s)
=== RUN   TestRepositoryCommitAtomicity
--- PASS: TestRepositoryCommitAtomicity (0.00s)
=== RUN   TestZeroStacksAndStorageFailure
--- PASS: TestZeroStacksAndStorageFailure (0.02s)
=== RUN   TestModelErrorCodeAllowlist
--- PASS: TestModelErrorCodeAllowlist (0.00s)
=== RUN   TestReadCancellationKeepsWorker
=== RUN   TestReadCancellationKeepsWorker/cancel/get
=== RUN   TestReadCancellationKeepsWorker/cancel/current
=== RUN   TestReadCancellationKeepsWorker/cancel/hand_lookup
=== RUN   TestReadCancellationKeepsWorker/cancel/hand
=== RUN   TestReadCancellationKeepsWorker/cancel/get_health
=== RUN   TestReadCancellationKeepsWorker/cancel/health
=== RUN   TestReadCancellationKeepsWorker/deadline/get
=== RUN   TestReadCancellationKeepsWorker/deadline/current
=== RUN   TestReadCancellationKeepsWorker/deadline/hand_lookup
=== RUN   TestReadCancellationKeepsWorker/deadline/hand
=== RUN   TestReadCancellationKeepsWorker/deadline/get_health
=== RUN   TestReadCancellationKeepsWorker/deadline/health
--- PASS: TestReadCancellationKeepsWorker (0.05s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/get (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/current (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/hand_lookup (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/hand (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/get_health (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/cancel/health (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/get (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/current (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/hand_lookup (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/hand (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/get_health (0.00s)
    --- PASS: TestReadCancellationKeepsWorker/deadline/health (0.00s)
=== RUN   TestReadStorageFailureStillLatches
=== RUN   TestReadStorageFailureStillLatches/STORAGE_UNAVAILABLE
=== RUN   TestReadStorageFailureStillLatches/TEST_DATABASE_ERROR
=== RUN   TestReadStorageFailureStillLatches/context_deadline_exceeded
--- PASS: TestReadStorageFailureStillLatches (0.00s)
    --- PASS: TestReadStorageFailureStillLatches/STORAGE_UNAVAILABLE (0.00s)
    --- PASS: TestReadStorageFailureStillLatches/TEST_DATABASE_ERROR (0.00s)
    --- PASS: TestReadStorageFailureStillLatches/context_deadline_exceeded (0.00s)
=== RUN   TestUncertainBudgetWriteStillFaults
=== RUN   TestUncertainBudgetWriteStillFaults/context_canceled
=== RUN   TestUncertainBudgetWriteStillFaults/context_deadline_exceeded
--- PASS: TestUncertainBudgetWriteStillFaults (0.00s)
    --- PASS: TestUncertainBudgetWriteStillFaults/context_canceled (0.00s)
    --- PASS: TestUncertainBudgetWriteStillFaults/context_deadline_exceeded (0.00s)
=== RUN   TestHealthReadFailureStillLatches
=== RUN   TestHealthReadFailureStillLatches/database/get_health
=== RUN   TestHealthReadFailureStillLatches/database/health
=== RUN   TestHealthReadFailureStillLatches/unmarked_database/get_health
=== RUN   TestHealthReadFailureStillLatches/unmarked_database/health
=== RUN   TestHealthReadFailureStillLatches/dependency_deadline/get_health
=== RUN   TestHealthReadFailureStillLatches/dependency_deadline/health
=== RUN   TestHealthReadFailureStillLatches/confirmed_mysql_failure_with_cancel/get_health
=== RUN   TestHealthReadFailureStillLatches/confirmed_mysql_failure_with_cancel/health
--- PASS: TestHealthReadFailureStillLatches (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/database/get_health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/database/health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/unmarked_database/get_health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/unmarked_database/health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/dependency_deadline/get_health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/dependency_deadline/health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/confirmed_mysql_failure_with_cancel/get_health (0.00s)
    --- PASS: TestHealthReadFailureStillLatches/confirmed_mysql_failure_with_cancel/health (0.00s)
PASS
ok  	texas-holdem/internal/service	3.981s
?   	texas-holdem/web	[no test files]
```
