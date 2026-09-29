// {"RUCM-CFG-SHA256": "33fe453c5d236ce82a0ba1ffd08222a6315dc1005f0177e62105f632c319218b", "SOURCE": "docs/spec/usecases/particular_case/レートリミットで待って再開する.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**
//
// **1週間のレートリミットを待つ上限（`rate_limit.weekly_wait_limit_minutes`）と、
// 担当を手放す判定を検査する**（issue #197）。
//
// **手放す前に「本当に止まっているか」を確かめる部分も、ここで検査する**
// （[docs/spec/turn_end_detect_mechanizm.md](../../../docs/spec/turn_end_detect_mechanizm.md) の 4-2）。
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
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/orchestrator"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/tracker"
)

// newUsageServer は Claude の OAuth usage API の代わりに使う偽のサーバを立てる。
//
// **本番の API へは接続しない。**
//
// t: 呼び出し元のテスト。後始末を t.Cleanup に登録する。
// limits: 返す枠の一覧（JSON にそのまま載る形）。
// 戻り値の1つ目: 偽サーバの URL。
// 戻り値の2つ目: 受け取ったリクエストの回数を数えるカウンタ。
func newUsageServer(t *testing.T, limits []map[string]any) (string, *atomic.Int32) {
	t.Helper()
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"limits": limits}); err != nil {
			t.Errorf("偽の usage API が応答を書けません: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &count
}

// newUsageReader は偽の usage API を向いた枠の読み取りを作る。
//
// t: 呼び出し元のテスト。
// endpoint: 偽サーバの URL。
// tokenEnv: トークンを入れた環境変数の名前。
// 戻り値: 組み立てた Reader。
func newUsageReader(t *testing.T, endpoint, tokenEnv string) *ratelimit.Reader {
	t.Helper()
	t.Setenv(tokenEnv, "test-token")
	reader, err := ratelimit.NewReader(ratelimit.Options{
		Config: config.RateLimitConfig{
			Source:         ratelimit.SourceOAuthUsageAPI,
			TokenSource:    ratelimit.TokenSourceEnv,
			TokenEnv:       tokenEnv,
			PollIntervalMs: 1,
		},
		Endpoint: endpoint,
	})
	if err != nil {
		t.Fatalf("ratelimit.NewReader に失敗した: %v", err)
	}
	return reader
}

// weeklyWaitFixture は「1週間の枠を待つ上限」の検査で使う一式を組み立てる（issue #197）。
//
// **担当者はこの機械（gh の持ち主）である。**手放す相手が自分でないと、外す対象が見つからない。
// **枠待ちの条件その2（turn_timeout_ms のあいだ hook が来ていない）は、時計を進めて作る。**
//
// t: 呼び出し元のテスト。
// limits: usage API が返す枠の一覧。
// limitMinutes: `rate_limit.weekly_wait_limit_minutes` に入れる値。
// tokenEnv: トークンを入れる環境変数の名前（テストごとに変える）。
// 戻り値: 組み立てた一式・印へ入れた issue・進められる時計。
func weeklyWaitFixture(
	t *testing.T, limits []map[string]any, limitMinutes int, tokenEnv string,
) (*stubFixture, tracker.Issue, *testClock) {
	t.Helper()
	return weeklyWaitFixtureWith(t, limits, limitMinutes, tokenEnv, nil)
}

// weeklyWaitFixtureWith は weeklyWaitFixture に、設定を足す手立てを付けたものである（issue #197）。
//
// **`weeklyWaitFixture` との違いは `extra` の1点だけである。**direct chat の門の検査は
// `tracker.direct_chat_state` を設定しないと作れないが、**それ以外の条件は1つも変えたくない。**
//
// t: 呼び出し元のテスト。
// limits: usage API が返す枠の一覧。
// limitMinutes: `rate_limit.weekly_wait_limit_minutes` に入れる値。
// tokenEnv: トークンを入れる環境変数の名前（テストごとに変える）。
// extra: 既定の設定を書いたあとに呼ぶ手立て。nil なら何もしない。
// 戻り値: 組み立てた一式・印へ入れた issue・進められる時計。
func weeklyWaitFixtureWith(
	t *testing.T, limits []map[string]any, limitMinutes int, tokenEnv string,
	extra func(*config.Config),
) (*stubFixture, tracker.Issue, *testClock) {
	t.Helper()
	return weeklyWaitFixtureAdopted(t, limits, limitMinutes, tokenEnv, extra, nil)
}

// weeklyWaitFixtureAdopted は weeklyWaitFixtureWith に、印へ入れる内容を差し替える手立てを付けたものである
// （issue #197）。
//
// **`Adopt` は、同じ issue がすでに印に在れば何もしない。**
// **だから、組み立てたあとに呼び直しても差し替わらない。**
// **agent 名を持たない run の検査は、最初からその形で入れる必要がある。**
//
// t: 呼び出し元のテスト。
// limits: usage API が返す枠の一覧。
// limitMinutes: `rate_limit.weekly_wait_limit_minutes` に入れる値。
// tokenEnv: トークンを入れる環境変数の名前（テストごとに変える）。
// extra: 既定の設定を書いたあとに呼ぶ手立て。nil なら何もしない。
// adopted: 印へ入れる内容。nil なら既定（agent 名と pane を持つ run）。
// 戻り値: 組み立てた一式・印へ入れた issue・進められる時計。
func weeklyWaitFixtureAdopted(
	t *testing.T, limits []map[string]any, limitMinutes int, tokenEnv string,
	extra func(*config.Config), adopted *orchestrator.AdoptedRun,
) (*stubFixture, tracker.Issue, *testClock) {
	t.Helper()
	endpoint, _ := newUsageServer(t, limits)
	reader := newUsageReader(t, endpoint, tokenEnv)
	clock := newTestClock()

	fx := newStubFixture(t, stubFixtureOptions{
		// **ログを溜める。**止めた理由が既定の水準で出ることを検査する（issue #173）。
		Logs: true,
		// **「止まっている」を表す状態にする**（issue #197）。
		// **`unknown` では手放さない。**herdr が状態を判定できないという意味であり、
		// **確かめられていないのに pane を閉じて担当を外すことになる。**
		AgentStatus: herdr.AgentStatusIdle,
		RateLimit:   reader,
		Now:         clock.Now,
		Mutate: func(cfg *config.Config) {
			cfg.Claude.TurnTimeoutMs = 60000
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.RateLimit.WeeklyWaitLimitMinutes = limitMinutes
			if extra != nil {
				extra(cfg)
			}
		},
	})

	// **担当者をこの機械にした issue を、印へ入れる。**
	issue := assignedIssue(188, "In Progress", testGHLogin)
	fx.Tracker.AddIssue(issue)
	run := orchestrator.AdoptedRun{
		AgentName:        normalize.SafeName("continuo-hello-world-188"),
		PaneID:           "w1:p1",
		SessionUUID:      "session-188",
		HerdrWorkspaceID: "w1",
	}
	if adopted != nil {
		run = *adopted
	}
	fx.Orc.Adopt(issue, run, false)

	// **枠待ちの条件その2 を満たす**（turn_timeout_ms のあいだ hook が来ていない）。
	clock.Advance(2 * time.Minute)
	return fx, issue, clock
}

// tickOnce は巡回を1回だけ回す（issue #197）。
//
// **1回では手放さない。**連番を初めて見た巡回では「そこからどれだけ止まっていたか」が
// 分からないので、**次の巡回まで待つ**（設計 3-27 の「段0 へ入る前に外すもの」の7行目。
// **段0b ではない。**あちらは「外す相手が決まるか」である）。
// **窓を満たすまで回すのは `waitForRelease` である。**
//
// fx: 対象の一式。
func tickOnce(fx *stubFixture) {
	fx.Orc.Tick(context.Background())
}

// waitForRelease は、担当を手放して印から外れるまで巡回を回す（issue #197）。
//
// **1回の巡回では手放さない。**連番を初めて見た巡回では
// 「そこからどれだけ止まっていたか」が分からないので、**次の巡回まで待つ**
// （設計 3-27 の「段0 へ入る前に外すもの」の7行目）。**手放しは別の goroutine で走る**ので、
// **巡回を止めて待つのではなく、時計を進めながら巡回を回し続ける。**
//
// t: 呼び出し元のテスト。
// fx: 対象の一式。
// clock: 進められる時計。
// identifier: 対象の issue の識別子。
func waitForRelease(t *testing.T, fx *stubFixture, clock *testClock, identifier string) {
	t.Helper()
	for i := 0; i < 60; i++ {
		if _, ok := viewOf(fx, identifier); !ok {
			return
		}
		clock.Advance(2 * time.Minute)
		fx.Orc.Tick(context.Background())
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("担当を手放して印から外れませんでした:\n%s", fx.Logs.String())
}

// assigneeLoginsOf は、いまボードに載っている担当者のログイン名を返す。
//
// fx: 対象の一式。
// id: issue の ID。
// 戻り値: 担当者のログイン名。
func assigneeLoginsOf(fx *stubFixture, id string) []string {
	issue, ok := fx.Tracker.IssueByID(id)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(issue.Assignees))
	for _, a := range issue.Assignees {
		out = append(out, a.Login)
	}
	return out
}

// TestQuota_画面が動いていれば枠待ちと判定しない は、stall の評価順を確かめる
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
func TestQuota_workingなら枠待ちと判定しない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, _ := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_scoped", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_W6")

	// **エージェントは長い1つのツール呼び出しの最中である。**
	// **枠待ちと判定される前に `working` にする。**判定してからでは、標識が立った run は
	// 次の巡回で状態を見に行かない（枠が明けたときに標識が外れる）。
	fx.Herdr.SetStatus(herdr.AgentStatusWorking)

	fx.Orc.Tick(context.Background())
	// **手放しの対象になった run は、打ち切りの本体まで落ちない**（issue #173）。
	// **そのため「agent が working なので待ち続けます」は出ない。**
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

