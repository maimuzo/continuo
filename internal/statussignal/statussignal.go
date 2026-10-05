// Package statussignal は表明（エージェントが応答に書く `CONTINUO-STATUS: <値>` の1行）の
// 読み方を1か所に持つ（docs/plans/continuo_design.md 3-25 / 3-26）。
//
// **読むのは2者である。**continuo 本体（turn が終わったあとに transcript から読む）と、
// `continuo hook`（`Stop` の入力の `last_assistant_message` から読む。issue #274）。
// **2者が別々の読み方を持つと、本体が正しいと読む表明を hook が差し戻す。**
// 行の拾い方・対象の解決・取り得る値に在るかの判定・一覧の文面を、ここへ寄せる。
//
// **この package は、orchestrator にも hookclient にも依存しない。**
// `continuo hook` は turn ごとに起動する短いプロセスなので、常駐プロセスの側の
// package を引き込まない。
package statussignal

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// targetPattern は表明の行に書かれた対象の issue を切り出す正規表現である（設計 3-26）。
//
//	CONTINUO-STATUS: review              対象なし（いま作業している issue）
//	CONTINUO-STATUS: #45 review          代表の issue と同じリポジトリの #45
//	CONTINUO-STATUS: octocat/hello-world#47 blocked   別リポジトリを明示した形
var targetPattern = regexp.MustCompile(`^(?:([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+))?#(\d+)$`)

// MaxSignalsPerTurn は1つの turn で受け付ける表明の件数の上限である。
//
// **上限を置く理由。**表明1件につき本体がカンバンを全ページ走査する GraphQL 呼び出しを
// 1回行い、カンバンに載っていなければコメントも書く。上限が無いと、エージェントは
// 1回の応答に印を並べるだけで GitHub API を任意の回数だけ呼ばせられ、
// 枠を使い切ると**他の run のトラッカー操作まで巻き添えで失敗する。**
//
// **10件で足りる根拠。**表明の対象は「まとめて直したグループ」である（設計 3-26）。
// 1つのセッションでまとめて片付ける issue の件数がこれを超えるなら、グループの切り方が
// 大きすぎる。超えた分は捨てる。
const MaxSignalsPerTurn = 10

// MaxSignalFieldBytes は表明の対象と値それぞれの長さの上限（バイト）である。
//
// **識別子は `<owner>/<repo>#<番号>` であり、値は `review` などの短い語である。**
// これを超える語は表明ではない。長い文字列をキーにした写像を持ち回らないために切る。
const MaxSignalFieldBytes = 256

// maxShownValueRunes は、文面へ載せる値の長さの上限（文字数）である。
//
// **値はエージェントが書いたものである。**256 バイトまで通るので、そのまま載せると
// 文面の大半が値になる。何を書いたかを思い出せる長さがあれば足りる。
const maxShownValueRunes = 40

// unshowableValue は、制御文字を落としたあとに何も残らなかった値の代わりに載せる語である。
const unshowableValue = "（表示できない値）"

