# 德州扑克最小项目契约（MVP v1）

## 文档状态与确认边界

本项目骨架已获实施确认。下方完整保留架构设计正文，精确 Go API、REST DTO、规则约束与逐场景验证矩阵供各 lane 对齐；原文中的实现目录和测试命令是后续目标，不代表本次已创建实现文件或执行测试。

凡设计报告中未获用户明确确认的产品细节，均仅作为 **MVP 默认建议** 记录，不能视为新增用户授权。例如轮询、单非终态游戏、游戏参数、付费门槛以及生产静态资源嵌入均按此边界理解。`web/dist` 当前忽略，后续嵌入策略由服务 lane 确定。真实模型调用和配置读取须另行获得授权；本次未读取用户 opencode 配置，也未复制敏感配置。

本次仅创建根 `go.mod`、`.gitignore`、本文档及空目录。依赖固定为 `github.com/go-kratos/kratos/v3 v3.0.0`，Go 版本为 `1.25.5`；不引入内部 kit、Wire、Apollo、protobuf，不运行 tidy，不 commit/push。

## 各 lane 公共接口导航与待确认缺口

| lane | 本文精确标题 | 未来声明位置与边界 |
|---|---|---|
| A：规则引擎 | `3. internal/poker：纯规则引擎契约` → `3.1 精确公共 API`、`3.2 必须冻结的规则` | `internal/poker/**`；仅冻结原文声明，不创建实现文件 |
| B：模型接入 | `4. internal/agent：配置、安全与模型接口` → `4.1 公共 API`、`4.2 Loader 与适配约束`、`4.3 决策与降级` | `internal/agent/**`；依赖 A 的接口 |
| C：应用与存储 | `5. 游戏管理、成本与存储` → `5.1 service 公共契约`、`5.2 生命周期`、`5.3 成本硬门槛`、`5.4 持久化与恢复` | 应用契约唯一声明点 `internal/service/contracts.go`；`cmd/**`、`internal/conf/**`、`internal/data/**` 归 C |
| D：HTTP 与 Web | `6. REST 与 JSON DTO` → `6.1 路径`、`Web 最小界面` | HTTP 契约唯一声明点 `internal/server/dto.go`；与 service 显式转换，不互相引用 |
| E：集成验证 | `7. 最小逐场景验证矩阵` | `tests/e2e/**`；未来 `Makefile`、`README.md` 由 E 创建；矩阵全部待验证 |

**保留原设计缺口，不自行补造接口：** `Manager` 未提供 `/api/v1/games/current` 对应方法；`GameView`、`CompletedHandView`、`HealthView` 只有语义引用，没有完整 Go 字段声明；health 响应和存储 repository API 也未给出精确声明。相关 lane 实现前须进行契约确认，不得擅自修改依赖方接口。配置 loader 的实际字段映射仍待安全结构核验，本次不核验用户配置。

以下为原设计正文（去除工具封装标签，正文保持原样）。

---
以下是可直接分派的 **MVP v1 契约**。本轮仅设计建议，不创建项目、不读取模型凭据、不修改文件；版本与环境信息沿用你提供的核验结果，未重新验证。

## 1. 固定架构与产品边界

采用：

```text
Web → REST → service 游戏调度 → poker 规则引擎
                          ├→ agent 模型决策
                          └→ data MySQL / Redis
```

- Kratos 只负责 HTTP、配置接线、进程生命周期；不用内部 kit、Apollo、OAuth、MCP、Wire、protobuf。
- 本机服务固定监听 `127.0.0.1:8081`，默认不开放跨域。HTTP Host 仅允许
  `127.0.0.1:8081` / `localhost:8081`；Origin 必须与 Host 的 HTTP 8081 来源完全一致。
  旧端口 8080 和相邻端口 8082 不允许作为 Host/Origin。Vite proxy target 与转发
  Origin 使用 `http://127.0.0.1:8081`；MySQL 3307 / Redis 6380 不受此次 HTTP 迁移影响。
- Web：Vite + React + TypeScript，开发走 Vite proxy，生产嵌入 `web/dist`。
- **实时方案选轮询**：运行中每秒 GET 一次，暂停/结束降低频率。MVP 不引入 SSE、订阅广播、重连补偿。
- **一个游戏一个串行 worker**；同一手最多一个模型请求。MVP 仅允许一个非终态游戏，避免重复调度与成本失控。
- 固定盲注、无 ante、无抽水、无补码、无 straddle。筹码为整数虚拟单位。
- 每座有独立模型绑定和独立决策实例；允许多个座位选择同一模型，不共享对话历史。
- 每手少于两名有筹码玩家时结束游戏。
- `max_hands` 必填，正整数；完成指定手数自动结束。
- 默认 `mock`；启动参数授权和单次游戏授权必须同时满足，才能调用付费模型。

