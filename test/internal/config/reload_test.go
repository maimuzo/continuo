package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
)

// 走行中の設定として使える最小の設定を作る。
//
// **DefaultConfig() は雛形なので、必須のキーが空である**（`tracker.provider.owner` など）。
// 検査を通る値を埋めてから使う。
func 走行中の設定(t *testing.T) config.Config {
	t.Helper()
	cfg := *config.DefaultConfig()
	cfg.Tracker.Provider.Owner = "octocat"
	cfg.Tracker.Provider.ProjectNumber = 3
	cfg.Tracker.Provider.StatusField = "Status"
	cfg.Workspace.Root = "/tmp/continuo-test-worktrees"
	return cfg
}

// Test読み直せる設定は差し替えたキーだけを入れ替える は、走行中に差し替えてよいキーだけが
// 新しい設定から写ることを確かめる。
//
// 目的: 「読み直せる4キー以外は、走っている continuo では絶対に変わらない」を固定する。
// 与える情報: いま効いている設定と、4キーと凍結側のキーの両方を変えた新しい設定。
// 成功条件: 4キーは新しい値になり、凍結側のキーは古い値のまま残る。
func Test読み直せる設定は差し替えたキーだけを入れ替える(t *testing.T) {
	old := 走行中の設定(t)
	old.Tracker.Provider.Handoff.OnAssigneeGate = config.OnAssigneeGateWarnAndComment
	old.Agent.MaxConcurrentAgents = 2
	old.Polling.IntervalMs = 30000

	next := old
	next.Tracker.Provider.Handoff.OnAssigneeGate = config.OnAssigneeGateWarnOnly
	next.Agent.MaxConcurrentAgents = 5
	next.Agent.MaxConcurrentAgentsByState = map[string]int{"In Progress": 3}
	next.Tracker.AutomatedStateRewrite = map[string]string{"AI Review": "In Progress"}
	// **凍結側。**これは古い値のまま残らなければならない。
	next.Polling.IntervalMs = 1000

	merged, err := config.MergeReloadable(old, next)
	if err != nil {
		t.Fatalf("混ぜられなかった: %v", err)
	}
	if merged.Tracker.Provider.Handoff.OnAssigneeGate != config.OnAssigneeGateWarnOnly {
		t.Errorf("on_assignee_gate が差し替わっていない: %q", merged.Tracker.Provider.Handoff.OnAssigneeGate)
	}
	if merged.Agent.MaxConcurrentAgents != 5 {
		t.Errorf("max_concurrent_agents が差し替わっていない: %d", merged.Agent.MaxConcurrentAgents)
	}
	if merged.Agent.MaxConcurrentAgentsByState["In Progress"] != 3 {
		t.Errorf("max_concurrent_agents_by_state が差し替わっていない: %v", merged.Agent.MaxConcurrentAgentsByState)
	}
	if merged.Tracker.AutomatedStateRewrite["AI Review"] != "In Progress" {
		t.Errorf("automated_state_rewrite が差し替わっていない: %v", merged.Tracker.AutomatedStateRewrite)
	}
	if merged.Polling.IntervalMs != 30000 {
		t.Errorf("凍結側の polling.interval_ms が差し替わってしまった: %d", merged.Polling.IntervalMs)
	}
}

// Test混ぜた結果が検査に落ちたら差し替えない は、どちらのファイル単体でも通る値の組み合わせが、
// 混ぜたときだけ不正になる場合にエラーを返すことを確かめる。
//
// 目的: 「混ぜた結果は、どちらのファイルとしても存在しない第3の組み合わせである」を固定する。
// 与える情報: 書き戻しの対応表のキーが、いま効いている設定の Status 名と衝突する新しい設定。
// 成功条件: エラーになり、混ぜた結果を返さない。
func Test混ぜた結果が検査に落ちたら差し替えない(t *testing.T) {
	old := 走行中の設定(t)

	next := old
	// **単体では通る。**新しいファイルでは Blocked を failure_state から外している想定にする。
	// ここでは「いま効いている設定に名前が出てくる Status」をキーにして、混ぜたときだけ落とす。
	next.Tracker.AutomatedStateRewrite = map[string]string{old.Tracker.FailureState: old.Tracker.RunningState}

	if _, err := config.MergeReloadable(old, next); err == nil {
		t.Fatalf("混ぜた結果が検査に落ちるはずが、通ってしまった")
	}
}

