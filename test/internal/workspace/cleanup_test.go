package workspace_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/workspace"
)

// cleanupFixture は片付けの検査1件分の状態である。
type cleanupFixture struct {
	// managerFixture は Manager と周辺の値である。
	*managerFixture
	// Prepared は用意した worktree である。
	Prepared *workspace.PrepareResult
	// SettingsPath は身元ファイルに書いた、issue ごとの設定ファイルのパスである。
	SettingsPath string
}

// newCleanupFixture は worktree を1つ用意し、身元ファイルと設定ファイルを置く。
//
// テスト用herdr mock の worktree.remove には「実体を本当に消す」副作用を登録する
// （本物の herdr は worktree を消す。消さないと `git branch -D` が必ず失敗し、
// 片付けの段4 を検証できない）。
//
// t: 呼び出し元のテスト。
// mutate: 設定を書き換える関数（nil 可）。
// 戻り値: 片付けの検査に使う状態。
func newCleanupFixture(t *testing.T, mutate func(cfg *config.Config)) *cleanupFixture {
	t.Helper()
	return newCleanupFixtureWith(t, fixtureOptions{Mutate: mutate})
}

// newCleanupFixtureWith は newCleanupFixture と同じ用意を、fixtureOptions を丸ごと
// 指定して行う（settings の置き場所を空にする検査などで使う）。
//
// t: 呼び出し元のテスト。
// opts: Manager の組み立てに渡す入力。
// 戻り値: 片付けの検査に使う状態。
func newCleanupFixtureWith(t *testing.T, opts fixtureOptions) *cleanupFixture {
	t.Helper()

	fx := newFixture(t, opts)
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	fx.Herdr.SetOnRequest(herdr.MethodWorktreeRemove, func(_ map[string]any) {
		// 本物の herdr と同じく worktree の実体を消す。
		// **接続ごとの goroutine なので t.Fatalf は使わない。**
		_ = exec.Command("git", "-C", fx.Repo.Dir, "worktree", "remove", "--force", prepared.Path).Run()
	})

	// 設計 3-12 の置き場所（`<実行時ディレクトリ>/issues/<issue>/settings.json`）に合わせる。
	// **SettingsRoot が空の検査でも実ファイルは要る**ので、そのときは一時ディレクトリへ置く。
	settingsBase := fx.SettingsRoot
	if settingsBase == "" {
		settingsBase = filepath.Join(t.TempDir(), "issues")
	}
	settingsDir := filepath.Join(settingsBase, "octocat-hello-world-188")
	if err := os.MkdirAll(settingsDir, 0o700); err != nil {
		t.Fatalf("issue ごとの設定ファイルの置き場所を作れない: %v", err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("issue ごとの設定ファイルを書けない: %v", err)
	}

	identity := workspace.Identity{
		IssueURL:         sampleIssue(188).URL,
		IssueIdentifier:  sampleIssue(188).Identifier,
		ProjectItemID:    "PVTI_test",
		Branch:           prepared.Branch.String(),
		HerdrWorkspaceID: "w9",
		SettingsPath:     settingsPath,
		CreatedAt:        time.Now(),
	}
	if err := fx.Manager.WriteIdentity(context.Background(), prepared.Path, identity); err != nil {
		t.Fatalf("WriteIdentity に失敗した: %v", err)
	}

	return &cleanupFixture{managerFixture: fx, Prepared: prepared, SettingsPath: settingsPath}
}

// setIdentityBranch は身元ファイルの branch だけを別の名前へ書き換える。
//
// **worktree が現に checkout している branch と食い違う身元ファイルを作る。**
// 身元ファイルは worktree の中にあってエージェントが書き換えられるので、
// 片付けは「実在して現物と一致する branch」しか消さない。
//
// t: 呼び出し元のテスト。
// cf: 片付けの検査に使う状態。
// branch: 身元ファイルへ書く branch 名。
func setIdentityBranch(t *testing.T, cf *cleanupFixture, branch string) {
	t.Helper()
	identity, err := cf.Manager.ReadIdentity(cf.Prepared.Path)
	if err != nil {
		t.Fatalf("身元ファイルを読めない: %v", err)
	}
	identity.Branch = branch
	if err := cf.Manager.WriteIdentity(context.Background(), cf.Prepared.Path, *identity); err != nil {
		t.Fatalf("身元ファイルを書けない: %v", err)
	}
}

// cleanupRequest は用意した worktree に対する片付けの入力を作る。
//
// cf: 片付けの検査に使う状態。
// 戻り値: base に main を指定した CleanupRequest。
func cleanupRequest(cf *cleanupFixture) workspace.CleanupRequest {
	return workspace.CleanupRequest{
		WorktreePath: cf.Prepared.Path,
		Base:         normalize.SafeName("main"),
	}
}

// upstreamOf は worktree が checkout している branch の upstream の名前を返す。
//
// **`git rev-parse @{u}` は upstream が無いと非 0 で終わる**ので、runGit ではテストが落ちる。
// `for-each-ref` なら upstream が無くても終了コードは 0 で、空文字が返る。
//
// t: 呼び出し元のテスト。
// cf: 片付けの検査に使う状態。
// 戻り値: upstream の ref 名（無ければ空文字）。
func upstreamOf(t *testing.T, cf *cleanupFixture) string {
	t.Helper()
	return runGit(t, cf.Prepared.Path,
		"for-each-ref", "--format=%(upstream)", "refs/heads/"+cf.Prepared.Branch.String())
}

// 目的: `-u` の無い push で別の名前へ出した worktree を片付けられることを確認する
// （設計 3-9 の手順2b の段1。#144（worktree の branch は変えず push 先だけ分ける））。
//
// **`git push origin HEAD:<別名>` は upstream を張らない。**upstream だけを見ると
// この worktree は base との差分が残ったままなので永久に片付かない。
// **リモート追跡 ref は `-u` の有無にかかわらず更新される**ので、そちらで判定する。
//
// 与える情報: commit を1つ積み、`-u` を付けずに別の名前へ push した worktree。
// 成功条件: Removed が真になり、worktree の実体が消えること。
func TestCleanup_uの無いpushで別名へ出していても消す(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	if err := os.WriteFile(filepath.Join(cf.Prepared.Path, "成果.md"), []byte("できた\n"), 0o600); err != nil {
		t.Fatalf("成果のファイルを書けない: %v", err)
	}
	runGit(t, cf.Prepared.Path, "add", ".")
	runGit(t, cf.Prepared.Path, "commit", "--quiet", "-m", "成果")
	runGit(t, cf.Prepared.Path, "push", "--quiet", "origin", "HEAD:pr-2nd")

	// 前提の確認。`-u` を付けていないので upstream は張られていない。
	if upstream := upstreamOf(t, cf); upstream != "" {
		t.Fatalf("前提が崩れている: `-u` の無い push で upstream %q が張られている", upstream)
	}

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed || result.Deferred {
		t.Fatalf("HEAD が remote に載っているのに片付けていない: %+v", *result)
	}
	if _, statErr := os.Stat(cf.Prepared.Path); statErr == nil {
		t.Fatal("worktree の実体が消えていない")
	}
}

// 目的: upstream が1本目の push 先のままでも、2本目の push 先で片付けられることを確認する
// （設計 3-9 の手順2b。**段1 が段2 より前にある**ことの検査）。
//
// **段2 を先に見ると見送られる。**upstream は1本目の branch を指したままなので
// `@{u}..HEAD` は 1 件を返す。**段1 は 2本目の push 先を見つけるので、消してよいと答える。**
//
// 与える情報: 1本目を `-u` 付きで push したあと、commit を積んで2本目を `-u` 無しで
// 別の名前へ push した worktree。
// 成功条件: Removed が真になること。
func TestCleanup_upstreamが1本目のままでも2本目のpush先で消す(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	if err := os.WriteFile(filepath.Join(cf.Prepared.Path, "一本目.md"), []byte("1本目\n"), 0o600); err != nil {
		t.Fatalf("1本目のファイルを書けない: %v", err)
	}
	runGit(t, cf.Prepared.Path, "add", ".")
	runGit(t, cf.Prepared.Path, "commit", "--quiet", "-m", "1本目")
	runGit(t, cf.Prepared.Path, "push", "--quiet", "-u", "origin", "HEAD:"+cf.Prepared.Branch.String())
	runGit(t, cf.Prepared.Path, "branch", "--set-upstream-to=origin/"+cf.Prepared.Branch.String())

	if err := os.WriteFile(filepath.Join(cf.Prepared.Path, "二本目.md"), []byte("2本目\n"), 0o600); err != nil {
		t.Fatalf("2本目のファイルを書けない: %v", err)
	}
	runGit(t, cf.Prepared.Path, "add", ".")
	runGit(t, cf.Prepared.Path, "commit", "--quiet", "-m", "2本目")
	runGit(t, cf.Prepared.Path, "push", "--quiet", "origin", "HEAD:pr-2nd")

	// 前提の確認。upstream は1本目のままなので、段2 だけを見ると 1 件先にいる。
	ahead := runGit(t, cf.Prepared.Path, "rev-list", "--count", "@{u}..HEAD")
	if ahead != "1" {
		t.Fatalf("前提が崩れている: upstream より先にある commit の数が %q（1 を期待）", ahead)
	}

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed || result.Deferred {
		t.Fatalf("2本目の push 先に HEAD が載っているのに片付けていない: %+v", *result)
	}
}

// 目的: upstream が無くても base と差分が無ければ消してよいことを確認する
// （設計 3-9 の手順2b。その branch で何も変えていない）。
// 与える情報: 作ったまま何も触っていない worktree。
// 成功条件: Removed が真になること。
func TestCleanup_upstreamが無くbaseと差分が無ければ消す(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed || result.Deferred {
		t.Fatalf("差分が無いのに片付けていない: %+v", *result)
	}
	if _, statErr := os.Stat(cf.Prepared.Path); statErr == nil {
		t.Fatal("worktree の実体が消えていない")
	}
}

// 目的: workspace_hooks.before_remove が、消す前の worktree を cwd にして実行されることを
// 確認する（設計 3-9 の手順2d）。
// 与える情報: 実行時の作業ディレクトリをファイルに書き出す before_remove。
// 成功条件: 書き出されたパスが worktree のパスと一致し、worktree が片付けられていること。
func TestCleanup_before_removeを消す前のworktreeをcwdにして実行する(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "cwd.txt")
	command := "pwd > " + marker
	cf := newCleanupFixture(t, func(cfg *config.Config) {
		cfg.WorkspaceHooks.BeforeRemove = &command
	})

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("片付けられていない: %+v", *result)
	}

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("before_remove が実行されていない（%s を読めない）: %v", marker, err)
	}
	got := strings.TrimSpace(string(data))
	resolved, err := filepath.EvalSymlinks(cf.Prepared.Path)
	if err != nil {
		// 既に worktree は消えているので、解決できなければ元のパスで比べる。
		resolved = cf.Prepared.Path
	}
	if got != cf.Prepared.Path && got != resolved {
		t.Fatalf("before_remove の cwd が worktree でない: got %q, want %q", got, cf.Prepared.Path)
	}
}