// {"RUCM-PATH": "P009"}
//
// TestQuota_担当が移っていたらafter_runを走らせずに止める は、代替フロー「待つ上限を超えた」の
// 担当の確かめで引き返す枝を検査する（設計 3-27 / 3-77c。issue #197）。
//
// 目的: **枠待ちのあいだ、担当は自分の意思と無関係に外れる。**
// `idle_timeout_ms` は「担当者の最後の進捗報告から」で数え、**枠待ち中は hook が来ないので
// 進捗のコメントも増えない。**
// **3-77c は「担当を外された機械は、その branch へ push してはならない」と決めている。**
// **確かめずに `after_run` を走らせると、利用者が書いた `git push` が別の機械の branch へ飛ぶ。**
//
// 与える情報: 1週間の枠が 100% で、リセットは48時間後。上限は300分。**担当者は別の人である。**
// 成功条件: 印から外れること。**別の人の担当者が残っていること**（こちらは触らない）。
func TestQuota_担当が移っていたらafter_runを走らせずに止める(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_W7")

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
	// **この経路の段に「pane を閉じる」がある**（RUCM の `担当が移っていた` の段3。
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
// TestQuota_1週間の枠のリセットが上限より先なら担当を手放す は、#197 の本体を確かめる
// （時刻で測る側）。
//
// 目的: **1週間の枠は最長で7日先までリセットされない。**待つ上限を設けないと、
// その issue を抱えたまま何日も止まる。
//
// 与える情報: 1週間の枠が 100% で、リセットは48時間後。上限は300分（5時間）。
// 成功条件: 印から外れ（スロットが空き）、担当者が空になること。
func TestQuota_1週間の枠のリセットが上限より先なら担当を手放す(t *testing.T) {
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
}

// TestQuota_人間が引き取っているrunは上限を超えても手放さない は、手放しの門を確かめる
// （設計 3-83。issue #197）。
//
// 目的: **手放しは打ち切りより重い。**`workspace_hooks.after_run`（利用者が書いた `git push`）を
// **人間の書きかけの木で走らせ、**issue の担当者からこの機械を外し、`released` のコメントを1件書き、
// **別の機械の入札を呼ぶ。**打ち切りの側（`checkStalls`）にはこの門が最初から在ったのに、
// **手放しの側には無かった**（実装レビュー1周目の HIGH）。
//
// **人間が pane で黙って読んでいるだけで、この窓に入る。**`claude.turn_timeout_ms` のあいだ
// 指示を送らなければ hook は1件も来ず、`agent_status` は `idle` を返し、`state_change_seq` も動かない。
// **そこへ1週間の枠が100%だと、門が全部開く。**
//
// 与える情報: 1週間の枠が 100% で、リセットは48時間後。上限は300分（5時間）。
// **Status は `tracker.direct_chat_state` の値にする。**
// 成功条件: 印に残り、担当者が変わらず、pane が1つも閉じられず、手放しの1行が出ないこと。
func TestQuota_人間が引き取っているrunは上限を超えても手放さない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixtureWith(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_DC1", func(cfg *config.Config) {
		cfg.Tracker.DirectChatState = humanState
	})
	// **カンバンの選択肢に入れてから Status を動かす**（入れないと候補の一覧へ足されない）。
	fx.Tracker.SetStatusOptions(directChatBoardOptions...)
	fx.Tracker.SetState(issue.ID, humanState)

	// **`waitForRelease` と同じ回数・同じ刻みで回す。**手放す側の検査はこの窓で手放している。
	for range 60 {
		clock.Advance(2 * time.Minute)
		fx.Orc.Tick(context.Background())
		time.Sleep(50 * time.Millisecond)
		if _, ok := viewOf(fx, issue.Identifier); !ok {
			t.Fatalf("人間が引き取っている run を印から外した:\n%s", fx.Logs.String())
		}
	}

	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 1 || got[0] != testGHLogin {
		t.Fatalf("人間が引き取っている run の担当者を書き換えた: %v", got)
	}
	if ids := fx.Herdr.ClosedPanes(); len(ids) != 0 {
		t.Fatalf("人間が話している pane を閉じた: %v", ids)
	}
	if got := fx.Logs.String(); strings.Contains(got, "担当を手放しました") {
		t.Fatalf("人間が引き取っている run を手放した:\n%s", got)
	}
}

