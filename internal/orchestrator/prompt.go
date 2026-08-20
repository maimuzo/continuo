package orchestrator

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/maimuzo/continuo/internal/tracker"
)

// renderFirstPrompt は1回目のプロンプトを描画する（設計 5-3）。
//
// **`missingkey=error` を付ける。**渡す変数は 5-3 の一覧に載っているものだけであり、
// **未知の変数を書いたテンプレートは描画に失敗させる**（黙って空文字を埋めない）。
// 失敗したらその issue を失敗として扱う。
//
// **issue の本文とコメントは入れない**（設計 3-29）。エージェントが
// `gh issue view <URL> --comments` で自分で読む。
//
// issue: 対象の issue。
// attempt: 試行回数。**1回目は nil を渡す**（仕様 12.3。`text/template` は nil を偽として
// 扱うので `{{if .attempt}}` が正しく動く）。**キーごと省いてはならない。**
// 戻り値の1つ目: 描画したプロンプト本文。
// 戻り値の2つ目: テンプレートの構文が誤っている場合、または一覧に無い変数を参照している
// 場合のエラー。
func (o *Orchestrator) renderFirstPrompt(issue tracker.Issue, attempt *int) (string, error) {
	tmpl, err := template.New("prompt").Option("missingkey=error").Parse(o.promptTemplate)
	if err != nil {
		return "", fmt.Errorf("プロンプトのテンプレートを解析できません: %w", err)
	}

	url := ""
	if issue.URL != nil {
		url = *issue.URL
	}
	data := map[string]any{
		"issue": map[string]any{
			"identifier": issue.Identifier,
			"owner":      issue.Owner,
			"repo":       issue.Repo,
			"number":     issue.Number,
			"url":        url,
			"title":      issue.Title,
			"state":      issue.State,
			"labels":     issue.Labels,
		},
		"attempt": attemptValue(attempt),
	}

	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("プロンプトのテンプレートを描画できません（5-3 の一覧に無い変数を書いていませんか）: %w", err)
	}
	return b.String(), nil
}

// attemptValue は `.attempt` に入れる値を返す。
//
// attempt: 試行回数。nil なら1回目である。
// 戻り値: 1回目は nil、それ以外は回数。
func attemptValue(attempt *int) any {
	if attempt == nil {
		return nil
	}
	return *attempt
}

// BuildContinuationPrompt は2回目以降のプロンプトを組み立てる（設計 5-4）。
//
// **本文（5-3）は送り直さない。**`SPEC.md` 7.1 が「継続の turn は、既にスレッドの履歴に
// ある元のタスクプロンプトを送り直すのではなく、継続の指示だけを送るべきである」と
// 定めているためである。
//
// **この文面はテンプレートを通さない。**Go が文字列として組み立てる。「あと何回で
// 打ち切るか」は continuo が持っている状態からしか作れず、利用者に書き換えさせると
// その状態を渡すための変数を追加で公開することになる。
//
// turnCount: この turn が continuo にとって何回目か（1始まり）。
// maxTurns: 打ち切りまでの上限（`agent.max_turns`）。
// missingSignal: 前回の turn に表明が無かったかどうか（設計 3-25 の第3層）。
// runningState: いま書き込まれている作業中の Status 名（`tracker.running_state`）。
// signalPrefix: 表明の印（`tracker.status_signal_prefix`）。
// 戻り値: 送る本文。
func BuildContinuationPrompt(
	turnCount int,
	maxTurns int,
	missingSignal bool,
	runningState string,
	signalPrefix string,
) string {
	remaining := maxTurns - turnCount
	if remaining < 0 {
		remaining = 0
	}

	var b strings.Builder
	fmt.Fprintf(&b, "続けてください。この確認は %d 回目です。あと %d 回で打ち切ります。\n", turnCount, remaining)
	if missingSignal {
		fmt.Fprintf(&b,
			"\n前回の応答に %s の行がありませんでした。Status がまだ %s のままです。"+
				"作業の状態を、応答の中に1行で書いてください。\n",
			signalPrefix, runningState)
	}
	fmt.Fprintf(&b,
		"\n権限で拒否された操作があれば、その内容を応答に書いて %s blocked を出してください。\n",
		signalPrefix)
	return b.String()
}

// buildCommentRequestPrompt は「作業の内容を issue のコメントに書いてください」だけを送る
// 本文を組み立てる（設計 3-25 の9段の段7）。
//
// **この送信は turn 数に数えない。**`max_turns` の判定に影響させない。
//
// issueURL: コメントを書く先の issue の URL。
// marker: コメントの先頭に書かせる印（`tracker.provider.comments.marker`）。
// 戻り値: 送る本文。
func buildCommentRequestPrompt(issueURL, marker string) string {
	var b strings.Builder
	b.WriteString("この作業で何をしたかを、issue のコメントに書いてください。\n\n")
	fmt.Fprintf(&b, "    gh issue comment %s --body \"%s\n    ここに何をしたかを書く\"\n\n", issueURL, marker)
	fmt.Fprintf(&b, "コメントの先頭には必ず %s の1行を入れてください。\n", marker)
	return b.String()
}

// buildUntrustedComment は未信頼のリポジトリについて人間へ知らせるコメント本文を作る
// （設計 3-6）。
//
// **同じリポジトリにつき1回だけ書く。**キーは `<owner>/<repo>` であり、issue ごとではない。
//
// owner: リポジトリの所有者名。
// repo: リポジトリ名。
// reason: 信頼の検査が返した理由。
// 戻り値: コメント本文。
func buildUntrustedComment(owner, repo, reason string) string {
	return fmt.Sprintf(
		"このリポジトリ（%s/%s）は Claude Code に信頼登録されていないため、continuo は着手できません。\n\n"+
			"信頼していないフォルダでは Claude Code の hook が1つも動かず、turn の終わりを検知できません。\n\n"+
			"直し方。WORKFLOW.md の `trust.repositories` に `%s/%s` を書き足してから、"+
			"`continuo trust` を実行してください。\n"+
			"何を許すことになるかは `continuo trust --dry-run` で先に見られます。\n\n"+
			"検査の結果: %s\n\n"+
			"このリポジトリについての通知は1回だけです（同じリポジトリの他の issue では出しません）。",
		owner, repo, owner, repo, reason)
}

// buildHandoffComment は人間へ引き渡すときの通知のコメント本文を作る。
//
// **成果の要約は書かない**（設計 3-29。continuo は代筆しない）。
// 書くのは「なぜ人間へ渡したか」だけである。
//
// identifier: issue の識別子。
// reason: 引き渡す理由。
// 戻り値: コメント本文。
func buildHandoffComment(identifier, reason string) string {
	return fmt.Sprintf("continuo が %s の作業を人間へ引き渡しました。\n\n理由: %s", identifier, reason)
}