// Test効かない項目はキーの道筋で出る は、凍結側のキーを変えたときに、
// ドット区切りのキーと前後の値が並ぶことを確かめる。
//
// 目的: 受け入れ条件「読み直しただけでは効かない項目を変えたとき、その項目名と前後の値が出る」を固定する。
// 与える情報: 混ぜた結果（古い値のまま）と、凍結側を変えた新しい設定。
// 成功条件: 変えた凍結側のキーがドット区切りで並び、前後の値が入る。差し替えたキーは並ばない。
func Test効かない項目はキーの道筋で出る(t *testing.T) {
	old := 走行中の設定(t)
	next := old
	next.Polling.IntervalMs = 1000
	next.Agent.MaxConcurrentAgents = 9 // 差し替えられる側。並んではいけない

	merged, err := config.MergeReloadable(old, next)
	if err != nil {
		t.Fatalf("混ぜられなかった: %v", err)
	}
	changes, err := config.FrozenChanges(merged, next)
	if err != nil {
		t.Fatalf("差分を作れなかった: %v", err)
	}

	var found *config.FrozenChange
	for i := range changes {
		if changes[i].Key == "agent.max_concurrent_agents" {
			t.Errorf("差し替えられる側のキーが「効かない項目」に並んでいる: %v", changes[i])
		}
		if changes[i].Key == "polling.interval_ms" {
			found = &changes[i]
		}
	}
	if found == nil {
		t.Fatalf("polling.interval_ms が並んでいない: %v", changes)
	}
	if found.From != "30000" || found.To != "1000" {
		t.Errorf("前後の値が入っていない: %+v", *found)
	}
}

// Test中身が同じなら差分は出ない は、何も変えていない設定で「効かない項目」が空になることを確かめる。
//
// 目的: map の並び順が実行のたびに変わっても、変えていない項目が「変わった」と出ないことを固定する。
// 与える情報: map を4つとも埋めた同じ設定を2つ。
// 成功条件: 差分が0件である。
func Test中身が同じなら差分は出ない(t *testing.T) {
	cfg := 走行中の設定(t)
	cfg.Claude.Env = map[string]string{"A": "1", "B": "2", "C": "3"}
	cfg.Agent.MaxConcurrentAgentsByState = map[string]int{"In Progress": 1, "Ready": 2}

	for i := 0; i < 20; i++ {
		changes, err := config.FrozenChanges(cfg, cfg)
		if err != nil {
			t.Fatalf("差分を作れなかった: %v", err)
		}
		if len(changes) != 0 {
			t.Fatalf("同じ設定なのに差分が出た: %v", changes)
		}
	}
}

// Test読み直せる設定に hook の経路のキーを入れない は、hook の宛先を決めるキーが
// 走行中に差し替えられないことを機械的に止める。
//
// 目的: 「動いている hook の設定と本体の版が食い違わない」という安全の根拠を、人の目に頼らない。
// 与える情報: hook の socket のパスを決めるキーだけを変えた新しい設定。
// 成功条件: 混ぜた結果でも、そのキーは古い値のまま残る。
func Test読み直せる設定に_hook_の経路のキーを入れない(t *testing.T) {
	old := 走行中の設定(t)
	listen := "/tmp/old-continuo/hooks.sock"
	old.Claude.HookBridge.Listen = &listen

	next := old
	other := "/tmp/new-continuo/hooks.sock"
	next.Claude.HookBridge.Listen = &other

	merged, err := config.MergeReloadable(old, next)
	if err != nil {
		t.Fatalf("混ぜられなかった: %v", err)
	}
	if merged.Claude.HookBridge.Listen == nil || *merged.Claude.HookBridge.Listen != listen {
		t.Fatalf("claude.hook_bridge.listen が読み直しで動いた（hook の宛先が本体と食い違う）: %v",
			merged.Claude.HookBridge.Listen)
	}
}

