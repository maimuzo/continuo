package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// directChatPanes は、この巡回で使う pane の写像を1回だけ作る（設計 3-82）。
//
// **候補1件ごとに引き直してはならない。**`panesByCwdErr` は `pane.list` と `agent.list` の
// 2本を投げて機械中の pane の写像を作るので、**候補が N 件あると巡回1回で 2N 本になる。**
//
// **引けなかったときは、その理由を返す。**呼び出し側は「pane が無い」と混ぜてはならない。
//
// ctx: `pane.list` に適用するコンテキスト。
// 戻り値の1つ目: 解決済みの cwd から pane を引く写像。
// 戻り値の2つ目: 引けなかった理由。
func (o *Orchestrator) directChatPanes(ctx context.Context) (map[string]herdr.Pane, error) {
	byCwd, _, err := o.panesByCwdErr(ctx)
	if err != nil {
		return nil, err
	}
	return byCwd, nil
}

// directChatPaneExists は、その issue の worktree に pane が1枚でもあるかを返す（設計 3-82）。
//
// **これが direct chat で pane を作るかどうかの、唯一の判定である。**
//
// **「Claude Code が居るか」では判定しない。**判定する手段が無いためである。
// herdr が agent を登録していないことは、Claude Code が居ないことを意味しない
// （`confirmStartup` の `agent_not_found` の扱いがそう書いている）。**herdr が pane を
// 復元したあと、Claude Code が起動直後から作業を始めると、入力待ちの画面が一度も
// 出ないので登録されない。**そこで `agent.start` を投げると、**人間が話している画面へ
// `claude …` というコマンド行がプロンプトとして届く。**
//
// **だから「pane が1枚でもあれば触らない」に倒す。**取りこぼす側の代償は
// 「人間が自分で Claude Code を立て直す」であり、取り違える側の代償は
// 「会話が汚れる」である。**前者は人間が気づけるが、後者は気づけない。**
//
// panes: この巡回で1回だけ作った pane の写像（`directChatPanes` の戻り値）。
// issue: 対象の issue。
// 戻り値の1つ目: pane が1枚でもあれば true。
// 戻り値の2つ目: worktree の置き場所を決められなかった場合のエラー
// （**エラーのときは「分からない」なので、触らない側に倒すこと**）。
func (o *Orchestrator) directChatPaneExists(panes map[string]herdr.Pane, issue tracker.Issue) (bool, error) {
	loc, warnings, err := workspace.Locate(o.ws.ResolvedRoot(), o.cfg.Herdr.Worktree.BranchTemplate, toIssueRef(issue))
	if err != nil {
		return false, err
	}
	// **正規化で情報が落ちたことを黙って捨てない**（着手の段3 の呼び出しと同じ扱い）。
	for _, w := range warnings {
		o.logger.Warn("worktree の置き場所の正規化で情報が落ちました",
			"identifier", issue.Identifier, "警告", w.Message)
	}
	want, ok := resolvePath(loc.Path)
	if !ok {
		// **まだ worktree が無い。**`resolvePath` は実体のあるパスしか解決しないので、
		// ここは「これから作る」場合に必ず通る。**pane も在りようがない。**
		want = loc.Path
	}
	_, found := panes[want]
	return found, nil
}

// enterDirectChatAfterSetup は、direct chat のために用意した run を direct chat へ入れる（設計 3-82）。
//
// **段11 の代わりである。**指示を送るのは人間なので、continuo は1文字も送らない。
//
// **`SendFirstPrompt` を下ろすことが要である。**下ろさないと、人間が continuo へ返した
// 最初の turn で**1回目の本文（5-3）が送られる。**あの本文は「この issue を読むこと」
// 「紐づく PR も読むこと」から始まるので、**人間が pane で積み上げた誘導を、
// エージェントが最初からやり直す。**issue #263 が消したかった症状そのものである。
//
// ctx: issue へのコメントに適用するコンテキスト。
// rs: 用意した run。
// issue: 対象の issue。
func (o *Orchestrator) enterDirectChatAfterSetup(ctx context.Context, rs *runState, issue tracker.Issue) {
	rs.clearSendFirstPrompt()
	// **用意中の印を下ろしてから、direct chat の印を立てる**（設計 3-82）。
	// 順番を逆にすると、その隙間に巡回が入っても何も起きないが、
	// **落とし忘れると、以後この run は二度と direct chat へ入れない。**
	rs.endDirectChatSetup()
	rs.enterDirectChatMode()
	o.logger.Info("direct chat の pane を用意しました（ここから先は人間が話しかけます。continuo は指示を送りません）",
		"identifier", issue.Identifier, "状態", issue.State)
	o.postDirectChatReady(ctx, issue)
}

