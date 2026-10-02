// {"RUCM-CFG-SHA256": "f99e2491290ee83ea6b886bb29b9721038763ee5082a7f53dd7a875fafc5a0d2", "SOURCE": "docs/spec/usecases/particular_case/issue の担当を入札で決める.cfg.json"}
//
// **ユースケース記述「issue の担当を入札で決める」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
// 巡回を何回か回すテストは、最初の巡回が通る経路の番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package orchestrator_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/handoff"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// {"RUCM-PATH": "P001"}
//
// Test_issueの担当を入札で決める_P001_勝ったら担当者になり入札とholdを1件ずつ書く は、基本フローを確かめる。
//
// 目的: 設計 3-77 の「担当者がいなければ入札し、勝ったら自分を担当者に加えて hold を書く」。
// 与える情報: 担当者のいない `Ready` の issue 1件と、ほかの機械の入札は無い状態。
// 成功条件: 着手されること。入札のコメントと hold のコメントが1件ずつ増え、
// 担当者が gh の持ち主1人になっていること。
func Test_issueの担当を入札で決める_P001_勝ったら担当者になり入札とholdを1件ずつ書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	waitFor(t, 5*time.Second, "dispatch される", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 1
	})

	node := issueNode(188)
	bids := fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffBidMarker)
	if len(bids) != 1 {
		t.Fatalf("入札のコメントが1件ではない: %d 件", len(bids))
	}
	// **入札の JSON に自分で名乗る欄は無い**（設計 3-77-0）。書いたのは誰かは投稿者が答える。
	if strings.Contains(bids[0].Body, `"host"`) {
		t.Errorf("入札の JSON に機械の名前の欄が残っている:\n%s", bids[0].Body)
	}
	if bids[0].Author != testGHLogin {
		t.Errorf("入札の投稿者が gh の持ち主になっていない: got %q, want %q", bids[0].Author, testGHLogin)
	}

	holds := fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffHoldMarker)
	if len(holds) != 1 {
		t.Fatalf("hold のコメントが1件ではない: %d 件", len(holds))
	}
	if !strings.Contains(holds[0].Body, `"branch":"continuo/octocat/hello-world/188"`) {
		t.Errorf("hold に branch の名前が入っていない:\n%s", holds[0].Body)
	}

	issue, ok := fx.Tracker.IssueByID("PVTI_item188")
	if !ok {
		t.Fatal("issue がカンバンから消えた")
	}
	if len(issue.Assignees) != 1 || issue.Assignees[0].Login != testGHLogin {
		t.Errorf("担当者が gh の持ち主1人になっていない: %+v", issue.Assignees)
	}
}

// {"RUCM-PATH": "P002"}
//
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
func Test_issueの担当を入札で決める_P002_holdを書けなかったら担当者を消し戻して着手しない(t *testing.T) {
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

// {"RUCM-PATH": "P005"}
//
// Test_issueの担当を入札で決める_P005_入札に負けたら担当者にならない は、勝者の決め方を確かめる。
//
// 目的: 設計 3-77 の「判定スコアがいちばん大きい機械が勝つ」。
// 与える情報: 判定スコアがこの機械より大きい、ほかの機械の入札が既に1件ある issue。
// 成功条件: 担当者にならず、着手もしないこと。hold のコメントも増えないこと。
func Test_issueの担当を入札で決める_P005_入札に負けたら担当者にならない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	node := issueNode(188)
	// **この機械の判定スコアは 270 である**（枠を読まない設定なので余裕値は 100 − マージン 10）。
	// それより大きい入札を、ほかの機械が先に書いてある状態にする。
	fx.Tracker.AddCommentBy(node, rivalLogin, handoff.FormatBid(handoff.Bid{
		FiveHour: 100, Weekly: 100, Score: 300, At: time.Now(),
	}, testBidWindow), time.Now().Add(-time.Minute))

	fx.Orc.Tick(context.Background())

	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffHoldMarker)); got != 0 {
		t.Errorf("負けたのに hold を書いている: %d 件", got)
	}
	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	if len(issue.Assignees) != 0 {
		t.Errorf("負けたのに担当者になっている: %+v", issue.Assignees)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Errorf("負けたのに着手している: %d 件", got)
	}
}

