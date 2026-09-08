package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// resumeBackoff はバックオフが明けた run を拾い直す（設計 3-21 / 3-25）。
//
// **巡回の先頭で印の集合を1回走査する。**候補の取得より前に行うのは、空きスロットの計算
// （3-16 の段-1）にこの結果が影響するためである。
//
//	BackoffUntil がゼロ値、または未来  … 何もしない
//	BackoffUntil を過ぎている          … その run を再 dispatch する（段0 から入り直す）
//
// ctx: 呼び出しに適用するコンテキスト。
// dispatchAllowed: この巡回で dispatch してよいか（偽なら再 dispatch も見送る）。
func (o *Orchestrator) resumeBackoff(ctx context.Context, dispatchAllowed bool) {
	if !dispatchAllowed {
		// **この巡回は dispatch を見送ると決まっている**（Status の選択肢名か gh の認証が
		// 検査に落ちた）。再 dispatch も着手の段0 から入り直す dispatch なので同じく見送る。
		// **バックオフの印は残す**ので、次の巡回でまた拾える。
		return
	}
	now := o.now()
	for _, rs := range o.snapshotRuns() {
		snap := rs.snapshot()
		if snap.BackoffUntil.IsZero() || now.Before(snap.BackoffUntil) {
			continue
		}
		o.logger.Info("バックオフが明けたので再 dispatch します",
			"identifier", snap.Identifier, "retry_count", snap.RetryCount)
		o.redispatch(ctx, rs)
	}
}

// reconcileRunning は実行中の issue の Status を ID 指定で取り直して照合する
// （巡回の GraphQL リクエストの2本目。`SPEC.md` 8.5 Part B / 設計 3-10）。
//
//	terminal_states           … worker を止めて workspace を掃除する
//	active_states かつ routable … 手元のスナップショットを更新する
//	active_states だが routable でない … **workspace を掃除せずに** worker を止める
//	それ以外（引き渡し・見えない） … **workspace を掃除せずに** worker を止める
//
// **終端と引き渡しは、書いたのがカンバンの自動化なら turn の終わりを待つ**
// （`holdForAutomatedMove`。設計 3-74）。**人間が動かしたときはいままでどおり即座に止める。**
//
// **`active_states` のまま routable でなくなった run は、その待ちの対象にしない。**
// Status の引き渡しではなく、リポジトリの信頼登録が外れた等の理由で止めるのだから、
// **Status を誰が書いたかで振る舞いを変えてはならない。**
//
// **バックオフ待ちの run は触らない。**再 dispatch を待っている最中である。
//
// **worker を止める処理は別の goroutine で回す**（設計 3-8）。ここは巡回のループの
// 中であり、**`agent.prompt` を待ち受けつきで呼ぶ経路をブロックしたまま通してはならない。**
//
// ctx: 呼び出しに適用するコンテキスト。
func (o *Orchestrator) reconcileRunning(ctx context.Context) {
	runs := o.snapshotRuns()
	if len(runs) == 0 {
		return
	}

	ids := make([]string, 0, len(runs))
	byID := make(map[string]*runState, len(runs))
	now := o.now()
	for _, rs := range runs {
		snap := rs.snapshot()
		if !snap.BackoffUntil.IsZero() && now.Before(snap.BackoffUntil) {
			continue
		}
		ids = append(ids, snap.IssueID)
		byID[snap.IssueID] = rs
	}
	if len(ids) == 0 {
		return
	}

	// **ここは「誰が Status を書いたか」も取る**（設計 3-61）。知らない Status になったとき、
	// 書き戻すか止めるかをその記録で決める（`handleUnknownState`）。
	issues, err := o.tracker.FetchIssuesByIDs(ctx, ids)
	if err != nil {
		o.logger.Warn("実行中の issue を取り直せません（この巡回では照合しません）", "error", err)
		return
	}

	seen := map[string]bool{}
	for _, issue := range issues {
		seen[issue.ID] = true
		rs, ok := byID[issue.ID]
		if !ok {
			continue
		}
		rs.setIssue(issue)

		switch {
		case containsFold(o.cfg.Tracker.TerminalStates, issue.State):
			// **書いたのがカンバンの自動化なら、turn の終わりを待つ**（設計 3-74）。
			// 「PR がマージされたら Done」の自動化が turn の途中で走ると、
			// **走っている Claude Code を continuo 自身が殺してしまう。**
			if o.holdForAutomatedMove(rs, issue) {
				continue
			}
			// **同期で呼んではならない**（設計 3-8）。片付けの前にコメントを確かめる
			// 経路（3-25 の9段）は `agent.prompt` を待ち受けつきで呼び、既定では最大
			// 1時間返らない。巡回のループがそこで止まると、dispatch も stall 検知も
			// 全部止まる。
			o.finishRunAsync(ctx, rs, "", fmt.Sprintf("Status が %s になっていました", issue.State))
		case containsFold(o.cfg.Tracker.ActiveStates, issue.State) && issue.Dispatchable:
			// まだ作業中で routable である。スナップショットの更新だけ。
			// **外から動かされていた記録は消す**（設計 3-50 / 3-74）。エージェントが表明で
			// 戻したのだから、猶予の起点も捨てる。
			rs.clearExternalMove()
		case issue.State != "" && !o.isKnownState(issue.State):
			// **continuo が知らない Status である**（設計 3-50）。黙って止めない。
			o.handleUnknownState(ctx, rs, issue)
		case containsFold(o.cfg.Tracker.ActiveStates, issue.State):
			// **Status は作業中のままだが routable でない**（設計 3-13）。リポジトリの信頼
			// 登録が外れた場合などがここへ来る。**Status の引き渡しではないので、書いたのが
			// 自動化かどうかを見ない**（設計 3-74）。待っても routable には戻らない。
			o.logger.Info("作業中の Status のままですが dispatch できなくなったので worker を止めます（worktree は残します）",
				"identifier", issue.Identifier, "状態", issue.State)
			o.stopAndReleaseAsync(ctx, rs)
		default:
			// 引き渡し（`In Review` / `Blocked` など、設定に名前が出てくる Status）。
			// **continuo 自身が書いた Status なら、turn の終わりの経路が処理中である**（設計 3-74c）。
			if o.holdForOwnMove(rs, issue) {
				continue
			}
			// **ここも書いたのが自動化なら turn の終わりを待つ**（設計 3-74）。
			if o.holdForAutomatedMove(rs, issue) {
				continue
			}
			o.logger.Info("作業中でも完了でもない状態になったので worker を止めます（worktree は残します）",
				"identifier", issue.Identifier, "状態", issue.State)
			o.stopAndReleaseAsync(ctx, rs)
		}
	}

	for id, rs := range byID {
		if seen[id] {
			continue
		}
		o.logger.Warn("issue がカンバンから見えなくなったので印から外します（continuo は面倒を見ません）",
			"identifier", rs.issue().Identifier)
		o.stopAndReleaseAsync(ctx, rs)
	}
}

