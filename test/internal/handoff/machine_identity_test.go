// **同じ GitHub アカウントで2台の機械を動かしたときの判定の検査である**（設計 3-77b）。
//
// **見張っているのは1点である。**「担当者のアカウントだけで自分の担当と決めない」こと。
// 1人が2台の機械を1つのアカウントで動かすのは、この機能のいちばん自然な使い方であり、
// **アカウントだけで比べると、勝った機械と負けた機械の両方が同じ issue に着手する。**
package handoff_test

import (
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/handoff"
)

// selfHost はテストで使う「この機械の名前」である（架空の名前）。
const selfHost = "mac-studio"

// twinHost は、同じアカウントで動いているもう1台の機械の名前である（架空の名前）。
const twinHost = "mac-mini"

// holdBy は、担当者と機械の名前を指定して hold のコメントを1件組み立てる。
//
// assignee: hold の `assignee` に書くログイン名。
// host: hold の `host` に書く機械の名前。
// created: 作成時刻。
// 戻り値: 判定へ渡す形のコメント。
func holdBy(assignee, host string, created time.Time) handoff.CommentView {
	return handoff.CommentView{
		Author: assignee,
		Body: handoff.FormatHold(handoff.Hold{
			Host: host, Assignee: assignee, Branch: "continuo/octocat/hello-world/188", At: created,
		}),
		CreatedAt: created,
	}
}

// 目的: 担当者が自分のアカウントでも、hold を持っているのが別の機械なら触らないことを確認する
// （設計 3-77b）。
//
// **ここが効かないと、1つのアカウントで動く2台が同じ branch の worktree を掴み、
// 2つ目の Claude Code が立つ。**
//
// 与える情報: 担当者は自分のアカウント1人。hold は同じアカウントの別の機械が1時間前に書いたもの。
// 成功条件: ActionSkipOtherMachine になること（入札もしない）。
func TestAssess_同じアカウントの別の機械が持つ担当には触らない(t *testing.T) {
	now := at()
	fresh := now.Add(-time.Hour)

	got := handoff.Assess(handoff.Situation{
		Assignees:   []string{selfLogin},
		Comments:    []handoff.CommentView{holdBy(selfLogin, twinHost, fresh)},
		SelfLogin:   selfLogin,
		SelfHost:    selfHost,
		Now:         now,
		IdleTimeout: idleTimeout,
	})
	if got.Action != handoff.ActionSkipOtherMachine {
		t.Fatalf("判定が違う: got %v, want %v", got.Action, handoff.ActionSkipOtherMachine)
	}
	if got.Hold.Host != twinHost {
		t.Errorf("読んだ hold の機械の名前が違う: got %q, want %q", got.Hold.Host, twinHost)
	}
}

// 目的: hold の機械の名前がこの機械なら、そのまま着手へ進むことを確認する（設計 3-77b）。
//
// 与える情報: 担当者は自分のアカウント1人。hold もこの機械が書いたもの。
// 成功条件: ActionProceed になること。
func TestAssess_この機械のholdなら進む(t *testing.T) {
	now := at()

	got := handoff.Assess(handoff.Situation{
		Assignees:   []string{selfLogin},
		Comments:    []handoff.CommentView{holdBy(selfLogin, selfHost, now.Add(-time.Hour))},
		SelfLogin:   selfLogin,
		SelfHost:    selfHost,
		Now:         now,
		IdleTimeout: idleTimeout,
	})
	if got.Action != handoff.ActionProceed {
		t.Fatalf("判定が違う: got %v, want %v", got.Action, handoff.ActionProceed)
	}
}

