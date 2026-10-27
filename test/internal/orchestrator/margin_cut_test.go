package orchestrator_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/tracker"
)

// marginFixture は、偽の usage API を読む stub の組み立てを作る（issue #262）。
//
// t: 呼び出し元のテスト。
// limits: 偽の usage API が返す期間の一覧。
// mutate: 設定を書き換える関数（nil でよい）。
// 戻り値の1つ目: 組み立てた stub。
// 戻り値の2つ目: 根のディレクトリ（hook を受ける socket と margin-status がその直下に置かれる）。
func marginFixture(t *testing.T, limits []map[string]any, mutate func(cfg *config.Config)) (*stubFixture, string) {
	t.Helper()
	root := t.TempDir()
	endpoint, _ := newUsageServer(t, limits)
	reader := newUsageReader(t, endpoint, "CONTINUO_TEST_OAUTH_TOKEN_MARGIN")
	return newStubFixture(t, stubFixtureOptions{
		Root:      root,
		Logs:      true,
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
			cfg.Trust.RequireRepoTrusted = false
			if mutate != nil {
				mutate(cfg)
			}
		},
	}), root
}

// isRunning は、その issue の run があるかを返す。
func isRunning(fx *stubFixture, identifier string) bool {
	for _, v := range fx.Orc.RunViews() {
		if v.Identifier == identifier {
			return true
		}
	}
	return false
}

// 目的: 担当が自分の issue でも、5時間の期間のマージンを切っていれば、Claude Code を起動せずに
// `Blocked` へ戻すことを確かめる（issue #262。設計 3-27a の着手の前）。
//
// **人間の決定は「担当になっているが一度blockedなどになっている状態で、余裕%が0%以下なら、
// In Progressステータスなどに遷移しても新たにrunを開始せず即座にblockedに遷移する」である**（2026-10-05）。
// **担当者は残す。**理由のコメントに、切った期間とリセットの時刻を書く。
//
// 与える情報: 5時間の期間が 93%（マージン 10）を返す usage API。この機械が担当者の `Ready` の issue が1件。
// 成功条件: run が始まらず、Status が `Blocked` になり、担当者がこの機械のままで、理由のコメントが1件あること。
func TestMarginCut_着手の前に5時間の期間を切っていればBlockedへ戻す(t *testing.T) {
	resets := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	fx, _ := marginFixture(t, []map[string]any{
		{"kind": "session", "percent": 93, "resets_at": resets, "severity": "normal"},
		{"kind": "weekly_all", "percent": 20, "resets_at": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339), "severity": "normal"},
	}, nil)
	issue := assignedIssue(301, "Ready", testGHLogin)
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())

	if isRunning(fx, issue.Identifier) {
		t.Fatalf("マージンを切っているのに Claude Code を起動しました:\n%s", fx.Logs.String())
	}
	if got := fx.Tracker.StateOf(issue.ID); got != "Blocked" {
		t.Errorf("Status = %q（Blocked を期待）:\n%s", got, fx.Logs.String())
	}
	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 1 || got[0] != testGHLogin {
		t.Errorf("担当者を残していません: %v", got)
	}
	found := false
	for _, c := range fx.Tracker.HandoffCommentsOf("I_node301") {
		if strings.Contains(c.Body, "レートリミットのマージンを切っている") && strings.Contains(c.Body, "5時間の期間が 93%") {
			found = true
		}
	}
	if !found {
		t.Errorf("理由のコメントに、切った期間が書かれていません: %+v", fx.Tracker.HandoffCommentsOf("I_node301"))
	}
}