**观战原则固定：**
- 手牌进行中：Web 不显示任何底牌。
- 手牌完成后：允许 Web 查看该手所有已发底牌，包括弃牌玩家。
- AI 永远只获得当前手自己的底牌；不传其他玩家底牌，也不传历史手牌揭示记录。
- “公共状态”和“已结束手牌详情”是两个不同 DTO，不能复用内部状态序列化。

---

## 2. 最小目录与分派写范围

```text
cmd/server/main.go

internal/
  poker/
    types.go rules.go showdown.go snapshot.go
    *_test.go
  agent/
    catalog.go config_loader.go decider.go
    mock.go openai.go anthropic.go
    *_test.go
  conf/config.go
  service/
    contracts.go manager.go worker.go repository.go
    *_test.go
  data/
    mysql.go redis.go repository.go
    migrations/001_init.sql
    *_test.go
  server/
    http.go handlers.go dto.go errors.go
    *_test.go

web/
  package.json vite.config.ts tsconfig.json
  index.html embed.go
  src/
    api.ts types.ts App.tsx
    components/
  # dist 为构建产物

tests/e2e/
  # 本机真实服务验证脚本及证据

go.mod go.sum
Makefile README.md
```

| 分派 | 独占写范围 | 依赖 |
|---|---|---|
| A：规则引擎 | `internal/poker/**` | 无 |
| B：模型接入 | `internal/agent/**` | A 的冻结接口 |
| C：应用与存储 | `cmd/**`、`internal/conf/**`、`internal/service/**`、`internal/data/**`、`go.mod`、`go.sum` | A/B 的冻结接口 |
| D：HTTP 与 Web | `internal/server/**`、`web/**` | service 接口与本文 DTO |
| E：集成验证 | `tests/e2e/**`、`Makefile`、`README.md` | A–D |

先建立下述契约文件，再并行实现。**依赖方不得自行改被依赖方接口**；发现不足先提交契约变更，由架构 review 确认。

`service/contracts.go` 与 `server/dto.go` 分别是应用契约、HTTP 契约的唯一声明点；两者通过显式转换连接，不互相引用。

---

## 3. `internal/poker`：纯规则引擎契约

### 3.1 精确公共 API

以下是接口声明，不是实现：

```go
type Chips int64
type Seat uint8
type Card string // "2c"..."Ac"，花色 c/d/h/s；解析时严格校验
type Deck [52]Card

type Street string // preflop/flop/turn/river/complete
type PlayerStatus string // active/folded/all_in/out
type ActionKind string // fold/check/call/raise_to

type Player struct {
    Seat          Seat
    Stack         Chips
    StreetBet     Chips
    Committed     Chips
    Status        PlayerStatus
}

type PlayerInit struct {
    Seat  Seat
    Stack Chips
}

type HandConfig struct {
    Players    []PlayerInit
    Button     Seat
    SmallBlind Chips
    BigBlind   Chips
}

type Action struct {
    Kind ActionKind
    To   Chips // 仅 raise_to 使用：本轮累计投入目标值，不是追加量
}

type LegalActions struct {
    CanFold      bool
    CanCheck     bool
    CanCall      bool
    CallAmount   Chips // 实际支付值，已截断至剩余筹码
    CanRaise     bool  // 是否拥有加注权，包括可能的短全押
    MinRaiseTo   Chips // 完整加注下界
    MaxRaiseTo   Chips // StreetBet + Stack
    ShortAllInTo *Chips // 低于 MinRaiseTo 的唯一合法加注目标
}

type PublicState struct {
    Street     Street
    Button     Seat
    SmallBlind Seat
    BigBlind   Seat
    Actor      *Seat
    Board      []Card
    Pot        Chips
    CurrentBet Chips
    Players    []Player
}

type Observation struct {
    State      PublicState
    Self       Seat
    Hole       [2]Card
    Legal      LegalActions
}

type PotAward struct {
    Amount   Chips
    Eligible []Seat
    Winners  []Seat
    Payouts  map[Seat]Chips
}

type HandResult struct {
    Reason     string // showdown/uncontested
    Pots       []PotAward
    Refunds    map[Seat]Chips
    Payouts    map[Seat]Chips // 总获奖，不包含 Refunds
    HoleCards  map[Seat][2]Card // 仅手牌结束后允许对观战接口提供
    FinalStack map[Seat]Chips
}

type State struct {
    // 全部字段私有：牌堆、底牌、行动记录、重开判断、待行动集合等
}

func StandardDeck() Deck
func NewHand(cfg HandConfig, deck Deck) (State, error)
func Legal(s State, seat Seat) (LegalActions, error)
func Apply(s State, seat Seat, action Action) (State, error)
func Public(s State) PublicState
func Observe(s State, seat Seat) (Observation, error)
// PrivateHole 是真人视角的窄接口：状态有效、座位合法且本手确实发过牌时返回
// 该座两张牌。非 actor、已弃牌、已全押、手牌完成都可读取自己的已发牌；
// 未发牌或非法状态返回错误；绝不返回其他座位牌、牌堆或合法动作。
// 它不改变 Observe/Legal/Apply 的既有行为。
func PrivateHole(s State, seat Seat) ([2]Card, error)
func Result(s State) (HandResult, bool)

func EncodeState(s State) ([]byte, error)
func DecodeState(data []byte) (State, error)
```

