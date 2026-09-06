package handoff

import (
	"encoding/json"
	"strings"
	"time"
	"unicode"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
)

// CommentView は判定に要るコメントの中身だけを写したものである。
//
// **`internal/tracker` の型をそのまま受け取らない。**このパッケージは
// GitHub を1バイトも読み書きしないので、トラッカーの型を知る必要が無い。
// **知らなければ、テストは値を並べるだけで書ける。**
type CommentView struct {
	// Author はコメントを書いたアカウントのログイン名である。取れなければ空文字。
	Author string
	// Body はコメント本文である。**印を剥がさず、そのまま渡すこと。**
	Body string
	// CreatedAt は GitHub がそのコメントに付けた作成時刻である。
	//
	// **投稿の順番を決めるのはこちらである。**入札・hold・released・回の区切りは、
	// **編集で動かせてはならない**ので、全部この時刻で比べる（設計 5-3k）。
	CreatedAt time.Time
	// UpdatedAt は本文が最後に編集された時刻である（設計 5-3k）。**取れなければゼロ値。**
	//
	// **エージェントは進捗の報告を、いちばん下にある自分のコメントへ書き足す**（設計 5-3j）。
	// **本文を編集しても CreatedAt は動かない**ので、これが無いと
	// **書き続けている機械の持ち回りの期限が1秒も進まない。**
	//
	// **これを見るのは `lastProgressOf` だけである。**
	UpdatedAt time.Time
}

// LastTouched は、そのコメントが最後に触られた時刻を返す（設計 5-3k）。
//
// **`UpdatedAt` が取れているとは限らない。**GraphQL の応答からフィールドが落ちれば
// ゼロ値になる。**ゼロ値をそのまま返すと、期限がゼロ時刻から数えられて、
// 生きている担当がその場で外れる。**だから新しいほうを返す。
//
// 戻り値: `CreatedAt` と `UpdatedAt` のうち新しいほう。
func (c CommentView) LastTouched() time.Time {
	if c.UpdatedAt.After(c.CreatedAt) {
		return c.UpdatedAt
	}
	return c.CreatedAt
}

// Situation は「いま issue がどう見えているか」である（設計 3-77b の表の入力）。
type Situation struct {
	// Assignees は issue に付いている担当者のログイン名である。
	Assignees []string
	// Comments は issue に付いているコメントの全件である。
	//
	// **全件でなければならない**（設計 3-77a）。新しい方から数十件だけを見ると、
	// **入札で押し流された hold のコメントが見えず、人間が付けた担当と区別できなくなる。**
	Comments []CommentView
	// SelfLogin は continuo が使う gh の持ち主のログイン名である。
	//
	// **持ち回りで参加者を見分ける値は、これ1つだけである**（設計 3-77-0）。
	// **同じ GitHub アカウントを複数の機械で使うことはサポートしない**ので、
	// アカウントが自分なら、担当しているのも自分である。
	//
	// **空文字なら「自分が誰か分からない」である。**そのときは担当者が付いている issue に
	// 一切触らない（自分の担当かどうかを言えないので、奪う側にも進む側にも倒せない）。
	SelfLogin string
	// Now はいまの時刻である。
	Now time.Time
	// IdleTimeout は担当者の最後の進捗報告からこれだけ経つと担当を外す長さである。
	// **数えるのは印の付いたコメントの最終更新日時で、印が1件も無いあいだは hold の時刻から数える。**
	IdleTimeout time.Duration
}

// Action は Assess が返す「次に何をするか」である（設計 3-77b の表と1対1）。
type Action int

