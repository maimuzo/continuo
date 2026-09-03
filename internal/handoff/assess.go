package handoff

import (
	"encoding/json"
	"strings"
	"time"

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
	// **これを見るのは `lastActivityOf` だけである。**
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
	// **空文字なら「自分が誰か分からない」である。**そのときは担当者が付いている issue に
	// 一切触らない（自分の担当かどうかを言えないので、奪う側にも進む側にも倒せない）。
	SelfLogin string
	// SelfHost はこの機械の名前である（`os.Hostname()` の値。設計 3-77）。
	//
	// **担当者のアカウントだけでは足りない。**1人が2台の機械を1つの GitHub アカウントで
	// 動かすのは、この機能のいちばん自然な使い方である。**アカウントだけで比べると、
	// 勝った機械と負けた機械の両方が「担当者は自分だ」と読み、同じ issue に2台が着手する。**
	// **だから hold のコメントの `host` と突き合わせる。**
	//
	// **空文字なら機械の名前で区別しない**（アカウントだけで判定していた頃と同じ動きになる）。
	SelfHost string
	// Now はいまの時刻である。
	Now time.Time
	// IdleTimeout は担当者の最後のコメントからこれだけ経つと担当を外す長さである。
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
	// ActionSkipOtherMachine は、担当者は自分のアカウントだが hold を持っているのは
	// 同じアカウントの別の機械なので触らないことを表す。**入札もしない。**
	//
	// **1人が2台の機械を1つのアカウントで動かすと、ここへ来る**（設計 3-77b）。
	// 担当者のアカウントだけを見て進むと、同じ issue に2台が着手する。
	ActionSkipOtherMachine
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
	case ActionSkipOtherMachine:
		return "同じアカウントの別の機械の担当"
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
	// Hold は、担当を外すときに読んだ hold のコメントである（ActionRelease のときだけ入る）。
	//
	// **released のコメントの `from` はここの `Host` から引く**（設計 3-77c）。
	Hold Hold
	// LastActivity は担当者が最後にコメントを書いた時刻である（担当者がいるときだけ入る）。
	//
	// **hold のコメントも担当者の活動として数える。**勝った直後は hold しか無いので、
	// 数えないと「最後のコメントが無い＝期限切れ」と読まれて、着手する前に担当を外される。
	LastActivity time.Time
}

// Assess は、いま見えているものから「次に何をするか」を決める（設計 3-77b の表）。
//
//	担当者が2人以上                                触らない。WARN を出す
//	担当者が無い                                   入札する
//	自分1人 ＋ この機械の hold                      着手・引き継ぎへ進む（入札しない）
//	自分1人 ＋ hold が1件も無い                     着手・引き継ぎへ進む（人間が付けた担当である）
//	自分1人 ＋ 別の機械の hold ＋ 期限内             触らない。入札もしない
//	自分1人 ＋ 別の機械の hold ＋ 期限切れ           担当を外して入札をやり直す
//	他人1人 ＋ hold が1件も無い                     触らない。人間が付けた担当である。WARN を出す
//	他人1人 ＋ hold あり ＋ 期限内                   触らない。入札もしない
//	他人1人 ＋ hold あり ＋ 期限切れ                 担当を外して入札をやり直す
//
// **担当者のアカウントだけで「自分の担当」と決めてはならない**（設計 3-77b）。
// **1人が2台の機械を1つの GitHub アカウントで動かすのが、この機能のいちばん自然な使い方である。**
// アカウントだけで比べると、勝った機械と負けた機械の両方が「担当者は自分だ」と読み、
// **同じ branch の worktree に2つ目の Claude Code が立つ。**
// **どの機械のものかは hold のコメントの `host` が答える。**
//
// **hold は「いまの担当者が書いたもの」だけを数える**（設計 3-77b）。
// **入札の回が変わっても hold のコメントは消えない**ので、機械が外れたあとに人間が
// 自分を担当者にすると、**古い機械の hold が「この担当者は機械である」の証拠に化ける。**
// **`Hold.Assignee` で担当者を突き合わせて、その化けを止める。**
//
// **期限は「hold を書いてから」ではなく「その担当者の最後のコメントが現れてから」で数える**
// （設計 3-77b）。進捗を書き続けている機械は担当を外されない。
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

	// **hold は、いまの担当者が書いたものだけを見る。**別の担当者の古い hold を数えると、
	// **人間が引き継いだ issue を機械が取り上げる。**
	hold, hasHold := LatestHoldFor(s.Comments, assignee)

	if strings.EqualFold(assignee, strings.TrimSpace(s.SelfLogin)) {
		return assessSelfAssigned(s, assignee, hold, hasHold)
	}

	if !hasHold {
		return Assessment{Action: ActionSkipHumanAssigned, Assignee: assignee}
	}

	last, hasLast := lastActivityOf(s.Comments, assignee)
	if !hasLast {
		// **担当者の書いたコメントが1件も無い。**hold は別の機械が書いたことになるので、
		// **期限を数える起点が無い。**触らない側へ倒す（奪って良い証拠が無い）。
		return Assessment{Action: ActionSkipHeld, Assignee: assignee, Hold: hold}
	}
	if s.Now.Sub(last) <= s.IdleTimeout {
		return Assessment{Action: ActionSkipHeld, Assignee: assignee, Hold: hold, LastActivity: last}
	}
	return Assessment{Action: ActionRelease, Assignee: assignee, Hold: hold, LastActivity: last}
}

