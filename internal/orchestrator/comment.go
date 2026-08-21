package orchestrator

import (
	"context"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
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
//  4. herdr の worktree.open で workspace を開き、その中の pane を pane.list で引く
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

	// 段4: worktree.open で workspace を開き、その中の pane を引く。
	opened, err := o.herdr.WorktreeOpen(ctx, herdr.WorktreeOpenParams{Path: snap.WorktreePath})
	if err != nil {
		o.logger.Warn("復元のための workspace を開けません", "identifier", snap.Identifier, "error", err)
		return
	}
	list, err := o.herdr.PaneList(ctx, herdr.PaneListParams{WorkspaceID: opened.Workspace.WorkspaceID})
	if err != nil || len(list.Panes) != 1 {
		o.logger.Warn("復元のための pane を引けません", "identifier", snap.Identifier, "error", err)
		return
	}
	paneID := list.Panes[0].PaneID
	rs.setPaneID(paneID)

	// 段5: --resume で復帰させる。**--settings と --permission-mode は毎回渡し直す**
	// （復元されないので、渡し直さないと hook が1つも効かない。設計 3-25）。
	name, err := o.resolveAgentName(ctx, rs.issue().Repo, rs.issue().Number)
	if err != nil {
		o.logger.Warn("復元のための agent 名を決められません", "identifier", snap.Identifier, "error", err)
		return
	}
	if _, err := o.herdr.AgentStartWithRetry(ctx, herdr.AgentStartParams{
		Name:   name,
		Kind:   o.cfg.Claude.Kind,
		PaneID: paneID,
		Args:   o.claudeStartArgs(identity.SettingsPath, "", identity.SessionUUID),
	}, agentStartRetries, agentStartRetryDelay); err != nil {
		o.logger.Warn("セッションを復元できません（No conversation found など）",
			"identifier", snap.Identifier, "error", err)
		o.failCommentRecovery(ctx, rs)
		return
	}
	rs.setAgentName(name)

	// 段6: idle か done になるのを待つ。
	if err := o.confirmStartup(ctx, rs); err != nil {
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

// hasRunComment は「この run が書いたコメント」があるかを返す（設計 3-25 の段1）。
//
// **marker が付いていて、かつ CreatedAt が runState.StartedAt より新しいものだけを数える。**
// worktree を再利用すると前の run のコメントが残っているためである。
//
// ctx: 呼び出しに適用するコンテキスト。
// nodeID: 下敷きの GitHub issue のノード ID。
// snap: 対象の run の写し。
// 戻り値: この run が書いたコメントがあれば true。
func (o *Orchestrator) hasRunComment(ctx context.Context, nodeID string, snap runSnapshot) bool {
	comments, err := o.tracker.FetchComments(ctx, nodeID, o.cfg.Tracker.Provider.Comments, o.cfg.Tracker.Comments)
	if err != nil {
		o.logger.Warn("コメントを読めません（書かれていないものとして扱います）",
			"identifier", snap.Identifier, "error", err)
		return false
	}
	for _, c := range comments {
		if !c.IsAgent {
			continue
		}
		if c.CreatedAt.After(snap.StartedAt) {
			return true
		}
	}
	return false
}

// failCommentRecovery はコメントを書かせられなかった run を人間へ渡す（設計 3-25 の段9）。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 対象の run。
func (o *Orchestrator) failCommentRecovery(ctx context.Context, rs *runState) {
	o.stopWorker(ctx, rs)
	if _, err := o.tracker.UpdateStatus(ctx, rs.IssueID, o.cfg.Tracker.FailureState, o.cfg.Tracker.TerminalStates); err != nil {
		o.logger.Warn("Status を落とせません", "identifier", rs.issue().Identifier, "error", err)
	}
	o.postHandoffComment(ctx, rs, "Claude Code は作業を終えたと表明しましたが、**何をしたのかを issue に書き残しませんでした。**"+
		"continuo は成果の要約を代筆しないので、このままでは何が行われたか誰にも分かりません。"+
		"\n【確かめ方】worktree の中身（下記）と `git log` を見て、実際に何が変わったかを確かめてください。"+
		"\n【よくある原因】エージェントがコメントの投稿に失敗した / 指示の文面にコメントを書く手順が無い。"+
		"\n【対処】成果を確かめたうえで、この issue を完了にするか着手待ちへ戻すかを決めてください。")
}
