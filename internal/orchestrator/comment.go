package orchestrator

import (
	"context"
	"os"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/redact"
	"github.com/maimuzo/continuo/internal/workspace"
)

// commentRecheckWait は「書いてください」と送ったあとにコメントを読み直すまでの待ちである。
const commentRecheckWait = 2 * time.Second

// ensureAgentComment は「この run が書いたコメント」があるかを確かめ、無ければ
// セッションを復元してエージェントに書かせる（設計 3-25 の9段）。
//
// **走らせるのは run が終わるときだけである。**毎 turn ではない（設計 3-25 の
// 「いつ走らせるか」）。**`max_dispatch_turns` に達したとき・stall で打ち切ったとき・
// リトライを使い切って人間へ渡すときも走らせる。**`working` の表明を受けて次の turn へ
// 進むときは走らせない。
//
// **1回も turn を送っていない run では何もしない**（`StartedAt` がゼロ値）。
// 会話が1つも無いセッションを復元しても、書かせる材料が無い。
//
//  1. issue のコメントを読み、「この run が書いたもの」があるかを見る
//     → marker が付いていて、かつ CreatedAt が runState.StartedAt より新しいものだけを数える
//     （**worktree を再利用すると前の run のコメントが残っている**ため）
//     → あれば、ここで終わり
//  2. 無ければ、まず走行中の worker を止める（pane.close）
//     → 止めないと、同じセッション UUID が2つ生きることになる
//  3. 身元ファイルからセッション UUID と設定ファイルのパスを読む
//  4. worktree を herdr の workspace として開き直し（着手の段3 と同じ Prepare を通す）、
//     その中の pane を pane.list で引く
//  5. その pane で agent.start を呼ぶ（--resume <UUID> --settings <設定ファイル> --permission-mode dontAsk）
//  6. agent_status が idle または done になるのを待つ
//  7. agent.prompt で「作業の内容を issue のコメントに書いてください」とだけ送る
//     → **この送信は turn 数に数えない**（max_dispatch_turns の判定に影響させない）
//  8. コメントを読み直し、書かれていれば終わり
//  9. それでも書かれなければ failure_state へ落として人間に渡す
//
// **continuo は代筆しない**（設計 3-25 / 3-29）。成果をまとめられるのはエージェントだけである。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
// stoppedWhileRecovering は、止められたことが原因の失敗かを判定する。
//
// **`Ctrl+C` のたびに「〜できません」が並ぶと、本当に壊れたときと見分けがつかない。**
// この経路は「終わった run にコメントを書かせる」ための後追いなので、
// **止められたなら、次の起動でやり直せばよい。**
//
// **1箇所ずつ書くと必ず漏れる。**実際、`agent.start` → `agent.prompt` → `agent.list` →
// `worktree.open` と、CI に4回見つけさせてから、ここへまとめた。
//
// ctx: 呼び出しに使ったコンテキスト。
// 戻り値: 止められたことが原因なら true。**そのときログは Debug で1行だけ出す。**
func (o *Orchestrator) stoppedWhileRecovering(ctx context.Context) bool {
	if ctx.Err() == nil {
		return false
	}
	o.logger.Debug("止められたので、コメントの依頼は次の起動に回します")
	return true
}

