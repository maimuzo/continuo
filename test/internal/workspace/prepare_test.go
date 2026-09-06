// {"RUCM-CFG-SHA256": "065bcb4e3c565798e567b17782f7ad234b2ef7eea433c4c0d000a348a2942dd3", "SOURCE": "docs/spec/usecases/particular_case/本家のリポジトリへ PR を出す.cfg.json"}
package workspace_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/workspace"
)

// 目的: worktree を新しく作る経路が、置き場所の規則どおりのパスに本物の worktree と branch を
// 作り、herdr には worktree.open を呼ぶ（create ではない）ことを確認する（設計 3-22 の手順7段）。
// 与える情報: default_branch が main の issue と、初期コミットを1つ持つ本物のリポジトリ。
// 成功条件: 置き場所に worktree ができ、branch が切られ、herdr へ送られたメソッドが
// worktree.open ちょうど1件で、pane.split も tab.create も呼ばれていないこと（設計 4-5）。
func TestPrepare_新規に作りherdrにはworktree_openを呼ぶ(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})

	result, err := fx.Manager.Prepare(context.Background(), sampleIssue(188))
	if err != nil {
		t.Fatalf("Prepare に失敗した: %v", err)
	}

	if !result.Created {
		t.Fatal("新規に作ったのに Created が偽になっている")
	}
	wantSuffix := filepath.Join("github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	if !strings.HasSuffix(result.Path, wantSuffix) {
		t.Fatalf("worktree のパスが置き場所の規則に合わない: got %q, want の末尾 %q", result.Path, wantSuffix)
	}
	if _, err := os.Stat(filepath.Join(result.Path, "README.md")); err != nil {
		t.Fatalf("worktree の中身が checkout されていない: %v", err)
	}
	if got := runGit(t, result.Path, "rev-parse", "--abbrev-ref", "HEAD"); got != "continuo/octocat/hello-world/188" {
		t.Fatalf("worktree が想定の branch を指していない: got %q", got)
	}

	// **worktree を開く呼び出しは worktree.open ちょうど1件である**（設計 4-5）。
	// pane.split も tab.create も呼ばない。worktree.create でもない。
	//
	// **その前後に workspace.list が読み取りとして入る**（issue #19）。
	// 前の1回は「リポジトリの親 workspace が、この呼び出しより前からあったか」を見る。
	// 無かったので、後ろの1回で「continuo が開かせた親」の ID を控える。
	// **前からあった場合、後ろの1回は呼ばない**（閉じる責任を負わないため）。
	methods := fx.Herdr.Methods()
	// **worktree.open のあとに workspace.rename が続く。**worktree.open の label は
	// 既に開かれている workspace には効かないので、開き直すたびに書き直す（設計 3-3）。
	want := []string{
		herdr.MethodWorkspaceList,
		herdr.MethodWorktreeOpen,
		herdr.MethodWorkspaceList,
		herdr.MethodWorkspaceRename,
	}
	if !slices.Equal(methods, want) {
		t.Fatalf("herdr へ送ったメソッドが想定と違う: got %v, want %v", methods, want)
	}
	if result.HerdrWorkspaceID != "w9" {
		t.Fatalf("herdr workspace の ID を受け取れていない: got %q", result.HerdrWorkspaceID)
	}
	if result.HerdrPaneID != "w9:p1" {
		t.Fatalf("worktree.open が作った pane の ID を受け取れていない: got %q", result.HerdrPaneID)
	}

	// 上の並びのとおり、worktree.open は2件目である（1件目は workspace.list の読み取り）。
	params := fx.Herdr.Requests()[1].Params
	if params["path"] != result.Path {
		t.Fatalf("worktree.open に渡した path が違う: got %v, want %q", params["path"], result.Path)
	}
	// **branch は渡してはならない。**herdr は path と branch の片方だけを受け付け、
	// 両方来ると `invalid_request: exactly one of path or branch is required` で弾く
	// （実測: 2026-08-20。実機で着手が全部落ちた）。
	if _, ok := params["branch"]; ok {
		t.Fatalf("worktree.open に branch を送っている（path と片方だけにすること）: %v", params["branch"])
	}
	if params["focus"] != false {
		t.Fatalf("worktree.open に focus=false を送っていない: got %v", params["focus"])
	}
	// **label は `owner/repo/issues/N` の形である**（設計 3-3。issue #12）。
	wantLabel := herdr.IssueLabel("octocat", "hello-world", 188)
	if params["label"] != wantLabel {
		t.Fatalf("worktree.open の label が owner/repo/issues/N でない: got %v, want %q",
			params["label"], wantLabel)
	}

	// **既に開かれていた workspace のために label を書き直す。**
	// 上の並びのとおり、workspace.rename は4件目である。
	renameParams := fx.Herdr.Requests()[3].Params
	if renameParams["workspace_id"] != "w9" {
		t.Fatalf("workspace.rename の宛先が worktree.open の返した workspace でない: got %v",
			renameParams["workspace_id"])
	}
	if renameParams["label"] != wantLabel {
		t.Fatalf("workspace.rename の label が owner/repo/issues/N でない: got %v, want %q",
			renameParams["label"], wantLabel)
	}
}

