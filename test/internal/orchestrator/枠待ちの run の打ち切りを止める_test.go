// {"RUCM-CFG-SHA256": "1f611f184a9169ba0e8bdbb782e7f1aa04e131eadd5042aeca078cdfcd877f8a", "SOURCE": "docs/spec/usecases/particular_case/枠待ちの run の打ち切りを止める.cfg.json"}
//
// **ユースケース記述「枠待ちの run の打ち切りを止める」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package orchestrator_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/ratelimit"
)

// {"RUCM-PATH": "P006"}
//
// TestQuota_100パーセントかつhookが来ていない run だけを枠待ちにする は、
// 枠待ちの判定が2条件の連言であることを確かめる。
//
// 目的: 設計 3-27 の「**この run は枠待ちである**は次の2つが同時に成り立つとき。
// 条件その1: `percent` が 100 に達している。条件その2: その run から `claude.turn_timeout_ms` の
// あいだ hook が1件も来ていない」と、「**`severity` は見ない**」を守っていることを示す。
//
// **条件その2 を入れる理由。**枠を使い切っていても、別の run は動いていることがある。
// 枠の状態だけで全部の run の時計を止めると、固まった run を見逃す。
//
// 与える情報: ステータスラインから届いた5時間の期間の使用率が100%（issue #284）。
// hook が来ていない run と、閾値の手前で hook を受けた run。
// 成功条件: 前者だけが枠待ちになり、時計が止まる。後者は枠待ちにならない。
func Test_枠待ちのrunの打ち切りを止める_P006_100パーセントかつhookが来ていないrunだけを枠待ちにする(t *testing.T) {
	fx := newStubFixture(t, stubFixtureOptions{
		AgentStatus: herdr.AgentStatusUnknown,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.TurnTimeoutMs = 50
			cfg.RateLimit.Source = ratelimit.SourceStatusline
		},
	})
	t.Cleanup(fx.Close)
	// **回復待ちと閾値は新しさを問わない**ので、初めて見るセッションの1行で足りる。
	fx.Orc.OnStatusline(slLine("pane-a", 100, slWin(100, time.Now().Add(2*time.Hour)), nil))
	adoptRun(fx, 188)
	adoptRun(fx, 189)

	// 閾値を跨がせる。**189 だけは直前に hook を受けているので時計が新しい。**
	time.Sleep(120 * time.Millisecond)
	fx.Orc.OnHook(toolHook("session-189", "PostToolUse"))

	fx.Orc.Tick(context.Background())

	v188, ok := viewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatalf("枠待ちにすべき run が印から外れている")
	}
	if !v188.WaitingQuota {
		t.Fatalf("枠が100%%で hook も来ていないのに枠待ちにしていない: %+v", v188)
	}
	if v188.RetryCount != 0 {
		t.Fatalf("枠待ちの run を stall として殺している: retry_count = %d", v188.RetryCount)
	}

	v189, ok := viewOf(fx, "octocat/hello-world#189")
	if !ok {
		t.Fatalf("hook を受けている run が印から外れている")
	}
	if v189.WaitingQuota {
		t.Fatalf("hook が来ている run まで枠待ちにしている（固まった run を見逃す）: %+v", v189)
	}
}

// {"RUCM-PATH": "P003"}
//
// Test_枠待ちのrunの打ち切りを止める_P003_枠を使い切っていなければ待ち直さない は、枠待ちの条件その1 を確かめる。
//
// **枠待ちの判定は2つの条件をどちらも満たすときだけ立つ**（設計 3-27）。
// **1つ目は「使い切っている枠が1つでもあること」。**
// 使い切っていないのに待ち直すと、動いていない run を永久に抱える。
//
// 目的: 枠に余裕があるとき、枠待ちにしないこと。
// 与える情報: ステータスラインから届いた使用率 50%（issue #284）と、hook を1件も受けていない run。
// 成功条件: 枠待ちの印が立たないこと。
func Test_枠待ちのrunの打ち切りを止める_P003_枠を使い切っていなければ待ち直さない(t *testing.T) {
	fx := newStubFixture(t, stubFixtureOptions{
		AgentStatus: herdr.AgentStatusUnknown,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.TurnTimeoutMs = 50
			cfg.RateLimit.Source = ratelimit.SourceStatusline
		},
	})
	t.Cleanup(fx.Close)
	feedFreshQuota(fx.Orc, "pane-a", time.Now(), 50, 10)
	adoptRun(fx, 188)

	time.Sleep(120 * time.Millisecond)
	fx.Orc.Tick(context.Background())

	v, ok := viewOf(fx, "octocat/hello-world#188")
	if !ok {
		t.Fatal("run が印から外れている")
	}
	if v.WaitingQuota {
		t.Errorf("枠に余裕があるのに枠待ちにしている: %+v", v)
	}
}