// reconcileWorktrees は worktree を走査して身元ファイルを読み、Status を ID 指定で
// 取り直して照合する（巡回の GraphQL リクエストの3本目。設計 3-9 の手順7）。
//
//	cleanup.on_states に入っている            … worktree と branch を片付ける
//	active_states に戻っていて pane が生きている … その pane を閉じる（手順7b）
//	それ以外（引き渡し・見えない）             … 何もしない。**pane も worktree も残す**
//
// **印に入っている worktree はここでは触らない。**実行中の照合（reconcileRunning）が見る。
//
// ctx: 呼び出しに適用するコンテキスト。
func (o *Orchestrator) reconcileWorktrees(ctx context.Context) {
	scanned, err := o.ws.Scan()
	if err != nil {
		o.logger.Warn("worktree の置き場所を走査できません", "error", err)
		return
	}

	type orphan struct {
		path     string
		identity *workspace.Identity
	}
	var orphans []orphan
	ids := make([]string, 0, len(scanned))
	for _, w := range scanned {
		if w.Identity == nil {
			continue
		}
		if _, claimed := o.lookupRunByID(w.Identity.ProjectItemID); claimed {
			continue
		}
		orphans = append(orphans, orphan{path: w.Path, identity: w.Identity})
		ids = append(ids, w.Identity.ProjectItemID)
	}
	if len(orphans) == 0 {
		return
	}

	// **「誰が Status を書いたか」は取らない**（設計 3-61）。見るのは `State` が
	// `cleanup.on_states` に入っているかだけである。
	issues, err := o.tracker.FetchIssuesByIDsWithoutTimeline(ctx, ids)
	if err != nil {
		o.logger.Warn("worktree の照合で issue を取り直せません", "error", err)
		return
	}
	states := make(map[string]tracker.Issue, len(issues))
	for _, issue := range issues {
		states[issue.ID] = issue
	}

	for _, orph := range orphans {
		issue, ok := states[orph.identity.ProjectItemID]
		if !ok {
			// もう見えない issue の worktree。**勝手に消さない**（設計 3-4）。
			continue
		}
		if o.ws.ShouldCleanup(issue.State) {
			// 手順7: `cleanup.on_states` に入っていれば片付ける。
			// **ここで pane を閉じない。**`worktree.remove` の応答は workspace ごと
			// 閉じるので、その中の pane も一緒に消える（設計 3-9 の手順3）。
			result, err := o.ws.Cleanup(ctx, workspace.CleanupRequest{WorktreePath: orph.path})
			if err != nil {
				o.logger.Warn("取り残された worktree を片付けられません",
					"identifier", issue.Identifier, "path", orph.path, "error", err)
				continue
			}
			if result.Removed {
				o.logger.Info("取り残された worktree を片付けました",
					"identifier", issue.Identifier, "path", orph.path)
			}
			continue
		}
		// 手順7b: **Status が `active_states` に戻ったときだけ** pane を閉じる。
		// この条件を外してはならない。**`In Review` / `Blocked` の run は、復元が
		// 「pane も worktree も残す」と決めて印に入れていない**（設計 3-4 の段5a）。
		// 条件なしに閉じると、復元の直後の巡回が、人間のレビュー待ちで正常に
		// 止まっている Claude Code を毎巡回で落とす。
		if containsFold(o.cfg.Tracker.ActiveStates, issue.State) {
			o.closeOrphanPane(ctx, orph.path, orph.identity)
		}
	}
}

// clearQuotaWaitWhenBack は、枠が明けた run から枠待ちの印を外す（設計 3-27）。
//
// **外す契機は2つある。**
//
//	一、`resets_at` を過ぎたこと
//	二、使い切っている枠が1つも無くなったこと
//
// **二を落としてはならない。**`resets_at` が `null` で返る枠だけを使い切っていると、
// `QuotaResetAt` はゼロ値のままで、**一では永久に外れない。**
// turn のループは同じ判定を持っているが、
// **herdr が一時的に届かないと、待ちループは印を外さずに goroutine を畳む**（設計 3-27）。
// **そのとき外す者が1人もいなくなり、run はスロットと pane を continuo の再起動まで握り続ける。**
//
// **`checkStalls` の `claude.turn_timeout_ms` の門より前で呼ぶ。**
// **0 以下でも枠待ちの印は立つ**ので、門のあとに置くと、その設定の機械で一度も外れない。
// **`weekly_wait_limit_minutes: 0`（上限を設けない）と組み合わさると必ず当たる。**
// [docs/FAQ.md](../../docs/FAQ.md) が1台で動かす人に勧めている値である。
//
// **枠の写しは呼び出し側が1回のロックで取ったものを受け取る。**
// **ここで取り直すと、同じ巡回の中で run ごとに違う写しの答えが混ざる。**
//
// snap: この巡回で読んだ枠の写し。**nil なら「余裕が無い枠は無い」として扱う。**
// now: いまの時刻。
func (o *Orchestrator) clearQuotaWaitWhenBack(snap *ratelimit.Snapshot, stale bool, now time.Time) {
	// **古い写しでは「使い切っている」と答えない**（issue #173）。
	// **答えると、その写しで立った印を誰も外せなくなる。**
	// **`resets_at` が `null` の枠だけが100%だった機械では、時刻でも外れない。**
	// **資格情報が切れたまま、打ち切りの時計が永久に止まる。**
	full := snap != nil && !stale && snap.AnySelected(handoff.Full())
	for _, rs := range o.snapshotRuns() {
		st := rs.snapshot()
		if !st.WaitingQuota {
			continue
		}
		switch {
		case !st.QuotaResetAt.IsZero() && !now.Before(st.QuotaResetAt):
			// **ここでもログを出す**（issue #173）。
			// **「枠待ちと判定したので stall の時計を止めます」に対になる行が要る。**
			// **こちらがいちばん多い枝である**（枠は時刻で明ける）。
			// **出さないと、止まった run が再開したのかどうかを利用者が読めない。**
			rs.clearWaitingQuota(now)
			o.logger.Info("枠のリセット時刻を過ぎたので、枠待ちの印を外します"+
				"（入札できるとは限りません。余裕値はマージンのぶん手前で尽きます）",
				"identifier", st.Identifier, "resets_at", st.QuotaResetAt)
		case !full:
			o.logger.Info("使い切っている枠が無くなったので、枠待ちの印を外します"+
				"（入札できるとは限りません。余裕値はマージンのぶん手前で尽きます）",
				"identifier", st.Identifier)
			rs.clearWaitingQuota(now)
		}
	}
}