// 目的: workspace_hooks.before_remove が失敗しても片付けを止めないことを確認する
// （設計 3-9 の手順2d。失敗しても記録して続ける）。
// 与える情報: 必ず失敗する before_remove。
// 成功条件: Cleanup がエラーを返さず、worktree が片付けられていること。
func TestCleanup_before_removeが失敗しても片付けを続ける(t *testing.T) {
	command := "exit 1"
	cf := newCleanupFixture(t, func(cfg *config.Config) {
		cfg.WorkspaceHooks.BeforeRemove = &command
	})

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("before_remove の失敗で Cleanup が止まった: %v", err)
	}
	if !result.Removed {
		t.Fatalf("before_remove の失敗で片付けが止まっている: %+v", *result)
	}
}

// tamperIdentity は worktree の身元ファイルを書き換える（エージェントが書き換えた状態を作る）。
//
// **身元ファイルは worktree の直下にあり、その worktree ではエージェントが
// `--permission-mode auto`（既定）で動く**（設計 3-16 の段9）ので、この状態は現実に起こりうる。
//
// t: 呼び出し元のテスト。
// cf: 片付けの検査に使う状態。
// mutate: 読み取った身元ファイルを書き換える関数。
func tamperIdentity(t *testing.T, cf *cleanupFixture, mutate func(identity *workspace.Identity)) {
	t.Helper()
	identity, err := cf.Manager.ReadIdentity(cf.Prepared.Path)
	if err != nil {
		t.Fatalf("身元ファイルを読めない: %v", err)
	}
	mutate(identity)
	if err := cf.Manager.WriteIdentity(context.Background(), cf.Prepared.Path, *identity); err != nil {
		t.Fatalf("身元ファイルを書けない: %v", err)
	}
}