// {"RUCM-PATH": "P006"}
//
// Test_issueの担当を入札で決める_P006_締め切りの前は担当者にならない は、締め切りの待ちを確かめる。
//
// 目的: 設計 3-77 の「締め切りは、入札が1件も無い issue への最初の投稿から bid_window_ms」。
// 与える情報: 締め切りを3分にした設定と、担当者のいない issue 1件。
// 成功条件: 入札のコメントは1件書かれるが、担当者は付かず、着手もしないこと。
func Test_issueの担当を入札で決める_P006_締め切りの前は担当者にならない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.Provider.Handoff.BidWindowMs = 180000
		},
	})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	node := issueNode(188)
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffBidMarker)); got != 1 {
		t.Fatalf("入札のコメントが1件ではない: %d 件", got)
	}
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffHoldMarker)); got != 0 {
		t.Errorf("締め切り前なのに hold を書いている: %d 件", got)
	}
	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	if len(issue.Assignees) != 0 {
		t.Errorf("締め切り前なのに担当者になっている: %+v", issue.Assignees)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Errorf("締め切り前なのに着手している: %d 件", got)
	}
}

// {"RUCM-PATH": "P006"}
//
// Test_issueの担当を入札で決める_P006_古い入札が残っていても締め切りをまたいで担当者が決まる は、設計 3-77e を確かめる。
//
// 目的: **前の回の入札は issue に残り続ける**（入札は1回ごとに新しいコメントを書く）。
// それを数え続けると締め切りが常にその古い時刻から数えられ、**次の回が1度も始まらない。**
// **巡回のたびに入札のコメントだけが増え、担当者は永久に決まらない。**
//
// 与える情報: 締め切りを3分にした設定と、30分前に書かれたほかの機械の入札1件
// （判定スコアはこの機械より大きい **300**）。時計は手で進める。
// 成功条件: 巡回を3回行っても、**この機械の入札のコメントは1件だけ**であること。
// 締め切りを過ぎた巡回でこの機械が担当者になり、hold が1件書かれ、着手されること。
func Test_issueの担当を入札で決める_P006_古い入札が残っていても締め切りをまたいで担当者が決まる(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{
		Now: clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.Provider.Handoff.BidWindowMs = 180000
		},
	})
	holdPrompt(fx)
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	node := issueNode(188)
	// **終わった回の入札である。**締め切り（3分）にも決着の猶予（さらに3分）にも入らない。
	fx.Tracker.AddCommentBy(node, rivalLogin, handoff.FormatBid(handoff.Bid{
		FiveHour: 100, Weekly: 100, Score: 300, At: clock.Now().Add(-30 * time.Minute),
	}, testBidWindow), clock.Now().Add(-30*time.Minute))

	// 1回目。**次の回を始める入札を1件書き、締め切りを待つ。**
	fx.Orc.Tick(context.Background())
	if got := len(ownBidsOf(fx, node)); got != 1 {
		t.Fatalf("1回目の巡回でこの continuo の入札が1件ではない: %d 件", got)
	}

	// 2回目（30秒後）。**締め切りの中なので、入札は増えない。**
	clock.Advance(30 * time.Second)
	fx.Orc.Tick(context.Background())
	if got := len(ownBidsOf(fx, node)); got != 1 {
		t.Fatalf("締め切りを待つあいだに入札が増えた: %d 件", got)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Fatalf("締め切り前なのに着手している: %d 件", got)
	}

	// 3回目（締め切りの後）。**勝って担当者になる。**
	clock.Advance(4 * time.Minute)
	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "dispatch される", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 1
	})

	if got := len(ownBidsOf(fx, node)); got != 1 {
		t.Errorf("巡回のたびに入札が増えている: %d 件", got)
	}
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffHoldMarker)); got != 1 {
		t.Errorf("hold のコメントが1件ではない: %d 件", got)
	}
	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	if len(issue.Assignees) != 1 || issue.Assignees[0].Login != testGHLogin {
		t.Errorf("担当者が gh の持ち主1人になっていない: %+v", issue.Assignees)
	}
}

