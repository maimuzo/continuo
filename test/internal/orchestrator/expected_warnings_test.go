// テストが意図して起こす失敗のログを、1箇所に集めたものである。
//
// **continuo が動いている間のログは、テストの一部である。**
// 実運用で出た欠陥（ボードから消えた issue が1件混ざるだけで、走行中の全 issue の
// 取り直しが巻き添えで落ちる）は、**そのときログに出ていたのに誰も見ていなかった。**
//
// **ここに書いていないテストで WARN / ERROR が1行でも出たら、そのテストは落ちる。**
// 落ちたら、まず「実装の欠陥ではないか」を疑うこと。**テストを緩めて通してはならない。**
//
// **目印は部分一致で照合する。**ログの msg のうち、括弧の前までを書く。
package orchestrator_test

// expectedWarnings は、テスト関数名から「そのテストで出てよい WARN / ERROR の目印」を引く。
//
// **キーはテスト関数名である**（サブテストは親の名前で引く）。
// **値は、その状況を作っているのがテスト自身であることを、読んで確かめられるものにする。**

// abandonChain は、**turn を終わらせないままテストが閉じたときに連鎖して出るログ**である。
//
// **どれが出るかは、テストが終わる時点で run がどの段にいたかで変わる。**
// 着手の途中なら「着手に失敗しました」、turn の待ち受け中なら「run を諦めて…」、
// リトライを使い切っていれば「リトライの回数を使い切りました」が出る。
// **1件ずつ宣言しても収束しない。**同じテストでも、実行のたびに出るものが変わる。
// そこで newFixture が既定で許す。
//
// **その代わり、この連鎖はログでは検証しない。**リトライの暴発は、
// `TestCheckStalls_1回のstallでabandonが2回走らない`（2回走らないこと）と
// `TestResumeBackoff_*`（採り直しの回数）が、**ログではなく回数と Status で守っている。**
//
// **この連鎖を検証したいテストは、ログではなく Status・pane・コメントで確かめること。**
// 「人間へ渡した」ことをログの有無で確かめると、他のテストの巻き添えで壊れる。
var abandonChain = []string{
	"run を諦めてリトライを積みました",
	"リトライの回数を使い切りました",
	// **`internal/orchestrator/dispatch.go` が `label+"に失敗しました…"` で組み立てる。**
	// label は「着手」か「再 dispatch」なので、**目印には label を含めない**。
	// grep で `着手に失敗` を探すと1件も出ないが、実行時には出る。
	"に失敗しました（待って試し直します）",
}