func (o *Orchestrator) ensureAgentComment(ctx context.Context, rs *runState) {
	nodeID := issueNodeID(rs.issue())
	if nodeID == "" {
		// draft issue にはコメントできない。
		return
	}
	snap := rs.snapshot()
	if snap.StartedAt.IsZero() {
		// **1回も turn を送っていない run には、書かせる材料が無い。**着手そのものに
		// 失敗した場合（設定ファイルを書けない・pane が引けない・テンプレートの描画に
		// 失敗した）がこれである。復元しても、そのセッションには会話が1つも無い。
		o.logger.Info("turn を1回も送っていないので、コメントの確認は行いません",
			"identifier", snap.Identifier)
		return
	}

	if o.hasRunComment(ctx, nodeID, snap) {
		return
	}
	o.logger.Info("この run のコメントが無いので、セッションを復元して書かせます", "identifier", snap.Identifier)

	// 段2: 先に worker を止める（同じセッション UUID が2つ生きるのを防ぐ）。
	o.stopWorker(ctx, rs)

	// 段3: 身元ファイルからセッション UUID と設定ファイルのパスを読む。
	if snap.WorktreePath == "" {
		o.logger.Warn("worktree のパスが分からないので復元できません", "identifier", snap.Identifier)
		return
	}
	identity, err := o.ws.ReadIdentity(snap.WorktreePath)
	if err != nil {
		o.logger.Warn("身元ファイルを読めないので復元できません", "identifier", snap.Identifier, "error", err)
		return
	}
	if identity.SessionUUID == "" || identity.SettingsPath == "" {
		o.logger.Warn("身元ファイルにセッション UUID か設定ファイルのパスがありません",
			"identifier", snap.Identifier)
		return
	}

	// 段4: worktree を herdr の workspace として開き直し、その中の pane を引く。
	//
	// **`worktree.open` を自分で呼ばず、着手の段3 と同じ `workspace.Manager.Prepare` を通す。**
	// `worktree.open` は `cwd` にリポジトリ本体を渡さないと
	// `worktree_not_found: worktree path not found` で断る（実測: 2026-08-25、test/live。
	// 設計 6-10 の表）。**その `cwd` に渡す clone の場所を知っているのは Prepare だけである。**
	// Prepare を通せば `focus: false`・`label`（`owner/repo/issues/N`）・
	// 開いたものが本当にこの worktree かの検算・**continuo が開かせたリポジトリの親 workspace の
	// 控え**（issue #19）も、着手のときと同じ1箇所から出る。**2箇所に書くと必ずずれる。**
	//
	// **worktree の実体が無ければ呼ばない。**Prepare は無ければ `git worktree add` で
	// 作り直すので、**片付け済みの worktree をここで復活させてしまう。**
	if _, statErr := os.Stat(snap.WorktreePath); statErr != nil {
		o.logger.Warn("worktree の実体が無いので復元しません（作り直しません）",
			"identifier", snap.Identifier, "path", snap.WorktreePath, "error", statErr)
		return
	}
	prepared, err := o.ws.Prepare(ctx, toIssueRef(rs.issue()))
	if err != nil {
		if o.stoppedWhileRecovering(ctx) {
			return
		}
		o.logger.Warn("復元のための workspace を開けません", "identifier", snap.Identifier, "error", err)
		return
	}
	// **開かせた親 workspace を身元ファイルへ控える**（issue #19）。控えないと、
	// 片付けが閉じる相手を知らないまま終わり、この経路で開いた workspace が残る。
	o.recordRepoWorkspace(ctx, prepared, identity)
	paneID, err := o.resolvePane(ctx, prepared)
	if err != nil {
		if o.stoppedWhileRecovering(ctx) {
			return
		}
		o.logger.Warn("復元のための pane を引けません", "identifier", snap.Identifier, "error", err)
		return
	}
	rs.setPaneID(paneID)

	// 段5: --resume で復帰させる。**--settings と --permission-mode は毎回渡し直す**
	// （復元されないので、渡し直さないと hook が1つも効かない。設計 3-25）。
	name, err := o.resolveAgentName(ctx, rs.issue().Repo, rs.issue().Number)
	if err != nil {
		if o.stoppedWhileRecovering(ctx) {
			return
		}
		o.logger.Warn("復元のための agent 名を決められません", "identifier", snap.Identifier, "error", err)
		return
	}
	if _, err := o.herdr.AgentStartWithRetry(ctx, herdr.AgentStartParams{
		Name:   name,
		Kind:   o.cfg.Claude.Kind,
		PaneID: paneID,
		Args:   o.claudeStartArgs(identity.SettingsPath, "", identity.SessionUUID),
	}, agentStartBusyBudget, agentStartRetryDelay); err != nil {
		if o.stoppedWhileRecovering(ctx) {
			return
		}
		o.logger.Warn("セッションを復元できません（No conversation found など）",
			"identifier", snap.Identifier, "error", err)
		o.failCommentRecovery(ctx, rs)
		return
	}
	rs.setAgentName(name)

	// 段6: idle か done になるのを待つ。
	if err := o.confirmStartup(ctx, rs); err != nil {
		if o.stoppedWhileRecovering(ctx) {
			return
		}
		o.logger.Warn("復元した agent が落ち着きません", "identifier", snap.Identifier, "error", err)
		o.failCommentRecovery(ctx, rs)
		return
	}

	// 段7: 「コメントに書いてください」とだけ送る。**turn 数に数えない。**
	//
	// **待ちの上限には `claude.turn_timeout_ms` を使う**（画面が変わらないまま待てる時間）。
	// **この run はもう印から外れる途中なので、巡回の stall 検知は見ていない。**
	// 返らなければこの上限で切り上げて段8（コメントの読み直し）へ進む。
	if _, err := o.herdr.AgentPrompt(ctx, herdr.AgentPromptParams{
		Target: name,
		Text:   buildCommentRequestPrompt(issueURL(rs.issue()), o.cfg.Tracker.Comments.Marker),
		Wait: &herdr.AgentWaitOptions{
			TimeoutMs: o.cfg.Claude.TurnTimeoutMs,
			Until:     waitUntilStatuses(o.cfg.Claude.WaitUntil),
		},
	}); err != nil {
		if o.stoppedWhileRecovering(ctx) {
			return
		}
		o.logger.Warn("コメントを書かせるプロンプトを送れません", "identifier", snap.Identifier, "error", err)
	}

	// 段8: コメントを読み直す。
	select {
	case <-ctx.Done():
		return
	case <-time.After(commentRecheckWait):
	}
	if o.hasRunComment(ctx, nodeID, snap) {
		o.logger.Info("エージェントがコメントを書きました", "identifier", snap.Identifier)
		o.stopWorker(ctx, rs)
		return
	}

	// 段9: それでも書かれなければ人間に渡す。
	o.failCommentRecovery(ctx, rs)
}

