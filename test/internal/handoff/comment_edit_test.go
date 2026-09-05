package handoff_test

import (
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/handoff"
)

// 目的: 進捗の報告を書き足した（コメントを編集した）だけでも、持ち回りの期限が延びることを固定する
// （#194（進捗コメントの間隔と重ね方が、人間の決定と逆に実装されている）。設計 5-3j / 5-3k）。
//
// **なぜ要るか。**エージェントは進捗の報告を新しいコメントにせず、
// **いちばん下にある自分の進捗報告へ書き足す**（設計 5-3j）。**コメントを増やさないためである。**
// **本文を編集しても GitHub は作成時刻を動かさない**（2026-09-03 に実測）。
// **作成時刻だけで数えると、1時間おきに書き続けている機械が18時間で担当を外され、
// 別の機械が同じ issue を最初からやり直す。**
//
// 与える情報: 担当者のコメントが19時間前に作られ、1時間前に編集されている状況。
// 成功条件: 担当を外さない（`期限内の担当`）こと。
func TestAssess_進捗を書き足した機械の担当は外さない(t *testing.T) {
	now := at()
	got := handoff.Assess(handoff.Situation{
		Assignees: []string{otherLogin},
		Comments: []handoff.CommentView{
			holdComment(otherLogin, now.Add(-20*time.Hour)),
			progressComment(otherLogin, now.Add(-19*time.Hour), now.Add(-time.Hour)),
		},
		SelfLogin:   selfLogin,
		Now:         now,
		IdleTimeout: idleTimeout,
	})

	if got.Action != handoff.ActionSkipHeld {
		t.Fatalf("進捗を書き足している機械の担当を外そうとしている: got %v, want %v",
			got.Action, handoff.ActionSkipHeld)
	}
	if !got.LastProgress.Equal(now.Add(-time.Hour)) {
		t.Errorf("最後の進捗報告として更新時刻を採っていない: got %v, want %v",
			got.LastProgress, now.Add(-time.Hour))
	}
}

// 目的: 更新時刻が取れなかったコメントでも、作成時刻で数えることを固定する（設計 5-3k）。
//
// **なぜ要るか。**`updatedAt` は GraphQL の応答から落ちうる（フィールドを要求していない経路、
// 偽サーバ、`null`）。**そのときゼロ値をそのまま採ると、期限が西暦1年から数えられ、
// 生きて働いている担当がその場で外れる。**
//
// 与える情報: 担当者の進捗報告が1時間前に作られ、更新時刻はゼロ値の状況。
// 成功条件: 担当を外さないこと。
func TestAssess_更新時刻が取れなくても作成時刻で数える(t *testing.T) {
	now := at()
	got := handoff.Assess(handoff.Situation{
		Assignees: []string{otherLogin},
		Comments: []handoff.CommentView{
			holdComment(otherLogin, now.Add(-20*time.Hour)),
			progressComment(otherLogin, now.Add(-time.Hour), time.Time{}),
		},
		SelfLogin:   selfLogin,
		Now:         now,
		IdleTimeout: idleTimeout,
	})

	if got.Action != handoff.ActionSkipHeld {
		t.Fatalf("更新時刻がゼロ値なだけで担当を外そうとしている: got %v, want %v",
			got.Action, handoff.ActionSkipHeld)
	}
}

// 目的: `LastTouched` が、作成時刻と更新時刻の新しいほうを返すことを固定する（設計 5-3k）。
//
// 与える情報: 更新時刻が新しい場合・ゼロ値の場合・作成時刻より古い場合の3つ。
// 成功条件: どれでも新しいほうが返ること。
func TestCommentView_最後に触られた時刻は新しいほうを返す(t *testing.T) {
	base := at()
	for _, c := range []struct {
		name string
		view handoff.CommentView
		want time.Time
	}{
		{
			name: "更新時刻のほうが新しい",
			view: handoff.CommentView{CreatedAt: base.Add(-time.Hour), UpdatedAt: base},
			want: base,
		},
		{
			name: "更新時刻がゼロ値",
			view: handoff.CommentView{CreatedAt: base.Add(-time.Hour)},
			want: base.Add(-time.Hour),
		},
		{
			// **GitHub はこの形を返さない。**返っても作成時刻より前へ倒れないことを固定する。
			name: "更新時刻のほうが古い",
			view: handoff.CommentView{CreatedAt: base, UpdatedAt: base.Add(-time.Hour)},
			want: base,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.view.LastTouched(); !got.Equal(c.want) {
				t.Errorf("最後に触られた時刻が違う: got %v, want %v", got, c.want)
			}
		})
	}
}

