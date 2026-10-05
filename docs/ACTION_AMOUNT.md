# 动作记录「实际投入金额」字段（Amount）

## 背景

动作记录原先只有 `To`（本街累计投入目标值），前端无法直接显示「跟注 100」这类完整信息。
本次为 `ActionView` / `ActionDTO` 各新增 `Amount poker.Chips`（JSON 名 `amount`），
语义为：**该动作本次实际投入额**。

## 契约（已冻结）

| 动作 | `Amount` | `To` |
| --- | --- | --- |
| fold | 0 | 0 |
| check | 0 | 0 |
| call | 本次补差跟注额（`Legal.CallAmount`） | 0 |
| raise_to | 本次追加额（`action.To − 动作前该座位本街 StreetBet`） | 本街累计投入目标值 |

既有 `To` 语义不变；`poker` 引擎 `Apply`/`Legal` 语义未改动。

## 改动文件

| 文件 | 改动 |
| --- | --- |
| `internal/service/contracts.go` | `ActionView` 新增 `Amount poker.Chips` 字段 |
| `internal/service/worker.go` | 新增取数函数 `actionAmount`；`submit`（真人路径）与 `commitDecision`（AI 路径）两处 `ActionView` 构造填充 `Amount` |
| `internal/server/dto.go` | `ActionDTO` 新增 `Amount poker.Chips`（`json:"amount"`）；`Game()` 转换透传 `a.Amount` |
| `internal/service/human_test.go` | 新增测试 `TestActionAmount`、`TestHumanActionAmount` |
| `internal/service/manager_test.go` | 新增测试 `TestAIActionAmount` |
| `internal/server/dto_test.go` | 新增测试 `TestActionDTOCarriesAmount` |

## 字段位置与取数逻辑

### 字段

`internal/service/contracts.go`：

```go
type ActionView struct {
	HandNo   uint32
	Sequence uint64
	Seat     poker.Seat
	Kind     poker.ActionKind
	To       poker.Chips
	// Amount 是本次动作的实际投入额：fold/check 为 0，call 为本次补差跟注额，
	// raise_to 为相对动作前本街投入的追加额。To 仍为本街累计投入目标值。
	Amount                    poker.Chips
	Source                    string
	ErrorCode                 *string
	InputTokens, OutputTokens *int64
}
```

`internal/server/dto.go`：

```go
type ActionDTO struct {
	...
	To           poker.Chips      `json:"to"`
	Amount       poker.Chips      `json:"amount"`
	...
}
```

### 取数逻辑（worker.go）

两处构造都在 `poker.Apply` **之后**用「动作前的解码状态 `s`」计算，因此拿到的是动作前
StreetBet / CallAmount，不受 Apply 副作用影响：

```go
func actionAmount(s poker.State, seat poker.Seat, action poker.Action) poker.Chips {
	switch action.Kind {
	case "call":
		if l, err := poker.Legal(s, seat); err == nil {
			return l.CallAmount
		}
		return 0
	case "raise_to":
		for _, p := range poker.Public(s).Players {
			if p.Seat == seat {
				return action.To - p.StreetBet
			}
		}
		return 0
	default:
		return 0
	}
}
```

- `submit`（真人，约 worker.go:240）：

  ```go
  a := ActionView{HandNo: ..., Kind: action.Kind, To: action.To,
  	Amount: actionAmount(s, seat, action), Source: source, ErrorCode: code}
  ```

- `commitDecision`（AI，约 worker.go:355 附近）：

  ```go
  a := ActionView{HandNo: r.View.Hand.Number, ..., Seat: obs.Self, Kind: action.Kind,
  	To: action.To, Amount: actionAmount(s, obs.Self, action), ...}
  ```

`actionAmount` 使用传入的 pre-Apply 状态（`commitDecision` 中的 `s` 即由 `r.Snapshot` 解码、
revision 已校验），未改动引擎 `Apply`/`Legal` 语义；`raise_to` 的「动作前 StreetBet」可靠可取，
无需硬编码。

## 新增测试函数

- `internal/service/human_test.go`：`TestActionAmount`、`TestHumanActionAmount`
- `internal/service/manager_test.go`：`TestAIActionAmount`
- `internal/server/dto_test.go`：`TestActionDTOCarriesAmount`

## 场景 → 测试 → 结果