// {"RUCM-PATH": "P014"}
//
// Test_issueの担当を入札で決める_P014_枠を読めなくても自分が担当のissueには着手する は、巡回を打ち切っていないことを
// 確かめる（設計 3-77j。issue #173）。
//
// 目的: **枠を読めないだけで巡回を打ち切ってはならない。**打ち切ると、
// **この機械が既に担当者になっている issue まで着手されなくなる**（印が無いのでこの経路からしか
// 拾えない）。**期限切れの担当を外す経路も通らない。**
//
// 与える情報: usage API が 500 を返す。**この機械（gh の持ち主）が担当者の `Ready` の issue が1件。**
// 成功条件: その issue が dispatch されること。
func Test_issueの担当を入札で決める_P014_枠を読めなくても自分が担当のissueには着手する(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	reader := newUsageReader(t, srv.URL, "CONTINUO_TEST_OAUTH_TOKEN_MINE")

	fx := newStubFixture(t, stubFixtureOptions{
		// **ログを溜める。**止めた理由が既定の水準で出ることを検査する（issue #173）。
		Logs:      true,
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	fx.Tracker.AddIssue(assignedIssue(191, "Ready", testGHLogin))

	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#191" {
			return
		}
	}
	t.Fatalf("既に自分が担当の issue にまで着手していない（枠を読めないだけで巡回を打ち切っている）:\n%s",
		fx.Logs.String())
}

// {"RUCM-PATH": "P014"}
//
// Test_issueの担当を入札で決める_P014_枠が逼迫していても担当が自分のissueには着手する は、
// 止める範囲が入札の要る issue だけであることを確かめる（設計 3-27。issue #173）。
//
// 目的: **巡回を丸ごと打ち切ってはならない。**
// **以前は `rate_limit.pause_above_percent` を超えると `dispatchCandidates` が即 `return` していた。**
// **その設定は消えた**（人間の決定。2026-09-06）。**打ち切ると、この機械が既に担当者に
// なっている issue まで着手されなくなる**（印が無いのでこの経路からしか拾えない）。
// **再起動で復元した run も拾えない**（`restart.orphan_running_action` の既定 `redispatch` は
// 復元では何もせず、次の巡回に委ねる）。**`handoffGate` の中にある「期限切れの担当を外す」
// 経路も通らなくなる**ので、詰まったカンバンを誰も解けない。
//
// 与える情報: 1回目は 99% を返し、2回目以降は 500 を返す usage API。
// **この機械が担当者の `Ready` の issue が1件**（入札を要さない経路）。
// 成功条件: その issue に着手すること。
func Test_issueの担当を入札で決める_P014_枠が逼迫していても担当が自分のissueには着手する(t *testing.T) {
	var reads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if reads.Add(1) > 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{"limits": []map[string]any{
			{"kind": "session", "percent": 99, "resets_at": nil, "severity": "normal"},
		}}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("偽の usage API が応答を書けません: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	reader := newUsageReader(t, srv.URL, "CONTINUO_TEST_OAUTH_TOKEN_STALE")

	fx := newStubFixture(t, stubFixtureOptions{
		// **ログを溜める。**止めた理由が既定の水準で出ることを検査する（issue #173）。
		Logs:      true,
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Trust.RequireRepoTrusted = false
		},
	})
	fx.Tracker.AddIssue(assignedIssue(192, "Ready", testGHLogin))

	// 1回目で 99% を読み、2回目からは読めなくなる。
	fx.Orc.Tick(context.Background())
	fx.Orc.Tick(context.Background())

	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == "octocat/hello-world#192" {
			return
		}
	}
	t.Fatalf("担当が自分の issue にまで着手していない（巡回を丸ごと打ち切っている）:\n%s",
		fx.Logs.String())
}

