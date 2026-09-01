// {"RUCM-CFG-SHA256": "347ee23a1a99fc2a0637b259c00510bdd8f48cdb7f340d653599af6bf1894721", "SOURCE": "docs/spec/usecases/particular_case/worktree と branch を片付ける.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**「worktree と branch を片付ける」の
// 7本のパスに、それぞれ対応するテストがある。
package workspace_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
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

// {"RUCM-PATH": "P028"}
//
// 目的: 未コミットの変更（未追跡のファイル）が残っていれば worktree を消さないことを確認する
// （設計 3-9 の手順2。エージェントが作った成果物が消えるのを防ぐ）。
// 与える情報: worktree の中に置いた、commit も add もしていないファイル。
// 成功条件: Deferred が真、Removed が偽、worktree が残り、herdr に worktree.remove を
// 送っていないこと。ShouldComment が真であること（1回目の見送り）。
func TestCleanup_未コミットの変更があれば消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	if err := os.WriteFile(filepath.Join(cf.Prepared.Path, "作りかけ.md"), []byte("途中\n"), 0o600); err != nil {
		t.Fatalf("未追跡のファイルを書けない: %v", err)
	}

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if result.Removed || !result.Deferred {
		t.Fatalf("未コミットの変更があるのに片付けを見送っていない: %+v", *result)
	}
	if !result.ShouldComment {
		t.Fatal("1回目の見送りなのに ShouldComment が偽になっている")
	}
	if len(result.Reasons) == 0 {
		t.Fatal("見送った理由が返っていない")
	}
	if _, statErr := os.Stat(cf.Prepared.Path); statErr != nil {
		t.Fatalf("見送ったのに worktree が消えている: %v", statErr)
	}
	if slices.Contains(cf.Herdr.Methods(), herdr.MethodWorktreeRemove) {
		t.Fatalf("見送ったのに herdr へ worktree.remove を送っている: %v", cf.Herdr.Methods())
	}
}

// 目的: 消さなかった worktree について、issue へのコメントが1回だけになることを確認する
// （設計 3-9 の手順2c。「1回だけ」の記録は身元ファイルに持つ）。
// 与える情報: 1回目の見送りのあとに MarkCleanupDeferred を呼んでから、もう一度片付けを試みる。
// 成功条件: 2回目の ShouldComment が偽になること。
func TestCleanup_見送りのコメントは1回だけになる(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	if err := os.WriteFile(filepath.Join(cf.Prepared.Path, "作りかけ.md"), []byte("途中\n"), 0o600); err != nil {
		t.Fatalf("未追跡のファイルを書けない: %v", err)
	}

	first, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("1回目の Cleanup に失敗した: %v", err)
	}
	if !first.ShouldComment {
		t.Fatal("1回目の ShouldComment が偽になっている")
	}
	// orchestrator が issue へのコメントに成功したあとに呼ぶ経路。
	if err := cf.Manager.MarkCleanupDeferred(cf.Prepared.Path, time.Now()); err != nil {
		t.Fatalf("MarkCleanupDeferred に失敗した: %v", err)
	}

	second, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("2回目の Cleanup に失敗した: %v", err)
	}
	if !second.Deferred {
		t.Fatal("2回目も見送るはずなのに見送っていない")
	}
	if second.ShouldComment {
		t.Fatal("2回目なのに ShouldComment が真になっている（コメントが積み上がる）")
	}
}

