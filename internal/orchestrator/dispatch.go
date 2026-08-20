package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// agentStartRetries は agent.start が agent_pane_busy を返したときに再試行する回数である
// （設計 2-1 / 3-16 の段9）。
const agentStartRetries = 3

// agentStartRetryDelay は agent.start の再試行の間隔である。
const agentStartRetryDelay = 500 * time.Millisecond

// agentStatusPollInterval は段10 で agent_status を見直す間隔である。
const agentStatusPollInterval = 500 * time.Millisecond

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
		o.logger.Info("枠が閾値を超えているので新規の dispatch を止めます（走行中の turn は止めません）",
			"pause_above_percent", o.cfg.RateLimit.PauseAbovePercent)
		return
	}

	var claimed []claimedRun
	for _, issue := range candidates {
		if ctx.Err() != nil {
			break
		}
		// 既に印を持っている issue は dispatch しない（設計 3-10 / 4-2）。
		if _, taken := o.lookupRunByID(issue.ID); taken {
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
			continue
		}
		if !hasRequiredLabels(issue, o.cfg.Tracker.RequiredLabels) {
			continue
		}

		// 段-1: 空きスロットを数える。**印を付ける前に行う**（付けてから弾くと印が残る）。
		if !o.hasFreeSlot() {
			o.logger.Info("空きスロットが尽きたので、この巡回ではこれ以上 dispatch しません",
				"max_concurrent_agents", o.cfg.Agent.MaxConcurrentAgents)
			break
		}

		if rs, ok := o.claimForDispatch(ctx, issue); ok {
			claimed = append(claimed, claimedRun{rs: rs, issue: issue})
		}
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
	runs := o.snapshotRuns()
	if len(runs) >= o.cfg.Agent.MaxConcurrentAgents {
		return false
	}

	limits := o.cfg.Agent.MaxConcurrentAgentsByState
	if len(limits) == 0 {
		return true
	}
	limit, ok := lookupFolded(limits, o.cfg.Tracker.RunningState)
	if !ok {
		// 該当するキーが無ければ、その Status には全体の上限だけを適用する。
		return true
	}

	inRunningState := 0
	for _, rs := range runs {
		if strings.EqualFold(strings.TrimSpace(rs.snapshot().State), strings.TrimSpace(o.cfg.Tracker.RunningState)) {
			inRunningState++
		}
	}
	return inRunningState < limit
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
			return false
		}
	}
	return true
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
	// 段0: dispatch の直前の検査（設計 3-6 の「issue ごと」の表）。
	if !o.preflight(ctx, issue) {
		return nil, false
	}

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
	if err := o.startRun(ctx, rs, issue, reuse); err != nil {
		if reuse {
			o.logger.Warn("再 dispatch に失敗しました", "identifier", issue.Identifier, "error", err)
			o.failRun(ctx, rs, fmt.Sprintf("再 dispatch に失敗しました: %v", err))
			return
		}
		o.logger.Warn("着手に失敗しました", "identifier", issue.Identifier, "error", err)
		o.failRun(ctx, rs, fmt.Sprintf("着手に失敗しました: %v", err))
	}
}

