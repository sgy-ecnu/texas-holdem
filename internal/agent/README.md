# 模型接入 lane

## 冻结接口与成本边界

公共 API 与 `docs/contracts.md §4.1` 一致。`Factory.Models()` 总是包含 `mock`，并返回独立的安全目录切片。`NewDecider` 不进行网络调用；每次创建一个独立、无历史的决策器。真实请求只由 `Decide` 触发；双授权和调用额度由 service lane 管理，本包不绕过该边界自动探测或重试。

## 只读配置与合并规则

- 显式路径指向 `opencode.json` 或 `opencode.jsonc` 时，仅读取该路径及同目录这两个精确文件名，不扫描备份、父目录、项目配置或全局其他位置。显式指定的文件必须存在，另一个允许缺失。
- 固定顺序为 JSON 后 JSONC，JSONC 优先，与入参选择哪个文件无关。其他文件名仅加载该文件；`.jsonc` 使用 JSONC 词法扫描，其余按严格 JSON。
- 对象递归合并，非对象值后者覆盖。相同 provider 的 `options`、`models` 均逐字段合并；同模型字段 JSONC 覆盖，未提及模型保留。显式 `null` 不表示删除，在需要对象/字符串的位置会被拒绝。
- 只映射 `provider.<id>.npm/package`、`options.baseURL/apiKey/headers`、`models.<model>.id`。`name`、cost、limit 等不参与请求或目录输出。模型输出上限固定默认 256，不沿用 opencode 模型上限。
- `npm/package` 只有精确的 `@ai-sdk/openai`、`@ai-sdk/openai-compatible` 映射为 OpenAI-compatible，`@ai-sdk/anthropic` 映射为 Anthropic；未知包或相互冲突标识为 unsupported。不依据 baseURL 猜协议，不隐式补全内建 provider。
- 配置值支持完整 `{env:NAME}`、`{file:path}` 和 literal。file 路径相对指定配置目录解析，支持绝对路径及 `~/`，只读、最多 64 KiB，首尾空白裁剪。缺失 env 保留模型为 unverified；非法引用/文件读取错误仅返回固定安全码。literal 不写入其他文件、不日志输出。
- 默认端点（仅原生包映射且未配置 baseURL 时）：`@ai-sdk/openai` 为 `https://api.openai.com/v1`、`@ai-sdk/anthropic` 为 `https://api.anthropic.com/v1`。自定义 `@ai-sdk/openai-compatible` 必须提供 baseURL。配置的 baseURL 按 API 根路径追加 `/chat/completions` 或 `/messages`；不允许 URL userinfo、query、fragment。认证通过请求头发送，不跟随 HTTP 重定向。
- `options.headers` 的值支持相同引用规则，但不得覆写认证、Host、Content-Type、Content-Length 等协议头。不将 header 名或值用于错误文本。
- 配置最多 4 MiB；JSONC 使用标准库词法扫描，尊重字符串、转义、两类注释和尾逗号，并拒绝重复键、尾随内容。所有解析错误无原始片段、底层错误、配置路径或凭据。

## 隐私、协议与可用性

模型目录只公开 `Ref/Label/Provider/Availability` 四个冻结字段；Label 使用模型键，绝不复制配置中的任意 name 文本。HTTP `error_code` 由服务 DTO 层映射，不向冻结 `ModelDescriptor` 添加字段。Catalog、Factory、真实 Decider 的格式化与 JSON 编码不暴露私有配置。

请求为单条 user message，其 content 为固定 action 格式指令加当前 `poker.Observation` 的 JSON；动态数据只有 Observation，不包含历史、其他玩家底牌、内部状态或终局揭示。协议要求严格 JSON action，由解码器拒绝未知字段、重复键、大小写变体、null、尾随内容、非法 kind/to；再按当前 Legal 校验，service 仍须通过扑克引擎验证。

请求非流式，默认 15 秒 deadline、最大响应体 1 MiB、输出上限 256 tokens；失败无重试。超时、协议/认证错误或非法动作返回 check（允许时）否则 fold，并附固定安全错误码；usage 缺失保持 nil。上层暂停/停止必须丢弃被取消请求的返回值，不应用降级动作。

初始状态为 mock/configured/unverified/unsupported，加载和构造阶段零网络。首次实际调用后更新 available/unavailable；模型 action 无效但响应协议成功，仍标 available，不把动作合法性当网络连通性。父 context 取消不改变先前可用性。

## 验证范围

`agent_test.go` 仅依赖冻结扑克类型，不依赖扑克实现函数。两协议通过本机 httptest 校验认证、URL、单条 Observation、非流式请求、token 上限、usage 和可用性；没有任何实际 provider 网络调用。真实 provider 测试需要后续明确付费授权，当前未验证。

可复现：`go test -mod=readonly -v ./internal/agent`、`go test -mod=readonly -v -race ./internal/agent`。显式设置 `AGENT_CATALOG_PATH` 可启用 `TestLocalCatalogModels`，只读该路径并输出安全 model ref 清单，不发网络请求。