// assessSelfAssigned は「担当者が自分のアカウント1人」のときの判定である（設計 3-77b）。
//
// **アカウントが自分でも、この機械の担当だとは限らない。**1つのアカウントで2台の機械を
// 動かしていると、**勝ったのは別の機械かもしれない。**それは hold のコメントの `host` で分かる。
//
// **hold が1件も無いときは進む。**そこは人間が担当者を付けた issue であり、
// 設計 3-77b の表が「担当者が自分1人なら着手・引き継ぎへ進む」と決めている。
// **この機械の名前を知らない（`SelfHost` が空）ときも進む。**機械の名前で区別できないので、
// アカウントだけで判定していた頃と同じ動きへ落とす。
//
// s: いま見えているもの。
// assignee: いま付いている担当者のログイン名（自分のアカウント）。
// hold: その担当者が書いたいちばん新しい hold。
// hasHold: hold が1件でもあれば true。
// 戻り値: 判定と、その判定に使った値。
func assessSelfAssigned(s Situation, assignee string, hold Hold, hasHold bool) Assessment {
	selfHost := strings.TrimSpace(s.SelfHost)
	holdHost := strings.TrimSpace(hold.Host)
	if !hasHold || selfHost == "" || holdHost == "" {
		return Assessment{Action: ActionProceed, Assignee: assignee}
	}
	if strings.EqualFold(holdHost, selfHost) {
		return Assessment{Action: ActionProceed, Assignee: assignee, Hold: hold}
	}

	// **同じアカウントの別の機械が担当している。**
	// **期限の数え方は他人の担当と揃える**（設計 3-77b）。揃えないと、その機械が落ちたとき
	// **担当者が自分のアカウントのままなので、どの機械もこの issue を拾えなくなる。**
	last, hasLast := lastActivityOf(s.Comments, assignee)
	if !hasLast {
		return Assessment{Action: ActionSkipOtherMachine, Assignee: assignee, Hold: hold}
	}
	if s.Now.Sub(last) <= s.IdleTimeout {
		return Assessment{Action: ActionSkipOtherMachine, Assignee: assignee, Hold: hold, LastActivity: last}
	}
	return Assessment{Action: ActionRelease, Assignee: assignee, Hold: hold, LastActivity: last}
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

// lastActivityOf は、その担当者が最後に何かを書いた時刻を返す。
//
// **数えるのは投稿者が一致するコメント全部である。**進捗のコメントも hold のコメントも、
// どちらも「その機械はまだ生きている」ことの証拠なので同じに扱う（設計 3-77b）。
//
// **書き足しも同じ証拠として数える**（設計 5-3k）。エージェントは進捗の報告を
// **いちばん下にある自分のコメントへ書き足す**ので（設計 5-3j）、
// **作成時刻だけを見ると、1時間ごとに書き続けている機械の期限が1秒も進まない。**
// **だから `LastTouched`（作成時刻と更新時刻の新しいほう）で数える。**
//
// **`updatedAt` を見るのは、このパッケージではここだけである。**
// 入札・hold・released・回の区切りは投稿の順番を決めているので、**編集で動かせてはならない。**
//
// **進捗の報告の印で絞り込まない。**印は本文の先頭に置くただの文字列で、誰でも書ける（設計 3-65）。
// **絞り込むと、印1つで期限の数え方が変わる。**投稿者が一致することは既に上で見ている。
//
// comments: issue に付いているコメントの全件。
// login: 担当者のログイン名。
// 戻り値の1つ目: いちばん新しい時刻。
// 戻り値の2つ目: その担当者のコメントが1件でもあれば true。
func lastActivityOf(comments []CommentView, login string) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, c := range comments {
		if !strings.EqualFold(strings.TrimSpace(c.Author), strings.TrimSpace(login)) {
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
// **`Bid.PostedAt` には作成時刻を渡す。更新時刻は使わない**（設計 5-3k）。
// **入札の締め切りと勝敗は、この時刻で決まる。**編集で動かせるようにすると、
// **負けた機械が、あとから自分の入札を「新しく」できる。**
//
// comments: issue に付いているコメントの全件。
// 戻り値: 読めた入札（コメントの並び順のまま）。
func CollectBids(comments []CommentView) []Bid {
	out := make([]Bid, 0, len(comments))
	for _, c := range comments {
		if b, ok := ParseBid(c.Body, c.CreatedAt); ok {
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
// **古いほうを読むと、既に居ない機械の名前を released のコメントへ書くことになる。**
//
// **新しいかどうかは作成時刻で見る。更新時刻は使わない**（設計 5-3k）。
// **使うと、古い hold を1文字直すだけで最新の hold に化け、担当している機械の名前が入れ替わる。**
//
// comments: issue に付いているコメントの全件。
// assignee: いま付いている担当者のログイン名。**空文字なら1件も返さない。**
// 戻り値の1つ目: いちばん新しい hold。
// 戻り値の2つ目: その担当者の hold が1件でもあれば true。
func LatestHoldFor(comments []CommentView, assignee string) (Hold, bool) {
	login := strings.TrimSpace(assignee)
	if login == "" {
		return Hold{}, false
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
	return latest, found
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
// **いつ・どの機械の担当が外されたのかを人間があとから辿れない。**
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

// HasBidBy は、その機械が既に入札を書いているかを返す。
//
// **書いていれば、締め切りまで待つだけである。**入札のたびに新しいコメントを書くので、
// **これを見ないと巡回のたびに入札が1件ずつ増える。**
//
// bids: その issue に付いている入札。
// host: この機械の名前。
// 戻り値の1つ目: この機械が最後に書いた入札。
// 戻り値の2つ目: 1件でもあれば true。
func HasBidBy(bids []Bid, host string) (Bid, bool) {
	var latest Bid
	found := false
	for _, b := range bids {
		if !strings.EqualFold(strings.TrimSpace(b.Host), strings.TrimSpace(host)) {
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
// **引き継ぐ機械の名前は書かない。**外すのは入札をやり直す前なので、
// そのとき勝つ機械はまだ決まっていない（設計 3-77c）。
//
// r: 書く released。
// 戻り値: 印を先頭に置いたコメント本文。
func FormatReleased(r Released) string {
	return config.HandoffReleasedMarker + "\n" + marshalLine(r) + "\n\n" +
		i18n.T(i18n.KeyHandoffReleasedReassign) + "\n" +
		i18n.T(i18n.KeyHandoffReleasedDoNotPush, r.From) + "\n"
}
