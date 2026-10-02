// {"RUCM-CFG-SHA256": "d1270e1c4559d46586a022205db0ab15bf1abd0265675eaef1f85d89575ca3a3", "SOURCE": "docs/spec/usecases/particular_case/worktree と branch を片付ける.cfg.json"}
//
// **ユースケース記述「worktree と branch を片付ける」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
// テストは `Manager.Cleanup` を直に呼ぶので、巡回が片付けの対象を選ぶ段は通らない。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package workspace_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/loop"
	"github.com/maimuzo/continuo/internal/normalize"
	"github.com/maimuzo/continuo/internal/workspace"
)

// {"RUCM-PATH": "P006"}
//
// 目的: 未コミットの変更（未追跡のファイル）が残っていれば worktree を消さないことを確認する
// （設計 3-9 の手順2。エージェントが作った成果物が消えるのを防ぐ）。
// 与える情報: worktree の中に置いた、commit も add もしていないファイル。
// 成功条件: Deferred が真、Removed が偽、worktree が残り、herdr に worktree.remove を
// 送っていないこと。ShouldComment が真であること（1回目の見送り）。
func Test_worktreeとbranchを片付ける_P006_未コミットの変更があれば消さない(t *testing.T) {
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

// {"RUCM-PATH": "P003"}
//
// 目的: upstream があり push 済みなら片付け、そのとき worktree.remove に渡すのが
// **身元ファイルの herdr workspace の ID** であることを確認する（設計 3-9 の手順2b・3）。
// 与える情報: worktree の中で commit して push し、upstream を持たせた状態。
// 成功条件: Removed が真、worktree.remove の params が workspace_id（path でも branch でもない）、
// **worktree.remove のあとに workspace.close を呼んでいない**こと、branch が消えていること、
// issue ごとの設定ファイルが消えていること。
func Test_worktreeとbranchを片付ける_P003_push済みなら消してbranchと設定ファイルも消す(t *testing.T) {
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

// {"RUCM-PATH": "P006"}
//
// 目的: upstream があり push されていない commit が残っていれば消さないことを確認する
// （設計 3-9 の手順2b の upstream がある側）。
// 与える情報: push したあとにもう1つ commit を積んだ worktree。
// 成功条件: Deferred が真になり、理由に push されていないことが書かれていること。
func Test_worktreeとbranchを片付ける_P006_push済みでないcommitが残っていれば消さない(t *testing.T) {
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

// {"RUCM-PATH": "P006"}
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
func Test_worktreeとbranchを片付ける_P006_baseが分からなければ消さない(t *testing.T) {
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

// {"RUCM-PATH": "P002"}
//
// 目的: worktree の実体を消し切れなかったら、branch も設定ファイルも消さずに止まることを
// 確認する（RUCM のステップ12。実体が残ったまま先へ進むと、中身のある worktree だけが
// 取り残される）。
// 与える情報: 書き込みを落とした worktree のディレクトリ（`os.RemoveAll` が必ず失敗する）。
// 成功条件: Cleanup がエラーを返し、worktree が残り、branch と issue ごとの設定ファイルが
// どちらも残っていること。
func Test_worktreeとbranchを片付ける_P002_worktreeを消し切れなければbranchも設定ファイルも消さない(t *testing.T) {
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

// {"RUCM-PATH": "P008"}
//
// 目的: 消す直前の封じ込め検査に落ちたら、何も消さずに失敗することを確認する
// （設計 3-20。「消す直前」がいちばん危ない検査点である）。
// 与える情報: 置き場所の外側にある worktree のパス。
// 成功条件: Cleanup がエラーを返し、herdr へ何も送らず、その worktree が残っていること。
func Test_worktreeとbranchを片付ける_P008_置き場所の外側は消さずに失敗する(t *testing.T) {
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

// {"RUCM-PATH": "P009"}
//
// 目的: cleanup.enabled が偽なら何も消さず、かつ「見送った」と分かる戻り値になることを
// 確認する（設計 3-9 の手順5。デバッグ時に中身を見たい場合がある）。
// 与える情報: cleanup.enabled を偽にした設定。
// 成功条件: Removed が偽・Deferred が真・理由が入り・ShouldComment が偽で、
// worktree が残り、herdr へ何も送っていないこと
// （理由だけが入って Deferred が偽だと、呼び出し側が「消した」「見送った」「無効」を
// 区別できない）。
func Test_worktreeとbranchを片付ける_P009_無効なら何もしない(t *testing.T) {
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

// {"RUCM-PATH": "P013"}
//
// 目的: 片付けを始める判定が cleanup.on_states に入った時点であり、
// active でなくなった時点ではないことを確認する（設計 3-9 の手順1）。
// 与える情報: on_states が Done だけの設定と、Done / done / In Review / Blocked の各 Status。
// 成功条件: Done は大文字小文字を無視して真になり、In Review と Blocked は偽になること
// （そこで消すと、人間が回答して Ready へ戻したときに作業成果が失われる）。
func Test_worktreeとbranchを片付ける_P013_on_statesに入った時点で片付ける(t *testing.T) {
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

// {"RUCM-PATH": "P004"}
//
// 目的: herdr が別のパスを開いている workspace を答えたら、何も消さないことを確認する
// （設計 3-9 の段3。検算の答えが食い違ったら止まる。RUCM のステップ9 で消す宛先を
// 確定できないときの経路であり、before_remove も実行しない）。
// 与える情報: 常に別のパスを worktree として答えるテスト用herdr mock。
// 成功条件: Cleanup がエラーになり、worktree.remove を1度も送らず、worktree が残ること。
func Test_worktreeとbranchを片付ける_P004_herdrが別のパスを答えたら何も消さない(t *testing.T) {
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

// {"RUCM-PATH": "P016"}
//
// 目的: 身元ファイルの JSON が壊れていても、消さずにエラー付きで返すことを確認する
// （設計 3-4 の段2。段6 の書き込み途中で落ちた場合）。
// 与える情報: 壊れた JSON の身元ファイル。
// 成功条件: 結果に Err 付きで含まれ、Identity が nil で、ファイルが残っていること。
func Test_worktreeとbranchを片付ける_P016_壊れた身元ファイルはエラー付きで返す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	root := fx.Manager.ResolvedRoot()

	broken := filepath.Join(root, "github.com", "octocat", "hello-world", "continuo-1")
	putIdentityFile(t, broken, `{"issue_url": "https://exa`)

	found, err := fx.Manager.Scan()
	if err != nil {
		t.Fatalf("Scan に失敗した: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("走査の結果が1件でない: %+v", found)
	}
	if found[0].Identity != nil {
		t.Fatalf("壊れているのに中身が読めたことになっている: %+v", found[0])
	}
	if !errors.Is(found[0].Err, workspace.ErrIdentityBroken) {
		t.Fatalf("Err が ErrIdentityBroken でない: %v", found[0].Err)
	}
	if _, statErr := os.Stat(filepath.Join(broken, ".continuo.json")); statErr != nil {
		t.Fatalf("壊れた身元ファイルが消されている: %v", statErr)
	}
}

// {"RUCM-PATH": "P017"}
//
// 目的: 置き場所そのものを読めなければ走査が失敗し、**どの worktree にも触らない**ことを
// 確認する（RUCM のステップ2。材料を取れない巡回では何も決めない）。
// 与える情報: 読み取りの permission を落とした置き場所。
// 成功条件: Scan がエラーを返し、結果が0件で、置き場所の中身が消えていないこと。
func Test_worktreeとbranchを片付ける_P017_置き場所を読めなければ走査が失敗する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	root := fx.Manager.ResolvedRoot()

	worktree := filepath.Join(root, "github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	putIdentityFile(t, worktree, `{"issue_identifier":"octocat/hello-world#188"}`)

	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatalf("置き場所の permission を落とせない: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	found, err := fx.Manager.Scan()
	if err == nil {
		t.Fatalf("置き場所を読めないのにエラーにならなかった: %+v", found)
	}
	if len(found) != 0 {
		t.Fatalf("読めないのに worktree を拾っている: %+v", found)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatalf("置き場所の permission を戻せない: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(worktree, ".continuo.json")); statErr != nil {
		t.Fatalf("走査に失敗しただけで worktree が消えている: %v", statErr)
	}
}

// {"RUCM-PATH": "P006"}
//
// 目的: 消さなかった worktree について、issue へのコメントが1回だけになることを確認する
// （設計 3-9 の手順2c。「1回だけ」の記録は身元ファイルに持つ）。
// 与える情報: 1回目の見送りのあとに MarkCleanupDeferred を呼んでから、もう一度片付けを試みる。
// 成功条件: 2回目の ShouldComment が偽になること。
func Test_worktreeとbranchを片付ける_P006_見送りのコメントは1回だけになる(t *testing.T) {
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

// {"RUCM-PATH": "P006"}
//
// 目的: upstream が無いときに base からの差分で判定することを確認する
// （設計 3-9 の手順2b の upstream が無い側。**commit の有無では判定しない**）。
// 与える情報: 一度も push していない worktree に積んだ commit（作業ツリーは clean）。
// 成功条件: Deferred が真になること（commit があっても upstream が無いので失うものがある）。
func Test_worktreeとbranchを片付ける_P006_upstreamが無くbaseと差分があれば消さない(t *testing.T) {
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

// {"RUCM-PATH": "P006"}
//
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
func Test_worktreeとbranchを片付ける_P006_gitが答えられないときは次にすべきことを添える(t *testing.T) {
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

// {"RUCM-PATH": "P007"}
//
// 目的: worktree の `.git` が別のリポジトリを指すよう書き換えられていたら、
// **そのリポジトリに破壊的な git コマンドを撃たない**ことを確認する
// （設計 3-9 の段4。`git branch -D` の宛先を git の答えだけで決めない）。
// 与える情報: `.git` を別のリポジトリへ向けた worktree と、その別のリポジトリにある branch。
// 成功条件: Cleanup がエラーになり、別のリポジトリの branch が残っていること。
func Test_worktreeとbranchを片付ける_P007_worktreeのgitが書き換えられていたら別のリポジトリに触らない(t *testing.T) {
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

// {"RUCM-PATH": "P001"}
//
// 目的: 実体の無い登録が**自分が消した1件だけ**なら `git worktree prune` を撃つことを
// 確認する（設計 3-37-9b）。**守りが効きすぎて何も掃除しなくなる方向の壊れを殺す。**
// 掃除しないままにすると、次に同じパスへ worktree を作るとき
// `missing but already registered worktree` で着手が失敗する。
// 与える情報: continuo が消す worktree だけが登録されたリポジトリ。
// herdr は「消しました」と答えるのに実体を消さない。
// 成功条件: Cleanup が成功し、**その登録が `git worktree list` から消えている**こと。
// 残ったものに prune の案内が1件も入っていないこと。
func Test_worktreeとbranchを片付ける_P001_自分が消した1件だけならpruneを撃つ(t *testing.T) {
	cf := newCleanupFixture(t, nil)
	keepWorktreeOnRemove(cf)

	result, err := cf.Manager.Cleanup(context.Background(), cleanupRequest(cf))
	if err != nil {
		t.Fatalf("Cleanup に失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("worktree を片付けていない: %+v", *result)
	}

	registered := registeredWorktrees(t, cf)
	if strings.Contains(registered, cf.Prepared.Path) {
		t.Fatalf("実体を消した worktree の登録 %q が残っている（prune を撃っていない）:\n%s",
			cf.Prepared.Path, registered)
	}
	if left := pruneLeftovers(result.Leftovers, cf.Repo.Dir); len(left) > 0 {
		t.Fatalf("掃除できているのに登録が残ったと言っている: %v", left)
	}
}

// {"RUCM-PATH": "P005"}
//
// 目的: 巡回の中の片付け（NoWait）が、押さえられた clone では何も消さずに ErrCloneBusy を返し、
// 閉じたあとの次の巡回で片付くことを確かめる（巡回が押さえを待つと、止まった run の検知と着手が止まる）。
// 与える情報: worktree を1つ用意したあと、statusline取得の workspace を同じ clone に開き、
// NoWait の Cleanup を呼ぶ。閉じたあとにもう一度 NoWait の Cleanup を呼ぶ。
// 成功条件: 1回目は待たずに ErrCloneBusy が返り、`worktree.open`・`worktree.remove` が届かず、
// worktree も設定ファイルも残ること。引き直しの仕事が TryDo で積まれていること。
// 閉じたあとの2回目は Removed で返り、worktree が消えること。
func Test_worktreeとbranchを片付ける_P005_NoWaitの片付けは押さえられたcloneで何も消さずErrCloneBusyを返す(t *testing.T) {
	l := loop.New(nil)
	l.Start()
	t.Cleanup(l.Close)
	fake := newStatuslineHerdr(t)
	runner := &recordingRunner{inner: l, fake: fake, keyed: make(chan string, 16)}
	cf := newCleanupFixtureWith(t, fixtureOptions{Herdr: fake, Loop: runner})
	fx := &statuslineFixture{managerFixture: cf.managerFixture, Loop: l, Runner: runner}

	ws, openedAt := openStatusline(t, fx, cf.Repo.Dir)
	busy := asyncResult(func() error {
		_, err := cf.Manager.Cleanup(context.Background(), forceCleanup(cf.Prepared.Path, true))
		return err
	})
	if err := waitResult(t, busy, "NoWait の Cleanup"); !errors.Is(err, workspace.ErrCloneBusy) {
		t.Fatalf("Cleanup = %v, want ErrCloneBusy", err)
	}
	got := methodsAfter(fake, openedAt)
	if slices.Contains(got, herdr.MethodWorktreeOpen) || slices.Contains(got, herdr.MethodWorktreeRemove) {
		t.Fatalf("押さえられた clone で worktree.open か worktree.remove を送った: %v", got)
	}
	if _, err := os.Stat(cf.Prepared.Path); err != nil {
		t.Fatalf("ErrCloneBusy なのに worktree が消えた: %v", err)
	}
	if _, err := os.Stat(cf.SettingsPath); err != nil {
		t.Fatalf("ErrCloneBusy なのに設定ファイルが消えた: %v", err)
	}
	sawTry := false
	for _, c := range runner.Calls() {
		if c.Try && c.Key != "" && errors.Is(c.Err, loop.ErrBusy) {
			sawTry = true
		}
	}
	if !sawTry {
		t.Fatalf("引き直しの仕事が TryDo で積まれていない: %+v", runner.Calls())
	}

	if outcome, err := cf.Manager.CloseStatuslineWorkspace(context.Background(), ws); err != nil || outcome != workspace.StatuslineClosed {
		t.Fatalf("CloseStatuslineWorkspace = (%v, %v)", outcome, err)
	}
	result, err := cf.Manager.Cleanup(context.Background(), forceCleanup(cf.Prepared.Path, true))
	if err != nil {
		t.Fatalf("閉じたあとの NoWait の Cleanup が失敗した: %v", err)
	}
	if !result.Removed {
		t.Fatalf("閉じたあとの Cleanup で片付かなかった: %+v", *result)
	}
	if _, err := os.Stat(cf.Prepared.Path); !os.IsNotExist(err) {
		t.Fatalf("片付けたのに worktree が残っている: %v", err)
	}
}
