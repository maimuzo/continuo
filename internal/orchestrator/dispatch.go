package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// agentStartBusyBudget は agent.start が `agent_pane_busy` を返したときに粘る時間である
// （設計 2-1 / 3-16 の段9）。
//
// **`agent_pane_busy` は「pane がまだシェルのプロンプトに来ていない」という意味である。**
// `worktree.open` が pane を作った直後は、シェルの起動が終わるまでこれが返り続ける。
// **回数（3回 × 500ms = 1.5秒）では足りず、実運用で issue が着手できなかった**
// （2026-08-21、設計 6-2）。シェルの起動はプロファイルの中身とマシンの速さで変わるので、
// 回数ではなく時間で粘る。
const agentStartBusyBudget = 30 * time.Second

// agentStartRetryDelay は agent.start の再試行の間隔である。
const agentStartRetryDelay = 500 * time.Millisecond

// agentStatusPollInterval は段10 で agent_status を見直す間隔である。
const agentStatusPollInterval = 500 * time.Millisecond

// ErrStartupRetryable は「起動に失敗したが、待てば通るかもしれない」ことを表す。
//
// **これを包んだエラーは人間へ渡さず、バックオフして次の巡回で試し直す**（設計 3-16 の段10）。
// herdr の `agent_status` が `unknown` を返すのは「まだ見分けられていない」という意味であり、
// pane を作った直後や Claude Code が起動しきる前の一瞬にも起きる。**ここで人間へ渡すと、
// 待てば通ったはずの issue が毎回 Blocked で止まる**（2026-08-21 に実際に起きた）。
//
// **errors.Is の比較対象なので、この値の identity を変えてはならない。**
// **i18n.Sentinel に替えても identity は変わらない**（比較するのはこの変数そのものである）。
// **それでもまだ替えないのは、この文が入る先が internal/orchestrator の日本語のままの文だからである。**
// ここだけ英語にすると、1つの引き渡しコメントの中で日本語と英語が混ざる。
// **この package の人間向けの文言をまとめて資源へ移すときに、一緒に替えること。**
var ErrStartupRetryable = errors.New("起動に失敗しましたが、待てば通るかもしれません")

// ErrStatusNotWritten は、着手の段2 でボードの Status を書かなかったことを表す。
//
// **これは失敗ではない。**item がもう見えないか、取り直した結果 `terminal_states` か
// `failure_state` に入っていたということであり、いずれも「いま着手してはいけない」を
// 意味する。**人間へ渡さず、印だけ静かに外す**（`failure_state` へ落とすと、
// 人間が `Blocked` に置いた issue に continuo が上書きしたことになる）。
//
// **errors.Is の比較対象なので、この値の identity を変えてはならない。**
// **i18n.Sentinel に替えても identity は変わらない**（比較するのはこの変数そのものである）。
// **それでもまだ替えないのは、この文が出る先がログだけだからである**（人間へは渡さない）。
// **ログは運用者が読むものなので、画面に出す文言と同じ資源には載せていない。**
// **この package の人間向けの文言をまとめて資源へ移すときに、一緒に見直すこと。**
var ErrStatusNotWritten = errors.New("カンバンの Status を書かなかったので着手しません")

// dispatchBlockedStates は着手の段2 で「この状態なら Status を書かない」一覧を返す。
//
// **これは二重の守りの外側であって、主の守りではない。**主の守りは
// `dispatchStatusAllowed`（許可リスト）である。拒否リストは「設定に名前が出てくる状態」
// しか並べられず、**ボードにあって設定に出てこない状態（`In Review` など）を1つも
// 拒否できない**ためである。
//
// **並べるのは、設定に名前が出てくるもののうち `active_states` に入っていないもの全部である。**
// `terminal_states`・`failure_state`・`dispatch_state`・`status_signal_map` の遷移先を集める。
//
// 戻り値: 拒否する Status の一覧（**設定のスライスは書き換えない。**新しい配列を返す）。
func (o *Orchestrator) dispatchBlockedStates() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(o.cfg.Tracker.TerminalStates)+2)
	add := func(state string) {
		s := strings.TrimSpace(state)
		if s == "" {
			return
		}
		// **`active_states` に入っているものは拒否しない。**`running_state` 自身も
		// ここに入る（再 dispatch で `In Progress` のまま書き直すことがある）。
		if containsFold(o.cfg.Tracker.ActiveStates, s) {
			return
		}
		key := strings.ToLower(s)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, s)
	}
	for _, s := range o.cfg.Tracker.TerminalStates {
		add(s)
	}
	add(o.cfg.Tracker.FailureState)
	add(o.cfg.Tracker.DispatchState)
	for _, dest := range o.cfg.Tracker.StatusSignalMap {
		if dest != nil {
			add(*dest)
		}
	}
	return out
}

// dispatchStatusAllowed は着手の段2 で「いま Status を書いてよいか」を許可リストで決める。
//
// **言いたいこと。**拒否リストでは守れない。**書いてよいのは `active_states` に
// 入っているときだけ**であり、それ以外は全部やめる。
//
// **なぜ拒否リストでは守れないか。**ボードの Status は人間が自由に増やせる。
// `In Review` は `active_states` にも `terminal_states` にも `failure_state` にも
// 入らない（設計 3-9 / 3-10。`In Review` を `terminal_states` に入れてはならない）ので、
// **拒否リストに載らず、人間へ引き渡し済みの issue を `In Progress` で上書きしてしまう。**
//
// **なぜここで取り直すか。**候補の一覧は GitHub のサーバ側の検索結果であり、
// 直前に書いた値が索引へ反映される前は古い写しが返る（設計 3-34）。
// **ID 指定の取り直しは索引を通らない**ので、いまのボードの値が返る。
// バックオフ明けの再 dispatch でも同じで、印を付けたときの写しは古い。
//
// **1回の追加の問い合わせで済ませる。**これは dispatch の直前にだけ通る経路であり、
// 巡回のたびに全件へ走るものではない。
//
// ctx: 呼び出しに適用するコンテキスト。
// itemID: 着手する project item の ID。
// identifier: ログに出す `<owner>/<repo>#<番号>`。
// 戻り値: `active_states` にあれば true。**取り直しに失敗したときも false**
// （分からないなら書かない）。
func (o *Orchestrator) dispatchStatusAllowed(ctx context.Context, itemID, identifier string) bool {
	// **「誰が Status を書いたか」は取らない**（設計 3-61）。見るのは `State` が
	// `active_states` に入っているかだけである。
	current, err := o.tracker.FetchIssuesByIDsWithoutTimeline(ctx, []string{itemID})
	if err != nil {
		o.logger.Warn("着手の直前に Status を取り直せないので着手しません（次の巡回でやり直します）",
			"identifier", identifier, "error", err)
		return false
	}
	if len(current) == 0 {
		o.logger.Warn("着手の直前に取り直したら item が見えないので着手しません",
			"identifier", identifier)
		return false
	}
	state := current[0].State
	if containsFold(o.cfg.Tracker.ActiveStates, state) {
		return true
	}
	o.logger.Info("着手の直前に取り直した Status が active_states に無いので着手しません（人間が動かした可能性があります）",
		"identifier", identifier, "取り直した Status", state,
		"active_states", strings.Join(o.cfg.Tracker.ActiveStates, ", "))
	return false
}