var expectedWarnings = map[string][]string{
	"TestTurn_max_dispatch_turnsに達したらfailure_stateへ落とす":                 {"hook の transcript_path を解決できないので捨てました"},
	"TestDispatch_workspaceのpaneが1つでなければそのissueを失敗にする":                  {"TestDispatch_workspaceのpaneが1つでなければそのissueを失敗にする", "run を諦めてリトライを積みました", "着手に失敗しました"},
	"TestExternalFailure_Statusを書けなければworktreeを作らない":                    {"Status を落とせません", "TestExternalFailure_Statusを書けなければworktreeを作らない", "着手に失敗しました"},
	"TestAbandon_打ち切るときはworkerを止める前にコメントを確かめる":                          {"run を諦めてリトライを積みました"},
	"TestCheckStalls_1回のstallでabandonが2回走らない":                           {"リトライの回数を使い切りました"},
	"TestDispatch_unknownのまま期限を過ぎたら人間へ渡さず試し直す":                          {"着手に失敗しました"},
	"TestDispatch_既に印を持っているissueは二重にdispatchしない":                        {"着手に失敗しました"},
	"TestDispatch_状態ごとの上限はrunning_stateのバケツで数える":                        {"run を諦めてリトライを積みました", "着手に失敗しました"},
	"TestDispatch_起動直後にblockedならescを送ってから失敗にする":                         {"着手に失敗しました"},
	"TestExternalFailure_paneのlabelを書けなければ着手しない":                        {"着手に失敗しました"},
	"TestExternalFailure_paneを引けなければ着手しない":                              {"着手に失敗しました"},
	"TestExternalFailure_turnの終わりにissueが消えていたら手放す":                      {"run を諦めてリトライを積みました"},
	"TestExternalFailure_worktreeを開けなければ着手を諦めて次の巡回に委ねる":                 {"着手に失敗しました"},
	"TestRUCMQuota_P007_枠を見ない設定なら枠明けを待たない":                              {"run を諦めてリトライを積みました"},
	"TestRUCMQuota_P015_枠を読めなければ枠待ちにせず打ち切る":                             {"着手に失敗しました"},
	"TestRUCM_P011_入力を受け付けないまま期限を過ぎたら人間へ渡す":                             {"run を諦めてリトライを積みました"},
	"TestRUCM_P013_paneが使えないまま期限を過ぎたら人間へ渡す":                             {"リトライの回数を使い切りました", "着手に失敗しました"},
	"TestResumeBackoff_再dispatchはUUIDを採り直して1回目の本文を送る":                   {"run を諦めてリトライを積みました"},
	"TestRunViews_再dispatchでもトークンの累計が巻き戻らない":                            {"run を諦めてリトライを積みました"},
	"TestSlots_上限が1なら1件ずつ着手する":                                          {"run を諦めてリトライを積みました"},
	"TestSlots_上限まで着手したらそれ以上着手しない":                                      {"run を諦めてリトライを積みました"},
	"TestSlots_状態ごとの上限は大文字小文字を無視する":                                     {"run を諦めてリトライを積みました"},
	"TestSlots_状態ごとの上限も効く":                                              {"run を諦めてリトライを積みました"},
	"TestSlots_該当しない状態には全体の上限だけを適用する":                                   {"run を諦めてリトライを積みました"},
	"TestDispatch_テンプレートに一覧に無い変数を書いたらそのissueを失敗にする":                     {"プロンプトを組み立てられません"},
	"TestDispatch_アダプタが未信頼と判定した_issue_にもコメントを1回残す":                      {"リポジトリが Claude Code に信頼登録されていません"},
	"TestDispatch_未信頼のリポジトリへのコメントはリポジトリにつき1回だけ":                         {"リポジトリが Claude Code に信頼登録されていません"},
	"TestExternalFailure_Statusの選択肢が食い違ったら着手しない":                        {"Status の選択肢名が設定と一致しません"},
	"TestExternalFailure_transcriptを読めなくてもturnを終えられる":                   {"hook の transcript_path を解決できないので捨てました", "transcript のパスが分からないので表明を読めません"},
	"TestExternalFailure_ボードを読めなくても巡回は止まらない":                            {"候補の取得に失敗しました"},
	"TestHandoff_worktreeを持たない_run_には調べるところを出さない":                       {"リポジトリが Claude Code に信頼登録されていません"},
	"TestOnHook_worktreeの外のcwdを名乗るhookは捨てる":                             {"hook の cwd がその run の worktree の外なので捨てました"},
	"TestOnHook_許可された置き場所の外のtranscript_pathは覚えない":                       {"hook の transcript_path が許可された置き場所の外なので捨てました"},
	"TestOnHook_通常のファイルでないtranscript_pathは覚えない":                         {"hook の transcript_path が通常のファイルではないので捨てました"},
	"TestPreflight_未信頼なら着手せず承認を促すコメントを1件書く":                             {"リポジトリが Claude Code に信頼登録されていません"},
	"TestPreflight_未信頼の通知は巡回のたびに繰り返さない":                                 {"リポジトリが Claude Code に信頼登録されていません"},
	"TestRUCMHandoff_P012_知らない表明ではStatusを動かさない":                         {"表明の値が status_signal_map にありません"},
	"TestReconcile_active_statesに戻ったらdispatchの前にpaneを閉じる":               {"印に入っていない worktree に生きた pane があったので閉じます"},
	"TestRestore_agent_statusがblockedなら引き継がずfailure_stateへ落としてpaneを閉じる": {"権限の確認で止まっているので引き継ぎません"},
	"TestRestore_agent_statusが知らない値ならpaneを閉じてworktreeとStatusを残す":        {"agent_status を判断できないので引き継ぎません"},
	"TestRestore_agentの一覧を取れなくてもpaneを1つも閉じない":                           {"agent の一覧を取れません"},
	"TestRestore_agent名の無いpaneは閉じてworktreeとStatusを残す":                   {"pane に agent 名が無いので、この Claude Code へはもう送れません"},
	"TestRestore_socketのパスが前回と違えば引き継がずpaneを閉じる":                         {"hook を受ける socket のパスが前回と違うので引き継ぎません"},
	"TestRestore_取り直しで見つからないrunはpaneもworktreeも残して印から外す":                 {"取り直しで見つからなかったので何もしません"},
	"TestRestore_取り直しに失敗しても起動を続けpaneを閉じる":                               {"取り直しに失敗したので引き継ぎません", "復元のための取り直しに失敗しました"},
	"TestRestore_同じissueのworktreeが2つあるとき新しいほうを採り古いほうのpaneを段4で閉じる":       {"同じ issue の worktree が2つあるので created_at が新しいほうを採ります", "同じ issue の2つ目の worktree に pane があったので閉じます"},
	"TestRestore_同じworktreeにpaneが2つあるとき1つだけ引き継ぎ残りを閉じる":                  {"同じ worktree に pane が2つあります", "同じ worktree に2つ目の pane があったので閉じます"},
	"TestRestore_壊れた身元ファイルは無視してログに出す":                                   {"身元ファイルを読めない worktree を飛ばしました"},
	"TestRestore_引き継いだ回数が上限ならturnを1回も送らずfailure_stateへ落とす":              {"引き継いだ回数が上限に達したので引き継ぎません"},
	"TestRestore_身元ファイルの無いworktreeのpaneは閉じずにログへ残す":                      {"身元ファイルの無い worktree に pane がありました"},
	"TestSignal_ボードに載っていない対象はコメントに残して捨てる":                               {"表明が指す issue がボードに載っていません"},
	"TestTick_選択肢名が合わなければその巡回のdispatchだけを飛ばす":                           {"Status の選択肢名が設定と一致しません"},
	"TestTurn_turnループを起こせなかったらNeedsPromptを立て直す":                         {"turn ループが既に走っているので、次の巡回で起こし直します"},
}
