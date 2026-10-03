# 扑克规则引擎验证记录

执行目录：`/Users/gaosper/Documents/mycode/texas-holdem`。

写范围只有 `internal/poker/**`。没有修改模块文件、契约文档或其他 lane；没有 commit/push，没有读取凭据。实现只依赖 Go 标准库。公共类型、字段和函数签名按 `docs/contracts.md` §3.1 实现，API 偏差：无。

## 测试文件与测试内容

测试完整源代码：`internal/poker/rules_test.go`。下表所有函数均在该文件中，普通测试和 race 测试的结果均为 PASS。

| 场景 | 函数 | 断言内容 |
|---|---|---|
| 2人和9人位置 | TestBlindAndActionOrder | HU、多人的盲注及翻前/翻后行动顺序；HU逐轮发牌；稀疏座位和零筹码座位 |
| 最小加注 | TestMinimumRaise | 翻前20下界、非法19、完整加注至35后下界60；翻后开池BB下界及增量更新 |
| 短全押重开 | TestShortAllInReopening | 单次短全押不重开、两次累计达到阈值重开、欠注者重新响应、未行动者保留权利 |
| 无响应者不可加注 | TestNoRaiseWithoutResponder | 全押对手不允许加注、跟注截断、无需决策自动完成 |
| 盲注不足 | TestBlindShortStack | SB/BB不足分别全押，配置BB仍为基准，无响应者不加注，未匹配金额退款，双盲全押自动跑牌 |
| 边池和退款 | TestSidePotsAndRefunds | 120/60分层、弃牌资金保留、40退款；真实Apply链形成90/60池和40退款 |
| 平分余数 | TestSplitPotOddChip | 三个池独立分配，button后获奖者优先取余数，座位回绕 |
| 七选五牌力 | TestHandRanking | 九种牌型排序、wheel与六高顺比较、同牌型踢脚、公共牌平局 |
| 自动跑牌和全弃结算 | TestRunoutAndUncontested | 全押跑满五张、全弃立即结束且不跑牌、退款与获奖分离、终局无行动、三街烧牌 |
| 守恒 | TestChipConservationRandomSequences | seed=20261003，200手2–9人，每步Stack+Committed、Pot与StreetBet子集、终局余额清零；Apply输入不变；每五步验证快照恢复的Observation一致 |
| 私牌隔离 | TestObservationPrivacy | 仅actor可Observe、只有自身底牌、无其他私牌/牌堆/历史揭示字段、State全私有、终局不可Observe |
| 输出隔离 | TestOutputIsolation | 输入配置、Players/Board/Actor、Observation、ShortAllInTo、结果各层slice/map均不共享 |
| 并发纯值分支 | TestConcurrentBranchIsolation | 16个并发分支从相同输入运行至结算，修改各分支返回值，原始快照不变 |
| 初始化边界 | TestInitializationValidation | 座位数/唯一/范围、至少两人有筹码、button资格、负数/总额上限/溢出、盲注、52张严格合法且唯一的牌 |
| 动作合法性 | TestActionValidation | 非actor、非法kind、无欠注call、非法check、非raise的To、负数及超额目标被拒绝 |
| 快照版本与恢复 | TestSnapshotValidationAndResume | 行动点和终局恢复后相同结果，版本/未知字段/非法行动/重复键/53张牌/缺日志/null/尾部数据拒绝，零值State不可编码 |

## 计数与边界

- 首次RED：编译失败，测试函数未运行；0个函数PASS、0个函数FAIL，包级FAIL 1。
- 第一次实现后：顶层14 PASS/1 FAIL，子测试15 PASS/1 FAIL（合计29 PASS/2 FAIL）。HU发牌期望写成了错误的第7张牌，按实际逐轮发牌位置修正为2d/2s，没有更改发牌规则。
- 快照扩展RED一：顶层14 PASS/1 FAIL，子测试16 PASS/0 FAIL（合计30 PASS/1 FAIL）。
- 快照扩展RED二：相同计数。发现测试复用JSON map残留字段，先修正测试隔离；随后暴露Go数组JSON解码截断53张牌的问题。
- 最终普通测试：顶层16 PASS/0 FAIL，子测试16 PASS/0 FAIL（合计32 PASS/0 FAIL），包级PASS。
- 最终race测试：相同计数，包级PASS；未出现数据竞争报告。

风险与未验证项：