// 目的: 身元ファイルの settings_path が置き場所の外側なら消さないことを確認する
// （設計 3-12。settings_path はエージェントが書き換えられる値である）。
// 与える情報: 置き場所の外側にあるファイルを指した settings_path。
// 成功条件: worktree は消えるが、そのファイルが残っていること。
func TestCleanup_置き場所の外側のsettings_pathは消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	outside := filepath.Join(t.TempDir(), "大事なもの.json")
	if err := os.WriteFile(outside, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("外側のファイルを書けない: %v", err)
	}
	tamperIdentity(t, cf, func(identity *workspace.Identity) { identity.SettingsPath = outside })

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("片付けてよい worktree なのに消していない: %+v", *result)
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Fatalf("置き場所の外側のファイルが消されている: %v", statErr)
	}
}

// 目的: settings_path が `..` で置き場所の外へ抜ける値でも消さないことを確認する
// （設計 3-12。filepath.Clean で畳んでから判定する）。
// 与える情報: `<置き場所>/../大事なもの.json` を指した settings_path。
// 成功条件: そのファイルが残っていること。
func TestCleanup_親をたどるsettings_pathは消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	outside := filepath.Join(filepath.Dir(cf.SettingsRoot), "大事なもの.json")
	if err := os.WriteFile(outside, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("外側のファイルを書けない: %v", err)
	}
	tamperIdentity(t, cf, func(identity *workspace.Identity) {
		identity.SettingsPath = filepath.Join(cf.SettingsRoot, "..", "大事なもの.json")
	})

	if _, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf)); err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Fatalf("`..` で外へ抜ける settings_path のファイルが消されている: %v", statErr)
	}
}

