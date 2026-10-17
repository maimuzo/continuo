package orchestrator_test

import (
	"context"
	"encoding/json"
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
// 確かめる（issue #262。組み込みの指示書の 5-8 がこれを読む）。
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
// 成功条件: 1回目に送った文面が、5-8 の節を含み、いちばん最後に切り上げの文を持つこと。
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
	if !strings.Contains(got, "## 5-8. レートリミットのマージンを切ったら、切り上げる") {
		t.Errorf("1回目の文面に 5-8 がありません")
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "組み込みの指示書の 5-8 の切り上げだけを行ってください。") {
		t.Errorf("1回目の文面の最後に切り上げの文がありません: %q", got[max(0, len(got)-200):])
	}
}