// releaseQuotaWaitExceeded は、1週間の枠を待つ上限を超えた run を手放す
// （設計 3-27。issue #197）。
//
// **手放しの入口はここ1本だけである**（人間の決定。2026-09-06）。
// turn 側の待ちループからは手放さない。**判断に要る材料を読むのが、この経路しかないためである。**
//
// **`checkStalls` の `claude.turn_timeout_ms` の門より前で呼ぶ。**
// **0 以下でも枠待ちの印は立つ**ので、門のあとに置くとその設定の機械で一度も効かない。
//
// **見る順序を入れ替えてはならない。**herdr へ問い合わせるのはいちばん最後である。
//
//  1. 枠待ちの印が立っているか                 … メモリ上の値。ただ
//  2. 1週間の枠の余裕が無く、待っても明けないか … 最後に読めた枠の写し。ただ
//  3. pane が止まっているか                    … **herdr へ1回問い合わせる**（既定5秒の持ち時間）
//
// **段3 を先に置くと、巡回のたびに走っている run の数だけ herdr を叩くことになる。**
// その間、stall 検知も枠の読み直しも止まる。
//
// **非同期に手放す。**担当者を外す要求とコメントの投稿と pane を閉じる要求が乗るので、
// **同じ巡回で複数の run が超えると直列に積まれる**（設計 3-8）。
//
// ctx: 呼び出しに適用するコンテキスト。
// quotaSnap: この巡回で1回だけ読んだ枠の写し。
// quotaStale: その写しが古いか。
// now: この巡回の時刻。
func (o *Orchestrator) releaseQuotaWaitExceeded(
	ctx context.Context, quotaSnap *ratelimit.Snapshot, quotaStale bool, now time.Time,
) map[*runState]bool {
	// **手放しの対象だと判定した run を返す**（issue #173）。
	// **打ち切りの側は、この集合を飛ばす。**
	//
	// **飛ばさないと、90〜99%の帯で手放しが1回も成立しない。**
	// 枠待ちの印は使用率100でしか立たないので、92%の run は打ち切りの本体まで落ちる。
	// **そこで `revision` は動かず、無音の閾値も超えているので、打ち切りが先に殺す。**
	// **手放しは2回続けて同じ連番を見る必要があるため、1回目の観測では必ず「まだ」と答える。**
	// **つまり、打ち切りが毎回勝つ。**
	// **入札と手放しの線を余裕値へ移した意味が、既定の設定で丸ごと消える。**
	// **読んだ `agent.get` の結果は持ち回さない**（issue #173）。
	//
	// **4周目に持ち回す形へ変えたが、5周目に戻した。**
	// **手放しの判定は run ごとに herdr を1回叩き、`herdr.read_timeout_ms`
	// （既定5000ミリ秒）まで待つ。**run が12件あれば、この関数を抜けるまでに60秒経ちうる。
	// **その写しを打ち切りの段1 が使い回すと、60秒前の状態で「止まっている」と決めることになる。**
	// **その間に動き出した run を打ち切ることになり、2回叩く費用より重い。**
	//
	// **2回叩くことは、[docs/spec/turn_end_detect_mechanizm.md](../../docs/spec/turn_end_detect_mechanizm.md) の
	// 4-5 の #5 に「残っている」として記録してある。**
	handling := map[*runState]bool{}
	// **どの枠に余裕が無いかは、run ごとに変わらない**（issue #173）。
	// **ループの中で作ると、run の数だけ枠の一覧を走査して文字列を作り直すことになる。**
	shortKinds := strings.Join(quotaSnap.SelectedKinds(handoff.ShortWeekly(o.bidMargins())), ", ")
	// **写しは呼び出し側が1回だけ読む**（設計 3-27）。**ここで取り直してはならない。**
	// **`checkStalls` は、このあと同じ run に `noteWeeklyShort` を当てる。**
	// `pollQuota` は turn の goroutine から並行に走って写しを差し替えるので、
	// **2回読むと、こちらが「余裕が無い」と控えた時刻を、あちらが「余裕がある」で消しうる。**
	// **消えると、リセット時刻を読めない枠で上限を測る唯一の道（経過時間）が閉じる。**
	for _, rs := range o.snapshotRuns() {
		snap := rs.snapshot()
		// **画面を持っていない run は、この経路で扱えない**（issue #197）。
		//
		// **`paneStopped` は herdr へ問い合わせる。**pane が既に閉じている run では
		// `agent.get` が誤りを返し、**そのたびに info の1行が出る。**
		// 1週間の枠の余裕が無いあいだ、**巡回のたびに run の数だけ積む。**
		// **issue #173 が読めるようにしようとしているログを、こちらが埋めることになる。**
		//
		// **手放せないことは変わらない。**確かめられない pane を閉じて担当を外す道は無い。
		// **打ち切りの経路（`checkStalls` の本体）が、同じ2つを同じ理由で外している。**
		if snap.AgentName == "" {
			continue
		}
		// **終わりに向かっている run は飛ばす**（issue #173）。
		// **別の goroutine が `finishRunClaimed` の途中で、pane を閉じたところかもしれない。**
		// **そこへ `agent.get` を投げると誤りが返り、run ごとに1回だけの info を1つ使い切る。**
		// **その run はそもそも手放しの候補ではない。**
		//
		// **`beginTerminal` で確かめてはならない。**あれは印を立てるので、
		// **turn ループが `terminating` を見て 500ms 待つことになる**（`turn.go`）。
		// **読むだけの `terminalBusy` を使う。**
		if rs.terminalBusy() {
			continue
		}
		if !snap.BackoffUntil.IsZero() && now.Before(snap.BackoffUntil) {
			continue
		}
		// **枠待ちの印は見ない**（人間の決定。2026-09-06。issue #197）。
		// **印は「使用率100」で立ち、この判定は「1週間の余裕値が0以下」で効く。**
		// **印を門にすると、100%でしか手放せなくなり、
		// 「入札するときの余裕値で判定して」という指示が効かなくなる。**
		if !o.weeklyWaitExceededWith(quotaSnap, quotaStale, rs) {
			continue
		}
		// **その run が進んでいないことを確かめる**（issue #197）。
		// **人間の指示は「今paneの内容が動いていたら止まるまで待って」である。**
		// **「動いていない」を pane の見た目だけで測ると、turn と turn のあいだの
		// ふつうの間や、進捗のコメントを書いている最中の run まで拾う。**
		// **既に一度、印を門にするのをやめている**（印は使用率100でしか立たないので、
		// 余裕値で判定するという決定が効かなくなる）。**代わりに、印の2つ目の条件だけを使う。**
		//
		//	claude.turn_timeout_ms のあいだ hook が1件も来ていない
		//
		// **これは「枠が満杯か」を1バイトも見ないので、余裕値の線を壊さない。**
		// **打ち切りを切っている機械では、この物差しが無い。**
		// **そのときは `agent_status` と `state_change_seq` だけで判断する**（`paneStopped`）。
		// **言えないことを理由に手放さないと、上限がその設定の機械で一度も効かない。**
		// **打ち切りの側と同じ門を先に置く**（issue #173）。
		// **`runIdleForTurnTimeout` は、この turn で hook を1件も受けていなければ
		// 経過を測らずに真を返す**（`beginTurn` が毎 turn そこを偽へ戻す）。
		// **門が無いと、指示を送った直後の run が「進んでいない」と読まれる。**
		// そこへ `agent_status` が2回続けて `idle` を返すと、
		// **枠が尽きてもいないのに、turn の開始から2巡回で担当を手放すことになる。**
		// **打ち切りの側は、同じ述語を `LastSeenAt` の門の後ろでしか呼んでいない。**
		// **`LastSeenAt` がゼロの run は、いまは存在しない**（`newRunState` が現在時刻を入れ、
		// 進める側しかない）。**それでも残す**（issue #173）。
		// **下の2つの門は、どちらもこの値からの経過を測る。**
		// **ゼロが入る経路が将来できたとき、1970年からの経過として通ってしまう。**
		// **通ると、着手した瞬間の run が「上限を超えた」と読まれて手放される。**
		if snap.LastSeenAt.IsZero() {
			continue
		}
		// **`after_run` を走らせ切った run は、この門で待たせない**（issue #173）。
		// **この門は「指示を送った直後の run を手放さない」ために在る。**
		// **既に `after_run` まで進んだ run は、その心配が無い。**
		// **待たせると、5時間の枠が明けて `clearWaitingQuota` が `LastSeenAt` を進めた瞬間に、
		// やり直しが `claude.turn_timeout_ms`（既定1時間）ぶん遠のく。**
		// **そのあいだ、担当者は付いたまま・push は済んだまま・誰も動かない run が残る。**
		if !snap.AfterRunDone {
			if silence := time.Duration(o.cfg.Claude.TurnTimeoutMs) * time.Millisecond; silence > 0 &&
				now.Sub(snap.LastSeenAt) < silence {
				continue
			}
		}
		if !o.stallDetectionOff() && !o.runIdleForTurnTimeout(rs) {
			continue
		}
		// **打ち切りを切っている機械では、経過の床をここで置く**（issue #173）。
		//
		// **`claude.turn_timeout_ms` が0以下だと、上の2つの門がどちらも素通りになる。**
		// 残るのは `paneStopped` だけで、**turn と turn のあいだで `idle` に見えるだけの
		// 健全な run が、2巡回（既定60秒）で手放される。**
		// **`workspace_hooks.after_run` を書いていない機械では、そのとき push が走らない。**
		// **次に拾う機械は remote から作り直すので、push していない commit が失われる。**
		//
		// **床には `weekly_wait_limit_minutes` を使う。**利用者が「1週間の枠をどれだけ待つか」
		// として書いた値であり、**新しい設定を増やさずに済む。**
		// **`WeeklyShortSince` は巡回のたびに控えている**ので、そのまま使える。
		//
		// **打ち切りが効いている機械では、この床は要らない。**上の2つの門が既に効いている。
		if o.stallDetectionOff() {
			limit := time.Duration(o.cfg.RateLimit.WeeklyWaitLimitMinutes) * time.Minute
			if limit > 0 && (snap.WeeklyShortSince.IsZero() || now.Sub(snap.WeeklyShortSince) <= limit) {
				continue
			}
		}
		stopped, mine := o.paneStopped(ctx, rs)
		if !mine {
			// **この経路では二度と進まない run である**（`agent.get` を読めない、
			// または `working` / `blocked` / `unknown`）。**打ち切りに任せる。**
			// **飛ばすと、止める者が1人もいなくなる。**
			continue
		}
		if !stopped {
			// **止まったと確かめられていない。**次の巡回でやり直す。
			//
			// **この巡回だけ、打ち切りから守る**（issue #173）。
			// **守るのは「1回目の観測は必ず偽を返す」という2巡回ぶんの隙間だけである。**
			// **手放しを撃ったあとは守らない。**撃って失敗し続ける run を守ると、
			// **打ち切りが1回も来ず、リトライも積まれず、`failure_state` へも落ちない。**
			// **pane とスロットを握ったまま、continuo を再起動するまで残る。**
			handling[rs] = true
			continue
		}
		// **`shortKinds` は、判定に使ったこの写しから、ループの外で作ってある**（issue #173）。
		// **手放しの本体で読み直すと、判定した写しとログに出す数字が別々になる。**
		o.releaseBecauseQuotaWaitAsync(ctx, rs, shortKinds)
	}
	return handling
}

