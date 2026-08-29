package orchestrator

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/maimuzo/continuo/internal/hookserver"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/tracker"
)

// hookChanSize は run ごとの hook の受け口の深さである。
//
// **溢れたら捨てて警告を出す。**turn の終わりの判定に使うのは Stop と
// UserPromptSubmit（`<task-notification>`）だけなので、道具の hook が溜まって
// あふれても判定は壊れない。stall の時計は受け口とは無関係に OnHook が直接進めるため、
// 捨てても「生きていることの確認」は失われない（設計 3-21）。
const hookChanSize = 256

// maxTrackedSubagents は「走っている」と覚えておく subagent の件数の上限である。
//
// **hook の中身は外部入力である**（設計 3-2 / 3-23）。`agent_id` を作り変えた
// `SubagentStart` を何万件でも送れるので、上限が無いと1つの run のメモリが際限なく膨らむ。
//
// **上限に達したら、そこから先は覚えない**（`SubagentStop` を待たずに捨てる）。
// **覚えなかったぶんは「走っていない」側に倒れる。**倒す向きはこちらでなければならない。
// 逆に倒すと、作り話の `SubagentStart` を送るだけで引き渡しを永久に足止めできる。
const maxTrackedSubagents = 64

// maxSubagentLabelRunes は通知に載せる subagent の名前1つあたりの長さの上限（文字数）である。
//
// **`agent_id` も `agent_type` も外部入力であり、そのまま issue のコメントへ載る。**
const maxSubagentLabelRunes = 48

// backgroundTaskTypeSubagent は `background_tasks[].type` が subagent であることを表す値である。
//
// **実測で出ている値は `subagent` と `shell` の2つである**
// （docs/evidence/hooks_probe_20260817.jsonl）。
const backgroundTaskTypeSubagent = "subagent"

