package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/workspace"
)

// 目的: `--id` を付けた continuo が、自分の置き場所に名乗りを置くことを確かめる
// （設計 3-17b）。
//
// **これが無いと、既定側の `continuo abandon` が二度と何も片付けられなくなる。**
//
// 与える情報: `InstanceID` に `e2e` を渡した Manager。
// 成功条件: 置き場所の直下に目印のファイルがあり、中身の `id` が `e2e` であること。
func TestNew_idを付けると置き場所に名乗りを置く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{InstanceID: "e2e"})

	marker := filepath.Join(fx.Manager.ResolvedRoot(), workspace.InstanceMarkerName)
	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("名乗り %s を読めません: %v", marker, err)
	}
	if !strings.Contains(string(raw), `"id": "e2e"`) {
		t.Fatalf("名乗りに名前が入っていない: %s", raw)
	}
}

// 目的: `NoCreate` を渡したときに、置き場所も名乗りも作らないことを確かめる
// （設計 3-17g）。
//
// **`continuo abandon --dry-run` がこれを渡す。**あちらは「何も書かない」と
// README で約束している。**打ち間違えた `--id` の置き場所に名乗りが残ると、
// そこが既定側の走査から永久に隠れる。**
//
// 与える情報: まだ無い `workspace.root` と `InstanceID`、そして `NoCreate`。
// 成功条件: 置き場所も名乗りも作られず、走査が0件で返ること。
func TestNew_NoCreateなら置き場所も名乗りも作らない(t *testing.T) {
	root := filepath.Join(t.TempDir(), "worktrees", "typo")
	cfg := *config.DefaultConfig()
	cfg.Workspace.Root = root

	mgr, err := workspace.New(workspace.Options{
		Config:     cfg,
		HomeDir:    t.TempDir(),
		InstanceID: "typo",
		NoCreate:   true,
	})
	if err != nil {
		t.Fatalf("workspace.New に失敗した: %v", err)
	}

	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("置き場所 %s を作っている（err=%v）", root, err)
	}
	if _, err := os.Lstat(filepath.Join(root, workspace.InstanceMarkerName)); !os.IsNotExist(err) {
		t.Fatalf("名乗りを書いている: %s", filepath.Join(root, workspace.InstanceMarkerName))
	}

	// **置き場所が無いなら worktree は0件である。**エラーにしない（設計 3-17g）。
	found, err := mgr.ScanUnidentified()
	if err != nil {
		t.Fatalf("置き場所が無いだけで走査がエラーになった: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("置き場所が無いのに worktree を数えている: %v", found)
	}
}

// registerInstance は `~/.continuo/id/<名前>/` を作る。
//
// **`--id <名前>` を使った continuo は必ずこれを持つ**（`instance.Layout.EnsureLockDir` が
// ロックを置く前に作る）。走査はこの実在を、名乗りの裏付けとして見る（設計 3-17f）。
//
// t: 呼び出し元のテスト。
// home: Manager に渡したホームディレクトリ。
// id: `--id` に渡された名前。
func registerInstance(t *testing.T, home, id string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(workspace.InstanceRegistryDir(home), id), 0o700); err != nil {
		t.Fatalf("~/.continuo/id/%s を作れません: %v", id, err)
	}
}