// {"RUCM-PATH": "P009"}
//
// Test_枠待ちのrunの打ち切りを止める_P009_担当が移っていたらafter_runを走らせずに止める は、代替フロー「担当が移っていた」
// （待つ上限を超えたが、担当の確かめで引き返す経路）を検査する（設計 3-27 / 3-77c。issue #197）。
//
// 目的: **枠待ちのあいだ、担当は自分の意思と無関係に外れる。**
// `idle_timeout_ms` は「担当者の最後の進捗報告から」で数え、**枠待ち中は hook が来ないので
// 進捗のコメントも増えない。**
// **3-77c は「担当を外された機械は、その branch へ push してはならない」と決めている。**
// **確かめずに `after_run` を走らせると、利用者が書いた `git push` が別の機械の branch へ飛ぶ。**
//
// 与える情報: 1週間の枠が 100% で、リセットは48時間後。上限は300分。**担当者は別の人である。**
// **`workspace_hooks.after_run` は、走ると目印のファイルを作るコマンドにしてある。**
// 成功条件: 印から外れること。**別の人の担当者が残っていること**（こちらは触らない）。
// **目印のファイルが出来ていないこと**（`after_run` を走らせていない）。
func Test_枠待ちのrunの打ち切りを止める_P009_担当が移っていたらafter_runを走らせずに止める(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	worktree := t.TempDir()
	fx, issue, clock := weeklyWaitFixtureAdopted(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_W7", withAfterRunMarker, adoptedWithWorktree(worktree))

	// **待っているあいだに、別の機械が担当を取っていった。**
	// **`testGHLogin` とは違うアカウントにする。**同じにすると「担当は自分のまま」になる。
	fx.Tracker.SetAssignees(issue.ID, "another-machine")

	waitForRelease(t, fx, clock, issue.Identifier)

	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 1 || got[0] != "another-machine" {
		t.Fatalf("担当が移っているのに担当者へ触っている: %v", got)
	}
	if got := fx.Logs.String(); !strings.Contains(got, "担当が移ったので") {
		t.Fatalf("担当が移ったことを出していない:\n%s", got)
	}
	if got := fx.Logs.String(); strings.Contains(got, "担当を手放しました") {
		t.Fatalf("担当が移っているのに手放しの経路を通っている:\n%s", got)
	}
	// **`after_run` を走らせていないこと**（設計 3-77c。担当を外された機械は push してはならない）。
	if _, err := os.Stat(filepath.Join(worktree, afterRunMarker)); err == nil {
		t.Fatalf("担当が移っているのに workspace_hooks.after_run を走らせている:\n%s", fx.Logs.String())
	}
	// **この経路の段に「pane を閉じる」がある**（RUCM の `担当が移っていた` の、pane を閉じる段。
	// 実装レビュー3周目の MEDIUM）。
	//
	// **この検査は `leaveDirectChatMode` を守っていない**（実装レビュー4周目の MEDIUM。実測）。
	// **この一式は `tracker.direct_chat_state` を設定しないので、`DirectChatMode` が偽である。**
	// **`stopWorker` の門は最初から当たらない。**確かめているのは、
	// **ふつうの場合（direct chat に入っていない run）に pane が閉じることだけである。**
	if ids := fx.Herdr.ClosedPanes(); len(ids) == 0 {
		t.Fatalf("担当が移った run の pane を閉じていない:\n%s", fx.Logs.String())
	}
}

// {"RUCM-PATH": "P010"}
//
// Test_枠待ちのrunの打ち切りを止める_P010_1週間の枠のリセットが上限より先なら担当を手放す は、#197 の本体を確かめる
// （時刻で測る側）。
//
// 目的: **1週間の枠は最長で7日先までリセットされない。**待つ上限を設けないと、
// その issue を抱えたまま何日も止まる。
//
// 与える情報: 1週間の枠が 100% で、リセットは48時間後。上限は300分（5時間）。
// 成功条件: 印から外れ（スロットが空き）、担当者が空になること。
func Test_枠待ちのrunの打ち切りを止める_P010_1週間の枠のリセットが上限より先なら担当を手放す(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_W1")

	tickOnce(fx)
	waitForRelease(t, fx, clock, issue.Identifier)

	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 0 {
		t.Fatalf("担当者が残っている: %v", got)
	}
	// **結果の1行が既定の水準で出ること。**
	// 「上限を超えたので手放します」は `Debug` である（手放せずに戻る経路が毎巡回で通るため）。
	if got := fx.Logs.String(); !strings.Contains(got, "担当を手放しました") {
		t.Fatalf("手放したことを出していない:\n%s", got)
	}
	// **`after_run` を設定していないので、push できたことを確かめられなかった、と書くこと**
	// （理由は `weekly_wait_limit_no_push`）。**「実行済みです」と書くと、次に拾う機械が
	// remote の続きから始められると読んでしまう。**
	body := releasedBodyOf(fx, issue)
	if !strings.Contains(body, "push できたことを確かめられませんでした") || strings.Contains(body, "実行済みです") {
		t.Fatalf("after_run を走らせていないのに、released の本文がそう言っていない:\n%s", body)
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_枠待ちのrunの打ち切りを止める_P007_担当を確かめられないうちは手放さない は、代替フロー「手放さずに待ち続ける」を確かめる
// （設計 3-27 の段0a。issue #197）。
//
// 目的: **段0a の答えは3つある。「はい」「いいえ」「分からない」である。**
// **「分からない」を「はい」へ畳んではならない。**畳むと、issue を1回読めなかっただけで
// **利用者が書いた `git push` が、別の機械の branch へ飛ぶ。**
//
// 与える情報: 1週間の枠が 100% でリセットは48時間後。上限は10分。
// **issue の取り直しが誤りを返す状態**（`FetchIssuesByIDs` が落ちる）。
// 成功条件: 手放さないこと。担当者が変わらず、pane が1つも閉じられず、
// 見送りの1行が出ること。
func Test_枠待ちのrunの打ち切りを止める_P007_担当を確かめられないうちは手放さない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 10, "CONTINUO_TEST_OAUTH_TOKEN_W_UNKNOWN_ASSIGNEE")
	// **issue を取り直せない状態にする。**`mayReleaseOwnWork` は「分からない」を返す。
	fx.Tracker.SetIDsError(errors.New("取り直せません（検査）"))

	for range 10 {
		clock.Advance(2 * time.Minute)
		fx.Orc.Tick(context.Background())
		time.Sleep(50 * time.Millisecond)
		if _, ok := viewOf(fx, issue.Identifier); !ok {
			t.Fatalf("担当を確かめられないのに印から外した:\n%s", fx.Logs.String())
		}
	}

	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 1 || got[0] != testGHLogin {
		t.Fatalf("担当を確かめられないのに担当者を書き換えた: %v", got)
	}
	if ids := fx.Herdr.ClosedPanes(); len(ids) != 0 {
		t.Fatalf("担当を確かめられないのに pane を閉じた: %v", ids)
	}
	if got := fx.Logs.String(); !strings.Contains(got, "いまの担当を確かめられないので見送ります") {
		t.Fatalf("見送った理由を出していない:\n%s", got)
	}
}

// {"RUCM-PATH": "P010"}
//
// Test_枠待ちのrunの打ち切りを止める_P010_92パーセントでも打ち切られずに手放される は、90〜99%の帯を確かめる
// （issue #173 / #197）。
//
// 目的: **枠待ちの印は使用率100でしか立たない。**
// **入札と手放しの線を余裕値へ移したので、92% の run は「手放しの対象」だが「枠待ちの印」は立たない。**
// **そのまま打ち切りの本体まで落ちると、`revision` は動かず無音の閾値も超えているので、
// 打ち切りが先に殺す。****手放しは2回続けて同じ連番を見る必要があるため、1回目は必ず「まだ」と答える。**
// **つまり打ち切りが毎回勝ち、線を移した意味が既定の設定で丸ごと消える。**
//
// 与える情報: 1週間の枠が **92%**（余裕値は 100−92−10 = −2 で0以下。**ただし100ではない**）。
// リセットは48時間後（上限を超える）。
// 成功条件: **打ち切られずに、担当を手放すこと。**`failure_state` へ落ちないこと。
func Test_枠待ちのrunの打ち切りを止める_P010_92パーセントでも打ち切られずに手放される(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 92, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_W9")

	waitForRelease(t, fx, clock, issue.Identifier)

	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 0 {
		t.Fatalf("担当者が残っている: %v", got)
	}
	// **打ち切りの文面が出ていないこと。**出ていれば、打ち切りが先に勝っている。
	if got := fx.Logs.String(); strings.Contains(got, "止まったものと判断して打ち切りました") {
		t.Fatalf("手放しの対象なのに打ち切っている:\n%s", got)
	}
	if got := fx.Logs.String(); !strings.Contains(got, "担当を手放しました") {
		t.Fatalf("手放していない:\n%s", got)
	}
}

// {"RUCM-PATH": "P005"}
//
// Test_枠待ちのrunの打ち切りを止める_P005_workingなら枠待ちと判定しない は、stall の評価順を確かめる
// （設計 3-27。issue #197）。
//
// 目的: **枠待ちの条件は「使用率が100」と「hook が来ていない」の2つで、
// 「枠を待っている」と「長い1つの仕事をしている」を区別できない。**
// hook はツールが終わってから飛ぶので、**1時間を超える1回のツール呼び出しの最中は1件も来ない。**
// **そこへ1週間のモデル別の枠が100%だと条件が両方そろい、正常に走っている run が枠待ちと名乗る。**
// **stall の時計が止まったまま戻らないので、そのあと本当に固まっても誰も止められない。**
//
// **専用の仕組みは持たない。**`checkStalls` の評価順で、`agent_status` を枠待ちの判定より前に置く。
//
// 与える情報: 1週間のモデル別の枠が 100% で、リセットは48時間後。上限は300分。**`agent_status` が `working` である。**
// 成功条件: 枠待ちと判定しないこと。印から外れないこと。担当者が残っていること。
//
// **CFG のパスに対応づけない。**この判定は基本フローの stall の評価順であり、
// 代替フローではない。
func Test_枠待ちのrunの打ち切りを止める_P005_workingなら枠待ちと判定しない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, _ := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_scoped", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_W6")

	// **エージェントは長い1つのツール呼び出しの最中である。**
	// **枠待ちと判定される前に `working` にする。**判定してからでは、標識が立った run は
	// 次の巡回で状態を見に行かない（枠が明けたときに標識が外れる）。
	fx.Herdr.SetStatus(herdr.AgentStatusWorking)

	fx.Orc.Tick(context.Background())
	// **`working` の run は、手放しの対象にならない**（`paneStopped` が偽を返す。issue #173）。
	// **打ち切りの本体まで落ち、そこで `working` を読んで待ち続ける。**
	// **確かめるのは、打ち切られていないことそのものである。**
	if got := fx.Logs.String(); strings.Contains(got, "止まったものと判断して打ち切りました") {
		t.Fatalf("working なのに打ち切っている:\n%s", got)
	}

	if _, ok := viewOf(fx, issue.Identifier); !ok {
		t.Fatalf("working なのに印から外している")
	}
	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 1 || got[0] != testGHLogin {
		t.Fatalf("担当者が変わっている: %v", got)
	}
	if got := fx.Logs.String(); strings.Contains(got, "枠待ちと判定したので") {
		t.Fatalf("画面が動いているのに枠待ちと判定している:\n%s", got)
	}
}