// 目的: 用意の手順の1段目が `git worktree prune` であることを確認する（設計 3-22 の段1）。
// 与える情報: 「登録は残っているが実体が消えている」状態にした worktree のパス
// （worktree を作ってからディレクトリだけを消す）。
// 成功条件: prune が走るので、同じパスへの Prepare が
// `fatal: missing but already registered worktree` にならずに成功すること。
func TestPrepare_登録だけ残った状態をpruneしてから作る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188)

	first, err := fx.Manager.Prepare(context.Background(), issue)
	if err != nil {
		t.Fatalf("1回目の Prepare に失敗した: %v", err)
	}
	// 実体だけを消す（git の登録は残る）。branch も消して、作り直せる状態にする。
	if err := os.RemoveAll(first.Path); err != nil {
		t.Fatalf("worktree の実体を消せない: %v", err)
	}

	second, err := fx.Manager.Prepare(context.Background(), issue)
	if err != nil {
		t.Fatalf("prune が効いていれば成功するはずの Prepare が失敗した: %v", err)
	}
	if !second.Created {
		t.Fatal("実体が消えているのに再利用と判定された")
	}
}

// 目的: 目的のパスに worktree があり git にも登録されていれば再利用し、既存の身元ファイルを
// 先に読むことを確認する（設計 3-22 の段2 / 3-18）。
// 与える情報: 1度 Prepare してから身元ファイルを書いた worktree に対する2度目の Prepare。
// 成功条件: Created が偽になり、ExistingIdentity に既存の takeover_count が入っていること。
func TestPrepare_再利用のとき既存の身元ファイルを先に読む(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188)

	first := prepareWorktree(t, fx, issue)
	existing := fullIdentity(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	existing.TakeoverCount = 2
	if err := fx.Manager.WriteIdentity(context.Background(), first.Path, existing); err != nil {
		t.Fatalf("WriteIdentity に失敗した: %v", err)
	}

	second, err := fx.Manager.Prepare(context.Background(), issue)
	if err != nil {
		t.Fatalf("2回目の Prepare に失敗した: %v", err)
	}
	if second.Created {
		t.Fatal("既にある worktree なのに新規作成と判定された")
	}
	if second.ExistingIdentity == nil {
		t.Fatal("既存の身元ファイルが読まれていない")
	}
	if second.ExistingIdentity.TakeoverCount != 2 {
		t.Fatalf("既存の takeover_count が読めていない: got %d", second.ExistingIdentity.TakeoverCount)
	}
}

