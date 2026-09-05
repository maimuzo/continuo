// Package handoff_test は、複数の機械で担当を持ち回るときの判定を確かめる
// （設計 3-77 / 3-77a / 3-77b / 3-77c）。
//
// **確かめたいことは4つある。**
//
//	余裕値と判定スコアの式が設計のとおりであること
//	投稿しない3つの条件が、そのとおりに黙ること
//	同点の決着が「いちばん最初に投稿した機械」であること
//	担当を外す判定が、担当者の最後のコメントからの経過で決まること
package handoff_test

import (
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// testAccount はテストで使う「この continuo が使う gh の持ち主」である（架空の名前）。
const testAccount = "octocat-bot-c"

// at はテストで使う固定の時刻を返す。
//
// **その機械のタイムゾーンで書くことを確かめたいので、UTC にしない**（設計 3-77a）。
//
// 戻り値: 固定の時刻（+09:00）。
func at() time.Time {
	return time.Date(2026, 8, 29, 16, 45, 0, 0, time.FixedZone("JST", 9*60*60))
}

// snapshot は枠の写しを組み立てる。
//
// limits: `kind` と `percent` の組。
// 戻り値: 組み立てた写し。
func snapshot(limits ...[2]any) *ratelimit.Snapshot {
	out := &ratelimit.Snapshot{}
	for _, l := range limits {
		out.Limits = append(out.Limits, ratelimit.Limit{
			Kind:    l[0].(string),
			Percent: l[1].(int),
		})
	}
	return out
}

// 目的: 余裕値と判定スコアが設計 3-77 の式のとおりに出ることを確認する。
//
//	5時間余裕値 = 100 − 5時間の使用率 − 5時間マージン
//	1週間余裕値 = 100 − 1週間の使用率（いちばん大きいもの）− 1週間マージン
//	判定スコア  = 5時間余裕値 × 2 + 1週間余裕値
//
// 与える情報: 5時間の枠が 3%、1週間全体の枠が 74%、モデル別の枠が 80% の写しと、
// マージンが両方 10%。
// 成功条件: 5時間余裕値が 87、1週間余裕値が 10、判定スコアが 184 になること
// （**1週間はモデル別の 80% を採る。74% ではない**）。
func TestEvaluate_余裕値と判定スコアが式のとおりに出る(t *testing.T) {
	snap := snapshot(
		[2]any{handoff.LimitKindSession, 3},
		[2]any{handoff.LimitKindWeeklyAll, 74},
		[2]any{handoff.LimitKindWeeklyScoped, 80},
	)

	bid, skip := handoff.Evaluate(snap, true, handoff.Margins{FiveHour: 10, Weekly: 10}, 95, at())

	if skip != handoff.SkipNone {
		t.Fatalf("入札してよいはずなのに黙った: %v", skip)
	}
	if bid.FiveHour != 87 {
		t.Errorf("5時間余裕値が式と違う: got %d, want 87（100 − 3 − 10）", bid.FiveHour)
	}
	if bid.Weekly != 10 {
		t.Errorf("1週間余裕値が式と違う: got %d, want 10（100 − 80 − 10。モデル別の枠のほうが大きい）", bid.Weekly)
	}
	if bid.Score != 184 {
		t.Errorf("判定スコアが式と違う: got %d, want 184（87 × 2 + 10）", bid.Score)
	}
	// **`Evaluate` は識別子を入れない**（設計 3-77-0）。枠の判定は gh の持ち主を引くより
	// 先に走るので、この時点では持ち主が分かっていない。**埋めるのは `bidForIssue` である。**
	if bid.Author != "" {
		t.Errorf("枠の判定が識別子を入れている（持ち主はまだ分かっていない）: got %q", bid.Author)
	}
	if _, offset := bid.At.Zone(); offset != 9*60*60 {
		t.Errorf("時刻がその機械のタイムゾーンで入っていない（Z に直してはならない）: %s", bid.At.Format(time.RFC3339))
	}
}

// 目的: 1週間の使用率が「1週間全体の枠とモデル別の枠のいちばん大きいもの」であることを確認する。
//
// **モデル別の枠は一定量を使うまで現れない。**現れないものは判定に入らない。
//
// 与える情報: 1週間全体の枠だけがある写しと、モデル別の枠のほうが大きい写し。
// 成功条件: どちらもいちばん大きい値を返すこと。
func TestWeeklyPercent_いちばん大きい1週間の枠を採る(t *testing.T) {
	only := snapshot([2]any{handoff.LimitKindWeeklyAll, 16})
	if got, ok := handoff.WeeklyPercent(only); !ok || got != 16 {
		t.Errorf("モデル別の枠が無いときに1週間全体の枠を採っていない: got %d ok=%v, want 16 true", got, ok)
	}

	both := snapshot(
		[2]any{handoff.LimitKindWeeklyAll, 16},
		[2]any{handoff.LimitKindWeeklyScoped, 44},
	)
	if got, ok := handoff.WeeklyPercent(both); !ok || got != 44 {
		t.Errorf("いちばん大きい1週間の枠を採っていない: got %d ok=%v, want 44 true", got, ok)
	}
}

// 目的: 投稿しない3つの条件が、そのとおりに黙ることを確認する（設計 3-77 の表）。
//
// **黙る理由を取り違えてはならない。**枠を読めないのに「余裕値がマイナス」と記録すると、
// 人間は資格情報ではなく使用率を疑うことになる。
//
// 与える情報: 3つの条件それぞれを満たす写しと設定。
// 成功条件: それぞれの理由で黙ること。
func TestEvaluate_投稿しない3つの条件(t *testing.T) {
	cases := []struct {
		name              string
		snap              *ratelimit.Snapshot
		margins           handoff.Margins
		pauseAbovePercent int
		want              handoff.SkipReason
	}{
		{
			name:              "枠を読めない",
			snap:              nil,
			margins:           handoff.Margins{FiveHour: 10, Weekly: 10},
			pauseAbovePercent: 95,
			want:              handoff.SkipQuotaUnreadable,
		},
		{
			name:              "枠が1件も返らない",
			snap:              snapshot(),
			margins:           handoff.Margins{FiveHour: 10, Weekly: 10},
			pauseAbovePercent: 95,
			want:              handoff.SkipQuotaUnreadable,
		},
		{
			name: "枠の使い過ぎ",
			snap: snapshot(
				[2]any{handoff.LimitKindSession, 96},
				[2]any{handoff.LimitKindWeeklyAll, 10},
			),
			margins:           handoff.Margins{},
			pauseAbovePercent: 95,
			want:              handoff.SkipPauseThreshold,
		},
		{
			name: "余裕値がマイナス",
			snap: snapshot(
				[2]any{handoff.LimitKindSession, 92},
				[2]any{handoff.LimitKindWeeklyAll, 10},
			),
			margins:           handoff.Margins{FiveHour: 10, Weekly: 10},
			pauseAbovePercent: 95,
			want:              handoff.SkipNoHeadroom,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, skip := handoff.Evaluate(c.snap, true, c.margins, c.pauseAbovePercent, at())
			if skip != c.want {
				t.Errorf("黙る理由が違う: got %v, want %v", skip, c.want)
			}
		})
	}
}

