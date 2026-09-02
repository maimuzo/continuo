package orchestrator

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/tracker"
)

// RepoNoticeReason は、コードのリポジトリまわりで着手できなかった理由の種類である
// （設計 issue144 の 11e / 11f）。
//
// **担当者の関門（GateReason）とは別に持つ。**あちらは
// [Orchestrator.clearGate] が「関門より前で飛ばした」ときに数え直すので、
// **preflight より前で止まるこの経路を数え続けられない。**
type RepoNoticeReason string

const (
	// RepoNoticeCodeRepoUndecided は、Development のリンクから
	// コードのリポジトリを1つに決められなかったことを表す（設計 issue144 の 11e の文面1）。
	RepoNoticeCodeRepoUndecided RepoNoticeReason = "code_repo_undecided"
	// RepoNoticeCodeRepoMismatch は、worktree の置き場所と、
	// いまリンクされているコードのリポジトリが食い違うことを表す（設計 issue144 の 11f の文面3）。
	RepoNoticeCodeRepoMismatch RepoNoticeReason = "code_repo_mismatch"
)

// repoNotice は、1つの issue の1つの理由についての案内の状態である（設計 3-68）。
//
// **メモリにしか置かない。**再起動すると消えるので「1回の起動につき1回」になる。
// **その旨を本文の末尾に1行書く。**
type repoNotice struct {
	// Count は、同じ理由で続けて止めた回数である。
	Count int
	// FirstSeenAt は、その理由で最初に止めた時刻である。
	FirstSeenAt time.Time
	// Posted は、issue へ案内を書いたことを表す。
	//
	// **投稿に失敗しても真にする。**成否で分けると、投稿が失敗し続ける issue へ
	// 巡回のたびにコメントを積むことになる。
	Posted bool
}

// repoNoticeKey は、印の鍵を組み立てる。
//
// **鍵は「issue の identifier ＋ 理由の種類」である**（設計 issue144 の 11e）。
// 理由が変わったら別の印になるので、数え直しから始まる。
//
// identifier: `<owner>/<repo>#<番号>`。
// reason: 止めた理由の種類。
// 戻り値: 印の鍵。
func repoNoticeKey(identifier string, reason RepoNoticeReason) string {
	return identifier + "\x00" + string(reason)
}

// noteRepoIssue は、コードのリポジトリまわりで止めたことを記録し、
// issue へ案内を書くべきかを返す（設計 3-68 / issue144 の 11e）。
//
// **1回目では書かない。**カンバンの候補一覧は GitHub のサーバ側の検索結果であり、
// **索引の反映が遅れて1巡回だけ答えが揺れることがある**（3-34）。
// 1回目で書くと、揺れただけの issue に誤った案内が1件残る。**消す手段は無い。**
//
// identifier: `<owner>/<repo>#<番号>`。
// reason: 止めた理由の種類。
// 戻り値: まだ書いておらず、3回続けて止まり、かつ最初に止めてから60秒たっていれば true。
func (o *Orchestrator) noteRepoIssue(identifier string, reason RepoNoticeReason) bool {
	now := o.now()
	key := repoNoticeKey(identifier, reason)

	o.mu.Lock()
	defer o.mu.Unlock()

	n := o.repoNotices[key]
	if n == nil {
		n = &repoNotice{Count: 0, FirstSeenAt: now}
		o.repoNotices[key] = n
	}
	n.Count++
	if n.Posted {
		return false
	}
	if n.Count < noticeMinCount || now.Before(n.FirstSeenAt.Add(noticeMinAge)) {
		return false
	}
	// **投稿の前に印を付ける。**投稿の成否で分けると、失敗し続ける issue へ
	// 巡回のたびにコメントを積むことになる。
	n.Posted = true
	return true
}

