# 德州扑克引擎规则矩阵补测与验证报告

- 项目：`texas-holdem`（本地 Go 德州扑克）
- 基线版本：`954e171`（`git status --short` 于开始与结束时均无输出）
- 范围：仅在 `internal/poker/` 补独立测试；未触碰 web、scripts、config、`docs/contracts.md`、`internal/service`、`internal/server`
- 凭据硬规则：未读取 `.env.local` / `config.local.yaml` 的值
- 新增测试文件：`internal/poker/rule_verify_test.go`
- 新增规模：7 个顶层测试函数 + 44 个子测试；现有 19 个顶层测试 / 30 个子测试全部保持通过（无回归）

## 1. 验证命令与结果

```text
go test -mod=readonly -race -count=1 -v ./internal/poker/
go vet ./internal/poker/
```

- `go test`：**通过**。PASS 行 100 条（新增 51 + 原有 49），FAIL 0，`ok texas-holdem/internal/poker 1.426s`
- `go vet`：**通过**，无输出
- 结论：补测后未发现引擎违反德州规则；3 个首轮失败均为**测试期望写错**，已修正测试（见第 3 节）。未修改 `rules.go` / `showdown.go` / `types.go`。

## 2. 新增测试清单（场景 → 测试函数 → 结果）

对应架构师矩阵第 10 节「优先验证顺序」1–7。

### 优先级 1：有人 bet/raise 后，后位与返回的前位 check 必须拒绝

| 场景 | 测试函数 | 结果 |
|---|---|---|
| 翻牌 A bet20，后位 B 本街欠20 → `CanCheck=false`、`CanCall=true`、`CallAmount=20`，且 `Apply(check)` 拒绝 | `TestCheckDeniedAfterBetOrRaise/later_player_cannot_check_after_bet` | PASS |
| **关键回归**：A check → B bet20 → C call20 → 返回 A，A 不能因“之前 check 过”再次 check（仍在翻牌） | `TestCheckDeniedAfterBetOrRaise/returning_checker_cannot_check_after_later_bet` | PASS |
| 已跟20后对手 raise 到50，返回欠30 → check 拒绝 | `TestCheckDeniedAfterBetOrRaise/check_denied_after_full_raise` | PASS |
| 已跟10后短全押到15，返回欠5、无加注权 → check 拒绝，fold 合法 | `TestCheckDeniedAfterBetOrRaise/check_denied_after_short_all_in` | PASS |

> 结论：用户怀疑的「前面下注后面还能 check」不成立；`Legal`/`Apply` 已按欠注拒绝，本轮以回归测试固化。

### 优先级 2：前位 check → 后位完整 bet → 前位可 check-raise；加注后重新响应，不提前换街

| 场景 | 测试函数 | 结果 |
|---|---|---|
| A check → B bet20 → C call20 → A `CanRaise=true`、`MinRaiseTo=40`、`CallAmount=20`；`raise_to 39` 拒绝；A raise 到40 后 B 必须重新响应、仍在翻牌、B 可再 raise 至少到60 | `TestCheckRaiseAndReopen` | PASS |

### 优先级 3：短全押后 call/fold 与加注权分开，含逐玩家累计重开

| 场景 | 测试函数 | 结果 |
|---|---|---|
| 5人 `[200,18,20,200,15]` 顺序 3→4→0→1→2：3 call10、4 all-in15、0 call15、1 all-in18、2 all-in20 → 3 累计重开（`MinRaiseTo=30`、`CallAmount=10`）；3 call20 后 0 只欠5且不重开、raise 拒绝 | `TestShortAllInRightsAndCumulativeReopen/per_player_cumulative_reopen` | PASS |
| 4人累计增9（到19）不重开；累计增10（恰好20）才重开、`MinRaiseTo=30` | `TestShortAllInRightsAndCumulativeReopen/cumulative_9_no_reopen_10_reopen` | PASS |
| 单次短全押不重开：无加注权，call/fold 权保留，raise 拒绝 | `TestShortAllInRightsAndCumulativeReopen/single_short_all_in_call_or_fold_only` | PASS |

### 优先级 4：`raise_to` 是累计目标非追加额；完整/短全押上下界