// Parse は集めた text から表明を拾う（設計 3-25 の段6・段7、および 3-26）。
//
// **行に割って探すのが要点である。**印が他の文と同じブロックに入ることがある
// （実例: `3つとも完了しました…\n\nCONTINUO-STATUS: review`）。ブロックの一致では取れない。
//
// **印は行頭にあるものだけを拾う**（先頭の空白は許す）。行のどこにあっても拾うと、
// エージェントが issue の本文やコメントを**引用しただけ**で表明が成立する
// （設計 3-29 のとおりエージェントは `gh issue view` で issue を自分で読む）。
// 行き先は Status の変更であり、`terminal_states` に入れば worktree と branch の削除まで
// 進むので、**issue を立てられる人なら誰でも引ける経路になってしまう。**
//
// **1つの turn で受け付けるのは `MaxSignalsPerTurn` 件までである。**超えた分は捨てる。
//
// **印が複数あれば、issue ごとに最後に現れたものを採る**（設計 3-25）。
//
// **対象の解決に失敗した行は、対象なしの行として扱う。**先頭の語が
// `#<番号>` にも `<owner>/<repo>#<番号>` にも当てはまらなければ、その語を**表明の値**と
// みなし、対象は `currentIdentifier` にする。
//
// **解決できた対象は、カンバンに載っているかどうかを見ずにそのまま識別子として返す。**
// 載っているかを引くのは本体の側である（設計 3-26）。
//
// texts: assistant の text ブロックの本文の並び（現れた順）。
// prefix: 表明の印（`tracker.status_signal_prefix`。例 `CONTINUO-STATUS:`）。
// currentIdentifier: いま作業している issue の識別子。**対象を書かない行はこれを指す。**
// 戻り値: 対象の識別子から表明の値への対応。
func Parse(texts []string, prefix, currentIdentifier string) map[string]string {
	result := map[string]string{}
	if prefix == "" {
		return result
	}

	owner, repo := splitIdentifier(currentIdentifier)

	for _, text := range texts {
		for _, line := range strings.Split(text, "\n") {
			trimmed := strings.TrimLeft(line, " \t\u3000")
			if !strings.HasPrefix(trimmed, prefix) {
				continue
			}
			rest := strings.TrimSpace(trimmed[len(prefix):])
			if rest == "" {
				continue
			}
			if len(rest) > MaxSignalFieldBytes*2 {
				// 表明の行に収まる長さではない。
				continue
			}
			fields := strings.Fields(rest)

			target := currentIdentifier
			var value string
			switch {
			case len(fields) == 1:
				value = fields[0]
			default:
				resolved, ok := resolveTarget(fields[0], owner, repo)
				if !ok {
					// 対象として解釈できない語が先頭に来た場合は、
					// 「対象なし」の行だと解釈して1語目を値として扱う。
					value = fields[0]
					break
				}
				target = resolved
				value = fields[1]
			}

			value = strings.TrimSpace(strings.Trim(value, ".。"))
			if value == "" {
				continue
			}
			if len(value) > MaxSignalFieldBytes || len(target) > MaxSignalFieldBytes {
				continue
			}
			if _, known := result[target]; !known && len(result) >= MaxSignalsPerTurn {
				// 上限に達している。**新しい対象は増やさない**（既にある対象の
				// 上書きは段7 のとおり続ける）。
				continue
			}
			// 段7: 同じ issue に複数あれば、最後に現れたものが勝つ。
			result[target] = strings.ToLower(value)
		}
	}
	return result
}

// resolveTarget は表明の行に書かれた対象を、`<owner>/<repo>#<番号>` の識別子へ直す
// （設計 3-26）。
//
// **`#<番号>` は代表の issue と同じリポジトリを指す。**別リポジトリを指すときは
// `<owner>/<repo>#<番号>` と書かせる。
//
// raw: 行に書かれた対象の文字列。
// owner: いま作業している issue の所有者名。
// repo: いま作業している issue のリポジトリ名。
// 戻り値の1つ目: 解決した識別子。
// 戻り値の2つ目: 対象として解釈できれば true。
func resolveTarget(raw, owner, repo string) (string, bool) {
	m := targetPattern.FindStringSubmatch(raw)
	if m == nil {
		return "", false
	}
	if m[1] != "" {
		return m[1] + "#" + m[2], true
	}
	if owner == "" || repo == "" {
		return "", false
	}
	return owner + "/" + repo + "#" + m[2], true
}

// splitIdentifier は `<owner>/<repo>#<番号>` を owner と repo に割る。
//
// identifier: issue の識別子。
// 戻り値の1つ目: 所有者名。割れなければ空文字。
// 戻り値の2つ目: リポジトリ名。割れなければ空文字。
func splitIdentifier(identifier string) (string, string) {
	hash := strings.LastIndex(identifier, "#")
	if hash < 0 {
		return "", ""
	}
	slash := strings.Index(identifier[:hash], "/")
	if slash < 0 {
		return "", ""
	}
	return identifier[:slash], identifier[slash+1 : hash]
}