const (
	// ActionBid は担当者が1人もいないので入札することを表す。
	ActionBid Action = iota
	// ActionProceed は担当者が自分1人なので、入札せずに着手・引き継ぎへ進むことを表す。
	ActionProceed
	// ActionRelease は期限の切れた担当を外し、released のコメントを書いてから入札をやり直すことを表す。
	ActionRelease
	// ActionSkipHeld は、他人の担当が期限内なので触らないことを表す。**入札もしない。**
	ActionSkipHeld
	// ActionSkipHumanAssigned は、hold のコメントが1件も無い担当なので触らないことを表す。
	//
	// **人間が付けた担当である。**hold があることが「その担当者は機械である」の
	// 唯一の証拠なので、無ければ奪わない（設計 3-77b）。
	ActionSkipHumanAssigned
	// ActionSkipManyAssignees は、担当者が2人以上いるので触らないことを表す。**WARN を出す。**
	ActionSkipManyAssignees
	// ActionSkipSelfUnknown は、gh の持ち主が分からないので担当のある issue に触らないことを表す。
	ActionSkipSelfUnknown
)

// String は判定を人間が読める1語で返す（ログに出す）。
//
// 戻り値: 判定を表す語。
func (a Action) String() string {
	switch a {
	case ActionBid:
		return "入札する"
	case ActionProceed:
		return "自分の担当"
	case ActionRelease:
		return "期限切れの担当を外す"
	case ActionSkipHeld:
		return "期限内の担当"
	case ActionSkipHumanAssigned:
		return "人間が付けた担当"
	case ActionSkipManyAssignees:
		return "担当者が2人以上"
	case ActionSkipSelfUnknown:
		return "gh の持ち主が分からない"
	default:
		return "不明"
	}
}

// Assessment は Assess の答えである。
type Assessment struct {
	// Action は次に何をするかである。
	Action Action
	// Assignee は、いま付いている担当者のログイン名である（1人のときだけ入る）。
	Assignee string
	// Hold は、その担当者が書いたいちばん新しい hold である。
	//
	// **読むのは `ActionRelease` のときだけである。**`ActionSkipHeld` でも値は入るが、
	// **その経路の呼び出し側は `Assignee` と `LastProgress` しか見ない。**
	// **使うのは `Branch` だけである。**担当を外したときに書く released のコメントへ、
	// その branch の名前を写す（`releaseExpiredAssignee`）。
	// **`from` に書くアカウントは、この欄ではなく `Assignee` から取る**（設計 3-77-0）。
	Hold Hold
	// LastProgress は、担当者がまだ生きていることを最後に示した時刻である
	// （担当者がいるときだけ入る）。
	//
	// **中身は2つのうち新しいほうである**（設計 3-77b / 5-3l）。
	//
	//	進捗報告の印（config.ProgressMarker）が付いた、その担当者のコメントの LastTouched()
	//	hold のコメントが作られた時刻（＝その担当が始まった時刻）
	//
	// **hold の時刻を下限に置くのは、勝った直後には進捗報告が1件も無いからである。**
	// 置かないと、着手する前にその場で期限切れと読まれる。
	// **前の担当のときに書かれた古い進捗報告に引きずられないのも、この下限のおかげである。**
	LastProgress time.Time
}