// recordRepoWorkspace は、コメントの復元が開かせたリポジトリの親 workspace を
// 身元ファイルへ控える（issue #19）。
//
// **控えないと、片付けが閉じる相手を知らないまま終わる。**着手の段3 では
// `workspace.Manager.Prepare` が「自分より前から親 workspace があったか」を見て、
// **自分が開かせたときだけ ID を返す。**その値を段6 で身元ファイルへ書いているので、
// 復元の経路でも同じことをしないと、この経路で開いた workspace だけが閉じ残る。
//
// **既に控えてある値は上書きしない。**先に書いてあるのは着手のときの値であり、
// そちらが「continuo が開かせた」ことの記録として正しい。
//
// ctx: 呼び出しに適用するコンテキスト（git を実行するときに使う）。
// prepared: `Prepare` が返した結果。
// identity: 段3 で読んだ身元ファイル（**この関数が書き換える**）。
func (o *Orchestrator) recordRepoWorkspace(
	ctx context.Context,
	prepared *workspace.PrepareResult,
	identity *workspace.Identity,
) {
	if prepared.HerdrRepoWorkspaceID == "" || identity.HerdrRepoWorkspaceID != "" {
		return
	}
	identity.HerdrRepoWorkspaceID = prepared.HerdrRepoWorkspaceID
	if err := o.ws.WriteIdentity(ctx, prepared.Path, *identity); err != nil {
		o.logger.Warn("復元で開いた親 workspace の ID を身元ファイルへ書けませんでした",
			"path", prepared.Path, "herdr_repo_workspace_id", prepared.HerdrRepoWorkspaceID, "error", err)
	}
}

