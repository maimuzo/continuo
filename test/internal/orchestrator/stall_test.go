package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// stallTimeout はこのファイルのテストで使う「画面が変わらないまま待てる時間」である
// （`claude.turn_timeout_ms`。fake の時計で進める）。
const stallTimeout = 60 * time.Second

// TestCheckStalls_画面の版が増えている間は何時間かかっても打ち切らない は、
// 打ち切りの物差しが turn の総実行時間ではないことを確かめる。
//
// 目的: `SPEC.md` 10.6 の "maximum silence interval while a turn stream is active; each
// app-server output resets it, so it is not a total turn runtime cap"（turn の流れが動いて
// いる間の最大の沈黙の間隔。app-server の出力ごとにリセットされる。総実行時間の上限では
// ない）を、continuo では **herdr の pane の `revision`（画面の版）** で測っていることを示す。
//
// 与える情報: hook を1件も出さず、`agent_status` も `working` のまま変わらない run。
// **画面の版だけが巡回のたびに増える。**閾値を200回またぐ（3時間20分ぶん）。
// 成功条件: 一度も打ち切られない（リトライが積まれず、pane も閉じられない）。
//
// **実時間はゼロである。**`testing/synctest` の bubble の中で時計を進める。
func TestCheckStalls_画面の版が増えている間は何時間かかっても打ち切らない(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newStubFixture(t, stubFixtureOptions{
			AgentStatus: herdr.AgentStatusWorking,
			Mutate: func(cfg *config.Config) {
				cfg.Claude.TurnTimeoutMs = int(stallTimeout / time.Millisecond)
			},
		})
		adoptRun(fx, 188)

		// 閾値を200回またぐ（60秒 × 201 ≒ 3時間21分）。
		// **そのたびに画面の版が1つ増える。**
		const rounds = 200
		for i := range rounds {
			fx.Herdr.BumpRevision()
			time.Sleep(stallTimeout + time.Second)
			fx.Orc.Tick(context.Background())
			synctest.Wait()

			v, ok := viewOf(fx, "maimuzo/koetsumugi#188")
			if !ok {
				t.Fatalf("画面が変わり続けている run を印から外した（%d 周目）", i+1)
			}
			if v.RetryCount != 0 {
				t.Fatalf("画面の版が増えているのに打ち切った（%d 周目）: retry_count = %d", i+1, v.RetryCount)
			}
		}
		if ids := fx.Herdr.ClosedPanes(); len(ids) != 0 {
			t.Fatalf("画面が変わり続けている run の pane を閉じた: %v", ids)
		}
	})
}

// TestCheckStalls_画面の版が止まったまま閾値を超えたら打ち切る は、打ち切りの条件を確かめる。
//
// 目的: 設計 3-21 の「版が `claude.turn_timeout_ms` のあいだ増えなければ打ち切る」を示す。
// **猶予は与えない。**`agent_status` が `working` でも、画面が変わっていなければ止まっている。
//
// 与える情報: 画面の版が一度も増えない run（`agent_status` は `working` のまま）。
// 成功条件: 最初に閾値をまたいだ巡回でリトライが1つ積まれ、pane が閉じられる。
//
// **実時間はゼロである。**
func TestCheckStalls_画面の版が止まったまま閾値を超えたら打ち切る(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newStubFixture(t, stubFixtureOptions{
			AgentStatus: herdr.AgentStatusWorking,
			Mutate: func(cfg *config.Config) {
				cfg.Claude.TurnTimeoutMs = int(stallTimeout / time.Millisecond)
			},
		})
		adoptRun(fx, 188)

		// 閾値の手前では打ち切らない。
		time.Sleep(stallTimeout - time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		v, ok := viewOf(fx, "maimuzo/koetsumugi#188")
		if !ok {
			t.Fatalf("閾値の手前で run を印から外している")
		}
		if v.RetryCount != 0 {
			t.Fatalf("閾値の手前で打ち切っている: retry_count = %d", v.RetryCount)
		}

		// 閾値をまたいだら打ち切る。**猶予を1回与えて待ち直してはならない。**
		time.Sleep(2 * time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		v, ok = viewOf(fx, "maimuzo/koetsumugi#188")
		if !ok {
			t.Fatalf("バックオフ中の run を印から外している（30秒後の巡回で即座に拾い直される）")
		}
		if v.RetryCount != 1 {
			t.Fatalf("画面の版が止まったまま閾値を超えたのにリトライを積んでいない: retry_count = %d", v.RetryCount)
		}
		if v.BackoffUntil.IsZero() {
			t.Fatalf("バックオフの期限を入れていない")
		}
		if len(fx.Herdr.ClosedPanes()) == 0 {
			t.Fatalf("stall で止めたのに pane を閉じていない（pane.close が唯一の手段である）")
		}
	})
}

// TestCheckStalls_PreToolUseで時計がリセットされる は、生きていることの確認を確かめる。
//
// 目的: 設計 3-21 の「`PreToolUse` と `PostToolUse` を全ツールに張る。届くたびに時計を
// リセットする。**turn の終わりの判定には使わない。生きていることの確認だけに使う**」を示す。
//
// 与える情報: 閾値の手前で `PreToolUse` が1件届き、そのあと閾値未満だけ時間が進む。
// 成功条件: stall と判定されない（リトライが積まれない）。
//
// **実時間はゼロである。**
func TestCheckStalls_PreToolUseで時計がリセットされる(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newStubFixture(t, stubFixtureOptions{
			AgentStatus: herdr.AgentStatusUnknown,
			Mutate: func(cfg *config.Config) {
				cfg.Claude.TurnTimeoutMs = int(stallTimeout / time.Millisecond)
			},
		})
		adoptRun(fx, 188)

		// 閾値の手前で道具の hook が1件届く。
		time.Sleep(stallTimeout - 5*time.Second)
		if !fx.Orc.OnHook(toolHook("session-188", "PreToolUse")) {
			t.Fatalf("PreToolUse を知らない run のものとして捨てた")
		}

		// そこから閾値未満だけ進める。
		time.Sleep(stallTimeout - 5*time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		v, ok := viewOf(fx, "maimuzo/koetsumugi#188")
		if !ok {
			t.Fatalf("時計がリセットされていれば止まらないはずの run が印から外れている")
		}
		if v.RetryCount != 0 {
			t.Fatalf("PreToolUse で時計がリセットされていない: retry_count = %d", v.RetryCount)
		}
	})
}