// redispatch はバックオフが明けた run を再 dispatch する（設計 3-25 の「明けたらどう再開するか」）。
//
//	段0 から入り直す（検査をやり直す。信頼が外れているかもしれない）
//	段1 は飛ばす（既に印がある）
//	段2 の Status の書き込みは行う（取り直して terminal_states でなければ書く）
//	段3 の worktree は再利用する（身元ファイルの takeover_count を1つ増やす）
//	段5 の設定ファイルは作り直す（socket のパスが変わっているかもしれない）
//	**セッション UUID は新しく採番する**（一度使った UUID は再利用できない。設計 3-3）
//	RetryCount はそのまま。BackoffUntil はゼロ値へ戻す
//
// **送るのは1回目の本文（5-3）である。**UUID を採り直した時点で会話履歴を持たない
// 別のセッションになっているので、継続の指示（5-4）だけでは何をすべきか伝わらない。
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
		"identifier", issue.Identifier, "repo", key, "理由", reason)

	if o.cfg.Trust.OnUntrusted != "skip_and_comment" {
		return
	}
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		o.logger.Warn("draft issue にはコメントできないのでログだけにします", "identifier", issue.Identifier)
		return
	}
	if _, err := o.tracker.PostComment(ctx, nodeID,
		buildUntrustedComment(issue.Owner, issue.Repo, reason),
		o.cfg.Tracker.Provider.Comments.SelfMarker); err != nil {
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
	// **書く前に必ず ID 指定で取り直す**（UpdateStatus が内部で行う）。
	// **terminal_states に入っていたら書かない。**
	if _, err := o.tracker.UpdateStatus(ctx, issue.ID, o.cfg.Tracker.RunningState, o.cfg.Tracker.TerminalStates); err != nil {
		return fmt.Errorf("Status を %s へ書けません: %w", o.cfg.Tracker.RunningState, err)
	}
	// **段-1 の状態ごとの上限は running_state のバケツで数える**（設計 3-16）。
	// 手元のスナップショットを書き換えておかないと、次の巡回で取り直すまで
	// dispatch 前の Status（Ready）のまま数えてしまう。
	rs.setIssueState(o.cfg.Tracker.RunningState)

	// 段3: worktree を用意し、herdr workspace として開く。
	prepared, err := o.ws.Prepare(ctx, toIssueRef(issue))
	if err != nil {
		return fmt.Errorf("作業用の worktree を用意できませんでした。\n%w", err)
	}
	rs.setWorkspaceInfo(prepared.Path, prepared.Base, prepared.HerdrWorkspaceID)

	// 段4: worktree を新しく作ったときだけ after_create を走らせる（仕様 5.3.4）。
	if prepared.Created {
		if err := o.ws.RunHook(ctx, workspace.HookAfterCreate, prepared.Path); err != nil {
			return fmt.Errorf("workspace_hooks.after_create に失敗しました: %w", err)
		}
	}

	// 段5: Claude Code の設定ファイルを worktree の外に作る（再 dispatch でも作り直す）。
	settingsPath, err := o.writeSettingsFile(issue.Identifier)
	if err != nil {
		return err
	}
	rs.setSettingsPath(settingsPath)

	// セッション UUID は起動のたびに新しく採番する（設計 3-3）。
	sessionUUID, err := o.newSessionUUID()
	if err != nil {
		return err
	}
	rs.setSessionUUID(sessionUUID)
	o.bindSession(rs, sessionUUID)
	// **ここから先は会話履歴を持たない別のセッションである。**worker の世代を1つ進め、
	// 次の turn で1回目の本文（5-3）を送るようにする（設計 5-3 / 5-4）。
	// **再 dispatch でも同じである。**UUID を採り直しているので、継続の指示（5-4）だけを
	// 送ると、どの issue を何のためにやるのかが1文字も伝わらない。
	rs.beginAttempt()

	// 段6: worktree の中に身元ファイルを書く。ここまで来れば、落ちても身元が分かる。
	identity := workspace.Identity{
		IssueURL:         issueURL(issue),
		IssueIdentifier:  issue.Identifier,
		ProjectItemID:    issue.ID,
		Branch:           prepared.Branch.String(),
		Base:             prepared.Base.String(),
		HerdrWorkspaceID: prepared.HerdrWorkspaceID,
		SocketPath:       o.socketPath,
		SettingsPath:     settingsPath,
		SessionUUID:      sessionUUID,
		CreatedAt:        o.now(),
	}
	identity = workspace.MergeForReuse(identity, prepared.ExistingIdentity)
	if err := o.ws.WriteIdentity(ctx, prepared.Path, identity); err != nil {
		return fmt.Errorf("身元ファイルを書けません: %w", err)
	}
	if reuse {
		// 再 dispatch のときも引き継いだ回数を数える（設計 3-18）。
		if _, err := o.ws.IncrementTakeover(ctx, prepared.Path); err != nil {
			o.logger.Warn("引き継いだ回数を増やせませんでした", "identifier", issue.Identifier, "error", err)
		}
	}

	// 段7: before_run（失敗したら致命）。
	if err := o.ws.RunHook(ctx, workspace.HookBeforeRun, prepared.Path); err != nil {
		return fmt.Errorf("workspace_hooks.before_run に失敗しました: %w", err)
	}

	// 段8: worktree.open が開いた workspace の pane を pane.list で引く。
	// **pane を新しく作らない**（pane.split も tab.create も呼ばない。設計 4-5）。
	paneID, err := o.resolvePane(ctx, prepared)
	if err != nil {
		return err
	}
	rs.setPaneID(paneID)
	if _, err := o.herdr.PaneRename(ctx, herdr.PaneRenameParams{
		PaneID: paneID,
		Label:  issueURL(issue),
	}); err != nil {
		return fmt.Errorf("pane の label に issue の URL を書けません: %w", err)
	}

	// 段9: その pane で Claude Code を起動する。
	// **環境変数は設定ファイルの env に書く。**pane にも agent.start にも渡さない（設計 3-12）。
	agentName, err := o.resolveAgentName(ctx, issue.Repo, issue.Number)
	if err != nil {
		return err
	}
	if _, err := o.herdr.AgentStartWithRetry(ctx, herdr.AgentStartParams{
		Name:   agentName,
		Kind:   o.cfg.Claude.Kind,
		PaneID: paneID,
		Args:   o.claudeStartArgs(settingsPath, sessionUUID, ""),
	}, agentStartRetries, agentStartRetryDelay); err != nil {
		return fmt.Errorf("Claude Code を起動できません: %w", err)
	}
	rs.setAgentName(agentName)
	if err := o.ws.SetAgentName(ctx, prepared.Path, agentName.String()); err != nil {
		o.logger.Warn("身元ファイルへ agent 名を書けませんでした", "identifier", issue.Identifier, "error", err)
	}

	// 段10: agent_status が idle か done であることを確かめる。
	if err := o.confirmStartup(ctx, rs); err != nil {
		return err
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
		return "", errors.New("herdr の workspace の ID が空です（herdr.worktree.create_via_herdr が false かもしれません）")
	}
	list, err := o.herdr.PaneList(ctx, herdr.PaneListParams{WorkspaceID: prepared.HerdrWorkspaceID})
	if err != nil {
		return "", fmt.Errorf("workspace の pane を引けません: %w", err)
	}
	if len(list.Panes) != 1 {
		return "", fmt.Errorf(
			"workspace %s の pane が %d 個あります（1つであるべきです。人間が触った workspace かもしれません）",
			prepared.HerdrWorkspaceID, len(list.Panes))
	}
	return list.Panes[0].PaneID, nil
}

