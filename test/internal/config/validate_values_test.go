// front matter の値の検証の検査である。
//
// **`yaml.Strict()` が未知のキーを弾いたあと、値そのものが使えるかをここで見る。**
// **使えない値のまま起動を通すと、issue に着手してから初めて落ちる。**
// そのときには worktree も pane も作ったあとで、人間には何が悪いのか分からない。
package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/scaffold"
)

// loadWithReplaced は雛形の1行だけを差し替えて読み込む。
//
// t: 呼び出し元のテスト。
// key: 差し替える行のキー（`project_number` など。行頭の空白は無視して探す）。
// line: 差し替え後の行（インデントを含む全文）。
// 戻り値: config.Load の返したエラー。
func loadWithReplaced(t *testing.T, key, line string) error {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")

	var b strings.Builder
	replaced := false
	// **雛形はプレースホルダのままでは検証を通らない。**owner とボードの番号を埋めてから、
	// 見たい1行だけを壊す（そうしないと、常にプレースホルダのエラーが先に返る）。
	base := scaffold.TemplateWithValues(scaffold.Values{Owner: "octocat", ProjectNumber: 3})
	for _, l := range strings.Split(base, "\n") {
		if !replaced && strings.HasPrefix(strings.TrimLeft(l, " \t"), key+":") {
			b.WriteString(line + "\n")
			replaced = true
			continue
		}
		b.WriteString(l + "\n")
	}
	if !replaced {
		t.Fatalf("差し替える行が見つからない: %s", key)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	_, err := config.Load(path)
	return err
}

// TestValidate_使えない値は起動する前に弾く は、値の検証を確かめる。
//
// **どれも「起動はできるが、着手してから落ちる」形の誤りである。**
// **設定を読んだ時点で弾かないと、worktree と pane を作ったあとで初めて分かる。**
//
// 目的: 表に並べた値をそれぞれ弾き、**どのキーが悪いかをエラーに入れること。**
// 与える情報: 1行だけを壊した WORKFLOW.md。
// 成功条件: エラーになり、キーの名前がエラーの文面に入っていること。
func TestValidate_使えない値は起動する前に弾く(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		line string
		want string
	}{
		{"ボードの番号が0", "project_number", "    project_number: 0", "project_number"},
		{"ボードの番号が負", "project_number", "    project_number: -1", "project_number"},
		{"Status フィールドの名前が空", "status_field", `    status_field: ""`, "status_field"},
		{"owner が空", "owner", `    owner: ""`, "owner"},
		{"tracker.kind が知らない値", "kind", "  kind: gitlab_boards", "tracker.kind"},
		{"running_state が空", "running_state", `  running_state: ""`, "running_state"},
		{"dispatch_state が空", "dispatch_state", `  dispatch_state: ""`, "dispatch_state"},
		{"failure_state が空", "failure_state", `  failure_state: ""`, "failure_state"},
		{"active_states が空", "active_states", "  active_states: []", "active_states"},
		{"terminal_states が空", "terminal_states", "  terminal_states: []", "terminal_states"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := loadWithReplaced(t, tc.key, tc.line)
			if err == nil {
				t.Fatalf("%s を弾いていない", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("どのキーが悪いか分からない: %v", err)
			}
		})
	}
}

// TestValidate_running_stateはactive_statesに入っていること は、整合の検査を確かめる。
//
// **`running_state` は「着手したときに書く Status」である。**
// **`active_states`（巡回で拾う Status）に入っていないと、着手した瞬間に候補から外れ、
// 誰も面倒を見なくなる。**
//
// 目的: `running_state` が `active_states` に無ければ弾くこと。
// 与える情報: `active_states` から `In Progress` を抜いた設定。
// 成功条件: エラーになること。
func TestValidate_running_stateはactive_statesに入っていること(t *testing.T) {
	err := loadWithReplaced(t, "active_states", `  active_states: ["Ready"]`)
	if err == nil {
		t.Fatal("running_state が active_states に無いのに通してしまった")
	}
	if !strings.Contains(err.Error(), "running_state") && !strings.Contains(err.Error(), "active_states") {
		t.Errorf("何が食い違っているか分からない: %v", err)
	}
}

// TestValidate_数値の範囲を外れたら弾く は、時間と件数の検査を確かめる。
//
// **0 や負の値は「即座に期限切れ」や「1件も着手しない」を意味する。**
// **設定の書き間違いなのに、動きとしては「静かに何もしない」になる。**
//
// 目的: 範囲外の数値を弾くこと。
// 与える情報: 0 や負の値を入れた設定。
// 成功条件: エラーになり、キーの名前が入っていること。
func TestValidate_数値の範囲を外れたら弾く(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		line string
		want string
	}{
		{"同時実行数が0", "max_concurrent_agents", "  max_concurrent_agents: 0", "max_concurrent_agents"},
		{"同時実行数が負", "max_concurrent_agents", "  max_concurrent_agents: -1", "max_concurrent_agents"},
		{"巡回の間隔が0", "poll_interval_ms", "  poll_interval_ms: 0", "poll_interval_ms"},
		{"指示の上限が0", "max_dispatch_turns", "  max_dispatch_turns: 0", "max_dispatch_turns"},
		{"枠の閾値が101", "pause_above_percent", "  pause_above_percent: 101", "pause_above_percent"},
		{"枠の閾値が負", "pause_above_percent", "  pause_above_percent: -1", "pause_above_percent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := loadWithReplaced(t, tc.key, tc.line)
			if err == nil {
				t.Fatalf("%s を弾いていない", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("どのキーが悪いか分からない: %v", err)
			}
		})
	}
}

// TestValidate_知らない選択肢を弾く は、列挙の検査を確かめる。
//
// 目的: 決められた値のどれでもない文字列を弾くこと。
// 与える情報: 存在しない値を入れた設定。
// 成功条件: エラーになり、キーの名前が入っていること。
func TestValidate_知らない選択肢を弾く(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		line string
		want string
	}{
		{"枠の出所が知らない値", "source", "  source: some_other_api", "rate_limit.source"},
		{"言語が知らない値", "language", "language: fr", "language"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := loadWithReplaced(t, tc.key, tc.line)
			if err == nil {
				t.Fatalf("%s を弾いていない", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("どのキーが悪いか分からない: %v", err)
			}
		})
	}
}