// 目的: 既定の continuo が、`--id` を付けた continuo の置き場所へ入らないことを確かめる
// （設計 3-17f）。
//
// **`--id e2e` の worktree は `<workspace.root>/e2e/<host>/<owner>/<repo>/<スラグ>` にある。**
// 既定側の走査は `<workspace.root>` からちょうど4階層を返すので、名乗りが無ければ
// `<workspace.root>/e2e/<host>/<owner>/<repo>` が「身元ファイルの無いディレクトリ」として
// 拾われ、**`continuo abandon` が判断を保留したまま止まる。**
//
// 与える情報: `<root>/e2e/github.com/octocat/hello-world` の階層と、名乗りの有無。
// 成功条件: 名乗りが無ければ拾い、名乗りと `~/.continuo/id/e2e/` が揃えば1件も拾わないこと。
func TestScanUnidentified_別のinstanceの置き場所へは入らない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	root := fx.Manager.ResolvedRoot()

	other := filepath.Join(root, "e2e")
	repoDir := filepath.Join(other, "github.com", "octocat", "hello-world")
	if err := os.MkdirAll(filepath.Join(repoDir, "continuo-octocat-hello-world-188"), 0o700); err != nil {
		t.Fatalf("別の instance の置き場所を作れません: %v", err)
	}

	// **名乗りを置く前は拾う。**ここが空だと、この検査は何も守っていない。
	before, err := fx.Manager.ScanUnidentified()
	if err != nil {
		t.Fatalf("ScanUnidentified に失敗した: %v", err)
	}
	if len(before) != 1 || before[0] != repoDir {
		t.Fatalf("名乗りが無いのに拾っていない（検査が空振りしている）: %v", before)
	}

	if err := workspace.WriteInstanceMarker(other, "e2e"); err != nil {
		t.Fatalf("名乗りを書けません: %v", err)
	}
	registerInstance(t, fx.Home, "e2e")

	after, err := fx.Manager.ScanUnidentified()
	if err != nil {
		t.Fatalf("ScanUnidentified に失敗した: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("別の instance の置き場所を数えている: %v", after)
	}
}

// 目的: 名乗りの `id` が置き場所の名前と食い違うときは飛ばさないことを確かめる
// （設計 3-17f）。
//
// **worktree の中ではエージェントが `--permission-mode dontAsk` で動く。**
// 名乗りはそこから書けるので、**中身を見ずに飛ばすと、1バイト置くだけで
// `continuo abandon` の目から隠せる。**
//
// 与える情報: ディレクトリ名と食い違う `id` を書いた名乗り。
// 成功条件: いつもどおり数えること。
func TestScanUnidentified_名前の食い違う名乗りでは飛ばさない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	root := fx.Manager.ResolvedRoot()

	other := filepath.Join(root, "e2e")
	repoDir := filepath.Join(other, "github.com", "octocat", "hello-world")
	if err := os.MkdirAll(repoDir, 0o700); err != nil {
		t.Fatalf("別の instance の置き場所を作れません: %v", err)
	}
	// **`e2e` ではなく `other` と名乗らせる。**
	if err := workspace.WriteInstanceMarker(other, "other"); err != nil {
		t.Fatalf("名乗りを書けません: %v", err)
	}
	registerInstance(t, fx.Home, "other")
	registerInstance(t, fx.Home, "e2e")

	found, err := fx.Manager.ScanUnidentified()
	if err != nil {
		t.Fatalf("ScanUnidentified に失敗した: %v", err)
	}
	if len(found) != 1 || found[0] != repoDir {
		t.Fatalf("名前が食い違う名乗りで飛ばしてしまった: %v", found)
	}
}

