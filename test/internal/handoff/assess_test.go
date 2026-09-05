package handoff_test

import (
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/i18n"
)

// selfLogin はテストで使う「この機械の gh の持ち主」である（架空の名前）。
const selfLogin = "octocat-bot-a"

// otherLogin はテストで使う「別の機械の gh の持ち主」である（架空の名前）。
const otherLogin = "octocat-bot-b"

// idleTimeout はテストで使う担当の期限である（設計 3-77b の既定と同じ18時間）。
const idleTimeout = 18 * time.Hour

// holdComment は hold のコメント1件を組み立てる。
//
// author: 書き手のログイン名。**hold の `assignee` にも同じ値が入る。**
// created: 作成時刻。
// 戻り値: 判定へ渡す形のコメント。
func holdComment(author string, created time.Time) handoff.CommentView {
	return holdCommentOn(author, "continuo/octocat/hello-world/188", created)
}

// holdCommentOn は branch の名前を指定して hold のコメント1件を組み立てる。
//
// **どの hold を読んだかを見分けるために branch を使う。**
// 持ち回りで参加者を見分ける値はアカウントのログイン名なので（設計 3-77-0）、
// **同じ担当者の hold は、branch でしか見分けられない。**
//
// author: 書き手のログイン名。
// branch: hold に書く branch の名前。
// created: 作成時刻。
// 戻り値: 判定へ渡す形のコメント。
func holdCommentOn(author, branch string, created time.Time) handoff.CommentView {
	return handoff.CommentView{
		Author: author,
		Body: handoff.FormatHold(handoff.Hold{
			Assignee: author, Branch: branch, At: created,
		}),
		CreatedAt: created,
	}
}

// progressComment は、エージェントの進捗報告のコメント1件を組み立てる（設計 5-3j / 5-3l）。
//
// **持ち回りの期限を進めるのは、この印が付いたコメントだけである。**
//
// author: 書き手のログイン名。
// created: 作成時刻。
// updated: 最後に編集された時刻（**ゼロ値なら編集されていない**）。
// 戻り値: 判定へ渡す形のコメント。
func progressComment(author string, created, updated time.Time) handoff.CommentView {
	return handoff.CommentView{
		Author:    author,
		Body:      "<!-- continuo:agent -->\n" + config.ProgressMarker + "\nまだ作業中です。",
		CreatedAt: created,
		UpdatedAt: updated,
	}
}

// 目的: 見えているものと判定の対応が、設計 3-77b の表のとおりであることを確認する。
//
// **この表が壊れると、人間が付けた担当を機械が奪う。**
//
// 与える情報: 表の6行それぞれに当たる状況。
// 成功条件: それぞれ表のとおりの判定になること。
func TestAssess_見えているものと判定の対応(t *testing.T) {
	now := at()
	fresh := now.Add(-time.Hour)
	stale := now.Add(-idleTimeout - time.Minute)

	cases := []struct {
		name string
		s    handoff.Situation
		want handoff.Action
	}{
		{
			name: "担当者が無い",
			s:    handoff.Situation{SelfLogin: selfLogin},
			want: handoff.ActionBid,
		},
		{
			name: "担当者が自分1人",
			s:    handoff.Situation{Assignees: []string{selfLogin}, SelfLogin: selfLogin},
			want: handoff.ActionProceed,
		},
		{
			name: "担当者が2人以上",
			s:    handoff.Situation{Assignees: []string{selfLogin, otherLogin}, SelfLogin: selfLogin},
			want: handoff.ActionSkipManyAssignees,
		},
		{
			name: "他人1人でholdが1件も無い",
			s: handoff.Situation{
				Assignees: []string{otherLogin},
				Comments:  []handoff.CommentView{{Author: otherLogin, Body: "進めています", CreatedAt: fresh}},
				SelfLogin: selfLogin,
			},
			want: handoff.ActionSkipHumanAssigned,
		},
		{
			name: "他人1人でholdありで期限内",
			s: handoff.Situation{
				Assignees:   []string{otherLogin},
				Comments:    []handoff.CommentView{holdComment(otherLogin, fresh)},
				SelfLogin:   selfLogin,
				Now:         now,
				IdleTimeout: idleTimeout,
			},
			want: handoff.ActionSkipHeld,
		},
		{
			name: "他人1人でholdありで期限切れ",
			s: handoff.Situation{
				Assignees:   []string{otherLogin},
				Comments:    []handoff.CommentView{holdComment(otherLogin, stale)},
				SelfLogin:   selfLogin,
				Now:         now,
				IdleTimeout: idleTimeout,
			},
			want: handoff.ActionRelease,
		},
		{
			name: "gh の持ち主が分からない",
			s:    handoff.Situation{Assignees: []string{otherLogin}, SelfLogin: ""},
			want: handoff.ActionSkipSelfUnknown,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := handoff.Assess(c.s)
			if got.Action != c.want {
				t.Errorf("判定が違う: got %v, want %v", got.Action, c.want)
			}
		})
	}
}

