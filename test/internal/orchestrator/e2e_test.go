package orchestrator_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
)

// TestTick_1件のissueが候補に上がってからDoneで片付くまで通る は、
// 1件の issue が最初から最後まで通ることを、Claude Code を起動せずに確かめる。
//
// 目的: 第6段階の受け入れの基準（「1件の issue が候補に上がってから Done で片付くまで」）を
// 1本のテストで通す。**着手の13段・turn の終わりの判定・表明の読み取り・完了の真実の源が
// ボードであること・worktree と branch の片付け**を、この1本がまとめて通る。
//
// 与える情報:
//   - ボードに `Ready` の issue が1件（本物の git の clone がある）
//   - テスト用herdr mock（実 herdr は使わない）
//   - `agent.prompt` を受けたら、Claude Code が「実装して push し、コメントを書き、
//     自分で `gh` を叩いて Status を `Done` へ動かし、`CONTINUO-STATUS: review` を書いて
//     turn を終えた」状態を作り、`Stop` hook を continuo へ流す
//
// 成功条件:
//   - Status が `Ready` → `In Progress` へ書かれる（段2）
//   - worktree が本物の git で作られ、身元ファイルと設定ファイルが置かれる（段3〜6）
//   - **pane を新しく作らない**（`pane.split` も `tab.create` も呼ばない）
//   - agent が起動し、1回目の turn が送られる（段9〜11）
//   - 表明を受けても、ボードが既に `Done` なら Status を書き戻さない
//   - `pane.close` で worker が止まり、worktree と branch が本当に消える
//   - 印（実行中の一覧）から外れる
func TestTick_1件のissueが候補に上がってからDoneで片付くまで通る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188, "Ready")
	fx.Tracker.AddIssue(issue)

	transcriptDir := t.TempDir()
	var promptedText string
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		promptedText, _ = params["text"].(string)

		// エージェントが作業を終えた状態を作る。
		path := writeTranscript(t, transcriptDir, "session-1.jsonl", []any{
			typedUserLine("p1", "実装してください"),
			assistantLine("req1", "実装して commit と push をしました。\n\nCONTINUO-STATUS: review", false),
		})
		// 何をしたかは**エージェントが**コメントに残す（continuo は代筆しない。設計 3-25 / 3-29）。
		fx.Tracker.AddComment("I_node188", "<!-- continuo:agent -->\n実装しました", true, time.Now())
		// **完了の真実の源はボードである。**エージェントが gh で Done へ動かした状況を作る。
		fx.Tracker.SetState(issue.ID, "Done")

		if !fx.Orc.OnHook(stopEvent("session-1", path, "p1")) {
			t.Errorf("continuo が session-1 の hook を知らない run のものとして捨てた")
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "idle"},
		}, nil
	})

	fx.Orc.Tick(context.Background())

	waitFor(t, 10*time.Second, "run が印から外れる", func() bool {
		return len(fx.Orc.RunningIdentifiers()) == 0
	})

	// 段2: Status を tracker.running_state へ書いた（ハードコードしない）。
	if fx.Tracker.CountCall("UpdateStatus") == 0 {
		t.Fatalf("Status を1度も書いていない: %v", fx.Tracker.Calls())
	}
	// 表明（review）を受けても、ボードが既に Done なので書き戻していない。
	if got := fx.Tracker.StateOf(issue.ID); got != "Done" {
		t.Fatalf("Done を巻き戻してしまった: got %q, want %q", got, "Done")
	}

	// **pane を新しく作らない**（設計 4-5 / 3-16 の段8）。
	methods := fx.Herdr.Methods()
	for _, m := range methods {
		if m == herdr.MethodPaneSplit || m == "tab.create" {
			t.Fatalf("pane を新しく作っている（1 worktree = 1 workspace に反する）: %v", methods)
		}
	}
	for _, want := range []string{
		herdr.MethodWorktreeOpen, herdr.MethodPaneList, herdr.MethodPaneRename,
		herdr.MethodAgentStart, herdr.MethodAgentPrompt, herdr.MethodPaneClose,
		herdr.MethodWorktreeRemove,
	} {
		if fx.Herdr.CountMethod(want) == 0 {
			t.Fatalf("%s が呼ばれていない: %v", want, methods)
		}
	}

	// 段8: pane の label に issue の URL を書いた（設計 3-3）。
	renameParams := fx.Herdr.ParamsOf(t, herdr.MethodPaneRename)
	if renameParams["label"] != *issue.URL {
		t.Fatalf("pane の label が issue の URL になっていない: got %v, want %q", renameParams["label"], *issue.URL)
	}

	// 段11: 1回目のプロンプトはテンプレートの描画結果である（**issue の本文は入れない**）。
	if !strings.Contains(promptedText, "maimuzo/koetsumugi#188") ||
		!strings.Contains(promptedText, "gh issue view") {
		t.Fatalf("1回目のプロンプトがテンプレートの描画結果になっていない: %q", promptedText)
	}

	// worktree と branch が本当に消えている（本物の git で確かめる）。
	worktreePath := filepath.Join(fx.WorktreeRoot, "github.com", "maimuzo", "koetsumugi", "continuo-maimuzo-koetsumugi-188")
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree が残っている: %s (err=%v)", worktreePath, err)
	}
	branches := runGit(t, fx.Repo.Dir, "branch", "--list", "continuo/maimuzo/koetsumugi/188")
	if branches != "" {
		t.Fatalf("branch が残っている: %q", branches)
	}
}