约束：

- `Apply` 返回新状态，不能修改输入；所有切片/map 输出必须脱离内部可变存储。
- 仅当前 actor 可行动；结束后无 actor、无合法动作。
- `Observe` 仅允许当前 actor，且只能取得该座底牌。
- 状态快照包含内部牌堆与底牌，必须带格式版本、解码后校验；**绝不能直接返回 HTTP**。
- 引擎不做 IO、随机数、模型请求、数据库访问。service 使用 `crypto/rand` 洗牌，传入完整牌堆。
- 初始化检查：2–9 个座位、座位唯一且在 `0..8`、至少两人有筹码、button 有资格参与、牌堆恰好 52 张唯一合法牌、`0 < SB < BB`。
- 总筹码限制为 `≤ 10^12`，所有数值非负并检查溢出；避免前端整数精度问题。

### 3.2 必须冻结的规则

**位置与盲注**

- 多人：button 后第一名有筹码玩家为 SB，下一名为 BB；翻牌前 BB 后行动，翻牌后 button 后第一名仍可行动玩家开始。
- Heads-up：button 就是 SB；翻牌前 button/SB 先行动，翻牌后 BB 先行动。
- 采用简单的 **moving button**：下一手 button 移至顺时针下一名有筹码玩家；MVP 不实现 dead-button 桌规。
- 发牌从 button 后第一名参赛玩家开始，逐轮各一张；每个公共牌街使用 burn card。
- 盲注不足时支付剩余筹码并全押；翻牌前完整下注基准仍为配置的 BB。

**动作与最小加注**

- `call` 自动支付 `min(应跟金额, Stack)`；不另设 `all_in` 动作。
- 开池下注也用 `raise_to`；无人下注时完整下注下界为 BB。
- 完整加注增量更新 `lastFullRaiseSize`；短全押不更新它。
- 无加注权时，即使筹码很多，也只能跟注或弃牌。
- 低于 `MinRaiseTo` 的加注，仅允许 `To == MaxRaiseTo` 且仍大于 `CurrentBet`。

**短全押重开**

按“每名玩家上次行动时面对的下注额”判断：

- 玩家尚未行动，保留加注权。
- 已行动玩家，之后面对的**累计增加额**达到完整加注阈值，才重开加注权。
- 多次短全押可以累计重开，不能仅看最后一次动作是否完整加注。
- 短全押仍须让欠注玩家重新作出跟注/弃牌选择。
- 不允许向没有任何可响应筹码的对手继续加注。

**结算**

- 未匹配下注先退款，不能形成“只有一名玩家有资格”的假边池。
- 按每人整手投入额分层形成主池、边池；弃牌者参与出资，但不参与获奖。
- 每个池独立评估最佳五张牌，可平分。
- 平分余数从 button 后第一名获奖者起，顺时针每人一单位。
- 只剩一个未弃牌玩家，立即结束，不要求跑完公共牌。
- 无需再决策时自动跑完公共牌并结算。
- 使用标准七选五比较，包含 A2345 顺子与公共牌平局。

**筹码守恒不变量**

- 手中：`sum(Stack) + sum(Committed) == initialTotal`。
- 结束：`sum(FinalStack) == initialTotal`。
- `PublicState.Pot == sum(Committed)`，`StreetBet` 是其中子集，不能再次相加。
- 结束时投入余额清零，历史池金额仅存在 `HandResult`，不得重复计入当前筹码。

---

## 4. `internal/agent`：配置、安全与模型接口

### 4.1 公共 API

```go
type ProviderKind string // mock/openai_compatible/anthropic
type Availability string
// mock/configured/unverified/available/unavailable/unsupported

type ModelDescriptor struct {
    Ref          string // 稳定的本地引用，例如 providerID/modelID
    Label        string
    Provider     ProviderKind
    Availability Availability
}

type Catalog struct {
    // 私有：provider 配置、凭据来源、解析后的模型信息
}

type Decision struct {
    Action       poker.Action
    InputTokens  *int64
    OutputTokens *int64
}

type Decider interface {
    Decide(context.Context, poker.Observation) (Decision, error)
}

type Factory interface {
    Models() []ModelDescriptor
    NewDecider(modelRef string) (Decider, error)
}

func LoadCatalog(path string) (*Catalog, error)
func NewFactory(catalog *Catalog) (Factory, error)
func NewMockDecider() Decider
```