// 目的: 期限を「担当者の最後の進捗報告」から数えることを確認する（設計 3-77b / 5-3l）。
//
// **hold を書いた時刻から数えてはならない。**進捗を書き続けている機械が
// 18時間で担当を外されることになる。
//
// 与える情報: hold は19時間前だが、担当者の進捗報告は1時間前にある状況。
// 成功条件: 担当を外さない（`期限内の担当`）こと。
func TestAssess_進捗を書き続けている機械の担当は外さない(t *testing.T) {
	now := at()
	got := handoff.Assess(handoff.Situation{
		Assignees: []string{otherLogin},
		Comments: []handoff.CommentView{
			holdComment(otherLogin, now.Add(-19*time.Hour)),
			progressComment(otherLogin, now.Add(-time.Hour), time.Time{}),
		},
		SelfLogin:   selfLogin,
		Now:         now,
		IdleTimeout: idleTimeout,
	})

	if got.Action != handoff.ActionSkipHeld {
		t.Fatalf("進捗を書いている機械の担当を外そうとしている: got %v, want %v",
			got.Action, handoff.ActionSkipHeld)
	}
	if !got.LastProgress.Equal(now.Add(-time.Hour)) {
		t.Errorf("期限の起点が最後の進捗報告になっていない: got %v, want %v",
			got.LastProgress, now.Add(-time.Hour))
	}
}

// 目的: 進捗報告の印が付いていないコメントでは、持ち回りの期限が1秒も延びないことを固定する
// （#194（進捗コメントの間隔と重ね方が、人間の決定と逆に実装されている）。設計 5-3l）。
//
// **なぜ要るか。**エージェントも continuo も人間も、同じ GitHub アカウントで投稿する
// （[internal/tracker/ghuser.go:23-25](../../../internal/tracker/ghuser.go#L23-L25)）。
// **投稿者だけで数えると、人間が無関係なコメントを1件書いただけで期限が18時間先へ延びる。**
// **黙り込んだエージェントを、別の機械が永久に拾い直せない。**死活確認そのものが成り立たない。
//
// 与える情報: hold は19時間前。担当者のアカウントで、印の無いコメントが1分前に投稿されている状況。
// 成功条件: 担当を外すこと。期限の起点が hold の時刻（19時間前）のままであること。
func TestAssess_印の無いコメントでは期限が延びない(t *testing.T) {
	now := at()
	held := now.Add(-19 * time.Hour)

	got := handoff.Assess(handoff.Situation{
		Assignees: []string{otherLogin},
		Comments: []handoff.CommentView{
			holdComment(otherLogin, held),
			// **人間が書いた無関係なコメントである。**投稿者は担当者と同じアカウントになる。
			{Author: otherLogin, Body: "ここはどうなっていますか", CreatedAt: now.Add(-time.Minute)},
		},
		SelfLogin:   selfLogin,
		Now:         now,
		IdleTimeout: idleTimeout,
	})

	if got.Action != handoff.ActionRelease {
		t.Fatalf("印の無いコメント1件で、黙り込んだ機械の担当が延びている: got %v, want %v",
			got.Action, handoff.ActionRelease)
	}
	if !got.LastProgress.Equal(held) {
		t.Errorf("期限の起点が印の無いコメントへ動いている: got %v, want %v（hold の時刻）",
			got.LastProgress, held)
	}
}