// 目的: 担当が自分の issue で、1週間の期間で待つ上限を超えていても、Claude Code が動けるなら、いつもどおり着手する
// ことを確かめる（issue #262。人間の「まだAIが動くならpushと引き継ぎコメントを書いてから担当を剥がす」）。
//
// 与える情報: 1週間の期間が 93% でリセットが3日先（待つ上限は既定300分）。5時間の期間は 20%。
// 成功条件: run が始まり、担当者がまだ外れていないこと（外すのは最初の turn の切り上げのあと）。
func TestMarginCut_着手の前に上限を超えていても動けるなら着手する(t *testing.T) {
	fx, _ := marginFixture(t, []map[string]any{
		{"kind": "session", "percent": 20, "resets_at": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339), "severity": "normal"},
		{"kind": "weekly_all", "percent": 93, "resets_at": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339), "severity": "normal"},
	}, nil)
	issue := assignedIssue(302, "Ready", testGHLogin)
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())

	if !isRunning(fx, issue.Identifier) {
		t.Fatalf("Claude Code が動けるのに着手していません:\n%s", fx.Logs.String())
	}
	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 1 || got[0] != testGHLogin {
		t.Errorf("切り上げの前に担当を外しています: %v", got)
	}
}

// 目的: 担当が自分の issue で、1週間の期間で待つ上限を超えていて、Claude Code が動けない（使用率100%）なら、
// run を始めずに担当を外し、「走らせていない」と分かる本文を書くことを確かめる（issue #262）。
//
// **人間の「レートリミットに達していたらもう何もできないので途中までのものは諦める」（2026-10-05）。**
//
// 与える情報: 1週間の期間が 100% でリセットが3日先。
// 成功条件: run が始まらず、担当者が外れ、released の本文に `weekly_wait_limit_not_started` が載ること。
func TestMarginCut_着手の前に上限を超えて動けなければrunを始めずに担当を外す(t *testing.T) {
	fx, _ := marginFixture(t, []map[string]any{
		{"kind": "session", "percent": 20, "resets_at": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339), "severity": "normal"},
		{"kind": "weekly_all", "percent": 100, "resets_at": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339), "severity": "normal"},
	}, nil)
	issue := assignedIssue(303, "Ready", testGHLogin)
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())

	if isRunning(fx, issue.Identifier) {
		t.Fatalf("使用率100%%なのに Claude Code を起動しました:\n%s", fx.Logs.String())
	}
	if got := assigneeLoginsOf(fx, issue.ID); len(got) != 0 {
		t.Errorf("担当を外していません: %v\n%s", got, fx.Logs.String())
	}
	if body := releasedBodyOf(fx, issue); !strings.Contains(body, `"reason":"weekly_wait_limit_not_started"`) {
		t.Errorf("released の本文に理由の値がありません:\n%s", body)
	}
}

// 目的: マージンを切っていなければ、margin-status に `MARGIN: ok` を書き、切っていれば `MARGIN: cut` を書くことを
// 確かめる（issue #262。組み込みの指示書の「レートリミットのマージンを切ったら、切り上げる」がこれを読む）。
//
// 与える情報: 5時間の期間が 50% と 93% の2通り。
// 成功条件: 実行時ディレクトリ（hook を受ける socket と同じディレクトリ）の margin-status が期待どおりであること。
func TestMarginCut_巡回のたびにmargin_statusを書く(t *testing.T) {
	for _, c := range []struct {
		name    string
		percent int
		prefix  string
	}{
		{"切っていない", 50, "MARGIN: ok"},
		{"切った", 93, "MARGIN: cut kind=session used=93 margin=10 resets_at="},
	} {
		t.Run(c.name, func(t *testing.T) {
			fx, root := marginFixture(t, []map[string]any{
				{"kind": "session", "percent": c.percent, "resets_at": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339), "severity": "normal"},
			}, nil)
			fx.Orc.Tick(context.Background())
			data, err := os.ReadFile(filepath.Join(root, "margin-status"))
			if err != nil {
				t.Fatalf("margin-status を読めません: %v\n%s", err, fx.Logs.String())
			}
			if !strings.HasPrefix(string(data), c.prefix) || !strings.Contains(string(data), " expires_at=") {
				t.Errorf("margin-status = %q（%q で始まり expires_at を持つことを期待）", data, c.prefix)
			}
		})
	}
}