// runState は run ごとの実行時状態である（設計 3-25 の型定義）。
//
// **プロセスが落ちると消える。**永続化層は作らない（設計 3-4）。
// orchestrator が `map[string]*runState` で持ち、**この map に入っていることが
// 「自分が取った」印であり「実行中」でもある**（設計 3-10 / 3-25。2つの集合を持たない）。
//
// **複数の goroutine が触る。**巡回のループ（stall の判定・照合）と、run ごとの
// turn ループと、hook の配送が同時に触るので、可変のフィールドは mu で守る。
type runState struct {
	// mu は下の可変フィールドを守る。
	mu sync.Mutex

	// ===== 設計 3-25 が定める項目 =====

	// IssueID は project item の ID である（印の map のキーでもある）。
	IssueID string
	// AgentName は herdr の agent 名である。agent.prompt / agent.wait の宛先はこれである。
	AgentName normalize.SafeName
	// PaneID は herdr の pane ID である。pane.close の宛先である。
	PaneID string
	// SessionUUID は Claude Code のセッション UUID である。
	// **起動のたびに新しく採番する**（一度使った UUID は再利用できない。設計 3-3）。
	SessionUUID string
	// PromptID は直前に受けた hook の prompt_id である。
	// **投入時には取れない**ので UserPromptSubmit を受けた時点で入れる（設計 3-25）。
	// 空なら照合しない。
	PromptID string
	// TurnCount は continuo が送ったプロンプトの回数である（設計 3-14）。
	// **Claude Code 自身が投入する `<task-notification>` は数えない。**
	// **枠が明けたときに送る継続の指示は数える**（設計 3-27）。
	TurnCount int
	// RetryCount は stall や起動失敗で積んだリトライの回数である（設計 3-21）。
	RetryCount int
	// BackoffUntil はこの時刻まで再 dispatch しない、という時刻である。ゼロ値なら待たない。
	BackoffUntil time.Time
	// WaitingQuota は枠待ちと判定したことを表す（設計 3-27）。
	// **真の間は stall と turn_timeout の判定を飛ばす。**
	// 外す契機は「枠の resets_at を過ぎたこと」だけである。
	WaitingQuota bool
	// NeedsPrompt は次の turn を送るべき状態であることを表す（設計 3-4 の段5b）。
	// turn ループが拾って agent.prompt を送り、送ったら false へ戻す。
	NeedsPrompt bool
	// StartedAt はこの run が最初の turn を送った時刻である。
	// 「この run が書いたコメント」を前の run のものと区別するのに使う（設計 3-25）。
	StartedAt time.Time
	// LastSeenAt は stall の時計である（設計 3-21）。
	//
	// **これは「最後に hook を受けた時刻」ではない。**hook を1件も受けていなくても、
	// turn を送った時点（beginTurn）・枠待ちを外した時点（clearWaitingQuota）・
	// 画面の版が増えたのを確かめた時点（noteRevision）に現在時刻へ進む。
	// **stall の判定にだけ使う。**「最後に hook を受けた時刻」は LastHookAt が持つ。
	LastSeenAt time.Time
	// LastRevision は最後に見た画面の版である（herdr の pane の revision）。
	//
	// **agent.start / 引き継いだ pane の値を種にし、以後は checkStalls が見るたびに更新する。**
	// 種を入れないと、最初の判定が必ず「版が変わった」になり、打ち切りまでに閾値を2回
	// またぐことになる。
	LastRevision uint64
	// RevisionAt は画面の版が最後に増えたのを確かめた時刻である。
	//
	// **人間へ見せる文面に「画面が最後に変わってからどれだけ経ったか」を書くために持つ。**
	// run を作った時点で現在時刻を入れる（ゼロ値のままだと 1970 年からの経過を表示してしまう）。
	RevisionAt time.Time
	// LastHookAt は最後に hook を実際に受けた時刻である。
	//
	// **進めるのは noteHook だけである。**1件も受けていなければゼロ値のままである。
	// **人間が「エージェントが生きているか」を見る欄はこちらである**（ダッシュボード）。
	// stall の時計（LastSeenAt）と混ぜると、固まったエージェントでも
	// continuo が次の turn を送った瞬間に「0秒前」と表示されてしまう。
	LastHookAt time.Time

	// ===== 設計 3-25 の型に無いが、実装に要る項目 =====

	// Issue は dispatch した時点の issue のスナップショットである。
	// プロンプトの描画・表明の対象の解決・コメントの投稿先に使う。
	Issue tracker.Issue
	// LastWrittenState は continuo がこの run のためにボードへ最後に書いた Status である。
	//
	// **知らない Status になった issue へ「元は何だったか」を書くために持つ**（設計 3-50）。
	// **書き込みが成功した時点でだけ入れる。**入れるのは着手の段2（running_state）と、
	// 表明どおりに Status を動かしたときである。ゼロ値なら continuo は1度も書いていない。
	LastWrittenState string
	// WorktreePath は worktree の絶対パスである。
	WorktreePath string
	// Base は worktree を作ったときの base である（片付けの手順2b に要る）。
	Base normalize.SafeName
	// SettingsPath は issue ごとの Claude Code の設定ファイルの絶対パスである（設計 3-12）。
	SettingsPath string
	// HerdrWorkspaceID は herdr が開いた workspace の ID である（worktree.remove に要る）。
	HerdrWorkspaceID string
	// TranscriptPath は直近の hook が渡した transcript のパスである（設計 3-15 / 3-25）。
	TranscriptPath string
	// QuotaResetAt は枠待ちを外す時刻である（設計 3-27）。
	QuotaResetAt time.Time
	// Tokens はこの run が始めてからの累計のトークンである（設計 3-15）。
	//
	// **中身は「いまのセッションの transcript を `requestId` で重複排除して足した値」＋
	// 「それより前のセッションの分（tokensBase）」である。**
	// transcript のファイル名はセッション UUID なので（設計 3-15 の実測）、
	// 再 dispatch で UUID を採り直すと集計の対象ファイルが別物になる。
	// **足しておかないと、ダッシュボードの累計が再 dispatch のたびに巻き戻る。**
	// **ダッシュボード（第9段階）だけが読む。**判断には使わない。
	Tokens TokenUsage
	// tokensBase は前のセッションまでの累計である（セッションを採り直した時点で畳み込む）。
	// **foldTokensBase が Tokens をここへ移し、setTokens がこれに足して Tokens を作る。**
	//
	// **セッションへ `--resume` で復帰したときは畳み込まない**（設計 3-3b）。
	// 復帰すると transcript のファイルは同じままなので、畳み込むと
	// **同じファイルの中身を2回数える。**
	tokensBase TokenUsage
	// TokensAt は Tokens を集計した時刻である。ゼロ値なら一度も集計していない。
	TokensAt time.Time
	// MissingSignal は前回の turn に表明が無かったことを表す（設計 3-25 の第3層）。
	// 真なら次の継続の指示に、表明を促す1文を差し込む。
	MissingSignal bool
	// Finished は run が終わって印から外したことを表す。
	// turn ループが自分の goroutine を止める判定に使う。
	Finished bool
	// SendFirstPrompt は「次に送る turn は1回目の本文（5-3）である」ことを表す。
	//
	// **会話履歴の有無を表す値ではない**（設計 3-3b）。以前は「いまのセッションに
	// 会話履歴が無い」という1つの値でこの分岐を兼ねていたが、**再着手でセッションへ
	// `--resume` で復帰するようになって、その2つは一致しなくなった。**
	// 復帰した run には会話履歴があるのに、送るのは1回目の本文だからである。
	// **1つの値に2つの意味を持たせると、どちらの意味で書かれた分岐なのかが読めなくなる。**
	//
	// **着手と再着手で真になる**（beginAttempt）。1回目の本文には「issue を読むこと」と
	// 「紐づく PR も読むこと」が入っており、**`In Review` から差し戻された issue で
	// 新しく付いたレビューを読ませられるのは、この本文だけである。**
	//
	// **復元で引き継いだ run は偽である**（設計 3-4 の段5c）。continuo が起動し直された
	// だけで、その worker は前の turn の続きを走らせている最中かもしれない。送るのは
	// **継続の指示（5-4）**である。**turn 数を 1 から数え直すのは打ち切りの計算のためで
	// あって、1回目をやり直すことではない。**
	//
	// **turn を1つ送るたびに偽へ戻す**（beginTurn）。
	SendFirstPrompt bool

	// ===== run の終わり方と worker の世代 =====

	// terminating は「この run を終わらせる処理」が走っている最中であることを表す。
	//
	// **巡回のループが同じ run を続けて終わらせにかかるのを防ぐ。**終わらせる処理は
	// 3-25 の9段（`agent.prompt` を待ち受けつきで呼ぶ）を通ることがあり、既定では最大
	// 1時間返らない。その間に次の巡回が来ても、二重に走らせてはならない。
	//
	// **リトライを積んでバックオフに入る場合は偽へ戻す**（run はまだ続くため）。
	//
	// **書き戻しはこの印を取らない**（設計 3-56）。取らせると「終わらせない持ち主」が
	// できてしまい、この印を見て「終わらせる処理が走っている」と判断する場所が
	// すべて誤判定する。書き戻しは `rewriting` を使う。
	terminating bool
	// rewriting は「ボードの自動化が動かした Status の書き戻し」が飛んでいる最中である
	// ことを表す（設計 3-56）。
	//
	// **`terminating` とは別の印である。**書き戻しは run を終わらせない。
	// **この印が立っている間は run を手放してはならない。**手放したあとに書き込みが
	// 着地すると、印の消えた issue に「作業中」の Status が書かれ、次の巡回が
	// **同じ worktree に2本目の Claude Code を立てる。**
	rewriting bool
	// rewriteDone は、飛んでいる書き戻しが終わったときに閉じるチャネルである。
	// **`rewriting` が偽のときは nil である。**
	rewriteDone chan struct{}
	// terminalWaiting は、書き戻しの終わりを待っている「終わらせる処理」の本数である
	// （設計 3-56）。
	//
	// **1本でも待っていれば、新しい書き戻しは始めない。**始めさせると、待っている側が
	// 書き戻しの列に永久に割り込まれる。
	terminalWaiting int
	// workerEpoch は worker（pane と agent）を起こした回数である。
	//
	// **turn ループは自分が起こされたときの世代を覚えておき、それが変わっていたら
	// run を諦めない**（設計 3-21）。巡回の stall 検知が先に run を諦めたあと、まだ
	// `agent.prompt` の待ちに入っていた turn ループが同じ run をもう一度諦めると、
	// RetryCount が2倍の速さで消費される。
	workerEpoch int
	// workerStopped は、いまの世代の worker が既に止められている（pane を閉じた）
	// ことを表す。startRun が新しい worker を起こすたびに偽へ戻す。
	workerStopped bool

	// ===== turn の終わりの判定に使う、turn ごとの状態 =====

	// hookCh は turn の終わりの判定に使う hook の受け口である。
	// **stall の時計とは独立している。**溢れたら捨てて警告を出す。
	hookCh chan hookserver.HookEvent
	// stopSeenAt は「background_tasks が空の Stop」を最後に受けた時刻である。
	// turn を送るたびにゼロ値へ戻す。
	stopSeenAt time.Time
	// hookSeenThisTurn は、この turn で hook を1件でも受けたかどうかである。
	// 枠待ちの判定の条件その2（この run から hook が来ていない）に使う（設計 3-27）。
	hookSeenThisTurn bool
	// runningSubagents は、いま走っている subagent である（キー: `agent_id`、値: `agent_type`）。
	//
	// **`blocked` で esc を送る前に「走っているものがあるか」を見るためだけに持つ**
	// （設計 3-11 の「走っている subagent を待ってから esc を送る」）。
	// **turn の終わりの判定には使わない。**判定は `Stop` の `background_tasks` である（設計 3-2）。
	//
	// **`SubagentStart` で足し、`SubagentStop` で外す。**turn を送るたび（`beginTurn`）と、
	// 「`background_tasks` が空の `Stop`」を受けたときに空へ戻す。
	// **戻さないと、`SubagentStop` を1件取りこぼしただけで印が永久に残る。**
	runningSubagents map[string]string
	// backgroundSubagents は、直近の `Stop` / `SubagentStop` が申告した走行中の subagent である
	// （キー: `background_tasks[].id`、値: `agent_type`）。
	//
	// **`runningSubagents` と足し合わせて使う**（設計 3-11）。**片方だけでは足りない。**
	// 親が subagent を待っている間は `Stop` が1度も来ないので `background_tasks` が空のままであり、
	// 逆に `SubagentStart` を取りこぼすと `runningSubagents` が空のままになる。
	//
	// **`id` と `agent_id` は突き合わせられる**（設計 1-3。`SubagentStart.agent_id` /
	// `Stop.background_tasks[].id` / `SubagentStop.agent_id` は named subagent 15件すべてで
	// 同じ文字列だった）。だから2つを1つの集合にまとめてよい。
	//
	// **`type` が `subagent` のものだけを入れる。**`shell` のものは、この節が扱う
	// 「サブエージェントを止めた」の話ではない。
	//
	// **`SubagentStop` を受けたら、その `agent_id` をここからも外す**（`noteSubagentStop`）。
	// **`SubagentStop` 自身が `background_tasks` を持ってきて、いま終わったその subagent を
	// `status` が `running` のまま載せてくるためである**（実測記録1件で確認）。
	// **turn を送るときと「`background_tasks` が空の `Stop`」でも空へ戻すが、
	// `blocked` で終わる turn にはどちらも来ない。**
	backgroundSubagents map[string]string
	// handoffSubagents は **esc を送った時点で**走っていた subagent である（設計 3-11）。
	// キーと値は `runningSubagents` と同じ（`agent_id` → `agent_type`）。
	//
	// **引き渡しの通知は、理由の文面も【調べるところ】もここから作る。**
	// 通知を投稿するのは esc の数百ミリ秒あとであり、その間に `SubagentStop` が届くと、
	// **「N 件を止めました」と書きながら、記録は1件も載らない**が起きる。
	// **どちらも同じ時点で数える。**
	handoffSubagents map[string]string
	// handoffSubagentsFrozen は handoffSubagents に値を入れ終えたかどうかである。
	//
	// **0件で凍結した場合と、1度も凍結していない場合を区別するために持つ。**
	// 偽のあいだ（`blocked` 以外の引き渡し）は、いま走っているものをそのまま使う。
	handoffSubagentsFrozen bool
	// turnLoopRunning は turn ループの goroutine が走っているかどうかである。
	// **同じ run に2本目を立てない**ための印である。
	turnLoopRunning bool
	// awaitTurnEnd は「turn を送らずに、走っている turn の終わりを待つ」ことを表す。
	//
	// **復元で `agent_status` が `working` の run を引き継いだときに立てる**
	// （設計 3-4 の段5a2）。立てないと、その run の turn ループが1本も起きず、
	// `Stop` hook を誰も読まないまま claude.turn_timeout_ms まで放置される。
	// 巡回が拾って turn ループを起こし、起こしたら偽へ戻す。
	awaitTurnEnd bool
	// externalMoveSince は「continuo が意図していない Status へ外から動かされている」と
	// 最初に見た時刻である（設計 3-50 / 3-73）。ゼロ値なら、いまは continuo が
	// 意図した Status（`active_states` のいずれか）である。
	//
	// **猶予の起点である。**巡回のたびに入れ直すと猶予が永久に切れないので、
	// 既に入っているときは触らない。`active_states` に戻ったら消す。
	externalMoveSince time.Time
	// externalMoveKind は externalMoveSince がどの種類の動かされ方を数えているかである
	// （設計 3-73）。**種類が変わったら起点を切り直すために持つ。**
	//
	// **同時には起きないが、順に起きる。**知らない Status で9分待った run が、続けて
	// 自動化に `Done` へ動かされることがある。起点を繰り越すと、そこから測る猶予が
	// 残り1分しかない。**別の理由で止まりかけたのだから、猶予は最初から数え直す。**
	externalMoveKind externalMoveKind
	// automatedRewrites は、ボードの自動化が動かした Status を書き戻した回数である
	// （設計 3-56）。**キーは自動化が書いた Status（小文字にして前後の空白を落としたもの）。**
	//
	// **上限を持たないと止まらない。**書き戻した直後に自動化がまた動く組み合わせがあると、
	// continuo とボードが同じ issue の Status を押し合い続ける。
	automatedRewrites map[string]int
	// automatedRewriteFailures は、書き戻しが**ボードを1ミリも動かせないまま終わった**回数である
	// （設計 3-56）。キーは automatedRewrites と同じ作り方である。
	//
	// **押し合いの上限とは別に数える。**ボードが動かなかったぶんは押し合いの枠を返すので、
	// **返すだけだと「毎回失敗する書き込みを永久に打ち続ける」ことになる。**
	// 人間が戻す先の選択肢をボードから消した場合がそれである。
	// **ボードが目的の Status になったら 0 に戻す。**
	// **上限に達したまま時間が経ったときも 0 に戻す**（`expireAutomatedRewriteFailures`）。
	// 戻さないと「続けて何回」を数えているつもりで、**通信が回復しても永久に拒む。**
	automatedRewriteFailures map[string]int
	// automatedRewriteFailedAt は、その Status について最後に「戻せなかった」を数えた時刻である
	// （設計 3-56）。キーは automatedRewrites と同じ作り方である。
	//
	// **「続けて何回」を時間で切り直すために持つ。**
	automatedRewriteFailedAt map[string]time.Time
	// automatedRewriteHandedOff は、その Status とその理由について「ここからは人間へ渡す」と
	// 既にログへ出したかである（設計 3-56）。
	//
	// **キーは Status だけでは足りない。**人間へ渡る道は「押し合いの上限」と
	// 「戻せない失敗の上限」の2本あり、**Status だけで数えると、先に起きたほうが
	// もう片方のログを永久に黙らせる。**理由もキーに入れる（`automatedHandoffKey`）。
	//
	// **出すのは1度だけである。**上限に達したあとも巡回は30秒ごとに同じ判定へ来るので、
	// 毎回出すと猶予のあいだに同じ行が20回ほど流れ、他の行が埋もれる。
	automatedRewriteHandedOff map[string]bool
	// workerStopCtx は「この世代の worker を止めた」ことを turn ループへ伝える経路である
	// （設計 3-51）。
	//
	// **止める側が turn ループへ「待つのをやめろ」と伝える手段である。**これが無いと、
	// continuo が自分で pane を閉じた1秒後に `agent.prompt` が
	// `agent is no longer running` で落ち、**自分のせいの失敗が外の障害のように WARN で出る。**
	// **worker の世代ごとに作り直す**（beginAttempt）。
	workerStopCtx context.Context
	// workerStopCancel は workerStopCtx を終わらせる。stopWorker が呼ぶ。
	workerStopCancel context.CancelFunc
	// handoffPosted は引き渡しの通知を投稿済みであることを表す。
	//
	// **1つの run について1件だけにする。**failure_state へ落とす経路（finishRunClaimed）と
	// コメントを書かせられなかった経路（failCommentRecovery）が続けて走ると、
	// 理由の違う通知が2件並ぶ。
	handoffPosted bool
}