// 目的: 設定ファイルの置き場所を渡していないときは settings_path を消さないことを確認する
// （内側かどうかを確かめられないため）。
// 与える情報: SettingsRoot が空の Manager と、実在する settings_path。
// 成功条件: worktree は消えるが、そのファイルが残っていること。
func TestCleanup_置き場所を渡していなければsettings_pathを消さない(t *testing.T) {
	empty := ""
	cf := newCleanupFixtureWith(t, fixtureOptions{SettingsRoot: &empty})

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("片付けてよい worktree なのに消していない: %+v", *result)
	}
	if _, statErr := os.Stat(cf.SettingsPath); statErr != nil {
		t.Fatalf("置き場所が分からないのに設定ファイルを消している: %v", statErr)
	}
}

// tamperGitFile は worktree の `.git` を書き換え、別のリポジトリを指させる。
//
// **worktree の `.git` はディレクトリではなく `gitdir: …` と書かれただけの 0644 の
// ファイルである。**その worktree ではエージェントが `--permission-mode auto`（既定）で
// 動く（設計 3-16 の段9）ので、この書き換えは現実に起こりうる。
//
// t: 呼び出し元のテスト。
// worktreePath: 書き換える worktree のパス。
// victim: 代わりに指させるリポジトリ。
func tamperGitFile(t *testing.T, worktreePath string, victim *testRepo) {
	t.Helper()
	gitFile := filepath.Join(worktreePath, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+filepath.Join(victim.Dir, ".git")+"\n"), 0o644); err != nil {
		t.Fatalf("worktree の .git を書き換えられない: %v", err)
	}
}