// 目的: 進捗報告が1件も無いあいだは、hold のコメントが作られた時刻から数えることを固定する
// （設計 5-3l）。
//
// **なぜ要るか。**入札に勝った直後には、進捗報告がまだ1件も無い。
// **下限を置かないと、その場で「18時間経った」と読まれ、着手する前に担当が外れる。**
//
// 与える情報: hold だけがある状況（1時間前と、18時間1分前の2通り）。
// 成功条件: 1時間前なら触らない。18時間1分前なら担当を外すこと。
func TestAssess_進捗報告が無ければholdの時刻から数える(t *testing.T) {
	now := at()
	for _, c := range []struct {
		name string
		held time.Time
		want handoff.Action
	}{
		{"勝った直後", now.Add(-time.Hour), handoff.ActionSkipHeld},
		{"勝ったまま黙り込んだ", now.Add(-idleTimeout - time.Minute), handoff.ActionRelease},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := handoff.Assess(handoff.Situation{
				Assignees:   []string{otherLogin},
				Comments:    []handoff.CommentView{holdComment(otherLogin, c.held)},
				SelfLogin:   selfLogin,
				Now:         now,
				IdleTimeout: idleTimeout,
			})
			if got.Action != c.want {
				t.Fatalf("判定が違う: got %v, want %v", got.Action, c.want)
			}
			if !got.LastProgress.Equal(c.held) {
				t.Errorf("期限の起点が hold の時刻になっていない: got %v, want %v",
					got.LastProgress, c.held)
			}
		})
	}
}

// 目的: 前の担当のときに書かれた古い進捗報告で、始めたばかりの担当を外さないことを固定する
// （設計 5-3l）。
//
// **なぜ要るか。**進捗報告のコメントは、担当が移っても消えない。
// **hold の時刻を下限に置かないと、古い進捗報告のほうが「最後の進捗」として読まれ、
// 1分前に担当を取ったばかりの機械がその場で外される。**
//
// 与える情報: 100時間前の進捗報告が残っており、hold は1分前に書かれた状況。
// 成功条件: 担当を外さないこと。期限の起点が hold の時刻であること。
func TestAssess_前の担当の古い進捗報告では新しい担当を外さない(t *testing.T) {
	now := at()
	held := now.Add(-time.Minute)

	got := handoff.Assess(handoff.Situation{
		Assignees: []string{otherLogin},
		Comments: []handoff.CommentView{
			progressComment(otherLogin, now.Add(-100*time.Hour), time.Time{}),
			holdComment(otherLogin, held),
		},
		SelfLogin:   selfLogin,
		Now:         now,
		IdleTimeout: idleTimeout,
	})

	if got.Action != handoff.ActionSkipHeld {
		t.Fatalf("担当を取ったばかりの機械を、古い進捗報告を根拠に外している: got %v, want %v",
			got.Action, handoff.ActionSkipHeld)
	}
	if !got.LastProgress.Equal(held) {
		t.Errorf("期限の起点が古い進捗報告へ落ちている: got %v, want %v（hold の時刻）",
			got.LastProgress, held)
	}
}

// 目的: 進捗報告かどうかの見分け方が、組み込みのプロンプトの見つけ方と揃っていることを固定する
// （設計 5-3l）。
//
// **なぜ要るか。**組み込みのプロンプトは、エージェント自身に
// `.body | contains("<!-- continuo:progress -->")` で書き足す先を探させる（設計 5-3j の段1）。
// **Go 側だけを厳しくすると、エージェントは自分の進捗報告だと思って書き足し続けているのに
// continuo が数えず、生きている担当が18時間で外れる。**
//
// 与える情報: 印が2行目にあるコメント・印だけのコメント・印の無いコメント。
// 成功条件: 印を含むものだけが真になること。
func TestIsProgressReport_印を含むものだけを数える(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
		want bool
	}{
		{"組み込みが書かせる形", "<!-- continuo:agent -->\n" + config.ProgressMarker + "\nまだ作業中です。", true},
		{"印だけ", config.ProgressMarker, true},
		{"エージェントの印だけ", "<!-- continuo:agent -->\n実装しました", false},
		{"印が無い", "ここはどうなっていますか", false},
		{"空", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := handoff.IsProgressReport(c.body); got != c.want {
				t.Errorf("判定が違う: got %v, want %v（本文 %q）", got, c.want, c.body)
			}
		})
	}
}

