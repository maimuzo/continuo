package trust_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/trust"
)

// 目的: --dry-run が「信頼のダイアログが見せていたはずの一覧」を出すことを確認する。
//
// **これが無いと、人間が中身を確かめる機会が消える**（設計 3-33）。
// 対象の `.claude/settings.json` の permissions.allow と permissions.additionalDirectories、
// `.mcp.json` の MCP サーバー名が、すべて出ていることを見る。
//
// 与える情報: 権限3つと追加ディレクトリ1つを要求する settings.json と、
// MCP サーバーを2つ宣言する .mcp.json を持つリポジトリ。
// 成功条件: Requirements にその全部が入り、出力にも全部が現れること。
func TestPlan_信頼すると何が効くようになるかを読み取る(t *testing.T) {
	repo := initRepo(t, "hello-world")
	writeJSON(t, filepath.Join(repo, ".claude", "settings.json"), `{
	  "permissions": {
	    "allow": ["Bash(rm:*)", "Read", "WebFetch"],
	    "deny": [],
	    "additionalDirectories": ["/etc", "~/secrets"]
	  }
	}`)
	writeJSON(t, filepath.Join(repo, ".mcp.json"), `{
	  "mcpServers": {
	    "payments": {"command": "node", "args": ["server.js", "--live"]},
	    "docs": {"url": "https://example.invalid/mcp", "type": "http"}
	  }
	}`)
	home, _ := fakeHome(t, `{"projects":{}}`)

	report, err := trust.Plan(context.Background(), trust.Options{
		Repositories: []string{"octocat/hello-world"},
		HomeDir:      home,
		ResolveClone: staticClones(map[string]string{"octocat/hello-world": repo}),
	})
	if err != nil {
		t.Fatalf("調べられなかった: %v", err)
	}
	if len(report.Entries) != 1 {
		t.Fatalf("列挙した1件だけが対象になっていない: %+v", report.Entries)
	}

	got := report.Entries[0]
	if got.Problem != "" {
		t.Fatalf("調べられない理由が出ている: %s", got.Problem)
	}
	if got.Trusted {
		t.Error("まだ登録していないのに信頼済みと判定している")
	}
	if want := trustKeyOf(t, repo); got.TrustKey != want {
		t.Errorf("信頼の鍵が git の答えと違う: got %q, want %q", got.TrustKey, want)
	}
	assertSameStrings(t, "permissions.allow",
		[]string{"Bash(rm:*)", "Read", "WebFetch"}, got.Requirements.Allow)
	assertSameStrings(t, "permissions.additionalDirectories",
		[]string{"/etc", "~/secrets"}, got.Requirements.AdditionalDirectories)
	if len(got.Requirements.MCPServers) != 2 {
		t.Fatalf("MCP サーバーが2つ読めていない: %+v", got.Requirements.MCPServers)
	}
	// 名前の辞書順に並ぶ。
	if got.Requirements.MCPServers[0].Name != "docs" || got.Requirements.MCPServers[1].Name != "payments" {
		t.Errorf("MCP サーバーの名前が想定と違う: %+v", got.Requirements.MCPServers)
	}
	if !strings.Contains(got.Requirements.MCPServers[1].Summary, "node server.js --live") {
		t.Errorf("何が起動されるのかが読めていない: %+v", got.Requirements.MCPServers[1])
	}

	var out bytes.Buffer
	if err := trust.WriteRequirements(&out, report); err != nil {
		t.Fatalf("要求内容を書き出せなかった: %v", err)
	}
	for _, want := range []string{
		"octocat/hello-world", "Bash(rm:*)", "WebFetch", "/etc", "~/secrets", "docs", "payments",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("要求内容の出力に %q が出ていない:\n%s", want, out.String())
		}
	}
}