// 目的: 既存の身元ファイルが壊れていたら新規として扱う（読めなくても Prepare を失敗させない）
// ことを確認する（設計 3-18）。
// 与える情報: 再利用できる worktree に置いた、壊れた JSON の身元ファイル。
// 成功条件: Prepare が成功し、ExistingIdentity が nil になり、壊れたファイルは消えていないこと。
func TestPrepare_壊れた身元ファイルは新規として扱う(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188)

	first := prepareWorktree(t, fx, issue)
	identityPath := fx.Manager.IdentityPath(first.Path)
	if err := os.WriteFile(identityPath, []byte("{壊れている"), 0o600); err != nil {
		t.Fatalf("壊れた身元ファイルを書けない: %v", err)
	}

	second, err := fx.Manager.Prepare(context.Background(), issue)
	if err != nil {
		t.Fatalf("壊れた身元ファイルで Prepare が失敗した: %v", err)
	}
	if second.ExistingIdentity != nil {
		t.Fatalf("壊れた身元ファイルが採用されている: %+v", *second.ExistingIdentity)
	}
	if _, err := os.Stat(identityPath); err != nil {
		t.Fatalf("壊れた身元ファイルが消されている: %v", err)
	}
}

// 目的: 実体はあるが git の登録が無い worktree を、乗っ取らずエラーにすることを確認する
// （設計 3-22 の段3。空ディレクトリは git が黙って乗っ取る）。
// 与える情報: 置き場所の規則どおりのパスに人間が手で作った、git に登録の無いディレクトリ。
// 成功条件: Prepare が ErrUnregisteredWorktree を返し、そのディレクトリの中身が消えないこと。
func TestPrepare_登録の無い実体は乗っ取らずエラーにする(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})

	path := filepath.Join(
		fx.Manager.ResolvedRoot(), "github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("人間が作ったディレクトリを用意できない: %v", err)
	}
	marker := filepath.Join(path, "人間のファイル.txt")
	if err := os.WriteFile(marker, []byte("消してはならない\n"), 0o600); err != nil {
		t.Fatalf("目印のファイルを書けない: %v", err)
	}

	_, err := fx.Manager.Prepare(context.Background(), sampleIssue(188))
	if !errors.Is(err, workspace.ErrUnregisteredWorktree) {
		t.Fatalf("登録の無い実体なのに ErrUnregisteredWorktree にならない: %v", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("乗っ取らないはずのディレクトリの中身が消えている: %v", statErr)
	}
}

// 目的: `git worktree add -b` がパスの検査で落ちたとき、先に作られた孤児 branch を
// その場で消すことを確認する（設計 3-22 の段5。実測で確認された git の挙動）。
// 与える情報: 書き込めない（0500）親ディレクトリを置き場所の中に用意した状態での Prepare。
// 成功条件: Prepare がエラーになり、**branch が残っていない**こと。
func TestPrepare_worktree_addの失敗時に孤児branchを消す(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})

	// worktree を作る先の親ディレクトリを、書き込めない状態で先に用意する。
	parent := filepath.Join(fx.Manager.ResolvedRoot(), "github.com", "octocat", "hello-world")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatalf("親ディレクトリを作れない: %v", err)
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatalf("親ディレクトリを読み取り専用にできない: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	if _, err := fx.Manager.Prepare(context.Background(), sampleIssue(188)); err == nil {
		t.Fatal("worktree を作れないのに Prepare が成功した")
	}

	branches := runGit(t, fx.Repo.Dir, "branch", "--list", "continuo/octocat/hello-world/188")
	if strings.TrimSpace(branches) != "" {
		t.Fatalf("孤児 branch が残っている: %q", branches)
	}
}