// paneStopped は「この run の pane が完全に止まっているか」を返す
// （設計 3-27。issue #197）。
//
// **人間の指示は「そのセッションのサブエージェントを含め完全停止するまで待って」である**
// （2026-09-06）。**2つとも満たしたときだけ「止まっている」とする。**
//
//	state_change_seq … 前に見た値から変わっていない（連番が 0 でない）
//	agent_status     … idle か done である
//
// **`agent_status` で `working` だけを弾くのでは足りない。**`unknown` は
// 「**agent は居るが herdr が状態を判定できない**」という意味であり（`internal/herdr/types.go`）、
// **確かめられていない。**`blocked` は人間の入力待ちなので、閉じると確認の画面ごと消える。
// **だから「止まっている」と言えるのは `idle` と `done` の2つだけである。**
//
// **`runningSubagentList()` は使えない。**一度は3つ目の条件にしたが、取り下げた。
//
// **あの一覧を空にする経路は4つあり、4つとも hook か次の turn で駆動する**
// （`docs/spec/turn_end_detect_mechanizm.md` の 3-8 に並べてある）。
//
//  1. 次の turn を始める（`runState.beginTurn`）
//  2. `SubagentStop` を受ける（`runState.noteSubagentStop`）
//  3. `Stop` が `background_tasks` を空で載せて届く（`runstate.go` の `noteHook`）
//  4. `background_tasks` が空で届く（`Stop` に限らない。同じく `noteHook`）
//
// **枠待ちの最中は、4つとも起きない。**次の turn は枠が明けるまで送られず、hook も来ない。
// **つまり、枠が尽きた瞬間にサブエージェントが走っていた run は、一覧が永久に空にならず、
// この関数が二度と真を返さない。**手放しの仕組みが、いちばん効いてほしい場面で1回も動かなくなる。
//
// **サブエージェントは `agent_status` が受け持つ。**サブエージェントの出力も同じ pane へ出るので、
// **何かが動いているあいだ herdr は `working` を返す。**
// **`working` の決め方そのものは測ってある**
// （[docs/spec/turn_end_detect_mechanizm.md](../../docs/spec/turn_end_detect_mechanizm.md) の 3-4。
// herdr が当てた規則は `osc_title_working` で、端末タイトルのスピナー1文字を見ている）。
// **測っていないのは「subagent が走っている最中も、Claude Code がその1文字を書き続けるか」だけである**（同じ文書の6節）。
// **測れていないので、`unknown` を「止まっている」に入れない形で安全側へ倒してある。**
//
// **herdr へ届かなければ「止まっていない」を返す。**確かめられないときは手放さない側へ倒す。
// この判定の先には GitHub への2回の書き込みと pane を閉じる操作がある。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// 戻り値: 完全に止まっていれば true。
// 戻り値の2つ目は「手放しの判定が面倒を見ている run か」である（issue #173）。
//
// **偽なら、打ち切りに任せる。**`agent.get` を読めない run と、`working` / `blocked` / `unknown` の run は、
// **手放しの経路では二度と進まない。**そこを打ち切りからも守ると、**止める者が1人もいなくなる。**
// **turn ループは総実行時間では打ち切らない**（`turn.go` の待ちの説明）ので、
// **その run は pane とスロットを握ったまま、continuo を再起動するまで残る。**
// **枠がいちばん苦しい局面で、打ち切りという最後の安全網が選択的に外れることになる。**
//
// **真になるのは `idle` か `done` を読めたときだけである。**
// **守りたいのは「1回目の観測は必ず偽を返す」という2巡回ぶんの隙間だけであり、それで足りる。**
func (o *Orchestrator) paneStopped(ctx context.Context, rs *runState) (bool, bool) {
	agent, err := o.agentInfo(ctx, rs)
	if err != nil {
		// **run ごとに1回だけ出す**（issue #173。見送りの `Warn` と同じ理由）。
		// **人間が pane を閉じた run は、ここで永久に読めない。**
		// **`claude.turn_timeout_ms` が0以下だと打ち切りも来ないので、
		// 既定の30秒間隔で1時間に120行になる。**
		// **issue #173 が読めるようにしたいログを、そこで埋めることになる。**
		if rs.noteQuotaReleaseUnknown() {
			o.logger.Info("画面の状態を読めないので、1週間の枠の上限を超えていても手放しません"+
				"（次の巡回でやり直します。この行は run ごとに1回だけ出します）",
				"identifier", rs.issue().Identifier, "error", err)
		}
		return false, false
	}
	if agent.AgentStatus != herdr.AgentStatusIdle && agent.AgentStatus != herdr.AgentStatusDone {
		return false, false
	}
	// **状態が変わっていれば、まだ動いている。**
	//
	// **`revision`（pane の版）と比べてはならない**（issue #173）。
	// **あれは画面を1バイトも見ていない。**herdr が増やすのは端末タイトルの本文が変わったときだけで、
	// **continuo の pane では issue の識別子で固定されるので永久に動かない。**
	// **実測（2026-09-08、herdr 0.8.2）で、働いている3つの pane が2分間ずっと `revision: 1` だった。**
	// **比べても常に「同じ」なので、この判定は実質「`agent_status` を2回読んだ」だけになっていた。**
	//
	// **`state_change_seq` は、その agent の状態が変わったときだけ刻み直される。**
	// **30秒あけた2回の読み取りの間に `working` の山が入っていれば、値が動くので気づける。**
	//
	// **`checkStalls` の側の時計（`LastSeenAt`）を進めてはならない。**進めると、
	// **手放しの門も打ち切りも「まだ閾値に達していない」と答え続け、
	// 止まった run を誰も片付けなくなる。**
	//
	// **だから、この判定は自分が読んだ連番だけを覚える。**
	// **2回続けて同じなら止まっている。**初回は必ず偽を返す。
	if agent.StateChangeSeq == 0 {
		// **連番を返さない herdr の版である**（issue #173）。
		// **この経路では二度と進まない。**`noteQuotaProbe` が常に偽を返すためである。
		// **面倒を見ていると名乗ってはならない。**名乗ると打ち切りからも守ることになり、
		// **止める者が1人もいなくなる。**pane とスロットを握ったまま、continuo を再起動するまで残る。
		// **判定できないときは、打ち切りに任せる。**
		return false, false
	}
	stopped, first := rs.noteQuotaProbe(agent.StateChangeSeq)
	// **守るのは、1回目の観測を取った直後の1巡回だけである**（issue #173）。
	//
	// **3周目に「連番を読めたなら守る」へ広げたが、5周目に戻した。**
	// **広げると、連番が毎回変わる run が永久に守られる。**
	// 確認の画面を出しては消す agent は、巡回のたびに `idle` のまま連番だけが動く。
	// **手放しは2回続けて同じ連番を要るので成立せず、打ち切りも毎回飛ばされる。**
	// **その run は pane とスロットを握ったまま、continuo を再起動するまで残る。**
	//
	// **狭めたことで失うもの。**2回目の観測で連番が変わっていた run は、
	// **手放しではなく打ち切りで片付く。**
	// **それでよい。**`agent_status` が `idle`/`done` のまま無音の閾値を超えている run は、
	// **連番が動いていても「進んでいない」である**（`working` なら段1 が先に拾う）。
	// **打ち切りは pane を閉じてリトライを積み、理由のコメントを残す。**
	return stopped, stopped || first
}