// TestTick_着手の13段が設計の順番どおりに進む は、着手の順番を確かめる。
//
// 目的: 設計 3-16 の「印を先に付け、Status を次に書く。実体を作るのはそのあとである」を
// 守っていることを示す。**順番を変えると、途中で落ちたとき同じ worktree に
// Claude Code が2つ立つ。**
//
// 与える情報: `Ready` の issue が1件。`agent.prompt` は turn を終わらせずに保留する。
// 成功条件:
//   - Status の書き込み（段2）が `worktree.open`（段3）より前に起きる
//   - `worktree.open` → `pane.list` → `pane.rename` → `agent.start` → `agent.get` →
//     `agent.prompt` の順で herdr を呼ぶ
//   - 設定ファイル（段5）と身元ファイル（段6）が、agent の起動より前に置かれている
func TestTick_着手の13段が設計の順番どおりに進む(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	settingsSeen := make(chan struct{}, 1)
	identitySeen := make(chan struct{}, 1)
	worktreePath := filepath.Join(fx.WorktreeRoot, "github.com", "maimuzo", "koetsumugi", "continuo-maimuzo-koetsumugi-188")
	settingsPath := filepath.Join(fx.RuntimeDir, "issues", "maimuzo-koetsumugi-188", "settings.json")

	fx.Herdr.Handle(herdr.MethodAgentStart, func(params map[string]any) (any, *rpcErr) {
		// 段9 の時点で、段5 の設定ファイルと段6 の身元ファイルが置かれているはずである。
		if _, err := os.Stat(settingsPath); err == nil {
			settingsSeen <- struct{}{}
		}
		if _, err := os.Stat(filepath.Join(worktreePath, ".continuo.json")); err == nil {
			identitySeen <- struct{}{}
		}
		return map[string]any{
			"type":  "agent_started",
			"agent": map[string]any{"name": params["name"], "agent_status": "idle"},
		}, nil
	})
	// turn を終わらせない（このテストは着手の順番だけを見る）。
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "working"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "1回目の turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})

	if len(settingsSeen) == 0 {
		t.Fatalf("agent を起動する時点で設定ファイル（段5）が置かれていない: %s", settingsPath)
	}
	if len(identitySeen) == 0 {
		t.Fatalf("agent を起動する時点で身元ファイル（段6）が置かれていない: %s", worktreePath)
	}

	// 段2（Status の書き込み）が段3（worktree.open）より前に起きている。
	// **トラッカーと herdr を混ぜた1本の並びで比べる。**別々の記録では前後関係が分からない。
	entries := fx.Timeline.Entries()
	updateIdx := fx.Timeline.IndexOf("tracker.UpdateStatus")
	openIdx := fx.Timeline.IndexOf("herdr." + herdr.MethodWorktreeOpen)
	if updateIdx < 0 {
		t.Fatalf("Status を書いていない: %v", entries)
	}
	if openIdx < 0 {
		t.Fatalf("worktree.open を呼んでいない: %v", entries)
	}
	if updateIdx > openIdx {
		t.Fatalf("段2（Status の書き込み）が段3（worktree.open）より後に起きている: %v", entries)
	}

	want := []string{
		herdr.MethodWorktreeOpen, herdr.MethodPaneList, herdr.MethodPaneRename,
		herdr.MethodAgentList, herdr.MethodAgentStart, herdr.MethodAgentGet, herdr.MethodAgentPrompt,
	}
	if got := filterMethods(fx.Herdr.Methods(), want); !equalStrings(got, want) {
		t.Fatalf("herdr の呼び出しの順番が設計 3-16 と違う:\n got  %v\n want %v", got, want)
	}
}

