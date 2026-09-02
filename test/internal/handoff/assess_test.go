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
// author: 書き手のログイン名。
// host: hold に書く機械の名前。
// created: 作成時刻。
// 戻り値: 判定へ渡す形のコメント。
func holdComment(author, host string, created time.Time) handoff.CommentView {
	return handoff.CommentView{
		Author: author,
		Body: handoff.FormatHold(handoff.Hold{
			Host: host, Assignee: author, Branch: "continuo/octocat/hello-world/188", At: created,
		}),
		CreatedAt: created,
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
				Comments:    []handoff.CommentView{holdComment(otherLogin, "thinkpad", fresh)},
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
				Comments:    []handoff.CommentView{holdComment(otherLogin, "thinkpad", stale)},
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

// 目的: 期限を「担当者の最後のコメント」から数えることを確認する（設計 3-77b）。
//
// **hold を書いた時刻から数えてはならない。**進捗を書き続けている機械が
// 18時間で担当を外されることになる。
//
// 与える情報: hold は19時間前だが、担当者の進捗のコメントは1時間前にある状況。
// 成功条件: 担当を外さない（`期限内の担当`）こと。
func TestAssess_進捗を書き続けている機械の担当は外さない(t *testing.T) {
	now := at()
	got := handoff.Assess(handoff.Situation{
		Assignees: []string{otherLogin},
		Comments: []handoff.CommentView{
			holdComment(otherLogin, "thinkpad", now.Add(-19*time.Hour)),
			{Author: otherLogin, Body: "まだ動いています", CreatedAt: now.Add(-time.Hour)},
		},
		SelfLogin:   selfLogin,
		Now:         now,
		IdleTimeout: idleTimeout,
	})

	if got.Action != handoff.ActionSkipHeld {
		t.Fatalf("進捗を書いている機械の担当を外そうとしている: got %v, want %v",
			got.Action, handoff.ActionSkipHeld)
	}
}

// 目的: 担当を外すときに読む hold が、いちばん新しいものであることを確認する（設計 3-77c）。
//
// **古い hold を読むと、既に居ない機械の名前を released のコメントへ書くことになる。**
//
// 与える情報: hold が2件（古いほうの機械の名前が `old-host`、新しいほうが `thinkpad`）ある状況。
// 成功条件: 新しいほうの機械の名前が返ること。
func TestAssess_担当を外すとき新しいholdの機械の名前を返す(t *testing.T) {
	now := at()
	got := handoff.Assess(handoff.Situation{
		Assignees: []string{otherLogin},
		Comments: []handoff.CommentView{
			holdComment(otherLogin, "old-host", now.Add(-100*time.Hour)),
			holdComment(otherLogin, "thinkpad", now.Add(-50*time.Hour)),
		},
		SelfLogin:   selfLogin,
		Now:         now,
		IdleTimeout: idleTimeout,
	})

	if got.Action != handoff.ActionRelease {
		t.Fatalf("期限切れなのに担当を外さない: got %v", got.Action)
	}
	if got.Hold.Host != "thinkpad" {
		t.Errorf("いちばん新しい hold を読んでいない: got %q, want %q", got.Hold.Host, "thinkpad")
	}
}

// 目的: 入札のコメントの形が設計 3-77a のとおりであることと、書いたものを読み戻せることを確認する。
//
// 与える情報: 入札1件。
// 成功条件: 印で始まり、JSON のキーが `host` / `five_hour` / `weekly` / `score` / `at` であり、
// 時刻がその機械のタイムゾーンのままであること。読み戻すと同じ値になること。
func TestFormatBid_コメントの形と読み戻し(t *testing.T) {
	bid := handoff.Bid{Host: "mac-studio", FiveHour: 87, Weekly: 16, Score: 190, At: at()}
	body := handoff.FormatBid(bid, 3*time.Minute)

	if !strings.HasPrefix(body, config.HandoffBidMarker+"\n") {
		t.Fatalf("入札の印で始まっていない:\n%s", body)
	}
	for _, key := range []string{`"host":"mac-studio"`, `"five_hour":87`, `"weekly":16`, `"score":190`} {
		if !strings.Contains(body, key) {
			t.Errorf("入札の JSON に %s がない:\n%s", key, body)
		}
	}
	if !strings.Contains(body, "+09:00") {
		t.Errorf("時刻がその機械のタイムゾーンで書かれていない（Z に直してはならない）:\n%s", body)
	}

	posted := at().Add(time.Second)
	got, ok := handoff.ParseBid(body, posted)
	if !ok {
		t.Fatalf("書いた入札を読み戻せない:\n%s", body)
	}
	if got.Host != bid.Host || got.Score != bid.Score || got.FiveHour != bid.FiveHour || got.Weekly != bid.Weekly {
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
// 与える情報: 印はあるが JSON が壊れているコメントと、機械の名前が空のコメント。
// 成功条件: どちらも入札として読めないこと。
func TestParseBid_読めない入札は数えない(t *testing.T) {
	broken := config.HandoffBidMarker + "\nこれは JSON ではありません {"
	if _, ok := handoff.ParseBid(broken, at()); ok {
		t.Error("JSON でないコメントを入札として読んでいる")
	}

	noHost := config.HandoffBidMarker + "\n" + `{"five_hour":100,"weekly":100,"score":300}`
	if _, ok := handoff.ParseBid(noHost, at()); ok {
		t.Error("機械の名前の無いコメントを入札として読んでいる")
	}
}

// 目的: released のコメントに、引き継ぐ機械の名前を書かないことを確認する（設計 3-77c）。
//
// **外すのは入札をやり直す前なので、そのとき勝つ機械はまだ決まっていない。**
// 書くと、外した機械が負けたときに嘘になる。
//
// 与える情報: released 1件。
// 成功条件: 印で始まり、`from` と `branch` と `at` だけを持ち、`to` を持たないこと。
// 本文に「push しないでください」にあたる文言が入っていること。
func TestFormatReleased_引き継ぐ機械の名前は書かない(t *testing.T) {
	i18n.Use(i18n.SourceLang)
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })

	body := handoff.FormatReleased(handoff.Released{
		From: "mac-studio", Branch: "continuo/octocat/hello-world/188", At: at(),
	})

	if !strings.HasPrefix(body, config.HandoffReleasedMarker+"\n") {
		t.Fatalf("released の印で始まっていない:\n%s", body)
	}
	if !strings.Contains(body, `"from":"mac-studio"`) {
		t.Errorf("外した機械の名前が入っていない:\n%s", body)
	}
	if strings.Contains(body, `"to":`) {
		t.Errorf("引き継ぐ機械の名前を書いている（この段では決まっていない）:\n%s", body)
	}
	if !strings.Contains(body, "mac-studio で走っていた作業") {
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

// 目的: 同じ機械が入札を2件書かないための判定が効くことを確認する（設計 3-77a）。
//
// **これを見ないと、巡回のたびに入札が1件ずつ増える。**
//
// 与える情報: 自分の入札を含む入札3件。
// 成功条件: 自分の入札が見つかること。含まれない機械では見つからないこと。
func TestHasBidBy_自分の入札を見つける(t *testing.T) {
	base := at()
	bids := []handoff.Bid{
		{Host: "thinkpad", Score: 100, PostedAt: base},
		{Host: testHost, Score: 190, PostedAt: base.Add(time.Minute)},
	}

	if _, ok := handoff.HasBidBy(bids, testHost); !ok {
		t.Error("自分の入札を見つけられていない")
	}
	if _, ok := handoff.HasBidBy(bids, "mac-studio"); ok {
		t.Error("書いていない機械の入札を見つけている")
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
	bid := handoff.Bid{Host: "mac-studio", FiveHour: 87, Weekly: 16, Score: 190, At: at()}

	for _, lang := range []i18n.Lang{i18n.LangJA, i18n.LangEN} {
		i18n.Use(lang)

		body := handoff.FormatBid(bid, 3*time.Minute)
		if got := strings.Count(body, "}"); got != 1 {
			t.Errorf("[%s] 人間が読む行に `}` が入っている（JSON がそこまで伸びる）: %d 個\n%s", lang, got, body)
		}

		got, ok := handoff.ParseBid(body, at())
		if !ok {
			t.Fatalf("[%s] 人間が読む行を足した入札を読み戻せない:\n%s", lang, body)
		}
		if got.Host != bid.Host || got.FiveHour != bid.FiveHour ||
			got.Weekly != bid.Weekly || got.Score != bid.Score {
			t.Errorf("[%s] 読み戻した入札が違う: got %+v, want %+v", lang, got, bid)
		}
		if !got.At.Equal(bid.At) {
			t.Errorf("[%s] 読み戻した入札の時刻が違う: got %s, want %s", lang, got.At, bid.At)
		}
	}
	i18n.Use(i18n.DefaultLang)
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

	body := handoff.FormatBid(handoff.Bid{Host: "mac-studio", Score: 190, At: at()}, 3*time.Minute)

	if !strings.Contains(body, "mac-studio がこの issue の担当に立候補しています") {
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

	bid := handoff.Bid{Host: "mac-studio", Score: 190, At: at()}

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

// 目的: hold のコメントへ人間が読む行を足しても、JSON が壊れないことを確認する（設計 3-77b）。
//
// **壊れると担当の判定そのものが落ちる。**hold は「その担当者は機械である」の唯一の証拠なので、
// 読めなくなると、**別の機械がこの issue を「人間が付けた担当」と読んで触らなくなる**か、
// 逆に担当を奪う側へ倒れる。
//
// 与える情報: hold 1件。
// 成功条件: どちらの言語でも `}` が JSON の分の1つだけで、読み戻すと元の値が全部取れること。
func TestFormatHold_人間が読む行を足してもJSONが壊れない(t *testing.T) {
	hold := handoff.Hold{
		Host:     "mac-studio",
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
		if got.Host != hold.Host || got.Assignee != hold.Assignee || got.Branch != hold.Branch {
			t.Errorf("[%s] 読み戻した hold が違う: got %+v, want %+v", lang, got, hold)
		}
		if !got.At.Equal(hold.At) {
			t.Errorf("[%s] 読み戻した hold の時刻が違う: got %s, want %s", lang, got.At, hold.At)
		}
	}
	i18n.Use(i18n.DefaultLang)
}

// 目的: hold のコメントが、担当の決まり方と次に始まることを人間に伝えることを確認する（設計 3-77b）。
//
// 与える情報: hold 1件（branch の名前あり）。
// 成功条件: どの機械が担当になったか・なぜその機械か・どの branch で始まるかが本文に出ていること。
func TestFormatHold_担当の決まり方が人間に読める(t *testing.T) {
	i18n.Use(i18n.SourceLang)
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })

	body := handoff.FormatHold(handoff.Hold{
		Host:     "mac-studio",
		Assignee: selfLogin,
		Branch:   "continuo/octocat/hello-world/188",
		At:       at(),
	})

	if !strings.Contains(body, "担当は mac-studio に決まりました") {
		t.Errorf("どの機械が担当になったかが人間に読める形で書かれていない:\n%s", body)
	}
	if !strings.Contains(body, "余裕がいちばん大きい") {
		t.Errorf("なぜその機械が選ばれたのかが書かれていない:\n%s", body)
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

	body := handoff.FormatHold(handoff.Hold{Host: "mac-studio", Assignee: selfLogin, At: at()})

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
