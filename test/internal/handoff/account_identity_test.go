// **持ち回りで参加者を見分ける値の検査である**（設計 3-77-0）。
//
// **見張っているのは2点である。**
//
//	見分ける値は gh の持ち主のログイン名である … 担当者のアカウントが自分なら、担当も自分である
//	入札の識別子は投稿者から取る             … 本文の JSON には書かない。書けば騙れる
//
// **同じ GitHub アカウントを複数の機械で使うことはサポートしない**（2026-09-04 に人間が決定）。
// **だから「アカウントは自分だが担当しているのは別の機械」という状態は作れない。**
package handoff_test

import (
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/handoff"
)

// 目的: 担当者が自分のアカウントなら、hold が何であっても着手へ進むことを確認する
// （設計 3-77-0 / 3-77b）。
//
// **アカウント1つにつき continuo は1つである。**hold を読んで機械を見分ける段は無い。
//
// 与える情報: 担当者は自分のアカウント1人。hold は自分の担当として1時間前に書かれている。
// 成功条件: ActionProceed になること。
func TestAssess_自分のアカウントの担当なら進む(t *testing.T) {
	now := at()

	got := handoff.Assess(handoff.Situation{
		Assignees:   []string{selfLogin},
		Comments:    []handoff.CommentView{holdComment(selfLogin, now.Add(-time.Hour))},
		SelfLogin:   selfLogin,
		Now:         now,
		IdleTimeout: idleTimeout,
	})
	if got.Action != handoff.ActionProceed {
		t.Fatalf("判定が違う: got %v, want %v", got.Action, handoff.ActionProceed)
	}
}

// 目的: 担当者が自分のアカウントなら、期限を過ぎていても自分から担当を奪わないことを確認する
// （設計 3-77-0）。
//
// **ここが効かないと、進捗報告を18時間書かなかった自分の run を、自分で外しにいく。**
// **奪う相手が自分なので、担当者を外して入札からやり直すことに意味が無い。**
//
// 与える情報: 担当者は自分のアカウント1人。hold は19時間前で、進捗報告も1件も無い。
// 成功条件: ActionProceed になること（**ActionRelease になってはならない**）。
func TestAssess_自分のアカウントの担当は期限を過ぎても外さない(t *testing.T) {
	now := at()
	stale := now.Add(-idleTimeout - time.Minute)

	got := handoff.Assess(handoff.Situation{
		Assignees:   []string{selfLogin},
		Comments:    []handoff.CommentView{holdComment(selfLogin, stale)},
		SelfLogin:   selfLogin,
		Now:         now,
		IdleTimeout: idleTimeout,
	})
	if got.Action != handoff.ActionProceed {
		t.Fatalf("自分の担当を自分で外そうとしている: got %v, want %v", got.Action, handoff.ActionProceed)
	}
}

// 目的: 入札を見分ける値が、コメントの投稿者から来ることを確認する（設計 3-77-0）。
//
// **本文の JSON からは取らない。**本文は第三者にも書けるので、
// **他の continuo のログイン名を騙られると、騙られたほうは `HasBidBy` が真になって
// その回は入札しない。**GitHub が付ける投稿者は騙れない。
//
// 与える情報: 投稿者が `octocat-bot-b` で、本文の JSON に `host` として別の名前を混ぜた入札1件。
// 成功条件: 識別子が投稿者（`octocat-bot-b`）になること。本文に混ぜた名前では見つからないこと。
func TestCollectBids_識別子は投稿者から取る(t *testing.T) {
	created := at()
	// **いまの continuo はこの欄を書かない。**第三者が手で書いたコメントを模している。
	body := config.HandoffBidMarker + "\n" +
		`{"host":"octocat-bot-a","five_hour":100,"weekly":100,"score":300,"at":"2026-08-29T16:45:00+09:00"}`

	bids := handoff.CollectBids([]handoff.CommentView{{
		Author: otherLogin, Body: body, CreatedAt: created,
	}})
	if len(bids) != 1 {
		t.Fatalf("入札を読めていない: %d 件", len(bids))
	}
	if bids[0].Author != otherLogin {
		t.Errorf("識別子が投稿者になっていない: got %q, want %q", bids[0].Author, otherLogin)
	}
	if _, ok := handoff.HasBidBy(bids, selfLogin); ok {
		t.Error("本文に書かれた名前で入札を見つけている（騙れてしまう）")
	}
}