// {"RUCM-PATH": "P021"}
//
// 目的: upstream があり push 済みなら片付け、そのとき worktree.remove に渡すのが
// **身元ファイルの herdr workspace の ID** であることを確認する（設計 3-9 の手順2b・3）。
// 与える情報: worktree の中で commit して push し、upstream を持たせた状態。
// 成功条件: Removed が真、worktree.remove の params が workspace_id（path でも branch でもない）、
// **worktree.remove のあとに workspace.close を呼んでいない**こと、branch が消えていること、
// issue ごとの設定ファイルが消えていること。
func TestCleanup_push済みなら消してbranchと設定ファイルも消す(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	if err := os.WriteFile(filepath.Join(cf.Prepared.Path, "成果.md"), []byte("できた\n"), 0o600); err != nil {
		t.Fatalf("成果のファイルを書けない: %v", err)
	}
	runGit(t, cf.Prepared.Path, "add", ".")
	runGit(t, cf.Prepared.Path, "commit", "--quiet", "-m", "成果")
	runGit(t, cf.Prepared.Path, "push", "--quiet", "-u", "origin", "HEAD:"+cf.Prepared.Branch.String())
	runGit(t, cf.Prepared.Path, "branch", "--set-upstream-to=origin/"+cf.Prepared.Branch.String())

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed || result.Deferred {
		t.Fatalf("push 済みなのに片付けていない: %+v", *result)
	}

	var removeParams map[string]any
	for _, req := range cf.Herdr.Requests() {
		if req.Method == herdr.MethodWorktreeRemove {
			removeParams = req.Params
		}
		if req.Method == "workspace.close" {
			t.Fatal("worktree.remove のあとに workspace.close を呼んでいる（workspace ごと閉じてしまう）")
		}
	}
	if removeParams == nil {
		t.Fatalf("herdr へ worktree.remove を送っていない: %v", cf.Herdr.Methods())
	}
	if removeParams["workspace_id"] != "w9" {
		t.Fatalf("worktree.remove に身元ファイルの workspace_id を渡していない: %v", removeParams)
	}
	for _, key := range []string{"path", "branch"} {
		if _, ok := removeParams[key]; ok {
			t.Fatalf("worktree.remove に %q を渡している（引数は workspace_id である）: %v", key, removeParams)
		}
	}

	if branches := runGit(t, cf.Repo.Dir, "branch", "--list", cf.Prepared.Branch.String()); strings.TrimSpace(branches) != "" {
		t.Fatalf("branch が消えていない（worktree.remove は branch を消さない）: %q", branches)
	}
	if _, statErr := os.Stat(cf.SettingsPath); statErr == nil {
		t.Fatal("issue ごとの設定ファイルが消えていない")
	}
}

// {"RUCM-PATH": "P020"}
//
// 目的: `cleanup.delete_branch` が偽なら、worktree を消しても branch は残し、
// **残ったものとして画面へ出す**ことを確認する（設計 3-9 の段4）。
// **「worktree を消した」と「branch も消えた」を同じ意味に読ませない。**
// 残っているのに消えたことにされると、残骸を探す人はログを疑うところから始める。
// 与える情報: `cleanup.delete_branch` を偽にした設定と、push 済みの worktree。
// 成功条件: Removed が真、BranchDeleted が偽、branch が clone に残っている、
// 残ったものに「設定で消さない」ことが積まれていること。
func TestCleanup_deleteBranchが偽ならbranchを残して残ったものに積む(t *testing.T) {
	cf := newCleanupFixture(t, func(cfg *config.Config) { cfg.Cleanup.DeleteBranch = false })

	if err := os.WriteFile(filepath.Join(cf.Prepared.Path, "成果.md"), []byte("できた\n"), 0o600); err != nil {
		t.Fatalf("成果のファイルを書けない: %v", err)
	}
	runGit(t, cf.Prepared.Path, "add", ".")
	runGit(t, cf.Prepared.Path, "commit", "--quiet", "-m", "成果")
	runGit(t, cf.Prepared.Path, "push", "--quiet", "-u", "origin", "HEAD:"+cf.Prepared.Branch.String())
	runGit(t, cf.Prepared.Path, "branch", "--set-upstream-to=origin/"+cf.Prepared.Branch.String())

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("worktree を片付けていない: %+v", *result)
	}
	if result.BranchDeleted {
		t.Fatal("cleanup.delete_branch が false なのに branch を消したと答えている")
	}
	if branches := runGit(t, cf.Repo.Dir, "branch", "--list", cf.Prepared.Branch.String()); strings.TrimSpace(branches) == "" {
		t.Fatalf("cleanup.delete_branch が false なのに branch %s を消している", cf.Prepared.Branch.String())
	}
	want := i18n.T(i18n.KeyWorkspaceLeftoverBranchDisabled, cf.Prepared.Branch.String())
	if !slices.Contains(result.Leftovers, want) {
		t.Fatalf("残ったものに %q が積まれていない: %v", want, result.Leftovers)
	}
}

