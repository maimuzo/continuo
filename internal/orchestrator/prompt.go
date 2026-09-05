package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/prompt"
	"github.com/maimuzo/continuo/internal/tracker"
)

// renderFirstPrompt は1回目のプロンプトを組み立てる（設計 5-3 / 5-3c）。
//
// **3つの断片を別々に変数展開してから連結する**（internal/prompt）。
// **`missingkey=error` を付ける。**渡す変数は 5-3 の一覧に載っているものだけであり、
// **未知の変数を書いたテンプレートは変数展開に失敗させる**（黙って空文字を埋めない）。
// 失敗したらその issue を失敗として扱う。
//
// **issue の本文とコメントは入れない**（設計 3-29）。エージェントが
// `gh` の JSON 出力（`gh issue view --json comments` と REST）で自分で読む。
// **テキスト表示（`--comments`）は使わせない**（設計 3-72）。
//
// issue: 対象の issue。
// attempt: 試行回数。**1回目は nil を渡す**（仕様 12.3。`text/template` は nil を偽として
// 扱うので `{{if .attempt}}` が正しく動く）。**キーごと省いてはならない。**
// 戻り値の1つ目: 組み立てたプロンプト本文。
// 戻り値の2つ目: テンプレートの構文が誤っている場合、または一覧に無い変数を参照している
// 場合のエラー。**どの断片の何行目かがエラーの文言に入る。**
func (o *Orchestrator) renderFirstPrompt(issue tracker.Issue, attempt *int) (string, error) {
	// **変数はここで組み立てない**（issue #183）。`continuo prompt --show --url` も
	// **同じ `prompt.RenderData` を呼ぶ。**別々に組み立てると、片方を直したときにずれ、
	// **あのコマンドが「送られる文面」ではないものを見せることになる。**
	data := prompt.RenderData(issue, attempt, o.cfg.Tracker.Provider.Handoff.ProgressIntervalMs)

	out, err := o.promptFragments.Render(data)
	if err != nil {
		return "", i18n.Errorf(i18n.KeyOrchestratorRenderFirstPromptRenderFailed, err)
	}
	return out, nil
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
// **進捗報告の印を付けるなと明示する**（issue #178）。段1 は
// 進捗報告の印が付いたコメントを、成果の報告として数えない。
// **一方で組み込みの指示書は「いちばん下のコメントが自分の進捗報告なら、その1件に書き足す」と
// 頼んでいる**（設計 5-3j）。**言わずに頼むと、エージェントは指示どおり進捗報告へ書き足し、
// 段8 がまた「書かれていない」と判定して、段9 で `failure_state` へ落ちる。**
// **書いたのに人間へ引き渡される。**
//
// **印そのものは埋めない**（`bareProgressMarker`）。埋めると、エージェントが
// 「この印は付けていません」と書き写しただけで、その報告が途中経過として捨てられる。
// **照合の側も先頭の印の並びだけを見るようにしてあるが**（`handoff.StartsAsProgressReport`）、
// **送る側でも埋めない。**守りは2つとも要る。
//
// **禁じるのは「囲み付き」のほうである**（issue #178）。報告を捨てるのは
// `<!` から始まる形で、**囲みを外した形は捨てない。**
// **囲みを外した形のほうを禁じると、捨てられるほうが1度も禁じられないまま残る。**
// しかも次の行が「本文の中では囲みを外した形で」と言うので、
// **外した形だけが禁止だと読める。**囲み付きを先頭に置いたエージェントの報告は
// `hasRunComment` に飛ばされ、**書いたのに `failure_state` へ落ちる。**
// 書き分けは [docs/upgrading.md:239-245](docs/upgrading.md#L239-L245) に揃える。
//
// issueURL: コメントを書く先の issue の URL。
// marker: コメントの先頭に書かせる印（`tracker.comments.marker`）。
// 戻り値: 送る本文。
func buildCommentRequestPrompt(issueURL, marker string) string {
	var b strings.Builder
	b.WriteString("この作業で何をしたかを、issue のコメントに書いてください。\n\n")
	fmt.Fprintf(&b, "    gh issue comment %s --body \"%s\n    ここに何をしたかを書く\"\n\n", issueURL, marker)
	fmt.Fprintf(&b, "コメントの先頭には必ず %s の1行を入れてください。\n", marker)
	// **「その印」と書かない**（issue #178）。**直前の文が名乗っているのは `marker`
	// （エージェントの印）である。**取り違えてそちらを外されると、`c.IsAgent` が偽になり、
	// **書いたのに `failure_state` へ落ちる。**この経路が防ごうとした結末そのものである。
	// **2度目も名前を書き切る。**値は `config.ProgressMarker` から作るので、定義は1つのままである。
	fmt.Fprintf(&b,
		"\n**新しく1件投稿してください。**途中経過の報告（本文のいちばん上の印の並びに、"+
			"囲み付きの %[1]s（`<!` から始まるあの形）が入っているもの）へ書き足すと、"+
			"continuo はそれを成果の報告として数えません。\n"+
			"**この報告の先頭に、囲み付きの %[1]s を置かないでください。**"+
			"%[2]s のほうは、上のとおり必ず入れてください。\n"+
			"**本文の中で %[1]s について書くときは、囲みを外した %[1]s の形で書いてください。**\n"+
			"囲み付きのまま書くと、次の途中経過の報告が、この報告へ書き足されます。\n"+
			"**%[2]s のほうは、囲みを外さないでください。**外すと、この報告が数えられません。\n",
		bareProgressMarker(), marker)
	return b.String()
}

// bareProgressMarker は、進捗報告の印から HTML のコメントの囲みを外した文字列を返す。
//
// **エージェントへ送る文面に、囲み付きの印そのものを埋めてはならない**（issue #178）。
//
// **成果の報告を数えるかどうかは `handoff.StartsAsProgressReport` が決めており、
// あちらは本文の先頭にある印の並びしか見ない。**書き写しただけでは捨てられない。
// **埋めてはいけない理由は別にある。**エージェント自身が「書き足す先」を探す問い合わせ
// （組み込みの 5-3 の段1。`.body | contains("<!-- " + "continuo:progress" + " -->")`）は
// **印を本文のどこからでも拾う。**囲み付きで引用した成果の報告があると、
// **次の進捗報告がその成果の報告へ書き足され、読む人には別の話が1件に混ざって見える。**
//
// **値は `config.ProgressMarker` から作る。**印を2箇所で定義すると、片方を直したときにずれる。
//
// 戻り値: `continuo:progress` のような、囲みを外した印の中身。
func bareProgressMarker() string {
	s := strings.TrimPrefix(config.ProgressMarker, commentOpenMarker)
	s = strings.TrimSuffix(s, commentCloseMarker)
	return strings.TrimSpace(s)
}

// commentOpenMarker と commentCloseMarker は HTML のコメントの囲みである。
const (
	commentOpenMarker  = "<!--"
	commentCloseMarker = "-->"
)

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

// buildGatedComment は、着手の関門で止めたことを人間へ知らせるコメント本文を作る
// （issue #134 / #136 / #140）。
//
// **担当者のログイン名を1文字も書かない**（設計 8-1）。コメントを書き換える経路も
// 消す経路も無いので、担当者が変わっても書き直せない。**書き直すとは「もう1件足す」ことでしかなく、
// 担当者を付け替えるたびに古い案内が積まれる。**
// **やることは担当者が誰であっても同じである**（担当者を外すか、処理させたい PC の gh の持ち主にする）。
// いま誰が付いているかは issue の画面の右側に出ているし、
// WARN の1行とダッシュボードにも常に最新の値が出る。
//
// **1行目の印は理由ごとに違う。**再起動をまたいだ照合に使うので、
// 理由が変わったら別の案内として1回だけ書けるようにする（設計 6-5）。
// **`<!-- continuo:self -->` は付けない。**`postComment` が先頭に足す。
//
// **`internal/i18n` は使わない。**`internal/orchestrator` の人間向けの文言は
// 「まとめて資源へ移す」と決まっており、この issue だけ先に移すと揃わない。
//
// reason: 止めた理由の種類。**`GateReasonManyAssigneesWithSelf` はここへ来ない**
// （その理由では issue へ1バイトも書かない。設計 8-3）。来たら空文字を返す。
// 戻り値: コメント本文（2行目以降）。
func buildGatedComment(reason GateReason) string {
	switch reason {
	case GateReasonHumanAssigned:
		return gateNoticeMarker(reason) + "\n" +
			"この issue には担当者が付いているため、continuo は着手しません。\n" +
			"continuo が付けた担当ではないので、人間が作業中だと判断しています。\n\n" +
			"着手させるには、GitHub の画面でその担当者を外してください。\n\n" +
			"担当者を、この issue を処理させたい PC の gh の持ち主にしておく道もあります。その PC が着手します。\n" +
			"ただし、そのアカウントを使う continuo が1台だけのときに限ります。\n" +
			"同じアカウントで2台以上動かしていると、2台とも「自分の担当だ」と読み、同時に着手します。\n\n" +
			"どちらの道も、いま付いている担当者が作業中でないことを確かめてから行ってください。\n\n" +
			"この案内は、この理由につき、そのアカウントにつき1回だけ書きます。\n"
	case GateReasonManyAssignees:
		return gateNoticeMarker(reason) + "\n" +
			"この issue には担当者が2人以上付いているため、continuo は着手しません。\n" +
			"人間が作業を分担していると判断しています。\n\n" +
			"着手させるには、GitHub の画面で担当者を1人も付いていない状態にしてください。\n\n" +
			"この案内は、この理由につき1回だけ書きます。\n" +
			"ただし、この issue が着手待ちの一覧から一度外れて戻ると、もう一度書くことがあります。\n" +
			"外れるのは、continuo を再起動したとき、Status を着手待ちから一度外して戻したとき、\n" +
			"GitHub の検索の反映が遅れて1巡回だけ一覧に出なかったときです。\n" +
			"この経路は issue のコメントを読まないので、前に書いたことを手元から確かめられません。\n"
	default:
		// **知らない理由で issue へ書かない。**空文字なら postGateNotice が投稿を見送る。
		return ""
	}
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
	// **止めた時点で走っていたバックグラウンド処理を出す**（設計 3-81）。
	// **`shell`（`run_in_background` の Bash）も入る。**pane を閉じると途中で終わるので、
	// **人間が「何が道連れになったか」を知る唯一の手がかりである。**
	if len(hc.BackgroundTasks) > 0 {
		quoted := make([]string, 0, len(hc.BackgroundTasks))
		for _, t := range hc.BackgroundTasks {
			quoted = append(quoted, "`"+t+"`")
		}
		line := "- **止めた時点で走っていたバックグラウンド処理**: " + strings.Join(quoted, " / ")
		// **切り捨てたなら、切り捨てたと書く**（設計 3-81b）。**黙って上から数件だけ出すと、
		// 読んだ人は「道連れになったのはこれで全部だ」と読む。**subagent の記録の側は
		// 置き場所のディレクトリを併せて出すので追えるが、**こちらには追える先が無い。**
		if hc.BackgroundTasksOmitted > 0 {
			line += fmt.Sprintf("（ほかに %d 件）", hc.BackgroundTasksOmitted)
		}
		lines = append(lines, line)
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
	// BackgroundTasks は、止めた時点で「まだ走っている」と申告されていた
	// バックグラウンド処理の名前である（設計 3-81）。
	//
	// **種類で絞らない。**`shell`（`run_in_background` の Bash）も入る。
	// **件数は `handoffBackgroundTaskLimit` 件までに切ったものを渡すこと。**
	// 申告は hook から来る外部入力であり、そのまま issue のコメントへ載る（設計 3-23）。
	BackgroundTasks []string
	// BackgroundTasksOmitted は、上の切り捨てで落とした件数である（設計 3-81b）。
	//
	// **0 でなければ「ほかに N 件」と書く。****黙って上から数件だけ出すと、
	// 読んだ人は「道連れになったのはこれで全部だ」と読む。**
	BackgroundTasksOmitted int
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