// Assess は、いま見えているものから「次に何をするか」を決める（設計 3-77b の表）。
//
//	担当者が2人以上                                触らない。WARN を出す
//	担当者が無い                                   入札する
//	自分のアカウント1人                            着手・引き継ぎへ進む（入札しない）
//	他人1人 ＋ hold が1件も無い                     触らない。人間が付けた担当である。WARN を出す
//	他人1人 ＋ hold あり ＋ 期限内                   触らない。入札もしない
//	他人1人 ＋ hold あり ＋ 期限切れ                 担当を外して入札をやり直す
//
// **担当者のアカウントが自分なら、担当しているのも自分である**（設計 3-77-0）。
// **同じ GitHub アカウントを複数の機械で使うことはサポートしない**ので、
// **アカウント1つにつき continuo は1つである。**hold を見て機械を見分ける段は置かない。
//
// **hold は「いまの担当者が書いたもの」だけを数える**（設計 3-77b）。
// **入札の回が変わっても hold のコメントは消えない**ので、機械が外れたあとに人間が
// 自分を担当者にすると、**古い機械の hold が「この担当者は機械である」の証拠に化ける。**
// **`Hold.Assignee` で担当者を突き合わせて、その化けを止める。**
//
// **期限は「その担当者の進捗報告が最後に現れてから」で数える**（設計 3-77b / 5-3l）。
// **数えるのは進捗報告の印（config.ProgressMarker）が付いたコメントだけである。**
// **投稿者だけで数えてはならない。**エージェントも continuo も人間も同じ GitHub アカウントで
// 投稿するので（[internal/tracker/ghuser.go](../tracker/ghuser.go) の 23-25行）、
// **人間が無関係なコメントを1件書いただけで、黙り込んだエージェントの期限が延びてしまう。**
//
// **進捗報告が1件も無いあいだは、hold のコメントが作られた時刻から数える。**
// 勝った直後には進捗報告が無いので、下限を置かないとその場で期限切れになる。
//
// s: いま見えているもの。
// 戻り値: 判定と、その判定に使った値。
func Assess(s Situation) Assessment {
	logins := nonEmpty(s.Assignees)
	switch {
	case len(logins) >= 2:
		return Assessment{Action: ActionSkipManyAssignees}
	case len(logins) == 0:
		return Assessment{Action: ActionBid}
	}

	assignee := logins[0]
	if strings.TrimSpace(s.SelfLogin) == "" {
		// **自分が誰か分からないまま担当のある issue に触らない**（設計 3-65 と同じ立場）。
		// 触ると、自分の担当を「他人の担当」と読んで自分から奪うことになる。
		return Assessment{Action: ActionSkipSelfUnknown, Assignee: assignee}
	}

	if strings.EqualFold(assignee, strings.TrimSpace(s.SelfLogin)) {
		// **担当者が自分のアカウントなら、担当しているのも自分である**（設計 3-77-0）。
		// **hold の有無も、誰が書いたかも見ない。**アカウント1つにつき continuo は1つである。
		return Assessment{Action: ActionProceed, Assignee: assignee}
	}

	// **hold は、いまの担当者が書いたものだけを見る。**別の担当者の古い hold を数えると、
	// **人間が引き継いだ issue を機械が取り上げる。**
	hold, holdAt, hasHold := LatestHoldFor(s.Comments, assignee)

	if !hasHold {
		return Assessment{Action: ActionSkipHumanAssigned, Assignee: assignee}
	}

	last, hasLast := lastProgressOf(s.Comments, assignee, holdAt)
	if !hasLast {
		// **期限を数える起点が1つも無い。**hold のコメントに作成時刻が入っておらず、
		// その担当者の進捗報告も1件も無い状態である。
		// **触らない側へ倒す**（奪ってよい証拠が無い）。
		return Assessment{Action: ActionSkipHeld, Assignee: assignee, Hold: hold}
	}
	if s.Now.Sub(last) <= s.IdleTimeout {
		return Assessment{Action: ActionSkipHeld, Assignee: assignee, Hold: hold, LastProgress: last}
	}
	return Assessment{Action: ActionRelease, Assignee: assignee, Hold: hold, LastProgress: last}
}