// 目的: 入札の回の区切り（`RoundStart`）が、編集で動かないことを固定する（設計 5-3k / 3-77e）。
//
// **なぜ要るか。**区切りは「いちばん新しい hold か released が現れた時刻」である。
// **更新時刻で数えると、古い hold を1文字編集するだけで区切りが未来へ動く。**
// **締め切りが永久に来ず、担当者が1台も決まらなくなる。**
//
// 与える情報: hold が10時間前に作られ、いま編集された状況。
// 成功条件: 区切りが作成時刻（10時間前）のままであること。
func TestRoundStart_編集しても回の区切りは動かない(t *testing.T) {
	now := at()
	created := now.Add(-10 * time.Hour)

	view := holdComment(otherLogin, created)
	view.UpdatedAt = now

	got, ok := handoff.RoundStart([]handoff.CommentView{view})
	if !ok {
		t.Fatal("hold があるのに回の区切りを取れない")
	}
	if !got.Equal(created) {
		t.Errorf("編集で回の区切りが動いた: got %v, want %v（作成時刻）", got, created)
	}
}

// 目的: いちばん新しい hold の選び方が、編集で入れ替わらないことを固定する（設計 5-3k）。
//
// **なぜ要るか。**いちばん新しい hold は「いまどの branch で作業しているか」の答えである。
// **更新時刻で選ぶと、古い hold を1文字直すだけで、担当が始まった時刻と branch の名前が入れ替わる。**
// 担当を外すときに書く released のコメントが、既に使われていない branch を名指しすることになる。
//
// 与える情報: 古い hold（`old-branch`）がいま編集され、新しい hold（`new-branch`）は編集されていない状況。
// 成功条件: 新しく作られたほう（`new-branch`）が返ること。
func TestLatestHoldFor_編集しても新しいholdは入れ替わらない(t *testing.T) {
	now := at()

	old := holdCommentOn(otherLogin, "old-branch", now.Add(-100*time.Hour))
	old.UpdatedAt = now
	recent := holdCommentOn(otherLogin, "new-branch", now.Add(-50*time.Hour))

	got, gotAt, ok := handoff.LatestHoldFor([]handoff.CommentView{old, recent}, otherLogin)
	if !ok {
		t.Fatal("hold があるのに取れない")
	}
	if got.Branch != "new-branch" {
		t.Errorf("編集で hold の新旧が入れ替わった: got %q, want %q", got.Branch, "new-branch")
	}
	// **返す時刻も作成時刻である。**ここが更新時刻になると、期限の下限が編集で未来へ動く。
	if !gotAt.Equal(now.Add(-50 * time.Hour)) {
		t.Errorf("hold の時刻が作成時刻になっていない: got %v, want %v",
			gotAt, now.Add(-50*time.Hour))
	}
}

// 目的: 入札の投稿時刻（`Bid.PostedAt`）が、編集で動かないことを固定する（設計 5-3k / 3-77a）。
//
// **なぜ要るか。**同点の決着は「いちばん最初に投稿した入札」で行う（設計 3-77）。
// **更新時刻を採ると、負けた continuo があとから自分の入札を1文字直すだけで、投稿時刻を新しくできる。**
//
// 与える情報: 入札が3時間前に作られ、いま編集された状況。
// 成功条件: `PostedAt` が作成時刻（3時間前）のままであること。
func TestCollectBids_編集しても投稿時刻は動かない(t *testing.T) {
	now := at()
	created := now.Add(-3 * time.Hour)

	bids := handoff.CollectBids([]handoff.CommentView{{
		Author: otherLogin,
		Body: handoff.FormatBid(handoff.Bid{
			Author: otherLogin, FiveHour: 40, Weekly: 60, Score: 140, At: created,
		}, time.Minute),
		CreatedAt: created,
		UpdatedAt: now,
	}})

	if len(bids) != 1 {
		t.Fatalf("入札を1件読めていない: got %d 件", len(bids))
	}
	if !bids[0].PostedAt.Equal(created) {
		t.Errorf("編集で入札の投稿時刻が動いた: got %v, want %v（作成時刻）", bids[0].PostedAt, created)
	}
}

// 目的: いちばん新しい released の選び方が、編集で入れ替わらないことを固定する（設計 5-3k）。
//
// **なぜ要るか。**released は「いつ・どのアカウントの担当が外されたか」の記録である。
// **更新時刻で選ぶと、古い記録が最新に化け、ログに間違ったアカウント名が残る。**
//
// 与える情報: 古い released（`old-host`）がいま編集され、新しい released（`thinkpad`）は編集されていない状況。
// 成功条件: 新しく作られたほう（`thinkpad`）が返ること。
func TestLatestReleased_編集しても新しいreleasedは入れ替わらない(t *testing.T) {
	now := at()

	got, ok := handoff.LatestReleased([]handoff.CommentView{
		{
			Author: otherLogin,
			Body: handoff.FormatReleased(handoff.Released{
				From: "old-host", Branch: "continuo/octocat/hello-world/188", At: now.Add(-100 * time.Hour),
			}),
			CreatedAt: now.Add(-100 * time.Hour),
			UpdatedAt: now,
		},
		{
			Author: otherLogin,
			Body: handoff.FormatReleased(handoff.Released{
				From: "thinkpad", Branch: "continuo/octocat/hello-world/188", At: now.Add(-50 * time.Hour),
			}),
			CreatedAt: now.Add(-50 * time.Hour),
		},
	})
	if !ok {
		t.Fatal("released があるのに取れない")
	}
	if got.From != "thinkpad" {
		t.Errorf("編集で released の新旧が入れ替わった: got %q, want %q", got.From, "thinkpad")
	}
}