// {"RUCM-PATH": "P027"}
//
// 目的: upstream があり push されていない commit が残っていれば消さないことを確認する
// （設計 3-9 の手順2b の upstream がある側）。
// 与える情報: push したあとにもう1つ commit を積んだ worktree。
// 成功条件: Deferred が真になり、理由に push されていないことが書かれていること。
func TestCleanup_push済みでないcommitが残っていれば消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	if err := os.WriteFile(filepath.Join(cf.Prepared.Path, "成果.md"), []byte("できた\n"), 0o600); err != nil {
		t.Fatalf("成果のファイルを書けない: %v", err)
	}
	runGit(t, cf.Prepared.Path, "add", ".")
	runGit(t, cf.Prepared.Path, "commit", "--quiet", "-m", "成果")
	runGit(t, cf.Prepared.Path, "push", "--quiet", "-u", "origin", "HEAD:"+cf.Prepared.Branch.String())
	runGit(t, cf.Prepared.Path, "branch", "--set-upstream-to=origin/"+cf.Prepared.Branch.String())

	if err := os.WriteFile(filepath.Join(cf.Prepared.Path, "続き.md"), []byte("まだ push していない\n"), 0o600); err != nil {
		t.Fatalf("続きのファイルを書けない: %v", err)
	}
	runGit(t, cf.Prepared.Path, "add", ".")
	runGit(t, cf.Prepared.Path, "commit", "--quiet", "-m", "続き")

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if result.Removed || !result.Deferred {
		t.Fatalf("未 push の commit があるのに片付けている: %+v", *result)
	}
	if !strings.Contains(strings.Join(result.Reasons, " / "), "push") {
		t.Fatalf("理由に push のことが書かれていない: %v", result.Reasons)
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

// 目的: upstream が無いときに base からの差分で判定することを確認する
// （設計 3-9 の手順2b の upstream が無い側。**commit の有無では判定しない**）。
// 与える情報: 一度も push していない worktree に積んだ commit（作業ツリーは clean）。
// 成功条件: Deferred が真になること（commit があっても upstream が無いので失うものがある）。
func TestCleanup_upstreamが無くbaseと差分があれば消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	if err := os.WriteFile(filepath.Join(cf.Prepared.Path, "成果.md"), []byte("できた\n"), 0o600); err != nil {
		t.Fatalf("成果のファイルを書けない: %v", err)
	}
	runGit(t, cf.Prepared.Path, "add", ".")
	runGit(t, cf.Prepared.Path, "commit", "--quiet", "-m", "成果")

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if result.Removed || !result.Deferred {
		t.Fatalf("upstream が無く base と差分があるのに片付けている: %+v", *result)
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

// {"RUCM-PATH": "P027"}
//
// 目的: HEAD が remote に載っておらず、upstream も base も無いときは、判定できないので
// 消さないことを確認する（設計 3-9 の手順2b の段4。base を推測して消すと成果を失う）。
//
// **commit を1つ積んでから呼ぶ。**積まないと HEAD が `refs/remotes/origin/main` に載ったままで、
// 段1 が真になって消してよいと判定される（それは正しい。失うものが無い）。
// 段4 を通すには、段1 を偽にしておく必要がある。
//
// 与える情報: 一度も push していない commit を積み、Base を空にした CleanupRequest。
// 成功条件: Deferred が真になり、worktree が残ること。
func TestCleanup_baseが分からなければ消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	if err := os.WriteFile(filepath.Join(cf.Prepared.Path, "成果.md"), []byte("できた\n"), 0o600); err != nil {
		t.Fatalf("成果のファイルを書けない: %v", err)
	}
	runGit(t, cf.Prepared.Path, "add", ".")
	runGit(t, cf.Prepared.Path, "commit", "--quiet", "-m", "成果")

	result, err := cf.Manager.Cleanup(context.Background(), workspace.CleanupRequest{
		WorktreePath: cf.Prepared.Path,
	})
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if result.Removed || !result.Deferred {
		t.Fatalf("base が分からないのに片付けている: %+v", *result)
	}
	if _, statErr := os.Stat(cf.Prepared.Path); statErr != nil {
		t.Fatalf("見送ったのに worktree が消えている: %v", statErr)
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

// {"RUCM-PATH": "P025"}
//
// 目的: worktree の実体を消し切れなかったら、branch も設定ファイルも消さずに止まることを
// 確認する（RUCM のステップ12。実体が残ったまま先へ進むと、中身のある worktree だけが
// 取り残される）。
// 与える情報: 書き込みを落とした worktree のディレクトリ（`os.RemoveAll` が必ず失敗する）。
// 成功条件: Cleanup がエラーを返し、worktree が残り、branch と issue ごとの設定ファイルが
// どちらも残っていること。
func TestCleanup_worktreeを消し切れなければbranchも設定ファイルも消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	// **中身ではなく worktree 自身の書き込みを落とす。**親を落とすと中身だけが先に消え、
	// 「実体が残っている」状態を作れない。
	if err := os.Chmod(cf.Prepared.Path, 0o500); err != nil {
		t.Fatalf("worktree の permission を落とせない: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(cf.Prepared.Path, 0o700) })

	if _, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf)); err == nil {
		t.Fatal("worktree を消し切れていないのにエラーにならなかった")
	}
	if _, statErr := os.Stat(cf.Prepared.Path); statErr != nil {
		t.Fatalf("消し切れなかったはずの worktree が消えている: %v", statErr)
	}
	branches := runGit(t, cf.Repo.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if !strings.Contains(branches, cf.Prepared.Branch.String()) {
		t.Fatalf("worktree を消せていないのに branch を消している: %s", branches)
	}
	if _, statErr := os.Stat(cf.SettingsPath); statErr != nil {
		t.Fatalf("worktree を消せていないのに設定ファイルを消している: %v", statErr)
	}
}

// {"RUCM-PATH": "P029"}
//
// 目的: 消す直前の封じ込め検査に落ちたら、何も消さずに失敗することを確認する
// （設計 3-20。「消す直前」がいちばん危ない検査点である）。
// 与える情報: 置き場所の外側にある worktree のパス。
// 成功条件: Cleanup がエラーを返し、herdr へ何も送らず、その worktree が残っていること。
func TestCleanup_置き場所の外側は消さずに失敗する(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	outside := filepath.Join(t.TempDir(), "外側の作業場")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatalf("外側のディレクトリを作れない: %v", err)
	}

	_, err := cf.Manager.Cleanup(context.Background(), workspace.CleanupRequest{
		WorktreePath: outside,
		Base:         normalize.SafeName("main"),
	})
	if err == nil {
		t.Fatal("置き場所の外側なのにエラーにならなかった")
	}
	if slices.Contains(cf.Herdr.Methods(), herdr.MethodWorktreeRemove) {
		t.Fatalf("外側なのに herdr へ worktree.remove を送っている: %v", cf.Herdr.Methods())
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Fatalf("外側のディレクトリが消されている: %v", statErr)
	}
}

// {"RUCM-PATH": "P032"}
//
// 目的: cleanup.enabled が偽なら何も消さず、かつ「見送った」と分かる戻り値になることを
// 確認する（設計 3-9 の手順5。デバッグ時に中身を見たい場合がある）。
// 与える情報: cleanup.enabled を偽にした設定。
// 成功条件: Removed が偽・Deferred が真・理由が入り・ShouldComment が偽で、
// worktree が残り、herdr へ何も送っていないこと
// （理由だけが入って Deferred が偽だと、呼び出し側が「消した」「見送った」「無効」を
// 区別できない）。
func TestCleanup_無効なら何もしない(t *testing.T) {
	cf := newCleanupFixture(t, func(cfg *config.Config) { cfg.Cleanup.Enabled = false })

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if result.Removed {
		t.Fatalf("片付けが無効なのに消している: %+v", *result)
	}
	if !result.Deferred {
		t.Fatalf("片付けが無効なのに見送りとして返っていない: %+v", *result)
	}
	if len(result.Reasons) == 0 {
		t.Fatalf("見送った理由が入っていない: %+v", *result)
	}
	if result.ShouldComment {
		t.Fatalf("設定で無効にしただけなのに issue へコメントしようとしている: %+v", *result)
	}
	if _, statErr := os.Stat(cf.Prepared.Path); statErr != nil {
		t.Fatalf("worktree が消えている: %v", statErr)
	}
	if slices.Contains(cf.Herdr.Methods(), herdr.MethodWorktreeRemove) {
		t.Fatalf("片付けが無効なのに herdr へ worktree.remove を送っている: %v", cf.Herdr.Methods())
	}
}

// {"RUCM-PATH": "P030"}
//
// 目的: 片付けを始める判定が cleanup.on_states に入った時点であり、
// active でなくなった時点ではないことを確認する（設計 3-9 の手順1）。
// 与える情報: on_states が Done だけの設定と、Done / done / In Review / Blocked の各 Status。
// 成功条件: Done は大文字小文字を無視して真になり、In Review と Blocked は偽になること
// （そこで消すと、人間が回答して Ready へ戻したときに作業成果が失われる）。
func TestShouldCleanup_on_statesに入った時点で片付ける(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Cleanup.OnStates = []string{"Done"} },
	})

	for _, state := range []string{"Done", "done", " Done "} {
		if !fx.Manager.ShouldCleanup(state) {
			t.Fatalf("%q が片付けの対象と判定されない", state)
		}
	}
	for _, state := range []string{"In Review", "Blocked", "In Progress", "Ready", ""} {
		if fx.Manager.ShouldCleanup(state) {
			t.Fatalf("%q が片付けの対象と判定された（成果が失われる）", state)
		}
	}
}