// 目的: branch が作られる前に `git worktree add -b` が落ちた場合、孤児 branch の削除失敗
// としてエラーを塗り替えないことを確認する（設計 3-22 の段5 が求めているのは
// 「先に作られた孤児 branch を消す」ことだけであり、無いものの削除失敗は本当の原因を隠す）。
// 与える情報: 存在しない base を default_branch に持つ issue。
// 成功条件: Prepare が失敗し、そのエラー文に孤児 branch の話が出てこないこと。
func TestPrepare_branchが作られる前の失敗を孤児branchの削除失敗にしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188)
	issue.NativeRef = map[string]any{"default_branch": "存在しないbase"}

	_, err := fx.Manager.Prepare(context.Background(), issue)
	if err == nil {
		t.Fatal("base を解決できないのに Prepare が成功した")
	}
	if strings.Contains(err.Error(), "孤児 branch") {
		t.Fatalf("孤児 branch が無いのに削除の話がエラー文に出ている: %v", err)
	}

	branches := runGit(t, fx.Repo.Dir, "branch", "--list", "continuo/octocat/hello-world/188")
	if strings.TrimSpace(branches) != "" {
		t.Fatalf("branch が作られている: %q", branches)
	}
}

// 目的: herdr.worktree.base が null のとき、Issue.NativeRef["default_branch"] を base に使う
// ことを確認する（設計 3-22 の段4）。
// 与える情報: base を null にした設定と、default_branch が main の issue。
// 成功条件: 作られた branch の起点が main の commit になっていること。
func TestPrepare_baseがnullならdefault_branchを使う(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Herdr.Worktree.Base = nil },
	})

	result, err := fx.Manager.Prepare(context.Background(), sampleIssue(188))
	if err != nil {
		t.Fatalf("Prepare に失敗した: %v", err)
	}
	if result.Base.String() != "main" {
		t.Fatalf("base が default_branch から取られていない: got %q", result.Base.String())
	}
	head := runGit(t, result.Path, "rev-parse", "HEAD")
	mainHead := runGit(t, fx.Repo.Dir, "rev-parse", "main")
	if head != mainHead {
		t.Fatalf("worktree の起点が main でない: got %q, want %q", head, mainHead)
	}
}

// {"RUCM-PATH": "P013"}
//
// 目的: base を決められない issue を失敗として扱う（base を推測しない）ことを確認する
// （設計 3-22 の段4）。
//
// **「本家のリポジトリへ PR を出す」もここに載る。**あちらの issue は非公開のリポジトリにあり、
// **コードのリポジトリの名前は issue の本文にしか無い。**base を推測されると、continuo は
// 知りもしないリポジトリの branch を起点にしてしまう。
//
// 与える情報: base が null の設定と、NativeRef に default_branch を持たない issue。
// 成功条件: Prepare が ErrBaseUnknown を返し、worktree も branch も作られないこと。
func TestPrepare_baseもdefault_branchも無ければ失敗させる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Herdr.Worktree.Base = nil },
	})
	issue := sampleIssue(188)
	issue.NativeRef = map[string]any{}

	_, err := fx.Manager.Prepare(context.Background(), issue)
	if !errors.Is(err, workspace.ErrBaseUnknown) {
		t.Fatalf("base を決められないのに ErrBaseUnknown にならない: %v", err)
	}
	if branches := runGit(t, fx.Repo.Dir, "branch", "--list", "continuo/*"); strings.TrimSpace(branches) != "" {
		t.Fatalf("base が決まらないのに branch が作られている: %q", branches)
	}
}

// 目的: clone が引けない issue を飛ばせるようにエラーで区別できることを確認する
// （設計 3-22。continuo は勝手に clone しない）。
// 与える情報: 空文字を返す ghq の差し替え。
// 成功条件: Prepare が ErrCloneNotFound を返すこと。
func TestPrepare_cloneが無ければErrCloneNotFoundになる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		GhqList: func(_ context.Context, _, _ string) (string, error) { return "", nil },
	})

	_, err := fx.Manager.Prepare(context.Background(), sampleIssue(188))
	if !errors.Is(err, workspace.ErrCloneNotFound) {
		t.Fatalf("clone が無いのに ErrCloneNotFound にならない: %v", err)
	}
}