// 目的: 列挙されていないリポジトリを対象にしないことを確認する。
//
// **ボードは他人が編集できる。**issue を足せる人が信頼させるリポジトリを増やせないよう、
// 対象は `trust.repositories` に書かれたものだけに限る（設計 3-33）。
//
// 与える情報: clone が2つあるが、trust.repositories には片方しか書いていない状態。
// 成功条件: 調査結果が1件だけで、それが列挙したほうであること。
func TestPlan_列挙されていないリポジトリは対象にしない(t *testing.T) {
	listed := initRepo(t, "listed")
	unlisted := initRepo(t, "unlisted")
	home, _ := fakeHome(t, `{"projects":{}}`)

	report, err := trust.Plan(context.Background(), trust.Options{
		Repositories: []string{"octocat/listed"},
		HomeDir:      home,
		ResolveClone: staticClones(map[string]string{
			"octocat/listed":   listed,
			"octocat/unlisted": unlisted,
		}),
	})
	if err != nil {
		t.Fatalf("調べられなかった: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Repository != "octocat/listed" {
		t.Fatalf("列挙していないリポジトリまで対象になっている: %+v", report.Entries)
	}
}

// 目的: clone が無いリポジトリを、登録の対象から外して理由つきで返すことを確認する。
//
// **continuo は勝手に clone しない。**手元に無いものを信頼させることもしない。
//
// 与える情報: clone のパスを引けない（空文字が返る）リポジトリ。
// 成功条件: Actionable が偽で、理由に ghq のコマンドが出ること。Pending には入らないこと。
func TestPlan_cloneが無ければ登録の対象から外す(t *testing.T) {
	home, _ := fakeHome(t, `{"projects":{}}`)

	report, err := trust.Plan(context.Background(), trust.Options{
		Repositories: []string{"octocat/nowhere"},
		HomeDir:      home,
		ResolveClone: staticClones(map[string]string{}),
	})
	if err != nil {
		t.Fatalf("調べられなかった: %v", err)
	}
	got := report.Entries[0]
	if got.Actionable() {
		t.Fatalf("clone が無いのに登録の対象になっている: %+v", got)
	}
	if !strings.Contains(got.Problem, "ghq list -p -e octocat/nowhere") {
		t.Errorf("clone が無いことを引いたコマンドつきで説明していない: %q", got.Problem)
	}
	if len(report.Pending()) != 0 {
		t.Errorf("登録の対象に数えられている: %+v", report.Pending())
	}
	if len(report.Problems()) != 1 {
		t.Errorf("調べられなかった件として数えられていない: %+v", report.Problems())
	}
}

// 目的: 設定ファイルを持たないリポジトリでも、何も要求していないと分かる形で出ることを確認する。
//
// 与える情報: .claude/settings.json も .mcp.json も無いリポジトリ。
// 成功条件: Requirements.Empty が真で、出力に「ありません」と出ること。
func TestPlan_設定ファイルが無ければ何も要求していないと分かる形で出す(t *testing.T) {
	repo := initRepo(t, "plain")
	home, _ := fakeHome(t, `{"projects":{}}`)

	report, err := trust.Plan(context.Background(), trust.Options{
		Repositories: []string{"octocat/plain"},
		HomeDir:      home,
		ResolveClone: staticClones(map[string]string{"octocat/plain": repo}),
	})
	if err != nil {
		t.Fatalf("調べられなかった: %v", err)
	}
	if !report.Entries[0].Requirements.Empty() {
		t.Errorf("何も要求していないはずなのに要求内容が入っている: %+v", report.Entries[0].Requirements)
	}

	var out bytes.Buffer
	if err := trust.WriteRequirements(&out, report); err != nil {
		t.Fatalf("要求内容を書き出せなかった: %v", err)
	}
	if !strings.Contains(out.String(), ".claude/settings.json: ありません") {
		t.Errorf("設定ファイルが無いことを出していない:\n%s", out.String())
	}
}

// 目的: リポジトリの中の設定ファイルが symlink なら、中身を読まずに知らせることを確認する。
//
// **リポジトリの外にあるものを「このリポジトリの要求内容」として見せてはならない。**
// 見せてしまうと、人間は違うファイルを見て信頼の可否を判断することになる。
//
// 与える情報: .claude/settings.json がリポジトリの外を指す symlink であるリポジトリ。
// 成功条件: allow が読まれず、注意書きに symlink であることが出ること。
func TestPlan_設定ファイルがsymlinkなら読まずに知らせる(t *testing.T) {
	repo := initRepo(t, "linked")
	outside := filepath.Join(t.TempDir(), "outside-settings.json")
	writeJSON(t, outside, `{"permissions":{"allow":["Bash"]}}`)
	linkPath := filepath.Join(repo, ".claude", "settings.json")
	writeJSON(t, filepath.Join(repo, ".claude", "keep"), `{}`)
	if err := symlink(outside, linkPath); err != nil {
		t.Fatalf("symlink を作れなかった: %v", err)
	}
	home, _ := fakeHome(t, `{"projects":{}}`)

	report, err := trust.Plan(context.Background(), trust.Options{
		Repositories: []string{"octocat/linked"},
		HomeDir:      home,
		ResolveClone: staticClones(map[string]string{"octocat/linked": repo}),
	})
	if err != nil {
		t.Fatalf("調べられなかった: %v", err)
	}
	req := report.Entries[0].Requirements
	if len(req.Allow) != 0 {
		t.Errorf("symlink の先を読んでいる: %+v", req.Allow)
	}
	if !containsSubstring(req.Notes, "symlink") {
		t.Errorf("symlink であることを知らせていない: %v", req.Notes)
	}
}

// assertSameStrings は文字列の並びが期待どおりであることを確かめる。
//
// t: テストコンテキスト。
// label: 失敗メッセージに出す呼び名。
// want: 期待する並び。
// got: 実際の並び。
func assertSameStrings(t *testing.T, label string, want, got []string) {
	t.Helper()
	if strings.Join(want, "\x00") != strings.Join(got, "\x00") {
		t.Errorf("%s が想定と違う: got %v, want %v", label, got, want)
	}
}

// containsSubstring は文字列の一覧のどれかが、指定した部分文字列を含むかを返す。
//
// lines: 調べる文字列の一覧。
// sub: 含まれていてほしい部分文字列。
// 戻り値: どれか1つでも含んでいれば真。
func containsSubstring(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// TestPlan_cloneが無ければ取ってきて調べ直す は、
// FetchClone を渡したときに clone を取りに行くことを確かめる。
//
// 目的: 設計 3-22 の「continuo が勝手に clone しない」を `continuo trust` の
// 本番実行に限って解いたことを示す。**人間が ghq get を手で叩く手順を無くす。**
// 与える情報: 最初は clone が引けず、取得のあとだけ引けるようになる resolver。
// 成功条件: 取得が1回呼ばれ、調べられない理由が消え、信頼の鍵が求まる。
func TestPlan_cloneが無ければ取ってきて調べ直す(t *testing.T) {
	repo := initRepo(t, "hello-world")
	home, _ := fakeHome(t, `{"projects":{}}`)

	// **取得の前は空を返し、取得のあとだけパスを返す。**`ghq get` の前後を再現する。
	fetched := 0
	notified := ""
	resolve := func(_ context.Context, owner, name string) (string, error) {
		if fetched == 0 {
			return "", nil
		}
		return repo, nil
	}

	report, err := trust.Plan(context.Background(), trust.Options{
		Repositories: []string{"octocat/hello-world"},
		HomeDir:      home,
		ResolveClone: resolve,
		FetchClone: func(_ context.Context, owner, name string) error {
			fetched++
			return nil
		},
		OnFetch: func(repository string) { notified = repository },
	})
	if err != nil {
		t.Fatalf("調べられなかった: %v", err)
	}
	if fetched != 1 {
		t.Fatalf("clone の取得が %d 回呼ばれた（1回であるべき）", fetched)
	}
	if notified != "octocat/hello-world" {
		t.Errorf("取りに行くことを知らせていない: %q", notified)
	}
	got := report.Entries[0]
	if got.Problem != "" {
		t.Fatalf("取ってきたのに調べられない理由が出ている: %s", got.Problem)
	}
	if got.ClonePath != repo {
		t.Errorf("取ったあとの clone のパスが違う: got %q, want %q", got.ClonePath, repo)
	}
}

// TestPlan_FetchCloneを渡さなければ取りに行かない は、
// `--dry-run` が読むだけであることを確かめる。
//
// 目的: 読むだけのつもりで叩いた人のディスクを無断で使わないこと（設計 3-33）。
// 与える情報: clone を引けない resolver と、nil の FetchClone。
// 成功条件: 「--dry-run では取りに行きません」が理由に出る。
func TestPlan_FetchCloneを渡さなければ取りに行かない(t *testing.T) {
	home, _ := fakeHome(t, `{"projects":{}}`)

	report, err := trust.Plan(context.Background(), trust.Options{
		Repositories: []string{"octocat/hello-world"},
		HomeDir:      home,
		ResolveClone: staticClones(map[string]string{}),
	})
	if err != nil {
		t.Fatalf("調べられなかった: %v", err)
	}
	got := report.Entries[0]
	if !strings.Contains(got.Problem, "--dry-run では取りに行きません") {
		t.Errorf("取りに行かない理由が出ていない: %q", got.Problem)
	}
	if got.ClonePath != "" {
		t.Errorf("clone を引けないのにパスが入っている: %q", got.ClonePath)
	}
}