// Test印は中身が同じなら変わらない は、更新時刻だけが動いたファイルを読み直さないことを確かめる。
//
// 目的: エディタが中身を変えずに保存し直したときに、読み直しが走らないことを固定する。
// 与える情報: 同じ中身で2回書いたファイル。
// 成功条件: 2つの印が「同じ」と判定される。
func Test印は中身が同じなら変わらない(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(path, []byte("---\nlanguage: ja\n---\n本文\n"), 0o600); err != nil {
		t.Fatalf("書けなかった: %v", err)
	}
	first, err := config.StampOf(path)
	if err != nil {
		t.Fatalf("印を取れなかった: %v", err)
	}
	if err := os.WriteFile(path, []byte("---\nlanguage: ja\n---\n本文\n"), 0o600); err != nil {
		t.Fatalf("書けなかった: %v", err)
	}
	second, err := config.StampOf(path)
	if err != nil {
		t.Fatalf("印を取れなかった: %v", err)
	}
	if !first.Same(second) {
		t.Errorf("中身が同じなのに違う印になった")
	}

	if err := os.WriteFile(path, []byte("---\nlanguage: en\n---\n本文\n"), 0o600); err != nil {
		t.Fatalf("書けなかった: %v", err)
	}
	third, err := config.StampOf(path)
	if err != nil {
		t.Fatalf("印を取れなかった: %v", err)
	}
	if first.Same(third) {
		t.Errorf("中身が違うのに同じ印になった")
	}
}

// Test本文が変わったことを知らせるキーがある は、報告に使うキーが空でないことを確かめる。
//
// 目的: 本文の変化を報告する行の見出しが、空のまま出ないようにする。
// 与える情報: なし。
// 成功条件: キーが空でなく、WORKFLOW.md の話だと読める。
func Test本文が変わったことを知らせるキーがある(t *testing.T) {
	if strings.TrimSpace(config.PromptBodyKey) == "" {
		t.Fatalf("本文の変化を知らせるキーが空である")
	}
	if !strings.Contains(config.PromptBodyKey, "WORKFLOW.md") {
		t.Errorf("キーから、どのファイルの話か読めない: %q", config.PromptBodyKey)
	}
}

// Test環境変数の値はログに出さない は、`claude.env` を書き換えたときに値が伏せられることを確かめる。
//
// 目的: 利用者が鍵を置く場所の値が、ログを貼り付けただけで外へ出ないようにする。
// 与える情報: `claude.env` の値だけを書き換えた設定。
// 成功条件: キーは `claude.env` で出て、前後の値がどちらも伏せられている。
func Test環境変数の値はログに出さない(t *testing.T) {
	old := 走行中の設定(t)
	old.Claude.Env = map[string]string{"ANTHROPIC_AUTH_TOKEN": "ひみつ-旧"}
	next := old
	next.Claude.Env = map[string]string{"ANTHROPIC_AUTH_TOKEN": "ひみつ-新"}

	changes, err := config.FrozenChanges(old, next)
	if err != nil {
		t.Fatalf("差分を作れなかった: %v", err)
	}
	var found bool
	for _, c := range changes {
		if strings.Contains(c.From, "ひみつ") || strings.Contains(c.To, "ひみつ") {
			t.Errorf("環境変数の値がそのまま出ている: %v", c)
		}
		if c.Key == "claude.env" {
			found = true
			if c.From == c.To && c.From == "" {
				t.Errorf("伏せた値が空になっている: %v", c)
			}
		}
		if strings.HasPrefix(c.Key, "claude.env.") {
			t.Errorf("環境変数の中へ降りてしまっている（伏せる表が引かれない）: %v", c)
		}
	}
	if !found {
		t.Errorf("claude.env が変わったことが出ていない: %v", changes)
	}
}