// dispatchCandidates は候補を並び順のまま、空きスロットが尽きるまで dispatch する
// （設計 4-2 / 3-16 の段-1）。
//
// **返ってきた配列の順序をそのまま使う。**自前で並べ替えない（並び順を決めるのは人間である。
// 設計 3-30）。
//
// **巡回のループはここでブロックしない**（設計 3-8）。同期で行うのは段-1（空きスロットの
// 検査）・段0（dispatch 直前の検査）・段1（印を付ける）までである。**段2以降は別の
// goroutine で回す。**段3〜段10 は git の worktree 作成・利用者が書いた workspace_hooks
// （既定60秒）・起動の待ち（既定60秒）を順に通るので、既定値と max_concurrent_agents=2 では
// 1回の巡回が数分返らず、その間 stall 検知も枠の読み取りも止まる。
//
// **同じ巡回で印を付けた run は、印を付けた順に1本の goroutine で処理する。**
// 並行に走らせると、ボードの並び順どおりに着手したことを外から確かめられなくなる。
//
// ctx: 呼び出しに適用するコンテキスト。
// candidates: `active_states` で取った候補（ボードの並び順）。
func (o *Orchestrator) dispatchCandidates(ctx context.Context, candidates []tracker.Issue) {
	if o.dispatchPaused() {
		// **INFO のままにする**（issue #134）。
		// **一度 WARN へ上げたが、8本のテストが落ちた**（v0.1.11 で試した）。
		// どれも正常な動作を作っているもので、
		// **「異常ではないものを異常として出そうとしている」という信号だった。**
		// 枠が戻れば自分で再開するので、人間が手を動かす必要は無い。
		// **代わりに、戻し方を同じ行に書いた。**探し当てた人が次にすることが分かる。
		o.logger.Info("枠が閾値を超えているので新規の dispatch を止めます（走行中の turn は止めません）。"+
			"枠が戻れば自分で再開します。すぐ動かしたいときは rate_limit.pause_above_percent を上げてください",
			"pause_above_percent", o.cfg.RateLimit.PauseAbovePercent)
		return
	}

	// **持ち回りの判定でコメントを読む枠を、巡回1回ぶんに戻す**（設計 3-77a）。
	o.resetHandoffFetchBudget()

	var claimed []claimedRun
	for _, issue := range candidates {
		if ctx.Err() != nil {
			break
		}
		// 既に印を持っている issue は dispatch しない（設計 3-10 / 4-2）。
		if _, taken := o.lookupRunByID(issue.ID); taken {
			// **関門より前で飛ばした**（設計 6-1）。止めているのは担当者の関門ではないので、
			// 古い理由と誤った直し方をダッシュボードに出し続けない。
			o.clearGate(issue.ID)
			continue
		}
		// **候補の Status が active_states に入っていることを自分で確かめる。**
		// 一覧は GitHub のサーバ側の検索結果であり、直前に書いた値が索引へ反映される
		// 前に取り直すと、`Blocked` にした issue がそのまま返ってくる（設計 3-34）。
		// **候補に載っていること自体は「いま着手してよい」の根拠にならない。**
		if !containsFold(o.cfg.Tracker.ActiveStates, issue.State) {
			o.logger.Warn("頼んだ Status に無い候補が返ったので飛ばします（絞り込みの反映待ちの可能性があります）",
				"identifier", issue.Identifier, "返ってきた Status", issue.State,
				"頼んだ Status", strings.Join(o.cfg.Tracker.ActiveStates, ", "))
			// **ここも関門より前である**（設計 6-1）。人間が Status を動かした直後は
			// この分岐へ落ち続けるので、消さないと「担当者を全部外してください」という
			// **いまは効かない直し方**をダッシュボードが出し続ける。
			// **案内を書いた事実は `clearGate` が残す**ので、数え直しで2件目が書かれることはない。
			o.clearGate(issue.ID)
			continue
		}
		// 同じ理由で失敗し続けている issue は、人間が Status を動かすまで拾わない。
		if o.skipByFailure(issue) {
			o.clearGate(issue.ID)
			continue
		}
		if !issue.Dispatchable {
			// **ここで捨てると preflight を通らないので、直し方が人間へ届かない**（設計 3-33）。
			// preflight が自分で信頼を検査し、未信頼なら issue へコメントを1回だけ残す。
			// **draft issue は owner も repo も持たない**ので、信頼の検査には掛けない。
			// **通知を出したあとは検査し直さない。**呼ぶたびに git を1プロセス起こす。
			// 信頼が付けば Dispatchable が真になってここへ来なくなるので、取りこぼさない。
			if issue.Owner != "" && !o.alreadyNotified(issue.Owner, issue.Repo) {
				o.preflight(ctx, issue)
			}
			o.clearGate(issue.ID)
			continue
		}
		if missing := missingRequiredLabels(issue, o.cfg.Tracker.RequiredLabels); len(missing) > 0 {
			// **黙って飛ばさない**（issue #134）。ここは v0.1.10 まで1行も出さなかった。
			// **設定した本人でも、どの issue がどのラベル待ちなのかを知る手立てが無かった。**
			//
			// **Debug ではなく Info にする。**Status が着手待ちのまま動かないので、
			// 人間から見ると「止まっている」ようにしか見えない。
			//
			// **1つの issue につき1回だけ出す**（`noteUntrusted` と同じ形）。
			// `required_labels` は大量の対象外を除けるための道具なので、
			// **無制限に出すと、この節が読ませたい残り2つの行が流れて埋まる。**
			// **印は「issue と、足りているラベルの組み合わせ」で持つ。**
			// issue の ID だけで持つと、1つ足した人が「まだ足りない」を知る手立てを失う。
			joined := strings.Join(missing, ", ")
			if o.noteLabelSkip(issue.ID + "\x00" + joined) {
				o.logger.Info("必須のラベルが揃っていないので飛ばします（この組み合わせでは1回だけ出します）",
					"identifier", issue.Identifier,
					"足りないラベル", joined,
					"required_labels", strings.Join(normalizedLabels(o.cfg.Tracker.RequiredLabels), ", "),
					"この issue のラベル", strings.Join(issue.Labels, ", "))
			} else {
				o.logger.Debug("必須のラベルが揃っていないので飛ばしました（通知は済んでいます）",
					"identifier", issue.Identifier, "足りないラベル", joined)
			}
			o.clearGate(issue.ID)
			continue
		}

		// 段-1: 空きスロットを数える。**印を付ける前に行う**（付けてから弾くと印が残る）。
		if free, blocker, limit := o.freeSlotBlocker(); !free {
			// **INFO のままにする**（issue #134。上の dispatchPaused と同じ理由）。
			// **同時に動かす数の上限に達しただけで、異常ではない。**
			//
			// **どちらの上限で止まったかを名乗る。**上限は2つあり、
			// 効かないほうを上げても何も変わらない。
			o.logger.Info("空きスロットが尽きたので、この巡回ではこれ以上 dispatch しません。"+
				"走っているものが終われば順に着手します。同時に動かす数を増やすには "+
				blocker+" を上げてください",
				"上限に達した設定", blocker,
				"その上限", limit,
				"ここで打ち切った issue", issue.Identifier)
			break
		}

		// 段0: dispatch の直前の検査（設計 3-6 の「issue ごと」の表）。
		// **持ち回りの判定より先に行う**（設計 3-77b）。**担当者を書いてから落とすと、
		// 着手しなかった issue に担当者と hold だけが残り、ほかの機械は
		// `idle_timeout_ms`（既定18時間）触らない。**この機械では信頼していないが
		// 別の機械では信頼しているリポジトリが、そのあいだ塞がる。
		if !o.preflight(ctx, issue) {
			o.clearGate(issue.ID)
			continue
		}

		// 段-0: 担当の持ち回りを決める（設計 3-77）。**空きスロットを数えたあとに行う。**
		// 入札に勝つのは「いま着手できる機械」でなければならず、
		// **枠が空いていない機械が勝つと、issue は誰にも着手されないまま止まる。**
		decision := o.handoffGate(ctx, issue)
		if decision.stop {
			// **コメントを読む枠を使い切った。**候補はボードの並び順で来るので、
			// 上から順に見ることは保たれる。**続きは次の巡回で見る。**
			break
		}
		if !decision.proceed {
			continue
		}

		rs, ok := o.claimForDispatch(ctx, issue)
		if !ok {
			if decision.acquired {
				// **この巡回で書いた担当者を消し戻す**（設計 3-77c）。
				o.undoHandoffAcquire(ctx, issue)
			}
			continue
		}
		// **担当者になった直後は、確かめ直しの時計を進めておく**（設計 3-77c）。
		// 進めないと、最初の turn の終わりで必ず issue を1件取り直すことになる。
		rs.markHandoffChecked(o.now())
		rs.setHandoffAcquired(decision.acquired)
		claimed = append(claimed, claimedRun{rs: rs, issue: issue})
	}
	if len(claimed) == 0 {
		return
	}

	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		for _, c := range claimed {
			if ctx.Err() != nil {
				return
			}
			o.runStartOrFail(ctx, c.rs, c.issue, false)
		}
	}()
}

// claimedRun は印を付け終えて、着手の段2以降を待っている run である。
type claimedRun struct {
	// rs は印を付けた run である。
	rs *runState
	// issue は着手する issue のスナップショットである。
	issue tracker.Issue
}

