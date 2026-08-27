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
		// **知らない値を黙って既定に丸めない**（設計 3-49）。`halt` と書いた利用者は
		// 「止まる」つもりでいる。丸めた側が偶然一致しても、次に `continue` と書いたときには
		// **飛ばすつもりが止まる。**書いた値が効いていないことに気づけない。
		{"壊れた worktree の扱いが知らない値", "on_broken_worktree",
			"  on_broken_worktree: halt", "workspace.on_broken_worktree"},
		{"壊れた worktree の扱いが空", "on_broken_worktree",
			`  on_broken_worktree: ""`, "workspace.on_broken_worktree"},
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

// TestValidate_書き戻しの対応表の値を弾く は、設計 3-54 の検査を確かめる。
//
// **`automated_state_rewrite` は「自動化が書いた Status → 戻す先の Status」の対応表である。**
// **キーと値が同じだと1バイトも動かない。**同じ値の書き込みは省かれるので（設計 3-53）、
// 知らない Status のまま巡回のたびに書きに行き続ける。**空文字も Status 名として存在しない。**
//
// 目的: 使えない対応表を、起動する前に弾くこと。
// 与える情報: `automated_state_rewrite` の1行だけを差し替えた WORKFLOW.md。
// 成功条件: エラーになり、キーの名前がエラーの文面に入っていること。
func TestValidate_書き戻しの対応表の値を弾く(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{"戻す先が空", `  automated_state_rewrite: {"In Progress": ""}`},
		{"キーが空", `  automated_state_rewrite: {"": "In Progress"}`},
		{"キーと戻す先が同じ", `  automated_state_rewrite: {"In Progress": "In Progress"}`},
		{"キーと戻す先が大文字小文字だけ違う", `  automated_state_rewrite: {"In Progress": "in progress"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := loadWithReplaced(t, "automated_state_rewrite", tc.line)
			if err == nil {
				t.Fatalf("%s を弾いていない", tc.name)
			}
			if !strings.Contains(err.Error(), "automated_state_rewrite") {
				t.Errorf("どのキーが悪いか分からない: %v", err)
			}
		})
	}
}

// TestValidate_書き戻しの対応表は空でも書いてあっても通る は、設計 3-54 を確かめる。
//
// **既定は空である。**書かなければ挙動は変わらないので、既存の WORKFLOW.md をそのまま使える。
//
// 目的: 空の対応表と、正しく書いた対応表の両方が通ること。
// 与える情報: 雛形そのまま（空）と、1件書いた対応表。
// 成功条件: どちらも読めて、読んだ値がそのまま入っていること。
func TestValidate_書き戻しの対応表は空でも書いてあっても通る(t *testing.T) {
	if err := loadWithReplaced(t, "automated_state_rewrite", `  automated_state_rewrite: {}`); err != nil {
		t.Fatalf("空の対応表で起動が止まった: %v", err)
	}
	if err := loadWithReplaced(t, "automated_state_rewrite",
		`  automated_state_rewrite: {"Todo": "Ready"}`); err != nil {
		t.Fatalf("正しく書いた対応表で起動が止まった: %v", err)
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

// TestValidate_branch_templateにissueの番号が無ければ弾く は、branch 名の衝突を防ぐ検査を確かめる。
//
// **番号が入っていないと、issue が違っても同じ branch 名になる。**
// そのとき `continuo abandon <issue>` は、worktree が無い経路で「規則から組み立てた名前」
// だけを頼りに消す相手を決めるので、**名指しされた issue とは別の issue の branch を消す。**
//
// 目的: `herdr.worktree.branch_template` に `.issue.number` が無ければ、起動する前に弾くこと。
// 与える情報: `{{.issue.number}}` を落とした branch_template を書いた WORKFLOW.md。
// 成功条件: エラーになり、キーの名前と `.issue.number` が文面に入っていること。
func TestValidate_branch_templateにissueの番号が無ければ弾く(t *testing.T) {
	err := loadWithReplaced(t, "branch_template",
		`    branch_template: "continuo/{{.issue.owner}}/{{.issue.repo}}"`)
	if err == nil {
		t.Fatal("issue の番号が入っていない branch_template を通してしまった")
	}
	if !strings.Contains(err.Error(), "herdr.worktree.branch_template") {
		t.Errorf("どのキーが悪いか分からない: %v", err)
	}
	if !strings.Contains(err.Error(), ".issue.number") {
		t.Errorf("何を足せばよいのか分からない: %v", err)
	}
}

// TestValidate_branch_templateにissueの番号があれば通す は、上の検査が効きすぎていないことを確かめる。
//
// **番号さえ入っていれば、並べ方は利用者の自由である。**
// 番号を含む書き方まで弾くと、既に設定している利用者が起動できなくなる。
//
// 目的: `.issue.number` を含む branch_template を通すこと。
// 与える情報: owner も repo も使わず、番号だけを使う branch_template。
// 成功条件: エラーにならないこと。
func TestValidate_branch_templateにissueの番号があれば通す(t *testing.T) {
	if err := loadWithReplaced(t, "branch_template",
		`    branch_template: "issue-{{.issue.number}}"`); err != nil {
		t.Fatalf("issue の番号が入っている branch_template を弾いてしまった: %v", err)
	}
}

// TestResolvePath_ディレクトリを渡したら中の設定ファイルを見る は、パスの解決を確かめる。
//
// **`continuo init <ディレクトリ>` がディレクトリを取るので、他のサブコマンドも揃える。**
// 揃っていなかった版では、`continuo doctor <ディレクトリ>` が
// 「is a directory」で落ち、**設定を読まないまま検査が進んでいた。**
// そのとき言語の設定も効かず、環境変数の言語で結果が出ていた
// （手元は英語、CI は未設定だったので、CI でだけ落ちて見つかった）。
//
// 目的: ディレクトリを渡したら、その中の WORKFLOW.md を指すこと。
// 与える情報: WORKFLOW.md を1つ置いたディレクトリ。
// 成功条件: 解決したパスがそのファイルを指すこと。
func TestResolvePath_ディレクトリを渡したら中の設定ファイルを見る(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "WORKFLOW.md")
	if err := os.WriteFile(want, []byte("---\n---\n"), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を置けません: %v", err)
	}

	got, err := config.ResolvePath(dir, dir)
	if err != nil {
		t.Fatalf("解決できません: %v", err)
	}
	if got != want {
		t.Errorf("ディレクトリの中の設定ファイルを指していません: %s（期待 %s）", got, want)
	}
}

// TestResolvePath_ファイルを渡したらそのまま使う は、既存の使い方が壊れていないことを確かめる。
//
// 目的: ファイルのパスを渡したら、そのまま返すこと。
// 与える情報: 実在するファイルのパス。
// 成功条件: 渡したパスがそのまま返ること。
func TestResolvePath_ファイルを渡したらそのまま使う(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "別の名前.md")
	if err := os.WriteFile(p, []byte("---\n---\n"), 0o600); err != nil {
		t.Fatalf("ファイルを置けません: %v", err)
	}

	got, err := config.ResolvePath(p, dir)
	if err != nil {
		t.Fatalf("解決できません: %v", err)
	}
	if got != p {
		t.Errorf("渡したパスをそのまま使っていません: %s（期待 %s）", got, p)
	}
}

// TestResolvePath_存在しないパスはそのまま返す は、「無い」ことの扱いを確かめる。
//
// **存在しないことを、ここでエラーにしてはならない。**
// `continuo doctor` は「設定ファイルが無い」ことも検査結果の1件として報告する。
//
// 目的: 存在しないパスを、そのまま返すこと。
// 与える情報: 実在しないパス。
// 成功条件: エラーにならず、渡したパスが返ること。
func TestResolvePath_存在しないパスはそのまま返す(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "無い.md")

	got, err := config.ResolvePath(p, dir)
	if err != nil {
		t.Fatalf("存在しないことをエラーにしています: %v", err)
	}
	if got != p {
		t.Errorf("渡したパスをそのまま返していません: %s", got)
	}
}