// TestQuota_100の枠が1つも無ければ上限は効かず打ち切りが受け持つ は、#197 の境界を確かめる。
//
// 目的: **リセット時刻を読めない枠では、経過で測る枝しか無い**（設計 3-27。issue #197）。
// **その枝の起点は `WeeklyShortSince`（この run が1週間の余裕の無さを最初に見た時刻）で、
// 巡回のたびに控え直される。****控える前に時計を進めても、経過は0のままである。**
//
// **枠待ちの印は門ではない**（実装レビュー3周目の HIGH で名前と説明を直した）。
// **以前この検査は「100 の枠が1つも無ければ上限は効かない」と名乗っていたが、それは実装に無い規則である。**
// 同じファイルの `TestQuota_92パーセントでも打ち切られずに手放される` が、
// **100 の枠を1つも持たない状態で手放すことを確かめている**（人間の決定。2026-09-06）。
//
// 与える情報: 1週間のモデル別の枠が 95%（余裕値は `100 − 95 − 10 = −15` で0以下）。
// **`resets_at` は `null` なので、時刻で測る枝は「分からない」と答える。**上限は10分。
// **巡回のあいだ時計を進めない**（`clock.Advance` はループの外で1回だけ）。
// 成功条件: **担当を手放さないこと。**「担当を手放しました」が1行も出ず、担当者が変わらないこと。
func TestQuota_リセット時刻が読めず経過も溜まっていなければ手放さない(t *testing.T) {
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_scoped", "percent": 95, "resets_at": nil, "severity": "normal"},
	}, 10, "CONTINUO_TEST_OAUTH_TOKEN_W2")

	// **20分進める。**上限（10分）を超えるが、**起点を控える前なので経過は0である。**
	// **このあとのループでは時計を進めない**ので、経過は溜まらない。
	clock.Advance(20 * time.Minute)
	for range 5 {
		fx.Orc.Tick(context.Background())
		time.Sleep(50 * time.Millisecond)
	}

	if got := fx.Logs.String(); strings.Contains(got, "担当を手放しました") {
		t.Fatalf("経過が溜まっていないのに担当を手放した:\n%s", got)
	}
	// **担当者は残る。**
	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 1 || got[0] != testGHLogin {
		t.Fatalf("担当者を触った: %v", got)
	}
}