// 目的: 担当を外すときに読む hold が、いちばん新しいものであることを確認する（設計 3-77c）。
//
// **古い hold を読むと、既に使われていない branch の名前を released のコメントへ書くことになる。**
//
// 与える情報: hold が2件（古いほうの branch が `old-branch`、新しいほうが `new-branch`）ある状況。
// 成功条件: 新しいほうの branch の名前が返ること。
func TestAssess_担当を外すとき新しいholdのbranchを返す(t *testing.T) {
	now := at()
	got := handoff.Assess(handoff.Situation{
		Assignees: []string{otherLogin},
		Comments: []handoff.CommentView{
			holdCommentOn(otherLogin, "old-branch", now.Add(-100*time.Hour)),
			holdCommentOn(otherLogin, "new-branch", now.Add(-50*time.Hour)),
		},
		SelfLogin:   selfLogin,
		Now:         now,
		IdleTimeout: idleTimeout,
	})

	if got.Action != handoff.ActionRelease {
		t.Fatalf("期限切れなのに担当を外さない: got %v", got.Action)
	}
	if got.Hold.Branch != "new-branch" {
		t.Errorf("いちばん新しい hold を読んでいない: got %q, want %q", got.Hold.Branch, "new-branch")
	}
}

// 目的: 入札のコメントの形が設計 3-77a のとおりであることと、書いたものを読み戻せることを確認する。
//
// 与える情報: 入札1件。
// 成功条件: 印で始まり、JSON のキーが `five_hour` / `weekly` / `score` / `at` であり、
// **`host` のキーが1つも無いこと**（設計 3-77-0。誰が書いたかは投稿者が答える）。
// 時刻がその機械のタイムゾーンのままであること。読み戻すと同じ値になること。
func TestFormatBid_コメントの形と読み戻し(t *testing.T) {
	bid := handoff.Bid{Author: selfLogin, FiveHour: 87, Weekly: 16, Score: 190, At: at()}
	body := handoff.FormatBid(bid, 3*time.Minute)

	if !strings.HasPrefix(body, config.HandoffBidMarker+"\n") {
		t.Fatalf("入札の印で始まっていない:\n%s", body)
	}
	for _, key := range []string{`"five_hour":87`, `"weekly":16`, `"score":190`} {
		if !strings.Contains(body, key) {
			t.Errorf("入札の JSON に %s がない:\n%s", key, body)
		}
	}
	// **自分で名乗る欄を本文へ書いてはならない**（設計 3-77-0）。
	// 書くと、本文の値と GitHub が付ける投稿者という、同じ事実の出どころが2つできる。
	if strings.Contains(body, `"host"`) {
		t.Errorf("入札の JSON に機械の名前の欄が残っている:\n%s", body)
	}
	if !strings.Contains(body, "+09:00") {
		t.Errorf("時刻がその機械のタイムゾーンで書かれていない（Z に直してはならない）:\n%s", body)
	}

	posted := at().Add(time.Second)
	got, ok := handoff.ParseBid(body, selfLogin, posted)
	if !ok {
		t.Fatalf("書いた入札を読み戻せない:\n%s", body)
	}
	if got.Author != selfLogin || got.Score != bid.Score || got.FiveHour != bid.FiveHour || got.Weekly != bid.Weekly {
		t.Errorf("読み戻した入札が違う: got %+v, want %+v", got, bid)
	}
	if !got.PostedAt.Equal(posted) {
		t.Errorf("投稿の時刻が入っていない: got %s, want %s", got.PostedAt, posted)
	}
}