// Lookup は表明の値から遷移先の Status を引く。
//
// **前後の空白を落とし、大文字と小文字を区別せずに比べる。**設定のキーに `Review` と
// 書いた利用者でも、`Parse` が小文字にした値 `review` と一致する。
// **hook と本体が同じ比べ方をするために、ここの1つだけを呼ぶ。**
//
// values: `tracker.status_signal_map`。
// value: 表明の値（`review` / `blocked` / `working` など）。
// 戻り値の1つ目: 遷移先の Status。nil なら動かさない。
// 戻り値の2つ目: キーがあれば true。
func Lookup(values map[string]*string, value string) (*string, bool) {
	for k, v := range values {
		if strings.EqualFold(strings.TrimSpace(k), strings.TrimSpace(value)) {
			return v, true
		}
	}
	return nil, false
}

// Invalid は、取り得る値に無かった表明1件である（issue #274）。
type Invalid struct {
	// Target は表明の対象の識別子である（`<owner>/<repo>#<番号>`）。
	// 対象を書かない行では、いま作業している issue の識別子が入る。
	Target string
	// Value は書かれていた値である（`Parse` が小文字にしたもの）。
	Value string
}

// FindInvalid は、拾った表明のうち取り得る値に無いものを集める（issue #274）。
//
// **対象の名前順に並べて返す。**写像の走査の順は毎回変わるので、並べないと
// 同じ応答から違う文面ができる。
//
// signals: `Parse` が返した、対象から値への対応。
// values: `tracker.status_signal_map`。**空なら、何も返さない。**
// 戻り値: 取り得る値に無かった表明の並び。1件も無ければ nil。
func FindInvalid(signals map[string]string, values map[string]*string) []Invalid {
	if len(values) == 0 {
		// **対応表が空なら、返せる一覧が無い。**全部の表明が決まり以外になるが、
		// 「この中から選んで」と言える値が1つも無い。hook の側も、値の無いファイルでは調べない
		// （`File.Usable`）。
		return nil
	}
	var out []Invalid
	for target, value := range signals {
		if _, known := Lookup(values, value); known {
			continue
		}
		out = append(out, Invalid{Target: target, Value: value})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

// showValue は、エージェントが書いた値を文面へ載せる形に直す。
//
// **制御文字を落とし、長さを切る。**値はエージェントの出力であり、そのまま pane への
// 入力に入る。**backtick は落とさない。**値を backtick で囲んで書いた行（印のあとに
// backtick 付きの review）は、値そのものが backtick 付きであり、落として載せると一覧に在る値と同じに見えて、何が違ったのかが
// 伝わらない。囲みには「」を使い、値の中の backtick がそのまま見えるようにする。
//
// value: 表明の値。
// 戻り値: 「」で囲んだ値。落としたあとに何も残らなければ `unshowableValue`。
func showValue(value string) string {
	var b strings.Builder
	n := 0
	for _, r := range value {
		if unicode.IsControl(r) {
			continue
		}
		if n >= maxShownValueRunes {
			b.WriteString("…")
			break
		}
		b.WriteRune(r)
		n++
	}
	if n == 0 {
		return unshowableValue
	}
	return "「" + b.String() + "」"
}

// shortTarget は、対象の識別子を表明の行に書く形へ直す。
//
// **いま作業している issue と同じリポジトリなら `#<番号>` にする。**エージェントが
// そのまま写して書ける形で見せるためである。
//
// target: 対象の識別子（`<owner>/<repo>#<番号>`）。
// currentIdentifier: いま作業している issue の識別子。
// 戻り値: 表明の行に書く対象の形。
func shortTarget(target, currentIdentifier string) string {
	owner, repo := splitIdentifier(currentIdentifier)
	tOwner, tRepo := splitIdentifier(target)
	if owner == "" || !strings.EqualFold(owner, tOwner) || !strings.EqualFold(repo, tRepo) {
		return target
	}
	return target[strings.LastIndex(target, "#"):]
}

// describeInvalid は、取り得る値に無かった表明を1文の中へ並べる形にする。
//
// **対象がいま作業している issue なら値だけを、別の issue なら対象を添えて書く。**
//
// invalid: 取り得る値に無かった表明の並び。
// currentIdentifier: いま作業している issue の識別子。
// 戻り値: 「、」で繋いだ並び。
func describeInvalid(invalid []Invalid, currentIdentifier string) string {
	parts := make([]string, 0, len(invalid))
	for _, inv := range invalid {
		if strings.EqualFold(inv.Target, currentIdentifier) {
			parts = append(parts, showValue(inv.Value))
			continue
		}
		parts = append(parts, inv.Target+" の値 "+showValue(inv.Value))
	}
	return strings.Join(parts, "、")
}

// WriteValueList は取り得る値の一覧を書く（issue #274）。
//
// **値の名前順に並べる。**一覧と行き先は対応表から機械的に作り、値ごとの意味の文は
// 持たない（利用者が対応表を書き換えられるため）。
//
// **行頭を `- ` と backtick にする。**エージェントが一覧を応答へ書き写しても、
// その行は印から始まらないので、本体は表明として読まない（`Parse` は行頭の印だけを拾う）。
//
// **対応表が空なら、何も書かない。**「取り得る値は次のとおりです」のあとに何も無い文面を作らない。
//
// b: 書き込む先。
// prefix: 表明の印。
// values: `tracker.status_signal_map`。
func WriteValueList(b *strings.Builder, prefix string, values map[string]*string) {
	if len(values) == 0 {
		return
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("取り得る値は次のとおりです。\n\n")
	for _, k := range keys {
		if dest := values[k]; dest != nil {
			fmt.Fprintf(b, "- `%s %s` … Status を %s へ動かします\n", prefix, strings.TrimSpace(k), *dest)
			continue
		}
		fmt.Fprintf(b, "- `%s %s` … Status を動かしません\n", prefix, strings.TrimSpace(k))
	}
}

// hasMovingValue は、Status を動かす値（行き先が null でない値）が1つでも在るかを返す。
func hasMovingValue(values map[string]*string) bool {
	for _, dest := range values {
		if dest != nil {
			return true
		}
	}
	return false
}

// writeTargetNote は、別の issue を指す表明が決まり以外だったときの1文を書く。
//
// **書かないと、エージェントは一覧の行をそのまま写す。**一覧の行は対象を書かない形なので、
// 写した行は**いま作業している issue の表明**になり、直したかった issue の代わりに
// 自分の issue の Status が動く。
//
// b: 書き込む先。
// prefix: 表明の印。
// invalid: 取り得る値に無かった表明の並び。
// currentIdentifier: いま作業している issue の識別子。
func writeTargetNote(b *strings.Builder, prefix string, invalid []Invalid, currentIdentifier string) {
	for _, inv := range invalid {
		if strings.EqualFold(inv.Target, currentIdentifier) {
			continue
		}
		fmt.Fprintf(b, "%s の分は、`%s %s <値>` の形で書き直してください。"+
			"対象を付けずに書くと、いま作業している issue の表明になります。\n",
			inv.Target, prefix, shortTarget(inv.Target, currentIdentifier))
	}
}

// HasTarget は、取り得る値に無かった表明の中に、その対象のものが在るかを返す。
//
// invalid: 取り得る値に無かった表明の並び。
// identifier: 探す対象の識別子。
// 戻り値: 1件でも在れば true。
func HasTarget(invalid []Invalid, identifier string) bool {
	for _, inv := range invalid {
		if strings.EqualFold(inv.Target, identifier) {
			return true
		}
	}
	return false
}

// hasStayingValue は、Status を動かさない値（行き先が null の値）が1つでも在るかを返す。
func hasStayingValue(values map[string]*string) bool {
	for _, dest := range values {
		if dest == nil {
			return true
		}
	}
	return false
}

// WriteInvalidGuidance は、表明の値が決まり以外だったときに返す文面を書く（issue #274）。
//
// **2つの経路が同じ文面を使う。**`continuo hook` が `Stop` を差し戻すときの `reason`
// （経路1）と、本体が次の turn に送る継続の指示（経路2）である。違うのは1行目の
// 「この応答」「前回の応答」と、作業を止める言い方だけなので、`when` と `stop` で受ける。
//
// **いまの Status を言い切る文は入れない。**対象の違う行が混ざっていると、動いた Status と
// 動かなかった Status が両方在る。人間と direct chat をしている pane と、成果の報告を
// 書かせ直しているセッションでは、本体は表明を読まない。**「この値では Status を動かせません」は
// どの場面でも事実である。**
//
// **作業を止める言い方は、止めてよいときにだけ出す。**次の2つが両方そろったときである。
//
//	いま作業している issue の表明が決まり以外である
//	    … 別の issue を指す行だけが決まり以外なら、自分の作業は途中かもしれない
//	対応表に、Status を動かさない値が在る
//	    … 無い対応表では、「まだ続きがある」を表す値を選べない。止めると、
//	      作業の途中のエージェントが Status を動かす値を選び、run が途中で人間へ渡る
//
// **Status を動かさない値が無い対応表では、表明を書かずに続ける道を示す。**
// 利用者が `status_signal_map` を自分で書くと、既定の対応表は丸ごと置き換わる。
// `working` を書かなかった利用者の対応表で、指示書どおりに `working` と書いたエージェントが
// ここへ来る。いままでは、その値は無視されて「続けてください」が届いていた。
//
// b: 書き込む先。
// when: 1行目の頭に置く語（「この応答」「前回の応答」）。
// stop: 書き直しを求める文の頭に置く語（「作業は進めず」「作業を進める前に」）。
// prefix: 表明の印。
// currentIdentifier: いま作業している issue の識別子。
// invalid: 取り得る値に無かった表明の並び。**1件以上あること。**
// values: `tracker.status_signal_map`。**1件以上あること**（空なら `FindInvalid` が何も返さない）。
func WriteInvalidGuidance(
	b *strings.Builder,
	when, stop, prefix, currentIdentifier string,
	invalid []Invalid,
	values map[string]*string,
) {
	own := HasTarget(invalid, currentIdentifier)
	staying := hasStayingValue(values)

	fmt.Fprintf(b, "%sの表明の値 %s は、決められた値ではありません。この値では Status を動かせません。\n",
		when, describeInvalid(invalid, currentIdentifier))
	WriteValueList(b, prefix, values)
	b.WriteString("\n")
	writeTargetNote(b, prefix, invalid, currentIdentifier)
	if own && staying {
		fmt.Fprintf(b, "%s、この中から選んで、応答の最後に、行頭から1行で書き直してください。\n", stop)
	} else {
		b.WriteString("この中から選んで、応答の最後に、行頭から1行で書き直してください。\n")
	}
	if !staying {
		b.WriteString("この一覧には、まだ作業が続くことを表す値がありません。" +
			"まだ作業が続くなら、表明は書かずに、そのまま作業を続けてください。\n")
	}
	if own && hasMovingValue(values) {
		b.WriteString("作業を終えたつもりなら、続きの作業はせず、Status を動かす値から選んでください。\n")
	}
}

// BlockReason は、`Stop` hook が差し戻すときに Claude Code へ渡す `reason` を組み立てる
// （issue #274 の経路1）。
//
// prefix: 表明の印。
// currentIdentifier: いま作業している issue の識別子。
// invalid: 取り得る値に無かった表明の並び。**1件以上あること。**
// values: 取り得る値と行き先。
// 戻り値: `reason` の本文。
func BlockReason(prefix, currentIdentifier string, invalid []Invalid, values map[string]*string) string {
	var b strings.Builder
	WriteInvalidGuidance(&b, "この応答", "作業は進めず", prefix, currentIdentifier, invalid, values)
	return b.String()
}