// closeOrphanPane は印に入っていない worktree に付いている pane を閉じる
// （設計 3-9 の手順7b）。
//
// **閉じないと、次の巡回で同じ worktree に2つ目の Claude Code が立つ。**
//
// **身元ファイルの `herdr_workspace_id` を宛先にしてはならない。**身元ファイルは
// worktree の直下にあり、その worktree ではエージェントが `--permission-mode dontAsk` で
// 動く（設計 3-16 の段9）。**つまりこの値はエージェントが書き換えられる。**
// 書き換えられた値をそのまま `pane.close` へ渡すと、**同じ機械で走っている別の run の
// Claude Code を turn の途中で殺せる。**
//
// **そこで身元ファイルを1つも使わず、herdr 自身に答えさせる。**`pane.list` を絞り込みなしで
// 引き、**pane の `cwd` がこの worktree のパスと同じ場所を指すものだけ**を閉じる。
// worktree のパスは封じ込め検査（設計 3-20）を通った置き場所の内側の実体であり、
// エージェントには書き換えられない。**照合はシンボリックリンクを解決してから行う**
// （置き場所は解決済みだが、pane の cwd は起動時の文字列がそのまま入りうる。設計 3-4 の段4）。
//
// ctx: 呼び出しに適用するコンテキスト。
// worktreePath: 対象の worktree の絶対パス（走査で得た値）。
// identity: worktree の身元ファイル（**ログに出す issue の名前にだけ使う**）。
func (o *Orchestrator) closeOrphanPane(ctx context.Context, worktreePath string, identity *workspace.Identity) {
	want, ok := resolvePath(worktreePath)
	if !ok {
		// 解決できないパスは突き合わせの対象から外す（設計 3-4 の段4 と同じ判断）。
		o.logger.Warn("worktree のパスを解決できないので pane は閉じません",
			"identifier", identity.IssueIdentifier, "path", worktreePath)
		return
	}
	list, err := o.herdr.PaneList(ctx, herdr.PaneListParams{})
	if err != nil {
		o.logger.Warn("pane の一覧を取れないので pane は閉じません",
			"identifier", identity.IssueIdentifier, "path", worktreePath, "error", err)
		return
	}
	for _, p := range list.Panes {
		if p.Agent == "" {
			continue
		}
		got, ok := resolvePath(p.Cwd)
		if !ok || got != want {
			continue
		}
		o.logger.Warn("印に入っていない worktree に生きた pane があったので閉じます",
			"identifier", identity.IssueIdentifier, "pane_id", p.PaneID, "cwd", p.Cwd)
		if _, err := o.herdr.PaneClose(ctx, herdr.PaneCloseParams{PaneID: p.PaneID}); err != nil {
			o.logger.Warn("pane を閉じられませんでした", "pane_id", p.PaneID, "error", err)
		}
	}
}