// 目的: 判定スコアがいちばん大きい機械が勝つことを確認する（設計 3-77）。
//
// 与える情報: 判定スコアの違う入札3件。
// 成功条件: いちばん大きい入札が返ること。
func TestWinner_判定スコアがいちばん大きい機械が勝つ(t *testing.T) {
	base := at()
	bids := []handoff.Bid{
		{Author: "a", Score: 120, PostedAt: base},
		{Author: "b", Score: 190, PostedAt: base.Add(time.Minute)},
		{Author: "c", Score: 150, PostedAt: base.Add(2 * time.Minute)},
	}

	winner, ok := handoff.Winner(bids)
	if !ok {
		t.Fatal("入札があるのに勝者が決まらなかった")
	}
	if winner.Author != "b" {
		t.Errorf("判定スコアがいちばん大きいアカウントが勝っていない: got %q, want %q", winner.Author, "b")
	}
}

// 目的: 同点なら、いちばん最初に投稿した機械が勝つことを確認する（設計 3-77）。
//
// **比べるのは GitHub が付けた投稿の時刻である。**入札の JSON に書かれた `at` は
// 投稿者が自分で書いた値なので、**時計を戻せば必ず勝ててしまう。**
//
// 与える情報: 判定スコアが同じ入札3件（`at` は逆順に細工してある）。
// 成功条件: いちばん先に投稿された機械が勝つこと。
func TestWinner_同点なら最初に投稿した機械が勝つ(t *testing.T) {
	base := at()
	bids := []handoff.Bid{
		// **`at` は未来を指しているが、投稿は2番目である。**
		{Author: "late", Score: 190, At: base.Add(-time.Hour), PostedAt: base.Add(time.Minute)},
		{Author: "first", Score: 190, At: base.Add(time.Hour), PostedAt: base},
		{Author: "last", Score: 190, At: base, PostedAt: base.Add(2 * time.Minute)},
	}

	winner, ok := handoff.Winner(bids)
	if !ok {
		t.Fatal("入札があるのに勝者が決まらなかった")
	}
	if winner.Author != "first" {
		t.Errorf("同点の決着が投稿の時刻になっていない: got %q, want %q", winner.Author, "first")
	}
}