// 目的: 人間が別の branch へ切り替えた worktree を、そのまま再利用しないことを確認する
// （設計 3-22 の段2・段3。乗っ取らない）。
//
// **再利用の前に git へ現物を答えさせないと、**エージェントが意図しない branch の上で
// 作業し、食い違いに気づくのは片付けのとき（成果が別 branch に積まれたあと）になる。
//
// **番兵は専用のものである**（issue #142）。「登録されていません」と名乗ると、
// 読んだ人間は docs/FAQ.md の別の症状（ディレクトリだけが残っている）へ行き、
// **生きている worktree を消しにいく。**
//
// 与える情報: 用意したあとに別の branch へ切り替えた worktree と、同じ issue の再用意。
// 成功条件: ErrWorktreeBranchMismatch になり（ErrUnregisteredWorktree にはならない）、
// 文面に確かめ方と switch の案内が入り、worktree が消されずに残ること。
func TestPrepare_別のbranchへ切り替えられたworktreeは再利用しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	ctx := context.Background()
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	runGit(t, prepared.Path, "checkout", "--quiet", "-b", "人間が切り替えた")

	_, err := fx.Manager.Prepare(ctx, sampleIssue(188))
	if !errors.Is(err, workspace.ErrWorktreeBranchMismatch) {
		t.Fatalf("別の branch を出している worktree を再利用している: %v", err)
	}
	// **登録の無い実体と取り違えていないこと。**登録はされている。
	if errors.Is(err, workspace.ErrUnregisteredWorktree) {
		t.Fatalf("別の branch を「登録が無い」と取り違えている: %v", err)
	}
	// **detached HEAD とも取り違えていないこと。**原因も直し方も違う。
	if errors.Is(err, workspace.ErrWorktreeDetached) {
		t.Fatalf("別の branch を detached HEAD と取り違えている: %v", err)
	}
	msg := err.Error()
	// **引数の取りこぼしを文面で押さえる。**指定子と引数の数がずれると fmt がここへ書く。
	for _, bad := range []string{"%!", "(MISSING)", "(EXTRA"} {
		if strings.Contains(msg, bad) {
			t.Fatalf("文面に引数の取りこぼしがある（%s）: %s", bad, msg)
		}
	}
	for _, want := range []string{"人間が切り替えた", "switch", "【確かめ方】", prepared.Path} {
		if !strings.Contains(msg, want) {
			t.Errorf("文面に %q が入っていない: %s", want, msg)
		}
	}
	if _, statErr := os.Stat(prepared.Path); statErr != nil {
		t.Fatalf("再利用できない worktree を消している: %v", statErr)
	}
}

// 目的: detached HEAD の worktree に、別の branch にいる場合とは違う番兵と文面を返すことを
// 確認する（issue #132）。
//
// **文面を分けないと、人間は「"HEAD" をチェックアウトしています」という案内を読み、**
// **"HEAD" という名前の branch を探しに行く。**原因も直し方も伝わらない。
//
// 与える情報: 用意したあとに commit を直接チェックアウトした worktree と、同じ issue の再用意。
// 成功条件: ErrWorktreeDetached になり（ErrUnregisteredWorktree にはならない）、
// 文面に detached HEAD と switch の案内が入り、worktree が消されずに残ること。
func TestPrepare_detachedHEADのworktreeは専用の番兵で断る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	ctx := context.Background()
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	runGit(t, prepared.Path, "checkout", "--quiet", "--detach", "HEAD")

	_, err := fx.Manager.Prepare(ctx, sampleIssue(188))
	if !errors.Is(err, workspace.ErrWorktreeDetached) {
		t.Fatalf("detached HEAD なのに ErrWorktreeDetached にならない: %v", err)
	}
	// **別の branch にいる場合と取り違えていないこと。**
	if errors.Is(err, workspace.ErrUnregisteredWorktree) {
		t.Fatalf("detached HEAD を「登録が無い」と取り違えている: %v", err)
	}
	// **branch の食い違いとも取り違えていないこと**（issue #142）。
	// 3-68 の通知が「飛ばした理由の種類」を鍵にするので、2つを混ぜると数え直しが効かない。
	if errors.Is(err, workspace.ErrWorktreeBranchMismatch) {
		t.Fatalf("detached HEAD を branch の食い違いと取り違えている: %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"detached HEAD", "switch", prepared.Path} {
		if !strings.Contains(msg, want) {
			t.Errorf("文面に %q が入っていない: %s", want, msg)
		}
	}
	if _, statErr := os.Stat(prepared.Path); statErr != nil {
		t.Fatalf("detached HEAD の worktree を消している: %v", statErr)
	}
}