// 目的: 印だけを真似た入札を数えないことを確認する（設計 3-77a）。
//
// **数えると、使用率0の入札が生まれて必ず勝ってしまう。**
//
// **投稿者の分からないコメントも数えない**（設計 3-77-0）。GitHub は削除済みアカウントの
// コメントに投稿者を付けない。**数えると、その入札が勝った回はどの continuo も着手しない。**
// **判定スコアがいちばん大きければ、同点にならなくても勝つ。**同点の3段目まで来た場合も、空文字がどのログイン名にも勝ち、
// その回はどの continuo も着手しなくなる。**
//
// **時刻の入っていないコメントも数えない。**continuo は入札に必ず `at` を書く。
// **これが無いと `{}` だけの本文が判定スコア0の入札として通り、
// `Deadline` の起点（いちばん古い投稿時刻）をその1件が奪う。**
//
// 与える情報: 印はあるが JSON が壊れているコメント・投稿者の分からないコメント・
// 時刻の入っていないコメント。
// 成功条件: どれも入札として読めないこと。
func TestParseBid_読めない入札は数えない(t *testing.T) {
	broken := config.HandoffBidMarker + "\nこれは JSON ではありません {"
	if _, ok := handoff.ParseBid(broken, selfLogin, at()); ok {
		t.Error("JSON でないコメントを入札として読んでいる")
	}

	valid := config.HandoffBidMarker + "\n" +
		`{"five_hour":100,"weekly":100,"score":300,"at":"2026-08-29T16:45:00+09:00"}`
	if _, ok := handoff.ParseBid(valid, "", at()); ok {
		t.Error("投稿者の分からないコメントを入札として読んでいる")
	}
	if _, ok := handoff.ParseBid(valid, "   ", at()); ok {
		t.Error("投稿者が空白だけのコメントを入札として読んでいる")
	}
	// **印だけ真似た本文は数えない。**時刻が入っていないものは continuo が書いたものではない。
	for _, body := range []string{
		config.HandoffBidMarker + "\n{}",
		config.HandoffBidMarker + "\n" + `{"five_hour":100,"weekly":100,"score":300}`,
	} {
		if _, ok := handoff.ParseBid(body, selfLogin, at()); ok {
			t.Errorf("時刻の無いコメントを入札として読んでいる:\n%s", body)
		}
	}
	// **投稿者さえ分かれば、機械の名前が無くても読める**（設計 3-77-0 の入札の形そのものである）。
	got, ok := handoff.ParseBid(valid, selfLogin, at())
	if !ok {
		t.Fatal("いまの形の入札を読めていない")
	}
	if got.Author != selfLogin {
		t.Errorf("入札の識別子に投稿者が入っていない: got %q, want %q", got.Author, selfLogin)
	}
}

// 目的: released のコメントに、引き継ぐ機械の名前を書かないことを確認する（設計 3-77c）。
//
// **外すのは入札をやり直す前なので、そのとき勝つ continuo はまだ決まっていない。**
// 書くと、外した側が負けたときに嘘になる。
//
// **`from` には、担当を外されたアカウントのログイン名が入る**（設計 3-77-0）。
//
// 与える情報: released 1件。
// 成功条件: 印で始まり、`from` と `branch` と `at` だけを持ち、`to` を持たないこと。
// 本文に「push しないでください」にあたる文言が入っていること。
func TestFormatReleased_引き継ぐアカウントは書かない(t *testing.T) {
	i18n.Use(i18n.SourceLang)
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })

	body := handoff.FormatReleased(handoff.Released{
		From: otherLogin, Branch: "continuo/octocat/hello-world/188", At: at(),
	})

	if !strings.HasPrefix(body, config.HandoffReleasedMarker+"\n") {
		t.Fatalf("released の印で始まっていない:\n%s", body)
	}
	if !strings.Contains(body, `"from":"`+otherLogin+`"`) {
		t.Errorf("外したアカウントの名前が入っていない:\n%s", body)
	}
	if strings.Contains(body, `"to":`) {
		t.Errorf("引き継ぐアカウントを書いている（この段では決まっていない）:\n%s", body)
	}
	if !strings.Contains(body, otherLogin+" のアカウントで走っていた作業") {
		t.Errorf("push してはならないことが人間に読める形で書かれていない:\n%s", body)
	}
}