// 目的: 投稿の時刻まで同じでも、機械ごとに違う勝者を選ばないことを確認する。
//
// **決め手を最後まで用意しないと、2台が同じ issue を掴む。**
//
// 与える情報: 判定スコアも投稿の時刻も同じ入札2件を、並び順を変えて2回。
// 成功条件: どちらの並びでも同じアカウントが勝つこと。
func TestWinner_全部同じでも同じ勝者を選ぶ(t *testing.T) {
	base := at()
	a := handoff.Bid{Author: "alpha", Score: 190, PostedAt: base}
	b := handoff.Bid{Author: "beta", Score: 190, PostedAt: base}

	first, _ := handoff.Winner([]handoff.Bid{a, b})
	second, _ := handoff.Winner([]handoff.Bid{b, a})
	if first.Author != second.Author {
		t.Fatalf("並び順で勝者が変わった: %q と %q", first.Author, second.Author)
	}
}

// 目的: まだ使っていない枠が応答に現れなくても、「読めなかった」として黙らないことを確認する
// （設計 3-77）。
//
// **モデル別の枠は一定量を使うまで現れない。**現れないことを「読めなかった」と扱うと、
// **週の頭にはどの機械も入札できなくなる。**
//
// 与える情報: 5時間の枠だけが載っている写し。
// 成功条件: 入札してよいと答え、1週間余裕値が「まだ使っていない」ぶんになること。
func TestEvaluate_使っていない枠が現れなくても黙らない(t *testing.T) {
	snap := snapshot([2]any{handoff.LimitKindSession, 3})

	bid, skip := handoff.Evaluate(snap, true, handoff.Margins{FiveHour: 10, Weekly: 10}, 95, at())

	if skip != handoff.SkipNone {
		t.Fatalf("使っていない枠が無いだけで黙った: %v", skip)
	}
	if bid.Weekly != 90 {
		t.Errorf("1週間余裕値が使用率0として計算されていない: got %d, want 90（100 − 0 − 10）", bid.Weekly)
	}
}