// clearStopSeen は「background_tasks が空の Stop」を受けた記録を消す。
//
// **`<task-notification>` を受けて turn が続くと分かったときに呼ぶ**（設計 3-2）。
// 消さないと、次の待ち直しで前の Stop をそのまま turn の終わりと取り違える。
func (rs *runState) clearStopSeen() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.stopSeenAt = time.Time{}
}

// newRunState は runState を組み立てる。
//
// issueID: project item の ID。
// issue: dispatch する時点の issue のスナップショット。
// now: いまの時刻（LastSeenAt と RevisionAt の初期値。ゼロ値のままだと即座に stall と
// 判定され、人間へ見せる経過時間も 1970 年起点になる）。
// 戻り値: 組み立てた runState。
func newRunState(issueID string, issue tracker.Issue, now time.Time) *runState {
	stopCtx, stopCancel := context.WithCancel(context.Background())
	return &runState{
		IssueID:          issueID,
		Issue:            issue,
		LastSeenAt:       now,
		RevisionAt:       now,
		hookCh:           make(chan hookserver.HookEvent, hookChanSize),
		workerStopCtx:    stopCtx,
		workerStopCancel: stopCancel,
	}
}

// snapshot は巡回のループから安全に読める形で、判定に使う値の写しを返す。
//
// **ポインタをそのまま渡して外から触らせない。**巡回のループと turn ループが
// 同時に触るためである。
//
// 戻り値: 判定に使う値の写し。
func (rs *runState) snapshot() runSnapshot {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return runSnapshot{
		IssueID:          rs.IssueID,
		Identifier:       rs.Issue.Identifier,
		AgentName:        rs.AgentName,
		PaneID:           rs.PaneID,
		SessionUUID:      rs.SessionUUID,
		TurnCount:        rs.TurnCount,
		RetryCount:       rs.RetryCount,
		BackoffUntil:     rs.BackoffUntil,
		WaitingQuota:     rs.WaitingQuota,
		QuotaResetAt:     rs.QuotaResetAt,
		LastRevision:     rs.LastRevision,
		RevisionAt:       rs.RevisionAt,
		LastSeenAt:       rs.LastSeenAt,
		LastHookAt:       rs.LastHookAt,
		StartedAt:        rs.StartedAt,
		WorktreePath:     rs.WorktreePath,
		Base:             rs.Base,
		SettingsPath:     rs.SettingsPath,
		TranscriptPath:   rs.TranscriptPath,
		HerdrWorkspaceID: rs.HerdrWorkspaceID,
		Finished:         rs.Finished,
		SendFirstPrompt:  rs.SendFirstPrompt,
		State:            rs.Issue.State,
		Title:            rs.Issue.Title,
		URL:              issueURL(rs.Issue),
		Tokens:           rs.Tokens,
		TokensAt:         rs.TokensAt,
		hookSeenThisTurn: rs.hookSeenThisTurn,
	}
}

// runSnapshot は runState の値の写しである。巡回のループが判定に使う。
type runSnapshot struct {
	IssueID          string
	Identifier       string
	AgentName        normalize.SafeName
	PaneID           string
	SessionUUID      string
	TurnCount        int
	RetryCount       int
	BackoffUntil     time.Time
	WaitingQuota     bool
	QuotaResetAt     time.Time
	LastRevision     uint64
	RevisionAt       time.Time
	LastSeenAt       time.Time
	LastHookAt       time.Time
	StartedAt        time.Time
	WorktreePath     string
	Base             normalize.SafeName
	SettingsPath     string
	TranscriptPath   string
	HerdrWorkspaceID string
	Finished         bool
	SendFirstPrompt  bool
	State            string
	Title            string
	URL              string
	Tokens           TokenUsage
	TokensAt         time.Time
	hookSeenThisTurn bool
}

// beginTurn は turn を1つ送る直前の状態にする（設計 3-14）。
//
// **TurnCount を増やすのはここだけである。**continuo が送った回数だけを数えるので、
// Claude Code 自身が投入する `<task-notification>` では増えない。
//
// now: いまの時刻。
// 戻り値: 増やしたあとの TurnCount。
func (rs *runState) beginTurn(now time.Time) int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.TurnCount++
	rs.NeedsPrompt = false
	// 1回目の本文（5-3）を送るのは、新しいセッションの最初の turn だけである。
	// 2回目からは継続の指示（5-4）に切り替える（設計 3-8 / `SPEC.md` 7.1）。
	rs.SendFirstPrompt = false
	rs.stopSeenAt = time.Time{}
	rs.hookSeenThisTurn = false
	// **前の turn の subagent を持ち越さない**（設計 3-11）。持ち越すと、次の turn で
	// `blocked` になったときに「走っている」と誤って判定し、猶予いっぱい待たされる。
	rs.resetSubagentsLocked()
	rs.LastSeenAt = now
	if rs.StartedAt.IsZero() {
		rs.StartedAt = now
	}
	// 受け口に前の turn の残りが溜まっていると、次の turn の判定を誤らせる。
	for {
		select {
		case <-rs.hookCh:
		default:
			return rs.TurnCount
		}
	}
}