// {"RUCM-PATH": "P004"}
//
// Test_枠待ちのrunの打ち切りを止める_P004_手放せないrunは打ち切りに任せる は、打ち切りを飛ばす範囲を確かめる
// （issue #173）。
//
// 目的: **手放しの対象を打ち切りから守る門を足したが、守る範囲を広げすぎてはならない。**
// **`agent.get` を読めない run と、`blocked` / `unknown` の run は、
// 手放しの経路では二度と進まない**（`paneStopped` が `idle` と `done` しか通さない）。
// そこを打ち切りからも守ると、**止める者が1人もいなくなる。**
// **turn ループは総実行時間では打ち切らない**ので、
// **その run は pane とスロットを握ったまま、continuo を再起動するまで残る。**
// **枠がいちばん苦しい局面で、最後の安全網が選択的に外れることになる。**
//
// 与える情報: 1週間の枠が **92%**（手放しの条件は満たすが、**枠待ちの印は立たない**）。
// リセットは48時間後。**ただし agent は `unknown` のまま**（＝手放しの経路では進まない）。
// **100% で試してはならない。**そこでは枠待ちの印が立ち、打ち切りが止まるのが元からの正しい振る舞いである。
// **この検査が見たいのは、印が立たない帯で、私が足した門が打ち切りを止めていないかである。**
//
// **`working` で試してはならない**（2026-09-08 に前提を入れ替えた。issue #173）。
// **`working` は「進んでいる」の唯一の信号なので、打ち切りの側が意図して見送る**
// （[docs/spec/turn_end_detect_mechanizm.md](../../docs/spec/turn_end_detect_mechanizm.md) の 4-1）。
// **`unknown` は、どちらの経路にも拾われない状態の代表である。**
// 成功条件: **打ち切られること。**握ったまま残らないこと。
func Test_枠待ちのrunの打ち切りを止める_P004_手放せないrunは打ち切りに任せる(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 92, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_W10")

	// **`unknown` のまま固まった run にする。**手放しの経路はここで止まる。
	fx.Herdr.SetStatus(herdr.AgentStatusUnknown)

	for i := 0; i < 60; i++ {
		if _, ok := viewOf(fx, issue.Identifier); !ok {
			return // 打ち切られた（印から外れた）。それでよい
		}
		clock.Advance(2 * time.Minute)
		fx.Orc.Tick(context.Background())
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("手放せない run が打ち切られずに残っています:\n%s", fx.Logs.String())
}

// {"RUCM-PATH": "P010"}
//
// Test_枠待ちのrunの打ち切りを止める_P010_after_runを設定していれば走らせてから担当を手放す は、#197 の引き継ぎの本体を確かめる
// （設計 3-27 / 3-77c）。
//
// 目的: **手放す前に、利用者が設定した後始末（たとえば `git push`）を走らせること。**
// 走らせずに担当を外すと、次に拾う機械は remote から作り直すので、push していない commit が渡らない。
// **走ったなら、`released` の本文は「実行済みです」と書くこと**（理由は `weekly_wait_limit`）。
//
// 与える情報: 1週間の枠が 100% で、リセットは48時間後。上限は300分。
// `workspace_hooks.after_run` は、走ると目印のファイルを作るコマンド。run は worktree のパスを持つ。
// 成功条件: 目印のファイルが出来ていて、担当者が空になり、`released` の本文が「実行済みです」と書くこと。
func Test_枠待ちのrunの打ち切りを止める_P010_after_runを設定していれば走らせてから担当を手放す(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	worktree := t.TempDir()
	fx, issue, clock := weeklyWaitFixtureAdopted(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_W11", withAfterRunMarker, adoptedWithWorktree(worktree))

	tickOnce(fx)
	waitForRelease(t, fx, clock, issue.Identifier)

	if _, err := os.Stat(filepath.Join(worktree, afterRunMarker)); err != nil {
		t.Fatalf("workspace_hooks.after_run を走らせていない: %v\n%s", err, fx.Logs.String())
	}
	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 0 {
		t.Fatalf("担当者が残っている: %v", got)
	}
	body := releasedBodyOf(fx, issue)
	if !strings.Contains(body, "実行済みです") || strings.Contains(body, "push できたことを確かめられませんでした") {
		t.Fatalf("after_run を走らせたのに、released の本文がそう言っていない:\n%s", body)
	}
}

// {"RUCM-PATH": "P003"}
//
// Test_枠待ちのrunの打ち切りを止める_P003_枠を読めなければ枠待ちにせず打ち切る は、代替フロー「枠の残り」を検査する。
//
// **枠を読めないときに「枠待ちかもしれない」と待ち続けると、止まった run を永久に抱える。**
// 読めないなら枠の判定は諦め、通常の打ち切りとして扱う。
//
// 目的: 使用率を読めない状態で hook も来なければ、`turn_timeout_ms` で打ち切ること。
// 与える情報: 枠の判定を無効にした設定（`source: none`）と、hook を1件も送らない run。
// 成功条件（RUCM の POSTCONDITION）: pane が閉じられ、**印は残り**、
// Status は `running_state` のままであること（リトライで再開するため）。
func Test_枠待ちのrunの打ち切りを止める_P003_枠を読めなければ枠待ちにせず打ち切る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			// **hook が来なくなったら短い時間で打ち切る。**
			cfg.Claude.TurnTimeoutMs = 1200
		},
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())
	waitFor(t, 15*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	// **hook を1件も送らないまま巡回を回す。**
	waitFor(t, 30*time.Second, "pane が閉じられる", func() bool {
		fx.Orc.Tick(context.Background())
		return fx.Herdr.CountMethod(herdr.MethodPaneClose) > 0
	})

	// **枠明けを待っていない**（枠を読めないので待つ根拠がない）。
	if got := fx.Herdr.CountMethod(herdr.MethodAgentWait); got != 0 {
		t.Errorf("枠を読めないのに枠明けを待っている: agent.wait を %d 回送った", got)
	}
	// **Status は running_state のまま。**リトライが残っているので人間へ渡さない。
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "In Progress" {
		t.Errorf("リトライが残っているのに Status を動かしている: %s", got)
	}
}