// 目的: 着手の段0（CheckWorktreeUsable）でも、detached HEAD を同じ番兵で断ることを確認する
// （issue #132）。Prepare と段0 で判断が食い違うと、Status を書いてから落ちる。
//
// 与える情報: 用意したあとに commit を直接チェックアウトした worktree。
// 成功条件: CheckWorktreeUsable が ErrWorktreeDetached を返すこと。
func TestCheckWorktreeUsable_detachedHEADを段0で断る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	ctx := context.Background()
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	runGit(t, prepared.Path, "checkout", "--quiet", "--detach", "HEAD")

	err := fx.Manager.CheckWorktreeUsable(ctx, sampleIssue(188))
	if !errors.Is(err, workspace.ErrWorktreeDetached) {
		t.Fatalf("段0 が detached HEAD を断っていない: %v", err)
	}
}

// 目的: 着手の段0（CheckWorktreeUsable）でも、branch の食い違いを専用の番兵で断ることを
// 確認する（issue #142）。
//
// **daemon が実際に通るのはこちらである。**preflight が CheckWorktreeUsable を呼び、
// ここを通ったものだけが Prepare の段2 へ行く。**9個の引数は2箇所へ手で写してあり、
// i18n.Errorf は個数を検査しない。**片方だけ直すと、巡回のループが出す本物の文面だけが
// `%!s(MISSING)` になる。
//
// 与える情報: 用意したあとに別の branch へ切り替えた worktree。
// 成功条件: CheckWorktreeUsable が ErrWorktreeBranchMismatch を返し、
// 文面に引数の取りこぼしが1つも無いこと。
func TestCheckWorktreeUsable_別のbranchを段0で断る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	ctx := context.Background()
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	runGit(t, prepared.Path, "checkout", "--quiet", "-b", "人間が切り替えた")

	err := fx.Manager.CheckWorktreeUsable(ctx, sampleIssue(188))
	if !errors.Is(err, workspace.ErrWorktreeBranchMismatch) {
		t.Fatalf("段0 が branch の食い違いを断っていない: %v", err)
	}
	// **段2 と同じ番兵であること。**食い違うと、Status を書いてから落ちる。
	if errors.Is(err, workspace.ErrUnregisteredWorktree) {
		t.Fatalf("段0 が「登録が無い」と取り違えている: %v", err)
	}
	msg := err.Error()
	// **引数の取りこぼしを文面で押さえる。**指定子と引数の数がずれると fmt がここへ書く。
	for _, bad := range []string{"%!", "(MISSING)", "(EXTRA"} {
		if strings.Contains(msg, bad) {
			t.Fatalf("段0 の文面に引数の取りこぼしがある（%s）: %s", bad, msg)
		}
	}
	for _, want := range []string{"人間が切り替えた", "switch", "【確かめ方】", prepared.Path} {
		if !strings.Contains(msg, want) {
			t.Errorf("段0 の文面に %q が入っていない: %s", want, msg)
		}
	}
}