// 目的: 同じアカウントの別の機械が落ちたまま期限を過ぎたら、担当を外して入札をやり直すことを
// 確認する（設計 3-77b）。
//
// **ここが効かないと、担当者が自分のアカウントのままなので、どの機械もその issue を拾えない。**
//
// 与える情報: 担当者は自分のアカウント1人。hold は別の機械が19時間前に書いたもので、
// その担当者のコメントも19時間前が最後。
// 成功条件: ActionRelease になること。
func TestAssess_同じアカウントの別の機械が落ちたら期限で外す(t *testing.T) {
	now := at()
	stale := now.Add(-idleTimeout - time.Minute)

	got := handoff.Assess(handoff.Situation{
		Assignees:   []string{selfLogin},
		Comments:    []handoff.CommentView{holdBy(selfLogin, twinHost, stale)},
		SelfLogin:   selfLogin,
		SelfHost:    selfHost,
		Now:         now,
		IdleTimeout: idleTimeout,
	})
	if got.Action != handoff.ActionRelease {
		t.Fatalf("判定が違う: got %v, want %v", got.Action, handoff.ActionRelease)
	}
	if got.Hold.Host != twinHost {
		t.Errorf("released に書く機械の名前が違う: got %q, want %q", got.Hold.Host, twinHost)
	}
}

// 目的: 機械の名前を知らないときは、アカウントだけで判定していた頃と同じ動きへ落ちることを
// 確認する（設計 3-77b）。
//
// 与える情報: `SelfHost` が空。担当者は自分のアカウント1人で、hold は別の機械のもの。
// 成功条件: ActionProceed になること（機械の名前で区別できないので、止める根拠が無い）。
func TestAssess_機械の名前を知らなければアカウントだけで判定する(t *testing.T) {
	now := at()

	got := handoff.Assess(handoff.Situation{
		Assignees:   []string{selfLogin},
		Comments:    []handoff.CommentView{holdBy(selfLogin, twinHost, now.Add(-time.Hour))},
		SelfLogin:   selfLogin,
		Now:         now,
		IdleTimeout: idleTimeout,
	})
	if got.Action != handoff.ActionProceed {
		t.Fatalf("判定が違う: got %v, want %v", got.Action, handoff.ActionProceed)
	}
}

// 目的: 人間が引き継いだ issue を、既に居ない機械の古い hold を根拠に取り上げないことを
// 確認する（設計 3-77b）。
//
// **hold のコメントは、担当が移っても入札の回が変わっても消えない。**
// 担当者で絞らないと、**issue のどこかに hold が1件でもあるだけで「いまの担当者は機械である」
// と読まれ、人間の担当が外される。**
//
// 与える情報: いまの担当者は人間（`octocat`）。issue には、既に外れた機械が別の担当者
// （`octocat-bot-b`）として20時間前に書いた hold が1件残っている。
// 人間の最後のコメントは19時間前（期限切れ）。
// 成功条件: ActionSkipHumanAssigned になること（**ActionRelease になってはならない**）。
func TestAssess_他の担当者のholdでは人間の担当を外さない(t *testing.T) {
	now := at()
	humanLogin := "octocat"

	got := handoff.Assess(handoff.Situation{
		Assignees: []string{humanLogin},
		Comments: []handoff.CommentView{
			holdBy(otherLogin, twinHost, now.Add(-20*time.Hour)),
			{Author: humanLogin, Body: "私が引き取ります", CreatedAt: now.Add(-idleTimeout - time.Minute)},
		},
		SelfLogin:   selfLogin,
		SelfHost:    selfHost,
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
		holdBy(selfLogin, selfHost, now.Add(-3*time.Hour)),
		holdBy(otherLogin, twinHost, now.Add(-time.Hour)),
	}

	got, ok := handoff.LatestHoldFor(comments, selfLogin)
	if !ok {
		t.Fatal("その担当者の hold を見つけられていない")
	}
	if got.Host != selfHost {
		t.Errorf("別の担当者の hold を返している: got %q, want %q", got.Host, selfHost)
	}
	if _, ok := handoff.LatestHoldFor(comments, ""); ok {
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

	got := handoff.Assess(handoff.Situation{
		Assignees: []string{otherLogin},
		Comments: []handoff.CommentView{{
			Author:    otherLogin,
			Body:      handoff.FormatHold(handoff.Hold{Host: twinHost, At: stale}),
			CreatedAt: stale,
		}},
		SelfLogin:   selfLogin,
		SelfHost:    selfHost,
		Now:         now,
		IdleTimeout: idleTimeout,
	})
	if got.Action != handoff.ActionSkipHumanAssigned {
		t.Fatalf("判定が違う: got %v, want %v", got.Action, handoff.ActionSkipHumanAssigned)
	}
}