// clearRepoIssue は、その理由では止まらなくなったことを記録する（設計 issue144 の 11e）。
//
// **直したあと再発したら、もう一度知らせる。**`clearUntrusted` と同じ形である。
//
// identifier: `<owner>/<repo>#<番号>`。
// reason: 取り消す理由の種類。
func (o *Orchestrator) clearRepoIssue(identifier string, reason RepoNoticeReason) {
	key := repoNoticeKey(identifier, reason)
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.repoNotices, key)
}

// codeOwnerRepoOf は、その issue のコードのリポジトリを所有者名とリポジトリ名で返す。
//
// **リンクが0本なら issue のリポジトリと同じ値になる。**
// **draft issue は両方とも空文字になる**（owner も repo も持たないため）。
//
// issue: 対象の issue。
// 戻り値の1つ目: コードのリポジトリの所有者名。
// 戻り値の2つ目: コードのリポジトリ名。
func codeOwnerRepoOf(issue tracker.Issue) (string, string) {
	owner, repo := issue.CodeOwnerRepo()
	if owner == "" || repo == "" {
		return issue.Owner, issue.Repo
	}
	return owner, repo
}

// noteCodeRepoUndecided は、コードのリポジトリを決められないことを人間へ知らせる
// （設計 issue144 の 11e の文面1）。
//
// **Status を1バイトも動かさない。**動かさないので、書かないと誰にも届かない。
// **ログだけにしない**（3-68 が「ログは pane を見ていない限り誰にも届かない」と名指ししている）。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 着手しなかった issue。
func (o *Orchestrator) noteCodeRepoUndecided(ctx context.Context, issue tracker.Issue) {
	o.logger.Warn("リンクされた branch が別々のリポジトリを指すので着手しません（何も書き換えていません）",
		"identifier", issue.Identifier,
		"リンク", strings.Join(linkedBranchLines(issue), " / "))

	if !o.noteRepoIssue(issue.Identifier, RepoNoticeCodeRepoUndecided) {
		return
	}
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		o.logger.Warn("draft issue にはコメントできないのでログだけにします", "identifier", issue.Identifier)
		return
	}
	body := buildCodeRepoUndecidedComment(issue)
	if err := o.postComment(ctx, nodeID, body); err != nil {
		o.logger.Warn("コードのリポジトリを決められないことを issue へ書けませんでした",
			"identifier", issue.Identifier, "error", err)
	}
}

// noteCodeRepoMismatch は、worktree の置き場所といまのコードのリポジトリが食い違うことを
// 人間へ知らせる（設計 issue144 の 11f の文面3）。
//
// **候補から外すだけで、worktree も branch も1バイトも消さない。**
// **それでも issue へ書く。**Status が動かないので、書かないとその issue は
// 永久に着手されないまま、画面のどこにも理由が出ない。
//
// ctx: 呼び出しに適用するコンテキスト。
// issue: 取り直した issue。
// worktreePath: 食い違った worktree の絶対パス。
// pathRepo: 置き場所の2・3階層目（`<owner>/<repo>`）。
func (o *Orchestrator) noteCodeRepoMismatch(
	ctx context.Context, issue tracker.Issue, worktreePath, pathRepo string,
) {
	codeOwner, codeRepo := codeOwnerRepoOf(issue)
	linked := codeOwner + "/" + codeRepo

	o.logger.Warn("worktree の置き場所がコードのリポジトリと食い違うので候補にしません（消しません）",
		"path", worktreePath, "置き場所", pathRepo, "トラッカーが答えたコードのリポジトリ", linked)

	if !o.noteRepoIssue(issue.Identifier, RepoNoticeCodeRepoMismatch) {
		return
	}
	nodeID := issueNodeID(issue)
	if nodeID == "" {
		o.logger.Warn("draft issue にはコメントできないのでログだけにします", "identifier", issue.Identifier)
		return
	}
	body := buildCodeRepoMismatchComment(worktreePath, pathRepo, linked)
	if err := o.postComment(ctx, nodeID, body); err != nil {
		o.logger.Warn("置き場所とコードのリポジトリの食い違いを issue へ書けませんでした",
			"identifier", issue.Identifier, "error", err)
	}
}