// 目的: rebase を途中で止めた worktree も detached として断ることを確認する（issue #132）。
//
// **文面と docs/FAQ.md は rebase 中を名指しで案内している。**
// **その前提（rebase の途中は porcelain が detached を出す）を、テストで固定する。**
// ここが崩れると、案内だけが残って判定が別の分岐へ落ちる。
//
// 与える情報: 衝突で止めた rebase の途中にある worktree。
// 成功条件: ErrWorktreeDetached になること。
func TestPrepare_rebaseの途中もdetachedとして断る(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	ctx := context.Background()
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	// 同じファイルを別々に変えた2つの commit を作り、rebase で必ず衝突させる。
	conflict := filepath.Join(prepared.Path, "conflict.txt")
	base := runGit(t, prepared.Path, "rev-parse", "HEAD")
	if err := os.WriteFile(conflict, []byte("こちら\n"), 0o644); err != nil {
		t.Fatalf("ファイルを書けない: %v", err)
	}
	runGit(t, prepared.Path, "add", "conflict.txt")
	runGit(t, prepared.Path, "commit", "--quiet", "-m", "こちら側")
	runGit(t, prepared.Path, "checkout", "--quiet", "-b", "他方", base)
	if err := os.WriteFile(conflict, []byte("あちら\n"), 0o644); err != nil {
		t.Fatalf("ファイルを書けない: %v", err)
	}
	runGit(t, prepared.Path, "add", "conflict.txt")
	runGit(t, prepared.Path, "commit", "--quiet", "-m", "あちら側")

	// **衝突で止まることが目的なので、失敗を許す。**runGit は失敗でテストを止めるので使えない。
	rebase := exec.Command("git", "-C", prepared.Path, "rebase", prepared.Branch.String())
	if out, err := rebase.CombinedOutput(); err == nil {
		t.Fatalf("rebase が衝突せずに通ってしまった:\n%s", out)
	}

	err := fx.Manager.CheckWorktreeUsable(ctx, sampleIssue(188))
	if !errors.Is(err, workspace.ErrWorktreeDetached) {
		t.Fatalf("rebase の途中を detached として断っていない: %v", err)
	}
}