// checkStalls は stall を判定する（設計 3-21 / 3-27 の評価順）。
//
// **測るのは「画面が変わらない時間」であって、turn の総実行時間ではない。**
// `SPEC.md` 10.6 は `turn_timeout_ms` を *"maximum silence interval while a turn stream is
// active; each app-server output resets it, so it is not a total turn runtime cap"*
// （turn の流れが動いている間の最大の沈黙の間隔。app-server の出力ごとにリセットされる。
// 総実行時間の上限ではない）と定めている。continuo には app-server が無いので、
// **「app-server の出力」に相当するものを herdr の `agent_status` で測る**
// （[docs/spec/turn_end_detect_mechanizm.md](../../docs/spec/turn_end_detect_mechanizm.md) の 4-1。issue #173）。
// **`revision`（画面の版）ではない。**あれは continuo の pane では永久に動かない（3-2）。
//
// **時計が動いていない run について、上から順に見る。**
//
//  1. `agent_status` が `working` か（agent.get）
//     → `working` なら時計を起こし直す。**1つの turn に何時間かかっていても打ち切らない**
//  2. `working` ではない。枠待ちか（percent が 100 かつ この run から hook が来ていない）
//     → 枠待ちなら「時計を止めている」標識を付けて終わり。**殺さない**
//  3. 枠待ちでもない
//     → worker を止め、リトライを積む
//
// **段1 を段2 より前に置く。順番を入れ替えてはならない**（設計 3-27。issue #197）。
// **枠待ちの条件は「長い1つのツール呼び出し」と区別できない。**
// 後ろに置くと、**正常に走っている run が枠待ちと名乗り、stall の時計が止まったまま戻らない。**
//
// **枠待ちの run は判定そのものを飛ばす**（`WaitingQuota` が立っている間は時計が止まっている）。
// **`LastSeenAt` は進めない**（進めると、枠が明けたあとに「最後に動いていた時刻」が分からなくなる）。
//
// **`claude.turn_timeout_ms` が 0 以下なら判定そのものを行わない**（`SPEC.md` 8.4 の
// *"If stall_timeout_ms <= 0, skip stall detection entirely"*）。
//
// ctx: 呼び出しに適用するコンテキスト。
func (o *Orchestrator) checkStalls(ctx context.Context) {
	// **1週間の枠を待つ上限は、打ち切りの判定を切っていても効かせる**（設計 3-27。issue #197）。
	// **下の `silence <= 0` より前に呼ぶ。**`claude.turn_timeout_ms` が 0 以下でも
	// **枠待ちの印は立つ**（`isQuotaWaiting` は hook を1件も受けていない run では
	// 無音の長さを見ない）。**あとに置くと、その設定の機械で上限が一度も効かない。**
	//
	// **枠の写しは、この巡回で1回だけ読む**（設計 3-27。issue #197）。
	// **手放しの側と、下の `noteWeeklyShort` の側で別々に読んではならない。**
	// `pollQuota` は turn の goroutine から並行に走るので、2回のあいだに写しが差し替わると、
	// **片方が控えた「余裕が無くなった時刻」を、もう片方が消しうる。**
	now := o.now()
	quotaSnap, quotaStale := o.quotaSnapshotWithStale()
	releasing := o.releaseQuotaWaitExceeded(ctx, quotaSnap, quotaStale, now)
	// **時刻を取り直す**（issue #173）。
	// **`releaseQuotaWaitExceeded` は run ごとに herdr を1回叩く。**
	// `herdr.read_timeout_ms`（既定5000ミリ秒）まで待つので、
	// **run が12件あれば60秒経っていることがある。**
	// **そのまま使うと、下で書く時計が全部その秒数だけ古くなる。**
	now = o.now()

	// **余裕の無い1週間の枠があるかを、同じ写しから見る。**
	weeklyShort := quotaSnap.AnySelected(handoff.ShortWeekly(o.bidMargins()))

	// **余裕が無くなった時刻は、枠待ちの印の有無によらず、巡回のたびに控える**（設計 3-27）。
	// **`weeklyWaitExceeded` の中だけで控えてはならない。**あれは印が立っている run しか
	// 通らないので、**印が別の経路で外れると、以後どこからも消されない。**
	// **消されないと、何日か普通に動いたあと1週間の枠の余裕がもう一度無くなったときに、
	// 何日も前の時刻との差で「上限を超えた」と判定し、1分も待たずに手放す。**
	//
	// **下の `silence <= 0` の門より前に置く。**`claude.turn_timeout_ms` を0以下にしている
	// 機械では、あとに置くと**この記録も走らない。**
	// **読めなくなった写しでは控えない**（issue #197）。
	// **nil や古い写しは「余裕がある」と答えるので、そのまま控えると
	// `WeeklyShortSince` がゼロへ戻り、経過で測る道が閉じる。**
	// **手放しの側が同じ理由で拒んでいるものを、こちらだけ受け入れてはならない。**
	if quotaSnap != nil && !quotaStale {
		for _, rs := range o.snapshotRuns() {
			rs.noteWeeklyShort(weeklyShort, now)
		}
	}

	// **枠が明けた run の印を外すのも、`silence <= 0` より前で行う。**
	// **あとに置くと、`claude.turn_timeout_ms` を0以下にしている機械では、
	// 一度立った印を外す者が1人もいなくなる**（下の `clearQuotaWaitWhenBack` の説明）。
	// **印を外す側は、写しが古くても走らせる**（issue #173）。
	//
	// **立てる側と外す側で、非対称にしてはならない。**
	// **立てるのは `isQuotaWaitingWith` で、そちらへ古い写しを渡さない形にした**（下の段2）。
	// **外す側まで止めると、古い写しで立った印を誰も外せなくなる。**
	// **資格情報が切れた機械は、切れる直前の値を1日中返す。**
	// **その値が100%だったら、待っている run の打ち切りの時計が永久に止まる。**
	//
	// **外すのは安全な向きである。**外して困るのは「まだ枠が尽きているのに時計が動く」ことだけで、
	// **そのとき run は打ち切られてリトライを積む。**握ったまま残るよりはるかに軽い。
	o.clearQuotaWaitWhenBack(quotaSnap, quotaStale, now)

	silence := time.Duration(o.cfg.Claude.TurnTimeoutMs) * time.Millisecond
	if silence <= 0 {
		return
	}

	for _, rs := range o.snapshotRuns() {
		snap := rs.snapshot()
		if snap.WaitingQuota {
			// **印の出し入れは、上の `clearQuotaWaitWhenBack` が済ませている。**
			// **上限は上の `releaseQuotaWaitExceeded` が見ている。**ここでは見ない。
			continue
		}
		if releasing[rs] {
			// **手放しの対象である**（issue #173）。**打ち切ってはならない。**
			// **枠待ちの印は使用率100でしか立たない**ので、90〜99%の帯の run はここまで落ちる。
			// **手放しは2回続けて同じ連番を見る必要があり、1回目の観測では必ず「まだ」と答える。**
			// **飛ばさないと、打ち切りが毎回先に殺し、手放しが1回も成立しない。**
			// **枠が足りないだけの issue が `failure_state` へ落ちる**——
			// **issue #173 が直そうとしている症状そのものである。**
			continue
		}
		if !snap.BackoffUntil.IsZero() && now.Before(snap.BackoffUntil) {
			continue
		}
		if snap.AgentName == "" || snap.LastSeenAt.IsZero() {
			continue
		}
		if now.Sub(snap.LastSeenAt) < silence {
			continue
		}

		// 1. agent.get で agent の状態を取り、`working` なら待ち続ける。
		//
		// **`working` は、長い1回のツール呼び出しの最中でも返る**
		// （[docs/spec/turn_end_detect_mechanizm.md](../../docs/spec/turn_end_detect_mechanizm.md) の 4-1。
		// `go test` を走らせながら2秒おきに60回読み、**60サンプル全部が `working` だった**）。
		// **「1つの指示に何時間かかっても打ち切らない」という約束を果たす唯一の信号である。**
		//
		// **`revision`（pane の版）を見てはならない**（issue #173）。
		// **あれは画面を1バイトも見ていない。**herdr が増やすのは端末タイトルの本文が変わったときだけで、
		// **continuo の pane では issue の識別子で固定されるので永久に動かない。**
		// **実測で、働いている3つの pane が2分間ずっと `revision: 1` だった。**
		// **つまり、この段は1度も発火していなかった。**
		//
		// **`state_change_seq` も見てはならない。**`working` が続く間は動かないので、
		// **長いツール呼び出しでは `revision` と同じく発火しない。**
		// **そのうえ、状態が往復する run では毎回動くので、永久に打ち切れなくなる。**
		//
		// **枠待ちの判定より前に置く**（設計 3-27。issue #197）。
		// **枠待ちの条件は「使用率が100」と「hook が来ていない」の2つで、
		// 「枠を待っている」と「長い1つの仕事をしている」を区別できない。**
		// hook はツールが終わってから飛ぶので、**1時間を超える1回のツール呼び出しの
		// 最中は1件も来ない。**そこへ週次の枠が満杯だと条件が両方そろい、
		// **正常に走っている run を枠待ちと名乗らせて stall の時計を止める。**
		// **後ろに置くと、その run は本当に固まっても誰にも止められない。**
		//
		// **時計を進めるのは `working` のときだけである。**
		// **`working` の run は、そもそも枠待ちではない。**
		// だから「枠待ちの run は `LastSeenAt` を進めない」という約束は破れない。
		// **ここで読み直す**（issue #173）。
		// **手放しの判定が読んだ写しを使い回してはならない。**
		// **あちらは run ごとに herdr を待つので、最後の run では60秒前の写しになりうる。**
		agent, err := o.agentInfo(ctx, rs)
		if err == nil && agent.AgentStatus == herdr.AgentStatusWorking {
			rs.noteWorking(now)
			o.logger.Info("agent が working なので待ち続けます（turn の総実行時間では打ち切りません）",
				"identifier", snap.Identifier,
				"agent_status", string(agent.AgentStatus))
			continue
		}
		if err != nil {
			o.logger.Warn("agent の状態を読めませんでした（止まったものとして扱います）",
				"identifier", snap.Identifier, "error", err)
		}

		// 2. 動いていない。枠待ちかを見る。
		// **古い写しでは印を立てない**（issue #173）。
		// **立てると打ち切りの時計が止まり、外す側が古い写しを信じない限り誰も外せない。**
		// **同じ巡回の `weeklyWaitExceededWith` と `noteWeeklyShort` も、そう倒している。**
		if quotaSnap != nil && !quotaStale && o.isQuotaWaitingWith(quotaSnap, rs) {
			// **ここでは手放さない**（人間の決定。2026-09-06。issue #197）。
			// **手放しの入口は `releaseQuotaWaitExceeded` の1本だけである。**
			// **印を立てるだけにしておけば、次の巡回の先頭でそちらが拾う。**
			// **遅れるのは巡回1回ぶん（既定30秒）である。**
			//
			// **2箇所に置いてはならない。**片方だけが直る形になり、
			// **同じ問いに違う答えが返る**（それがこの issue の元の症状である）。
			resetAt, _ := o.quotaResetAtOf(quotaSnap)
			rs.setWaitingQuota(resetAt)
			o.logger.Info("枠待ちと判定したので stall の時計を止めます",
				"identifier", snap.Identifier, "resets_at", resetAt)
			continue
		}

		// 3. 版が止まったまま閾値を超えた。worker を止め、リトライを積む。
		// **同期で呼んではならない**（設計 3-8）。打ち切りになった場合は 3-25 の9段を
		// 通り、`agent.prompt` の待ち受けで既定1時間返らない。
		o.abandonRunAsync(ctx, rs, o.stalledReason(snap, agent, now))
	}
}