// 目的: 走っている最中にマージンを切ったら、次の turn に「続けてください」の代わりに切り上げの指示を送り、
// その turn の終わりに `Blocked` へ戻すことを確かめる（issue #262。設計 3-27a の場面 3 と 6）。
//
// **送るのは、今なら「続けてください」を送る場面（Status が作業中のまま turn が終わったとき）だけである。**
// **切り上げの turn の終わりには、Claude Code が引き継ぎのコメントを書いているので、成果のコメントの確かめは通る。**
//
// 与える情報: 着手のときは 5時間の期間が 50%。1回目の turn のあいだに 93%（マージン 10）へ上がる。
// 1回目と2回目の応答は、どちらも `CONTINUO-STATUS: working`。2回目の turn で、エージェントの引き継ぎのコメントが書かれる。
// 成功条件: 2回目に送った文面が切り上げの指示で、「続けてください」を含まず、run が終わって Status が `Blocked` になり、
// 担当者がこの機械のままであること。
func TestMarginCut_走っている最中に切ったら切り上げさせてBlockedへ戻す(t *testing.T) {
	var percent atomic.Int32
	percent.Store(50)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{"limits": []map[string]any{
			{"kind": "session", "percent": percent.Load(), "resets_at": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339), "severity": "normal"},
			{"kind": "weekly_all", "percent": 20, "resets_at": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339), "severity": "normal"},
		}}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("偽の usage API が応答を書けません: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	reader := newUsageReader(t, srv.URL, "CONTINUO_TEST_OAUTH_TOKEN_TURN")

	fx := newFixture(t, fixtureOptions{
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
		},
	})
	// **この fixture の巡回の間隔は1時間なので、新しさの幅の検査が WARN を出す。**この検査の対象ではない。
	fx.AllowLog("rate_limit.refresh_interval_ms が polling.interval_ms 以下")
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	transcriptDir := t.TempDir()
	first := writeTranscript(t, transcriptDir, "first.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "途中です。\n\nCONTINUO-STATUS: working", false),
	})
	second := writeTranscript(t, transcriptDir, "second.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "途中です。\n\nCONTINUO-STATUS: working", false),
		typedUserLine("p2", "切り上げてください"),
		assistantLine("req2", "push して引き継ぎのコメントを書きました。\n\nCONTINUO-STATUS: working", false),
	})

	var mu sync.Mutex
	var texts []string
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		text, _ := params["text"].(string)
		texts = append(texts, text)
		n := len(texts)
		mu.Unlock()

		path := first
		if n == 1 {
			// **1回目の turn のあいだに使用率が上がる。**次の巡回で読み直させる。
			percent.Store(93)
			fx.Orc.Tick(context.Background())
		}
		if n >= 2 {
			path = second
			fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n途中まで push しました。次は残りのテストを書きます", true, time.Now())
		}
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "run が終わる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})

	mu.Lock()
	defer mu.Unlock()
	if len(texts) < 2 {
		t.Fatalf("2回目の turn が送られていない: %d 回\n%s", len(texts), fx.Logs.String())
	}
	if strings.Contains(texts[1], "続けてください") || !strings.Contains(texts[1], "レートリミットのマージンを切りました") {
		t.Errorf("2回目に切り上げの指示を送っていない:\n%s", texts[1])
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Blocked" {
		t.Errorf("Status = %q（Blocked を期待）:\n%s", got, fx.Logs.String())
	}
	if got := assigneeLoginsOfFixture(fx, "PVTI_item188"); len(got) != 1 || got[0] != testGHLogin {
		t.Errorf("担当者を残していません: %v", got)
	}
}

