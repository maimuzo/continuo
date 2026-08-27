package orchestrator

import (
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
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
		return "", i18n.Errorf(i18n.KeyOrchestratorRenderFirstPromptTemplateUnparsable, err)
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
		return "", i18n.Errorf(i18n.KeyOrchestratorRenderFirstPromptRenderFailed, err)
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
// maxDispatchTurns: 打ち切りまでの上限（`agent.max_dispatch_turns`）。
// missingSignal: 前回の turn に表明が無かったかどうか（設計 3-25 の第3層）。
// runningState: いま書き込まれている作業中の Status 名（`tracker.running_state`）。
// signalPrefix: 表明の印（`tracker.status_signal_prefix`）。
// 戻り値: 送る本文。
func BuildContinuationPrompt(
	turnCount int,
	maxDispatchTurns int,
	missingSignal bool,
	runningState string,
	signalPrefix string,
) string {
	remaining := maxDispatchTurns - turnCount
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
// **この送信は turn 数に数えない。**`max_dispatch_turns` の判定に影響させない。
//
// issueURL: コメントを書く先の issue の URL。
// marker: コメントの先頭に書かせる印（`tracker.comments.marker`）。
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
			"この通知は continuo を起動するたびに1回だけです（同じリポジトリの他の issue では出しません）。",
		owner, repo, owner, repo, reason)
}