// 目的: 投稿者の分からないコメントを入札として数えないことを確認する（設計 3-77-0）。
//
// **GitHub は削除済みアカウントのコメントに投稿者を付けない。**
// **数えると、その入札が勝った回はどの continuo も着手しない。**
// **判定スコアがいちばん大きければ、同点にならなくても勝つ。**
// **同点でも、投稿が早ければ2段目で勝つ。**投稿の時刻まで同じなら3段目で、空文字がどのログイン名にも勝つ。
//
// 与える情報: 投稿者が空の入札1件と、投稿者のある入札1件。
// 成功条件: 投稿者のあるほうだけが返ること。
func TestCollectBids_投稿者の分からない入札は数えない(t *testing.T) {
	created := at()
	body := config.HandoffBidMarker + "\n" +
		`{"five_hour":100,"weekly":100,"score":300,"at":"2026-08-29T16:45:00+09:00"}`

	bids := handoff.CollectBids([]handoff.CommentView{
		{Author: "", Body: body, CreatedAt: created},
		{Author: selfLogin, Body: body, CreatedAt: created.Add(time.Minute)},
	})
	if len(bids) != 1 {
		t.Fatalf("投稿者の分からない入札を数えている: %d 件", len(bids))
	}
	if bids[0].Author != selfLogin {
		t.Errorf("残った入札が違う: got %q, want %q", bids[0].Author, selfLogin)
	}
}

// 目的: 古い版の continuo が書いたコメントを、そのまま読めることを確認する（設計 3-77-0）。
//
// **古いコメントには `host` の欄が入っている。**JSON は知らないキーを無視するので、
// **hold は `assignee` だけが読まれ、入札は投稿者から識別子が入る。**
// **ここが落ちると、版を上げた瞬間に、走っている issue の担当が読めなくなる。**
//
// 与える情報: `host` の欄を持つ古い形の hold と入札。
// 成功条件: どちらも読めて、識別子が正しく取れること。
func TestParse_古い形のコメントを読める(t *testing.T) {
	created := at()

	oldHold := config.HandoffHoldMarker + "\n" +
		`{"host":"mac-studio","assignee":"` + selfLogin + `","branch":"continuo/octocat/hello-world/188",` +
		`"at":"2026-08-29T18:45:00+09:00"}`
	hold, ok := handoff.ParseHold(oldHold)
	if !ok {
		t.Fatalf("古い形の hold を読めない:\n%s", oldHold)
	}
	if hold.Assignee != selfLogin {
		t.Errorf("古い形の hold の担当者が違う: got %q, want %q", hold.Assignee, selfLogin)
	}
	if hold.Branch != "continuo/octocat/hello-world/188" {
		t.Errorf("古い形の hold の branch が違う: got %q", hold.Branch)
	}

	oldBid := config.HandoffBidMarker + "\n" +
		`{"host":"mac-studio","five_hour":87,"weekly":16,"score":190,"at":"2026-08-29T16:45:00+09:00"}`
	bid, ok := handoff.ParseBid(oldBid, selfLogin, created)
	if !ok {
		t.Fatalf("古い形の入札を読めない:\n%s", oldBid)
	}
	if bid.Author != selfLogin {
		t.Errorf("古い形の入札の識別子が投稿者になっていない: got %q, want %q", bid.Author, selfLogin)
	}
	if bid.Score != 190 {
		t.Errorf("古い形の入札の判定スコアが違う: got %d, want %d", bid.Score, 190)
	}
}