// 目的: 走っている最中に1週間の期間のマージンを切り、リセットまでが待つ上限を超えるなら、切り上げの turn の終わりに
// 何も送らずに「行き先を待つ」印を立て、巡回の手放しが担当を外すことを確かめる（issue #262。設計 3-27a の場面 4）。
//
// **人間の決定は「担当外して引き継ぎできるようにしたい」**（2026-10-05）。**手放す前に push と引き継ぎのコメントを
// 書かせる**（切り上げの turn）。担当を外すのは今の巡回の手放しで、印の立った run は3つの待ちを飛ばす。
//
// 与える情報: 着手のときは1週間の期間が 50%。1回目の turn のあいだに 93% へ上がる（リセットは3日先。待つ上限は既定300分）。
// 成功条件: 2回目に送った文面が切り上げの指示で、3回目の turn が送られず、巡回を回すうちに担当者が外れ、
// released の本文が自分から手放した側のものであること。
func TestMarginCut_1週間の期間で上限を超えたら切り上げてから巡回が担当を外す(t *testing.T) {
	var percent atomic.Int32
	percent.Store(50)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{"limits": []map[string]any{
			{"kind": "session", "percent": 20, "resets_at": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339), "severity": "normal"},
			{"kind": "weekly_all", "percent": percent.Load(), "resets_at": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339), "severity": "normal"},
		}}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("偽の usage API が応答を書けません: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	reader := newUsageReader(t, srv.URL, "CONTINUO_TEST_OAUTH_TOKEN_WEEKLY")

	fx := newFixture(t, fixtureOptions{
		RateLimit: reader,
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
		},
	})
	fx.AllowLog("rate_limit.refresh_interval_ms が polling.interval_ms 以下")
	// **画面が止まっていると読めるようにする。**巡回の手放しは `idle` と、変わらない連番
	// （`state_change_seq`）を2巡回続けて読んでから担当を外す（`paneStopped`）。連番が 0 だと止まったと見なさない。
	fx.Herdr.Handle(herdr.MethodAgentGet, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type": "agent_info",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true,
				"state_change_seq": 7},
		}, nil
	})
	issue := assignedIssue(189, "Ready", testGHLogin)
	fx.Tracker.AddIssue(issue)

	transcriptDir := t.TempDir()
	first := writeTranscript(t, transcriptDir, "first.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "途中です。\n\nCONTINUO-STATUS: working", false),
	})
	second := writeTranscript(t, transcriptDir, "second.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "途中です。\n\nCONTINUO-STATUS: working", false),
		typedUserLine("p2", "切り上げてください"),
		assistantLine("req2", "push して引き継ぎのコメントを書きました。\n\nCONTINUO-STATUS: working", false),
	})

	var mu sync.Mutex
	var texts []string
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		text, _ := params["text"].(string)
		texts = append(texts, text)
		n := len(texts)
		mu.Unlock()

		path := first
		if n == 1 {
			percent.Store(93)
			fx.Orc.Tick(context.Background())
		}
		if n >= 2 {
			path = second
		}
		fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "切り上げの指示が送られる", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(texts) >= 2
	})
	released := false
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		fx.Orc.Tick(context.Background())
		if len(assigneeLoginsOfFixture(fx, issue.ID)) == 0 {
			released = true
			break
		}
	}
	if !released {
		t.Fatalf("巡回の手放しが担当を外していない:\n%s", fx.Logs.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(texts[1], "レートリミットのマージンを切りました") {
		t.Errorf("2回目に切り上げの指示を送っていない:\n%s", texts[1])
	}
	if len(texts) > 2 {
		t.Errorf("「行き先を待つ」run に指示を送っている（%d 回）:\n%s", len(texts), texts[2])
	}
	// **担当を外してから released を書く**（`removeOwnAssignee`）ので、担当者が外れた直後はまだ無いことがある。
	waitFor(t, 10*time.Second, "released のコメントが書かれる", func() bool {
		return releasedBodyOfFixture(fx, issue) != ""
	})
	if body := releasedBodyOfFixture(fx, issue); !strings.Contains(body, `"reason":"weekly_wait_limit`) ||
		strings.Contains(body, "weekly_wait_limit_not_started") {
		t.Errorf("released の本文が、切り上げてから自分で手放した側のものではありません:\n%s", body)
	}
}

// assigneeLoginsOfFixture は、本物に近い組み立て（fixture）で、issue の担当者のログイン名を返す。
func assigneeLoginsOfFixture(fx *fixture, id string) []string {
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

// 目的: 最初の turn でマージンを切っていたら、指示書と人間のコメントの節のいちばん後ろに、作業を始めずに
// 切り上げるよう添えることを確かめる（issue #262。設計 3-27a の場面 2 の2つ目の枝）。
//
// 与える情報: この機械が担当者の `Ready` の issue。1週間の期間が 93% でリセットが3日先（待つ上限を超える）。
// 5時間の期間は 20%（Claude Code は動ける）。
// 成功条件: エージェントが読むもの（最初のプロンプト・システムプロンプト・スキル）が
// 「レートリミットのマージンを切ったら、切り上げる」の節を含み、1回目に送った文面がいちばん最後に切り上げの文を持つこと。
func TestMarginCut_最初のturnで切っていたら指示書の最後に切り上げを添える(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		RateLimit: newUsageReader(t, func() string {
			u, _ := newUsageServer(t, []map[string]any{
				{"kind": "session", "percent": 20, "resets_at": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339), "severity": "normal"},
				{"kind": "weekly_all", "percent": 93, "resets_at": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339), "severity": "normal"},
			})
			return u
		}(), "CONTINUO_TEST_OAUTH_TOKEN_FIRST"),
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
		},
	})
	fx.AllowLog("rate_limit.refresh_interval_ms が polling.interval_ms 以下")
	prompts := recordPrompts(fx)
	fx.Tracker.AddIssue(assignedIssue(190, "Ready", testGHLogin))

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "1回目の turn が送られる", func() bool { return len(prompts()) > 0 })

	got := prompts()[0]
	// **切り上げの手順はスキル「reporting-progress」にある**（issue #288）。エージェントが読むもの全体で見る。
	if !strings.Contains(agentView(t, fx, "octocat/hello-world#190", got), "\n## レートリミットのマージンを切ったら、切り上げる\n") {
		t.Errorf("エージェントが読むものに「レートリミットのマージンを切ったら、切り上げる」の節がありません")
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "スキル「reporting-progress」の「レートリミットのマージンを切ったら、切り上げる」の切り上げだけを行ってください。") {
		t.Errorf("1回目の文面の最後に切り上げの文がありません: %q", got[max(0, len(got)-200):])
	}
}