// {"RUCM-PATH": "P008"}
//
// TestQuota_担当を確かめられないうちは手放さない は、代替フロー「手放さずに待ち続ける」を確かめる
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
func TestQuota_担当を確かめられないうちは手放さない(t *testing.T) {
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

// TestQuota_画面を持っていないrunは手放さない は、設計 3-27 の門の2つ目を確かめる（issue #197）。
//
// 目的: **`agent.get` が届かない run では、止まったかどうかを確かめられない。**
// **確かめられない pane を閉じて担当を外す道は無い。**
// **門を落とすと、`paneStopped` が run の数だけ誤りを返し、巡回のたびにログが積む**
// （issue #173 が読めるようにしようとしているログを埋める）。
//
// 与える情報: 1週間の枠が 100% でリセットは48時間後。上限は10分。**agent 名を持たない run。**
// 成功条件: 手放さないこと。担当者が変わらないこと。
func TestQuota_画面を持っていないrunは手放さない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	// **最初から agent 名を持たない形で印へ入れる。**
	// **`Adopt` は同じ issue が既に在れば何もしないので、あとから差し替えられない。**
	fx, issue, clock := weeklyWaitFixtureAdopted(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 10, "CONTINUO_TEST_OAUTH_TOKEN_W_NOAGENT", nil, &orchestrator.AdoptedRun{
		AgentName:        "",
		PaneID:           "w1:p1",
		SessionUUID:      "session-188",
		HerdrWorkspaceID: "w1",
	})

	for range 10 {
		clock.Advance(2 * time.Minute)
		fx.Orc.Tick(context.Background())
		time.Sleep(50 * time.Millisecond)
	}

	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 1 || got[0] != testGHLogin {
		t.Fatalf("画面を持っていない run の担当者を書き換えた: %v\n%s", got, fx.Logs.String())
	}
	if got := fx.Logs.String(); strings.Contains(got, "担当を手放しました") {
		t.Fatalf("画面を持っていない run を手放した:\n%s", got)
	}
}

// TestQuota_忙しいhookを受けた直後のrunは手放さない は、設計 3-27 の門の7つ目を確かめる
// （issue #197）。
//
// 目的: **この門を落とすと、指示を送った直後の run が「進んでいない」と読まれ、
// `idle` が2回続いた時点で手放される**（turn の開始から2巡回。既定60秒）。
// **別の機械が入札し直し、同じ worktree に2本目の Claude Code が立つ。**
//
// 与える情報: 1週間の枠が 100% でリセットは48時間後。上限は10分。
// **巡回のたびに忙しい hook を1件入れる**（無音が `claude.turn_timeout_ms`（この一式では60秒）に達しない）。
// 成功条件: 手放さないこと。担当者が変わらないこと。
func TestQuota_忙しいhookを受けた直後のrunは手放さない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 10, "CONTINUO_TEST_OAUTH_TOKEN_W_BUSYHOOK")

	for range 10 {
		// **忙しい hook を入れてから、時計を無音の閾値より短く進める。**
		fx.Orc.OnHook(subagentStartEvent("session-188", "", "a1f9f743842d397e1", "Explore"))
		clock.Advance(10 * time.Second)
		fx.Orc.Tick(context.Background())
		time.Sleep(50 * time.Millisecond)
	}

	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 1 || got[0] != testGHLogin {
		t.Fatalf("忙しい hook を受けている run の担当者を書き換えた: %v", got)
	}
	if got := fx.Logs.String(); strings.Contains(got, "担当を手放しました") {
		t.Fatalf("忙しい hook を受けている run を手放した:\n%s", got)
	}
}

// TestQuota_打ち切りを切っている機械では経過が上限を超えるまで手放さない は、
// 設計 3-27 の門の8つ目（床）を確かめる（issue #197）。
//
// 目的: **`claude.turn_timeout_ms` が0以下の機械では、無音の門が2本とも外れる。**
// **代わりに床が効く**——`WeeklyShortSince`（この run が1週間の余裕の無さを最初に見た時刻）からの
// 経過が `weekly_wait_limit_minutes` を超えるまで手放さない。
//
// **この門には冗長な相手が1つも無い**（実装レビュー4周目の MEDIUM）。
// **外すと、turn と turn のあいだで `idle` に見えるだけの健全な run が2巡回（既定60秒）で手放される。**
// **`workspace_hooks.after_run` を書いていない機械では push が走らないので、
// push していない commit が失われる。**
//
// 与える情報: 1週間の枠が 100% で、**リセットは48時間後**（時刻で測る枝は即座に「超えた」と答える）。
// 上限は20分。`claude.turn_timeout_ms` は0。**時計は上限より短く進める。**
// 成功条件: 手放さないこと。**上限を超えるまで進めたら手放すこと**（床が「効かない」のではないことも確かめる）。
func TestQuota_打ち切りを切っている機械では経過が上限を超えるまで手放さない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixtureWith(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 20, "CONTINUO_TEST_OAUTH_TOKEN_W_FLOOR", func(cfg *config.Config) {
		// **打ち切りの判定を切る。**この設定は validate が明示的に許している。
		cfg.Claude.TurnTimeoutMs = 0
	})

	// **起点を控えさせる。**`noteWeeklyShort` は巡回の中で控えるので、1回回す。
	tickOnce(fx)

	// **上限（20分）より短く進める。**床が効いていれば手放さない。
	// **1回の刻みは2分にする。**大きく飛ばすと使用率の写しが古くなり、
	// **手放しの側が「新しさを問う写し」を受け取れずに、床とは別の理由で見送る**
	// （設計 3-27。実装レビュー3周目の HIGH）。**それでは床を確かめられない。**
	for range 5 {
		clock.Advance(2 * time.Minute)
		fx.Orc.Tick(context.Background())
		time.Sleep(50 * time.Millisecond)
	}
	if got := fx.Logs.String(); strings.Contains(got, "担当を手放しました") {
		t.Fatalf("経過が上限を超える前に手放した（床が効いていない）:\n%s", got)
	}
	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 1 || got[0] != testGHLogin {
		t.Fatalf("経過が上限を超える前に担当者を書き換えた: %v", got)
	}

	// **上限を超えるまで進めたら手放す。**床が「永久に手放さない」形ではないことを確かめる。
	waitForRelease(t, fx, clock, issue.Identifier)
	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 0 {
		t.Fatalf("上限を超えたのに担当者が残っている: %v", got)
	}
}