1. 快照版本1存储初始配置、完整牌堆和行动日志，并重放合法行动恢复所有派生字段；包含私牌，未加密，存储层须限制访问，禁止用于HTTP或模型输入。
2. 行动日志随行动增长，纯值复制与快照重放有累计成本；超长日志性能、恶意大快照资源耗尽没有基准验证。
3. 下一手moving-button的推进属于调用者（冻结API只接受显式Button）；本包验证给定button的HU/多人位置，不宣称service跨手推进通过。
4. 边池和平分余数包含独立内部结算夹具，同时边池另有真实Apply行动链；不将夹具误称为真实服务端到端测试。
5. 其他lane的agent/service/HTTP、MySQL/Redis、Web与实际provider均未验证，没有创建stub替代依赖；不把引擎单元测试当作真实服务链路。

## 完整原始输出

以下保留每次执行的完整测试输出，不省略失败信息。计数是上文依据测试行计算的摘要，原始输出不做改写。

### 1. 首次RED

命令：`go test -mod=readonly -v ./internal/poker`

```text
# texas-holdem/internal/poker [texas-holdem/internal/poker.test]
internal/poker/rules_test.go:12:35: undefined: Chips
internal/poker/rules_test.go:12:42: undefined: State
internal/poker/rules_test.go:19:26: undefined: State
internal/poker/rules_test.go:19:38: undefined: ActionKind
internal/poker/rules_test.go:19:53: undefined: Chips
internal/poker/rules_test.go:19:60: undefined: State
internal/poker/rules_test.go:24:36: undefined: State
internal/poker/rules_test.go:24:43: undefined: State
internal/poker/rules_test.go:94:45: undefined: Chips
internal/poker/rules_test.go:94:80: undefined: Seat
internal/poker/rules_test.go:94:86: undefined: State
internal/poker/rules_test.go:94:86: too many errors
FAIL	texas-holdem/internal/poker [build failed]
FAIL
```

### 2. 第一次实现后的测试

命令：`gofmt -w internal/poker/types.go internal/poker/rules.go internal/poker/showdown.go internal/poker/snapshot.go internal/poker/rules_test.go && go test -mod=readonly -v ./internal/poker`

```text
=== RUN   TestBlindAndActionOrder
=== RUN   TestBlindAndActionOrder/heads_up
    rules_test.go:74: HU逐轮发牌顺序 [2d 2s]
=== RUN   TestBlindAndActionOrder/nine_seats
=== RUN   TestBlindAndActionOrder/sparse_and_out_seats
--- FAIL: TestBlindAndActionOrder (0.00s)
    --- FAIL: TestBlindAndActionOrder/heads_up (0.00s)
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
--- PASS: TestChipConservationRandomSequences (0.01s)
=== RUN   TestObservationPrivacy
--- PASS: TestObservationPrivacy (0.00s)
=== RUN   TestOutputIsolation
--- PASS: TestOutputIsolation (0.00s)
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
FAIL
FAIL	texas-holdem/internal/poker	0.166s
FAIL
```

### 3. 快照扩展RED一

命令：`gofmt -w internal/poker/rules_test.go && go test -mod=readonly -v ./internal/poker`

```text
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
--- PASS: TestChipConservationRandomSequences (0.01s)
=== RUN   TestObservationPrivacy
--- PASS: TestObservationPrivacy (0.00s)
=== RUN   TestOutputIsolation
--- PASS: TestOutputIsolation (0.00s)
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
    rules_test.go:689: 接受重复版本键
--- FAIL: TestSnapshotValidationAndResume (0.00s)
FAIL
FAIL	texas-holdem/internal/poker	0.164s
FAIL
```

### 4. 快照扩展RED二

命令：`gofmt -w internal/poker/rules_test.go && go test -mod=readonly -v ./internal/poker`

```text
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
--- PASS: TestChipConservationRandomSequences (0.01s)
=== RUN   TestObservationPrivacy
--- PASS: TestObservationPrivacy (0.00s)
=== RUN   TestOutputIsolation
--- PASS: TestOutputIsolation (0.00s)
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
    rules_test.go:676: 接受53张牌快照
--- FAIL: TestSnapshotValidationAndResume (0.00s)
FAIL
FAIL	texas-holdem/internal/poker	0.167s
FAIL
```

### 5. 最终GREEN

实际命令：`gofmt -w internal/poker/types.go internal/poker/rules.go internal/poker/showdown.go internal/poker/snapshot.go internal/poker/rules_test.go && go test -mod=readonly -v ./internal/poker`

独立复现测试命令：`go test -mod=readonly -v ./internal/poker`

```text
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
--- PASS: TestChipConservationRandomSequences (0.03s)
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
ok  	texas-holdem/internal/poker	0.177s
```

### 6. 最终race

命令：`go test -mod=readonly -race -v ./internal/poker`

```text
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
--- PASS: TestChipConservationRandomSequences (0.26s)
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
ok  	texas-holdem/internal/poker	1.429s
```