// nonEmpty は空文字を落としたログイン名の一覧を返す。
//
// logins: 元の一覧。
// 戻り値: 空文字を落とした一覧（**渡された配列は書き換えない**）。
func nonEmpty(logins []string) []string {
	out := make([]string, 0, len(logins))
	for _, l := range logins {
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}

// IsProgressReport は、そのコメントがエージェントの進捗報告かを返す（設計 5-3j / 5-3l）。
//
// **持ち回りの期限を進めてよいのは、これが真になるコメントだけである。**
//
// **投稿者は見ない。**呼ぶ側が担当者で絞る（`lastProgressOf`）。
//
// **印が本文のどこかに在れば真とする。****組み込みのプロンプトが
// エージェント自身に使わせている見つけ方（`.body | contains("<!-- continuo:progress -->")`）と
// 揃えるためである。**Go 側だけを厳しくすると、**エージェントは自分の進捗報告だと思って
// 書き足し続けているのに continuo が数えず、生きている担当が18時間で外れる。**
//
// **人間がこの印を書いても構わない**（2026-09-03 に人間が判断した）。
// そのコメントのぶん死活確認が効かなくなるだけであり、
// **印で絞らずに投稿者だけで数えて死活確認そのものを失うほうが重い。**
//
// body: コメント本文。
// 戻り値: 進捗報告の印を含んでいれば true。
func IsProgressReport(body string) bool {
	return strings.Contains(body, config.ProgressMarker)
}

// StartsAsProgressReport は、そのコメントが途中経過の報告として書き出されたかを返す
// （issue #178）。
//
// **見るのは本文の先頭にある印の並びだけである。**組み込みのプロンプトは
// `<!-- continuo:agent -->` の次の行に進捗報告の印を書かせており（設計 5-3j の段2b）、
// **エージェント自身の見つけ方（`.body | startswith(…)`）と同じ位置を見る。**
//
// **`IsProgressReport` と使い分ける。**あちらは印が本文のどこかに在れば真であり、
// **持ち回りの死活の判定はそれでよい**（設計 5-3l。厳しくすると生きている担当が外れる）。
// **こちらを緩くしてはならない。**
//
// **緩くすると何が起きるか。****成果の報告が印を引用しただけで、途中経過として捨てられる。**
// continuo は「書かれていない」と判定してセッションを復元し、
// **2度目も引用されれば `failure_state` へ落として人間へ渡す。**
// **書いてあるのに、書かなかったことにされる。**
// **印について説明する報告ほど起きやすい**（この判定を足した issue #178 の作業で実際に起きた）。
//
// body: コメント本文。
// 戻り値: 先頭の印の並びに進捗報告の印があれば true。
func StartsAsProgressReport(body string) bool {
	// **本文全体の先頭の空白だけを落とす。**`Comment.IsAgent` は
	// `strings.TrimSpace(body)` してから印を見るので（internal/tracker の `FetchComments`）、
	// **落とさないと2つの判定がずれる。**本文の先頭に空白が1つあるだけで、
	// **進捗報告が「この run の成果の報告」として数えられ、issue #178 がその形で直らない。**
	//
	// **落とす文字は `unicode.IsSpace` で揃える。**`strings.TrimLeft(body, " \t\r\n")` では狭い。
	// **`TrimSpace` は全角空白（U+3000）や NBSP（U+00A0）も落とす**ので、
	// **日本語で書く利用者がいちばん踏みやすい全角空白で、2つの判定がずれる。**
	//
	// **行ごとの字下げは落とさない。**落とすと、4桁字下げしたコード片での引用が
	// また「印の行」として通る（下の HasPrefix を見よ）。
	body = strings.TrimLeftFunc(body, unicode.IsSpace)
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			// **空行では止めない。**印と印のあいだに空行を挟む書き方がありうる。
			continue
		}
		// **字下げした行は、名乗りではない。**行頭ちょうどの `<!--` だけを見る。
		// **4桁の字下げは、組み込みのプロンプトが印を「見せる」ときの書き方そのものである**
		// （internal/prompt の stripComments が、その形を落とさずに残している）。
		// **字下げを許すと、印について説明する成果の報告が、いちばん起きやすい形で捨てられる。**
		if !strings.HasPrefix(line, commentOpen) {
			// **本文が始まった。**ここから先の印は、引用であって名乗りではない。
			return false
		}
		// **行そのものが印で始まっていること。**行の途中に現れる印は名乗りではない。
		// **`Contains` では、印を引用した HTML のコメントの行が真になる。**
		// 例: `<!-- この報告に <!-- continuo:progress --> は付けていません -->` は、
		// 行頭が `<!--` で、印を文字列として含む。**書いてあるのに「書かれていない」と
		// 判定され、復元が走り、2度目も同じなら `failure_state` へ落ちる**（issue #178 の再発）。
		if strings.HasPrefix(line, config.ProgressMarker) {
			return true
		}
	}
	return false
}