// TestQuota_人間が引き取っている最中に担当が移ったらpaneを閉じる は、
// `stopHandoffLostClaimed` が `leaveDirectChatMode` を先に呼ぶことを確かめる（設計 3-83f・3-83h）。
//
// 目的: **`stopWorker` は direct chat の印が立っていると門で止まり、pane を閉じない。**
// **その run を `release` が印から外すと、人が居る pane が残ったまま continuo がその run を忘れる。**
// **だから、担当が別の機械へ移ったときは、印を下ろしてから閉じる。**
//
// **この検査でも、`leaveDirectChatMode` の1行そのものは守れていない**（4周目に実測した）。
// **カードを作業中の Status へ戻した時点で、巡回の `updateDirectChatMode` が印を下ろす。**
// **だから `releaseBecauseQuotaWaitClaimed` へ着くときには、印はもう立っていない。**
// **印が立ったまま `!mine` の枝へ入るのは、手放しの非同期が走っている最中
// （最大90秒）に人間がカードを動かした窓だけで、その窓を検査から作る手立てが無い。**
// **`leaveDirectChatMode` は、その窓のための保険である。**
// **消しても全部の検査が緑になる**（実測。2026-09-29）。**それでも残す。**
// **消えると、人が居る pane が残ったまま continuo がその run を忘れる。**
//
// **この検査が確かめているのは、`!mine` の枝が
// 「pane を閉じる・印から外す・`after_run` を走らせない・担当者に触らない」を守ることである。**
//
// 与える情報: `tracker.direct_chat_state` を設定し、いったんその Status へ入れてから作業中へ戻した run。
// 1週間の枠が 100% でリセットは48時間後。上限は10分。**担当者を別のアカウントへ書き換える。**
// 成功条件: pane が閉じられ、印から外れること。**`after_run` は走らせず、担当者にも触らないこと。**
func TestQuota_人間が引き取っている最中に担当が移ったらpaneを閉じる(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixtureWith(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 10, "CONTINUO_TEST_OAUTH_TOKEN_W_DC_LOST", func(cfg *config.Config) {
		cfg.Tracker.DirectChatState = humanState
	})
	fx.Tracker.SetStatusOptions(directChatBoardOptions...)
	// **direct chat の印を立てる。**カードをその Status へ動かし、1回巡回を回す。
	fx.Tracker.SetState(issue.ID, humanState)
	tickOnce(fx)
	// **担当を別の機械へ移す。**`mayReleaseOwnWork` は「自分ではない」と答える。
	fx.Tracker.SetAssignees(issue.ID, "octodog")
	// **作業中の Status へ戻す。**巡回の門（`DirectChatMode`）を通さないと、手放しの経路へ入らない。
	// **印はこの巡回で下りるが、`stopWorker` が読むのは `releaseBecauseQuotaWaitClaimed` の中である。**
	fx.Tracker.SetState(issue.ID, fx.Config.Tracker.RunningState)

	for range 60 {
		clock.Advance(2 * time.Minute)
		fx.Orc.Tick(context.Background())
		time.Sleep(50 * time.Millisecond)
		if _, ok := viewOf(fx, issue.Identifier); !ok {
			break
		}
	}

	if _, ok := viewOf(fx, issue.Identifier); ok {
		t.Fatalf("担当が移った run を印から外していない:\n%s", fx.Logs.String())
	}
	if ids := fx.Herdr.ClosedPanes(); len(ids) == 0 {
		t.Fatalf("担当が移った run の pane を閉じていない:\n%s", fx.Logs.String())
	}
	// **担当者には触らない。**
	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 1 || got[0] != "octodog" {
		t.Fatalf("担当が移った run の担当者を触った: %v", got)
	}
	if got := fx.Logs.String(); strings.Contains(got, "担当を手放しました") {
		t.Fatalf("担当が移っているのに手放しの経路を通っている:\n%s", got)
	}
}