// 目的: 身元ファイルの herdr_workspace_id が書き換えられていても、**別の run の
// worktree を消させられない**ことを確認する（設計 3-9 の段3。この値もエージェントが
// 書き換えられるので、消す宛先は herdr に現物を答えさせる）。
// 与える情報: herdr_workspace_id を別の workspace の ID に書き換えた身元ファイルと、
// 開いている worktree のパスに対して "w9" を答える herdr。
// 成功条件: worktree.remove に渡る workspace_id が、書き換えられた値ではなく
// herdr が答えた "w9" であること。
func TestCleanup_身元ファイルのherdr_workspace_idが書き換えられていても他のworkspaceを消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	tamperIdentity(t, cf, func(identity *workspace.Identity) {
		identity.HerdrWorkspaceID = "w-他の-run"
	})

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("片付けてよい worktree なのに消していない: %+v", *result)
	}

	var removeParams map[string]any
	for _, req := range cf.Herdr.Requests() {
		if req.Method == herdr.MethodWorktreeRemove {
			removeParams = req.Params
		}
	}
	if removeParams == nil {
		t.Fatalf("herdr へ worktree.remove を送っていない: %v", cf.Herdr.Methods())
	}
	if removeParams["workspace_id"] == "w-他の-run" {
		t.Fatalf("身元ファイルに書かれた workspace_id をそのまま消しに行っている（別の run の worktree を消せる）: %v",
			removeParams)
	}
	if removeParams["workspace_id"] != "w9" {
		t.Fatalf("herdr が答えた workspace_id を消していない: %v", removeParams)
	}
}

// 目的: 身元ファイルが info/exclude に登録されていなくても、片付けが成立することを
// 確認する（設計 3-9 の手順2。登録は利用者の `git status` を汚さないための親切であって、
// 片付けの正しさをその成否に依存させない）。
// 与える情報: 身元ファイルを置いたあとに info/exclude を消した worktree。
// 成功条件: `git status --porcelain` に身元ファイルが出る状態でも Removed が真になること。
func TestCleanup_身元ファイルが未追跡でも片付けを見送らない(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	excludePath := filepath.Join(cf.Repo.Dir, ".git", "info", "exclude")
	if err := os.Remove(excludePath); err != nil {
		t.Fatalf("info/exclude を消せない: %v", err)
	}
	// 一時ファイルの残骸も同じく数から外れること（強制終了で残りうる）。
	leftover := cf.Manager.IdentityPath(cf.Prepared.Path) + ".tmp1234567"
	if err := os.WriteFile(leftover, []byte("{}"), 0o600); err != nil {
		t.Fatalf("一時ファイルの残骸を置けない: %v", err)
	}
	status := runGit(t, cf.Prepared.Path, "status", "--porcelain")
	if !strings.Contains(status, ".continuo.json") {
		t.Fatalf("前提が崩れている（身元ファイルが未追跡として出ていない）: %q", status)
	}

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("continuo 自身が置いたファイルを「利用者の成果」と数えて見送っている: %+v", *result)
	}
}