// tamperIdentity は worktree の身元ファイルを書き換える（エージェントが書き換えた状態を作る）。
//
// **身元ファイルは worktree の直下にあり、その worktree ではエージェントが
// `--permission-mode dontAsk` で動く**（設計 3-16 の段9）ので、この状態は現実に起こりうる。
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

// 目的: 身元ファイルの branch が書き換えられていても、利用者の別の branch を消さないことを
// 確認する（設計 3-9 の段4。身元ファイルはエージェントが書き換えられる場所にある）。
// 与える情報: branch を "main" に書き換えた身元ファイルと、片付けてよい worktree。
// 成功条件: worktree は消えるが、main が残っていること。
func TestCleanup_身元ファイルのbranchが書き換えられていても他のbranchを消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	tamperIdentity(t, cf, func(identity *workspace.Identity) { identity.Branch = "main" })

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("片付けてよい worktree なのに消していない: %+v", *result)
	}
	if branches := runGit(t, cf.Repo.Dir, "branch", "--list", "main"); strings.TrimSpace(branches) == "" {
		t.Fatal("利用者の main が消されている（身元ファイルの branch をそのまま git branch -D へ渡している）")
	}
}

// 目的: 身元ファイルの branch が worktree の現物と食い違うときは消さないことを確認する
// （設計 3-9 の段4。判定の根拠を git に置く）。
// 与える情報: 接頭辞は正しいが worktree がチェックアウトしていない branch 名。
// 成功条件: その branch が残っていること。
func TestCleanup_worktreeの現物と一致しないbranchは消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	runGit(t, cf.Repo.Dir, "branch", "continuo/octocat/hello-world/999")
	tamperIdentity(t, cf, func(identity *workspace.Identity) {
		identity.Branch = "continuo/octocat/hello-world/999"
	})

	if _, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf)); err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	branches := runGit(t, cf.Repo.Dir, "branch", "--list", "continuo/octocat/hello-world/999")
	if strings.TrimSpace(branches) == "" {
		t.Fatal("worktree がチェックアウトしていない branch を消している")
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
// ファイルである。**その worktree ではエージェントが `--permission-mode dontAsk` で
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

// {"RUCM-PATH": "P026"}
//
// 目的: herdr が別のパスを開いている workspace を答えたら、何も消さないことを確認する
// （設計 3-9 の段3。検算の答えが食い違ったら止まる。RUCM のステップ9 で消す宛先を
// 確定できないときの経路であり、before_remove も実行しない）。
// 与える情報: 常に別のパスを worktree として答えるテスト用herdr mock。
// 成功条件: Cleanup がエラーになり、worktree.remove を1度も送らず、worktree が残ること。
func TestCleanup_herdrが別のパスを答えたら何も消さない(t *testing.T) {
	other := filepath.Join(t.TempDir(), "別の-worktree")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatalf("別のパスを作れない: %v", err)
	}
	fake := newFakeHerdr(t, map[string]any{
		herdr.MethodWorktreeOpen:    worktreeOpenResult("w9", "w9:p1"),
		herdr.MethodWorktreeRemove:  worktreeRemoveResult("w9", ""),
		herdr.MethodWorkspaceRename: workspaceRenameResult("w9"),
	})
	cf := newCleanupFixtureWith(t, fixtureOptions{Herdr: fake})

	// **Prepare を通してから差し替える。**Prepare 自身も「herdr が別の場所を開いたら
	// 止める」検査を持つので（設計 6-2）、最初から別のパスを返すと Prepare で落ちて
	// Cleanup の検算に辿り着けない。
	open := worktreeOpenResult("w9", "w9:p1")
	open["worktree"] = map[string]any{"path": other}
	fake.SetResult(herdr.MethodWorktreeOpen, open)

	_, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err == nil {
		t.Fatal("herdr が別のパスを答えたのにエラーにならなかった")
	}
	if slices.Contains(cf.Herdr.Methods(), herdr.MethodWorktreeRemove) {
		t.Fatalf("検算に落ちたのに worktree.remove を送っている: %v", cf.Herdr.Methods())
	}
	if _, statErr := os.Stat(cf.Prepared.Path); statErr != nil {
		t.Fatalf("検算に落ちたのに worktree が消えている: %v", statErr)
	}
}