// TestQuota_1回目の指示をまだ送り始めていないrunは手放さない は、設計 3-27 の門の5つ目を
// 確かめる（実装レビュー5周目の MEDIUM。**6周目に名前と説明を直した**）。
//
// 目的: **無音の門は、この窓では2本とも開く。**
// 1本目（`LastBusyHookAt`）は、やり直した attempt では**前の attempt の時刻**が残っており、
// それは必ず閾値より古い。新しく着手した run ではゼロ値で、こちらも門を通す。
// 2本目（`runIdleForTurnTimeout`）は、`hookSeenThisTurn` が偽のとき**無条件に真**を返す。
// **残る守りは `paneStopped` の2巡回（既定60秒）だけになる。**
//
// **この門が塞ぐのは、`beginTurn` を通るまでの窓である**（`beginAttempt` から `beginTurn` まで
// とは限らない。`awaitFirst` の周は `beginTurn` を通らないので、印は真のまま残る）。
// **`beginTurn` は `agent.prompt` を投げる前に `SendFirstPrompt` を下ろす**ので、
// **「指示を投げたのに hook が1件も戻らない run」は門の外である。**
// **5周目はそちらも塞いだと書いたが、それは誤りだった**——`beginTurn` を通した状態を作って
// 測ると手放しは起きる（2026-09-29 に測った）。
// **門の外の窓は、設計 3-27 が「通してよい」と決めている。**塞ぐなら先に設計を直すこと。
//
// 与える情報: 1週間の枠が 100% でリセットは48時間後。上限は10分。
// **`beginAttempt` を通して、1回目の指示を送り始める前の状態にした run。**
// 成功条件: 手放さないこと。担当者が変わらず、pane が1つも閉じられないこと。
func TestQuota_1回目の指示をまだ送り始めていないrunは手放さない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 10, "CONTINUO_TEST_OAUTH_TOKEN_W_FIRSTPROMPT")
	// **`Adopt` は2経路とも `SendFirstPrompt` を立てない。**
	// **着手とやり直しの入口を通して、1回目の指示を送り始める前にする。**
	if !fx.Orc.BeginAttemptForTest(issue.ID) {
		t.Fatal("印を持つ run が無い")
	}

	for range 10 {
		clock.Advance(2 * time.Minute)
		fx.Orc.Tick(context.Background())
		time.Sleep(50 * time.Millisecond)
	}

	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 1 || got[0] != testGHLogin {
		t.Fatalf("1回目の指示をまだ送り始めていない run の担当者を書き換えた: %v\n%s", got, fx.Logs.String())
	}
	if ids := fx.Herdr.ClosedPanes(); len(ids) != 0 {
		t.Fatalf("1回目の指示をまだ送り始めていない run の pane を閉じた: %v", ids)
	}
	if got := fx.Logs.String(); strings.Contains(got, "担当を手放しました") {
		t.Fatalf("1回目の指示をまだ送り始めていない run を手放した:\n%s", got)
	}
}

// TestQuota_毎回状態が変わっていたら手放さない は、手放しの2つ目の条件を確かめる
// （issue #173）。
//
// 目的: **`revision`（pane の版）では、この場面を捕まえられなかった。**
// あれは画面を1バイトも見ておらず、herdr が増やすのは端末タイトルの本文が変わったときだけである。
// **continuo の pane ではタイトルが issue の識別子で固定されるので、永久に動かない**
// （2026-09-08 の実測。働いている3つの pane が2分間ずっと `revision: 1` だった）。
// **そのため2つ目の条件は常に真で、判定は実質「`agent_status` を2回読んだ」だけだった。**
// **観測と観測の間に `working` の山が丸ごと入っていても気づけない。**
//
// 与える情報: 1週間の枠が 100% でリセットは48時間後（上限を超える）。
// **巡回のたびに agent の状態が変わったことにする**（`state_change_seq` を増やす）。
// 成功条件: **手放さないこと。**印に残り、担当者も変わらないこと。
//
// **この検査は、実装を `revision` へ戻すと落ちる**（戻すと連番を見ないので、
// 2回目の観測で「変わっていない」と答えて手放す）。
func TestQuota_毎回状態が変わっていたら手放さない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_W7")

	// **手放しのテストと同じだけ巡回を回す。**違いは、巡回のたびに状態が変わることだけである。
	for i := 0; i < 60; i++ {
		if _, ok := viewOf(fx, issue.Identifier); !ok {
			t.Fatalf("状態が変わり続けているのに手放しました（%d 回目の巡回）:\n%s", i, fx.Logs.String())
		}
		fx.Herdr.BumpStateSeq()
		clock.Advance(2 * time.Minute)
		fx.Orc.Tick(context.Background())
		time.Sleep(50 * time.Millisecond)
	}

	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 1 || got[0] != testGHLogin {
		t.Fatalf("担当者が変わっている: %v", got)
	}
}

// TestQuota_連番を返さない版では手放さない は、安全側へ倒すことを確かめる（issue #173）。
//
// 目的: **`state_change_seq` は `omitempty` である。**欄を返さない herdr の版では
// **全 agent が 0 として読まれる。**そのまま比べると「2回続けて同じ」が常に成り立ち、
// **判定は `revision` のときと同じ恒真へ戻る。**
// **恒真へ戻るくらいなら、手放さない側へ倒す。**
//
// 与える情報: 1週間の枠が **92%**（手放しの条件は満たすが、**枠待ちの印は立たない**）。
// リセットは48時間後。**連番は1度も増やさない**（＝欄を返さない版の再現）。
// **100% で試してはならない。**そこでは枠待ちの印が立ち、打ち切りが止まるのが元からの正しい振る舞いである。
// 成功条件: **手放さないこと。ただし打ち切りからも守らないこと。**
func TestQuota_連番を返さない版では手放さない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 92, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_W8")

	// **欄を返さない版の herdr を作る。**
	fx.Herdr.ClearStateSeq()

	for i := 0; i < 60; i++ {
		if _, ok := viewOf(fx, issue.Identifier); !ok {
			// **打ち切られた。それでよい**（issue #173）。
			// **手放しはしない**（連番を読めないので、止まっていると言えない）。
			// **だが打ち切りからも守ってはならない。**守ると止める者が1人もいなくなり、
			// **pane とスロットを握ったまま continuo の再起動まで残る。**
			if got := fx.Logs.String(); strings.Contains(got, "担当を手放しました") {
				t.Fatalf("連番を読めない版なのに手放しました:\n%s", got)
			}
			return
		}
		clock.Advance(2 * time.Minute)
		fx.Orc.Tick(context.Background())
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("連番を読めない run が、手放されも打ち切られもせずに残っています:\n%s", fx.Logs.String())
}