// hasFreeSlot は空きスロットがあるかを返す（設計 3-16 の段-1）。
//
//	available_slots = max(agent.max_concurrent_agents - 実行中の一覧の件数, 0)
//
// **数えるのは印の集合の全件である**（設計 3-25）。走行中の run も、バックオフ待ちの run も、
// 枠待ちの run も数える（いずれも worktree か pane を掴んだままである）。
//
// **状態ごとの上限（`agent.max_concurrent_agents_by_state`）も同じ式で評価する。**
// **これから dispatch する候補は `tracker.running_state`（既定 In Progress）の枠を
// 消費するものとして数える**（候補は取得した時点ではまだ Ready だが、dispatch すれば
// 段2 で running_state へ書く。Ready のバケツで数えると上限を越えて dispatch できてしまう）。
//
// 戻り値: 空きがあれば true。
func (o *Orchestrator) hasFreeSlot() bool {
	free, _, _ := o.freeSlotBlocker()
	return free
}

// freeSlotBlocker は空きがあるかを返し、無いときは**どちらの上限で止まったか**を添える
// （issue #134）。
//
// **上限は2つある。**全体（`agent.max_concurrent_agents`）と、Status ごと
// （`agent.max_concurrent_agents_by_state`）である。
// **どちらで止まったかを言わないと、人間は効かないほうの設定を上げる。**
//
// 戻り値の1つ目: 空きがあるか。
// 戻り値の2つ目: 空きが無いときに上限へ達した設定のキー名（空きがあるときは空文字）。
// 戻り値の3つ目: そのキーに設定されている値（空きがあるときは0）。
func (o *Orchestrator) freeSlotBlocker() (bool, string, int) {
	runs := o.snapshotRuns()
	if len(runs) >= o.cfg.Agent.MaxConcurrentAgents {
		return false, "agent.max_concurrent_agents", o.cfg.Agent.MaxConcurrentAgents
	}

	limits := o.cfg.Agent.MaxConcurrentAgentsByState
	if len(limits) == 0 {
		return true, "", 0
	}
	limit, ok := lookupFolded(limits, o.cfg.Tracker.RunningState)
	if !ok {
		// 該当するキーが無ければ、その Status には全体の上限だけを適用する。
		return true, "", 0
	}

	inRunningState := 0
	for _, rs := range runs {
		if strings.EqualFold(strings.TrimSpace(rs.snapshot().State), strings.TrimSpace(o.cfg.Tracker.RunningState)) {
			inRunningState++
		}
	}
	if inRunningState >= limit {
		return false, "agent.max_concurrent_agents_by_state", limit
	}
	return true, "", 0
}

// lookupFolded は Status 名をキーにした写像を、大文字小文字を無視して引く（設計 3-13）。
//
// m: 引く写像。
// key: 引く Status 名。
// 戻り値の1つ目: 見つかった値。
// 戻り値の2つ目: 見つかれば true。
func lookupFolded(m map[string]int, key string) (int, bool) {
	target := strings.TrimSpace(key)
	for k, v := range m {
		if strings.EqualFold(strings.TrimSpace(k), target) {
			return v, true
		}
	}
	return 0, false
}

// hasRequiredLabels は `tracker.required_labels` をすべて持っているかを返す。
//
// **ラベルはアダプタが正規化済みである**（前後の空白を落として小文字。設計 3-13）。
//
// issue: 判定する issue。
// required: 必須ラベルの一覧。空なら制約なし。
// 戻り値: すべて持っていれば true。
func hasRequiredLabels(issue tracker.Issue, required []string) bool {
	return len(missingRequiredLabels(issue, required)) == 0
}

// missingRequiredLabels は、必須のラベルのうち**足りないものを全部**返す（issue #134）。
//
// **名前を添える理由。**2つの一覧（required_labels と、その issue のラベル）を
// 並べるだけだと、差分は人が目で取ることになる。
//
// **最初の1つだけ返してはならない。**この関数の結果は「1つの issue につき1回だけ」
// 出すログに載る。1つ目だけを出すと、それを付けた人が
// **「言われたラベルを付けたのに、まだ動かず、ログに何も出ない」**という状態に落ちる。
//
// issue: 検査する issue。
// required: 設定の `tracker.required_labels`。
// 戻り値: 足りないラベル（**照合に使う正規化済みの形**。揃っていれば長さ0）。
func missingRequiredLabels(issue tracker.Issue, required []string) []string {
	var missing []string
	for _, want := range required {
		w := strings.ToLower(strings.TrimSpace(want))
		if w == "" {
			continue
		}
		found := false
		for _, got := range issue.Labels {
			if got == w {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, w)
		}
	}
	return missing
}

// normalizedLabels は、設定に書かれたラベルを**照合に使う形**（小文字・前後の空白なし）へ揃える。
//
// **ログに出すために要る。**issue 側のラベルは取り込みのときに小文字化されているので
// （`internal/tracker/query.go` の `normalizeLabels`）、設定側を生のまま並べると
// **照合は通っているのにログだけが不一致に見え、原因を取り違える。**
//
// labels: 設定に書かれたラベル。
// 戻り値: 正規化したもの（空の要素は落とす）。
func normalizedLabels(labels []string) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if w := strings.ToLower(strings.TrimSpace(l)); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// dispatchOne は1件の issue に着手する（設計 3-16 の13段を、その順で実行する）。
//
//	段-1 空きスロットを数える            … 呼び出し側（dispatchCandidates）が済ませている
//	段0  dispatch 直前の検査             … 信頼済みか / 置き場所が設定の内側か
//	段1  「自分が取った」印を付ける        ← メモリの上での最初の段
//	段2  Status を tracker.running_state へ書く   ← 外部に残る最初の段
//	段3  worktree を用意し、herdr workspace として開く
//	段4  workspace_hooks.after_create（新しく作ったときだけ）
//	段5  Claude Code の設定ファイルを worktree の外に作る
//	段6  worktree の中に身元ファイルを書く
//	段7  workspace_hooks.before_run
//	段8  worktree.open が開いた workspace の pane を pane.list で引き、label を書く
//	段9  agent.start で Claude Code を起動する
//	段10 agent_status が idle か done であることを確かめる
//	段11 1回目の turn を送る（run ごとの goroutine で）
//
// **段0 を段2 より前に置く。**Status を書いてから検査に落ちて飛ばすと、`In Progress` は
// active_states なので毎巡回で候補に上がり続け、30秒ごとにコメントが積まれる。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 着手する issue。
func (o *Orchestrator) claimForDispatch(ctx context.Context, issue tracker.Issue) (*runState, bool) {
	// **段0（dispatch の直前の検査）は呼び出し側が済ませている。**
	// **持ち回りの判定より先に通しておく必要がある**（設計 3-77b）。ここで呼ぶと、
	// 担当者を書いたあとで落ちることになり、着手しなかった issue が18時間塞がる。

	// 段1: 印を付け、実行中の一覧へ入れる。
	rs, ok := o.claim(issue.ID, issue)
	if !ok {
		return nil, false
	}
	// **状態ごとの上限は running_state のバケツで数える**（設計 3-16 の段-1）。
	// **印を付けた時点で書き換える。**段2 は別の goroutine で走るので、そこまで待つと
	// 同じ巡回の次の候補を数えるときに dispatch 前の Status（Ready）のまま数えてしまい、
	// 上限を越えて dispatch できてしまう。
	rs.setIssueState(o.cfg.Tracker.RunningState)
	return rs, true
}