// 目的: 3つの印のどれかで始まるコメントを、エージェントへ渡す入力から外す対象と判定することを
// 確認する（設計 3-77a）。
//
// **外すのは3つだけである。**`continuo:agent` と `continuo:self` は今までどおり渡す。
//
// 与える情報: 5種類の印で始まるコメント。
// 成功条件: 入札・hold・released だけが真になること。
func TestIsMarked_外すのは3つの印だけ(t *testing.T) {
	cases := []struct {
		marker string
		want   bool
	}{
		{config.HandoffBidMarker, true},
		{config.HandoffHoldMarker, true},
		{config.HandoffReleasedMarker, true},
		{"<!-- continuo:agent -->", false},
		{"<!-- continuo:self -->", false},
	}
	for _, c := range cases {
		got := handoff.IsMarked(c.marker + "\n本文")
		if got != c.want {
			t.Errorf("%s の判定が違う: got %v, want %v", c.marker, got, c.want)
		}
	}
}

// 目的: 同じアカウントが入札を2件書かないための判定が効くことを確認する（設計 3-77a）。
//
// **これを見ないと、巡回のたびに入札が1件ずつ増える。**
//
// 与える情報: 自分の入札を含む入札2件。
// 成功条件: 自分の入札が見つかること。書いていないアカウントでは見つからないこと。
func TestHasBidBy_自分の入札を見つける(t *testing.T) {
	base := at()
	bids := []handoff.Bid{
		{Author: otherLogin, Score: 100, PostedAt: base},
		{Author: selfLogin, Score: 190, PostedAt: base.Add(time.Minute)},
	}

	if _, ok := handoff.HasBidBy(bids, selfLogin); !ok {
		t.Error("自分の入札を見つけられていない")
	}
	if _, ok := handoff.HasBidBy(bids, "octocat-bot-z"); ok {
		t.Error("書いていないアカウントの入札を見つけている")
	}
}

// 目的: 入札のコメントへ人間が読む行を足しても、JSON が壊れないことを確認する（設計 3-77a）。
//
// **足す文に `}` を入れてはならない。**payloadAfterMarker は最初の `{` と
// **最後の `}`** の間を切り出すので、あとに `}` が現れると JSON がそこまで伸びて壊れる。
// **壊れた入札は数に入らない**（ParseBid が偽を返す）ので、
// **その機械は入札しているつもりで、一度も勝てなくなる。**
//
// **言語を切り替えて両方見る。**文言は資源から引くので、日本語で守れていても
// 英語の訳に `}` が入れば同じことが起きる。
//
// 与える情報: 入札1件と、締め切りまでの長さ3分。
// 成功条件: どちらの言語でも `}` が JSON の分の1つだけで、読み戻すと元の値が全部取れること。
func TestFormatBid_人間が読む行を足してもJSONが壊れない(t *testing.T) {
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })

	bid := handoff.Bid{Author: selfLogin, FiveHour: 87, Weekly: 16, Score: 190, At: at()}

	for _, lang := range []i18n.Lang{i18n.LangJA, i18n.LangEN} {
		i18n.Use(lang)

		body := handoff.FormatBid(bid, 3*time.Minute)
		if got := strings.Count(body, "}"); got != 1 {
			t.Errorf("[%s] 人間が読む行に `}` が入っている（JSON がそこまで伸びる）: %d 個\n%s", lang, got, body)
		}

		got, ok := handoff.ParseBid(body, selfLogin, at())
		if !ok {
			t.Fatalf("[%s] 人間が読む行を足した入札を読み戻せない:\n%s", lang, body)
		}
		if got.Author != bid.Author || got.FiveHour != bid.FiveHour ||
			got.Weekly != bid.Weekly || got.Score != bid.Score {
			t.Errorf("[%s] 読み戻した入札が違う: got %+v, want %+v", lang, got, bid)
		}
		if !got.At.Equal(bid.At) {
			t.Errorf("[%s] 読み戻した入札の時刻が違う: got %s, want %s", lang, got.At, bid.At)
		}
	}
}

