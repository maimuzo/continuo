package doctor

import (
	"fmt"
	"io"
	"strings"

	"github.com/maimuzo/continuo/internal/i18n"
)

// 検査の見出し語のキーである（設計 3-32 / 3-35）。**見出し語はここでしか宣言しない。**
//
// **検査がいくつあるかは、この一覧を数えて得る。**
// 数を別の場所に書き写さない。書き写せば、検査を足したときに必ず片方だけが残る。
//
// **持つのは画面に出る語そのものではなく、文言を引くキーである**（設計 3-35）。
// 語は internal/i18n の資源にあり、表示するのは Report.Write だけである。
// 見出し語をここ以外で組み立ててはならない。人間はこの語で「どの前提が欠けたか」を
// 覚えるので、揺れると同じものが2つの名前で呼ばれることになる。
const (
	// LabelConfig は WORKFLOW.md が読めて front matter が検証を通るかの検査である。
	LabelConfig = i18n.KeyDoctorLabelConfig
	// LabelHerdr は herdr の socket の ping の protocol が設定と一致するかの検査である。
	LabelHerdr = i18n.KeyDoctorLabelHerdr
	// LabelClaude は `claude.kind` の実行ファイルが PATH にあるかの検査である。
	//
	// **これが無いと、着手は段9 まで進んでから必ず失敗する。**herdr は pane を作れるが、
	// そこで起動するはずの Claude Code が見つからず `unknown` を返す（2026-08-21 に実際に起きた）。
	LabelClaude = i18n.KeyDoctorLabelClaude
	// LabelRuntimeDir は hook を受ける socket を実際に置けるかの検査である。
	//
	// **文字列を組み立てるだけでは足りない。**実際にディレクトリを作り、
	// unix socket を listen して、すぐ閉じるところまで通す。
	//
	// **これが無かったとき、8項目すべてが ✓ で「足りないものはありません」と出たのに、
	// 起動だけが `mkdir /run/user/1000: permission denied` で落ちた**（issue #9）。
	LabelRuntimeDir = i18n.KeyDoctorLabelRuntimeDir
	// LabelGHAuth は `gh auth status` の scope に project が単独で並んでいるかの検査である。
	LabelGHAuth = i18n.KeyDoctorLabelGHAuth
	// LabelBoard は Bootstrap が通り、active_states の選択肢名が全部あるかの検査である。
	LabelBoard = i18n.KeyDoctorLabelBoard
	// LabelStatusNames は、設定に書いた Status 名と紛らわしい選択肢がボードに無いかの検査である。
	//
	// **`✗` にしない。**紛らわしいだけでは continuo は動く。だが取り違えたまま無人で回すと、
	// **人間が作業中の issue にエージェントが着手する。**
	LabelStatusNames = i18n.KeyDoctorLabelStatusNames
	// LabelClone は対象リポジトリが `ghq list -p -e` で見つかるかの検査である。
	LabelClone = i18n.KeyDoctorLabelClone
	// LabelTrust は対象リポジトリの clone のパスが `~/.claude.json` で承認済みかの検査である。
	LabelTrust = i18n.KeyDoctorLabelTrust
	// LabelCredentials は rate_limit の設定に応じて環境変数かファイルがあるかの検査である。
	LabelCredentials = i18n.KeyDoctorLabelCredentials
	// LabelClaudeHome は Claude Code の設定ディレクトリに実際に書けるかの検査である。
	//
	// **文字列を組み立てるだけでは足りない。**`~/.claude/session-env/<使い捨ての名前>` を
	// 実際に作って消す。Claude Code は SessionStart hook を走らせる前にここへ書き、
	// continuo はその hook を必ず張るので、**ここが書けないと issue は1件も始まらない。**
	//
	// **これが無かったとき、利用者のホームが read-only になった環境で、
	// doctor は9項目すべてを `✗` か `!` にして本当の原因を1つも指摘しなかった**（issue #11）。
	LabelClaudeHome = i18n.KeyDoctorLabelClaudeHome
	// LabelWorkspaceRoot は worktree の置き場所（`workspace.root`）に実際に書けるかの検査である。
	//
	// **ここが書けないと、着手は worktree を用意する段で必ず落ちる。**
	LabelWorkspaceRoot = i18n.KeyDoctorLabelWorkspaceRoot
	// LabelCleanupStates は `cleanup.on_states` が `tracker.terminal_states` に
	// 収まっているかの検査である（設計 3-9e。issue #35）。
	//
	// **`✗` にしない。**噛み合っていなくても continuo は起動し、走る。だが
	// **「終わっていない」と判定した直後に worktree を片付ける**という筋の通らない
	// 動きになる。**起動を止めると、いま動いている人の continuo が版を上げた瞬間に
	// 起動しなくなる**ので、警告に留める。
	LabelCleanupStates = i18n.KeyDoctorLabelCleanupStates
	// LabelRewriteKeys は `tracker.automated_state_rewrite` のキーがボードの Status の
	// 選択肢にあるかの検査である（設計 3-57。issue #67）。
	//
	// **`✗` にしない。**キーはボードに実在しなくてよく、無ければその行が引かれないだけである。
	// **`✗` にすると、ボードの自動化をやめて選択肢を消した人が抜け出せなくなる。**
	//
	// **黙って通してもいけない。**綴りを打ち間違えた行は一度も効かないまま死ぬのに、
	// **起動時の警告は `continuo doctor` には出てこない**（doctor は tracker のログを捨てる）。
	// **ここが、打ち間違いを人間に見せる唯一の場所である。**
	LabelRewriteKeys = i18n.KeyDoctorLabelRewriteKeys
	// LabelMissingKeys は、雛形にあって `WORKFLOW.md` に書かれていない設定項目が
	// あるかの検査である（設計 3-74。issue #85）。
	//
	// **`✗` にしない。**書かれていなくても continuo は起動し、走る（Go が持つ既定値が使われる）。
	// **`✗` にすると、版を上げた瞬間に、いま動いている人の起動が止まる。**
	//
	// **黙って通してもいけない。**版を上げて増えた設定項目は、
	// **リリースノートを読まないかぎり、存在に気づく手段が1つも無い。**
	// **ここが、増えた項目を人間に見せる唯一の場所である。**
	LabelMissingKeys = i18n.KeyDoctorLabelMissingKeys
)