// 目的: 60分の打ち切りの文面の1行目に、そのとき continuo が持っていた使用率を載せることを確かめる（issue #273）。
//
// **打ち切られた issue を開いた人が、レートリミットが関係していたのかを判断できるようにする。**
// **使用率を読まない設定（`rate_limit.source: none`）では足さない**（読まないと決めた利用者に、故障のような原因を案内しない）。
//
// 与える情報: 5時間の期間が 50% を返す usage API（と、使用率を読まない設定）で、1回目の turn で画面が止まる run。
// 成功条件: 読めていたときは、引き渡しの通知の1行目に「そのとき continuo が持っていた使用率は、5時間の期間が 50%」が入り、
// 【よくある原因】に読めていなかったことが入らない。読まない設定では、使用率の1文が入らない。
func TestMarginCut_打ち切りの文面に使用率を載せる(t *testing.T) {
	for _, c := range []struct {
		name     string
		read     bool
		contains string
	}{
		{"使用率を読める機械", true, "そのとき continuo が持っていた使用率は、5時間の期間が 50%、1週間の期間が 20% でした（最後に読めたのは 1分以内）。"},
		{"使用率を読まない機械", false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			clock := newTestClock()
			opts := fixtureOptions{
				Now: clock.Now,
				Mutate: func(cfg *config.Config) {
					cfg.Claude.TurnTimeoutMs = 1000
					cfg.Agent.MaxRetries = 0
					cfg.Tracker.VerifyStatesEvery = 0
					if c.read {
						cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
						cfg.RateLimit.PollIntervalMs = 1
					}
				},
			}
			if c.read {
				u, _ := newUsageServer(t, []map[string]any{
					{"kind": "session", "percent": 50, "resets_at": clock.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339), "severity": "normal"},
					{"kind": "weekly_all", "percent": 20, "resets_at": clock.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339), "severity": "normal"},
				})
				opts.RateLimit = newUsageReader(t, u, "CONTINUO_TEST_OAUTH_TOKEN_STALL")
			}
			fx := newFixture(t, opts)
			blockFirstPrompt(t, fx)
			fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
			fx.AllowLog("リトライの回数を使い切りました", "turn が終わったことを検知できません", "画面が変わらないまま", "stall",
				"rate_limit.refresh_interval_ms が polling.interval_ms 以下", "agent の状態を読めませんでした")

			fx.Orc.Tick(context.Background())
			waitFor(t, 5*time.Second, "1回目の turn が待ち受けに入る", func() bool {
				return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
			})
			clock.Advance(5 * time.Second)
			fx.Orc.Tick(context.Background())
			waitFor(t, 20*time.Second, "引き渡しの通知が issue に残る", func() bool {
				return len(fx.Tracker.HandoffCommentsOf("I_node188")) > 0
			})

			body := fx.Tracker.HandoffCommentsOf("I_node188")[0].Body
			if !strings.Contains(body, "止まったものと判断して打ち切りました") {
				t.Fatalf("打ち切りの文面ではありません:\n%s", body)
			}
			if c.read && !strings.Contains(body, c.contains) {
				t.Errorf("使用率の1文がありません（%q を期待）:\n%s", c.contains, body)
			}
			if !c.read && strings.Contains(body, "そのとき continuo") {
				t.Errorf("使用率を読まない設定なのに、使用率の1文を足しています:\n%s", body)
			}
			if strings.Contains(body, "continuo が使用率を読めていなかった") {
				t.Errorf("読めていた（または読まない）のに、読めていなかったことを原因に挙げています:\n%s", body)
			}
		})
	}
}