| 场景 | 测试函数 | 结果 |
| --- | --- | --- |
| call 的 amount = 补差跟注额（真人链路 Act→submit） | `TestHumanActionAmount/call` | PASS |
| raise_to 的 amount = 追加额（真人链路） | `TestHumanActionAmount/raise_to` | PASS |
| fold 的 amount = 0（真人链路） | `TestHumanActionAmount/fold` | PASS |
| check 的 amount = 0（真人链路） | `TestHumanActionAmount/check` | PASS |
| AI 路径 call 的 amount = 补差额（commitDecision） | `TestAIActionAmount/call` | PASS |
| AI 路径 raise_to 的 amount = 追加额（commitDecision） | `TestAIActionAmount/raise_to` | PASS |
| 取数函数四种动作（纯函数） | `TestActionAmount`（fold/check/call/raise_to 子用例） | PASS |
| DTO 序列化含 `amount`，且 raise_to 的 `to`/`amount` 并存 | `TestActionDTOCarriesAmount`（call/raise_to/fold/check 子用例） | PASS |
| 既有测试不回归 | 全量 `go test -mod=readonly -race -count=1 ./...` | PASS |

具体期望值（翻前按钮=1、SB=5、BB=10，AI 加注到 30 后真人 seat0 已投 10）：
call → amount=20、To=0；raise_to 到 50 → amount=40、To=50；fold/check → amount=0。
AI 路径（翻前 actor=seat0 已投小盲 5、currentBet=10）：call → amount=5；raise_to 20 → amount=15。

## RED 证据

先暂存三个源文件改动（字段/函数尚未存在），运行新增测试，编译失败：

命令：

```
git stash push -- internal/server/dto.go internal/service/contracts.go internal/service/worker.go
go test -mod=readonly -run 'TestActionAmount|TestHumanActionAmount|TestAIActionAmount|TestActionDTOCarriesAmount' ./internal/service/... ./internal/server/...
```

原始输出：

```
# texas-holdem/internal/service [texas-holdem/internal/service.test]
internal/service/human_test.go:276:14: undefined: actionAmount
internal/service/human_test.go:330:16: human[0].Amount undefined (type ActionView has no field or method Amount)
internal/service/manager_test.go:240:37: a.Amount undefined (type ActionView has no field or method Amount)
FAIL	texas-holdem/internal/service [build failed]
# texas-holdem/internal/server [texas-holdem/internal/server.test]
internal/server/dto_test.go:76:52: unknown field Amount in struct literal of type service.ActionView
internal/server/dto_test.go:77:61: unknown field Amount in struct literal of type service.ActionView
internal/server/dto_test.go:78:52: unknown field Amount in struct literal of type service.ActionView
internal/server/dto_test.go:79:54: unknown field Amount in struct literal of type service.ActionView
FAIL	texas-holdem/internal/server [build failed]
FAIL
```

## GREEN 证据

### 定向测试

命令：

```
go test -mod=readonly -count=1 -run 'TestActionAmount|TestHumanActionAmount|TestAIActionAmount|TestActionDTOCarriesAmount' ./internal/service/... ./internal/server/...
```

原始输出：

```
ok  	texas-holdem/internal/service	3.064s
ok  	texas-holdem/internal/server	3.131s
```

### 全量 `-race` 回归

命令：

```
go test -mod=readonly -race -count=1 ./...
```

原始输出（退出码 0）：

```
ok  	texas-holdem/cmd/config	1.266s
ok  	texas-holdem/cmd/server	1.483s
ok  	texas-holdem/internal/agent	1.497s
ok  	texas-holdem/internal/conf	1.824s
ok  	texas-holdem/internal/data	1.959s
ok  	texas-holdem/internal/poker	2.438s
ok  	texas-holdem/internal/server	2.616s
ok  	texas-holdem/internal/service	2.870s
?   	texas-holdem/web	[no test files]
```

### `go vet`

命令：

```
go vet ./...
```

原始输出（退出码 0）：无输出。

## 未验证边界

- 未启动真实 HTTP / MySQL / Redis，未做浏览器端到端展示验证；Web 渲染 `amount` 不在本次写范围。
- 未验证 live 直连模型的真实响应（不付费）；AI 路径通过注入 decider 的 mock 链路验证。
- 未改动 `web`、`scripts`、`config`、`docs/contracts.md` 与 `poker` 引擎语义。
- 未 commit / push。