// stalledReason は「`agent_status` が `working` にならないまま閾値を超えた」ときに
// 人間へ見せる文面を作る
// （設計 3-34b の形。何が起きたか →【確かめ方】→【よくある原因】→【対処】）。
//
// **`herdr agent read` を案内してはならない**（設計 3-34b）。この文面を載せたコメントの
// 直後に `pane.close` を呼ぶので、人間が読むときには agent が消えている。
//
// snap: 対象の run の写し。
// agent: agent.get が返した情報（読めなかった場合はゼロ値に近い）。
// now: いまの時刻。
// 戻り値: issue のコメントとログに載せる理由の文字列。
func (o *Orchestrator) stalledReason(snap runSnapshot, agent herdr.Agent, now time.Time) string {
	status := string(agent.AgentStatus)
	if status == "" {
		status = string(herdr.AgentStatusUnknown)
	}
	// **「一度も working になりませんでした」と書いてはならない**
	// （[docs/spec/turn_end_detect_mechanizm.md](../../docs/spec/turn_end_detect_mechanizm.md) の 4-1）。
	// **`agent.get` を読むのは、無音が閾値を超えた巡回だけである。**`working` が返れば
	// `noteWorking` が時計を起こし直して巡回を抜けるので、**ここへ落ちてくる run は
	// 「閾値を超えたあと、初めて `working` 以外を読んだ」1サンプルしか持っていない。**
	// **持っていない観測を文面に書くと、読んだ人は herdr か Claude Code の側を疑って原因を探す**
	// （5-1 の 2026-08-27 と同じ形の誤りである）。
	//
	// **そのままコピーして叩けるコマンドにする。**worktree のパスを埋め込まないと、
	// 読んだ人はまず「どこで叩くのか」を探すところから始めることになる。
	// **持っていないものは案内しない。**着手の途中で落ちた run は worktree も
	// 会話の記録も持っておらず、その行は【調べるところ】にも出ない（3-34b）。
	var parts []string
	if snap.WorktreePath != "" {
		parts = append(parts, fmt.Sprintf(
			"次のコマンドで、作業がどこまで進んでいたかを見てください。\n"+
				"```sh\ngit -C %q status\ngit -C %q log --oneline -5\n```",
			snap.WorktreePath, snap.WorktreePath))
	}
	if snap.TranscriptPath != "" {
		parts = append(parts,
			"下記の「Claude Code の会話の記録」を開き、末尾で何をしていたかを見てください。")
	}
	check := strings.Join(parts, "\n")
	if check == "" {
		// **worktree も会話の記録もまだ無い**（着手の途中で画面が止まった）。
		// 見に行ける場所が1つも無いので、次の巡回で何が起きるかだけを伝える。
		check = "この run は worktree も会話の記録もまだ持っていません。" +
			"continuo は pane を閉じ、リトライの回数が残っていれば着手からやり直します。"
	}
	return fmt.Sprintf(
		"continuo は herdr へ `agent.get` を投げて Claude Code の状態（`agent_status`）を見ています。"+
			"%s のあいだ hook が1件も届かなかったので、herdr へ状態を聞いたところ "+
			"`working` ではありませんでした（そのとき見た状態: %s）。"+
			"**止まったものと判断して打ち切りました。**"+
			"\n【確かめ方】%s"+
			"\n【よくある原因】確認の画面が出て人間の入力を待っていた / "+
			"応答の来ない相手を待ち続けていた / エージェントが応答を返し終えたまま次の指示を待っていた。"+
			"\n【対処】原因を直してから Status を着手待ちへ戻してください。"+
			"何も動かないまま待つ時間は WORKFLOW.md の `claude.turn_timeout_ms` で変えられます"+
			"（いまは %d ミリ秒）。**この値は turn の総実行時間の上限ではありません。**"+
			"`agent_status` が `working` である限り、1つの指示に何時間かかっても打ち切りません。",
		formatDuration(now.Sub(snap.LastSeenAt)), status,
		check, o.cfg.Claude.TurnTimeoutMs)
}

// formatDuration は経過時間を人間が読める日本語にする（`1時間3分` の形）。
//
// **`time.Duration.String()` を人間に見せない。**`1h3m0.5s` は読み手に伝わらない。
//
// d: 表す長さ。負なら 0 として扱う。
// 戻り値: 日本語の長さ（1分未満は「1分未満」）。
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d / time.Minute)
	if total < 1 {
		return "1分未満"
	}
	if total < 60 {
		return fmt.Sprintf("%d分", total)
	}
	if total%60 == 0 {
		return fmt.Sprintf("%d時間", total/60)
	}
	return fmt.Sprintf("%d時間%d分", total/60, total%60)
}