// 目的: 呼び出し側が base を渡さなくても、身元ファイルに書かれた base で判定できることを
// 確認する（設計 3-9 の手順2b。再起動をまたぐと呼び出し側は base を持っていない）。
// 与える情報: base を "main" と書いた身元ファイルと、Base を空にした CleanupRequest。
// 成功条件: 「base が分からない」で見送らず、Removed が真になること。
func TestCleanup_baseは身元ファイルから補える(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	tamperIdentity(t, cf, func(identity *workspace.Identity) { identity.Base = "main" })

	result, err := cf.Manager.Cleanup(context.Background(), workspace.CleanupRequest{
		WorktreePath: cf.Prepared.Path,
	})
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("身元ファイルの base で判定できるのに見送っている: %+v", *result)
	}
}

// 目的: 片付けた worktree の after_run の印を落とすことを確認する
// （常駐プロセスなので、消した worktree の印を残すとプロセスの寿命のあいだ増え続ける）。
// 与える情報: after_run を1回実行したあとの片付け。
// 成功条件: 片付けのあとに RunAfterRunOnce を呼ぶと「実行した」が返ること
// （印が残っていれば偽が返る）。
func TestCleanup_片付けたworktreeのafter_runの印を落とす(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	ctx := context.Background()

	// workspace_hooks.after_run は未設定なので、印の付け外しだけが起こる。
	if ran, err := cf.Manager.RunAfterRunOnce(ctx, cf.Prepared.Path); err != nil || !ran {
		t.Fatalf("1回目の RunAfterRunOnce が実行されていない: ran=%v err=%v", ran, err)
	}
	if ran, err := cf.Manager.RunAfterRunOnce(ctx, cf.Prepared.Path); err != nil || ran {
		t.Fatalf("2回目が実行されている（印が付いていない）: ran=%v err=%v", ran, err)
	}

	result, err := cf.Manager.Cleanup(ctx, cleanupRequest(cf))
	if err != nil || !result.Removed {
		t.Fatalf("片付けに失敗した: %+v err=%v", result, err)
	}

	if ran, err := cf.Manager.RunAfterRunOnce(ctx, cf.Prepared.Path); err != nil || !ran {
		t.Fatalf("片付けたのに after_run の印が残っている: ran=%v err=%v", ran, err)
	}
}