| 场景 | 测试函数 | 结果 |
|---|---|---|
| 座0 `raise_to 35` → `StreetBet=35/Stack=65/Committed=35`；后位 `MinRaiseTo=60/MaxRaiseTo=100`；`raise_to 59` 拒绝、`raise_to 60` 接受且只追加50 | `TestRaiseToIsCumulativeTarget/raise_to_is_target_not_increment` | PASS |
| 座0 本街已投35、回到它时最大目标100，`raise_to 100` 只追加65 并全押 | `TestRaiseToIsCumulativeTarget/all_in_target_only_adds_difference` | PASS |
| 翻牌无下注，短筹码7 可 check 或短全押到7；满筹码 `raise_to 7` 拒绝 | `TestRaiseToIsCumulativeTarget/short_open_all_in_below_bb` | PASS |
| 面对 CurrentBet10、min20、max15：`raise_to 14` 拒绝、`raise_to 15` 接受并 all-in | `TestRaiseToIsCumulativeTarget/short_all_in_bounds_14_rejected_15_accepted` | PASS |
| `raise_to 59` 拒绝、`raise_to 60` 接受（lastFullRaise=25） | `TestRaiseToIsCumulativeTarget/full_raise_bound_59_rejected_60_accepted` | PASS |
| 目标101 拒绝；目标100 接受并全押 | `TestRaiseToIsCumulativeTarget/all_in_at_max_target_and_above_rejected` | PASS |
| 短SB全押后 BB 仍有 check option | `TestRaiseToIsCumulativeTarget/short_sb_big_blind_option` | PASS |
| 短BB全押不降低跟注基准（10）与完整加注下界（20） | `TestRaiseToIsCumulativeTarget/short_bb_call_baseline_min_raise_20` | PASS |

### 优先级 5：主池、边池由不同牌力玩家获胜

| 场景 | 测试函数 | 结果 |
|---|---|---|
| board=`2c 3d 7h 9s Qc`，投入 `[30,60,100,30]`，座3弃牌；座0一对A赢主池120、座1一对K赢边池60、座2顶层40退款 | `TestMainAndSidePotsDistinctWinners/main_pot_and_side_pot_distinct_winners` | PASS |
| 弃牌座持有最强牌 `As Ad` → 不进 eligible/winners、赔注0 | `TestMainAndSidePotsDistinctWinners/folded_strongest_hand_not_eligible` | PASS |
| 投入 `[100,70,40,10]` 均未弃牌（board 皇家平局）→ 各层金额 40/90/60、eligible 4/3/2 人、座0退款30 | `TestMainAndSidePotsDistinctWinners/multi_level_amounts_and_eligibility` | PASS |

### 优先级 6：同级牌型与踢脚边界（`rankFive` 直接构造五张）

| 场景 | 测试函数 | 结果 |
|---|---|---|
| 高牌第5张、一对点数、两对高对/第5张、三条点数/踢脚、顺子六高胜wheel、同花第5张、葫芦三条优先/对子次之、四条点数/踢脚、同花顺六高胜wheel、皇家胜九高同花顺（共14例） | `TestRankFiveSameCategoryBoundaries/better_hand_wins/*` | PASS（14/14） |
| 同点数高牌、同点数同花 → 花色不平局 | `TestRankFiveSameCategoryBoundaries/suits_do_not_break_ties` | PASS |
| QKA23 不是顺子（category=0）；A2345 是顺子（category=4） | `TestRankFiveSameCategoryBoundaries/non_straight_A_high` | PASS |

### 优先级 7：最佳五张边界（`rankBest` 七张）

| 场景 | 测试函数 | 结果 |
|---|---|---|
| board `9c Tc Jd Qh 2s` + 底牌 `Ks 3d` → 9TJQK，只用1张底牌（不含3d） | `TestBestFiveBoundaries/straight_uses_one_hole_card` | PASS |
| board `2c 7d 9h Js Qc` + 底牌 `Kd Ts` → 9TJQK，用2张底牌 | `TestBestFiveBoundaries/straight_uses_two_hole_cards` | PASS |
| `Ac Ad Ah Kc Kd Kh 2s` → AAAKK（不选 KKKAA） | `TestBestFiveBoundaries/double_trips_pick_high_full_house` | PASS |
| `Ac Ad Kc Kd Qc Qd 2s` → AAKKQ | `TestBestFiveBoundaries/three_pairs_pick_top_two` | PASS |
| `Ac Kc Qc 9c 7c 3c 2d` → AKQ97 同花（丢3c） | `TestBestFiveBoundaries/six_flush_cards_drop_lowest` | PASS |
| `Ac 2d 3h 4s 5c 6d 6h` → 六高顺（不取 wheel，不因重复6漏判） | `TestBestFiveBoundaries/six_high_straight_beats_wheel_with_duplicate` | PASS |
| 同一7张牌排列重排 → 牌力不变 | `TestBestFiveBoundaries/permutation_invariance` | PASS |
| board 皇家同花顺，两名不同底牌玩家 → 0张底牌、平局、最佳五张=公共牌 | `TestBestFiveBoundaries/board_royal_flush_split_zero_hole` | PASS |
| 最佳五张是输入的无重复子集 | `TestBestFiveBoundaries/best_five_is_subset_of_input` | PASS |