// failDirectChatSetup は、direct chat の用意が落ちたときの後始末である（設計 3-82）。
//
// **カンバンへは1バイトも書かない。**通常の着手の失敗は `failure_state` を書くか、
// バックオフして再 dispatch する。**どちらも Status を動かすので、人間が置いたカードが
// `direct_chat_state` から外れる。**外れた次の巡回で pane が閉じ、
// **「人間が直接チャットしていると勝手に切断される」という issue #263 の症状が、
// 新しい経路で再発する。**
//
// **自分が開いた pane は閉じる。**ここへ来るのは「用意を始める前に pane が1枚も
// 無かった」場合だけなので（`directChatPaneExists`）、**閉じる相手は continuo が
// たったいま開いたものである。**人間の会話は入っていない。
//
// **次の巡回でやり直す。**印を外すので、カードが `direct_chat_state` のままなら
// 次の巡回が同じ経路へ入る。
//
// ctx: 呼び出しに適用するコンテキスト。
// rs: 用意に失敗した run。
// issue: 対象の issue。
// err: 失敗の内容。
func (o *Orchestrator) failDirectChatSetup(ctx context.Context, rs *runState, issue tracker.Issue, err error) {
	o.logger.Warn("direct chat の用意に失敗しました（カンバンは触りません。次の巡回でやり直します）",
		"identifier", issue.Identifier, "error", err)
	// **用意中の印を先に下ろす**（設計 3-82）。**下ろさないと巡回が direct chat の印を
	// 立てられないままになるが、それより大事なのは、この時点で `stopWorker` の門が
	// 開いていることである。**開いていないと、自分で開いた pane を閉じられない。
	rs.endDirectChatSetup()
	// **`claimTerminal` を通す**（設計 3-56）。書き戻しが飛んでいたら終わるまで待つ。
	if rs.claimTerminal(ctx) {
		o.stopWorker(ctx, rs)
		o.release(rs)
	}
}

// postDirectChatReady は「話しかけられます」を issue へ1件書く（設計 3-82）。
//
// **これを書かないと、人間はカードを動かしたあと、いつ pane ができたのかを知る手段が
// 無い。**continuo は Status を動かさないので、カンバンは1バイトも変わらない。
//
// **1つの issue につき1回だけ書く。**`direct_chat_state` に置いたままでも巡回は続くので、
// 印を持たないと30秒ごとに1件ずつ積まれる。**印は run の側に持つ**ので、
// continuo を再起動すると1件だけ増えうる。**それは許す。**再起動したことは、
// 人間が pane を作り直させたことを意味するためである。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 対象の issue。
func (o *Orchestrator) postDirectChatReady(ctx context.Context, issue tracker.Issue) {
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		// draft issue にはコメントできない。
		return
	}
	// **先頭の印は付けない。**`postComment` が `self_marker` を付ける。
	body := fmt.Sprintf(
		"この issue の pane を用意しました。**herdr の pane で、そのまま話しかけられます。**\n\n"+
			"- **continuo は指示を送りません。**応答の `%s` の行も読まず、Status も動かしません\n"+
			"- **pane も worktree も閉じません**\n"+
			"- 切りがついたら、カンバンで Status を `%s` のどれかへ戻してください。"+
			"**同じ pane・同じ会話のまま、続きの指示が飛びます**\n",
		o.cfg.Tracker.StatusSignalPrefix,
		strings.Join(o.cfg.Tracker.ActiveStates, " / "),
	)
	// **`o.tracker.PostComment` を直に呼ばない。**手元の絶対パスを縮める1箇所を迂回する
	// （`test/internal/redact` が機械で止めている）。
	if err := o.postComment(ctx, nodeID, body); err != nil {
		o.logger.Warn("direct chat の案内を issue へ書けませんでした（pane は用意できています）",
			"identifier", issue.Identifier, "error", err)
	}
}

// directChatReturnState は、direct chat から戻った先で書くべき Status を返す（設計 3-82）。
//
// **`dispatch_state`（既定 `Ready`）へ戻されたときだけ、`running_state` を書く。**
// 書かないと、エージェントが走っているのにカードは着手待ちに見える。
// **`agent.max_concurrent_agents_by_state` は `running_state` のバケツで数えるので、
// その run は上限の勘定からも外れる。**同じカンバンを見張る別の機械からは、
// 担当者の付いていない着手待ちの issue に見える。
//
// **`running_state` そのものへ戻されたときは書かない。**同じ値の書き込みは省かれるが、
// 取り直しの GraphQL が1本増えるだけで得るものが無い。
//
// cfg: tracker の設定。
// state: 戻った先の Status。
// 戻り値の1つ目: 書くべき Status 名。
// 戻り値の2つ目: 書く必要があるなら true。
func directChatReturnState(cfg config.TrackerConfig, state string) (string, bool) {
	// **`dispatch_state` そのものだけを見る**（設計 3-82）。
	//
	// **「`active_states` にあって `running_state` でない」で判定してはならない。**
	// `active_states` に3つ目の Status を書いている利用者のカードを、
	// **人間が選んだ値から `running_state` へ勝手に書き換えることになる。**
	if !strings.EqualFold(strings.TrimSpace(state), strings.TrimSpace(cfg.DispatchState)) {
		return "", false
	}
	if strings.EqualFold(strings.TrimSpace(cfg.DispatchState), strings.TrimSpace(cfg.RunningState)) {
		// **設定の検査が起動前に弾いているので、ここへは来ない。**来ても書きに行かない。
		return "", false
	}
	return cfg.RunningState, true
}