// 目的: 封じ込め検査を通したパスだけで以後の処理を行うことを確認する
// （設計 3-20。検査したパスと操作したパスが違うと、検査の保証がそのまま切れる）。
// 与える情報: 置き場所へのシンボリックリンクを経由した worktree のパス。
// 成功条件: 片付けが成立し、worktree.remove まで届くこと。
func TestCleanup_シンボリックリンク越しのパスでも片付けられる(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	link := filepath.Join(t.TempDir(), "リンク")
	if err := os.Symlink(cf.Manager.ResolvedRoot(), link); err != nil {
		t.Fatalf("置き場所へのシンボリックリンクを作れない: %v", err)
	}
	rel, err := filepath.Rel(cf.Manager.ResolvedRoot(), cf.Prepared.Path)
	if err != nil {
		t.Fatalf("置き場所からの相対パスを作れない: %v", err)
	}

	result, err := cf.Manager.Cleanup(context.Background(), workspace.CleanupRequest{
		WorktreePath: filepath.Join(link, rel),
		Base:         normalize.SafeName("main"),
	})
	if err != nil {
		t.Fatalf("シンボリックリンク越しの Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("シンボリックリンク越しだと片付けられていない: %+v", *result)
	}
}

// 目的: `git status --porcelain` の読み取りが上限で打ち切られたとき、
// **打ち切ったことを Inspect が持ち帰る**ことを確認する（設計 3-9 の手順2）。
// **打ち切りを落とすと、数千ファイルを失う worktree が「200 ファイル」に見える。**
// 見せた数より多く失うのが、いちばん困る誤りである。
// 与える情報: 8KB の上限を超えるだけの未追跡のファイルを置いた worktree。
// 成功条件: DirtyFilesTruncated が真、DirtyFiles が1以上、HasLoss が真であること。
func TestInspect_数え切れないほど変更があれば打ち切ったと分かる(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	// 1件あたり40バイトほどの行になるので、400件で8KBの上限を必ず超える。
	for i := 0; i < 400; i++ {
		name := fmt.Sprintf("未追跡のファイル-%04d.md", i)
		if err := os.WriteFile(filepath.Join(cf.Prepared.Path, name), []byte("途中\n"), 0o600); err != nil {
			t.Fatalf("未追跡のファイルを書けない: %v", err)
		}
	}

	leftover, err := cf.Manager.Inspect(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Inspect に失敗した: %v", err)
	}
	if !leftover.DirtyFilesTruncated {
		t.Fatalf("上限を超えているのに打ち切りが伝わっていない（数えた件数: %d）", leftover.DirtyFiles)
	}
	if leftover.DirtyFiles < 1 {
		t.Fatalf("打ち切った出力から1件も数えていない: %d", leftover.DirtyFiles)
	}
	if !leftover.HasLoss() {
		t.Fatal("未コミットの変更があるのに失うものが無いと言っている")
	}
}

// 目的: 変更が上限に収まる件数なら、打ち切りが偽のまま実数が入ることを確認する
// （設計 3-9 の手順2）。
// **常に「以上」と出しては、何ファイル失うのかが分からない。**
// 与える情報: 未追跡のファイルを3件だけ置いた worktree。
// 成功条件: DirtyFilesTruncated が偽、DirtyFiles が 3 であること。
func TestInspect_収まる件数なら実数を数える(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("作りかけ-%d.md", i)
		if err := os.WriteFile(filepath.Join(cf.Prepared.Path, name), []byte("途中\n"), 0o600); err != nil {
			t.Fatalf("未追跡のファイルを書けない: %v", err)
		}
	}

	leftover, err := cf.Manager.Inspect(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Inspect に失敗した: %v", err)
	}
	if leftover.DirtyFilesTruncated {
		t.Fatal("上限に収まっているのに打ち切ったことになっている")
	}
	if leftover.DirtyFiles != 3 {
		t.Fatalf("コミットしていない変更の件数が 3 ではなく %d だった", leftover.DirtyFiles)
	}
}

// 目的: **未追跡のディレクトリの中身を1件に畳まずに数える**ことを確認する
// （設計 3-9 の手順2）。
// **`git status --porcelain` の既定（-unormal）は、未追跡のディレクトリを
// `?? <ディレクトリ>/` の1行にまとめる**（実測: 2026-08-25、git 2.50.1）。
// その行数をそのまま件数として見せると、**数千ファイルを失う worktree が
// 「1 ファイル」に見える。**人間はその数を見て `--force` を付けるかどうかを決めるので、
// **見せた数より多く失う**という、いちばん困る誤りになる。
// 与える情報: `生成物/深い/場所/` の下に5ファイルを置いた worktree
// （worktree の直下にはファイルを1つも置かない）。
// 成功条件: DirtyFiles が 5、HasLoss が真であること。
func TestInspect_未追跡ディレクトリの中身を1件に畳まない(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	dir := filepath.Join(cf.Prepared.Path, "生成物", "深い", "場所")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("未追跡のディレクトリを作れない: %v", err)
	}
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("成果-%d.md", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("途中\n"), 0o600); err != nil {
			t.Fatalf("未追跡のファイルを書けない: %v", err)
		}
	}

	leftover, err := cf.Manager.Inspect(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Inspect に失敗した: %v", err)
	}
	if leftover.DirtyFiles != 5 {
		t.Fatalf("未追跡のディレクトリの中身を数え落としている: got %d, want 5（失う量を実際より少なく見せている）",
			leftover.DirtyFiles)
	}
	if !leftover.HasLoss() {
		t.Fatal("未コミットの変更があるのに失うものが無いと言っている")
	}
}