// runStartOrFail は着手の段2〜11 を実行し、失敗したらその run を失敗として扱う。
//
// **巡回のループから同期で呼んではならない**（設計 3-8）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 印を付けた run。
// issue: 着手する issue。
// reuse: 再 dispatch かどうか。
func (o *Orchestrator) runStartOrFail(ctx context.Context, rs *runState, issue tracker.Issue, reuse bool) {
	err := o.startRun(ctx, rs, issue, reuse)
	if err == nil {
		return
	}
	label := "着手"
	if reuse {
		label = "再 dispatch"
	}
	// **Status を書かなかったときは、人間へ渡さず静かに印を外す。**
	// ボードは continuo が触る前の状態のままなので、伝えるべきことが1つも無い。
	// **ここで failure_state へ落とすと、人間が Blocked に置いた issue を上書きする。**
	if errors.Is(err, ErrStatusNotWritten) {
		o.logger.Info(label+"を取りやめました（カンバンの Status を書かなかったため）",
			"identifier", issue.Identifier, "理由", summaryLine(err.Error()))
		// **書き戻しが飛んでいたら、終わるまで待ってから印を取る**（設計 3-56）。
		// 待たずに戻ると、着手を取りやめた run の印が外れないまま残る。
		if rs.claimTerminal(ctx) {
			o.stopWorker(ctx, rs)
			o.release(rs)
		}
		// **この着手で書いた担当者を消し戻す**（設計 3-77c）。ボードは continuo が
		// 触る前の状態のままなので、issue の担当者も元へ戻す。**残すと、着手しなかった
		// issue をほかの機械が18時間触らない。**
		if rs.handoffAcquired() {
			o.undoHandoffAcquire(ctx, issue)
		}
		return
	}
	// **待てば通るかもしれない失敗は、人間へ渡さずバックオフして試し直す**（設計 3-16 の段10）。
	// **リトライを使い切ったら abandonRun が自分で failure_state へ落とす**ので、
	// 直らない失敗が永久に回り続けることはない。
	if errors.Is(err, ErrStartupRetryable) {
		o.logger.Warn(label+"に失敗しました（待って試し直します）", "identifier", issue.Identifier, "error", err)
		o.abandonRun(ctx, rs, fmt.Sprintf("%sに失敗しました: %v", label, err))
		return
	}
	o.logger.Warn(label+"に失敗しました", "identifier", issue.Identifier, "error", err)
	o.failRun(ctx, rs, fmt.Sprintf("%sに失敗しました: %v", label, err))
}

// redispatch はバックオフが明けた run を再 dispatch する（設計 3-25 の「明けたらどう再開するか」）。
//
//	段0 から入り直す（検査をやり直す。信頼が外れているかもしれない）
//	段1 は飛ばす（既に印がある）
//	段2 の Status の書き込みは行う（取り直して terminal_states でなければ書く）
//	段3 の worktree は再利用する（身元ファイルの takeover_count を1つ増やす）
//	段5 の設定ファイルは作り直す（socket のパスが変わっているかもしれない）
//	**セッションは身元ファイルの UUID へ `--resume` で復帰する**（設計 3-3b）
//	RetryCount はそのまま。BackoffUntil はゼロ値へ戻す
//
// **送るのは1回目の本文（5-3）である。**復帰して会話履歴があっても変えない。
// 継続の指示（5-4）には「issue を読むこと」「紐づく PR も読むこと」が無いので、
// **差し戻しで新しく付いたレビューを読まないまま進むことになる**（設計 3-3b）。
// **`.attempt` には `RetryCount + 1` が入る**（5-3 の「この作業は n 回目の試行です」）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 再 dispatch する run。
func (o *Orchestrator) redispatch(ctx context.Context, rs *runState) {
	issue := rs.issue()
	if !o.preflight(ctx, issue) {
		// 検査に落ちたら、この巡回では何もしない。次の巡回でまた見る。
		return
	}
	// **印は同期で更新する。**次の巡回が同じ run をもう一度拾わないようにする。
	rs.clearBackoff()
	// **段2以降は別の goroutine で回す**（設計 3-8。巡回のループをブロックしない）。
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		o.runStartOrFail(ctx, rs, issue, true)
	}()
}

// preflight は dispatch の直前の検査を行う（着手の段0。設計 3-6 の「issue ごと」の表）。
//
// **ここで失敗しても continuo は止まらない。**その issue だけ飛ばす。
//
//	対象リポジトリが Claude Code に信頼登録されているか  → 飛ばす（trust.on_untrusted に従う）
//	worktree の置き場所が設定の内側に収まるか            → その issue を失敗として扱う（3-20）
//	目的のパスの worktree をそのまま使えるか            → 飛ばす（3-16b。判断は 3-22 の段2・段3 と同じ）
//	その branch を別の場所の worktree が使っていないか  → 飛ばす（3-16b。目的のパスが空でも落ちる）
//
// **後ろ2つをここで見るのは、Status を書く前に落とすためである。**着手が確定して
// 失敗する issue でも段2 で `In Progress` を書いてしまうと、`In Progress` は
// active_states なので次の巡回でまた候補に上がり、`In Progress` と `Blocked` の
// 往復が永久に続く。**この検査は1バイトも書かない**（CheckWorktreeUsable が両方を見る）。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 検査する issue。
// 戻り値: 検査を通ったら true。
func (o *Orchestrator) preflight(ctx context.Context, issue tracker.Issue) bool {
	if o.cfg.Trust.RequireRepoTrusted {
		trusted, reason, err := o.ws.CheckTrust(issue.Owner, issue.Repo)
		if err != nil {
			o.logger.Warn("リポジトリの信頼を検査できません（この issue を飛ばします）",
				"identifier", issue.Identifier, "error", err)
			return false
		}
		if !trusted {
			o.noteUntrusted(ctx, issue, reason)
			return false
		}
		o.clearUntrusted(issue.Owner, issue.Repo)
	}

	loc, warnings, err := workspace.Locate(o.ws.ResolvedRoot(), o.cfg.Herdr.Worktree.BranchTemplate, toIssueRef(issue))
	if err != nil {
		o.logger.Warn("worktree の置き場所を決められません（この issue を飛ばします）",
			"identifier", issue.Identifier, "error", err)
		return false
	}
	for _, w := range warnings {
		o.logger.Warn("識別子の正規化で情報が落ちました", "identifier", issue.Identifier, "message", w.Message)
	}
	if err := workspace.CheckContainment(o.ws.ResolvedRoot(), loc.Path); err != nil {
		o.logger.Warn("worktree の置き場所が設定の内側に収まりません（この issue を飛ばします）",
			"identifier", issue.Identifier, "error", err)
		return false
	}
	if err := o.ws.CheckWorktreeUsable(ctx, toIssueRef(issue)); err != nil {
		// **Status を1バイトも書かずに飛ばす。**直し方はエラー文が持っている。
		o.logger.Warn("目的の worktree をそのまま使えません（Status を書かずにこの issue を飛ばします）",
			"identifier", issue.Identifier, "error", err)
		return false
	}
	return true
}

// noteUntrusted は未信頼のリポジトリを人間へ知らせる（設計 3-6）。
//
// **issue へのコメントは、そのリポジトリにつき1回だけである。**キーは `<owner>/<repo>` で
// あり、issue ごとではない。未信頼の issue は `Ready` のまま候補に残り続けるので、
// 素朴に実装すると30秒ごとに永久にコメントが積まれる。
//
// **draft issue はノード ID を持たないので、コメントせずログだけにする。**
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: そのリポジトリで最初に候補に上がった issue。
// reason: 信頼の検査が返した理由。
func (o *Orchestrator) noteUntrusted(ctx context.Context, issue tracker.Issue, reason string) {
	key := issue.Owner + "/" + issue.Repo

	o.mu.Lock()
	_, already := o.notified[key]
	if !already {
		o.notified[key] = o.now()
	}
	o.mu.Unlock()

	if already {
		o.logger.Debug("未信頼のリポジトリの issue を飛ばしました（通知は済んでいます）",
			"identifier", issue.Identifier, "repo", key)
		return
	}

	o.logger.Warn("リポジトリが Claude Code に信頼登録されていません（この issue を飛ばします）",
		"identifier", issue.Identifier, "repo", key, "理由", summaryLine(reason))

	if o.cfg.Trust.OnUntrusted != "skip_and_comment" {
		return
	}
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		o.logger.Warn("draft issue にはコメントできないのでログだけにします", "identifier", issue.Identifier)
		return
	}
	if err := o.postComment(ctx, nodeID,
		buildUntrustedComment(issue.Owner, issue.Repo, reason)); err != nil {
		o.logger.Warn("未信頼のリポジトリの通知を投稿できませんでした",
			"identifier", issue.Identifier, "error", err)
	}
}