`Decider` 不接受 `State`、`HandResult`、游戏全量事件或数据库句柄。

### 4.2 Loader 与适配约束

- `opencode.json/jsonc` 仅作**只读来源**；路径由服务端配置提供，HTTP 不允许提交任意文件路径。
- 支持 JSONC 注释、尾逗号；不要用正则简单删除注释。
- 凭据仅进程内读取、解析、传给 SDK；不复制进项目配置、MySQL、Redis或模型列表 DTO。
- 凭据相关错误仅输出固定错误码与字段路径，不输出原始解析片段、请求头、响应体或配置对象。
- provider 协议根据实际配置识别，不能因存在 `baseURL` 就认定 OpenAI-compatible。
- 支持的两类先实现：OpenAI-compatible chat completions、Anthropic messages。
- 不支持的 provider 明确标记 `unsupported`，禁止假装兼容。
- provider endpoint 必须来自管理员配置，不接受浏览器修改。

**尚需按实际配置核验的点：** provider 的包标识、模型结构、环境变量引用语法、认证字段及 URL 语义。当前未读取文件，不承诺猜测出的字段映射；实现者应做不含凭据的结构核验。

### 4.3 决策与降级

- 每次请求重新构造无历史消息的 observation；提示中只能出现公共状态、自己的两张牌、合法动作。
- 输出仅接受：

```json
{"kind":"raise_to","to":300}
```

其他动作 `to` 必须为 `0` 或省略。严格解析后交引擎验证，模型声明合法不算合法。

- 模型请求默认 15 秒超时，输出上限默认 256 tokens。
- 超时、协议错误、非法动作：**能 check 则 check，否则 fold**。
- 单次失败不重试，防止成本翻倍。
- mock：能 check 则 check，否则 call；不产生网络请求。
- 可用性状态与游戏动作结果分离：非法动作不等于 provider 不可达。
- 启动不发真实模型探测请求；配置有效只是 `configured/unverified`。首次真实调用后更新 `available/unavailable`。
- HTTP 只返回安全错误码，如 `MODEL_TIMEOUT`、`MODEL_AUTH_FAILED`，不返回原始错误文本。

---

## 5. 游戏管理、成本与存储

### 5.1 service 公共契约

```go
type Mode string // mock/live

type SeatKind string // ai/human；旧记录空值按 ai 解释

type SeatConfig struct {
    Seat     poker.Seat
    Name     string
    Stack    poker.Chips
    ModelRef string
    Kind     SeatKind
}

type CreateGame struct {
    Seats        []SeatConfig
    Button       poker.Seat
    SmallBlind   poker.Chips
    BigBlind     poker.Chips
    MaxHands     uint32
    Mode         Mode
    MaxLiveCalls uint32 // live 必须 > 0；mock 必须为 0
    // PlayerSessionHash 只由可信 HTTP 适配层注入，非 JSON 请求字段；
    // 真人创建必须提供非零摘要，全 AI 创建不得提供。
    PlayerSessionHash *PlayerSessionHash
}

type Command string // start/pause/resume/stop

type Manager interface {
    Models(context.Context) ([]agent.ModelDescriptor, error)
    Create(context.Context, CreateGame) (GameView, error)
    Get(context.Context, string) (GameView, error)
    Control(context.Context, string, Command) (GameView, error)
    GetHand(context.Context, string, uint32) (CompletedHandView, error)
    Health(context.Context) (HealthView, error)
    // GetPlayer/Act 校验会话摘要后返回本人视角；不改变原有公共方法行为。
    GetPlayer(context.Context, string, PlayerSessionHash) (PlayerGameView, error)
    Act(context.Context, string, PlayerSessionHash, HumanAction) (PlayerGameView, error)
}

// 真人契约（增量，详见 docs/contracts.md 后续「真人上桌」章节）：
//   - 总座位 2..9，允许 0 或 1 个真人；真人 model_ref 必须为空、Stack>0。
//   - 真人不创建 Decider、不消费 live 额度；mode 只决定 AI 的 mock/live。
//   - 等待真人为 running + hand.waiting_for="human_action"，不新增生命周期状态。
//   - 超时缺省 60s，一律弃牌（即使能 check），动作记录 source=fallback、
//     error_code=HUMAN_ACTION_TIMEOUT；正常提交 source=human、error_code=null。
//   - 私牌只在请求时从引擎状态投影，绝不进入公共 GameView 或 Redis。
```

`GameView`、`CompletedHandView` 的语义与下面 DTO 一致，但不带 JSON tags；HTTP 层负责转换。所有 `context.Context` 向存储与模型传播，不使用 `gin.Context`。

### 5.2 生命周期

```text
created → running ⇄ paused
   └──────────────→ stopped
running → completed / failed
```