// {"RUCM-PATH": "P015"}
//
// Test_issueの担当を入札で決める_P015_期限切れの担当を外してreleasedを書く は、設計 3-77c を確かめる。
//
// 目的: 「担当者の最後のコメントから idle_timeout_ms を過ぎたら、担当を外して入札をやり直す」。
// 与える情報: ほかの機械が担当していて、その機械の最後のコメントが19時間前にある issue。
// 成功条件: 担当者からその機械が外れ、released のコメントが1件増え、
// **その本文に外したアカウントの名前が入っていて、引き継ぐアカウントの名前は入っていない**こと。
func Test_issueの担当を入札で決める_P015_期限切れの担当を外してreleasedを書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", rivalLogin))

	node := issueNode(188)
	old := time.Now().Add(-19 * time.Hour)
	fx.Tracker.AddCommentBy(node, rivalLogin, handoff.FormatHold(handoff.Hold{
		Assignee: rivalLogin, Branch: "continuo/octocat/hello-world/188", At: old,
	}), old)

	fx.Orc.Tick(context.Background())

	released := fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffReleasedMarker)
	if len(released) != 1 {
		t.Fatalf("released のコメントが1件ではない: %d 件", len(released))
	}
	if !strings.Contains(released[0].Body, `"from":"`+rivalLogin+`"`) {
		t.Errorf("released に外したアカウントの名前が入っていない:\n%s", released[0].Body)
	}
	if strings.Contains(released[0].Body, `"to":`) {
		t.Errorf("released に引き継ぐアカウントを書いている（この段では決まっていない）:\n%s", released[0].Body)
	}

	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	for _, a := range issue.Assignees {
		if a.Login == rivalLogin {
			t.Errorf("期限切れの担当が外れていない: %+v", issue.Assignees)
		}
	}
}

// {"RUCM-PATH": "P015"}
//
// Test_issueの担当を入札で決める_P015_期限切れの担当を外したあと前の回の入札に負けない は、設計 3-77e を確かめる。
//
// 目的: **担当を外した直後は、前の回に勝った機械の入札が必ず issue に残っている。**
// それを数えると、担当を外した機械は毎回その入札に負ける。**担当者は誰にも書かれず、
// 巡回のたびに入札のコメントだけが増える。**この機能の主目的である「期限で担当を入れ替える」
// 経路が、そのままこの状態に入る。
//
// 与える情報: ほかの機械が担当していて、その機械の hold が19時間前、
// **その機械が前の回に勝ったときの入札（判定スコア 300）がその1分前**にある issue。
// 成功条件: 担当が外れ、この機械が担当者になって着手すること。
func Test_issueの担当を入札で決める_P015_期限切れの担当を外したあと前の回の入札に負けない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", rivalLogin))

	node := issueNode(188)
	old := time.Now().Add(-19 * time.Hour)
	// **前の回の入札。**hold より前にあり、判定スコアはこの機械（270）より大きい。
	fx.Tracker.AddCommentBy(node, rivalLogin, handoff.FormatBid(handoff.Bid{
		FiveHour: 100, Weekly: 100, Score: 300, At: old.Add(-time.Minute),
	}, testBidWindow), old.Add(-time.Minute))
	fx.Tracker.AddCommentBy(node, rivalLogin, handoff.FormatHold(handoff.Hold{
		Assignee: rivalLogin, Branch: "continuo/octocat/hello-world/188", At: old,
	}), old)

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "dispatch される", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 1
	})

	if got := len(ownBidsOf(fx, node)); got != 1 {
		t.Errorf("この continuo の入札が1件ではない: %d 件", got)
	}
	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	if len(issue.Assignees) != 1 || issue.Assignees[0].Login != testGHLogin {
		t.Errorf("前の回の入札に負けて担当者になれていない: %+v", issue.Assignees)
	}
	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffHoldMarker)); got != 2 {
		t.Errorf("hold のコメントが2件（前の回とこの回）ではない: %d 件", got)
	}
}

// {"RUCM-PATH": "P031"}
//
// Test_issueの担当を入札で決める_P031_期限内の他人の担当には入札もしない は、設計 3-77b の表の1行を確かめる。
//
// 目的: 「他人1人 ＋ hold あり ＋ 期限内」は触らない（入札もしない）。
// 与える情報: ほかの機械が担当していて、その機械の hold と進捗のコメントが1時間前にある issue。
// 成功条件: 入札も hold も1件も増えず、担当者が変わらないこと。
func Test_issueの担当を入札で決める_P031_期限内の他人の担当には入札もしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", rivalLogin))

	node := issueNode(188)
	fx.Tracker.AddCommentBy(node, rivalLogin, handoff.FormatHold(handoff.Hold{
		Assignee: rivalLogin, Branch: "continuo/octocat/hello-world/188", At: time.Now(),
	}), time.Now().Add(-time.Hour))

	fx.Orc.Tick(context.Background())

	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, config.HandoffBidMarker)); got != 0 {
		t.Errorf("期限内の他人の担当に入札している: %d 件", got)
	}
	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	if len(issue.Assignees) != 1 || issue.Assignees[0].Login != rivalLogin {
		t.Errorf("担当者が変わっている: %+v", issue.Assignees)
	}
}