// clearUntrusted は信頼が付いたリポジトリの記録を消す（設計 3-6）。
//
// **また外れたときにもう一度知らせるためである。**
//
// owner: リポジトリの所有者名。
// repo: リポジトリ名。
// alreadyNotified は、そのリポジトリへ未信頼の通知を既に出したかを返す。
//
// **巡回のたびに信頼を検査し直すのを避けるために使う。**`CheckTrust` は
// `git rev-parse` を1プロセス起こし、`~/.claude.json` を読む。どちらもキャッシュを通らない。
//
// owner: リポジトリの所有者名。
// repo: リポジトリ名。
// 戻り値: 既に通知を出していれば真。
func (o *Orchestrator) alreadyNotified(owner, repo string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, ok := o.notified[owner+"/"+repo]
	return ok
}

// noteLabelSkip は、必須のラベルが足りないことを「まだ知らせていない」かを返し、
// 初回なら印を付ける（issue #134）。
//
// **`required_labels` は大量の対象外を除けるための道具である。**
// 巡回のたびに INFO を出すと、同じ節が読ませたい残り2つの行が流れて埋まる。
// **`alreadyNotified` と同じ考え方で、1回だけにする。**
//
// **鍵は issue の ID だけにしない。**足りないラベルの並びも混ぜる。
// **issue の ID だけで持つと、1つ足した人が「まだ足りない」を知る手立てを失う。**
//
// **消す仕組みは持たない。**足りないラベルが変わるたびに新しい鍵になり、そのとき1回出る。
// 全部揃えば候補に入り、この分岐そのものを通らなくなる。
//
// key: project item の ID と、足りないラベルの並びを繋いだもの。
// 戻り値: 初回なら真。既に知らせていれば偽。
func (o *Orchestrator) noteLabelSkip(key string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.labelSkipped[key]; ok {
		return false
	}
	if o.labelSkipped == nil {
		o.labelSkipped = make(map[string]struct{})
	}
	o.labelSkipped[key] = struct{}{}
	return true
}

func (o *Orchestrator) clearUntrusted(owner, repo string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.notified, owner+"/"+repo)
}

// startRun は着手の段2〜11 を実行する（設計 3-16）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 印を付けた run。
// issue: 着手する issue。
// reuse: 再 dispatch かどうか（真なら身元ファイルの takeover_count を1つ増やす）。
// 戻り値: いずれかの段で失敗した場合のエラー。
func (o *Orchestrator) startRun(ctx context.Context, rs *runState, issue tracker.Issue, reuse bool) error {
	// 段2: ボードの Status を tracker.running_state へ書く。**ハードコードしない。**
	//
	// **書く前に ID 指定で取り直し、`active_states` にあるときだけ書く**（許可リスト）。
	// 拒否リストだけでは `In Review` のような「設定に名前が出てこない状態」を守れず、
	// 人間へ引き渡し済みの issue を上書きしてしまう。
	if !o.dispatchStatusAllowed(ctx, issue.ID, issue.Identifier) {
		return ErrStatusNotWritten
	}
	// **拒否リストも渡し続ける。**UpdateStatus はこのあともう一度 ID 指定で取り直すので、
	// 上の取り直しとの隙間に人間が動かした場合は、そちらが最後の砦になる。
	moved, err := o.tracker.UpdateStatus(ctx, issue.ID, o.cfg.Tracker.RunningState, o.dispatchBlockedStates())
	if err != nil {
		return i18n.Errorf(i18n.KeyOrchestratorStartRunStatusUpdateFailed, o.cfg.Tracker.RunningState, err)
	}
	if !moved.Reached {
		// **書かなかったのに段3 へ進んではならない。**item がもう見えないか、
		// 取り直した結果 terminal_states / failure_state に入っていたということである。
		// どちらも「いま着手してはいけない」を意味する。
		return ErrStatusNotWritten
	}
	// **動かしたなら、その記録を issue に残す**（設計 3-29）。
	// **既に running_state だった場合は書き込みが起きないので、コメントも出ない。**
	o.postStatusMove(ctx, issue.Identifier, issueNodeID(issue),
		newStatusMove(moved, o.cfg.Tracker.RunningState),
		"この issue に着手し、Claude Code の pane を立てたためです")
	// **段-1 の状態ごとの上限は running_state のバケツで数える**（設計 3-16）。
	// 手元のスナップショットを書き換えておかないと、次の巡回で取り直すまで
	// dispatch 前の Status（Ready）のまま数えてしまう。
	rs.setIssueState(o.cfg.Tracker.RunningState)
	// **書けたことを控える**（設計 3-50）。知らない Status になったときに
	// 「元は何だったか」を書くために要る。
	rs.setLastWrittenState(o.cfg.Tracker.RunningState)

	// 段3: worktree を用意し、herdr workspace として開く。
	prepared, err := o.ws.Prepare(ctx, toIssueRef(issue))
	if err != nil {
		failed := i18n.Errorf(i18n.KeyOrchestratorStartRunWorktreePrepareFailed, err)
		// **workspace が「待てば通るかもしれない」と言った失敗は、人間へ渡さない**
		// （設計 3-22d）。リンクされた branch の fetch が回線で落ちただけの issue を
		// `failure_state` へ置くと、`tracker.active_states` から外れるので
		// **人間がカンバンで戻すまで continuo は二度と拾わない。**
		// **やり直しは `agent.max_retries` で頭打ちになる**ので、
		// 直らない失敗が永久に回り続けることはない（abandonRun）。
		if errors.Is(err, workspace.ErrRetryable) {
			return fmt.Errorf("%w: %w", ErrStartupRetryable, failed)
		}
		return failed
	}
	rs.setWorkspaceInfo(prepared.Path, prepared.Base, prepared.HerdrWorkspaceID)

	// 段4: worktree を新しく作ったときだけ after_create を走らせる（仕様 5.3.4）。
	if prepared.Created {
		if err := o.ws.RunHook(ctx, workspace.HookAfterCreate, prepared.Path); err != nil {
			return i18n.Errorf(i18n.KeyOrchestratorStartRunAfterCreateHookFailed, err)
		}
	}

	// 段5: Claude Code の設定ファイルを worktree の外に作る（再 dispatch でも作り直す）。
	settingsPath, err := o.writeSettingsFile(issue)
	if err != nil {
		return err
	}
	rs.setSettingsPath(settingsPath)

	// 段5b: どのセッションで起動するかを決める（設計 3-3b）。
	//
	// **既存の worktree を再利用していて、身元ファイルにセッション UUID があるなら、
	// そのセッションへ `--resume` で復帰する。**前回の会話が残るので、エージェントは
	// 「前回どこまでやったか」を自分で分かったうえで続きに入れる。
	// **新規の着手は新しく採番する。**一度使った UUID を `--session-id` に渡すと
	// `Session ID ... is already in use.` で起動に失敗する（設計 3-3）。
	//
	// **`--resume` は session_id を変えない**（実測: 2026-08-26。復帰の前後で hook が
	// 名乗る `session_id` と `transcript_path` が一致した）。**だから復帰した run も、
	// この UUID のままで hook を引ける。**
	resumeUUID := ""
	if prepared.ExistingIdentity != nil {
		resumeUUID = prepared.ExistingIdentity.SessionUUID
	}
	sessionUUID := resumeUUID
	if resumeUUID == "" {
		sessionUUID, err = o.newSessionUUID()
		if err != nil {
			return err
		}
	}
	rs.setSessionUUID(sessionUUID)
	o.bindSession(rs, sessionUUID)
	// **worker の世代を1つ進め、次の turn で1回目の本文（5-3）を送るようにする。**
	// **復帰した場合もそうする**（設計 3-3b）。継続の指示（5-4）には
	// 「issue を読むこと」「紐づく PR も読むこと」が入っていないので、それだけを送ると
	// **`In Review` から差し戻された issue で、新しく付いたレビューを読まないまま進む。**
	//
	// **復帰した場合はトークンの集計の基準を作り直さない。**transcript のファイルが
	// 同じままなので、作り直すと同じファイルを2回数える（設計 3-15）。
	rs.beginAttempt(resumeUUID != "")
	if resumeUUID != "" {
		o.logger.Info("前回のセッションに復帰して再着手します（会話履歴を引き継ぎます）",
			"identifier", issue.Identifier, "session_uuid", resumeUUID, "worktree", prepared.Path)
	} else {
		o.logger.Info("新しいセッションを立てて着手します（会話履歴はありません）",
			"identifier", issue.Identifier, "session_uuid", sessionUUID, "worktree", prepared.Path)
	}

	// 段6: worktree の中に身元ファイルを書く。ここまで来れば、落ちても身元が分かる。
	identity := workspace.Identity{
		IssueURL:         issueURL(issue),
		IssueIdentifier:  issue.Identifier,
		ProjectItemID:    issue.ID,
		Branch:           prepared.Branch.String(),
		Base:             prepared.Base.String(),
		HerdrWorkspaceID: prepared.HerdrWorkspaceID,
		// **continuo が開かせたリポジトリの親 workspace を控える**（issue #19）。
		// ここに書かないと、片付けが閉じる相手を知らないまま終わる。
		HerdrRepoWorkspaceID: prepared.HerdrRepoWorkspaceID,
		SocketPath:           o.socketPath,
		SettingsPath:         settingsPath,
		SessionUUID:          sessionUUID,
		CreatedAt:            o.now(),
	}
	identity = workspace.MergeForReuse(identity, prepared.ExistingIdentity)
	if err := o.ws.WriteIdentity(ctx, prepared.Path, identity); err != nil {
		return i18n.Errorf(i18n.KeyOrchestratorStartRunIdentityWriteFailed, err)
	}
	if reuse {
		// 再 dispatch のときも引き継いだ回数を数える（設計 3-18）。
		if _, err := o.ws.IncrementTakeover(ctx, prepared.Path); err != nil {
			o.logger.Warn("引き継いだ回数を増やせませんでした", "identifier", issue.Identifier, "error", err)
		}
	}

	// 段7: before_run（失敗したら致命）。
	if err := o.ws.RunHook(ctx, workspace.HookBeforeRun, prepared.Path); err != nil {
		return i18n.Errorf(i18n.KeyOrchestratorStartRunBeforeRunHookFailed, err)
	}

	// 段8: worktree.open が開いた workspace の pane を pane.list で引く。
	// **pane を新しく作らない**（pane.split も tab.create も呼ばない。設計 4-5）。
	paneID, err := o.resolvePane(ctx, prepared)
	if err != nil {
		return err
	}
	rs.setPaneID(paneID)
	// **label は `owner/repo/issues/N` の形である**（設計 3-3）。
	// 組み立ては herdr.IssueLabel に寄せてある（workspace 側と形がずれないため）。
	if _, err := o.herdr.PaneRename(ctx, herdr.PaneRenameParams{
		PaneID: paneID,
		Label:  herdr.IssueLabel(issue.Owner, issue.Repo, issue.Number),
	}); err != nil {
		return i18n.Errorf(i18n.KeyOrchestratorStartRunPaneRenameFailed, err)
	}

	// 段9: その pane で Claude Code を起動する。
	// **環境変数は設定ファイルの env に書く。**pane にも agent.start にも渡さない（設計 3-12）。
	agentName, err := o.resolveAgentName(ctx, issue.Repo, issue.Number)
	if err != nil {
		return err
	}
	startErr := o.launchClaude(ctx, rs, prepared.Path, herdr.AgentStartParams{
		Name:   agentName,
		Kind:   o.cfg.Claude.Kind,
		PaneID: paneID,
		Args:   o.claudeStartArgs(settingsPath, sessionUUID, resumeUUID),
	})
	if startErr != nil && resumeUUID != "" {
		// **復帰に失敗した。新しいセッションで始め直す**（設計 3-3b）。
		//
		// **身元ファイルの UUID のセッションは消えていることがある。**
		// `~/.claude/projects/` の中身は利用者が消せるためである。実測（2026-08-26）:
		//
		//	claude --resume <無い UUID>
		//	→ 終了コード 1、標準エラーに `No conversation found with session ID: <UUID>`
		//	→ herdr 経由だと agent.start が `timeout: timed out waiting for agent startup` を返し、
		//	  pane はシェルのプロンプトへ戻る（同じ pane で、そのまま起動し直せる）
		//
		// **ここで諦めてはならない。**利用者が履歴を消しただけで issue が
		// `failure_state` へ落ちることになる。
		if newErr := o.restartWithNewSession(ctx, rs, prepared.Path, settingsPath, issue, resumeUUID, startErr); newErr != nil {
			return newErr
		}
		startErr = o.launchClaude(ctx, rs, prepared.Path, herdr.AgentStartParams{
			Name:   agentName,
			Kind:   o.cfg.Claude.Kind,
			PaneID: paneID,
			Args:   o.claudeStartArgs(settingsPath, rs.sessionUUID(), ""),
		})
	}
	if startErr != nil {
		return startErr
	}

	// 段11: 1回目の turn を送る。**巡回のループはここでブロックしない。**
	if !o.startTurnLoop(ctx, rs, false) {
		// **黙って捨ててはならない。**古い turn ループが `agent.prompt` の待ち受けから
		// 戻っていないと2本目は立てられない。印を立てて次の巡回で起こし直す（設計 3-8）。
		o.logger.Warn("前の turn ループがまだ走っているので、次の巡回で turn を送り直します",
			"identifier", issue.Identifier)
		rs.setNeedsPrompt()
	}
	return nil
}