// 目的: worktree の中から書いた名乗りだけでは、worktree を隠せないことを確かめる
// （設計 3-17f）。
//
// **エージェントは `--permission-mode dontAsk` で、worktree の中に居る。**
// **`../../../.continuo-instance` に `{"id":"github.com"}` を書くだけで、
// `github.com` の下の worktree が `Scan` / `ScanUnidentified` / `ScanBroken` の
// 3つ全部から消えていた。**そうなると復元は0件になり、`continuo abandon` は
// **「worktree が無い」経路に入って、生きている worktree の branch を消しにいく。**
//
// 与える情報: `<root>/github.com/.continuo-instance` に `{"id":"github.com"}` を書いた状態
// （`~/.continuo/id/github.com/` も作って、裏付けの検査だけに頼っていないことを示す）。
// 成功条件: 3つの走査がどれも、いつもどおり数えること。
func TestScan_ホスト名を名乗る名乗りでは隠せない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	root := fx.Manager.ResolvedRoot()

	hostDir := filepath.Join(root, "github.com")
	worktree := filepath.Join(hostDir, "octocat", "hello-world", "continuo-octocat-hello-world-188")
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		t.Fatalf("worktree を作れません: %v", err)
	}
	// **エージェントが worktree の中から相対パスで書ける先である。**
	if err := workspace.WriteInstanceMarker(hostDir, "github.com"); err != nil {
		t.Fatalf("名乗りを書けません: %v", err)
	}
	// **裏付けの側も揃えてみせる。**それでも `--id` に書けない名前なので飛ばさない。
	registerInstance(t, fx.Home, "e2e")
	if err := os.MkdirAll(filepath.Join(workspace.InstanceRegistryDir(fx.Home), "github.com"), 0o700); err != nil {
		t.Fatalf("~/.continuo/id/github.com を作れません: %v", err)
	}

	found, err := fx.Manager.ScanUnidentified()
	if err != nil {
		t.Fatalf("ScanUnidentified に失敗した: %v", err)
	}
	if len(found) != 1 || found[0] != worktree {
		t.Fatalf("ホスト名を名乗る名乗りで隠せてしまった: %v", found)
	}

	broken, err := fx.Manager.ScanBroken()
	if err != nil {
		t.Fatalf("ScanBroken に失敗した: %v", err)
	}
	if len(broken) != 1 || broken[0].Path != worktree {
		t.Fatalf("ScanBroken からも隠せてしまった: %v", broken)
	}
}

// 目的: `~/.continuo/id/<名前>/` が無ければ、名乗りがあっても飛ばさないことを確かめる
// （設計 3-17f）。
//
// **名乗りは `workspace.root` の中にあり、エージェントが書ける。**
// **置き場所の外に「その名前で continuo が動いた」証拠が要る。**
//
// 与える情報: 名乗りだけがあり、`~/.continuo/id/e2e/` が無い状態。
// 成功条件: いつもどおり数えること。
func TestScanUnidentified_置き場所の外に裏付けが無ければ飛ばさない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	root := fx.Manager.ResolvedRoot()

	other := filepath.Join(root, "e2e")
	repoDir := filepath.Join(other, "github.com", "octocat", "hello-world")
	if err := os.MkdirAll(repoDir, 0o700); err != nil {
		t.Fatalf("別の instance の置き場所を作れません: %v", err)
	}
	if err := workspace.WriteInstanceMarker(other, "e2e"); err != nil {
		t.Fatalf("名乗りを書けません: %v", err)
	}
	// **`~/.continuo/id/e2e/` は作らない。**

	found, err := fx.Manager.ScanUnidentified()
	if err != nil {
		t.Fatalf("ScanUnidentified に失敗した: %v", err)
	}
	if len(found) != 1 || found[0] != repoDir {
		t.Fatalf("裏付けが無いのに飛ばしてしまった: %v", found)
	}
}