- `pause`：取消当前模型请求，丢弃晚到结果，不执行降级动作，保留当前行动点。
- `resume`：从持久化状态继续；未完成的请求不恢复消息流，而是重新决策。
- `stop`：终态，不再请求模型；保留未结束手牌快照，**不伪造结算**。
- `completed`：达到最多手数，或不足两名有筹码玩家。
- 每个完成手牌后，先持久化结果，再检查上限和下一手资格。
- 控制命令和 worker 结果在同一个串行处理边界执行。
- 每次模型请求附带 `(gameID, revision, requestID)`；暂停、停止或 revision 变化后，晚到结果必须丢弃。
- 相同目标的控制命令幂等；其他非法迁移返回 `409 INVALID_STATE`。

**真人等待与超时（增量）：**轮到真人时仍为 `running`，`hand.waiting_for="human_action"`，
不新增生命周期状态。等待上限取本局私有时长（缺省 60s）；到点一律弃牌（即使能 check）。
`pause` 冻结剩余时长（清空截止时间），`resume` 以剩余时长续期（不重新赠送满额），`stop` 清除等待记录；
暂停/停止后提交动作被拒。`Act`、控制命令、worker 超时与 AI 结果共用现有串行互斥边界，
锁内重读 MySQL 权威记录并检查 revision/actor/等待点/截止时间；`now >= deadline` 时拒绝动作并
经同一链路提交超时弃牌，成功动作与超时动作只能有一个 CAS 提交。服务重启沿用
`running → paused/recovery`，原真人等待转为冻结等待，剩余时间取 `max(0, 原截止时间 - 恢复时服务端时间)`，
为 0 时恢复后立即弃牌，不自动执行动作。

### 5.3 成本硬门槛

必须同时满足：

1. 服务启动配置 `allow_paid_models=true`；
2. 创建游戏显式 `mode=live`；
3. `max_live_calls > 0`，且不超过管理员上限。

- `mode` 不可在运行中切换。
- 发请求**之前**在 MySQL 原子增加 `live_calls_used`。
- 超时、取消、失败都消耗一次调用额度；不宣称取消就不计费。
- 额度不足自动暂停，原因 `cost_limit`；本版不能加预算，用户只能停止或另建游戏。
- token usage 缺失显示 `null`，不能当作 `0`。
- 本版限制的是请求数与输出 tokens，不承诺美元预算准确性。

### 5.4 持久化与恢复

**MySQL 为唯一权威，Redis 仅缓存公共状态。**

最小表：

| 表 | 内容 |
|---|---|
| `games` | 游戏配置、状态、revision、完整引擎快照、手数、调用额度、当前请求标记 |
| `hands` | `(game_id, hand_no)` 唯一；终局结果、底牌揭示、最终筹码 |
| `actions` | 顺序号、手号、座位、实际动作、模型/mock/fallback来源、安全错误码、usage |

- 每次有效状态变化通过一个 MySQL 事务提交：revision CAS、快照、动作、必要的手牌结果。
- 请求开始也提交 revision 与调用额度，防止崩溃后重复预算。
- 不保存原始 prompt、模型原文、凭据或带秘密的错误。
- Redis 键 `poker:game:{id}:public`，内容仅公共 DTO + revision，有 TTL。
- 写顺序：MySQL commit 后再写 Redis。
- Redis 写失败不回滚已提交 MySQL；标记 degraded、使缓存无效，读取回 MySQL，不能返回旧缓存。
- 启动时 MySQL 与 Redis 都必须连接/Ping 成功、数据库 schema 合法；失败明确报错并退出，**不存在 memory fallback**。
- schema 迁移使用显式命令，不在普通服务启动时偷偷改表。
- 运行期 MySQL 失败：停止推进，进入持久化故障状态，拒绝继续调用模型。
- 重启将此前 `running` 游戏转为 `paused/recovery`；不自动恢复付费调用。已计费但无动作的请求按未完成处理，额度不退。
- MVP 单进程；第二个服务实例必须通过 MySQL 实例锁拒绝接管同一调度域。

---

## 6. REST 与 JSON DTO