// noteHook は hook を1件受けたことを記録する（stall の時計をリセットする。設計 3-21）。
//
// **turn の終わりの判定には使わない。**判定は hookCh を読む側が行う。
//
// **`LastHookAt` を進めるのはこの関数だけである。**枠待ちの間は stall の時計
// （`LastSeenAt`）を止めるが、**受けた事実そのものは必ず記録する。**
//
// ev: 受けた hook。
// now: 受けた時刻。
func (rs *runState) noteHook(ev hookserver.HookEvent, now time.Time) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.hookSeenThisTurn = true
	rs.LastHookAt = now
	if !rs.WaitingQuota {
		// 枠待ちの間は時計を止める（LastSeenAt を進めない。設計 3-27）。
		// **枠待ち中は hook が来ないので、ここに来ること自体がまれである。**
		rs.LastSeenAt = now
	}
	if ev.TranscriptPath != "" {
		rs.TranscriptPath = ev.TranscriptPath
	}
	if ev.HookEventName == hookUserPromptSubmit && ev.PromptID != "" {
		// 投入時には取れないので、UserPromptSubmit を受けた時点で入れる（設計 3-25）。
		rs.PromptID = ev.PromptID
	}
	if ev.BackgroundTasks != nil {
		// **`background_tasks` は Claude Code 自身の申告である**（設計 1-7 / 3-2）。
		// **中身をそのまま覚え直す**（差分ではない）。
		//
		// **手元の実測記録では `Stop` 4件と `SubagentStop` 1件にだけ入っていた**
		// （[docs/evidence/hooks_probe_20260817.jsonl](../../docs/evidence/hooks_probe_20260817.jsonl)
		// の14件。`SessionStart` / `UserPromptSubmit` / `PreToolUse` / `PostToolUse` /
		// `SubagentStart` には無い）。**記録は1回ぶんなので、他の hook に入らないとは言い切れない。**
		// だから hook の種類では絞らず、**入っていたら読む。**
		//
		// **その `SubagentStop` は、いま終わった subagent を `status` が `running` のまま
		// 載せていた。**だから `noteSubagentStop` が明示的に外す（`OnHook` はこちらを先に呼ぶ）。
		rs.setBackgroundSubagentsLocked(*ev.BackgroundTasks)
	}
	if ev.HookEventName == hookStop && ev.BackgroundTasks != nil && len(*ev.BackgroundTasks) == 0 {
		rs.stopSeenAt = now
		// **`background_tasks` が空なのは「1つも走っていない」ことである**（設計 3-2）。
		// **Claude Code 自身が数えたものなので、こちらの数え方より確かである。**
		// `SubagentStop` を取りこぼしていても、ここで印が残り続けないようにする。
		rs.resetSubagentsLocked()
	}
}

// noteSubagentStart は subagent が走り始めたことを覚える（設計 3-11）。
//
// **`agent_id` が空なら覚えない。**`SubagentStop` と対にできず、外す手立てが無くなる。
// **上限に達していたら、新しいものは覚えない**（`maxTrackedSubagents`）。
//
// agentID: `SubagentStart` の `agent_id`。
// agentType: `SubagentStart` の `agent_type`。通知に載せる名前に使う。
func (rs *runState) noteSubagentStart(agentID, agentType string) {
	if agentID == "" {
		return
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.runningSubagents == nil {
		rs.runningSubagents = make(map[string]string, 1)
	}
	if _, known := rs.runningSubagents[agentID]; !known && len(rs.runningSubagents) >= maxTrackedSubagents {
		return
	}
	rs.runningSubagents[agentID] = agentType
}

// noteSubagentStop は subagent が終わったことを覚える（設計 3-11）。
//
// **2つの集合の両方から外す。**走行中かどうかは `runningSubagents` と
// `backgroundSubagents` を足し合わせて決めるので（`runningSubagentsLocked`）、
// **片方だけ外しても「走行中」のままである。**
//
// **`backgroundSubagents` を外す必要があるのは、`SubagentStop` 自身が
// `background_tasks` を持ってくるからである。**実測記録の `SubagentStop` 1件は、
// **いま終わったその subagent を `status` が `running` のまま載せていた**
// （[docs/evidence/hooks_probe_20260817.jsonl](../../docs/evidence/hooks_probe_20260817.jsonl)）。
// `OnHook` は `noteHook`（= `background_tasks` を覚え直す）を先に呼ぶので、
// **ここで外さないと、いま終わったものが走行中として残る。**
// `backgroundSubagents` が空へ戻るのは「turn を送るとき」と
// 「`background_tasks` が空の `Stop` を受けたとき」だけであり、
// **`blocked` で終わる turn にはどちらも来ない。**
//
// agentID: `SubagentStop` の `agent_id`。空なら何もしない。
func (rs *runState) noteSubagentStop(agentID string) {
	if agentID == "" {
		return
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	delete(rs.runningSubagents, agentID)
	delete(rs.backgroundSubagents, agentID)
}

// setBackgroundSubagentsLocked は `background_tasks` の申告を覚え直す（設計 1-7 / 3-2）。
//
// **差分ではなく、丸ごと入れ替える。**`background_tasks` はその時点で走っているものの
// 一覧であり、載っていないものは走っていない。
//
// **`type` が `subagent` のものだけを入れる**（`shell` は扱わない。フィールドの実測値は
// docs/evidence/hooks_probe_20260817.jsonl にある）。
// **`status` では絞らない。**設計 3-2 が「`background_tasks` が空でなければ未完了」と
// 決めており、**そこへ新しい判断を足さない。**
//
// **件数に上限を置く**（`maxTrackedSubagents`）。hook は外部入力である（設計 3-2 / 3-23）。
//
// **呼び出し側が `rs.mu` を持っていること。**
//
// tasks: 受け取った `background_tasks`。
func (rs *runState) setBackgroundSubagentsLocked(tasks []hookserver.BackgroundTask) {
	if len(tasks) == 0 {
		rs.backgroundSubagents = nil
		return
	}
	got := make(map[string]string, len(tasks))
	for _, t := range tasks {
		if t.Type != backgroundTaskTypeSubagent || t.ID == "" {
			continue
		}
		if _, known := got[t.ID]; !known && len(got) >= maxTrackedSubagents {
			break
		}
		got[t.ID] = t.AgentType
	}
	if len(got) == 0 {
		rs.backgroundSubagents = nil
		return
	}
	rs.backgroundSubagents = got
}

// resetSubagentsLocked は走っている subagent の印を全部下ろす。
//
// **呼び出し側が `rs.mu` を持っていること。**turn を送るときと、
// 「`background_tasks` が空の `Stop`」を受けたときに呼ぶ。
//
// **`handoffSubagents` は下ろさない。**あちらは「esc を送った時点で走っていた」を
// 凍結したものであり、**そのあと何が起きても変わってはならない。**
func (rs *runState) resetSubagentsLocked() {
	rs.runningSubagents = nil
	rs.backgroundSubagents = nil
}

// freezeHandoffSubagents は「esc を送る時点で走っていた subagent」を凍結する（設計 3-11）。
//
// **esc を送る直前に1回だけ呼ぶ。**引き渡しの通知は、理由の文面（`blockedHandoffReason`）も
// 【調べるところ】の記録（`postHandoffComment`）も、ここで凍結した集合から作る。
// **2度呼んでも上書きするだけなので、同じ run で2回引き渡されることは無い**
// （引き渡しは `currentWorker` で1回に絞られている）。
//
// 戻り値: 凍結した subagent の名前の並び（名前順）。1件も走っていなければ nil。
func (rs *runState) freezeHandoffSubagents() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.handoffSubagents = rs.runningSubagentsLocked()
	rs.handoffSubagentsFrozen = true
	return subagentLabels(rs.handoffSubagents)
}

// handoffSubagentsLocked は引き渡しの通知が使う subagent の集合を返す（設計 3-11）。
//
// **凍結してあるならそれを返す。**凍結していない引き渡し（stall / 知らない Status など）では
// いま走っているものをそのまま返す。
//
// **呼び出し側が `rs.mu` を持っていること。**
//
// 戻り値: `agent_id` から `agent_type` への対応。1件も無ければ nil。
func (rs *runState) handoffSubagentsLocked() map[string]string {
	if rs.handoffSubagentsFrozen {
		return rs.handoffSubagents
	}
	return rs.runningSubagentsLocked()
}

// subagentLabels は subagent の集合を、通知へ載せてよい名前の並びに直す。
//
// **並びは名前順である。**同じ状態なら同じ文面が出るようにするためで、
// map の走査順のままだと引き渡しの通知が呼ぶたびに入れ替わる。
//
// running: `agent_id` から `agent_type` への対応。
// 戻り値: `<agent_type>(<agent_id>)` の形の並び。1件も無ければ nil。
func subagentLabels(running map[string]string) []string {
	if len(running) == 0 {
		return nil
	}
	out := make([]string, 0, len(running))
	for id, typ := range running {
		out = append(out, formatSubagentLabel(typ, id))
	}
	slices.Sort(out)
	return out
}

// runningSubagentsLocked は2つの申告を足し合わせて返す（設計 3-11）。
//
// **どちらかが「動いている」と言っているなら、動いていると扱う。**
//
//	`SubagentStart` から `SubagentStop` まで … 親が subagent を待っている間はこちらだけが持つ
//	直近の `Stop` の `background_tasks`      … `SubagentStart` を取りこぼしたときはこちらが拾う
//
// **同じ `agent_id` なら同じものである**（設計 1-3）。名前は `SubagentStart` 側を優先する。
//
// **呼び出し側が `rs.mu` を持っていること。**
//
// 戻り値: `agent_id` から `agent_type` への対応。1件も走っていなければ nil。
func (rs *runState) runningSubagentsLocked() map[string]string {
	if len(rs.runningSubagents) == 0 && len(rs.backgroundSubagents) == 0 {
		return nil
	}
	out := make(map[string]string, len(rs.runningSubagents)+len(rs.backgroundSubagents))
	for id, typ := range rs.backgroundSubagents {
		out[id] = typ
	}
	for id, typ := range rs.runningSubagents {
		out[id] = typ
	}
	return out
}

// runningSubagentList は、**いま**走っている subagent の名前を並べて返す（設計 3-11）。
//
// **凍結したものではなく、その瞬間の値である。**esc を送る前の待ち
// （`waitForRunningSubagents`）が、終わったかどうかを覗くために呼ぶ。
//
// 戻り値: `<agent_type>(<agent_id>)` の形の並び（名前順）。1件も走っていなければ nil。
func (rs *runState) runningSubagentList() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return subagentLabels(rs.runningSubagentsLocked())
}