// TestTick_設定ファイルに8つのhookと環境変数を書く は、段5 の中身を確かめる。
//
// 目的: 設計 3-2 の「7つ張る」の一覧（`PreToolUse` / `PostToolUse` は matcher を絞らない）と、
// 設計 3-12 の「**環境変数は設定ファイルの env に書く。**pane にも agent.start にも渡さない」を
// 守っていることを示す。
//
// 与える情報: `Ready` の issue が1件。
// 成功条件:
//   - `<実行時ディレクトリ>/issues/<スラグ>/settings.json` に hook が8種書かれている
//   - `PreToolUse` / `PostToolUse` の matcher が `*` である
//   - hook のコマンド行に socket と逃がし先の絶対パスが入っている
//   - `env` に `CLAUDE_CODE_RETRY_WATCHDOG` が入っている
//   - `agent.start` の params に env が**入っていない**
func TestTick_設定ファイルに8つのhookと環境変数を書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Herdr.Handle(herdr.MethodAgentPrompt, func(params map[string]any) (any, *rpcErr) {
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": params["target"], "agent_status": "working"},
		}, nil
	})

	fx.Orc.Tick(context.Background())
	waitFor(t, 5*time.Second, "agent が起動する", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentStart) > 0
	})

	settingsPath := filepath.Join(fx.RuntimeDir, "issues", "maimuzo-koetsumugi-188", "settings.json")
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("設定ファイルを読めない: %v", err)
	}
	var parsed struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("設定ファイルを JSON として解析できない: %v\n%s", err, raw)
	}

	wantHooks := []string{
		"Stop", "UserPromptSubmit", "SubagentStop", "SubagentStart",
		"Notification", "SessionStart", "PreToolUse", "PostToolUse",
	}
	for _, name := range wantHooks {
		entries, ok := parsed.Hooks[name]
		if !ok || len(entries) == 0 || len(entries[0].Hooks) == 0 {
			t.Fatalf("hook %s が設定ファイルに無い: %s", name, raw)
		}
		cmd := entries[0].Hooks[0].Command
		// **パスは shell の単一引用符で包む。**この文字列は Claude Code が shell で
		// 実行するので、引用しないとパスの空白でコマンド行が別の引数へ割れ、
		// hook が1つも届かなくなる。
		if !strings.Contains(cmd, "--socket '"+fx.SocketPath+"'") {
			t.Fatalf("hook %s のコマンド行に、引用した socket の絶対パスが無い: %q", name, cmd)
		}
		wantPending := filepath.Join(fx.RuntimeDir, "issues", "maimuzo-koetsumugi-188", "pending")
		if !strings.Contains(cmd, "--pending-dir '"+wantPending+"'") {
			t.Fatalf("hook %s のコマンド行に、引用した逃がし先の絶対パスが無い: %q", name, cmd)
		}
		if (name == "PreToolUse" || name == "PostToolUse") && entries[0].Matcher != "*" {
			t.Fatalf("%s の matcher を絞っている（メインが叩いた Bash の記録が落ちる）: %q",
				name, entries[0].Matcher)
		}
	}
	if len(parsed.Hooks) != len(wantHooks) {
		t.Fatalf("hook の数が想定と違う: got %d, want %d", len(parsed.Hooks), len(wantHooks))
	}
	if parsed.Env["CLAUDE_CODE_RETRY_WATCHDOG"] != "1" {
		t.Fatalf("環境変数が設定ファイルの env に入っていない: %v", parsed.Env)
	}
	if len(parsed.Permissions.Allow) == 0 {
		t.Fatalf("permissions.allow が空である: %s", raw)
	}

	// **agent.start に env を渡していない**（渡す手段が無い。設計 3-12）。
	startParams := fx.Herdr.ParamsOf(t, herdr.MethodAgentStart)
	if _, ok := startParams["env"]; ok {
		t.Fatalf("agent.start に env を渡している: %v", startParams)
	}
	args, _ := startParams["args"].([]any)
	joined := joinAny(args)
	for _, want := range []string{"--settings", settingsPath, "--session-id", "session-1", "--permission-mode", "dontAsk"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("起動フラグに %q が無い: %v", want, args)
		}
	}
}

// indexOf は並びの中で target が最初に現れる位置を返す（無ければ -1）。
func indexOf(list []string, target string) int {
	for i, v := range list {
		if v == target {
			return i
		}
	}
	return -1
}

// filterMethods は want に含まれるメソッド名だけを、呼ばれた順に抜き出す。
//
// **同じメソッドが複数回呼ばれても最初の1回だけを残す**（順番の検査に使う）。
func filterMethods(got, want []string) []string {
	allowed := map[string]bool{}
	for _, w := range want {
		allowed[w] = true
	}
	seen := map[string]bool{}
	out := []string{}
	for _, g := range got {
		if allowed[g] && !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	return out
}

// equalStrings は2つの並びが同じかを返す。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// joinAny は any の並びを空白でつないだ文字列にする（起動フラグの検査に使う）。
func joinAny(items []any) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		s, _ := it.(string)
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}