// 目的: 人間が引き継いだ issue を、既に居ない continuo の古い hold を根拠に取り上げないことを
// 確認する（設計 3-77b）。
//
// **hold のコメントは、担当が移っても入札の回が変わっても消えない。**
// 担当者で絞らないと、**issue のどこかに hold が1件でもあるだけで「いまの担当者は機械である」
// と読まれ、人間の担当が外される。**
//
// 与える情報: いまの担当者は人間（`octocat`）。issue には、既に外れた continuo が別の担当者
// （`octocat-bot-b`）として20時間前に書いた hold が1件残っている。
// 人間の最後のコメントは19時間前（期限切れ）。
// 成功条件: ActionSkipHumanAssigned になること（**ActionRelease になってはならない**）。
func TestAssess_他の担当者のholdでは人間の担当を外さない(t *testing.T) {
	now := at()
	humanLogin := "octocat"

	got := handoff.Assess(handoff.Situation{
		Assignees: []string{humanLogin},
		Comments: []handoff.CommentView{
			holdComment(otherLogin, now.Add(-20*time.Hour)),
			{Author: humanLogin, Body: "私が引き取ります", CreatedAt: now.Add(-idleTimeout - time.Minute)},
		},
		SelfLogin:   selfLogin,
		Now:         now,
		IdleTimeout: idleTimeout,
	})
	if got.Action != handoff.ActionSkipHumanAssigned {
		t.Fatalf("人間が引き継いだ担当を機械が取り上げようとしている: got %v, want %v",
			got.Action, handoff.ActionSkipHumanAssigned)
	}
}

// 目的: LatestHoldFor が担当者で hold を絞ることを確認する（設計 3-77b）。
//
// 与える情報: 2人ぶんの hold（`octocat-bot-b` が新しく、`octocat-bot-a` が古い）。
// 成功条件: `octocat-bot-a` で引くと、`octocat-bot-a` の hold が返ること。
// 担当者の名前が空なら1件も返らないこと。
func TestLatestHoldFor_担当者で絞る(t *testing.T) {
	now := at()
	comments := []handoff.CommentView{
		holdCommentOn(selfLogin, "self-branch", now.Add(-3*time.Hour)),
		holdCommentOn(otherLogin, "other-branch", now.Add(-time.Hour)),
	}

	got, gotAt, ok := handoff.LatestHoldFor(comments, selfLogin)
	if !ok {
		t.Fatal("その担当者の hold を見つけられていない")
	}
	if got.Assignee != selfLogin || got.Branch != "self-branch" {
		t.Errorf("別の担当者の hold を返している: got %+v", got)
	}
	// **作成時刻も返す。**持ち回りの期限は、この時刻を下限にして数える（設計 5-3l）。
	if !gotAt.Equal(now.Add(-3 * time.Hour)) {
		t.Errorf("その hold の作成時刻を返していない: got %v, want %v", gotAt, now.Add(-3*time.Hour))
	}
	if _, _, ok := handoff.LatestHoldFor(comments, ""); ok {
		t.Error("担当者の名前が空なのに hold を返している")
	}
}

// 目的: `assignee` を持たない hold は、誰のものとも数えないことを確認する（設計 3-77b）。
//
// **continuo が書く hold は必ず担当者のログイン名を持つ。**空なのは、人間が印だけ
// 真似て書いたときである。**奪ってよい証拠として使わない。**
//
// 与える情報: `assignee` が空の hold 1件と、期限切れの他人の担当。
// 成功条件: ActionSkipHumanAssigned になること。
func TestAssess_assigneeの無いholdは証拠にしない(t *testing.T) {
	now := at()
	stale := now.Add(-idleTimeout - time.Minute)

	body := handoff.FormatHold(handoff.Hold{At: stale})
	if strings.Contains(body, `"assignee":"`+otherLogin+`"`) {
		t.Fatalf("検査の前提が崩れている（assignee が入ってしまっている）:\n%s", body)
	}

	got := handoff.Assess(handoff.Situation{
		Assignees: []string{otherLogin},
		Comments: []handoff.CommentView{{
			Author:    otherLogin,
			Body:      body,
			CreatedAt: stale,
		}},
		SelfLogin:   selfLogin,
		Now:         now,
		IdleTimeout: idleTimeout,
	})
	if got.Action != handoff.ActionSkipHumanAssigned {
		t.Fatalf("判定が違う: got %v, want %v", got.Action, handoff.ActionSkipHumanAssigned)
	}
}