// {"RUCM-PATH": "P010"}
//
// TestQuota_92パーセントでも打ち切られずに手放される は、90〜99%の帯を確かめる
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
func TestQuota_92パーセントでも打ち切られずに手放される(t *testing.T) {
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

// TestQuota_手放せないrunは打ち切りに任せる は、打ち切りを飛ばす範囲を確かめる
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
func TestQuota_手放せないrunは打ち切りに任せる(t *testing.T) {
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

// TestQuota_5時間の枠だけなら上限を超えても待ち続ける は、人間が決めた表の1行目を確かめる。
//
// 目的: **2026-08-26 の決定「5時間枠 → 待つ。担当は変えない」。**
// 5時間の枠は待てば必ず明けるので、担当を動かす必要が無い。
//
// 与える情報: 5時間の枠だけが 100% で、リセットは48時間後（上限をはるかに超える）。
// 成功条件: 印から外れないこと。
func TestQuota_5時間の枠だけなら上限を超えても待ち続ける(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "session", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 300, "CONTINUO_TEST_OAUTH_TOKEN_W3")

	fx.Orc.Tick(context.Background())
	clock.Advance(10 * time.Hour)
	fx.Orc.Tick(context.Background())

	if _, ok := viewOf(fx, issue.Identifier); !ok {
		t.Fatalf("5時間の枠だけなのに担当を手放している:\n%s", fx.Logs.String())
	}
}

// TestQuota_画面の状態を判定できないうちは手放さない は、確かめられないときの倒し方を確かめる
// （設計 3-27。issue #197）。
//
// 目的: **herdr の `unknown` は「agent は居るが状態を判定できない」である。**
// **確かめられていないのに手放してはならない。**この判定の先には GitHub への2回の書き込みと
// pane を閉じる操作があり、**書きかけの編集を持ったまま閉じると、その編集は戻らない。**
//
// 与える情報: 1週間の枠が 100% でリセットは48時間後。上限は10分。
// **`agent_status` は `unknown`。**
// 成功条件: 上限を超えていても手放さないこと。
func TestQuota_画面の状態を判定できないうちは手放さない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 10, "CONTINUO_TEST_OAUTH_TOKEN_W_UNKNOWN")
	fx.Herdr.SetStatus(herdr.AgentStatusUnknown)

	for i := 0; i < 5; i++ {
		clock.Advance(2 * time.Minute)
		fx.Orc.Tick(context.Background())
	}

	if _, ok := viewOf(fx, issue.Identifier); !ok {
		t.Fatalf("画面の状態を判定できないのに担当を手放した:\n%s", fx.Logs.String())
	}
}

// TestQuota_走っている印が残っていても止まっていれば手放す は、
// 「サブエージェントの一覧を手放しの条件にしない」を確かめる（設計 3-27。issue #197）。
//
// 目的: **`runningSubagentList()` を条件にすると、この仕組みが1回も動かなくなる。**
// **あの一覧を空にする経路は4つあり、4つとも hook か次の turn で駆動する。**
// （`docs/spec/turn_end_detect_mechanizm.md` の 3-8 に並べてある）
// **枠待ちの最中は、どちらも起きない。**次の turn は枠が明けるまで送られず、hook も来ない。
// **つまり、枠が尽きた瞬間にサブエージェントが走っていた run は、一覧が永久に空にならない。**
// **手放しの仕組みが、いちばん効いてほしい場面で1回も動かなくなる。**
//
// **人間の指示（2026-09-06）は「サブエージェントを含め完全停止するまで待って」である。**
// **それは `agent_status` が受け持つ。**サブエージェントの出力も同じ pane へ出るので、
// 何かが動いているあいだ herdr は `working` を返す。
//
// 与える情報: 1週間の枠が 100% でリセットは48時間後。上限は10分。
// **走っているサブエージェントの印が1つ残っているが、`agent_status` は `idle`。**
// 成功条件: 手放すこと。
func TestQuota_走っている印が残っていても止まっていれば手放す(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 10, "CONTINUO_TEST_OAUTH_TOKEN_W_SUBAGENT")
	// **`SubagentStop` が来ないまま枠が尽きた状態を作る。**
	fx.Orc.OnHook(subagentStartEvent("session-188", "", "a1f9f743842d397e1", "Explore"))

	waitForRelease(t, fx, clock, issue.Identifier)

	// **印から外れる出口は手放しだけではない**（実装レビュー2周目の MEDIUM）。
	// **`mayReleaseOwnWork` が「担当は自分ではない」と答えると、`stopHandoffLostClaimed` へ落ちる。**
	// **あちらは `after_run` も `released` も通さずに pane を閉じて印から外す**ので、
	// **`viewOf` だけを見る検査は緑のままになる。**担当者とログの1行で区別する。
	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 0 {
		t.Fatalf("担当者が残っている（手放しの経路を通っていない）: %v", got)
	}
	if got := fx.Logs.String(); !strings.Contains(got, "担当を手放しました") {
		t.Fatalf("手放したことを出していない（打ち切りか畳みで消えた恐れがある）:\n%s", got)
	}
}

