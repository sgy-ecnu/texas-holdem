# agent lane 交付证据

## 范围与结果

仅写 `internal/agent/**`；未改 `go.mod/go.sum`、未引入外部依赖、未写扑克 stub、未调用真实模型、未 commit/push。先写 `agent_test.go`，等扑克契约类型出现后才运行包级验证。

公共声明见 `catalog.go`、`config_loader.go`、`factory.go`、`mock.go`；与 §4.1 的名称、字段、参数和返回值保持一致。HTTP DTO 的 error_code 不添加到冻结 ModelDescriptor，由 service/server 映射。

## 可复现命令与完整原始输出

从项目根目录执行：

```sh
AGENT_CATALOG_PATH="/Users/gaosper/.config/opencode/opencode.jsonc" go test -mod=readonly -v ./internal/agent
AGENT_CATALOG_PATH="/Users/gaosper/.config/opencode/opencode.jsonc" go test -mod=readonly -v -race ./internal/agent
```

这里只传路径，不传任何凭据。`AGENT_CATALOG_PATH` 仅控制只读目录测试；未设置时该测试跳过，其余测试仍执行。所有模型请求测试仅连接本地 httptest，目录测试不调用 Decide。

完整 stdout/stderr 原始输出：

- [首次构建 RED](verification/red-build.log)：`go test -mod=readonly -v ./internal/agent`，尚缺 NewFactory/NewMockDecider；0 个测试运行，1 个包构建失败。这不是行为断言 RED。
- [后续行为 RED](verification/red-header.log)：同一命令，规范化同名 header 未拒绝；顶层 12 PASS / 1 FAIL / 1 SKIP，子测试 26 PASS / 1 FAIL。对应 TestConfigLoaderFailures/header-collision。随后实现固定 CONFIG_HEADER_INVALID。
- [最终 GREEN 完整输出](verification/green.log)：顶层 **14 PASS / 0 FAIL / 0 SKIP**；子测试 **27 PASS / 0 FAIL**；共 41 条 PASS 记录，1 个包 PASS。实际输出结尾 `ok texas-holdem/internal/agent 3.427s`。
- [最终 race 完整输出](verification/race.log)：顶层 **14 PASS / 0 FAIL / 0 SKIP**；子测试 **27 PASS / 0 FAIL**；共 41 条 PASS 记录，1 个包 PASS，无 DATA RACE。实际输出结尾 `ok texas-holdem/internal/agent 4.459s`。

GREEN/race 日志由命令直接 tee 捕获，未截断。RED 日志保留当时工具回传完整原始输出，行号对应当时文件版本。未运行全项目、服务、存储、Web 或实际 provider 验证，不扩大本 lane 验证范围。

## 场景 → 测试函数 → 结果

测试源内容全部位于 [agent_test.go](agent_test.go)，只使用冻结扑克类型，不调用扑克实现函数。

