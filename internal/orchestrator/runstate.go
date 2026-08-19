package orchestrator

import (
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
	// LastSeenAt は最後に hook を受けた時刻である（stall の時計）。
	LastSeenAt time.Time

	// ===== 設計 3-25 の型に無いが、実装に要る項目 =====

	// Issue は dispatch した時点の issue のスナップショットである。
	// プロンプトの描画・表明の対象の解決・コメントの投稿先に使う。
	Issue tracker.Issue
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
	// GraceGiven は stall の猶予を1回与えたことを表す（設計 3-21 / 3-27）。
	// **2回目は与えない。**`working` のまま固まる場合があるためである。
	GraceGiven bool
	// MissingSignal は前回の turn に表明が無かったことを表す（設計 3-25 の第3層）。
	// 真なら次の継続の指示に、表明を促す1文を差し込む。
	MissingSignal bool
	// Finished は run が終わって印から外したことを表す。
	// turn ループが自分の goroutine を止める判定に使う。
	Finished bool
	// FreshSession は「いまのセッションに会話履歴が無い」ことを表す。
	//
	// **新しいセッション UUID で Claude Code を起動した直後だけ真である**
	// （着手の段5 と、バックオフ明けの再 dispatch。どちらも UUID を採り直すので
	// 会話履歴を持たない別のセッションになる）。真なら次に送る turn は
	// **1回目の本文（5-3）**である。
	//
	// **復元で引き継いだ run は偽である**（設計 3-4 の段5c）。セッションを引き継いで
	// いるのでエージェントは issue の URL も作法も既に知っており、送るのは
	// **継続の指示（5-4）**である。**turn 数を 1 から数え直すのは打ち切りの計算のためで
	// あって、1回目をやり直すことではない。**
	//
	// **turn を1つ送るたびに偽へ戻す**（beginTurn）。
	FreshSession bool

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
// now: いまの時刻（LastSeenAt の初期値。ゼロ値のままだと即座に stall と判定される）。
// 戻り値: 組み立てた runState。
func newRunState(issueID string, issue tracker.Issue, now time.Time) *runState {
	return &runState{
		IssueID:    issueID,
		Issue:      issue,
		LastSeenAt: now,
		hookCh:     make(chan hookserver.HookEvent, hookChanSize),
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
		GraceGiven:       rs.GraceGiven,
		LastSeenAt:       rs.LastSeenAt,
		StartedAt:        rs.StartedAt,
		WorktreePath:     rs.WorktreePath,
		Base:             rs.Base,
		SettingsPath:     rs.SettingsPath,
		HerdrWorkspaceID: rs.HerdrWorkspaceID,
		Finished:         rs.Finished,
		FreshSession:     rs.FreshSession,
		State:            rs.Issue.State,
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
	GraceGiven       bool
	LastSeenAt       time.Time
	StartedAt        time.Time
	WorktreePath     string
	Base             normalize.SafeName
	SettingsPath     string
	HerdrWorkspaceID string
	Finished         bool
	FreshSession     bool
	State            string
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
	rs.FreshSession = false
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
// ev: 受けた hook。
// now: 受けた時刻。
func (rs *runState) noteHook(ev hookserver.HookEvent, now time.Time) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.hookSeenThisTurn = true
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

// grantGrace は stall の猶予を1回だけ与える（設計 3-21 / 3-27）。
//
// **`LastSeenAt` を現在時刻にして、もう一度 `stall_timeout_ms` だけ待つ。**
// 与えたことを記録し、2回目は与えない。
//
// now: いまの時刻。
// 戻り値: 与えたら true。既に与えていたら false。
func (rs *runState) grantGrace(now time.Time) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.GraceGiven {
		return false
	}
	rs.GraceGiven = true
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

// setNeedsPrompt は「次の turn を送るべき」を立てる（設計 3-4 の段5c）。
func (rs *runState) setNeedsPrompt() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.NeedsPrompt = true
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

// setSessionUUID は採番したセッション UUID を入れる（着手の段5）。
//
// uuid: 新しく採番した UUID。**使い回してはならない**（設計 3-3）。
func (rs *runState) setSessionUUID(uuid string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.SessionUUID = uuid
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

// beginAttempt は新しい試行（着手・再 dispatch）を始めることを記録する。
//
// **worker の世代を1つ進め、「終わらせる処理が走っている」印と「worker を止めた」印を
// 外し、`FreshSession` を立てる。**セッション UUID を採り直した直後に呼ぶこと。
//
// **`FreshSession` を立てるのは、そのセッションが会話履歴を持たないからである。**
// 再 dispatch でも UUID は新しく採番するので（設計 3-3）、継続の指示（5-4）だけを送ると
// **どの issue を何のためにやるのかが1文字も伝わらない。**
//
// 戻り値: 新しい worker の世代。turn ループはこれを覚えて `currentWorker` に渡す。
func (rs *runState) beginAttempt() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.workerEpoch++
	rs.workerStopped = false
	rs.terminating = false
	rs.FreshSession = true
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