// releasedBodyOfFixture は、本物に近い組み立て（fixture）で、その issue の released のコメントの本文を返す。
func releasedBodyOfFixture(fx *fixture, issue tracker.Issue) string {
	var out []string
	for _, c := range fx.Tracker.MarkedHandoffCommentsOf(nodeIDOfIssue(issue), config.HandoffReleasedMarker) {
		out = append(out, c.Body)
	}
	return strings.Join(out, "\n")
}

// awaitScenario は、1週間の期間のマージンを切って「行き先を待つ」印が立った run を作る（issue #262）。
//
// **1回目の turn のあいだに1週間の期間が 93% へ上がり、2回目（切り上げ）の turn のあとに印が立つ。**
// 画面の連番を返さないので、巡回の手放しは担当を外さない（止まっていると確かめられない）。
//
// 戻り値の1つ目: 組み立て。
// 戻り値の2つ目: 送った文面の一覧を返す関数。
// 戻り値の3つ目: 1週間の期間の使用率を変える関数。
// 戻り値の4つ目: 1週間の期間のリセットまでの長さを変える関数。
func awaitScenario(t *testing.T, number int) (*fixture, func() []string, func(int32), func(time.Duration)) {
	t.Helper()
	var percent atomic.Int32
	var resetIn atomic.Int64
	percent.Store(50)
	resetIn.Store(int64(72 * time.Hour))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{"limits": []map[string]any{
			{"kind": "session", "percent": 20, "resets_at": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339), "severity": "normal"},
			{"kind": "weekly_all", "percent": percent.Load(), "resets_at": time.Now().Add(time.Duration(resetIn.Load())).UTC().Format(time.RFC3339), "severity": "normal"},
		}}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("偽の usage API が応答を書けません: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	fx := newFixture(t, fixtureOptions{
		RateLimit: newUsageReader(t, srv.URL, fmt.Sprintf("CONTINUO_TEST_OAUTH_TOKEN_AWAIT_%d", number)),
		Mutate: func(cfg *config.Config) {
			cfg.RateLimit.Source = ratelimit.SourceOAuthUsageAPI
			cfg.RateLimit.PollIntervalMs = 1
		},
	})
	fx.AllowLog("rate_limit.refresh_interval_ms が polling.interval_ms 以下")
	// **画面の連番（`state_change_seq`）は返さない**（既定の偽の herdr のまま）。連番が 0 だと、巡回の手放しは
	// 止まったと見なさないので、担当を外さない（`noteQuotaProbe`）。
	issue := assignedIssue(number, "Ready", testGHLogin)
	fx.Tracker.AddIssue(issue)
	node := fmt.Sprintf("I_node%d", number)
	transcriptDir := t.TempDir()
	first := writeTranscript(t, transcriptDir, "first.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "途中です。\n\nCONTINUO-STATUS: working", false),
	})
	second := writeTranscript(t, transcriptDir, "second.jsonl", []any{
		typedUserLine("p1", "実装してください"),
		assistantLine("req1", "途中です。\n\nCONTINUO-STATUS: working", false),
		typedUserLine("p2", "切り上げてください"),
		assistantLine("req2", "push して引き継ぎのコメントを書きました。\n\nCONTINUO-STATUS: working", false),
	})
	var mu sync.Mutex
	var texts []string
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		mu.Lock()
		text, _ := params["text"].(string)
		texts = append(texts, text)
		n := len(texts)
		mu.Unlock()
		path := first
		if n == 1 {
			percent.Store(93)
			fx.Orc.Tick(context.Background())
		}
		if n >= 2 {
			path = second
			fx.Tracker.AddComment(node, "<!-- continuo:agent -->\n途中まで push しました", true, time.Now())
		}
		if n <= 2 {
			fx.Orc.OnHook(stopEvent("session-1", path, "p1"))
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle", "interactive_ready": true},
		}, nil
	})
	got := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), texts...)
	}
	fx.Orc.Tick(context.Background())
	waitFor(t, 20*time.Second, "「行き先を待つ」印が立つ", func() bool {
		return strings.Contains(fx.Logs.String(), "行き先を巡回に決めさせます")
	})
	return fx, got, func(p int32) { percent.Store(p) }, func(d time.Duration) { resetIn.Store(int64(d)) }
}