// {"RUCM-PATH": "P033"}
//
// Test_issueの担当を入札で決める_P033_holdの無い担当は奪わない は、設計 3-77b の「人間が付けた担当」を確かめる。
//
// 目的: **hold のコメントがあることが「その担当者は機械である」の唯一の証拠である。**
// 無ければ人間が付けた担当なので、continuo は取り上げない。
// 与える情報: ほかの人が担当していて、hold のコメントが1件も無い issue
// （**進捗のコメントは1年前**。期限だけで判定していれば奪ってしまう）。
// 成功条件: 入札も hold も増えず、担当者が変わらないこと。
func Test_issueの担当を入札で決める_P033_holdの無い担当は奪わない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", rivalLogin))

	node := issueNode(188)
	fx.Tracker.AddCommentBy(node, rivalLogin, "去年書いた進捗", time.Now().Add(-365*24*time.Hour))

	fx.Orc.Tick(context.Background())

	if got := len(fx.Tracker.MarkedHandoffCommentsOf(node, "")); got != 0 {
		t.Errorf("人間が付けた担当の issue へ持ち回りのコメントを書いている: %d 件", got)
	}
	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	if len(issue.Assignees) != 1 || issue.Assignees[0].Login != rivalLogin {
		t.Errorf("人間が付けた担当を取り上げている: %+v", issue.Assignees)
	}
}

// {"RUCM-PATH": "P033"}
//
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
func Test_issueの担当を入札で決める_P033_他の担当者のholdでは人間の担当を外さない(t *testing.T) {
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

// {"RUCM-PATH": "P033"}
//
// 目的: 案内を issue へ書くのは、3巡回目かつ最初に止めてから60秒たったあとの1回だけであることを
// 確かめる（#140（人間が担当者で着手できないことを、issue のコメントとして1回だけ書く））。
//
// **1回目では書かない。**人間が担当者を付け替えている最中の1巡回で書くと、
// 数秒で解消する状態に永久に残るコメントを1件足すことになる。
//
// 与える情報: 人間が担当者になっている issue 1件と、1巡回ごとに30秒進む時計。
// 成功条件: 1巡回目と2巡回目では0件、3巡回目で1件、そのあと何度まわしても1件のままであること。
func Test_issueの担当を入札で決める_P033_案内は3巡回目に1回だけ書く(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin))
	fx.AllowLog("担当者が付いているので着手しません")
	node := issueNode(188)

	tickN(fx, clock, 2, 30*time.Second)
	if got := len(gatedComments(fx, node)); got != 0 {
		t.Fatalf("2巡回目までに書いている: %d 件", got)
	}

	tickN(fx, clock, 1, 30*time.Second)
	bodies := gatedComments(fx, node)
	if len(bodies) != 1 {
		t.Fatalf("3巡回目で1件書いていない: %d 件", len(bodies))
	}
	if !strings.Contains(bodies[0], "<!-- continuo:gated:human_assigned -->") {
		t.Errorf("理由の印が入っていない: %q", bodies[0])
	}
	if strings.Contains(bodies[0], humanLogin) {
		t.Errorf("担当者の名前を書いている（設計 8-1 が禁じている）: %q", bodies[0])
	}

	tickN(fx, clock, 5, 30*time.Second)
	if got := len(gatedComments(fx, node)); got != 1 {
		t.Errorf("巡回のたびに積んでいる: %d 件", got)
	}

	v, ok := gateViewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatal("記録が消えている")
	}
	if !v.Noticed {
		t.Error("書いたのに「まだ書いていない」ことになっている")
	}
}

