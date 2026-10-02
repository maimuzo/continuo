# issue #110: 記述を書き直したあとの、テストと経路の対応表

issue #110（ユースケース記述（RUCM）6本から、テストが1本も生成されていない）の作業記録である。
記述を書き直すと経路の番号が振り直されるので、印（`RUCM-PATH`）の付いたテストがどの経路に当たるかを、テストの中身を読んで決め直す。
書き直した役（subagent）が出した対応表を、ここへ書き留める。**記述をさらに分けると番号は変わるので、貼る前に測り直す。**

## worktree と branch を片付ける（2026-10-02。経路 35本 → 95本の版。このあと2本を分ける予定）

テストは `Manager.Cleanup` を直接呼ぶので、巡回の段1〜7 は通らない。`Cleanup` の中の分岐で当てた。

| テスト | 当たる経路の中身 |
| --- | --- |
| `TestCleanup_未コミットの変更があれば消さない` | 失うものが残っている |
| `TestCleanup_push済みでないcommitが残っていれば消さない` | 失うものが残っている |
| `TestCleanup_baseが分からなければ消さない` | 失うものが残っている（判定できないことも見送る理由） |
| `TestCleanup_push済みなら消してbranchと設定ファイルも消す` | herdr で消える・親の ID 無し・branch を削除 |
| `TestCleanup_deleteBranchが偽ならbranchを残して残ったものに積む` | `cleanup.delete_branch` が偽の枝 |
| `TestCleanup_worktreeを消し切れなければbranchも設定ファイルも消さない` | 消せないworktree |
| `TestCleanup_置き場所の外側は消さずに失敗する` | 置き場所の外 |
| `TestCleanup_無効なら何もしない` | 片付けの無効 |
| `TestShouldCleanup_on_statesに入った時点で片付ける` | Status が `cleanup.on_states` かの判定 |
| `TestCleanup_herdrが別のパスを答えたら何も消さない` | 宛先が定まらない |
| `TestCleanup_実在しないbranchを残ったものとして数えない` | branch が実在しない枝 |
| `TestCleanup_実在しないbranchはdeleteBranchが偽でも残ったものに数えない` | branch が実在しない枝 |
| `TestCleanup_実在するbranchを消せなければ理由を返す` | 検算に落ちて残す |
| `TestCleanup_continuoが開かせた親workspaceを閉じる` | 親を閉じる・branch を削除 |
| `TestCleanup_人間が開いた親workspaceは閉じない` | 親の ID 無し |
| `TestCleanup_同じリポジトリのworktreeが残っていれば親workspaceを閉じない` | 同じリポジトリの worktree が残っている枝 |
| `TestCleanup_親workspaceを閉じる責任を残ったworktreeへ渡す` | 同上（引き継ぎ） |
| `TestCleanup_引き継ぎは既にある親workspaceのIDを上書きしない` | 同上 |
| `TestCleanup_親workspaceのIDが現物と食い違えば閉じない` | 親の ID が現物と食い違う枝 |
| `TestScan_壊れた身元ファイルはエラー付きで返す` | 身元ファイルを読めない |
| `TestScan_置き場所を読めなければ走査が失敗する` | 材料を取れない（走査から） |

印は無いが、経路を通している既存のテスト（役が挙げたもの。本体を読んでいないものを含む）:
`TestCleanup_worktreeのgitが書き換えられていたら別のリポジトリに触らない`（リポジトリの食い違い）、
`TestStatusline_NoWaitの片付けは押さえられたcloneで何も消さずErrCloneBusyを返す`（clone が押さえられている）、
`TestCleanup_worktreeの現物と一致しないbranchは消さない`（検算に落ちて残す）、
`TestCleanup_upstreamが無くbaseと差分があれば消さない`・`TestCleanup_gitが答えられないときは次にすべきことを添える`（失うものが残っている）、
`TestCleanup_壊れたrefのbranchも片付ける`（壊れたref）、`TestCleanup_packedrefsから生き返るbranchを片付けたと答えない`（生き返ったref）。

この記述の番号を引いている箇所: 設計 6-17（題名「11本」、分岐元のステップ番号、「テストパスは35本」）、設計の 6-18e の表の「`壊れたref` と `消さないref`」、
`test/internal/workspace/cleanup_test.go`（「7本のパス」「RUCM のステップ12」「ステップ9」）、`repoworkspace_test.go`（「ステップ11〜20」）、`scan_test.go`。

人間へ報告すること: 巡回の片付けは、見送っても issue へコメントせず、身元ファイルへ時刻も書かない。設計 3-9 の手順2c（「issue へのコメントは1回だけ書く」）と逆である。

## 着手を取り消す（2026-10-02。経路 34本 → 40本）

対応表は [issue110_map_着手を取り消す.json](issue110_map_着手を取り消す.json) に在る（テストを移す道具が読む形）。
役が `go test -overlay` で全テストの出力と終了コードを記録して突き合わせた。直す前から印が実際と合っていなかったものが3本在った
（`herdrに繋げなくてもforceがあれば片付ける` と、pane 待ちで herdr が答えない2本。印は P008 だったが、実際は「残ったものがある」側の P012）。
印の無かった8本（direct chat の検査3本、`--id` の3本、`continuo abandon` の引数の誤りと使い方の表示）にも印を付けた。

この記述の番号を引いている箇所: 設計 6-19（「代替フローを18本」→ 21本）、設計 6-20（「テストパスは34本」→ 40本、「テスト62本」→ 実測64本）、設計 3-37 の段の表（direct chat の検査が無い）。

## 指示書に沿って issue を1件仕上げる / issue を着手から片付けまで見届ける（2026-10-02）

- `指示書に沿って…`: 経路 74本 → 274本になったので、分ける（文面を組み立てて送る / レビューを回す / まとめて直した issue へ成果を書く / 進捗報告を書く）。分けたあとで対応表を作り直す
- `issue を着手から…`: 経路 4本 → 6本。印の付いたテストは無い。経路を通している既存のテスト: `test/e2e/walkthrough_test.go` の `TestE2E_手順書の段1から段9までをmockだけで通す`（P001。基本フロー）
- 人間へ報告すること: direct chat（人間が引き取る）を扱う記述が1本も無い
