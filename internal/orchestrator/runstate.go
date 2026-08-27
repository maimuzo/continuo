package orchestrator

import (
	"context"
	"strings"
	"sync"
	"time"

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
	terminating bool
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
	// unknownStateSince は「continuo が知らない Status になっている」と最初に見た時刻である
	// （設計 3-50）。ゼロ値なら、いまは知っている Status である。
	//
	// **猶予の起点である。**巡回のたびに入れ直すと猶予が永久に切れないので、
	// 既に入っているときは触らない。知っている Status に戻ったら消す。
	unknownStateSince time.Time
	// automatedRewrites は、ボードの自動化が動かした Status を書き戻した回数である
	// （設計 3-54）。**キーは自動化が書いた Status（小文字にして前後の空白を落としたもの）。**
	//
	// **上限を持たないと止まらない。**書き戻した直後に自動化がまた動く組み合わせがあると、
	// continuo とボードが同じ issue の Status を押し合い続ける。
	automatedRewrites map[string]int
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
	if ev.HookEventName == hookStop && ev.BackgroundTasks != nil && len(*ev.BackgroundTasks) == 0 {
		rs.stopSeenAt = now
	}
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

// noteUnknownState は「continuo が知らない Status になっている」と見た時刻を控え、
// その起点を返す（設計 3-50）。
//
// **起点は最初に見たときのまま据え置く。**巡回のたびに入れ直すと、猶予が永久に切れない。
//
// now: いまの時刻。
// 戻り値: 猶予の起点（最初に見た時刻）。
func (rs *runState) noteUnknownState(now time.Time) time.Time {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.unknownStateSince.IsZero() {
		rs.unknownStateSince = now
	}
	return rs.unknownStateSince
}

// clearUnknownState は「知らない Status になっている」という記録を消す。
//
// **知っている Status に戻ったときに呼ぶ。**消さないと、次に知らない Status へ動かされた
// ときに、前回の起点で猶予を測ってしまう。
func (rs *runState) clearUnknownState() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.unknownStateSince = time.Time{}
}

// claimAutomatedRewrite は「自動化が動かした Status を書き戻す」1回ぶんを確保する
// （設計 3-54）。
//
// **確保できたときだけ true を返し、その場で1回ぶん数える。**巡回は30秒ごとに走るので、
// 数える前に書きに行くと、書き込みが終わる前の巡回が同じ書き戻しを何本も立てる。
//
// **数えるのは Status ごとである。**自動化が `In Progress` と `Done` の両方を書く運用で、
// 片方の回数がもう片方を食い潰さないようにする。
//
// state: 自動化が書いた Status 名（前後の空白と大文字小文字は無視して数える）。
// limit: 1つの Status につき書き戻してよい回数。
// 戻り値の1つ目: 確保できたら true。上限に達していたら false。
// 戻り値の2つ目: この Status をこれまでに書き戻した回数（確保した分を含まない）。
func (rs *runState) claimAutomatedRewrite(state string, limit int) (bool, int) {
	key := strings.ToLower(strings.TrimSpace(state))
	rs.mu.Lock()
	defer rs.mu.Unlock()
	done := rs.automatedRewrites[key]
	if done >= limit {
		return false, done
	}
	if rs.automatedRewrites == nil {
		rs.automatedRewrites = map[string]int{}
	}
	rs.automatedRewrites[key] = done + 1
	return true, done
}

// releaseAutomatedRewrite は、確保した書き戻しの1回ぶんを返す（設計 3-54）。
//
// **ボードが1ミリも動かなかったときに呼ぶ。**通信が失敗した・item がもう見えない・
// 既にその値だった・`terminal_states` に入っていたので書かなかった、のいずれかである。
//
// **返さないと、押し合いが1度も起きていない run が止まる。**枠は
// 「continuo とボードの自動化が同じ issue を押し合っている」ことを数えるためにあり、
// **押し合いはボードが動いたときにだけ起きる。**GitHub への書き込みが3回続けて
// 失敗しただけで上限に達すると、**その run はそこから書き戻しをやめ、次に自動化が
// 動いた時点で worker ごと止まる。**
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

// beginTerminal は「この run を終わらせる処理」を1本に絞る。
//
// **巡回のループから終わらせるときは、印を同期で確保してから goroutine を起こす。**
// 終わらせる処理は 3-25 の9段（`agent.prompt` を待ち受けつきで呼ぶ）を通ることがあり、
// **既定では最大1時間返らない。**印が無いと、次の巡回が同じ run をもう一度終わらせにかかる。
//
// 戻り値: 確保できたら true。既に走っている、または run が終わっていれば false。
func (rs *runState) beginTerminal() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.terminating || rs.Finished {
		return false
	}
	rs.terminating = true
	return true
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