// 目的: 「行き先を待つ」run は、マージンを切った期間がもう無いと読めたら、印を下ろして作業へ戻すことを確かめる
// （issue #262。設計 3-27a の巡回の側の段2）。
//
// 与える情報: 印が立ったあと、1週間の期間が別の期間に替わって使用率が 50% へ戻る（アカウントを替えたなど。
// **同じ期間の中では、保管値は使用率を下げない**ので、リセットの時刻も変える）。
// 成功条件: 3回目の turn が送られ、それが「続けてください」であること。担当者は外れていないこと。
func TestMarginCut_行き先を待つrunはマージンが戻ったら作業へ戻す(t *testing.T) {
	fx, texts, setPercent, setReset := awaitScenario(t, 193)
	setReset(100 * time.Hour)
	setPercent(50)
	resumed := false
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		fx.Orc.Tick(context.Background())
		if len(texts()) >= 3 {
			resumed = true
			break
		}
	}
	if !resumed {
		t.Fatalf("作業へ戻す指示が送られていない:\n%s", fx.Logs.String())
	}
	if got := texts()[2]; !strings.Contains(got, "続けてください") || strings.Contains(got, "レートリミットのマージンを切りました") {
		t.Errorf("マージンが戻ったのに、作業へ戻す指示になっていません:\n%s", got)
	}
	if got := assigneeLoginsOfFixture(fx, "PVTI_item193"); len(got) != 1 {
		t.Errorf("担当者を外しています: %v", got)
	}
}

// 目的: 「行き先を待つ」run は、行き先が `Blocked` に変わったら（リセットまでが待つ上限以内になった）、
// Status を戻すことを確かめる（issue #262。設計 3-27a の巡回の側の段2）。
//
// 与える情報: 印が立ったあと、1週間の期間のリセットが2時間後になる（待つ上限は既定300分）。
// 成功条件: Status が `Blocked` になり、担当者がこの機械のままで、3回目の turn が送られないこと。
func TestMarginCut_行き先を待つrunは行き先がBlockedに変わったらStatusを戻す(t *testing.T) {
	fx, texts, _, setReset := awaitScenario(t, 194)
	setReset(2 * time.Hour)
	waitFor(t, 20*time.Second, "Status が Blocked へ戻る", func() bool {
		fx.Orc.Tick(context.Background())
		return fx.Tracker.StateOf("PVTI_item194") == "Blocked"
	})
	if got := assigneeLoginsOfFixture(fx, "PVTI_item194"); len(got) != 1 || got[0] != testGHLogin {
		t.Errorf("担当者を残していません: %v", got)
	}
	if got := texts(); len(got) > 2 {
		t.Errorf("Blocked へ戻す run に指示を送っています（%d 回）:\n%s", len(got), got[2])
	}
}
