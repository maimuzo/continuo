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
		t.Fatalf("目印 %s を読めません: %v", marker, err)
	}
	if !strings.Contains(string(raw), `"id": "e2e"`) {
		t.Fatalf("目印に名前が入っていない: %s", raw)
	}
}

// 目的: 既定の continuo が、`--id` を付けた continuo の置き場所へ入らないことを確かめる
// （設計 3-17b）。
//
// **`--id e2e` の worktree は `<workspace.root>/e2e/<host>/<owner>/<repo>/<スラグ>` にある。**
// 既定側の走査は `<workspace.root>` からちょうど4階層を返すので、目印が無ければ
// `<workspace.root>/e2e/<host>/<owner>/<repo>` が「身元ファイルの無いディレクトリ」として
// 拾われ、**`continuo abandon` が判断を保留したまま止まる。**
//
// 与える情報: `<root>/e2e/github.com/octocat/hello-world` の階層と、目印の有無。
// 成功条件: 目印が無ければ拾い、目印を置けば1件も拾わないこと。
func TestScanUnidentified_別のinstanceの置き場所へは入らない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	root := fx.Manager.ResolvedRoot()

	other := filepath.Join(root, "e2e")
	repoDir := filepath.Join(other, "github.com", "octocat", "hello-world")
	if err := os.MkdirAll(filepath.Join(repoDir, "continuo-octocat-hello-world-188"), 0o700); err != nil {
		t.Fatalf("別の instance の置き場所を作れません: %v", err)
	}

	// **目印を置く前は拾う。**ここが空だと、この検査は何も守っていない。
	before, err := fx.Manager.ScanUnidentified()
	if err != nil {
		t.Fatalf("ScanUnidentified に失敗した: %v", err)
	}
	if len(before) != 1 || before[0] != repoDir {
		t.Fatalf("目印が無いのに拾っていない（検査が空振りしている）: %v", before)
	}

	if err := workspace.WriteInstanceMarker(other, "e2e"); err != nil {
		t.Fatalf("目印を書けません: %v", err)
	}

	after, err := fx.Manager.ScanUnidentified()
	if err != nil {
		t.Fatalf("ScanUnidentified に失敗した: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("別の instance の置き場所を数えている: %v", after)
	}
}

// 目的: 目印の `id` が置き場所の名前と食い違うときは飛ばさないことを確かめる
// （設計 3-17b）。
//
// **worktree の中ではエージェントが `--permission-mode dontAsk` で動く。**
// 目印はそこから書けるので、**中身を見ずに飛ばすと、1バイト置くだけで
// `continuo abandon` の目から隠せる。**
//
// 与える情報: ディレクトリ名と食い違う `id` を書いた目印。
// 成功条件: いつもどおり数えること。
func TestScanUnidentified_名前の食い違う目印では飛ばさない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	root := fx.Manager.ResolvedRoot()

	other := filepath.Join(root, "e2e")
	repoDir := filepath.Join(other, "github.com", "octocat", "hello-world")
	if err := os.MkdirAll(repoDir, 0o700); err != nil {
		t.Fatalf("別の instance の置き場所を作れません: %v", err)
	}
	// **`e2e` ではなく `other` と名乗らせる。**
	if err := workspace.WriteInstanceMarker(other, "other"); err != nil {
		t.Fatalf("目印を書けません: %v", err)
	}

	found, err := fx.Manager.ScanUnidentified()
	if err != nil {
		t.Fatalf("ScanUnidentified に失敗した: %v", err)
	}
	if len(found) != 1 || found[0] != repoDir {
		t.Fatalf("名前が食い違う目印で飛ばしてしまった: %v", found)
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