// 目的: 入札のコメントが、人間に「いま何が起きていて、いつ決まるか」を伝えることを確認する（設計 3-77a）。
//
// **1台で動かしていても、この入札のコメントは必ず出る。**JSON だけだと、
// issue を開いた人には `five_hour` が何の値なのかも、次に何が起きるのかも読めない。
//
// 与える情報: 入札1件と、締め切りまでの長さ3分。
// 成功条件: 立候補していることと、約3分後に自動で決まることが本文に出ていること。
func TestFormatBid_立候補と締め切りが人間に読める(t *testing.T) {
	i18n.Use(i18n.SourceLang)
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })

	body := handoff.FormatBid(handoff.Bid{Author: selfLogin, Score: 190, At: at()}, 3*time.Minute)

	if !strings.Contains(body, selfLogin+" がこの issue の担当に立候補しています") {
		t.Errorf("立候補していることが人間に読める形で書かれていない:\n%s", body)
	}
	if !strings.Contains(body, "約3分後") {
		t.Errorf("担当がいつ決まるかが書かれていない:\n%s", body)
	}
}

// 目的: 締め切りまでの長さが分に収まらないときの書き方を固定する（設計 3-77a）。
//
// **切り捨てにすると、30秒の設定が「約0分後」になる。**待てばよい長さが読めなくなるので、
// 1分未満は「約1分後」へ寄せる。**0 以下は「締め切りを待たない」と書く**
// （`bid_window_ms` に 0 を書ける。設計 3-77）。
//
// 与える情報: 締め切りまでの長さが30秒・0・マイナスの3通り。
// 成功条件: 30秒は「約1分後」、0 とマイナスは締め切りを待たない文になること。
func TestFormatBid_締め切りの書き方(t *testing.T) {
	i18n.Use(i18n.SourceLang)
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })

	bid := handoff.Bid{Author: selfLogin, Score: 190, At: at()}

	if got := handoff.FormatBid(bid, 30*time.Second); !strings.Contains(got, "約1分後") {
		t.Errorf("1分未満の締め切りが「約1分後」になっていない:\n%s", got)
	}
	for _, window := range []time.Duration{0, -time.Minute} {
		got := handoff.FormatBid(bid, window)
		if !strings.Contains(got, "締め切りを待たずに") {
			t.Errorf("締め切り %s のとき、待たずに決まることが書かれていない:\n%s", window, got)
		}
		if strings.Contains(got, "約") {
			t.Errorf("締め切り %s のとき、待ち時間を書いてしまっている:\n%s", window, got)
		}
	}
}

// 目的: 締め切りが1分のとき、英語の文言の文法が崩れないことを確認する（設計 3-77a）。
//
// **英語は DefaultLang である。**`language:` を書いていない利用者にはこれが出るので、
// **日本語だけを見ていると、崩れたまま気づけない。**分数を差し込む文言に 1 を渡すと
// "in about 1 minutes" になるため、1分のときだけ別の文言を引く。
//
// 与える情報: 締め切りまでの長さが30秒・1分・2分の3通り。英語で組み立てる。
// 成功条件: 30秒と1分は "in about a minute"、2分は "in about 2 minutes" になり、
// **どの場合も "1 minutes" が出ないこと。**
func TestFormatBid_英語でも1分の締め切りが崩れない(t *testing.T) {
	i18n.Use(i18n.LangEN)
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })

	bid := handoff.Bid{Author: selfLogin, Score: 190, At: at()}

	for _, window := range []time.Duration{30 * time.Second, time.Minute} {
		got := handoff.FormatBid(bid, window)
		if !strings.Contains(got, "in about a minute") {
			t.Errorf("締め切り %s の英語が \"in about a minute\" になっていない:\n%s", window, got)
		}
		if strings.Contains(got, "1 minutes") {
			t.Errorf("締め切り %s の英語に \"1 minutes\" が出ている:\n%s", window, got)
		}
	}

	if got := handoff.FormatBid(bid, 2*time.Minute); !strings.Contains(got, "in about 2 minutes") {
		t.Errorf("2分の締め切りが分数の文言になっていない:\n%s", got)
	}
}