// launchClaude は着手の段9（`agent.start`）と段10（起動の確認）を1回通す（設計 3-16）。
//
// **環境変数は設定ファイルの env に書く。**pane にも `agent.start` にも渡さない（設計 3-12）。
//
// **段10 で通らなければ `agent.start` からやり直す**（2026-08-21 に E2E で確定）。
// **herdr が pane を「使える」と判断しても、シェルのプロンプトが出ていないことがある。**
// そのとき `agent.start` はエラーを返さずに登録だけを済ませ、Claude Code は起動しない。
// **あとから `agent.prompt` を投げると `agent_not_ready` になる**ので、ここで見切る。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 起動する run。
// worktreePath: 身元ファイルへ agent 名を書き足す先。
// params: `agent.start` の引数。
// 戻り値: 起動できなかった場合のエラー。
func (o *Orchestrator) launchClaude(
	ctx context.Context, rs *runState, worktreePath string, params herdr.AgentStartParams,
) error {
	started, err := o.herdr.AgentStartWithRetry(ctx, params, agentStartBusyBudget, agentStartRetryDelay)
	if err != nil {
		return i18n.Errorf(i18n.KeyOrchestratorStartRunAgentStartFailed, err)
	}
	rs.setAgentName(params.Name)
	// **起動直後の画面の版を stall の判定の種にする**（設計 3-21）。種を入れないと、
	// 最初の判定が必ず「版が変わった」になり、打ち切りまでに
	// `claude.turn_timeout_ms` を2回またぐことになる。
	rs.noteRevision(started.Agent.Revision, o.now())
	if err := o.ws.SetAgentName(ctx, worktreePath, params.Name.String()); err != nil {
		o.logger.Warn("身元ファイルへ agent 名を書けませんでした",
			"identifier", rs.issue().Identifier, "error", err)
	}
	return o.confirmStartupWithRestart(ctx, rs, params)
}

// restartWithNewSession は、復帰に失敗した run を新しいセッションで始め直せる状態にする
// （設計 3-3b）。
//
// **`agent.start` はここでは呼ばない。**呼び出し側が新しい起動フラグで `launchClaude` を
// もう一度通す。ここでやるのは次の4つである。
//
//  1. セッション UUID を採り直し、hook の索引を張り替える
//  2. トークンの集計の基準を作り直す（新しいセッションの transcript は別のファイルである）
//  3. 身元ファイルの `session_uuid` を書き直す
//     → 書き直さないと、**次の再着手も同じ死んだ UUID へ復帰しにいく**
//  4. 何が起きたかをログに残す
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// worktreePath: 身元ファイルのある worktree の絶対パス。
// settingsPath: issue ごとの設定ファイルの絶対パス（ログに出す）。
// issue: 対象の issue。
// deadUUID: 復帰しようとして失敗したセッションの UUID。
// cause: 復帰に失敗した理由。
// 戻り値: UUID を採り直せなかった場合のエラー。
func (o *Orchestrator) restartWithNewSession(
	ctx context.Context, rs *runState, worktreePath, settingsPath string,
	issue tracker.Issue, deadUUID string, cause error,
) error {
	fresh, err := o.newSessionUUID()
	if err != nil {
		return err
	}
	rs.setSessionUUID(fresh)
	o.bindSession(rs, fresh)
	// **ここで初めて transcript のファイルが別物になる**（設計 3-15）。
	rs.foldTokensBase()
	if err := o.ws.SetSessionUUID(ctx, worktreePath, fresh); err != nil {
		o.logger.Warn("身元ファイルのセッション UUID を書き直せませんでした（次の再着手もまた復帰を試みます）",
			"identifier", issue.Identifier, "session_uuid", fresh, "error", err)
	}
	o.logger.Warn("前回のセッションへ復帰できなかったので、新しいセッションで始め直します",
		"identifier", issue.Identifier,
		"復帰しようとしたセッション", deadUUID,
		"新しいセッション", fresh,
		"設定ファイル", settingsPath,
		"error", cause)
	return nil
}