// TestQuota_5時間の枠の時刻で1週間の枠を判定しない は、待つ先の取り方を確かめる。
//
// 目的: **`LatestResetForClearing` は種別を選ばない。**1週間の枠が `resets_at` を持たず、
// 5時間の枠が2時間後に明けるとき、**あれを使うと「2時間後」で判定してしまい、
// 上限（10分）を超えないので手放さない。**
//
// 与える情報: 1週間の枠が 100% で `resets_at` が null。5時間の枠も 100% で2時間後。上限は10分。
// 成功条件: 経過で測って手放すこと（5時間の枠の時刻に引きずられない）。
func TestQuota_5時間の枠の時刻で1週間の枠を判定しない(t *testing.T) {
	soon := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "session", "percent": 100, "resets_at": soon, "severity": "normal"},
		{"kind": "weekly_scoped", "percent": 95, "resets_at": nil, "severity": "normal"},
	}, 10, "CONTINUO_TEST_OAUTH_TOKEN_W4")

	fx.Orc.Tick(context.Background())
	clock.Advance(20 * time.Minute)
	fx.Orc.Tick(context.Background())
	// **連番を初めて見た巡回では手放さない**（設計 3-27 の「段0 へ入る前に外すもの」の7行目）。
	waitForRelease(t, fx, clock, issue.Identifier)

	// **打ち切りで消えたのではないことを確かめる**（issue #197）。
	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 0 {
		t.Fatalf("担当者が残っている: %v", got)
	}
	if got := fx.Logs.String(); !strings.Contains(got, "担当を手放しました") {
		t.Fatalf("5時間の枠の時刻に引きずられず手放したことを出していない:\n%s", got)
	}
}

// TestQuota_上限が0なら1週間の枠でも待ち続ける は、逃げ道を確かめる。
//
// 目的: **`weekly_wait_limit_minutes: 0` は「上限を設けない」である。**
// `claude.turn_timeout_ms` と `tracker.provider.handoff.recheck_interval_ms` と同じ向きである。
//
// 与える情報: 1週間の枠が 100% で、リセットは48時間後。**上限は0。**
// 成功条件: 印から外れないこと。
func TestQuota_上限が0なら1週間の枠でも待ち続ける(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	fx, issue, clock := weeklyWaitFixture(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	}, 0, "CONTINUO_TEST_OAUTH_TOKEN_W5")

	fx.Orc.Tick(context.Background())
	clock.Advance(10 * time.Hour)
	fx.Orc.Tick(context.Background())

	if _, ok := viewOf(fx, issue.Identifier); !ok {
		t.Fatalf("上限が0なのに担当を手放している:\n%s", fx.Logs.String())
	}
}

// TestQuota_打ち切りを切っていても巡回は落ちない は、判定の置き場所を確かめる
// （設計 3-27。issue #197）。
//
// 目的: **`claude.turn_timeout_ms` が0以下だと、巡回の打ち切りの判定は行わない**
// （`SPEC.md` 8.4 が「0 以下なら stall 検知を行わない」と決めている）。
// **それでも枠待ちの印は立つ**（hook を1件も受けていない run は、無音の長さを見ずに
// 枠待ちと判定される）。**だから上限の判定を打ち切りの門より前へ出した。**
//
// 与える情報: `claude.turn_timeout_ms: 0`。1週間の枠が 100% で、リセットは48時間後。上限は300分。
// 成功条件: 巡回が落ちず、**枠待ちでない run を誤って手放さない**こと。
func TestQuota_打ち切りを切っていても巡回は落ちない(t *testing.T) {
	resetsAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	endpoint, _ := newUsageServer(t, []map[string]any{
		{"kind": "weekly_all", "percent": 100, "resets_at": resetsAt, "severity": "normal"},
	})
	reader := newUsageReader(t, endpoint, "CONTINUO_TEST_OAUTH_TOKEN_W6")
	clock := newTestClock()

	fx := newStubFixture(t, stubFixtureOptions{
		// **ログを溜める。**止めた理由が既定の水準で出ることを検査する（issue #173）。
		Logs:        true,
		AgentStatus: herdr.AgentStatusUnknown,
		RateLimit:   reader,
		Now:         clock.Now,
		Mutate: func(cfg *config.Config) {
			// **打ち切りの判定を切る。**この設定は validate が明示的に許している。
			cfg.Claude.TurnTimeoutMs = 0
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.RateLimit.WeeklyWaitLimitMinutes = 300
		},
	})

	issue := assignedIssue(188, "In Progress", testGHLogin)
	fx.Tracker.AddIssue(issue)
	fx.Orc.Adopt(issue, orchestrator.AdoptedRun{
		AgentName:        normalize.SafeName("continuo-hello-world-188"),
		PaneID:           "w1:p1",
		SessionUUID:      "session-188",
		HerdrWorkspaceID: "w1",
	}, false)

	// **この検査は「落ちないこと」までしか確かめられない。**
	// **枠待ちの印を、巡回に入る前に立てる手立てが無いためである**
	// （`orchestrator.AdoptedRun` に枠待ちの欄が無く、その型は
	// このリポジトリの決まりで触れないファイルにある）。
	// **印を立てられるのは turn の待ちループだけで、そこを通すには turn を1回走らせる必要がある。**
	//
	// **確かめられているのは、`claude.turn_timeout_ms` が0以下でも巡回が落ちないことと、
	// 枠待ちでない run を誤って手放さないことの2つである。**
	// **上限そのものは、上の5本が確かめている。**
	fx.Orc.Tick(context.Background())

	if _, ok := viewOf(fx, issue.Identifier); !ok {
		t.Fatalf("枠待ちでない run を手放している:\n%s", fx.Logs.String())
	}
}