// {"RUCM-PATH": "P038"}
//
// Test_issueの担当を入札で決める_P038_担当者が2人以上なら触らない は、設計 3-77b の表の1行を確かめる。
//
// 目的: 担当者が2人以上いるのは人間が触っている合図なので、continuo は触らず WARN を出す。
// 与える情報: 担当者が2人いる issue。
// 成功条件: 持ち回りのコメントが1件も増えず、担当者が2人のままであること。
func Test_issueの担当を入札で決める_P038_担当者が2人以上なら触らない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.AllowLog("担当者が2人以上いるので触りません")
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", testGHLogin, rivalLogin))

	fx.Orc.Tick(context.Background())

	if got := len(fx.Tracker.MarkedHandoffCommentsOf(issueNode(188), "")); got != 0 {
		t.Errorf("担当者が2人いる issue へ書き込んでいる: %d 件", got)
	}
	issue, _ := fx.Tracker.IssueByID("PVTI_item188")
	if len(issue.Assignees) != 2 {
		t.Errorf("担当者の数が変わっている: %+v", issue.Assignees)
	}
}

// {"RUCM-PATH": "P038"}
//
// 目的: 担当者が2人以上で、そこに gh の持ち主が混じっていないときは、
// 案内も記録も作ることを確かめる（#136（担当者が2人以上いる issue も、着手できないことを知らせる））。
//
// 与える情報: 人間2人が担当者になっている issue 1件。
// 成功条件: 理由が `many_assignees` で、3巡回目に案内が1件書かれること。
func Test_issueの担当を入札で決める_P038_担当者が2人以上なら人間だけのときに案内する(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin, anotherHumanLogin))
	fx.AllowLog("担当者が2人以上いるので触りません")

	tickN(fx, clock, 3, 30*time.Second)

	v, ok := gateViewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatalf("着手できずに止まっているものに出ていない: %+v", fx.Orc.GateViews())
	}
	if v.Reason != orchestrator.GateReasonManyAssignees {
		t.Errorf("理由が違う: got %q, want %q", v.Reason, orchestrator.GateReasonManyAssignees)
	}
	bodies := gatedComments(fx, issueNode(188))
	if len(bodies) != 1 {
		t.Fatalf("案内を1件書いていない: %d 件", len(bodies))
	}
	if !strings.Contains(bodies[0], "<!-- continuo:gated:many_assignees -->") {
		t.Errorf("理由の印が入っていない: %q", bodies[0])
	}
}

// {"RUCM-PATH": "P038"}
//
// 目的: 担当者が2人以上で、そこに gh の持ち主が混じっているときは、
// **issue へは書かず、記録は作る**ことを確かめる（設計 8-3）。
//
// **この状態がいちばん切り分けが難しい。**この分岐は hold のコメントを1行も読まないので、
// 「人間が2人」と「人間1人＋別の機械が hold を持っている」を区別できない。
// **後者で「担当者をすべて外してください」と案内すると、走っている別の機械の担当が外れ、
// 次の巡回で同じ issue に2台が乗る。**
// **だからといってダッシュボードからも消すと、人間の手がかりが WARN の1行だけになる。**
//
// 与える情報: 人間1人と gh の持ち主が担当者になっている issue 1件。
// 成功条件: 案内が0件で、写しの理由が `many_assignees_with_self`、
// `NoticeSkip` が `unclear_owner` になっていること。
func Test_issueの担当を入札で決める_P038_担当者にghの持ち主が混じっていたら書かずに記録だけ残す(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	holdPrompt(fx)
	fx.Tracker.AddIssue(assignedIssue(188, "In Progress", humanLogin, testGHLogin))
	fx.AllowLog("担当者が2人以上いるので触りません")

	tickN(fx, clock, 5, 30*time.Second)

	if got := len(gatedComments(fx, issueNode(188))); got != 0 {
		t.Errorf("切り分けられないのに issue へ書いている: %d 件", got)
	}
	v, ok := gateViewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatalf("ダッシュボードから消えている（いちばん切り分けの難しい状態が読めなくなる）: %+v",
			fx.Orc.GateViews())
	}
	if v.Reason != orchestrator.GateReasonManyAssigneesWithSelf {
		t.Errorf("理由が違う: got %q, want %q", v.Reason, orchestrator.GateReasonManyAssigneesWithSelf)
	}
	if v.Noticed {
		t.Error("書いていないのに「書いた」ことになっている")
	}
	if v.NoticeSkip != orchestrator.GateNoticeUnclearOwner {
		t.Errorf("書かない理由が違う: got %q, want %q", v.NoticeSkip, orchestrator.GateNoticeUnclearOwner)
	}
}
