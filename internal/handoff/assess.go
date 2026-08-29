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
	CreatedAt time.Time
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
//	担当者が2人以上                          触らない。WARN を出す
//	担当者が無い                             入札する
//	担当者が自分1人                          着手・引き継ぎへ進む（入札しない）
//	他人1人 ＋ hold が1件も無い               触らない。人間が付けた担当である
//	他人1人 ＋ hold あり ＋ 期限内             触らない。入札もしない
//	他人1人 ＋ hold あり ＋ 期限切れ           担当を外して入札をやり直す
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
	if strings.EqualFold(assignee, strings.TrimSpace(s.SelfLogin)) {
		return Assessment{Action: ActionProceed, Assignee: assignee}
	}

	hold, ok := LatestHold(s.Comments)
	if !ok {
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
		if !found || c.CreatedAt.After(latest) {
			latest = c.CreatedAt
			found = true
		}
	}
	return latest, found
}

// CollectBids は、コメントの全件から入札を拾う（設計 3-77a）。
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

// LatestHold は、コメントの全件からいちばん新しい hold を拾う（設計 3-77b）。
//
// **いちばん新しいものを採る。**担当が何度か移った issue には hold が複数付いており、
// **古いほうを読むと、既に居ない機械の名前を released のコメントへ書くことになる。**
//
// comments: issue に付いているコメントの全件。
// 戻り値の1つ目: いちばん新しい hold。
// 戻り値の2つ目: hold が1件でもあれば true。
func LatestHold(comments []CommentView) (Hold, bool) {
	var latest Hold
	var latestAt time.Time
	found := false
	for _, c := range comments {
		h, ok := ParseHold(c.Body)
		if !ok {
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