// handoffSubagentIDs は、引き渡しの通知に載せる subagent の `agent_id` を並べて返す
// （設計 3-11）。
//
// **記録の置き場所を組み立てるために使う**（`SubagentTranscriptsFor`）。
// **`agent_id` は外部入力のままである。**パスの部品に使ってよいかは、使う側が
// `safeAgentID` で確かめる。
//
// **凍結してあるならその集合を使う**（`freezeHandoffSubagents`）。**理由の文面と
// 同じ時点で数えるためである。**凍結していない引き渡しでは、いま走っているものを使う。
//
// **並びは名前順である。**呼ぶたびに順番が入れ替わると、通知の文面が毎回変わる。
//
// 戻り値: `agent_id` の並び。1件も無ければ nil。
func (rs *runState) handoffSubagentIDs() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	running := rs.handoffSubagentsLocked()
	if len(running) == 0 {
		return nil
	}
	out := make([]string, 0, len(running))
	for id := range running {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// formatSubagentLabel は subagent の名前を、issue のコメントへ載せてよい形に直す。
//
// agentType: `agent_type`。空なら `subagent` とする。
// agentID: `agent_id`。
// 戻り値: `<agent_type>(<agent_id>)` の形の名前。
func formatSubagentLabel(agentType, agentID string) string {
	name := sanitizeSubagentField(agentType)
	if name == "" {
		name = "subagent"
	}
	return name + "(" + sanitizeSubagentField(agentID) + ")"
}

// sanitizeSubagentField は hook から来た文字列を、コメントへ載せてよい1行に均す。
//
// **`agent_type` も `agent_id` も hook から来る外部入力である**（設計 3-2 / 3-23）。
// **backtick と制御文字を落とし、長さを切る。**落とさないと、引き渡しの通知の
// code span を抜け出して、issue へ好きな Markdown を書き込める。
//
// s: 元の文字列。
// 戻り値: 均した文字列（`maxSubagentLabelRunes` 文字まで）。
func sanitizeSubagentField(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if r == '`' || unicode.IsControl(r) {
			continue
		}
		if n >= maxSubagentLabelRunes {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}

// stopSeen は「background_tasks が空の Stop」を受けているかを返す。
//
// 戻り値の1つ目: 受けた時刻。
// 戻り値の2つ目: 受けていれば true。
func (rs *runState) stopSeen() (time.Time, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.stopSeenAt, !rs.stopSeenAt.IsZero()
}

// setTokens は transcript から集計したトークンを記録する（設計 3-15）。
//
// **判断には使わない。**ダッシュボード（第9段階）が読むためだけに持つ。
// 集計は turn の終わりに1回だけ行うので、**HTTP の要求ごとに transcript を開き直さない。**
//
// **前のセッションの分（tokensBase）を足してから入れる。**usage は「いまのセッションの
// transcript 1ファイルの合計」であり、そのまま入れると再 dispatch のたびに累計が巻き戻る。
//
// usage: いまのセッションの transcript から集計したトークン。
// now: 集計した時刻。
func (rs *runState) setTokens(usage TokenUsage, now time.Time) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.Tokens = rs.tokensBase.Add(usage)
	rs.TokensAt = now
}

// setWaitingQuota は枠待ちの印を立てる（設計 3-27）。
//
// **LastSeenAt は進めない。**進めると、枠が明けたあとに「最後に動いていた時刻」が
// 分からなくなる。
//
// resetAt: 枠が明ける時刻。
func (rs *runState) setWaitingQuota(resetAt time.Time) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.WaitingQuota = true
	rs.QuotaResetAt = resetAt
}

// clearWaitingQuota は枠待ちの印を外し、stall の時計を動かし直す（設計 3-27）。
//
// now: いまの時刻。
func (rs *runState) clearWaitingQuota(now time.Time) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.WaitingQuota = false
	rs.QuotaResetAt = time.Time{}
	rs.LastSeenAt = now
}

// noteRevision は画面の版を見た結果を記録する（設計 3-21）。
//
// **版が変わっていれば時計を起こし直す。**`LastSeenAt` を現在時刻にして、
// もう一度 `claude.turn_timeout_ms` だけ待つ。**画面が変わり続けている限り、
// 1つの turn に何時間かかっても打ち切らない。**
//
// **減る向きの変化も「変わった」として扱う。**版が減るのは pane を作り直したときだけで、
// そのときも画面が別物になっているので待ち直すのが正しい。
//
// rev: agent.get が返した pane の版。
// now: いまの時刻。
// 戻り値: 版が変わっていたら true（＝画面が動いている）。同じなら false。
func (rs *runState) noteRevision(rev uint64, now time.Time) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.LastRevision == rev {
		return false
	}
	rs.LastRevision = rev
	rs.RevisionAt = now
	rs.LastSeenAt = now
	return true
}

// markFinished は run が終わったことを記録する。turn ループはこれを見て止まる。
func (rs *runState) markFinished() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.Finished = true
}

// isFinished は run が終わっているかを返す。
func (rs *runState) isFinished() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.Finished
}

// setNeedsPrompt は「次の turn を送るべき」を立てる（設計 3-4 の段5c / 3-8）。
//
// **turn ループを起こせなかったときに立て直すのにも使う。**古い turn ループが
// `agent.prompt` の待ち受けから戻っていないと2本目は立てられないので、
// 黙って捨てずにこれを立て、次の巡回で起こし直す。
func (rs *runState) setNeedsPrompt() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.NeedsPrompt = true
}

// setAwaitTurnEnd は「turn を送らずに turn の終わりを待つ」を立てる（設計 3-4 の段5a2）。
func (rs *runState) setAwaitTurnEnd() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.awaitTurnEnd = true
}

// takeAwaitTurnEnd は awaitTurnEnd が立っていれば下ろして true を返す。
//
// 戻り値: 立っていたら true。
func (rs *runState) takeAwaitTurnEnd() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !rs.awaitTurnEnd {
		return false
	}
	rs.awaitTurnEnd = false
	return true
}

// takeHandoffPost は引き渡しの通知をこの run で初めて投稿するかを返す。
//
// **投稿してよいのは1回だけである。**2回目以降は偽を返す。
//
// 戻り値: 投稿してよければ true。
func (rs *runState) takeHandoffPost() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.handoffPosted {
		return false
	}
	rs.handoffPosted = true
	return true
}

// takeNeedsPrompt は NeedsPrompt が立っていれば下ろして true を返す。
//
// 戻り値: 立っていたら true。
func (rs *runState) takeNeedsPrompt() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !rs.NeedsPrompt {
		return false
	}
	rs.NeedsPrompt = false
	return true
}

// setMissingSignal は「前回の turn に表明が無かった」を記録する（設計 3-25 の第3層）。
//
// missing: 表明が無ければ true。
func (rs *runState) setMissingSignal(missing bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.MissingSignal = missing
}

// missingSignal は「前回の turn に表明が無かった」かを返す。
func (rs *runState) missingSignal() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.MissingSignal
}