### 6.1 路径

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/api/v1/health` | MySQL/Redis 可用性、degraded 状态 |
| GET | `/api/v1/models` | 安全模型目录与可用性 |
| POST | `/api/v1/games` | 创建，默认 mock |
| GET | `/api/v1/games/current` | 浏览器刷新后找到非终态游戏；没有则 404 |
| GET | `/api/v1/games/{id}` | 轮询公共状态 |
| POST | `/api/v1/games/{id}/commands` | start/pause/resume/stop |
| GET | `/api/v1/games/{id}/hands/{hand_no}` | 仅已完成手牌详情 |
| GET | `/api/v1/games/{id}/player` | 会话校验后的真人本人视角 |
| POST | `/api/v1/games/{id}/action` | 提交真人动作，返回本人视角 |

`/player` 与 `/action` 只在 loopback 监听下可用：游戏不存在返回 `404`；游戏无真人、会话缺失或
不匹配返回 `403 PLAYER_NOT_AUTHORIZED`；非本机监听下返回 `403 HUMAN_LOCAL_ONLY`。
浏览器仍不得提交底牌、牌堆、座位号或 provider 配置。

**真人身份：**创建真人游戏的响应设置游戏级 `HttpOnly` Cookie `poker_player`（`Path=/api/v1/games/{id}`、
`SameSite=Strict`、不设 `Domain`/`Max-Age`）。HTTP 层生成 32 字节密码学随机凭据，服务端只保存其
SHA-256 摘要并做恒定时间比较；原始凭据绝不进入 JSON、URL、日志、测试证据或模型上下文。
同名重复 Cookie、格式错误一律 `403`。Cookie 丢失后不可重新认领，只能观战或另建游戏。

**真人动作请求：**

```json
{"expected_revision":12,"kind":"raise_to","to":300}
```

`expected_revision` 必填且 >0；`kind` 只接受 `fold/check/call/raise_to`；`raise_to` 必须带正整数 `to`
（本街累计投入目标值）。错误码：`400 ILLEGAL_ACTION`（当前等待点非法动作，不改变状态、不重置时限）、
`409 STALE_ACTION`（revision 不匹配：双击、跨手、超时后旧请求）、`409 INVALID_STATE`（非运行中的
真人等待点）、`409 ACTION_EXPIRED`（已过截止时间，同时经权威链路提交超时弃牌）、
`503 STORAGE_UNAVAILABLE`。成功返回 `200` 及提交后的本人 GameDTO。

创建请求：

```json
{
  "seats": [
    {"seat":0,"name":"AI-1","stack":10000,"model_ref":"provider/model-a"},
    {"seat":1,"name":"AI-2","stack":10000,"model_ref":"provider/model-b"}
  ],
  "button":0,
  "small_blind":50,
  "big_blind":100,
  "max_hands":100,
  "mode":"mock",
  "max_live_calls":0
}
```

`mode` 缺省为 `mock`；其余未声明默认值的字段必填。`model_ref` 仍保存以供观战，但 mock 不创建真实 provider 请求。

控制请求：

```json
{"command":"pause"}
```

公共游戏响应：

```json
{
  "id":"uuid",
  "revision":12,
  "status":"running",
  "reason":null,
  "mode":"mock",
  "max_hands":100,
  "hands_completed":2,
  "live_calls_used":0,
  "max_live_calls":0,
  "storage_degraded":false,
  "seats":[
    {"seat":0,"name":"AI-1","model_ref":"provider/model-a","stack":10200},
    {"seat":1,"name":"AI-2","model_ref":"provider/model-b","stack":9800}
  ],
  "hand":{
    "number":3,
    "street":"preflop",
    "button":0,
    "small_blind":0,
    "big_blind":1,
    "actor":0,
    "board":[],
    "pot":150,
    "current_bet":100,
    "players":[
      {"seat":0,"stack":10150,"street_bet":50,"committed":50,"status":"active"},
      {"seat":1,"stack":9700,"street_bet":100,"committed":100,"status":"active"}
    ]
  },
  "last_action":null
}
```

- `hand` 在尚未开始时为 `null`；终态可保留最后一手。
- `seats[].stack` 表示最近一次已结算余额；手中实时余额以 `hand.players[].stack` 为准，UI 必须区分。
- `last_action` 非空时：

```json
{
  "hand_no":3,
  "sequence":8,
  "seat":0,
  "kind":"call",
  "to":0,
  "source":"fallback",
  "error_code":"MODEL_TIMEOUT",
  "input_tokens":null,
  "output_tokens":null
}
```

已完成手牌响应：

```json
{
  "game_id":"uuid",
  "hand_no":2,
  "reason":"showdown",
  "button":1,
  "board":["Ah","Kd","7c","2s","9h"],
  "hole_cards":[
    {"seat":0,"cards":["Ac","Qs"]},
    {"seat":1,"cards":["Kh","Jd"]}
  ],
  "refunds":[],
  "payouts":[{"seat":0,"amount":400}],
  "pots":[
    {
      "amount":400,
      "eligible":[0,1],
      "winners":[0],
      "payouts":[{"seat":0,"amount":400}]
    }
  ],
  "final_stacks":[
    {"seat":0,"stack":10200},
    {"seat":1,"stack":9800}
  ]
}
```

Go 的 `map[Seat]Chips` 到 HTTP 必须转为上述数组，避免数字键编码差异。

**真人视角 DTO 增量（字段全部保留原文）：**

`GameDTO` 新增 `human_seat`（`*poker.Seat`，公共信息，全 AI 局为 `null`）与 `player`
（本人私有对象，公共响应恒为 `null`）；`HandDTO` 新增 `waiting_for`（运行中等待真人时为
`"human_action"`，否则 `null`）；`SeatDTO` 新增 `kind`（`"ai"`/`"human"`，旧记录空值规范化为 `"ai"`）。

```json
{
  "human_seat":0,
  "player":null,
  "hand":{"waiting_for":"human_action"}
}
```

本人响应中的私有部分：

```json
{
  "player":{
    "seat":0,
    "hand_no":3,
    "hole_cards":["Ac","Qs"],
    "legal_actions":{
      "can_fold":true,"can_check":false,"can_call":true,"call_amount":50,
      "can_raise":true,"min_raise_to":200,"max_raise_to":10000,"short_all_in_to":null
    },
    "action_deadline":"2026-10-03T12:01:00Z",
    "server_time":"2026-10-03T12:00:00Z"
  }
}
```

字段可见性：公共接口 hand 无底牌且 `player=null`；本人尚未开手 `hole_cards=[]` 且各私有字段
`null`；本人等待自己时给出 `legal_actions` 与 `action_deadline`；paused/stopped/complete 只保留
自己的已发牌。`MarshalGame` 与 Redis 继续调用公共转换，永远得到 `player:null`。

统一错误：

```json
{"error":{"code":"INVALID_STATE","message":"当前状态不允许该操作"}}
```

- `400`：输入/未知字段错误。
- `403`：服务未授权 live。
- `404`：不存在。
- `409`：状态冲突、已有非终态游戏、手牌未完成。
- `503`：存储不可用。

所有数组输出 `[]`，不输出 `null`；可空对象/字段按契约输出 `null`。模型目录只包含 `ref/label/provider/availability/error_code`，不含 endpoint、凭据来源或配置原文。

### Web 最小界面

配置页：座位数、独立模型、筹码、盲注、最多手数、mock/live 与 live 额度确认。

观战页：位置/筹码/投入/公共牌/当前行动座位、最近动作、手数、模型状态、存储状态，以及启动/暂停/恢复/停止。完成手牌详情单独打开。

轮询响应仅接受不小于当前 `revision` 的版本；发生 `503` 显示“状态未知/存储故障”，不把旧画面当运行成功。

### 配置与端口来源（配置说明，不改变上方 API 契约）

- 端口/地址的唯一默认值定义在 Go `internal/conf`，经冻结 CLI `go run ./cmd/config [-conf PATH]`
  以严格白名单 JSON 暴露：`http_base_url`、`http_address`、`http_host`、`http_port`、
  `frontend_port`。CLI 不依赖 `web/dist`、不需要存储秘密、不输出 secret。
- 解析优先级（后者覆盖前者）：内置默认 → base YAML（`POKER_CONFIG_FILE` 或
  `config/config.yaml`）→ 同目录 `config.local.yaml` → 白名单环境变量。
- YAML schema：`server.http.{host,port,timeout}`、`frontend.dev_port`、
  `data.mysql.dsn_env`、  `data.redis.{addr,db,password_env}`、
  `agents.{catalog_path,request_timeout,max_output_tokens,allow_paid_models,max_live_calls}`、
  `gameplay.human_action_timeout`（缺省 60s；显式 0/负值/null 拒绝；可被环境变量
  `POKER_HUMAN_ACTION_TIMEOUT` 覆盖）。该时长在创建时写入私有记录，重启修改 YAML 不追溯旧局。
- Python/Vite/Playwright 不嵌入 YAML 库，统一读取上述公共 JSON，不复制端口默认值；
  Vite 仅在 dev server 解析（build 不触发 CLI，避免与 dist 构建成环）。
- 旧 `.env.local` 保留 DSN/Redis 密码凭据；其非敏感旧值（RedisAddr/DB/Catalog/paid）
  不默认注入以盖过 YAML。E2E 强制 mock + `allow_paid_models=false`，dev 不被 ambient
  `true` 打开，`dev-live` 仅显式 action 打开。详见 `docs/CONFIG_TOOLING.md`。

---

## 7. 最小逐场景验证矩阵

以下是**待实现验证要求**，不是已通过的测试。测试函数名应与交付报告逐条对齐。

| 场景 | 指定测试/验证 | 必须断言 |
|---|---|---|
| 2人与9人位置 | `TestBlindAndActionOrder` | HU与多人盲注、翻前/翻后首行动 |
| minraise | `TestMinimumRaise` | 开池BB下界、完整加注更新增量 |
| 短全押 | `TestShortAllInReopening` | 单次不重开、累计重开、欠注仍须响应 |
| 禁止无对手加注 | `TestNoRaiseWithoutResponder` | 只剩全押对手时不能额外下注 |
| 边池/退款 | `TestSidePotsAndRefunds` | 不同投入、弃牌资金、未匹配退款 |
| 平分余数 | `TestSplitPotOddChip` | 多池分别按button顺序分余数 |
| 牌力 | `TestHandRanking` | wheel、同牌力踢脚、公共牌平局 |
| 自动结算 | `TestRunoutAndUncontested` | 全押跑完、全员弃至一人立即结束 |
| 守恒 | `TestChipConservationRandomSequences` | 固定随机种子、多手每一步及终局守恒 |
| 私牌隔离 | `TestObservationPrivacy` | opponent底牌/剩余牌堆/历史揭示完全缺失 |
| loader安全 | `TestConfigLoaderRedaction` | JSONC、env引用、测试literal均不进入输出/错误 |
| 协议适配 | `TestProviderAdapters` | 本地HTTP stub核验两类请求/响应、认证不进日志 |
| 降级 | `TestDecisionFallback` | 超时、畸形输出、非法动作分别check/fold |
| 成本门槛 | `TestLiveGateAndBudget` | 默认零网络；双授权；失败计额度；耗尽暂停 |
| 生命周期 | `TestPauseResumeStop` | 取消、晚结果丢弃、恢复同一行动点、停止不可恢复 |
| 原子持久化 | `TestRepositoryCommitAtomicity` | 快照/动作/结算一致，CAS冲突不双提交 |
| 崩溃恢复 | `TestRecoveryPausesRunningGame` | 不自动付费、不退请求额度、不重复结算 |
| Redis故障 | `TestRedisDegradation` | MySQL可读、旧缓存不被使用、状态明确degraded |
| 公共DTO | `TestPublicDTOPrivacy` | 当前手无底牌，未完成详情409，完成详情可揭示 |
| 本机真实链路 | `mock_multihand` | 真MySQL/Redis/Go/Web，连续多手至上限 |
| 存储启动失败 | `storage_fail_fast` | 分别停MySQL/Redis，进程非零退出且无memory替代 |
| 浏览器恢复 | `web_reload_and_controls` | 刷新找到当前局，暂停/恢复/停止与服务一致 |
| 生产静态资源 | `embedded_web` | 构建Go产物后无Vite仍可加载UI |
| 实际provider | `live_provider_smoke` | 仅显式授权后各协议一次调用；否则标未验证 |

基础可复现命令约定：

```text
go test ./internal/poker ./internal/agent ./internal/service ./internal/server
make test-storage
make e2e-mock
make e2e-storage-failure
make e2e-web
make e2e-embedded
make e2e-live
```

Make targets 必须在实现后明确前置条件与执行命令，不允许用 mock 代替 `test-storage` 或真实端到端。真实 provider 验证不自动纳入默认测试。

---

## 8. Review 优先级与责任

### 实施补充：消除跨 lane 接口歧义

- `Manager` 增加 `Current(context.Context) (GameView, error)`，没有非终态游戏时返回不存在错误。
- 模型列表响应固定为 `{"models":[]}`；每项字段为 `ref/label/provider/availability/error_code`，`error_code` 无错误时为 `null`。
- 健康响应固定为 `{"mysql":"available","redis":"available","storage_degraded":false,"allow_paid_models":false,"max_live_calls":1000}`；依赖不可用时对应值为 `unavailable`，并使用 503。
- 前端独占 `web/**`，但 `web/embed.go` 由服务实现 lane 维护。`web/dist` 为前端构建产物，不提交，生产 Go 构建必须先构建前端。
- 服务实现 lane 独占 `go.mod/go.sum`，其他 lane 不运行会修改模块文件的命令；依赖暂缺时报告阻塞，不越界写入。
- 接口未指定的 service 内部 View 和 Repository 类型由服务 lane 依据既定 DTO 和事务语义定义；不得改变现有公共 agent API。扑克引擎只允许**新增**只读窄接口 `PrivateHole`（见 §3.1），`Observe/Legal/Apply/Public/Result` 的行为必须保持不变。

**最高风险：**

1. 短全押累计重开、盲注不足、边池退款、余数顺序。
2. 暂停/停止后的晚到模型结果及恢复时重复请求。
3. 内部快照误用于 HTTP、历史揭示误传给 AI。
4. MySQL已提交但Redis失败后的旧缓存一致性。
5. literal凭据通过解析报错、SDK debug日志泄漏。
6. OpenAI-compatible 不同厂商的协议差异；不能只凭适配器名称宣布真实可用。

**我的责任边界：**冻结并核对引擎、agent、service、REST、Web 五处接口与语义一致；检查写范围冲突、成本与隐私边界、验证矩阵是否逐条有证据。后续必须依据实际代码、原始测试输出和真实链路复审；当前仅是架构契约，不构成实现通过或上线批准。
