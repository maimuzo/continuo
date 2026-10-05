# 判断ログ: 表明を読んでStatusを動かす

- 対象: `docs/spec/usecases/particular_case/表明を読んでStatusを動かす.rucm.md`
- 作成日 / 作成モデル: 2026-10-02 / Claude Opus 5.5（issue #110・#57。実装を読んで起こした）
- 参照した根拠資料: `docs/plans/continuo_design.md`（3-25 / 3-26 / 3-29 / 3-83 / 4-1）、`internal/orchestrator/lifecycle.go`、`internal/orchestrator/signal.go`、`internal/orchestrator/comment.go`、`internal/orchestrator/prompt.go`、`internal/tracker/adapter.go`、`internal/config/default.go`

## 判断一覧

| # | 判断対象 | 決定した値 | 合理的決定根拠 | 出典 | 自信 |
| --- | --- | --- | --- | --- | --- |
| 1 | 記述を分けたことと USE CASE NAME | `人間に判断を渡す` から表明の適用を分け、`表明を読んでStatusを動かす` と名づけた | **人間の決定（2026-10-02）: 独立した処理を1本に並べて掛け算になる記述は分ける。**表明の読み方（7通り）と、取り直した Status の行き先（6通り）は独立に組み合わさる。名前は実装がすること（transcript から表明を読み、写像の遷移先を書く）をそのまま書いた | コーディネーターの指示（2026-10-02）、`internal/orchestrator/lifecycle.go` の `handleTurnEnd` | 85% |
| 2 | 代替フローを全部 ABORT にしたこと | 6本とも ABORT | **この記述は Status を書くか書かないかで終わる。**どの終わり方でも、呼び出し元（`人間に判断を渡す` の段2）が issue を取り直して行き先を決める。次の turn を送るかどうかは、この記述では決まらない | `internal/orchestrator/lifecycle.go` の `handleTurnEnd`、`applySignals` | 90% |
| 3 | PRIMARY ACTOR と SECONDARY ACTORS | エージェント。GitHub Projects v2 | 表明を書くのはエージェントである。Status を読み書きする相手はボードである | `docs/plans/continuo_design.md#3-25` | 85% |
| 4 | 段3 と `表明なし` | 表明の行が1行以上あることを確かめる。無ければ促す合図を立てる | **促す1文を足すのは、表明の行が1行も無かったときだけである**（`setMissingSignal(len(signals) == 0)`） | `internal/orchestrator/lifecycle.go` の `handleTurnEnd`、`internal/orchestrator/prompt.go` の `BuildContinuationPrompt` | 95% |
| 5 | 段4 と `知らない表明` | 表明の値が写像にあることを確かめる。無ければ WARN を出して無視し、取り得る値の一覧を返す合図を立てる | 実装は打ち切らない。その行を捨てるだけである。促す合図は立てない（表明の行は在ったので）。**代わりに、取り得る値に無かった値を run に控え、次の継続の指示がその値と一覧を返す**（issue #274。人間の決定 2026-10-04「両方にして」の、次の turn の指示で返す側） | `internal/orchestrator/lifecycle.go` の `handleTurnEnd`、`applySignals`、`lookupSignalTarget`、`internal/statussignal` の `FindInvalid`、`internal/orchestrator/prompt.go` の `BuildContinuationPrompt` | 95% |
| 6 | 段5 と `動かさない表明` | 遷移先が null でないことを確かめる | 既定の写像は `working` を null にしている。いちばん普通の表明で、WARN を出さない別の枝である | `internal/orchestrator/lifecycle.go` の `applySignals`、`internal/config/default.go` | 95% |
| 7 | 段7 と `書いてはいけないStatus` | 取り直した Status が `terminal_states` にも `tracker.direct_chat_state` にも入っていないことを確かめる | **拒否リストは2つである**（`protectedStates`）。エージェントや利用者が先に動かした結果を巻き戻さない。以前は `terminal_states` だけを書いていた | `internal/orchestrator/lifecycle.go` の `protectedStates`、`internal/tracker/adapter.go` の `UpdateStatus` | 95% |
| 8 | 段8 と `既に同じStatus` | 取り直した Status が遷移先と違うことを確かめる。同じなら書き込みを要求せず、控えだけして ABORT | 実装は mutation を送らず、`Wrote` を偽で返す。**記録のコメントは、書き込みが起きたときだけ書く**ので、この道では書かない。`Reached` は真なので「最後に書いた Status」は控える | `internal/tracker/adapter.go` の `UpdateStatus`、`internal/orchestrator/comment.go` の `postStatusMove` | 95% |
| 9 | 段9（IF-ELSE を置かないこと） | 「遷移先の選択肢の書き込みを要求する」の1段 | 実装は表明の値で分岐しない。`blocked` の遷移先は写像の値で、`tracker.failure_state` とは別のキーである | `internal/orchestrator/lifecycle.go` の `applySignals` | 100% |
| 10 | 段10 と `Statusを書けない` | 書き込みを受け付けることを確かめる。失敗すれば WARN を出し、記録を書かない | 実装は書き込みの失敗で run を止めない。取り直しの失敗と、item が見えない場合も同じ状態で終わるので、検証を増やさずに表へ置いた | `internal/orchestrator/lifecycle.go` の `applySignals`、`internal/tracker/adapter.go` の `UpdateStatus` | 90% |
| 11 | 段11 と段12 の順番 | 控えてから、記録をコメントする | `applySignals` は `setLastWrittenState` のあとに `postStatusMove` を呼ぶ | `internal/orchestrator/lifecycle.go` の `applySignals` | 95% |
| 12 | 対象付きの表明（グループ）を書かなかったこと | rucm ブロックに書かない。表に1行置いた | 別の issue を指す行は `FetchIssueByIdentifier` で引き、別の run が担当中なら捨てる。設計 3-26 の独立した話題である | `docs/plans/continuo_design.md#3-26` | 75% |