// hasRunComment は「この run が書いたコメント」があるかを返す（設計 3-25 の段1）。
//
// **marker が付いていて、かつ CreatedAt が runState.StartedAt より新しいものだけを数える。**
// worktree を再利用すると前の run のコメントが残っているためである。
//
// **marker が付いているだけでは数えない**（設計 3-65）。印は本文の先頭に置くただの
// 文字列であり、**issue にコメントできる人なら誰でも同じものを書ける。**
// **投稿者が「continuo が使う gh の持ち主」であるものだけを、エージェントが書いたものとして扱う**
// （その照合は `FetchComments` が行い、結果が `Comment.IsAgent` である）。
// **持ち主が取れていなければ、いままでどおり印だけで判定する**（`ghLoginName` が空文字）。
//
// **「印はあるが投稿者が違う」コメントを見つけたら、WARN で名指しする**（設計 3-65）。
// **これがいちばん切り分けの難しい状態である。**issue の画面には印の付いたコメントが
// 見えているのに、continuo は「書かれていない」と判定してセッションを復元しにいく。
//
// ctx: 呼び出しに適用するコンテキスト。
// nodeID: 下敷きの GitHub issue のノード ID。
// snap: 対象の run の写し。
// 戻り値: この run が書いたコメントがあれば true。
func (o *Orchestrator) hasRunComment(ctx context.Context, nodeID string, snap runSnapshot) bool {
	// **最初の巡回より前にこの経路へ入ることがある**（引き継いだ run の turn が
	// 先に終わる場合）。**そのときはここで持ち主を取る。**
	// **まだ取れていなければ ghLoginRetryInterval ごとに取り直し、一度取れたら取りに行かない**
	// （設計 3-65。判定は ghLoginDue）。
	o.ensureGHLogin(ctx)
	comments, err := o.tracker.FetchComments(
		ctx, nodeID, o.cfg.Tracker.Provider.Comments, o.cfg.Tracker.Comments, o.ghLoginName())
	if err != nil {
		if o.stoppedWhileRecovering(ctx) {
			// **止められただけである。**「書かれていない」と答えて抜ける
			// （呼び出し側はそのまま復元へ進むが、その先も止められる）。
			return false
		}
		o.logger.Warn("コメントを読めません（書かれていないものとして扱います）",
			"identifier", snap.Identifier, "error", err)
		return false
	}
	found := false
	for _, c := range comments {
		if !c.CreatedAt.After(snap.StartedAt) {
			// 前の run のコメントである（worktree を再利用すると残っている）。
			continue
		}
		// **「印はあるが投稿者が違う」は、名指しでログに出す**（設計 3-65）。
		// **これがいちばん切り分けの難しい状態である。**issue の画面には印の付いた
		// コメントが見えているのに、continuo は「書かれていない」と判定する。
		// 出さないと、人間に見えるのは「この run のコメントが無いので…」の1行だけになり、
		// 印を騙られたのか本当に書かれていないのかが分からない。
		if c.MarkedByOther {
			o.logger.Warn("コメントに印は付いていますが、投稿者が gh の持ち主と違います"+
				"（エージェントが書いたものとして数えません）",
				"identifier", snap.Identifier, "投稿者", c.Author,
				"gh の持ち主", o.ghLoginName(), "url", c.URL)
			continue
		}
		if c.IsAgent {
			found = true
		}
	}
	return found
}

