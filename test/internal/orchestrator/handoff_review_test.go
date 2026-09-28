// **PR #108 のレビューで見つかった穴を塞いだことの検査である**（設計 3-77b / 3-77f 〜 3-77i）。
//
// **見張っているのは3点である。**
//
//	着手をやめたときの後始末  … 書いた担当者を消し戻す。**残すと18時間塞がる**
//	使用率の値の寿命         … 古くなったら入札しない。**古い値で入札し続けない**
//	巡回を塞がないこと        … コメントを読む issue の数に上限を置く
package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// 目的: 人間が引き継いだ issue を、既に居ない continuo の古い hold を根拠に取り上げないことを
// 確認する（設計 3-77b）。
//
// **hold のコメントは、担当が移っても入札の回が変わっても消えない。**
// 担当者で絞らないと、**issue のどこかに hold が1件でもあるだけで「いまの担当者は機械である」
// と読まれ、人間の担当が外される。**
//
// 与える情報: いまの担当者は人間（`octocat-human`）。issue には、別の担当者（`octocat-bot-b`）
// として20時間前に書かれた hold が1件残っている。人間の最後のコメントは19時間前。
// 成功条件: released のコメントが1件も書かれず、人間の担当が外れないこと。
func TestHandoff_他の担当者のholdでは人間の担当を外さない(t *testing.T) {
	const humanLogin = "octocat-human"

	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "Ready", humanLogin))

	node := issueNode(188)
	older := time.Now().Add(-20 * time.Hour)
	fx.Tracker.AddCommentBy(node, rivalLogin, handoff.FormatHold(handoff.Hold{
		Assignee: rivalLogin,
		Branch:   "continuo/octocat/hello-world/188", At: older,
	}), older)
	old := time.Now().Add(-19 * time.Hour)
	fx.Tracker.AddCommentBy(node, humanLogin, "私が引き取ります", old)

	fx.Orc.Tick(context.Background())

	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffReleasedMarker)); got != 0 {
		t.Errorf("人間が引き継いだ担当を外している: released が %d 件", got)
	}
	issue, ok := fx.Tracker.IssueByID("PVTI_item188")
	if !ok {
		t.Fatal("issue がカンバンから消えた")
	}
	if len(issue.Assignees) != 1 || issue.Assignees[0].Login != humanLogin {
		t.Errorf("人間の担当が外れている: %+v", issue.Assignees)
	}
}

// 目的: 着手の直前の検査に落ちる issue には、担当者を1バイトも書かないことを確認する
// （設計 3-77g）。
//
// **担当者と hold を書いてから落とすと、ほかの機械はそれを「期限内の担当」と読み、
// `idle_timeout_ms`（既定18時間）触らない。**
// **この機械では信頼していないが、別の機械では信頼しているリポジトリが、そのあいだ塞がる。**
//
// 与える情報: リポジトリを信頼登録していない状態（着手の直前の検査で必ず落ちる）と、
// 担当者のいない issue 1件。
// 成功条件: 入札も hold も1件も書かれず、担当者も付かないこと。
func TestHandoff_信頼していないリポジトリには担当者を書かない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Untrusted: true})
	holdPrompt(fx)
	fx.AllowLog("信頼登録されていません")
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	node := issueNode(188)
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffBidMarker)); got != 0 {
		t.Errorf("着手できない issue に入札している: %d 件", got)
	}
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffHoldMarker)); got != 0 {
		t.Errorf("着手できない issue の担当者になっている: hold が %d 件", got)
	}
	issue, ok := fx.Tracker.IssueByID("PVTI_item188")
	if !ok {
		t.Fatal("issue がカンバンから消えた")
	}
	if len(issue.Assignees) != 0 {
		t.Errorf("着手できない issue に担当者を書いた（18時間塞がる）: %+v", issue.Assignees)
	}
}