// 目的: 締め切りが「入札が1件も無い issue への最初の投稿」から数えられることを確認する
// （設計 3-77）。
//
// 与える情報: 3分ずれた入札2件と、締め切りまでの長さ3分。
// 成功条件: 締め切りが「いちばん古い入札の投稿時刻 + 3分」になること。
func TestDeadline_最初の入札から数える(t *testing.T) {
	base := at()
	bids := []handoff.Bid{
		{Author: "late", PostedAt: base.Add(3 * time.Minute)},
		{Author: "first", PostedAt: base},
	}

	got, ok := handoff.Deadline(bids, 3*time.Minute)
	if !ok {
		t.Fatal("入札があるのに締め切りが決まらなかった")
	}
	want := base.Add(3 * time.Minute)
	if !got.Equal(want) {
		t.Errorf("締め切りが最初の入札から数えられていない: got %s, want %s",
			got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// bidComment は入札のコメントを1件組み立てる。
//
// **本文から読み直させる。**Bid の値を直に並べると、印の付け方や JSON の形が
// 変わったことに気づけない。
//
// **投稿者を必ず入れる。**持ち回りで参加者を見分ける値はここである（設計 3-77-0）。
// 入れないと `CollectBids` が1件も返さない。
//
// account: 入札したアカウントのログイン名。
// score: 判定スコア。
// postedAt: GitHub がそのコメントに付けた作成時刻。
// 戻り値: 入札のコメント。
func bidComment(account string, score int, postedAt time.Time) handoff.CommentView {
	return handoff.CommentView{
		Author:    account,
		Body:      handoff.FormatBid(handoff.Bid{Author: account, Score: score, At: postedAt}, 3*time.Minute),
		CreatedAt: postedAt,
	}
}

// releasedComment は released のコメントを1件組み立てる。
//
// from: 担当を外されたアカウントのログイン名。
// postedAt: 作成時刻。
// 戻り値: released のコメント。
func releasedComment(from string, postedAt time.Time) handoff.CommentView {
	return handoff.CommentView{
		Body:      handoff.FormatReleased(handoff.Released{From: from, At: postedAt}),
		CreatedAt: postedAt,
	}
}

// accountsOf は入札の一覧からアカウントの名前を並べる。
//
// bids: 並べる入札。
// 戻り値: アカウントの名前をカンマでつないだもの（読めない失敗表示にしないため）。
func accountsOf(bids []handoff.Bid) string {
	out := ""
	for i, b := range bids {
		if i > 0 {
			out += ", "
		}
		out += b.Author
	}
	return "[" + out + "]"
}

// 目的: 古い入札と新しい入札が同居していても、次の回が始まることを確認する（設計 3-77e）。
//
// **これがいちばん起きやすい形である。**入札は1回ごとに新しいコメントを書くので、
// **古い入札は必ず残る。**残った古い入札から締め切りを数えると、締め切りは常に過ぎたことになり、
// **どの巡回でも「終わった回」と判定されて入札が1件も返らない。**呼び出し側は
// 毎回そこへ新しい入札を書き足すので、**コメントだけが増えて担当者が永久に決まらない。**
//
// 与える情報: 締め切り3分に対して、終わった回の入札1件（base）と、
// そのあとに書かれた入札1件（base+10分）。
// 成功条件: 新しいほうだけが返り、締め切りが「新しい入札 + 3分」から数え直されること。
// **次の巡回でも同じ1件が返る**こと（返らなければ、そこで入札がもう1件増える）。
func TestRoundBids_古い入札が残っていても次の回が始まる(t *testing.T) {
	base := at()
	window := 3 * time.Minute
	comments := []handoff.CommentView{
		bidComment("thinkpad", 300, base),
		bidComment(testAccount, 190, base.Add(10*time.Minute)),
	}

	got := handoff.RoundBids(comments, base.Add(10*time.Minute), window)
	if len(got) != 1 || got[0].Author != testAccount {
		t.Fatalf("いまの回の入札だけが返っていない: %s", accountsOf(got))
	}
	deadline, ok := handoff.Deadline(got, window)
	if !ok || !deadline.Equal(base.Add(13*time.Minute)) {
		t.Errorf("締め切りが新しい入札から数え直されていない: got %s, want %s",
			deadline.Format(time.RFC3339), base.Add(13*time.Minute).Format(time.RFC3339))
	}

	// **次の巡回。**ここで空が返ると、呼び出し側は入札をもう1件書いてしまう。
	next := handoff.RoundBids(comments, base.Add(10*time.Minute+30*time.Second), window)
	if len(next) != 1 || next[0].Author != testAccount {
		t.Errorf("次の巡回でいまの回の入札を落としている: %s", accountsOf(next))
	}
}

// 目的: 終わった回の入札を数え続けないことを確認する（設計 3-77e）。
//
// **勝った機械が担当者を書けないまま落ちると、その機械が永久に勝ち続ける。**
// 担当者がいないので hold の期限も効かず、issue は誰にも着手されないまま止まる。
//
// 与える情報: 3分の締め切りに対して、締め切りからさらに3分を過ぎた入札1件。
// 成功条件: 回が終わっていれば1件も返らず、回の中なら返ること。
func TestRoundBids_終わった回の入札は数えない(t *testing.T) {
	base := at()
	window := 3 * time.Minute
	comments := []handoff.CommentView{bidComment("thinkpad", 300, base)}

	// 締め切り（base+3分）からさらに3分。**回は終わっている。**
	if got := handoff.RoundBids(comments, base.Add(6*time.Minute+time.Second), window); len(got) != 0 {
		t.Errorf("終わった回の入札を数えている: %s", accountsOf(got))
	}
	// 締め切りは過ぎたが、決着の猶予の中である。
	if got := handoff.RoundBids(comments, base.Add(4*time.Minute), window); len(got) != 1 {
		t.Errorf("いまの回の入札を落としている: %s", accountsOf(got))
	}
	// 締め切りを待たない設定に決着の猶予は無い。
	if got := handoff.RoundBids(comments, base.Add(365*24*time.Hour), 0); len(got) != 1 {
		t.Errorf("締め切りを待たない設定で入札を落としている: %s", accountsOf(got))
	}
}

// 目的: 終わった回が2つ以上積まれていても、いまの回に行き着くことを確認する（設計 3-77e）。
//
// **1回ぶんだけ落として切り上げると、2つ前の回の入札が残る。**残ればそこから締め切りを
// 数えることになり、次の回が始まらない。
//
// 与える情報: 締め切り3分に対して、10分ずつ離れた入札3件（base / base+10分 / base+20分）。
// 成功条件: いちばん新しい1件だけが返ること。
func TestRoundBids_終わった回が積まれていても数え直す(t *testing.T) {
	base := at()
	window := 3 * time.Minute
	comments := []handoff.CommentView{
		bidComment("thinkpad", 300, base),
		bidComment("mac-studio", 280, base.Add(10*time.Minute)),
		bidComment(testAccount, 190, base.Add(20*time.Minute)),
	}

	got := handoff.RoundBids(comments, base.Add(20*time.Minute), window)
	if len(got) != 1 || got[0].Author != testAccount {
		t.Fatalf("いまの回の入札だけが返っていない: %s", accountsOf(got))
	}
}

// 目的: hold より前の入札を、いまの回に数えないことを確認する（設計 3-77e）。
//
// **hold は「その回に勝者が出た」という記録である。**そこで回は閉じている。
// **締め切りを待たない設定（`bid_window_ms: 0`）でも、この区切りは効く。**
//
// 与える情報: 前の回の入札1件と hold 1件、そのあとに書かれた入札1件。
// 成功条件: hold より後の1件だけが返ること（締め切りの有無によらず）。
func TestRoundBids_holdより前の入札は前の回のもの(t *testing.T) {
	base := at()
	comments := []handoff.CommentView{
		bidComment("thinkpad", 300, base),
		holdComment(otherLogin, base.Add(3*time.Minute)),
		bidComment(testAccount, 190, base.Add(19*time.Hour)),
	}
	now := base.Add(19 * time.Hour)

	for _, window := range []time.Duration{3 * time.Minute, 0} {
		got := handoff.RoundBids(comments, now, window)
		if len(got) != 1 || got[0].Author != testAccount {
			t.Errorf("hold より前の入札を数えている（締め切り %s）: %s", window, accountsOf(got))
		}
	}
}

// 目的: released と同じ時刻に書かれた入札を落とさないことを確認する（設計 3-77e）。
//
// **GitHub がコメントに付ける時刻は秒どまりである。**担当を外した機械は、その場で
// released と入札を続けて書くので、**2件は同じ秒に入る。**そこで入札を落とすと、
// **その機械は書くそばから自分の入札を捨て、巡回のたびにコメントが1件ずつ増える。**
//
// 与える情報: 前の回の入札1件（19時間前）と、同じ時刻に並んだ released と入札。
// 成功条件: 同じ時刻の入札が残り、前の回の入札は落ちること。
func TestRoundBids_releasedと同じ時刻の入札は残す(t *testing.T) {
	base := at()
	now := base.Add(19 * time.Hour)
	comments := []handoff.CommentView{
		bidComment("thinkpad", 300, base),
		holdComment(otherLogin, base.Add(3*time.Minute)),
		releasedComment("thinkpad", now),
		bidComment(testAccount, 190, now),
	}

	got := handoff.RoundBids(comments, now, 3*time.Minute)
	if len(got) != 1 || got[0].Author != testAccount {
		t.Fatalf("released と同じ時刻の入札を落としている: %s", accountsOf(got))
	}
}

// 目的: いまの回を閉じたコメントの時刻を読めることを確認する（設計 3-77e）。
//
// 与える情報: hold（3分後）と released（19時間後）が1件ずつあるコメントの列。
// 成功条件: いちばん新しい released の作成時刻が返ること。
func TestRoundStart_いちばん新しいholdかreleasedを採る(t *testing.T) {
	base := at()
	comments := []handoff.CommentView{
		bidComment("thinkpad", 300, base),
		holdComment(otherLogin, base.Add(3*time.Minute)),
		releasedComment("thinkpad", base.Add(19*time.Hour)),
	}

	got, ok := handoff.RoundStart(comments)
	if !ok {
		t.Fatal("hold も released もあるのに回の区切りが返らなかった")
	}
	if !got.Equal(base.Add(19 * time.Hour)) {
		t.Errorf("いちばん新しい区切りを採っていない: got %s, want %s",
			got.Format(time.RFC3339), base.Add(19*time.Hour).Format(time.RFC3339))
	}

	if _, ok := handoff.RoundStart([]handoff.CommentView{bidComment(testAccount, 190, base)}); ok {
		t.Error("入札だけの issue で回の区切りが返った（入札は回を閉じない）")
	}
}

// 目的: 締め切りを過ぎて届いた入札を、勝敗の判定に入れないことを確認する（設計 3-77）。
//
// 与える情報: 締め切りちょうどの入札と、締め切りの1秒後の入札。
// 成功条件: 締め切りちょうどは残り、1秒後は落ちること。
func TestBidsBefore_締め切りを過ぎた入札を落とす(t *testing.T) {
	base := at()
	deadline := base.Add(3 * time.Minute)
	bids := []handoff.Bid{
		{Author: "onTime", PostedAt: deadline},
		{Author: "late", PostedAt: deadline.Add(time.Second)},
	}

	got := handoff.BidsBefore(bids, deadline)
	if len(got) != 1 || got[0].Author != "onTime" {
		t.Fatalf("締め切りの扱いが違う: got %+v, want onTime だけ", got)
	}
}