## 3. 首轮失败分析与处理（均为测试期望修正，非引擎缺陷）

首轮 `go test -race -count=1` FAIL 3 处；逐一判定为**测试期望写错**：

1. `TestRaiseToIsCumulativeTarget/short_open_all_in_below_bb`
   - 现象：期望短筹码在无人下注时不能 check，实际 `CanCheck=true`。
   - 判定：**测试错误**。无人下注时该玩家欠注为 0，check 合法；短全押只是其另一选项。已改断言为 `CanCheck=true` 且 `ShortAllInTo=7`。
2. `TestShortAllInRightsAndCumulativeReopen/per_player_cumulative_reopen`
   - 现象：座3、座0 匹配后断言立即结算，实际进入翻牌。
   - 判定：**测试错误**。座0、座3 仍有筹码，匹配后应继续下一街，不应立即摊牌。已改为断言换街后再 `finishCalling` 结算。
3. `TestShortAllInRightsAndCumulativeReopen/cumulative_9_no_reopen_10_reopen`
   - 现象：3 人场景「累计增10」仍 `CanRaise=false`。
   - 判定：**测试错误**。该场景两名对手均已全押，无其他可响应者，按 B7 不允许建立无人可争夺的加注；这是正确行为。已把场景改为 4 人，保留一个可响应的玩家后再验证「恰好20重开」。

**未修改任何引擎代码**（`rules.go` / `showdown.go` / `types.go` 保持原样）。

## 4. 未验证边界 / 待确认桌规

以下属于桌规或产品策略，本轮未做（也不应由单手引擎单方面判定）：

- 已 check 的玩家面对不足 BB 的首个短全押是否保留加注权：本引擎按“不足完整下注不重开已行动者”处理（与架构师现金桌口径一致，矩阵第 262 行），本轮未把该口径固化为硬断言，待产品确认。
- 摊牌先后顺序、败者 muck、多人亮牌顺序：当前接口无相应流程。
- 跨手按钮移动（moving/dead button）、补盲：属桌管理，`NewHand` 不负责。
- 总筹码恰好 `1e12` 且发生最大下注/分层/退款的无溢出行为：本轮未构造到该上限的行动链。
- 状态快照/并发隔离的既有测试未改动；真实服务与 UI 链路不在本次范围。

## 5. 完整原始输出

命令：

```text
go vet ./internal/poker/
（无输出，退出码 0）

go test -mod=readonly -race -count=1 -v ./internal/poker/
```

原始输出（PASS 100 / FAIL 0）：