// linkedBranchLines は、リンクされた branch を「リポジトリ  branch 名」の行で返す。
//
// issue: 対象の issue。
// 戻り値: 行の並び。リンクが0本なら空。
func linkedBranchLines(issue tracker.Issue) []string {
	lines := make([]string, 0, len(issue.LinkedBranches))
	for _, l := range issue.LinkedBranches {
		lines = append(lines, fmt.Sprintf("%s  %s", l.NameWithOwner, l.Branch))
	}
	return lines
}

// buildCodeRepoUndecidedComment は、コードのリポジトリを決められないことを知らせる本文を作る
// （設計 issue144 の 11e の文面1）。
//
// **`internal/i18n` は使わない。**`internal/orchestrator` の人間向けの文言は
// 「まとめて資源へ移す」と決まっており（buildGatedComment に同じ断りがある）、
// **この issue だけ先に移すと揃わない。**
//
// **HTML の目印を本文に置かない。**`postComment` が本文の先頭に自分の目印を付け、
// `FetchComments` はその目印の付いた自分のコメントを結果から外すので、
// **置いても読み返せない。**
//
// issue: 着手しなかった issue。
// 戻り値: コメント本文。
func buildCodeRepoUndecidedComment(issue tracker.Issue) string {
	var b strings.Builder
	b.WriteString("この issue には、別々のリポジトリの branch が2本以上リンクされています。\n")
	b.WriteString("どちらのリポジトリで作業すべきかを continuo が決められないので、着手していません。\n")
	b.WriteString("**カンバンの Status も worktree も、1バイトも書き換えていません。**\n\n")
	for _, line := range linkedBranchLines(issue) {
		b.WriteString("    " + line + "\n")
	}
	if len(issue.LinkedBranches) == 0 {
		b.WriteString("    （リンクの本数が取得の窓に収まらないので、中身を出せません）\n")
	}
	b.WriteString("\nDevelopment のリンクを1本にしてから、Status を着手待ちへ戻してください。\n")
	// **日本語を `fmt.Sprintf` の書式へ入れない**（`internal/orchestrator` はまだ資源へ
	// 移していない package であり、新しく足すのは
	// `TestDesign_画面に出す文言を日本語で直に書いていない` が止める）。
	b.WriteString("確かめ方: gh issue develop --list " + strconv.Itoa(issue.Number) +
		" --repo " + issue.Owner + "/" + issue.Repo + "\n")
	b.WriteString("\nこの通知は、continuo の1回の起動につき1回だけ出ます。\n")
	return b.String()
}

// buildCodeRepoMismatchComment は、置き場所とコードのリポジトリの食い違いを知らせる本文を作る
// （設計 issue144 の 11f の文面3）。
//
// **「消していない」を本文に書く。**読む人がまず知りたいのはそこである。
//
// worktreePath: 食い違った worktree の絶対パス。
// pathRepo: 置き場所の2・3階層目（`<owner>/<repo>`）。
// linkedRepo: いまリンクされているコードのリポジトリ（`<owner>/<repo>`）。
// 戻り値: コメント本文。
func buildCodeRepoMismatchComment(worktreePath, pathRepo, linkedRepo string) string {
	return "この issue の worktree の置き場所が、いまリンクされているコードのリポジトリと食い違うので、\n" +
		"着手していません。**worktree も branch も消していません。**\n\n" +
		"    worktree    " + worktreePath + "\n" +
		"    置き場所     " + pathRepo + "\n" +
		"    リンクの先   " + linkedRepo + "\n\n" +
		"Development のリンクを元に戻すか、この worktree を手で片付けてから、" +
		"Status を着手待ちへ戻してください。\n\n" +
		"この通知は、continuo の1回の起動につき1回だけ出ます。\n"
}