// TestResumeBackoff_バックオフが明けた run を巡回の先頭で拾い直す は、再 dispatch を確かめる。
//
// 目的: 設計 3-21 / 3-25 の「巡回の先頭で印の集合を走査する。`BackoffUntil` を過ぎている
// → その run を再 dispatch する。**候補の取得より前に走査する**（空きスロットの計算に効く）」を示す。
//
// 与える情報: stall でリトライを積んだ run。バックオフが明けるまで時計を進める。
// 成功条件: 再 dispatch が走る（Status が `running_state` へ書き直される）。
//
// **セッション UUID の採り直しと、送る本文が1回目の本文（5-3）になることは、
// [TestResumeBackoff_再dispatchはUUIDを採り直して1回目の本文を送る] が確かめる。**
// このテストは通信を1本も行わない stub を使うので、`agent.start` の引数を観測できない。
//
// **実時間はゼロである。**
func TestResumeBackoff_バックオフが明けたrunを巡回の先頭で拾い直す(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newStubFixture(t, stubFixtureOptions{
			AgentStatus: herdr.AgentStatusUnknown,
			Mutate: func(cfg *config.Config) {
				cfg.Claude.TurnTimeoutMs = int(stallTimeout / time.Millisecond)
				cfg.Agent.MaxRetryBackoffMs = 10000
				// 信頼の検査は helpers_test.go のテスト用socket mock側で確かめている。
				// ここで見たいのは「バックオフが明けたら段0 から入り直す」ことだけである。
				cfg.Trust.RequireRepoTrusted = false
				cfg.Tracker.VerifyStatesEvery = 0
			},
		})
		adoptRun(fx, 188)

		time.Sleep(stallTimeout + time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		v, ok := viewOf(fx, "maimuzo/koetsumugi#188")
		if !ok || v.RetryCount != 1 {
			t.Fatalf("stall でリトライが積まれていない: %+v (ok=%v)", v, ok)
		}
		before := fx.Tracker.CountCall("UpdateStatus")

		// バックオフが明けるまで進める。
		time.Sleep(20 * time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		if fx.Tracker.CountCall("UpdateStatus") <= before {
			t.Fatalf("バックオフが明けたのに再 dispatch していない: %v", fx.Tracker.Calls())
		}
	})
}

// TestCheckStalls_打ち切りの文面は原因と対処を必ず書く は、設計 3-34b の文面の形を確かめる。
//
// 目的: 「何が起きたか →【確かめ方】→【よくある原因】→【対処】」の4つを必ず含め、
// **案内する設定キーとコマンドが実在する**ことを示す。**存在しないキーを案内した事故が
// 実際に起きている**（設計 3-34b の表）。
//
// 与える情報: 画面の版が増えないまま閾値を超え、**リトライの回数が尽きている** run
// （`agent.max_retries` が 0 なので1回目の打ち切りで人間へ渡す）。worktree のパスを持たせる。
// 成功条件: issue に付いた引き渡しの通知が、4つの見出しと `claude.turn_timeout_ms` の
// 現在値と、そのままコピーして叩ける `git -C` のコマンドを含むこと。
//
// **実時間はゼロである。**
func TestCheckStalls_打ち切りの文面は原因と対処を必ず書く(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := newStubFixture(t, stubFixtureOptions{
			AgentStatus: herdr.AgentStatusWorking,
			Mutate: func(cfg *config.Config) {
				cfg.Claude.TurnTimeoutMs = int(stallTimeout / time.Millisecond)
				cfg.Agent.MaxRetries = 0
			},
		})
		const worktreePath = "/tmp/continuo-test/koetsumugi/188"
		issue := sampleIssue(188, "In Progress")
		fx.Tracker.AddIssue(issue)
		fx.Orc.Adopt(issue, orchestrator.AdoptedRun{
			AgentName:        normalize.SafeName("continuo-koetsumugi-188"),
			PaneID:           "w1:p1",
			SessionUUID:      "session-188",
			HerdrWorkspaceID: "w1",
			WorktreePath:     worktreePath,
		}, false)

		time.Sleep(stallTimeout + time.Second)
		fx.Orc.Tick(context.Background())
		synctest.Wait()

		comments := fx.Tracker.CommentsOf("I_node188")
		if len(comments) != 1 {
			t.Fatalf("引き渡しの通知が1件だけ付いていない: %d 件", len(comments))
		}
		body := comments[0].Body
		for _, want := range []string{
			"【確かめ方】",
			"【よくある原因】",
			"【対処】",
			"`claude.turn_timeout_ms`",
			"60000 ミリ秒",
			"git -C \"" + worktreePath + "\" status",
			"turn の総実行時間の上限ではありません",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("打ち切りの文面に %q が無い:\n%s", want, body)
			}
		}
		// **`herdr agent read` を案内してはならない**（コメントの直後に pane を閉じるので、
		// 人間が読むときには agent が消えている。設計 3-34b）。
		if strings.Contains(body, "herdr agent read") {
			t.Errorf("消えている agent の読み取りを案内している:\n%s", body)
		}
	})
}