// 目的: `--id` が足した `<名前>/` だけを手掛かりに孤児 branch の掃除を始めないことを
// 確かめる（設計 3-9 の手順6b / 3-17b）。
//
// **`branch_template` が変数で始まる設定では、掃除は止まっている**（接頭辞が空になる）。
// **`--id e2e` を付けると接頭辞が `e2e/` になり、掃除が動き出す。**
// そのまま消すと、**人間が自分で切った `e2e/spike` を `git branch -D` で消す。**
//
// 与える情報: `e2e/{{.issue.repo}}-{{.issue.number}}`（`instance.Layout.Apply` が
// `{{.issue.repo}}-{{.issue.number}}` に `e2e/` を足した形）と、`e2e/spike` の branch。
// 成功条件: `InstanceID` を渡したときは1本も消さず、渡さないときは消すこと
// （渡さないほうも確かめないと、この検査は何も守っていない）。
func TestSweepOrphanBranches_idが足した接頭辞では掃除を始めない(t *testing.T) {
	// **`instance.Layout.Apply` が作る形をそのまま書く**（設計 3-17b）。
	// Apply がこの形にすることは test/internal/instance が固定している。
	const applied = "e2e/{{.issue.repo}}-{{.issue.number}}"

	kept := newFixture(t, fixtureOptions{
		InstanceID: "e2e",
		Mutate: func(cfg *config.Config) {
			cfg.Herdr.Worktree.BranchTemplate = applied
		},
	})
	keptWorktree, err := kept.Manager.Prepare(context.Background(), sampleIssue(188))
	if err != nil {
		t.Fatalf("worktree を用意できません: %v", err)
	}
	runGit(t, kept.Repo.Dir, "branch", "e2e/spike")

	deleted, err := kept.Manager.SweepOrphanBranches(context.Background(),
		workspace.OrphanBranchSweepRequest{Worktrees: []string{keptWorktree.Path}})
	if err != nil {
		t.Fatalf("SweepOrphanBranches がエラーを返した: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("--id が足した接頭辞だけで branch を消した: %v", deleted)
	}
	branches := runGit(t, kept.Repo.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if !strings.Contains(branches, "e2e/spike") {
		t.Fatalf("人間が切った branch を消してしまった: %s", branches)
	}

	// **`--id` を付けていなければ `e2e/` は素直な接頭辞である。**
	// ここが消えないなら、上の検査は接頭辞の判定ではなく別の理由で通っている。
	swept := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) {
			cfg.Herdr.Worktree.BranchTemplate = applied
		},
	})
	sweptWorktree, err := swept.Manager.Prepare(context.Background(), sampleIssue(188))
	if err != nil {
		t.Fatalf("worktree を用意できません: %v", err)
	}
	runGit(t, swept.Repo.Dir, "branch", "e2e/spike")

	deleted, err = swept.Manager.SweepOrphanBranches(context.Background(),
		workspace.OrphanBranchSweepRequest{Worktrees: []string{sweptWorktree.Path}})
	if err != nil {
		t.Fatalf("SweepOrphanBranches がエラーを返した: %v", err)
	}
	if len(deleted) != 1 || !strings.HasSuffix(deleted[0], "e2e/spike") {
		t.Fatalf("--id が無いときは接頭辞として使うはずだが消していない（検査が空振りしている）: %v", deleted)
	}
}

// 目的: `BranchPrefixForSweep` の判定そのものを固定する（設計 3-9 の手順6b / 3-17b）。
//
// 与える情報: `--id` の有無と、変数で始まるテンプレート・固定の接頭辞を持つテンプレート。
// 成功条件: `--id` が足した `<名前>/` だけのときに空文字を返し、
// 元の接頭辞があるときは `<名前>/` を含んだ接頭辞を返すこと。
func TestBranchPrefixForSweep_idが足した分だけなら空を返す(t *testing.T) {
	cases := []struct {
		name       string
		tmpl       string
		instanceID string
		want       string
	}{
		{name: "id が無く固定の接頭辞がある", tmpl: "continuo/{{.issue.number}}", want: "continuo/"},
		{name: "id が無く変数で始まる", tmpl: "{{.issue.repo}}-{{.issue.number}}", want: ""},
		{name: "id が足した分だけなら空", tmpl: "e2e/{{.issue.repo}}", instanceID: "e2e", want: ""},
		{name: "id の下に固定の接頭辞がある", tmpl: "e2e/continuo/{{.issue.number}}", instanceID: "e2e", want: "e2e/continuo/"},
		{name: "変数が1つも無ければ空", tmpl: "e2e/continuo-fixed", instanceID: "e2e", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := workspace.BranchPrefixForSweep(tc.tmpl, tc.instanceID); got != tc.want {
				t.Fatalf("接頭辞が違う: got %q, want %q", got, tc.want)
			}
		})
	}
}