// failCommentRecovery はコメントを書かせられなかった run を人間へ渡す（設計 3-25 の段9）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
func (o *Orchestrator) failCommentRecovery(ctx context.Context, rs *runState) {
	o.stopWorker(ctx, rs)
	moved, err := o.tracker.UpdateStatus(ctx, rs.IssueID, o.cfg.Tracker.FailureState, o.cfg.Tracker.TerminalStates)
	if err != nil {
		if o.stoppedWhileRecovering(ctx) {
			return
		}
		o.logger.Warn("Status を落とせません", "identifier", rs.issue().Identifier, "error", err)
	}
	o.postHandoffComment(ctx, rs, "Claude Code は作業を終えたと表明しましたが、**何をしたのかを issue に書き残しませんでした。**"+
		"continuo は成果の要約を代筆しないので、このままでは何が行われたか誰にも分かりません。"+
		"\n【確かめ方】worktree の中身（下記）と `git log` を見て、実際に何が変わったかを確かめてください。"+
		"\n【よくある原因】エージェントがコメントの投稿に失敗した / 指示の文面にコメントを書く手順が無い。"+
		"\n【対処】成果を確かめたうえで、この issue を完了にするか着手待ちへ戻すかを決めてください。",
		newStatusMove(moved, o.cfg.Tracker.FailureState))
}

// postStatusMove は continuo が Status を動かした記録を issue のコメントに残す（設計 3-29）。
//
// **書き込みが起きていなければ何もしない。**ボードが動いていないので書くことがない。
// 「動かさない」表明（`status_signal_map` の値が null）と、item が見えない・
// 書いてはいけない状態だった・既に同じ値だった場合が、これに当たる。
//
// **引き渡しの通知を出す経路からは呼ばない。**そちらは通知の本文に1行入れる
// （`buildHandoffComment`）。
//
// **この投稿が `hasRunComment` を満たすことはない。**`self_marker` が付くので
// `FetchComments` の結果から外れる（設計 3-29）。
//
// ctx: 呼び出しに適用するコンテキスト。
// identifier: issue の識別子（ログに出す）。
// nodeID: 投稿先の issue のノード ID。空なら何もしない。
// move: 動かした記録。
// why: 「なぜ」に入れる文。「〜ためです」で終わる形で渡す。
func (o *Orchestrator) postStatusMove(
	ctx context.Context, identifier, nodeID string, move statusMove, why string,
) {
	if !move.Wrote || nodeID == "" {
		return
	}
	if err := o.postComment(ctx, nodeID, buildStatusMoveComment(move, why, o.now())); err != nil {
		o.logger.Warn("Status を動かした記録を投稿できませんでした", "identifier", identifier, "error", err)
	}
}

// postComment は continuo のコメントを issue へ1件書く。
//
// **continuo が issue へ書くものは、例外なくここを通す**（設計 3-73）。
// **`o.tracker.PostComment` を直に呼んではならない。**
// `test/internal/redact` の検査が、この1本を迂回した呼び出しを構文木で落とす。
//
// **ここが、手元の絶対パスを `~` に縮める唯一の場所である。**本文を組み立てる場所は
// 6箇所あり、そのどれもが worktree・会話の記録・設定ファイルの絶対パスを載せうる。
// **git の失敗をそのまま貼る経路もある**ので、組み立てる側で縮めると必ず漏れる。
//
// **縮められなかったときは、投稿する前に警告を1行出す。**投稿そのものは止めない
// （止めると人間は「なぜ止まったのか」を知る手立てを失う）。**その代わり、
// 絶対パスが公開の issue へ出たことが、あとからログで辿れる。**
//
// ctx: 呼び出しに適用するコンテキスト。
// nodeID: 投稿先の issue のノード ID。
// body: コメント本文。
// 戻り値: 投稿に失敗したときのエラー。
func (o *Orchestrator) postComment(ctx context.Context, nodeID, body string) error {
	safe, redactErr := redact.Paths(body)
	if redactErr != nil {
		o.logger.Warn(
			"手元の絶対パスを縮められませんでした。本文をそのまま投稿します",
			"node_id", nodeID, "error", redactErr,
		)
	}
	_, err := o.tracker.PostComment(ctx, nodeID, safe, o.cfg.Tracker.Comments.SelfMarker)
	return err
}