// addRetry はリトライを1つ積み、バックオフの期限を入れる（設計 3-21）。
//
// **バックオフ中も印には残す**（外すと30秒後の巡回で即座に拾い直され、バックオフが効かない）。
//
// now: いまの時刻。
// backoff: 待つ長さ。
// 戻り値: 積んだあとのリトライ回数。
func (rs *runState) addRetry(now time.Time, backoff time.Duration) int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.RetryCount++
	rs.BackoffUntil = now.Add(backoff)
	return rs.RetryCount
}

// clearBackoff はバックオフの期限を消す（再 dispatch に入るとき。設計 3-25）。
//
// **RetryCount はそのままにする。**
func (rs *runState) clearBackoff() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.BackoffUntil = time.Time{}
}

// issue は issue のスナップショットの写しを返す。
//
// **直接フィールドを読ませない。**巡回のループ（照合で書き換える）と turn ループと
// 監視の読み取りが同時に触るためである。
//
// 戻り値: issue の写し。
func (rs *runState) issue() tracker.Issue {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.Issue
}

// setIssue は issue のスナップショットを差し替える（ID 指定で取り直したとき）。
//
// issue: 取り直した issue。
func (rs *runState) setIssue(issue tracker.Issue) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.Issue = issue
}

// setLastWrittenState は continuo がボードへ書いた Status を控える（設計 3-50）。
//
// **書き込みが成功したときだけ呼ぶ。**知らない Status になった issue へ
// 「元は何だったか」を書くための唯一の材料である。
//
// state: 書き込んだ Status 名。
func (rs *runState) setLastWrittenState(state string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.LastWrittenState = state
}

// lastWrittenState は continuo がボードへ最後に書いた Status を返す。
//
// 戻り値: 書いた Status 名。1度も書いていなければ空文字。
func (rs *runState) lastWrittenState() string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.LastWrittenState
}

// externalMoveKind は「外から動かされた」の種類である（設計 3-73）。
//
// **猶予の起点を種類ごとに切り直すために持つ。**種類が変わったのに起点を繰り越すと、
// 前の種類で待った時間ぶんだけ、次の猶予が短くなる。
type externalMoveKind int

const (
	// externalMoveNone は「外から動かされていない」を表す（起点はゼロ値）。
	externalMoveNone externalMoveKind = iota
	// externalMoveUnknownState は continuo が知らない Status へ動かされたことを表す（設計 3-50）。
	externalMoveUnknownState
	// externalMoveAutomatedHandoff はボードの自動化が終端・引き渡しの Status を書いたことを
	// 表す（設計 3-73）。
	externalMoveAutomatedHandoff
)

// noteExternalMove は「continuo が意図していない Status へ外から動かされている」と見た
// 時刻を控え、その起点を返す（設計 3-50 / 3-73）。
//
// **同じ種類が続くあいだ、起点は最初に見たときのまま据え置く。**巡回のたびに入れ直すと、
// 猶予が永久に切れない。
//
// **種類が変わったら起点を切り直す。**知らない Status で待っていた run が、続けて自動化に
// `Done` へ動かされることがある。**別の理由で止まりかけたのだから、猶予は最初から数え直す。**
//
// now: いまの時刻。
// kind: 何を数えているか（externalMoveNone を渡してはならない）。
// 戻り値: 猶予の起点（この種類を最初に見た時刻）。
func (rs *runState) noteExternalMove(now time.Time, kind externalMoveKind) time.Time {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.externalMoveSince.IsZero() || rs.externalMoveKind != kind {
		rs.externalMoveSince = now
		rs.externalMoveKind = kind
	}
	return rs.externalMoveSince
}

// clearExternalMove は「外から動かされている」という記録を消す。
//
// **`active_states` に戻ったときに呼ぶ。**消さないと、次に外から動かされたときに、
// 前回の起点で猶予を測ってしまう。
func (rs *runState) clearExternalMove() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.externalMoveSince = time.Time{}
	rs.externalMoveKind = externalMoveNone
}

// rewriteClaim は「自動化が動かした Status を書き戻す」1回ぶんの確保である（設計 3-56）。
//
// **枠を取った側だけが返せる形にしてある。**枠を Status 名だけで返す作りにすると、
// **巡回の goroutine と turn ループが同じ Status について同時に取ったとき、
// 片方の失敗がもう片方の枠を返してしまう。**返った枠は次の巡回がまた取れるので、
// **ボードが実際に動いた回数が上限を超える。**逆に、同じ失敗の経路を2度通ると
// **1回の確保で2回ぶん返し、押し合いの数え方が壊れる。**
//
// **`release` は何度呼んでも1回しか返さない**（`sync.Once`）。
// **nil に対して呼んでも安全である**（確保できなかったときの後始末を分岐で書かずに済む）。
type rewriteClaim struct {
	// rs は枠を取った run である。
	rs *runState
	// state は自動化が書いた Status 名（確保したときの綴りのまま）。
	state string
	// once は「返すのは1回だけ」を守る。
	once sync.Once
}

// release は確保した書き戻しの1回ぶんを返す（設計 3-56）。
//
// **ボードが1ミリも動かなかったときに呼ぶ。**通信が失敗した・item がもう見えない・
// 既にその値だった・`terminal_states` に入っていたので書かなかった、のいずれかである。
//
// **返さないと、押し合いが1度も起きていない run が止まる。**枠は
// 「continuo とボードの自動化が同じ issue を押し合っている」ことを数えるためにあり、
// **押し合いはボードが動いたときにだけ起きる。**GitHub への書き込みが3回続けて
// 失敗しただけで上限に達すると、**その run はそこから書き戻しをやめ、次に自動化が
// 動いた時点で worker ごと止まる。**
func (c *rewriteClaim) release() {
	if c == nil {
		return
	}
	c.once.Do(func() { c.rs.releaseAutomatedRewrite(c.state) })
}

// claimAutomatedRewrite は「自動化が動かした Status を書き戻す」1回ぶんを確保する
// （設計 3-56）。
//
// **確保できたときだけ handle を返し、その場で1回ぶん数える。**巡回は30秒ごとに走るので、
// 数える前に書きに行くと、書き込みが終わる前の巡回が同じ書き戻しを何本も立てる。
//
// **数えるのは Status ごとである。**自動化が `In Progress` と `Done` の両方を書く運用で、
// 片方の回数がもう片方を食い潰さないようにする。
//
// state: 自動化が書いた Status 名（前後の空白と大文字小文字は無視して数える）。
// limit: 1つの Status につき書き戻してよい回数。
// 戻り値の1つ目: 確保できたら handle。上限に達していたら nil。
// 戻り値の2つ目: この Status をこれまでに書き戻した回数（確保した分を含まない）。
func (rs *runState) claimAutomatedRewrite(state string, limit int) (*rewriteClaim, int) {
	key := strings.ToLower(strings.TrimSpace(state))
	rs.mu.Lock()
	defer rs.mu.Unlock()
	done := rs.automatedRewrites[key]
	if done >= limit {
		return nil, done
	}
	if rs.automatedRewrites == nil {
		rs.automatedRewrites = map[string]int{}
	}
	rs.automatedRewrites[key] = done + 1
	return &rewriteClaim{rs: rs, state: state}, done
}

// releaseAutomatedRewrite は枠を1つ返す。**呼ぶのは `rewriteClaim.release` だけである。**
//
// **0 より下へは減らさない。**確保していないのに返す呼び出しがあっても、
// 上限の意味が壊れないようにする。
//
// state: 自動化が書いた Status 名（claimAutomatedRewrite に渡したものと同じ）。
func (rs *runState) releaseAutomatedRewrite(state string) {
	key := strings.ToLower(strings.TrimSpace(state))
	rs.mu.Lock()
	defer rs.mu.Unlock()
	done := rs.automatedRewrites[key]
	if done <= 0 {
		return
	}
	rs.automatedRewrites[key] = done - 1
}

// noteAutomatedRewriteFailure は「書き戻したが、ボードは1ミリも動かなかった」を数える
// （設計 3-56）。
//
// **押し合いの枠とは別に数える。**枠は失敗のたびに返るので、
// **返すだけでは、毎回失敗する書き込みを永久に打ち続ける run ができる。**
//
// state: 自動化が書いた Status 名。
// add: 数える回数。**待っても直らない失敗では上限をそのまま渡す**（1回で人間へ渡すため）。
// now: いまの時刻（「続けて何回」を時間で切り直すために控える）。
// 戻り値: 数えたあとの回数。
func (rs *runState) noteAutomatedRewriteFailure(state string, add int, now time.Time) int {
	key := strings.ToLower(strings.TrimSpace(state))
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.automatedRewriteFailures == nil {
		rs.automatedRewriteFailures = map[string]int{}
	}
	if rs.automatedRewriteFailedAt == nil {
		rs.automatedRewriteFailedAt = map[string]time.Time{}
	}
	rs.automatedRewriteFailures[key] += add
	rs.automatedRewriteFailedAt[key] = now
	return rs.automatedRewriteFailures[key]
}