// commentOpen は HTML のコメントの開きである。
//
// **この判定は「印が HTML のコメントである」ことを前提にしている。**
// 進捗報告の印（`config.ProgressMarker`）は固定なので前提は崩れないが、
// **エージェントの印（`tracker.comments.marker`）は設定で変えられ、形を縛る検査が無い。**
// `marker: "[continuo-agent]"` のような値にすると、成果の報告の1行目が `<!--` で始まらず、
// `StartsAsProgressReport` は必ず偽を返す。**その利用者では issue #178 の直しが効かない。**
//
// **ただし、その利用者は既に別のところで壊れている。**組み込みの指示書は
// `<!-- continuo:agent -->` を文字列で埋め込んでおり、設定した marker を使っていない。
// **設定した marker を組み込みへ届ける道ができたときに、ここも見直すこと。**
//
// **同じ前提に立つ定数が、他に2つある。**印の形を変えるときは3つとも動かすこと。
//
//	[internal/prompt/prompt.go](../prompt/prompt.go) の commentOpen / commentClose
//	[internal/orchestrator/prompt.go](../orchestrator/prompt.go) の commentOpenMarker / commentCloseMarker
const commentOpen = "<!--"

// lastProgressOf は、その担当者がまだ生きていることを最後に示した時刻を返す（設計 3-77b / 5-3l）。
//
// **数えるのは、進捗報告の印が付いた、その担当者のコメントだけである。**
// **投稿者だけで数えてはならない。**エージェントも continuo も人間も、同じ GitHub アカウントで
// 投稿する（[internal/tracker/ghuser.go](../tracker/ghuser.go) の 23-25行）ので、
// **人間が無関係なコメントを1件書いただけで、黙り込んだエージェントの期限が延びる。**
// **それでは死活確認にならない。**
//
// **hold のコメントが作られた時刻を下限に置く。**その担当が始まった時刻である。
//
//	置かないと、勝った直後（進捗報告がまだ1件も無い）にその場で期限切れになる
//	置かないと、前の担当のときに書かれた古い進捗報告のほうが新しく見え、始めたばかりの担当が外れる
//
// **書き足しも生きている証拠として数える**（設計 5-3k）。エージェントは進捗の報告を
// **いちばん下にある自分のコメントへ書き足す**ので（設計 5-3j）、
// **作成時刻だけを見ると、1時間ごとに書き続けている機械の期限が1秒も進まない。**
// **だから `LastTouched`（作成時刻と更新時刻の新しいほう）で数える。**
//
// **`updatedAt` を見るのは、このパッケージではここだけである。**
// 入札・hold・released・回の区切りは投稿の順番を決めているので、**編集で動かせてはならない。**
//
// comments: issue に付いているコメントの全件。
// login: 担当者のログイン名。
// holdAt: いまの担当の hold のコメントが作られた時刻。**ゼロ値なら下限を置かない。**
// 戻り値の1つ目: いちばん新しい時刻。
// 戻り値の2つ目: 起点が1つでもあれば true（**hold の時刻か、その担当者の進捗報告**）。
func lastProgressOf(comments []CommentView, login string, holdAt time.Time) (time.Time, bool) {
	latest := holdAt
	found := !holdAt.IsZero()
	for _, c := range comments {
		if !strings.EqualFold(strings.TrimSpace(c.Author), strings.TrimSpace(login)) {
			continue
		}
		if !IsProgressReport(c.Body) {
			continue
		}
		touched := c.LastTouched()
		if !found || touched.After(latest) {
			latest = touched
			found = true
		}
	}
	return latest, found
}

// CollectBids は、コメントの全件から入札を拾う（設計 3-77a）。
//
// **`Bid.Author` には投稿者を渡す**（設計 3-77-0）。**持ち回りで参加者を見分ける値である。**
// **本文の JSON からは取らない。**本文は第三者にも書けるので、
// **他の continuo のログイン名を騙られると、騙られたほうは `HasBidBy` が真になって
// その回は入札しない。**GitHub が付ける投稿者は騙れない。
//
// **`Bid.PostedAt` には作成時刻を渡す。更新時刻は使わない**（設計 5-3k）。
// **入札の締め切りと勝敗は、この時刻で決まる。**編集で動かせるようにすると、
// **負けた continuo が、あとから自分の入札を「新しく」できる。**
//
// comments: issue に付いているコメントの全件。
// 戻り値: 読めた入札（コメントの並び順のまま）。**投稿者の分からないものは1件も入らない。**
func CollectBids(comments []CommentView) []Bid {
	out := make([]Bid, 0, len(comments))
	for _, c := range comments {
		if b, ok := ParseBid(c.Body, c.Author, c.CreatedAt); ok {
			out = append(out, b)
		}
	}
	return out
}