// resolvePane は worktree.open が作った workspace の中の pane を引く（着手の段8。設計 3-16）。
//
// **返る pane が1つでなければ、その issue を失敗として扱う**（人間が触った workspace かもしれない）。
//
// ctx: 呼び出しに適用するコンテキスト。
// prepared: worktree を用意した結果。
// 戻り値の1つ目: 使う pane の ID。
// 戻り値の2つ目: pane.list に失敗した場合、または pane が1つでない場合のエラー。
func (o *Orchestrator) resolvePane(ctx context.Context, prepared *workspace.PrepareResult) (string, error) {
	if prepared.HerdrWorkspaceID == "" {
		return "", i18n.Errorf(i18n.KeyOrchestratorResolvePaneWorkspaceIDEmpty)
	}
	list, err := o.herdr.PaneList(ctx, herdr.PaneListParams{WorkspaceID: prepared.HerdrWorkspaceID})
	if err != nil {
		return "", i18n.Errorf(i18n.KeyOrchestratorResolvePanePaneListFailed, err)
	}
	if len(list.Panes) != 1 {
		return "", i18n.Errorf(
			i18n.KeyOrchestratorResolvePanePaneCountUnexpected,
			prepared.HerdrWorkspaceID, len(list.Panes))
	}
	return list.Panes[0].PaneID, nil
}