```text
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
=== RUN   TestCheckDeniedAfterBetOrRaise
=== RUN   TestCheckDeniedAfterBetOrRaise/later_player_cannot_check_after_bet
=== RUN   TestCheckDeniedAfterBetOrRaise/returning_checker_cannot_check_after_later_bet
=== RUN   TestCheckDeniedAfterBetOrRaise/check_denied_after_full_raise
=== RUN   TestCheckDeniedAfterBetOrRaise/check_denied_after_short_all_in
--- PASS: TestCheckDeniedAfterBetOrRaise (0.00s)
    --- PASS: TestCheckDeniedAfterBetOrRaise/later_player_cannot_check_after_bet (0.00s)
    --- PASS: TestCheckDeniedAfterBetOrRaise/returning_checker_cannot_check_after_later_bet (0.00s)
    --- PASS: TestCheckDeniedAfterBetOrRaise/check_denied_after_full_raise (0.00s)
    --- PASS: TestCheckDeniedAfterBetOrRaise/check_denied_after_short_all_in (0.00s)
=== RUN   TestCheckRaiseAndReopen
--- PASS: TestCheckRaiseAndReopen (0.00s)
=== RUN   TestShortAllInRightsAndCumulativeReopen
=== RUN   TestShortAllInRightsAndCumulativeReopen/per_player_cumulative_reopen
=== RUN   TestShortAllInRightsAndCumulativeReopen/cumulative_9_no_reopen_10_reopen
=== RUN   TestShortAllInRightsAndCumulativeReopen/single_short_all_in_call_or_fold_only
--- PASS: TestShortAllInRightsAndCumulativeReopen (0.00s)
    --- PASS: TestShortAllInRightsAndCumulativeReopen/per_player_cumulative_reopen (0.00s)
    --- PASS: TestShortAllInRightsAndCumulativeReopen/cumulative_9_no_reopen_10_reopen (0.00s)
    --- PASS: TestShortAllInRightsAndCumulativeReopen/single_short_all_in_call_or_fold_only (0.00s)
=== RUN   TestRaiseToIsCumulativeTarget
=== RUN   TestRaiseToIsCumulativeTarget/raise_to_is_target_not_increment
=== RUN   TestRaiseToIsCumulativeTarget/all_in_target_only_adds_difference
=== RUN   TestRaiseToIsCumulativeTarget/short_open_all_in_below_bb
=== RUN   TestRaiseToIsCumulativeTarget/short_all_in_bounds_14_rejected_15_accepted
=== RUN   TestRaiseToIsCumulativeTarget/full_raise_bound_59_rejected_60_accepted
=== RUN   TestRaiseToIsCumulativeTarget/all_in_at_max_target_and_above_rejected
=== RUN   TestRaiseToIsCumulativeTarget/short_sb_big_blind_option
=== RUN   TestRaiseToIsCumulativeTarget/short_bb_call_baseline_min_raise_20
--- PASS: TestRaiseToIsCumulativeTarget (0.00s)
    --- PASS: TestRaiseToIsCumulativeTarget/raise_to_is_target_not_increment (0.00s)
    --- PASS: TestRaiseToIsCumulativeTarget/all_in_target_only_adds_difference (0.00s)
    --- PASS: TestRaiseToIsCumulativeTarget/short_open_all_in_below_bb (0.00s)
    --- PASS: TestRaiseToIsCumulativeTarget/short_all_in_bounds_14_rejected_15_accepted (0.00s)
    --- PASS: TestRaiseToIsCumulativeTarget/full_raise_bound_59_rejected_60_accepted (0.00s)
    --- PASS: TestRaiseToIsCumulativeTarget/all_in_at_max_target_and_above_rejected (0.00s)
    --- PASS: TestRaiseToIsCumulativeTarget/short_sb_big_blind_option (0.00s)
    --- PASS: TestRaiseToIsCumulativeTarget/short_bb_call_baseline_min_raise_20 (0.00s)
=== RUN   TestMainAndSidePotsDistinctWinners
=== RUN   TestMainAndSidePotsDistinctWinners/main_pot_and_side_pot_distinct_winners
=== RUN   TestMainAndSidePotsDistinctWinners/folded_strongest_hand_not_eligible
=== RUN   TestMainAndSidePotsDistinctWinners/multi_level_amounts_and_eligibility
--- PASS: TestMainAndSidePotsDistinctWinners (0.00s)
    --- PASS: TestMainAndSidePotsDistinctWinners/main_pot_and_side_pot_distinct_winners (0.00s)
    --- PASS: TestMainAndSidePotsDistinctWinners/folded_strongest_hand_not_eligible (0.00s)
    --- PASS: TestMainAndSidePotsDistinctWinners/multi_level_amounts_and_eligibility (0.00s)
=== RUN   TestRankFiveSameCategoryBoundaries
=== RUN   TestRankFiveSameCategoryBoundaries/better_hand_wins
=== RUN   TestRankFiveSameCategoryBoundaries/better_hand_wins/high_card_fifth_kicker
=== RUN   TestRankFiveSameCategoryBoundaries/better_hand_wins/pair_rank_A_over_K
=== RUN   TestRankFiveSameCategoryBoundaries/better_hand_wins/two_pair_high_pair
=== RUN   TestRankFiveSameCategoryBoundaries/better_hand_wins/two_pair_fifth_kicker
=== RUN   TestRankFiveSameCategoryBoundaries/better_hand_wins/trips_rank_over_kicker
=== RUN   TestRankFiveSameCategoryBoundaries/better_hand_wins/trips_kicker_order
=== RUN   TestRankFiveSameCategoryBoundaries/better_hand_wins/straight_six_over_wheel
=== RUN   TestRankFiveSameCategoryBoundaries/better_hand_wins/flush_fifth_card
=== RUN   TestRankFiveSameCategoryBoundaries/better_hand_wins/full_house_trips_first
=== RUN   TestRankFiveSameCategoryBoundaries/better_hand_wins/full_house_pair_second
=== RUN   TestRankFiveSameCategoryBoundaries/better_hand_wins/quads_rank
=== RUN   TestRankFiveSameCategoryBoundaries/better_hand_wins/quads_kicker
=== RUN   TestRankFiveSameCategoryBoundaries/better_hand_wins/straight_flush_six_over_wheel
=== RUN   TestRankFiveSameCategoryBoundaries/better_hand_wins/royal_over_nine_high_sf
=== RUN   TestRankFiveSameCategoryBoundaries/suits_do_not_break_ties
=== RUN   TestRankFiveSameCategoryBoundaries/non_straight_A_high
--- PASS: TestRankFiveSameCategoryBoundaries (0.00s)
    --- PASS: TestRankFiveSameCategoryBoundaries/better_hand_wins (0.00s)
        --- PASS: TestRankFiveSameCategoryBoundaries/better_hand_wins/high_card_fifth_kicker (0.00s)
        --- PASS: TestRankFiveSameCategoryBoundaries/better_hand_wins/pair_rank_A_over_K (0.00s)
        --- PASS: TestRankFiveSameCategoryBoundaries/better_hand_wins/two_pair_high_pair (0.00s)
        --- PASS: TestRankFiveSameCategoryBoundaries/better_hand_wins/two_pair_fifth_kicker (0.00s)
        --- PASS: TestRankFiveSameCategoryBoundaries/better_hand_wins/trips_rank_over_kicker (0.00s)
        --- PASS: TestRankFiveSameCategoryBoundaries/better_hand_wins/trips_kicker_order (0.00s)
        --- PASS: TestRankFiveSameCategoryBoundaries/better_hand_wins/straight_six_over_wheel (0.00s)
        --- PASS: TestRankFiveSameCategoryBoundaries/better_hand_wins/flush_fifth_card (0.00s)
        --- PASS: TestRankFiveSameCategoryBoundaries/better_hand_wins/full_house_trips_first (0.00s)
        --- PASS: TestRankFiveSameCategoryBoundaries/better_hand_wins/full_house_pair_second (0.00s)
        --- PASS: TestRankFiveSameCategoryBoundaries/better_hand_wins/quads_rank (0.00s)
        --- PASS: TestRankFiveSameCategoryBoundaries/better_hand_wins/quads_kicker (0.00s)
        --- PASS: TestRankFiveSameCategoryBoundaries/better_hand_wins/straight_flush_six_over_wheel (0.00s)
        --- PASS: TestRankFiveSameCategoryBoundaries/better_hand_wins/royal_over_nine_high_sf (0.00s)
    --- PASS: TestRankFiveSameCategoryBoundaries/suits_do_not_break_ties (0.00s)
    --- PASS: TestRankFiveSameCategoryBoundaries/non_straight_A_high (0.00s)
=== RUN   TestBestFiveBoundaries
=== RUN   TestBestFiveBoundaries/straight_uses_one_hole_card
=== RUN   TestBestFiveBoundaries/straight_uses_two_hole_cards
=== RUN   TestBestFiveBoundaries/double_trips_pick_high_full_house
=== RUN   TestBestFiveBoundaries/three_pairs_pick_top_two
=== RUN   TestBestFiveBoundaries/six_flush_cards_drop_lowest
=== RUN   TestBestFiveBoundaries/six_high_straight_beats_wheel_with_duplicate
=== RUN   TestBestFiveBoundaries/permutation_invariance
=== RUN   TestBestFiveBoundaries/board_royal_flush_split_zero_hole
=== RUN   TestBestFiveBoundaries/best_five_is_subset_of_input
--- PASS: TestBestFiveBoundaries (0.00s)
    --- PASS: TestBestFiveBoundaries/straight_uses_one_hole_card (0.00s)
    --- PASS: TestBestFiveBoundaries/straight_uses_two_hole_cards (0.00s)
    --- PASS: TestBestFiveBoundaries/double_trips_pick_high_full_house (0.00s)
    --- PASS: TestBestFiveBoundaries/three_pairs_pick_top_two (0.00s)
    --- PASS: TestBestFiveBoundaries/six_flush_cards_drop_lowest (0.00s)
    --- PASS: TestBestFiveBoundaries/six_high_straight_beats_wheel_with_duplicate (0.00s)
    --- PASS: TestBestFiveBoundaries/permutation_invariance (0.00s)
    --- PASS: TestBestFiveBoundaries/board_royal_flush_split_zero_hole (0.00s)
    --- PASS: TestBestFiveBoundaries/best_five_is_subset_of_input (0.00s)
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
ok  	texas-holdem/internal/poker	1.426s
```