// LatestHoldFor は、その担当者が書いたいちばん新しい hold を拾う（設計 3-77b）。
//
// **担当者で絞る。**hold のコメントは、担当が移っても入札の回が変わっても**消えない。**
// **絞らないと、issue のどこかに1件でも hold があれば「いまの担当者は機械である」と読まれる。**
// 機械が外れたあとに人間が自分を担当者にして18時間黙ると、
// **別の機械が古い機械の hold を証拠にして、人間の担当を外す。**
//
// **`Hold.Assignee` が空の hold は、誰のものとも数えない。**continuo が書く hold は必ず
// 担当者のログイン名を持つので、空なのは人間が印だけ真似て書いたときである。
// **触らない側へ倒す**（奪ってよい証拠として使わない）。
//
// **いちばん新しいものを採る。**担当が何度か移った issue には hold が複数付いており、
// **古いほうを読むと、既に使われていない branch の名前を released のコメントへ書くことになる。**
//
// **新しいかどうかは作成時刻で見る。更新時刻は使わない**（設計 5-3k）。
// **使うと、古い hold を1文字直すだけで最新の hold に化け、担当が始まった時刻と branch の名前が入れ替わる。**
//
// **作成時刻も返す。**持ち回りの期限は、その担当が始まった時刻を下限にして数える
// （`lastProgressOf`）。**JSON の中の `at` は投稿者が自分で書いた値なので使わない。**
// 時計を戻した機械が、自分の担当をいくらでも延ばせてしまう。
//
// comments: issue に付いているコメントの全件。
// assignee: いま付いている担当者のログイン名。**空文字なら1件も返さない。**
// 戻り値の1つ目: いちばん新しい hold。
// 戻り値の2つ目: その hold のコメントが作られた時刻。
// 戻り値の3つ目: その担当者の hold が1件でもあれば true。
func LatestHoldFor(comments []CommentView, assignee string) (Hold, time.Time, bool) {
	login := strings.TrimSpace(assignee)
	if login == "" {
		return Hold{}, time.Time{}, false
	}
	var latest Hold
	var latestAt time.Time
	found := false
	for _, c := range comments {
		h, ok := ParseHold(c.Body)
		if !ok {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(h.Assignee), login) {
			continue
		}
		if !found || c.CreatedAt.After(latestAt) {
			latest = h
			latestAt = c.CreatedAt
			found = true
		}
	}
	return latest, latestAt, found
}

// ParseReleased はコメント本文から released を読む（設計 3-77c）。
//
// body: コメント本文。
// 戻り値の1つ目: 読み取った released。
// 戻り値の2つ目: released として読めれば true。
func ParseReleased(body string) (Released, bool) {
	payload, ok := payloadAfterMarker(body, config.HandoffReleasedMarker)
	if !ok {
		return Released{}, false
	}
	var r Released
	if err := json.Unmarshal([]byte(payload), &r); err != nil {
		return Released{}, false
	}
	return r, true
}

// LatestReleased は、コメントの全件からいちばん新しい released を拾う（設計 3-77c）。
//
// **担当を外された機械が「何が起きたか」を記録に残すために読む。**
// これが無いと、ログには「担当が移った」としか残らず、
// **いつ・どのアカウントの担当が外されたのかを人間があとから辿れない。**
//
// **新しいかどうかは作成時刻で見る。更新時刻は使わない**（`LatestHoldFor` と同じ理由。設計 5-3k）。
//
// comments: issue に付いているコメントの全件。
// 戻り値の1つ目: いちばん新しい released。
// 戻り値の2つ目: released が1件でもあれば true。
func LatestReleased(comments []CommentView) (Released, bool) {
	var latest Released
	var latestAt time.Time
	found := false
	for _, c := range comments {
		r, ok := ParseReleased(c.Body)
		if !ok {
			continue
		}
		if !found || c.CreatedAt.After(latestAt) {
			latest = r
			latestAt = c.CreatedAt
			found = true
		}
	}
	return latest, found
}

