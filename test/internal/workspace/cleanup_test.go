package workspace_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	settingsDir := filepath.Join(settingsBase, "maimuzo-koetsumugi-188")
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

// 目的: upstream が無く base も分からないときは、判定できないので消さないことを確認する
// （設計 3-9 の手順2b。base を推測して消すと成果を失う）。
// 与える情報: Base を空にした CleanupRequest。
// 成功条件: Deferred が真になり、worktree が残ること。
func TestCleanup_baseが分からなければ消さない(t *testing.T) {
	cf := newCleanupFixture(t, nil)

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
	runGit(t, cf.Repo.Dir, "branch", "continuo/maimuzo/koetsumugi/999")
	tamperIdentity(t, cf, func(identity *workspace.Identity) {
		identity.Branch = "continuo/maimuzo/koetsumugi/999"
	})

	if _, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf)); err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	branches := runGit(t, cf.Repo.Dir, "branch", "--list", "continuo/maimuzo/koetsumugi/999")
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

// 目的: herdr が別のパスを開いている workspace を答えたら、何も消さないことを確認する
// （設計 3-9 の段3。検算の答えが食い違ったら止まる）。
// 与える情報: 常に別のパスを worktree として答えるテスト用herdr mock。
// 成功条件: Cleanup がエラーになり、worktree.remove を1度も送らず、worktree が残ること。
func TestCleanup_herdrが別のパスを答えたら何も消さない(t *testing.T) {
	other := filepath.Join(t.TempDir(), "別の-worktree")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatalf("別のパスを作れない: %v", err)
	}
	open := worktreeOpenResult("w9", "w9:p1")
	open["worktree"] = map[string]any{"path": other}
	fake := newFakeHerdr(t, map[string]any{
		herdr.MethodWorktreeOpen:   open,
		herdr.MethodWorktreeRemove: worktreeRemoveResult("w9", ""),
	})
	cf := newCleanupFixtureWith(t, fixtureOptions{Herdr: fake})

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