// confirmStartup は agent_status が idle か done になっていることを確かめる
// （着手の段10。設計 3-16 / 3-80）。
//
//	idle / done  … 合格。**done も合格である**（continuo は tab をフォーカスしないため
//	               実運用ではほぼ常に done 側になる）
//	blocked      … 確認の画面が出ている。**このまま turn を送ると本文が画面に食われて消える**ので、
//	               agent.send_keys で ["esc"] を送ってから失敗として扱う（設計 3-11）
//	working      … herdr.startup_timeout_ms まで待つ。超えたら起動失敗
//	unknown      … **`agent.start` が返った直後は必ずこれである**（socket API は起動完了を
//	               待たない。2026-08-21 に実測）。`working` と同じく herdr.startup_timeout_ms
//	               まで待ち、超えたら ErrStartupRetryable を包んで返す（設計 3-16 の段10）
//
// **`agent_not_found` の間も、hook が届いていれば諦める時計を進めない**（設計 3-80）。
// 詳しくは `startupAliveByHook` を見よ。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 起動した run。
// since: この確認を始めた時刻。**これより後に届いた hook だけを「生きている証拠」に使う**
// （設計 3-80）。前の run が残した `LastHookAt` を証拠にしないためである。
// 戻り値: 合格なら nil。それ以外はエラー。
func (o *Orchestrator) confirmStartup(ctx context.Context, rs *runState, since time.Time) error {
	startupTimeout := time.Duration(o.cfg.Herdr.StartupTimeoutMs) * time.Millisecond
	deadline := o.now().Add(startupTimeout)
	for {
		got, err := o.herdr.AgentGet(ctx, herdr.AgentGetParams{Target: rs.agentName()})
		switch {
		case err != nil && herdr.IsCode(err, herdr.ErrCodeAgentNotFound):
			// **`agent_not_found` は、herdr がその名前の agent を登録していないことしか
			// 意味しない**（設計 3-80）。**「Claude Code が起動していない」とは限らない。**
			// herdr が登録するのは入力待ちの画面を見分けたときなので、**復帰した
			// Claude Code が起動直後から作業を始めると、その画面が一度も出ずに登録されない。**
			//
			// **hook が届いているなら、Claude Code は生きている。**諦める時計を進めず、
			// hook が止まってから `herdr.startup_timeout_ms` ぶん待って初めて諦める。
			// **ここで合格にはしない。**合格は `idle`/`done` かつ `interactive_ready` の
			// ときだけである。**herdr が登録し直すのを待つ**（走っている turn が終われば
			// 入力待ちの画面が出る）。
			//
			// **hook が来ていないときの振る舞いは変えない。**期限を待たずにその場で戻り、
			// `confirmStartupWithRestart` が `agent.start` をやり直す（設計 3-16 の段10）。
			// **そこが「pane のシェルが準備できていなかった」からの唯一の復帰の道である。**
			hookAt, alive := o.startupAliveByHook(rs, since)
			if !alive || o.now().After(hookAt.Add(startupTimeout)) {
				return fmt.Errorf("%w: %s", ErrStartupRetryable, i18n.T(
					i18n.KeyOrchestratorConfirmStartupAgentGetFailed,
					rs.agentName(), rs.agentName(), err))
			}
			// **待ち直す。**下の待ちで間を空けてから、もう一度 `agent.get` を読む。
			// **諦めるのは、最後の hook から `herdr.startup_timeout_ms` が経ったときである。**
			o.logger.Debug("herdr は agent を登録していませんが hook が届いているので、起動の確認を待ち直します",
				"identifier", rs.issue().Identifier, "agent", rs.agentName(),
				"最後に hook を受けた時刻", hookAt)
		case err != nil:
			return i18n.Errorf(
				i18n.KeyOrchestratorConfirmStartupAgentGetFailed,
				rs.agentName(), rs.agentName(), err)
		default:
			switch got.Agent.AgentStatus {
			case herdr.AgentStatusIdle, herdr.AgentStatusDone:
				// **`interactive_ready` が偽なら、まだ入力を受け付けられない。**
				//
				// herdr 自身が `agent start` の説明でこう書いている（原文）:
				// "Success means the expected agent was detected in the same terminal and
				// is ready for input."（訳: 成功とは、同じ端末で目当ての agent が検知され、
				// **入力を受け付けられる状態になったことである**）。
				// **状態だけを見て進むと、`agent.prompt` が `agent_not_ready` で弾かれる**
				// （2026-08-21 に E2E で観測。設計 6-2）。
				if !got.Agent.InteractiveReady {
					if o.now().After(deadline) {
						return fmt.Errorf("%w: %s", ErrStartupRetryable, i18n.T(
							i18n.KeyOrchestratorConfirmStartupNotInteractive,
							rs.agentName(), got.Agent.AgentStatus, rs.agentName()))
					}
					break
				}
				return nil
			case herdr.AgentStatusBlocked:
				o.sendEscape(ctx, rs)
				return i18n.Errorf(
					i18n.KeyOrchestratorConfirmStartupBlocked,
					rs.agentName(), rs.agentName())
			case herdr.AgentStatusWorking:
				if o.now().After(deadline) {
					return i18n.Errorf(
						i18n.KeyOrchestratorConfirmStartupWorkingTimeout,
						o.cfg.Herdr.StartupTimeoutMs, rs.agentName(), rs.agentName(), o.cfg.Herdr.StartupTimeoutMs)
				}
			default:
				// **`unknown` は「まだ見分けられていない」であって「壊れている」ではない。**
				//
				// **herdr の socket API の `agent.start` は、起動が終わるのを待たずに返る**
				// （`herdr agent start` の CLI にある `--timeout`（既定30秒。原文:
				// "Wait for interactive readiness"）は socket 経由では効かない。2026-08-21 に実測）。
				// **返った直後は必ず `unknown` である。**Claude Code はそのあと数秒かけて立ち上がり、
				// `idle` になる。**ここで即座に諦めると、正常な起動を毎回「失敗」と呼ぶことになる。**
				//
				// したがって `working` と同じ扱いにし、`herdr.startup_timeout_ms` まで待つ。
				if o.now().After(deadline) {
					return fmt.Errorf("%w: %s", ErrStartupRetryable, i18n.T(
						i18n.KeyOrchestratorConfirmStartupUnknownStatus,
						rs.agentName(), got.Agent.AgentStatus, rs.agentName(), rs.agentName()))
				}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(agentStatusPollInterval):
		}
	}
}

// startupAliveByHook は「起動の確認をしている間に、その run から hook が届いたか」を返す
// （設計 3-80）。
//
// **hook が届いたことは、Claude Code が生きていることの証拠である。**hook を起こすのは
// Claude Code 自身であり、continuo は `session_id` でしか run を引けないが（設計 3-2）、
// その対応は着手の段5b の `bindSession` で**段9（`agent.start`）より前に**作ってある。
// **だから段9 のあとに届いた hook は、必ずこの run のものである。**
//
// **`since` より後のものだけを数える。**復帰の道は前回のセッション UUID をそのまま使うので
// （設計 3-3b。`--resume` は `session_id` を変えない）、**切らないと前の run が残した
// `LastHookAt` を今回の起動の証拠として読んでしまう。**
//
// **hook の種類では絞らない。**ここが答えるのは「生きているか」だけであり、
// 「turn が走っているか」ではない。**`SessionStart` しか来ていなくても、生きてはいる。**
// いつ turn を送ってよいかは `agent.get` が決める（`confirmStartup` は
// `idle`/`done` かつ `interactive_ready` でしか合格しない）。
//
// **`LastHookAt` は受け取った時刻であって、hook が生まれた時刻ではない。**逃がし先から
// 読み戻した古い hook でも進む（`hookserver.Server.ReplayPending`）。**読み戻しは起動時の
// 復元で走るので着手より前だが、「`since` より後に生まれた」ことまでは保証しない。**
//
// rs: 対象の run。
// since: 起動の確認を始めた時刻。
// 戻り値の1つ目: 最後に hook を受けた時刻。
// 戻り値の2つ目: `since` より後に hook を受けていれば true。
func (o *Orchestrator) startupAliveByHook(rs *runState, since time.Time) (time.Time, bool) {
	at := rs.snapshot().LastHookAt
	return at, at.After(since)
}

// sendEscape は権限の確認で止まった agent に `["esc"]` を送る（設計 3-11）。
//
// **これは安全に関わる。**`blocked` のまま次のプロンプトを投げると、保留中の権限要求が
// 承認されて実行される（3/3 で再現）。**投げた本文のほうは消える。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
func (o *Orchestrator) sendEscape(ctx context.Context, rs *runState) {
	// **直接フィールドを読まない。**再 dispatch の `setAgentName` と、古い turn ループの
	// この呼び出しが同時に走りうる（設計 3-25 の「すべてのフィールドを排他で守る」）。
	if rs.agentName() == "" {
		return
	}
	if err := o.herdr.AgentSendKeys(ctx, herdr.AgentSendKeysParams{
		Target: rs.agentName(),
		Keys:   []string{"esc"},
	}); err != nil {
		o.logger.Warn("blocked の agent へ esc を送れませんでした（保留中の権限要求が残ります）",
			"identifier", rs.issue().Identifier, "error", err)
		return
	}
	o.logger.Info("blocked の agent へ esc を送りました", "identifier", rs.issue().Identifier)
}

// toIssueRef は tracker.Issue を workspace.IssueRef へ写す。
//
// **workspace はトラッカーを知らない**ので、必要な項目だけを写した値で渡す。
//
// issue: 元の issue。
// 戻り値: worktree の用意に要る情報。
func toIssueRef(issue tracker.Issue) workspace.IssueRef {
	linkedBranch := ""
	if issue.BranchName != nil {
		linkedBranch = *issue.BranchName
	}
	return workspace.IssueRef{
		URL:           issueURL(issue),
		Identifier:    issue.Identifier,
		ProjectItemID: issue.ID,
		Owner:         issue.Owner,
		Repo:          issue.Repo,
		Number:        issue.Number,
		LinkedBranch:  linkedBranch,
		NativeRef:     issue.NativeRef,
	}
}

// issueURL は issue の URL を返す。draft issue は URL を持たないので空文字になる。
//
// issue: 対象の issue。
// 戻り値: issue の URL。
func issueURL(issue tracker.Issue) string {
	if issue.URL == nil {
		return ""
	}
	return *issue.URL
}

// issueNodeID は下敷きの GitHub issue のノード ID を返す（コメントの投稿先）。
//
// **draft issue はノード ID を持たない**ので空文字になる（設計 3-6）。
//
// issue: 対象の issue。
// 戻り値: ノード ID。取れなければ空文字。
func issueNodeID(issue tracker.Issue) string {
	if issue.NativeRef == nil {
		return ""
	}
	v, ok := issue.NativeRef["issue_node_id"].(string)
	if !ok {
		return ""
	}
	return v
}

// confirmStartupWithRestart は段10 を通し、通らなければ `agent.start` からやり直す。
//
// **なぜやり直すのか。**`worktree.open` が作った pane は、シェルの起動（プロファイルの
// 読み込みなど）が終わるまで、コマンドを受け取れない。**herdr はそれを `agent_pane_busy` で
// 教えてくれることもあれば、教えずに `agent.start` を受け付けてしまうこともある**
// （2026-08-21 に E2E で観測）。後者では Claude Code が1文字も起動せず、
// `agent.get` も `agent.prompt` も `agent_not_found` を返す。
//
// **1回の `agent.start` に賭けない。**`herdr.startup_timeout_ms` の中で、
// 「起動して確かめる」を繰り返す。
//
// **ただし hook が届いていたら、やり直さない**（設計 3-80）。**pane は Claude Code が
// 埋めているので、`agent.start` は `agent_pane_busy` を返し続ける。**そちらは
// 「pane がまだシェルのプロンプトに来ていない」という意味なので（設計 2-1）、
// **シェルではない別のものが居るこの場面では、`agentStartBusyBudget`（30秒）を
// 使い切るだけである。**
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 起動した run。
// params: やり直すときに使う `agent.start` の引数（初回と同じものを渡すこと）。
// 戻り値: 合格なら nil。期限まで通らなければ ErrStartupRetryable を包んだエラー。
func (o *Orchestrator) confirmStartupWithRestart(
	ctx context.Context, rs *runState, params herdr.AgentStartParams,
) error {
	since := o.now()
	deadline := since.Add(time.Duration(o.cfg.Herdr.StartupTimeoutMs) * time.Millisecond)
	var lastErr error
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if hookAt, alive := o.startupAliveByHook(rs, since); alive {
				// **生きているものへ `agent.start` を投げ直さない**（設計 3-80）。
				o.logger.Info("hook が届いているので agent.start はやり直しません（herdr が登録し直すのを待ちます）",
					"identifier", rs.issue().Identifier, "回数", attempt,
					"最後に hook を受けた時刻", hookAt, "前回の理由", summaryLine(lastErr.Error()))
			} else {
				o.logger.Info("Claude Code が起動していないので agent.start をやり直します",
					"identifier", rs.issue().Identifier, "回数", attempt, "前回の理由", summaryLine(lastErr.Error()))
				started, err := o.herdr.AgentStartWithRetry(ctx, params, agentStartBusyBudget, agentStartRetryDelay)
				if err != nil {
					// **やり直しの失敗で run を捨てない。**期限まではもう一度試す。
					// （既に登録されている場合も、ここへ来て次の確認で拾える。）
					o.logger.Warn("agent.start のやり直しに失敗しました",
						"identifier", rs.issue().Identifier, "error", err)
				} else {
					rs.noteRevision(started.Agent.Revision, o.now())
				}
			}
		}

		// **`since` は最初の1回だけ控えたものを使い回す**（設計 3-80）。
		// **やり直しのたびに取り直すと、hook が届いた直後に取り直した回で証拠が消え、
		// 下の期限で諦めてしまう。**見たいのは「この起動の確認を始めてから hook が来たか」である。
		err := o.confirmStartup(ctx, rs, since)
		if err == nil {
			return nil
		}
		lastErr = err
		// **`blocked` は待っても直らない**（人間が確認の画面に答えるまで進まない）。
		// `confirmStartup` が esc を送り終えているので、そのまま返す。
		if !errors.Is(err, ErrStartupRetryable) {
			return err
		}
		if !o.now().Before(deadline) {
			return err
		}

		timer := time.NewTimer(agentStatusPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