// RoundStart は、いまの回が始まりうるいちばん早い時刻を返す（設計 3-77e）。
//
// **前の回は hold か released で終わっている。**hold は勝った機械が担当者になったときに、
// released は期限の切れた担当を外したときに書かれる。
// **どちらかが現れた時点で、それより前の入札は前の回のものである。**
//
// **これが無いと、次の回が始まらない。**入札は1回ごとに新しいコメントを書くので、
// **前の回の入札は issue に残り続ける。**締め切りをその古い入札から数え続けると、
// 締め切りは常に過ぎたことになり、**担当者が永久に決まらない。**
//
// **時刻はコメントの作成時刻で見る。**入札の JSON の `at` は投稿者が自分で書いた値なので、
// **時計を戻せば回の区切りを跨げてしまう。**
//
// **更新時刻も使わない**（設計 5-3k）。使うと、**古い hold を1文字編集するだけで区切りが未来へ動き、
// 締め切りが永久に来ず、担当者が決まらなくなる。**
//
// comments: issue に付いているコメントの全件。
// 戻り値の1つ目: いちばん新しい hold か released の作成時刻。
// 戻り値の2つ目: どちらかが1件でもあれば true。
func RoundStart(comments []CommentView) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, c := range comments {
		if !endsRound(c.Body) {
			continue
		}
		if !found || c.CreatedAt.After(latest) {
			latest = c.CreatedAt
			found = true
		}
	}
	return latest, found
}

// endsRound は、そのコメントが入札の回を閉じるものかを返す。
//
// **入札の印は数えない。**入札は回を閉じない（回を開くものである）。
//
// body: コメント本文。
// 戻り値: hold か released として読めれば true。
func endsRound(body string) bool {
	if _, ok := ParseHold(body); ok {
		return true
	}
	_, ok := ParseReleased(body)
	return ok
}

// HasBidBy は、そのアカウントが既に入札を書いているかを返す。
//
// **書いていれば、締め切りまで待つだけである。**入札のたびに新しいコメントを書くので、
// **これを見ないと巡回のたびに入札が1件ずつ増える。**
//
// bids: その issue に付いている入札。
// login: gh の持ち主のログイン名（設計 3-77-0）。
// 戻り値の1つ目: そのアカウントが最後に書いた入札。
// 戻り値の2つ目: 1件でもあれば true。
func HasBidBy(bids []Bid, login string) (Bid, bool) {
	var latest Bid
	found := false
	for _, b := range bids {
		if !strings.EqualFold(strings.TrimSpace(b.Author), strings.TrimSpace(login)) {
			continue
		}
		if !found || b.PostedAt.After(latest.PostedAt) {
			latest = b
			found = true
		}
	}
	return latest, found
}

// FormatReleased は released のコメントの本文を組み立てる（設計 3-77c）。
//
// **JSON の下に、人間が読む2行を置く。**issue を開いた人が、何が起きたのかと
// 「その branch へ push してはならない」ことを、issue の上だけで読めるようにする。
//
// **引き継ぐアカウントは書かない。**外すのは入札をやり直す前なので、
// そのとき勝つ continuo はまだ決まっていない（設計 3-77c）。
//
// r: 書く released。
// 戻り値: 印を先頭に置いたコメント本文。
//
// **GitHub に載る本文とは1行違う。**投稿する直前に
// `config.WithAIMarker` が印の直後へ `<!-- continuo:ai -->` を足すためである（設計 3-82）。
// **先頭の印は動かないので、`IsMarked` も `payloadAfterMarker` もそのまま通る。**
func FormatReleased(r Released) string {
	return config.HandoffReleasedMarker + "\n" + marshalLine(r) + "\n\n" +
		i18n.T(i18n.KeyHandoffReleasedReassign) + "\n" +
		i18n.T(i18n.KeyHandoffReleasedDoNotPush, r.From) + "\n"
}