// LabelText は見出し語のキーを、いま使っている言語の語に直す。
//
// **表示するときだけ呼ぶ。**Result が持つのはキーであって語ではない。
//
// label: 見出し語のキー。
// 戻り値: 画面に出す語。
func LabelText(label i18n.Key) string { return i18n.T(label) }

// Symbol は検査1件の結果である（設計 3-32 の3値）。
type Symbol string

const (
	// SymbolOK は「通った」である。終了コードに影響しない。
	SymbolOK Symbol = "✓"
	// SymbolMissing は「足りない」である。**1つでもあれば終了コードは 1 になる。**
	SymbolMissing Symbol = "✗"
	// SymbolUnknown は「確かめられなかった」と「確かめたが、そのままだと取り違えやすい」である。
	// **どちらも起動を止めるほどではないので、終了コードは 0 のままにする。**
	SymbolUnknown Symbol = "!"
)

// worse は2つの記号のうち「重いほう」を返す（✗ > ! > ✓）。
//
// **1つの見出し語が対象を複数持つときに使う**（clone と信頼登録はボードに載っている
// リポジトリの数だけ対象がある）。1件でも足りなければ見出し語全体を ✗ にする。
//
// a: 比較する記号。
// b: 比較する記号。
// 戻り値: 重いほうの記号。
func worse(a, b Symbol) Symbol {
	rank := func(s Symbol) int {
		switch s {
		case SymbolMissing:
			return 2
		case SymbolUnknown:
			return 1
		default:
			return 0
		}
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// Result は検査1件の結果である。
type Result struct {
	// Label は見出し語のキーである（LabelConfig などの定数のいずれか）。
	// **画面に出る語そのものではない。**語に直すのは Write だけである（設計 3-35）。
	Label i18n.Key
	// Symbol は3値の結果である。
	Symbol Symbol
	// Detail は記号の右に出す1行の説明である。**なぜその記号になったか**を書く。
	Detail string
	// Notes は対象ごとの内訳である（リポジトリが複数あるときの1件ずつの結果）。
	// Detail の下に、同じ桁位置で並べて出す。
	Notes []string
	// Remedies は直し方である。`→ ` を頭に付けて出す。
	Remedies []string
}

// Report は検査結果をまとめたものである。
type Report struct {
	// Results は検査結果を検査した順に並べたものである。
	Results []Result
}

// add は検査結果を1件積む。
//
// res: 積む検査結果。
func (r *Report) add(res Result) {
	r.Results = append(r.Results, res)
}

// Counts は記号ごとの件数を返す。
//
// 戻り値: 通った件数・足りない件数・確かめられなかった件数。
func (r Report) Counts() (ok, missing, unknown int) {
	for _, res := range r.Results {
		switch res.Symbol {
		case SymbolOK:
			ok++
		case SymbolMissing:
			missing++
		case SymbolUnknown:
			unknown++
		}
	}
	return ok, missing, unknown
}

// ExitCode は終了コードを返す（設計 3-32）。
//
// **`✗` が1つでもあれば 1。`!` だけなら 0 である。**
// 確かめられなかっただけのものは「動くかもしれない」ので、人間の手を止めない。
//
// 戻り値: 終了コード（0 か 1）。
func (r Report) ExitCode() int {
	for _, res := range r.Results {
		if res.Symbol == SymbolMissing {
			return 1
		}
	}
	return 0
}

// labelColumn は見出し語を並べる桁数（端末の表示幅）である。
//
// いちばん長い見出し語（`hook の置き場所` と `worktree の場所` = 15 桁）が収まる幅にしてある。
const labelColumn = 16

// Write は検査結果を人間が読む形で書き出す（設計 3-32 の出力の形）。
//
//	✓ herdr           protocol 19（設定と一致）
//	✗ clone           octocat/hello-world が見つからない
//	                  → ghq get octocat/hello-world を実行してください
//
//	2件に問題があります（✗ 1件 / ! 1件）
//
// w: 書き出す先。
// 戻り値: 書き出しに失敗した場合のエラー。
func (r Report) Write(w io.Writer) error {
	var b strings.Builder
	indent := strings.Repeat(" ", 2+labelColumn)

	for _, res := range r.Results {
		label := LabelText(res.Label)
		b.WriteString(fmt.Sprintf("%s %s%s%s\n", res.Symbol, label, padding(label), res.Detail))
		for _, note := range res.Notes {
			b.WriteString(indent + note + "\n")
		}
		for _, remedy := range res.Remedies {
			b.WriteString(indent + "→ " + remedy + "\n")
		}
	}

	_, missing, unknown := r.Counts()
	b.WriteString("\n")
	switch {
	case missing+unknown == 0:
		b.WriteString(i18n.T(i18n.KeyDoctorSummaryAllOK, len(r.Results)) + "\n")
	case missing == 0:
		// **`!` だけのときを「問題があります」と書かない。**対象リポジトリが0件のとき
		// （ボードが空）もここへ来る。**ボードが空なのは設定の誤りではない**（設計 3-32）。
		b.WriteString(i18n.T(i18n.KeyDoctorSummaryUnknownOnly, unknown, unknown) + "\n")
	default:
		b.WriteString(i18n.T(i18n.KeyDoctorSummaryProblems, missing+unknown, missing, unknown) + "\n")
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// padding は見出し語のうしろに入れる空白を返す。
//
// **日本語の見出し語が混ざるので、文字数ではなく端末の表示幅で揃える。**
//
// label: 見出し語。
// 戻り値: 見出し語の右に入れる空白（表示幅が labelColumn に届かない場合は最低1つ）。
func padding(label string) string {
	n := labelColumn - displayWidth(label)
	if n < 1 {
		n = 1
	}
	return strings.Repeat(" ", n)
}

// displayWidth は端末に表示したときのおおよその桁数を返す。
//
// **CJK の文字を2桁として数える。**`utf8.RuneCountInString` で数えると、
// `設定ファイル` と `clone` が同じ桁に見えず、説明の開始位置が揃わない。
//
// s: 数える文字列。
// 戻り値: 表示幅（桁数）。
func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		// 0x2E80 以降は CJK の部首補助から始まる全角の領域である。
		// ひらがな・カタカナ・漢字・全角の括弧はすべてここに入る。
		if r >= 0x2E80 {
			width += 2
			continue
		}
		width++
	}
	return width
}