// buildHandoffComment は人間へ引き渡すときの通知のコメント本文を作る。
//
// **成果の要約は書かない**（設計 3-29。continuo は代筆しない）。
// 書くのは「なぜ人間へ渡したか」だけである。
//
// **Status を動かした記録も、独立したコメントにせずここへ1行入れる**（設計 3-29）。
// 引き渡しの通知は既に「なぜ人間に渡したか」を書いており、Status の遷移はその一部である。
// 別々に投稿すると、同じことが2件並ぶ。
//
// identifier: issue の識別子。
// reason: 引き渡す理由。
// hc: 「調べるところ」に出す場所。空の項目は行ごと出さない。
// move: Status を動かした記録。**書き込みが起きていなければ1行も出さない。**
// 戻り値: コメント本文。
func buildHandoffComment(identifier, reason string, hc handoffContext, move statusMove) string {
	var b strings.Builder
	fmt.Fprintf(&b, "continuo が %s の作業を人間へ引き渡しました。\n\n理由: %s\n", identifier, reason)

	if line := move.line(); line != "" {
		fmt.Fprintf(&b, "\n%s\n", line)
	}

	// **どこを見に行けばよいかを必ず添える。**理由だけを読んでも、人間は
	// 作業の跡がどこに残っているのかを知る手立てがない。
	// **pane を閉じたあとも残るものだけを出す。**このコメントを人間が読むのは
	// 数十分後であり、そのとき `herdr agent read` は agent_not_found で落ちる
	// （引き渡しの経路はコメントの直後に pane.close を呼ぶ）。
	lines := make([]string, 0, 5)
	if hc.WorktreePath != "" {
		lines = append(lines, fmt.Sprintf("- 作業していた場所: `%s`", hc.WorktreePath))
	}
	if hc.TranscriptPath != "" {
		lines = append(lines, fmt.Sprintf("- Claude Code の会話の記録: `%s`", hc.TranscriptPath))
	}
	// **subagent の記録も出す。**親の記録の末尾には何も残っていないことがあり、
	// そこだけを案内すると「何も無い」で行き止まりになる（issue #65）。
	if len(hc.SubagentTranscripts) > 0 {
		quoted := make([]string, 0, len(hc.SubagentTranscripts))
		for _, p := range hc.SubagentTranscripts {
			quoted = append(quoted, "`"+p+"`")
		}
		// **走行中のものだと分かる印を付ける。**`agent_id` から組み立てた記録は、
		// 止まった直前に動いていたものそのものである。**人間が「これが原因だ」と
		// 当たりを付けられる。**glob で拾ったものは、そう言い切れない。
		label := "- サブエージェントの記録（新しい順）: "
		if hc.SubagentRunning {
			label = "- **止めた時点で走っていた**サブエージェントの記録: "
		}
		lines = append(lines, label+strings.Join(quoted, " / "))
	}
	if hc.SubagentDir != "" {
		lines = append(lines, fmt.Sprintf("- サブエージェントの記録の置き場所: `%s`", hc.SubagentDir))
	}
	if hc.SettingsPath != "" {
		lines = append(lines, fmt.Sprintf("- continuo が渡した設定: `%s`", hc.SettingsPath))
	}
	if len(lines) > 0 {
		b.WriteString("\n【調べるところ】\n")
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

// handoffContext は引き渡しの通知に添える「調べるところ」である。
//
// **空の項目は行ごと出さない。**着手の途中で落ちた run は worktree も pane も
// 持っていないことがあり、空の値を出すと存在しない場所を見に行かせてしまう。
type handoffContext struct {
	// WorktreePath は worktree の絶対パスである。
	WorktreePath string
	// TranscriptPath は Claude Code の会話の記録の絶対パスである。
	//
	// **pane を閉じても残るのはこれである。**hook が渡してくるので、
	// turn を1回も終えていない run では空になる。
	TranscriptPath string
	// SubagentDir は subagent の記録の置き場所の絶対パスである。
	//
	// **親の記録の隣にある**（`<親の記録から `.jsonl` を落としたパス>/subagents`）。
	// subagent を1つも使わなかった turn では作られないので、そのときは空になる。
	SubagentDir string
	// SubagentTranscripts は subagent の記録の絶対パスを、更新時刻の新しい順に
	// 並べたものである（`handoffSubagentLimit` 件まで）。
	//
	// **ここではファイルを走査しない。**値として受け取る（`buildHandoffComment` を
	// 副作用の無い純関数のままにしておくため）。
	SubagentTranscripts []string
	// SubagentRunning は、SubagentTranscripts が「止めた時点で走っていた subagent の
	// `agent_id` から組み立てたもの」であることを表す（設計 3-11）。
	//
	// **偽なら glob で拾ったものである**（更新時刻の新しい順。走行中とは限らない）。
	SubagentRunning bool
	// SettingsPath は continuo が書いた Claude Code の設定ファイルの絶対パスである。
	// **worktree の中ではない**（設計 3-12）。
	SettingsPath string
}

// statusMove は continuo がボードの Status を動かした記録である（設計 3-29）。
//
// **Wrote が偽なら issue に1行も書かない。**item が見えない・書いてはいけない状態だった・
// 既に同じ値だった、のいずれかであり、**ボードは何も動いていない。**
type statusMove struct {
	// Wrote は書き込みの mutation を実際に呼んだかどうかである。
	Wrote bool
	// From は書き込む直前に ID 指定で取り直した Status である。
	// **巡回で読んだ値ではない。**古い値を「何から」として書くと、この記録が嘘をつく。
	From string
	// To は書き込んだ先の Status である。
	To string
}

// newStatusMove は UpdateStatus の戻り値から記録を作る。
//
// sw: UpdateStatus が返した結果。
// target: 書き込みを頼んだ Status。
// 戻り値: 組み立てた記録。
func newStatusMove(sw tracker.StatusWrite, target string) statusMove {
	return statusMove{Wrote: sw.Wrote, From: sw.Previous, To: target}
}

// line は「何から何へ動かしたか」の1行を返す。
//
// 戻り値: 動かしていれば1行。書き込みが起きていなければ空文字列。
func (m statusMove) line() string {
	if !m.Wrote {
		return ""
	}
	return fmt.Sprintf("Status を **%s → %s** へ動かしました。", m.From, m.To)
}

// buildStatusMoveComment は Status を動かした記録のコメント本文を作る（設計 3-29）。
//
// **`tracker.comments.self_marker` は付けない。**`PostComment` が自分で付ける。
//
// **引き渡しの通知を出す経路からは呼ばない。**そちらは通知の本文に1行入れる
// （`buildHandoffComment`）。同じことが2件並ぶのを避けるためである。
//
// m: 動かした記録。**Wrote が真であることは呼び出し側が確かめる。**
// why: 「なぜ」に入れる文。「〜ためです」で終わる形で渡す。
// at: 記録に載せる時刻。**`o.now()` を渡す。**`time.Now` を直に呼ぶと、
// 時刻を差し替えているテストが書けない。
// 戻り値: コメント本文。
func buildStatusMoveComment(m statusMove, why string, at time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", m.line())
	fmt.Fprintf(&b, "- なぜ: %s\n", why)
	fmt.Fprintf(&b, "- いつ: %s\n", at.Format("2006-01-02 15:04 (MST)"))
	b.WriteString("- 書いたのは continuo です（人間の操作ではありません）\n")
	return b.String()
}

// summaryLine は、人間へ見せる理由の1行目だけを返す（ログ用）。
//
// **issue のコメントには【確かめ方】まで全部載せるが、ログには1行目だけを出す**（設計 3-34b）。
// 巡回のたびに数行の案内が流れると、他の行が埋もれて読めなくなる。
//
// reason: 人間へ見せる理由（改行を含みうる）。
// 戻り値: 最初の改行までの部分。改行が無ければそのまま返す。
func summaryLine(reason string) string {
	if i := strings.IndexByte(reason, '\n'); i >= 0 {
		return reason[:i]
	}
	return reason
}