// 目的: 使用率の値が古くなったら、そこから先は入札しないことを確認する（設計 3-77i。issue #284）。
//
// **古い値で入札させない。**止まったセッションや statusline取得が届かない機械は、最後に
// 届いた「使用率 5%」を持ち続ける。**それで入札すると、正直に読めている機械に必ず勝つ。**
// 勝った機械は着手できないので、**その issue は誰にも進まない。**
// 新しさは「`rate_limits` を持つ新しい応答の行を最後に受けた時刻」から `refresh_interval_ms`
// までである（計画の「値の保管と読み方」）。
//
// 与える情報: 手で進める時計。ステータスラインから 5% の新しい応答の行を受けたあと、
// 巡回1回目で issue 188 に入札させる。時計を `refresh_interval_ms` より進め、
// 2回目の巡回の前に issue 189 を足す。trust.repositories は空（statusline取得は
// 「使える clone が無い」で終わる）。
// 成功条件: issue 188 には入札があり、**issue 189 には入札が1件も無い**こと。
func TestHandoff_値が古くなったら入札を止める(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceStatusline
			// **締め切りを待たせる。**待たせないと1回目の巡回で担当者になり、
			// スロットが埋まって2回目の候補を見なくなる。
			cfg.Tracker.Provider.Handoff.BidWindowMs = 3600000
		},
	})
	holdPrompt(fx)
	// **値が古くなると statusline取得を開き、使える clone が無いので WARN を出す。**
	// その状況はこのテストが作っている（trust.repositories を空にしている）。
	fx.AllowLog("使える clone が無い")
	feedFreshQuota(fx.Orc, "pane-a", clock.Now(), 5, 10)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(issueNode(188), config.HandoffBidMarker)); got != 1 {
		t.Fatalf("値が新しいのに入札していない: %d 件", got)
	}

	// **新しさの上限を跨がせてから、次の候補を足す。**
	clock.Advance(time.Duration(fx.Config.RateLimit.RefreshIntervalMs)*time.Millisecond + time.Second)
	fx.Tracker.AddIssue(sampleIssue(189, "Ready"))
	fx.Orc.Tick(context.Background())

	if got := len(fx.Tracker.MarkedHandoffCommentsOf(issueNode(189), config.HandoffBidMarker)); got != 0 {
		t.Errorf("値が古くなったのに古い写しで入札している: %d 件", got)
	}
}

// 目的: 入札に勝って担当者を書けても、hold のコメントを書けなかったら着手せず、
// 書いた担当者を消し戻すことを確認する（設計 3-77g）。
//
// **hold を書けないまま着手を許すと、担当者はあるが hold は無い状態が issue に残る。**
// **その状態は assess.go の「自分のアカウント1人＋hold が1件も無い」に落ち、
// 同じ GitHub アカウントの別の機械も「待たずに着手してよい」と読む。**
// **アカウントだけで比較していた頃と同じ穴が、この経路からもう一度開いてしまう。**
//
// 与える情報: 担当者のいない `Ready` の issue 1件。hold のコメント（`continuo:hold` で
// 始まるコメント）だけ投稿が失敗するようにした偽のトラッカー。
// 成功条件: 着手しないこと。入札のコメントは1件書かれるが hold は1件も書かれないこと。
// **担当者が消え戻り、released のコメントが1件書かれる**こと。
func TestHandoff_holdを書けなかったら担当者を消し戻して着手しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.AllowLog("hold のコメントを書けないので、着手を見送って担当者を消し戻します")
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.SetPostErrorForMarker(config.HandoffHoldMarker,
		errors.New("hold のコメントの投稿に失敗しました（テスト用）"))

	fx.Orc.Tick(context.Background())

	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Fatalf("hold を書けなかったのに着手した: 実行中 %d 件", got)
	}

	node := issueNode(188)
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffBidMarker)); got != 1 {
		t.Fatalf("入札のコメントが1件ではない: %d 件", got)
	}
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffHoldMarker)); got != 0 {
		t.Errorf("投稿が失敗したはずの hold が書かれている: %d 件", got)
	}

	issue, ok := fx.Tracker.IssueByID("PVTI_item188")
	if !ok {
		t.Fatal("issue がカンバンから消えた")
	}
	if len(issue.Assignees) != 0 {
		t.Errorf("hold を書けなかったのに担当者が残っている（18時間塞がる）: %+v", issue.Assignees)
	}

	released := fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffReleasedMarker)
	if len(released) != 1 {
		t.Fatalf("released のコメントが1件ではない: %d 件", len(released))
	}
	if !strings.Contains(released[0].Body, `"from":"`+testGHLogin+`"`) {
		t.Errorf("released に消し戻したアカウントの名前が入っていない:\n%s", released[0].Body)
	}
}