// 目的: worktree の `.git` が別のリポジトリを指すよう書き換えられていたら、
// **そのリポジトリに破壊的な git コマンドを撃たない**ことを確認する
// （設計 3-9 の段4。`git branch -D` の宛先を git の答えだけで決めない）。
// 与える情報: `.git` を別のリポジトリへ向けた worktree と、その別のリポジトリにある branch。
// 成功条件: Cleanup がエラーになり、別のリポジトリの branch が残っていること。
func TestCleanup_worktreeのgitが書き換えられていたら別のリポジトリに触らない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	victim := newTestRepo(t)
	runGit(t, victim.Dir, "branch", "continuo/victim-branch")

	tamperGitFile(t, cf.Prepared.Path, victim)

	if _, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf)); err == nil {
		t.Fatal(".git が書き換えられているのにエラーにならなかった")
	}
	branches := runGit(t, victim.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if !strings.Contains(branches, "continuo/victim-branch") {
		t.Fatalf("無関係のリポジトリの branch を消した: %s", branches)
	}
	if slices.Contains(cf.Herdr.Methods(), herdr.MethodWorktreeRemove) {
		t.Fatalf("検算に落ちたのに worktree.remove を送っている: %v", cf.Herdr.Methods())
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

// {"RUCM-PATH": "P019"}
//
// 目的: 身元ファイルに書かれた branch が**リポジトリに実在しない**とき、
// 残ったものとして数えないことを確認する（issue #27）。
// **着手が `git worktree add` で失敗し続けると、ディレクトリだけが残って
// branch は1度も作られない。**そこで「消せませんでした」と積むと、
// **利用者は存在しないものを探して消しに行く。**
// 与える情報: 失うものが無い worktree と、リポジトリに1度も作られていない branch 名を
// 書いた身元ファイル。
// 成功条件: Removed が真、BranchAbsent が真、BranchDeleted が偽、
// **Leftovers が空**であること。
func TestCleanup_実在しないbranchを残ったものとして数えない(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	// **接頭辞は continuo のままにする。**接頭辞で弾かれたのではなく、
	// 「リポジトリに実在しない」経路を通すためである。
	missing := cf.Prepared.Branch.String() + "-missing"
	setIdentityBranch(t, cf, missing)

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("失うものが無いのに片付けていない: %+v", *result)
	}
	if !result.BranchAbsent {
		t.Fatalf("実在しない branch なのに BranchAbsent が偽になっている: %+v", *result)
	}
	if result.BranchDeleted {
		t.Fatalf("消していない branch を「消した」と返している: %+v", *result)
	}
	if len(result.Leftovers) != 0 {
		t.Fatalf("実在しない branch を残ったものとして数えている: %v", result.Leftovers)
	}
}

// {"RUCM-PATH": "P019"}
//
// 目的: branch が実在しないなら、**`cleanup.delete_branch` が偽でも**残ったものに数えない
// ことを確認する（RUCM の基本フローで、実在の判定が設定の判定より先にある理由である）。
// **元から無いものを「設定で消さないので残しました」と言う理由は無い。**
// そう言われた利用者は、存在しない branch を探して消しに行く。
// 与える情報: `cleanup.delete_branch` を偽にした設定と、リポジトリに1度も作られていない
// branch 名を書いた身元ファイル。
// 成功条件: BranchAbsent が真で、Leftovers が空であること。
func TestCleanup_実在しないbranchはdeleteBranchが偽でも残ったものに数えない(t *testing.T) {
	cf := newCleanupFixture(t, func(cfg *config.Config) { cfg.Cleanup.DeleteBranch = false })
	missing := cf.Prepared.Branch.String() + "-missing"
	setIdentityBranch(t, cf, missing)

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.BranchAbsent {
		t.Fatalf("実在しない branch なのに BranchAbsent が偽になっている: %+v", *result)
	}
	if len(result.Leftovers) != 0 {
		t.Fatalf("元から無い branch を残ったものとして数えている: %v", result.Leftovers)
	}
}

// {"RUCM-PATH": "P021"}
//
// 目的: branch が**実在して**現物と食い違うときは、いままでどおり残ったものとして
// 理由を返すことを確認する（設計 3-9 の段4。issue #27 で消さなくなったのは
// 「実在しない」場合だけである）。
// 与える情報: 失うものが無い worktree と、リポジトリに実在するが worktree が
// チェックアウトしていない branch 名を書いた身元ファイル。
// 成功条件: Removed が真、BranchAbsent が偽、BranchDeleted が偽、
// Leftovers にその branch 名と理由が入っていること、その branch が残っていること。
func TestCleanup_実在するbranchを消せなければ理由を返す(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	stale := cf.Prepared.Branch.String() + "-old"
	runGit(t, cf.Repo.Dir, "branch", stale, "main")
	setIdentityBranch(t, cf, stale)

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("失うものが無いのに片付けていない: %+v", *result)
	}
	if result.BranchAbsent {
		t.Fatalf("実在する branch なのに BranchAbsent が真になっている: %+v", *result)
	}
	if result.BranchDeleted {
		t.Fatalf("現物と食い違う branch を消している: %+v", *result)
	}
	found := false
	for _, left := range result.Leftovers {
		if strings.Contains(left, stale) {
			found = true
		}
	}
	if !found {
		t.Fatalf("消せなかった branch %s が残ったものとして返っていない: %v", stale, result.Leftovers)
	}
	if strings.TrimSpace(runGit(t, cf.Repo.Dir, "branch", "--list", stale)) == "" {
		t.Fatalf("現物と食い違う branch %s を消している", stale)
	}
}