// 目的: hold のコメントへ人間が読む行を足しても、JSON が壊れないことを確認する（設計 3-77b）。
//
// **壊れると担当の判定そのものが落ちる。**hold は「その担当者は機械である」の唯一の証拠なので、
// 読めなくなると、**別の機械がこの issue を「人間が付けた担当」と読んで触らなくなる**か、
// 逆に担当を奪う側へ倒れる。
//
// 与える情報: hold 1件。
// 成功条件: どちらの言語でも `}` が JSON の分の1つだけで、読み戻すと元の値が全部取れること。
func TestFormatHold_人間が読む行を足してもJSONが壊れない(t *testing.T) {
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })

	hold := handoff.Hold{
		Assignee: selfLogin,
		Branch:   "continuo/octocat/hello-world/188",
		At:       at(),
	}

	for _, lang := range []i18n.Lang{i18n.LangJA, i18n.LangEN} {
		i18n.Use(lang)

		body := handoff.FormatHold(hold)
		if got := strings.Count(body, "}"); got != 1 {
			t.Errorf("[%s] 人間が読む行に `}` が入っている（JSON がそこまで伸びる）: %d 個\n%s", lang, got, body)
		}

		got, ok := handoff.ParseHold(body)
		if !ok {
			t.Fatalf("[%s] 人間が読む行を足した hold を読み戻せない:\n%s", lang, body)
		}
		if got.Assignee != hold.Assignee || got.Branch != hold.Branch {
			t.Errorf("[%s] 読み戻した hold が違う: got %+v, want %+v", lang, got, hold)
		}
		if !got.At.Equal(hold.At) {
			t.Errorf("[%s] 読み戻した hold の時刻が違う: got %s, want %s", lang, got.At, hold.At)
		}
	}
}

// 目的: hold のコメントが、担当の決まり方と次に始まることを人間に伝えることを確認する（設計 3-77b）。
//
// 与える情報: hold 1件（branch の名前あり）。
// 成功条件: どのアカウントが担当になったか・なぜそのアカウントか・どの branch で始まるかが
// 本文に出ていること。
func TestFormatHold_担当の決まり方が人間に読める(t *testing.T) {
	i18n.Use(i18n.SourceLang)
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })

	body := handoff.FormatHold(handoff.Hold{
		Assignee: selfLogin,
		Branch:   "continuo/octocat/hello-world/188",
		At:       at(),
	})

	if !strings.Contains(body, "担当は "+selfLogin+" に決まりました") {
		t.Errorf("どのアカウントが担当になったかが人間に読める形で書かれていない:\n%s", body)
	}
	if !strings.Contains(body, "余裕がいちばん大きい") {
		t.Errorf("なぜそのアカウントが選ばれたのかが書かれていない:\n%s", body)
	}
	if !strings.Contains(body, "branch continuo/octocat/hello-world/188 で作業を始めます") {
		t.Errorf("これから何が始まるかが書かれていない:\n%s", body)
	}
}

// 目的: branch の名前を組み立てられなかったときに、空白の穴が開かないことを確認する（設計 3-77b）。
//
// **呼び出し側は branch 名を組み立てられないと空文字を渡してくる**（`branchNameFor`）。
// そのまま差し込むと「これから branch  で作業を始めます」と出る。
//
// 与える情報: branch の名前が空の hold 1件。
// 成功条件: branch の名前を出さない文へ落ちること。JSON は壊れないこと。
func TestFormatHold_branchの名前が無いとき(t *testing.T) {
	i18n.Use(i18n.SourceLang)
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })

	body := handoff.FormatHold(handoff.Hold{Assignee: selfLogin, At: at()})

	if strings.Contains(body, "branch  で") {
		t.Errorf("branch の名前が空のまま差し込まれている:\n%s", body)
	}
	if !strings.Contains(body, "これから作業を始めます") {
		t.Errorf("branch の名前を出さない文へ落ちていない:\n%s", body)
	}
	if _, ok := handoff.ParseHold(body); !ok {
		t.Errorf("branch の名前が無い hold を読み戻せない:\n%s", body)
	}
}