// 目的: CheckWorktreeUsable が、Prepare と同じ判断を**1バイトも書かずに**返すことを確認する
// （着手の段0 でこれを呼び、失敗が確定している issue は Status を動かさずに飛ばす）。
// 与える情報: 3つの状況（まだ何も無い / 登録の無い実体がある / 正しく再利用できる）。
// 成功条件: それぞれ nil / ErrUnregisteredWorktree / nil を返し、
// **どの場合も worktree を作らず、herdr を1回も呼ばないこと。**
func TestCheckWorktreeUsable_書かずに着手できるかを判定する(t *testing.T) {
	ctx := context.Background()

	t.Run("まだ何も無いなら通す", func(t *testing.T) {
		fx := newFixture(t, fixtureOptions{})
		if err := fx.Manager.CheckWorktreeUsable(ctx, sampleIssue(188)); err != nil {
			t.Fatalf("まだ何も無いのに落としている: %v", err)
		}
		if methods := fx.Herdr.Methods(); len(methods) != 0 {
			t.Fatalf("検査だけのはずなのに herdr を呼んでいる: %v", methods)
		}
	})

	t.Run("登録の無い実体があるなら落とす", func(t *testing.T) {
		fx := newFixture(t, fixtureOptions{})
		loc := filepath.Join(
			fx.Root, "github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
		if err := os.MkdirAll(loc, 0o700); err != nil {
			t.Fatalf("登録の無い実体を置けません: %v", err)
		}
		err := fx.Manager.CheckWorktreeUsable(ctx, sampleIssue(188))
		if !errors.Is(err, workspace.ErrUnregisteredWorktree) {
			t.Fatalf("登録の無い実体を通している: %v", err)
		}
		// **検査は消さない。**中身が要るかどうかを判断できない。
		if _, statErr := os.Stat(loc); statErr != nil {
			t.Fatalf("検査だけのはずなのに実体を消している: %v", statErr)
		}
	})

	t.Run("正しく再利用できるなら通す", func(t *testing.T) {
		fx := newFixture(t, fixtureOptions{})
		prepared := prepareWorktree(t, fx, sampleIssue(188))
		if err := fx.Manager.CheckWorktreeUsable(ctx, sampleIssue(188)); err != nil {
			t.Fatalf("再利用できる worktree を落としている: %v", err)
		}
		if _, statErr := os.Stat(prepared.Path); statErr != nil {
			t.Fatalf("検査だけのはずなのに worktree を消している: %v", statErr)
		}
	})
}

// 目的: 目的の branch を**別の場所の worktree** が使っているとき、CheckWorktreeUsable が
// それを落とすことを確認する（設計 3-16b）。
//
// **目的のパスには何も無い。**それでも `git worktree add <目的のパス> <branch>` は
// `fatal: '<branch>' is already used by worktree at '<別のパス>'` で必ず落ちる。
// **目的のパスだけを見る検査（ErrUnregisteredWorktree）ではこの経路を拾えない。**
//
// 与える情報: 置き場所の外に、同じ branch を出す worktree を1つ作っておく。
// 成功条件: ErrBranchInUseElsewhere を返し、**目的のパスを作らず、
// 別の場所の worktree も消さないこと。**エラー文が `continuo abandon` を案内すること。
func TestCheckWorktreeUsable_branchを別のworktreeが使っているなら落とす(t *testing.T) {
	ctx := context.Background()
	fx := newFixture(t, fixtureOptions{})
	issue := sampleIssue(188)
	branch := "continuo/octocat/hello-world/188"

	// **置き場所の外**に、同じ branch を出す worktree を作る（人間が手で切った状態）。
	elsewhere := filepath.Join(t.TempDir(), "別の場所")
	runGit(t, fx.Repo.Dir, "worktree", "add", "-b", branch, elsewhere, fx.Repo.Base)

	err := fx.Manager.CheckWorktreeUsable(ctx, issue)
	if !errors.Is(err, workspace.ErrBranchInUseElsewhere) {
		t.Fatalf("branch を別の worktree が使っているのに通している: %v", err)
	}
	// **直し方をエラー文が持っていること。**ログに出るだけなので、ここに無いと人間へ届かない。
	for _, want := range []string{branch, elsewhere, "continuo abandon", issue.URL} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("エラー文に %q が無い: %s", want, err.Error())
		}
	}

	// **検査は1バイトも書かない。**目的のパスは作られていないこと。
	target := filepath.Join(
		fx.Root, "github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Errorf("検査だけのはずなのに目的のパスを作っている: %v", statErr)
	}
	// **別の場所の worktree も消さない。**中身が要るかどうかを判断できない。
	if _, statErr := os.Stat(elsewhere); statErr != nil {
		t.Errorf("検査だけのはずなのに別の場所の worktree を消している: %v", statErr)
	}
	if methods := fx.Herdr.Methods(); len(methods) != 0 {
		t.Errorf("検査だけのはずなのに herdr を呼んでいる: %v", methods)
	}
}

// 目的: **目的のパス自身**がその branch を出しているときは落とさないことを確認する
// （3-22 の段2 の再利用の経路）。
//
// **これを除外しないと、continuo が自分で作った worktree を再利用できなくなり、
// 2回目以降の着手が全部落ちる。**
//
// 与える情報: 1度 Prepare を通して作った worktree（branch は目的のパスが出している）。
// 成功条件: nil を返し、worktree が残っていること。
func TestCheckWorktreeUsable_目的のパス自身がbranchを使っているなら通す(t *testing.T) {
	ctx := context.Background()
	fx := newFixture(t, fixtureOptions{})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	// 目的のパスがその branch を出していることを、git に答えさせて確かめる。
	if got := runGit(t, prepared.Path, "rev-parse", "--abbrev-ref", "HEAD"); got != prepared.Branch.String() {
		t.Fatalf("前提が崩れている: worktree が %q を出している", got)
	}

	if err := fx.Manager.CheckWorktreeUsable(ctx, sampleIssue(188)); err != nil {
		t.Fatalf("再利用できる worktree を branch の重複として落としている: %v", err)
	}
	if _, statErr := os.Stat(prepared.Path); statErr != nil {
		t.Fatalf("検査だけのはずなのに worktree を消している: %v", statErr)
	}
}