// 目的: git が1つも答えられないまま片付けを見送ったとき、**次に何をすべきか**を
// 呼び出し側へ渡すことを確認する（設計 3-49）。
//
// **理由だけを出しても、読んだ人間は次に何をすればよいか分からない。**
// その worktree は壊れており、continuo は二度と自分では片付けられないので、
// 巡回のたびに同じ理由が出続ける。**人間が手で始末する道筋をその場に置く。**
//
// 与える情報: `.git` を読めない文字列で潰した worktree。
//
// 成功条件: 見送りになり、**worktree が消えず**、
// 「中を調べる」「控える」「消す」の3行が返ること。
func TestCleanup_gitが答えられないときは次にすべきことを添える(t *testing.T) {
	cf := newCleanupFixture(t, nil)

	// worktree の `.git` は `gitdir: …` と書かれただけのファイルである（issue #23）。
	// 潰すと `git -C <worktree> …` が1つも通らない。
	if err := os.WriteFile(
		filepath.Join(cf.Prepared.Path, ".git"), []byte("こわれている\n"), 0o644); err != nil {
		t.Fatalf("worktree の .git を潰せない: %v", err)
	}

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("片付けがエラーになった（見送りで返すこと）: %v", err)
	}
	if !result.Deferred || result.Removed {
		t.Fatalf("git が答えられないのに片付けてしまった: %+v", result)
	}
	if len(result.NextSteps) != 3 {
		t.Fatalf("次にすべきことが3行で入っていない: %+v", result.NextSteps)
	}
	if !strings.Contains(result.NextSteps[0], cf.Prepared.Path) {
		t.Errorf("1行目に調べる相手が入っていない: %q", result.NextSteps[0])
	}
	if !strings.Contains(result.NextSteps[1], "cp -a") {
		t.Errorf("2行目に控え方が入っていない: %q", result.NextSteps[1])
	}
	if !strings.Contains(result.NextSteps[2], "continuo abandon --force") {
		t.Errorf("3行目に消し方が入っていない: %q", result.NextSteps[2])
	}
	if _, statErr := os.Stat(cf.Prepared.Path); statErr != nil {
		t.Fatalf("壊れた worktree を消してしまった: %v", statErr)
	}
}