| 场景 | 测试函数 | 普通 / race 结果与断言 |
|---|---|---|
| loader 凭据安全 | TestConfigLoaderRedaction/literal、env、file | PASS / PASS；三来源仅在进程内解析；Catalog/Factory/Decider/目录的 JSON 与 `%v/%+v/%#v` 不含凭据、来源引用或端点；配置语法错误固定 CONFIG_INVALID |
| JSONC 与合并 | TestJSONCLexerAndMerge | PASS / PASS；转义引号、字符串中的注释标记、行/块注释、尾逗号；JSONC 优先、逐 provider/模型合并；畸形与尾随 JSON 拒绝 |
| loader 失败边界 | TestConfigLoaderFailures（8 子测试） | PASS / PASS；重复键、非法模型名、含认证 URL、换行凭据、非法 env 引用、文件缺失、保留 header、规范化 header 重名均固定安全码；未知包 unsupported，不凭 URL 推断 |
| 模型目录包含 mock | TestModelListAndMock | PASS / PASS；零配置包含 mock，切片无别名，未知 ref 拒绝；mock check/call，usage nil，零网络 |
| 本机真实配置模型清单 | TestLocalCatalogModels | PASS / PASS；只读显式配置路径，输出 25 个安全 ref；不调用真实模型 |
| OpenAI-compatible 协议 | TestProviderAdapters/openai | PASS / PASS；POST /v1/chat/completions、Bearer 测试认证、非流式、256 tokens、usage、首次实际本地调用更新 available |
| Anthropic 协议 | TestProviderAdapters/anthropic | PASS / PASS；POST /v1/messages、x-api-key 测试认证、固定 version、非流式、256 tokens、usage、首次实际本地调用更新 available |
| 无历史与 Observation 隐私 | TestObservationPrivacy + TestProviderAdapters | PASS / PASS；Observation 只有 State/Self/Hole/Legal；每协议连续两次请求均单消息，content 完整等于静态指令 + 当前 Observation JSON，无多余动态字段或历史 |
| 严格 action JSON | TestStrictActionJSON | PASS / PASS；合法样例接受；未知/重复字段、尾随内容、null、大小写变体、非整数、非法 kind/to、markdown 拒绝 |
| 动作边界 | TestLegalActionBoundaries | PASS / PASS；完整加注上下界、唯一短全押目标、非全押短加注拒绝、无动作权拒绝；service 仍须交引擎验证 |
| 超时降级 | TestDecisionFallback/timeout/check=true、false | PASS / PASS；短 deadline 本地实际请求，无重试，check/fold，unavailable |
| 畸形动作降级 | TestDecisionFallback/malformed/check=true、false | PASS / PASS；check/fold，无重试；协议成功但 action 非法，availability 仍 available |
| 非法动作降级 | TestDecisionFallback/illegal/check=true、false | PASS / PASS；超出合法 raise_to，check/fold，无重试，仍 available |
| 认证失败隐私 | TestDecisionFallback/auth/check=true、false | PASS / PASS；401，check/fold，不返回模型原文，unavailable |
| body 限额 | TestDecisionFallback/oversize/check=true、false | PASS / PASS；响应 >1 MiB 拒绝，check/fold，unavailable |
| 错误模型 body 隐私 | TestDecisionFallback/protocol/check=true、false | PASS / PASS；原文未进入返回错误，check/fold，unavailable |
| 两协议响应边界 | TestProviderResponseErrors/openai、anthropic | PASS / PASS；usage 缺失 nil，负 usage、尾随 JSON、provider error、null、空/错误内容结构拒绝且仅固定安全码 |
| 重定向凭据保护 | TestHTTPPrivacyRedirects | PASS / PASS；不跟随 307，目标服务零请求，固定 MODEL_HTTP_ERROR |
| 取消与默认限时 | TestCancellationAndLimits | PASS / PASS；默认 timeout 15 秒；预取消 context 零请求，不误改可用性 |
| 目录/可用性并发 | TestAvailabilityConcurrent | PASS / PASS；12 并发独立 decider 和 Models 读取，无 race |
| 真实 provider smoke | live_provider_smoke | **未验证**；未授权付费调用；httptest 不能代替实际 provider 验证 |

私牌隔离证据只覆盖 agent 接受/序列化 Observation 的边界；实际扑克 Observe 的私牌隔离由 poker lane 验证，不宣称本 lane 测试验证了引擎内部。

## 安全结构核验记录

由本地程序读取 opencode.json/jsonc，仅输出已知字段名、npm 白名单值、凭据来源类别和模型名称；未 raw read/grep/cat 配置，未输出任何凭据。当前结构是 `provider → npm/options/models`，options 已见 `apiKey/baseURL`，模型对象已见 `name/cost/limit`。JSON 包标识均为 `@ai-sdk/openai-compatible`；JSONC 包标识为该包或 `@ai-sdk/openai`。当前未配置 Anthropic provider，Anthropic 仅完成本地协议验证。

合并与字段规则详见 [README.md](README.md)。配置有效仅代表 configured，不表示实际 provider 连通、支持扑克 JSON、支持文本输入/该 token 参数或模型存在。尤其配置中的 sora 系列也仅列目录，尚未验证可用于 chat completions。

## 实际安全模型 ref 清单

共 **25 项 = mock 1 + 实际配置 24**。除 mock 外，当前目录状态均为 configured，**无实际调用、无 available 声明**。

```text
mock
deepseek/deepseek-flash
deepseek/deepseek-v4-flash
deepseek/deepseek-v4-pro
mybailian/glm-5.2
mybailian/kimi/kimi-k3
mybailian/qwen3.7-max
mybailian/qwen3.7-plus
mybailian/qwen3.8-flash
mybailian/qwen3.8-max
mybailian/qwq-plus
openai/gpt-4o
openaiyt/gpt-4o
openaiyt/gpt-5-pro
openaiyt/gpt-5.6-luna
openaiyt/gpt-5.6-sol
openaiyt/gpt-6-astra
openaiyt/gpt-6-luna
openaiyt/gpt-6-sol
openaiyt/sora-2
openaiyt/sora-2-pro
qwen/qwen-plus
ytmodel/deepseek-flash
ytmodel/deepseek-v4-flash
ytmodel/deepseek-v4-pro
```

## 未验证项与接线提醒

- 实际 OpenAI-compatible 与 Anthropic provider 均未验证；真实网络调用须用户后续显式授权，不能以配置存在或本地 httptest 代替。
- service 必须在调用 Decide 之前执行启动授权、单局 live 授权与调用额度计数；暂停/停止需丢弃取消后的结果，不应用返回的 fallback。
- ModelDescriptor 没有冻结 error_code 字段；HTTP DTO 必须自行映射安全码/null，不将 Catalog、内部 config 或错误原文用于 HTTP 输出。
- 不验证数据库、宿主、服务真实链路或 Web；不声称整个项目完成。