// clearAutomatedRewriteFailures は「戻せなかった」回数を 0 に戻す（設計 3-56）。
//
// **ボードが目的の Status になったときに呼ぶ。**一度でも戻せたのなら、
// それまでの失敗は一時的なものだったということである。
//
// state: 自動化が書いた Status 名。
func (rs *runState) clearAutomatedRewriteFailures(state string) {
	key := strings.ToLower(strings.TrimSpace(state))
	rs.mu.Lock()
	defer rs.mu.Unlock()
	delete(rs.automatedRewriteFailures, key)
	delete(rs.automatedRewriteFailedAt, key)
}

// expireAutomatedRewriteFailures は「戻せなかった」回数を、最後の失敗から時間が経っていれば
// 0 に戻す（設計 3-56）。
//
// **数えているのは「続けて何回」である。**成功で 0 に戻す道はあるが、
// **上限に達した run は書き戻しそのものをやめるので、その道へは二度と入れない。**
// **通信が回復しても永久に拒み続ける**ことになるので、時間でも切り直す。
//
// **巡回のたびに打ち直すのとは違う。**打ち直す間隔が30秒から `after` へ伸びる。
//
// state: 自動化が書いた Status 名。
// now: いまの時刻。
// after: 最後の失敗からこれだけ経っていたら 0 に戻す。
// 戻り値: 0 に戻したら true。
func (rs *runState) expireAutomatedRewriteFailures(state string, now time.Time, after time.Duration) bool {
	key := strings.ToLower(strings.TrimSpace(state))
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.automatedRewriteFailures[key] <= 0 {
		return false
	}
	last, ok := rs.automatedRewriteFailedAt[key]
	if !ok || now.Sub(last) < after {
		return false
	}
	delete(rs.automatedRewriteFailures, key)
	delete(rs.automatedRewriteFailedAt, key)
	return true
}

// automatedRewriteFailureCount は「戻せなかった」回数を返す（設計 3-56）。
//
// state: 自動化が書いた Status 名。
// 戻り値: 続けて戻せなかった回数。
func (rs *runState) automatedRewriteFailureCount(state string) int {
	key := strings.ToLower(strings.TrimSpace(state))
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.automatedRewriteFailures[key]
}

// automatedHandoffReason は「自動化が動かした Status を、なぜ人間へ渡すのか」である
// （設計 3-56）。
//
// **理由ごとに数える。**Status だけで数えると、先に起きたほうがもう片方のログを
// 永久に黙らせる（**人間には、起きているうちの1つしか見えなくなる**）。
type automatedHandoffReason string

const (
	// handoffByPushback は「continuo とボードの自動化が押し合って上限に達した」である。
	handoffByPushback automatedHandoffReason = "押し合いの上限"
	// handoffByFailures は「書き戻しがボードを1ミリも動かせないまま上限に達した」である。
	handoffByFailures automatedHandoffReason = "戻せない失敗の上限"
)

// noteAutomatedRewriteHandoff は「ここからは人間へ渡す」を**最初の1回だけ**真で返す
// （設計 3-56）。
//
// **巡回は30秒ごとに同じ判定へ来る。**毎回ログに出すと、猶予のあいだに同じ行が
// 20回ほど流れて他の行が埋もれる。**対応表に無かったときの分岐が1行も出さないのと
// 同じ理由である**（案内は issue のコメントに書く）。
//
// **数えるのは Status と理由の組である。**Status だけで数えると、
// 押し合いで先に1度出したあとに「戻せない失敗」が起きても、その行が出ない。
//
// state: 自動化が書いた Status 名。
// reason: 人間へ渡す理由。
// 戻り値: この Status とこの理由の組で初めて人間へ渡すなら true。
func (rs *runState) noteAutomatedRewriteHandoff(state string, reason automatedHandoffReason) bool {
	key := strings.ToLower(strings.TrimSpace(state)) + "\x00" + string(reason)
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.automatedRewriteHandedOff[key] {
		return false
	}
	if rs.automatedRewriteHandedOff == nil {
		rs.automatedRewriteHandedOff = map[string]bool{}
	}
	rs.automatedRewriteHandedOff[key] = true
	return true
}

// turnLoopActive は turn ループの goroutine が走っているかを返す（設計 3-50）。
//
// **「turn が動いている」の判定はこれである。**turn ループは turn を送ってから
// `Stop` hook を読み、表明を読んで Status を動かすところまでを1本で回す。
// **その goroutine が居る間は、エージェントの表明がこれから届きうる。**
//
// 戻り値: turn ループが走っていれば true。
func (rs *runState) turnLoopActive() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.turnLoopRunning
}

// markWorkerStopped は「この世代の worker を止めた」ことを記録し、
// turn ループへ「待つのをやめろ」と伝える（設計 3-51）。
//
// **`pane.close` を呼ぶ前に呼ぶこと。**閉じたあとに伝えると、turn ループは先に
// `agent is no longer running` を受け取り、**continuo 自身が止めたせいの失敗を
// 外の障害として印字する。**
func (rs *runState) markWorkerStopped() {
	rs.mu.Lock()
	rs.workerStopped = true
	cancel := rs.workerStopCancel
	rs.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// workerStopped は、いまの世代の worker を continuo 自身が止めたかを返す。
//
// **turn ループはこれを見て、自分のせいの失敗を WARN にしない**（設計 3-51）。
//
// 戻り値: 止めていれば true。
func (rs *runState) stoppedByContinuo() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.workerStopped
}

// workerStopContext は「この世代の worker を止めた」ときに終わるコンテキストを返す。
//
// **turn ループはこれで待ちを打ち切る**（設計 3-51）。
//
// 戻り値: 止めたときに終わるコンテキスト。
func (rs *runState) workerStopContext() context.Context {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.workerStopCtx
}

// setIssueState は issue のスナップショットの Status だけを差し替える。
//
// **段2 で running_state を書いたあとに呼ぶ**（設計 3-16 の段-1 が状態ごとの上限を
// running_state のバケツで数えるため）。
//
// state: 書き込んだ Status。
func (rs *runState) setIssueState(state string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.Issue.State = state
}

// setWorkspaceInfo は worktree を用意した結果を入れる（着手の段3）。
//
// path: worktree の絶対パス。
// base: worktree を作ったときの base。
// workspaceID: herdr の workspace の ID。
func (rs *runState) setWorkspaceInfo(path string, base normalize.SafeName, workspaceID string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.WorktreePath = path
	rs.Base = base
	rs.HerdrWorkspaceID = workspaceID
}

// setSettingsPath は issue ごとの設定ファイルのパスを入れる（着手の段5）。
//
// path: 設定ファイルの絶対パス。
func (rs *runState) setSettingsPath(path string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.SettingsPath = path
}

// setSessionUUID はこの run が使うセッション UUID を入れる（着手の段5b）。
//
// uuid: 新しく採番した UUID、または `--resume` で復帰する既存の UUID（設計 3-3b）。
// **新しく採番したものを使い回してはならない**（設計 3-3）。
func (rs *runState) setSessionUUID(uuid string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.SessionUUID = uuid
}

// sessionUUID はこの run が使っているセッション UUID を返す。
func (rs *runState) sessionUUID() string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.SessionUUID
}

// setPaneID は使う pane の ID を入れる（着手の段8）。
//
// paneID: herdr の pane ID。
func (rs *runState) setPaneID(paneID string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.PaneID = paneID
}

// setAgentName は herdr の agent 名を入れる（着手の段9）。
//
// name: agent 名。
func (rs *runState) setAgentName(name normalize.SafeName) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.AgentName = name
}

// agentName は herdr の agent 名を返す。
func (rs *runState) agentName() normalize.SafeName {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.AgentName
}

// foldTokensBase は、ここまでの累計トークンを「前のセッションまでの分」へ畳み込む
// （設計 3-15）。
//
// **セッションを新しく採番した直後にだけ呼ぶこと。**transcript のファイル名は
// セッション UUID なので、採り直すと集計の対象ファイルが別物になり、そこから集計した値には
// 前のセッションの分が入っていない。**畳み込まないと、ダッシュボードの累計が巻き戻る。**
//
// **`--resume` で復帰したときは呼んではならない。**復帰しても transcript のファイルは
// 同じままである（実測: 2026-08-26。復帰の前後で hook が渡す `transcript_path` が一致した）。
// **呼ぶと、同じファイルの中身を2回数える。**
func (rs *runState) foldTokensBase() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.tokensBase = rs.Tokens
}