// confirmStartup は agent_status が idle か done になっていることを確かめる
// （着手の段10。設計 3-16）。
//
//	idle / done  … 合格。**done も合格である**（continuo は tab をフォーカスしないため
//	               実運用ではほぼ常に done 側になる）
//	blocked      … 確認の画面が出ている。**このまま turn を送ると本文が画面に食われて消える**ので、
//	               agent.send_keys で ["esc"] を送ってから失敗として扱う（設計 3-11）
//	working      … startup_timeout_ms まで待つ。超えたら起動失敗
//	unknown      … 判断できないので起動失敗として扱う
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 起動した run。
// 戻り値: 合格なら nil。それ以外はエラー。
func (o *Orchestrator) confirmStartup(ctx context.Context, rs *runState) error {
	deadline := o.now().Add(time.Duration(o.cfg.Claude.StartupTimeoutMs) * time.Millisecond)
	for {
		got, err := o.herdr.AgentGet(ctx, herdr.AgentGetParams{Target: rs.agentName()})
		if err != nil {
			return fmt.Errorf(
				"Claude Code の状態を herdr に聞けませんでした（agent 名: %s）。"+
					"\n【確かめ方】`herdr agent get %s` を実行してください。"+
					"\n【よくある原因】herdr が落ちている / agent の登録が消えた。"+
					"\n元のエラー: %w",
				rs.agentName(), rs.agentName(), err)
		}
		switch got.Agent.AgentStatus {
		case herdr.AgentStatusIdle, herdr.AgentStatusDone:
			return nil
		case herdr.AgentStatusBlocked:
			o.sendEscape(ctx, rs)
			return fmt.Errorf(
				"Claude Code が起動直後に確認の画面で止まりました（agent 名: %s）。"+
					"continuo は esc を送って画面を閉じましたが、"+
					"**この issue は人間が見ないと進みません。**"+
					"\n【確かめ方】`herdr agent read %s --source recent-unwrapped --lines 40` で画面を見てください。"+
					"\n【よくある原因】そのフォルダが Claude Code に信頼登録されていない / "+
					"許可されていないコマンドを実行しようとした。"+
					"\n【対処】許可が要るなら WORKFLOW.md の `claude.permissions.allow` に足してください。",
				rs.agentName(), rs.agentName())
		case herdr.AgentStatusWorking:
			if o.now().After(deadline) {
				return fmt.Errorf(
					"Claude Code の起動が %d ミリ秒たっても落ち着きませんでした（agent 名: %s）。"+
						"起動はしていますが、待っている間ずっと `working` のままでした。"+
						"\n【確かめ方】`herdr agent read %s --source recent-unwrapped --lines 40` で画面を見てください。"+
						"\n【よくある原因】起動時の処理が重い / ネットワークが遅い。"+
						"\n【対処】WORKFLOW.md の `claude.startup_timeout_ms` を増やしてください（いまは %d）。",
					o.cfg.Claude.StartupTimeoutMs, rs.agentName(), rs.agentName(), o.cfg.Claude.StartupTimeoutMs)
			}
		default:
			return fmt.Errorf(
				"Claude Code が起動しませんでした（agent 名: %s、herdr が返した状態: %q）。"+
					"herdr は pane を作りましたが、そこで動いている Claude Code を見つけられませんでした。"+
					"\n【確かめ方】`herdr agent explain %s` で herdr が何を見て判断したかを、"+
					"`herdr agent read %s --source recent-unwrapped --lines 40` で画面に何が出ているかを見てください。"+
					"\n【よくある原因】claude コマンドが PATH に無い / "+
					"claude の起動が途中で失敗した / そのフォルダが Claude Code に信頼登録されていない。"+
					"\n【対処】`command -v claude` で claude が PATH にあるかを確かめてください"+
					"（**`continuo doctor` は claude の有無を検査しません**）。"+
					"信頼登録が足りないなら `continuo trust` を実行してください。",
				rs.agentName(), got.Agent.AgentStatus, rs.agentName(), rs.agentName())
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(agentStatusPollInterval):
		}
	}
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
	return workspace.IssueRef{
		URL:           issueURL(issue),
		Identifier:    issue.Identifier,
		ProjectItemID: issue.ID,
		Owner:         issue.Owner,
		Repo:          issue.Repo,
		Number:        issue.Number,
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