// beginAttempt は新しい試行（着手・再着手）を始めることを記録する。
//
// **worker の世代を1つ進め、「終わらせる処理が走っている」印と「worker を止めた」印を
// 外し、`SendFirstPrompt` を立てる。**起動するセッションを決めた直後に呼ぶこと。
//
// **`SendFirstPrompt` を立てるのは、着手も再着手も1回目の本文（5-3）から始めるからである**
// （設計 3-3b）。**セッションへ復帰した再着手でもそうする。**`In Review` から差し戻された
// issue では人間が PR にレビューを書いており、**「issue を読むこと」「紐づく PR も読むこと」が
// 入っているのは1回目の本文だけだからである。**継続の指示（5-4）にはそれが無い。
//
// **新しいセッションを採番した場合だけ、累計トークンを tokensBase へ畳み込む。**
// 復帰した場合の transcript は同じファイルなので、畳み込むと二重に数える（foldTokensBase）。
//
// resumed: 既存のセッションへ `--resume` で復帰する場合は真。新しく採番したなら偽。
// 戻り値: 新しい worker の世代。turn ループはこれを覚えて `currentWorker` に渡す。
func (rs *runState) beginAttempt(resumed bool) int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !resumed {
		rs.tokensBase = rs.Tokens
	}
	rs.workerEpoch++
	rs.workerStopped = false
	rs.terminating = false
	rs.SendFirstPrompt = true
	// **「止めた」の合図も作り直す**（設計 3-51）。前の世代のものを使い回すと、
	// 既に終わっているコンテキストを新しい turn ループへ渡すことになり、
	// 最初の turn を送る前に待ちが打ち切られる。
	if rs.workerStopCancel != nil {
		rs.workerStopCancel()
	}
	rs.workerStopCtx, rs.workerStopCancel = context.WithCancel(context.Background())
	return rs.workerEpoch
}

// workerGeneration はいまの worker の世代を返す（turn ループが起こされたときに覚える）。
func (rs *runState) workerGeneration() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.workerEpoch
}

// currentWorker は、その turn ループが起こした worker が今も現役かを返す（設計 3-21）。
//
// **turn ループはこれが偽なら run を諦めない。**巡回の stall 検知が先に run を諦めて
// pane を閉じた直後、まだ `agent.prompt` の待ちに入っていた turn ループが同じ run を
// もう一度諦めると、**RetryCount が2倍の速さで消費され、`failure_state` への書き込みと
// 引き渡しコメントの投稿が二重になる。**
//
// epoch: その turn ループが起こされたときの worker の世代。
// 戻り値: 世代が変わっておらず、worker もまだ止められていなければ true。
func (rs *runState) currentWorker(epoch int) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return !rs.Finished && !rs.workerStopped && rs.workerEpoch == epoch
}

// terminalGate は `beginTerminal` が印を確保できたかどうかと、確保できなかった理由である
// （設計 3-56）。
//
// **理由を返さないと、呼び出し側が「終わりに向かっている」と「書き戻しが飛んでいる」を
// 取り違える。**取り違えると、誰も終わらせていない run の turn ループが抜けて宙に浮くか、
// 終わらせる処理が黙って戻って Status も引き渡しのコメントも出なくなる。
type terminalGate int

const (
	// terminalClaimed は印を確保できたことを表す。
	terminalClaimed terminalGate = iota
	// terminalTaken は、既に別の「終わらせる処理」が走っている（または run が終わっている）
	// ことを表す。**呼び出し側は何もせずに戻ってよい。**その run は終わりに向かっている。
	terminalTaken
	// terminalRewriting は、自動化が動かした Status の書き戻しが飛んでいることを表す。
	// **run はまだ続いている。**待てば印は取れる（`claimTerminal`）。
	terminalRewriting
)

// beginTerminal は「この run を終わらせる処理」を1本に絞る。
//
// **巡回のループから終わらせるときは、印を同期で確保してから goroutine を起こす。**
// 終わらせる処理は 3-25 の9段（`agent.prompt` を待ち受けつきで呼ぶ）を通ることがあり、
// **既定では最大1時間返らない。**印が無いと、次の巡回が同じ run をもう一度終わらせにかかる。
//
// **書き戻しが飛んでいる間は確保させない**（設計 3-56）。確保させて run を手放すと、
// あとから着地する書き込みが印の消えた issue を「作業中」にし、次の巡回が
// **同じ worktree に2本目の Claude Code を立てる。**
// **ただしそれは `terminalTaken` とは別の答えである。**巡回のループから呼ぶ経路は
// 次の巡回でやり直せばよいので待たずに戻ってよいが、**turn ループのように
// 「自分が終わらせなければ誰も終わらせない」経路は `claimTerminal` で待つこと。**
//
// 戻り値: 確保できたら terminalClaimed。確保できなければその理由。
func (rs *runState) beginTerminal() terminalGate {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.terminating || rs.Finished {
		return terminalTaken
	}
	if rs.rewriting {
		return terminalRewriting
	}
	rs.terminating = true
	return terminalClaimed
}

// claimTerminal は「この run を終わらせる処理」の印を、**書き戻しの終わりを待ってから**
// 確保する（設計 3-56）。
//
// **「自分が終わらせなければ誰も終わらせない」経路が使う。**turn ループの
// `finishRun` / `failRun` / `abandonRun` がそれである。**待たずに戻ると、turn の上限に
// 達した run が Status も動かさず、引き渡しのコメントも出さず、印も外れないまま残る。**
//
// **待っている間は新しい書き戻しを始めさせない**（`terminalWaiting`）。
// 始めさせると、待っている側が書き戻しの列に永久に割り込まれる。
//
// **巡回のループから同期で呼んではならない**（設計 3-8）。待つ長さは書き込み1回ぶんである。
//
// ctx: 待ちを打ち切るコンテキスト。**終わっていたら印を取らずに偽を返す**
// （止められた run は次の起動で引き継ぐ。設計 3-4）。
// 戻り値: 確保できたら true。
func (rs *runState) claimTerminal(ctx context.Context) bool {
	rs.mu.Lock()
	rs.terminalWaiting++
	rs.mu.Unlock()
	defer func() {
		rs.mu.Lock()
		rs.terminalWaiting--
		rs.mu.Unlock()
	}()

	for {
		switch rs.beginTerminal() {
		case terminalClaimed:
			return true
		case terminalTaken:
			return false
		}
		done := rs.rewriteInFlight()
		if done == nil {
			// 直前に終わった。取り直す。
			continue
		}
		select {
		case <-done:
		case <-ctx.Done():
			return false
		}
	}
}

// rewriteInFlight は、飛んでいる書き戻しの完了を知らせるチャネルを返す（設計 3-56）。
//
// 戻り値: 書き戻しが飛んでいれば、終わったときに閉じるチャネル。飛んでいなければ nil。
func (rs *runState) rewriteInFlight() <-chan struct{} {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.rewriteDone
}

// rewriteGate は `beginRewrite` が印を確保できたかどうかと、確保できなかった理由である
// （設計 3-56）。
type rewriteGate int

const (
	// rewriteClaimed は書き戻しの印を確保できたことを表す。
	rewriteClaimed rewriteGate = iota
	// rewriteBusy は、別の書き戻しが既に飛んでいることを表す。**run はまだ続いている。**
	// 呼び出し側は書き戻しを諦めてよいが、**run を終わったことにしてはならない。**
	rewriteBusy
	// rewriteEnding は、この run が終わりに向かっている（または終わっている）ことを表す。
	rewriteEnding
)

// beginRewrite は「自動化が動かした Status の書き戻し」を1本に絞る（設計 3-56）。
//
// **終わらせる処理が走っている run へは書かない。**印が消えたあとに「作業中」の Status を
// 書くと、次の巡回が**同じ worktree に2本目の Claude Code を立てる。**
// **終わらせる処理が書き戻しの終わりを待っている場合も同じ扱いにする**
// （`terminalWaiting`）。その run はもう終わりに向かっている。
//
// **確保したら必ず `endRewrite` で返すこと。**返さないと、待っている
// `claimTerminal` が永久に返らない。
//
// 戻り値: 確保できたら rewriteClaimed。確保できなければその理由。
func (rs *runState) beginRewrite() rewriteGate {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.terminating || rs.Finished || rs.terminalWaiting > 0 {
		return rewriteEnding
	}
	if rs.rewriting {
		return rewriteBusy
	}
	rs.rewriting = true
	rs.rewriteDone = make(chan struct{})
	return rewriteClaimed
}

// endRewrite は書き戻しの印を返す（設計 3-56）。
//
// **`beginRewrite` が rewriteClaimed を返したときだけ呼ぶこと。**
func (rs *runState) endRewrite() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if !rs.rewriting {
		return
	}
	rs.rewriting = false
	close(rs.rewriteDone)
	rs.rewriteDone = nil
}

// endTerminal は「終わらせる処理」の印を外す。
//
// **run がまだ続く場合にだけ呼ぶ**（リトライを積んでバックオフに入ったとき）。
// 外さないと、次の stall の閾値でリトライが1つも積まれず、人間へ渡されないまま止まる。
func (rs *runState) endTerminal() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.terminating = false
}
