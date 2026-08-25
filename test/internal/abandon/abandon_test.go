// {"RUCM-CFG-SHA256": "a8f622fa57a04d542d3eae708b15be7b4fac4c6094bfd01c571c9a0e3b107dfc", "SOURCE": "docs/spec/usecases/particular_case/着手を取り消す.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**「着手を取り消す」のテストパスのうち、
// **判断が分かれるところ**を通るものに、それぞれ1本以上のテストがある。
// 残りは分岐の組み合わせ違いであり、組み合わせの片側を通しているテストで振る舞いが決まる。
//
// **CFG が数える組み合わせには、同時には起こりえないものが混ざる。**継続監視が動いている
// ことと `--dry-run` を指定したことは別々の分岐として数えられるが、実装では
// **`--dry-run` は継続監視への手離しを通らない**（設計 3-4 の段1）。
// 通る本数を数えるのはツールであり、**この見出しには本数を書かない**（2箇所に持つと必ずずれる）。
package abandon_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/abandon"
	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/lock"
)

// {"RUCM-PATH": "P617"}
//
// 目的: 片付ける worktree が1つも見つからないとき、何も消さずに終了コード 0 で
// 終わることを確認する（設計 3-4 の段2。消すものが無いのは失敗ではない）。
// **ボードも読まないこと**もあわせて確かめる（読むと `gh` を起動して API 枠を使う）。
// 与える情報: issue 188 の worktree だけがある置き場所と、issue 999 の URL。
// 成功条件: 終了コードが 0、issue 188 の worktree が残っている、
// herdr へ worktree.remove を送っていない、ボードのアダプタを1度も作っていないこと。
func TestAbandon_worktreeが無ければ何も消さずに終わる(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	code := fx.Run(t, 999, nil)

	assertExit(t, fx, code, abandon.ExitOK)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonNotFound, issueURL(999)))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
	if fx.TrackerBuilds() != 0 {
		t.Fatalf("消すものが無いのにボードのアダプタを %d 回作っている", fx.TrackerBuilds())
	}
}

// {"RUCM-PATH": "P618"}
//
// 目的: 同じ issue の worktree が2つあるときに、何も消さずに止まることを確認する
// （設計 3-4 の段2。どちらを消すかは人間が中身を見て決めることであり、
// continuo が選んではならない）。
// 与える情報: issue 188 の worktree と、身元ファイルの issue_url を 188 に書き換えた
// issue 189 の worktree。
// 成功条件: 終了コードが 1、2つとも残っている、herdr へ worktree.remove を
// 送っていない、候補の一覧が出ていること。
func TestAbandon_同じissueのworktreeが2つあれば止まる(t *testing.T) {
	fx := newFixture(t)
	first := fx.Prepare(t, 188)
	second := fx.Prepare(t, 189)
	// **身元ファイルの issue_url だけを 188 に付け替える。**パスも branch も 189 のまま
	// なので、「パスから組み立てて探す」実装ではこの状態を作れない。
	fx.WriteIdentity(t, second, issueURL(188), 188,
		filepath.Join(fx.SettingsRoot, "octocat-hello-world-189", "settings.json"))

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonErrMultiple, 2, issueURL(188)))
	assertWorktreeExists(t, fx, first.Path)
	assertWorktreeExists(t, fx, second.Path)
	assertNoRemoval(t, fx)
}

// {"RUCM-PATH": "P510"}
//
// 目的: `--dry-run` が何も消さないことを確認する（設計 3-4 の段3 で終わる）。
// **worktree と branch を消すコマンドなので、消す前に何が消えるかを見られなければならない。**
// 与える情報: issue 188 の worktree と `--dry-run`。
// 成功条件: 終了コードが 0、worktree が残っている、branch も残っている、
// herdr へ worktree.remove を送っていない、消すものの一覧と
// 「何も消していません」の1行が出ていること。
func TestAbandon_dryRunは何も消さない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.DryRun = true })

	assertExit(t, fx, code, abandon.ExitOK)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonPlanHeader))
	assertContains(t, fx, i18n.T(i18n.KeyAbandonPlanWorktree, prepared.Path))
	assertContains(t, fx, i18n.T(i18n.KeyAbandonDryRunNote))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
	if !branchExists(t, fx, prepared.Branch.String()) {
		t.Fatalf("--dry-run なのに branch %s が消えている", prepared.Branch.String())
	}
	if len(fx.Tracker.Updates()) != 0 {
		t.Fatalf("--dry-run なのに Status を動かしている: %v", fx.Tracker.Updates())
	}
}

// {"RUCM-PATH": "P510"}
//
// 目的: worktree の `.git` が壊れていても、`--dry-run` が**何が消えるかを見せられる
// 範囲で見せて終わる**ことを確認する（設計 3-4 の段3。issue #23）。
// **これができないと、`abandon` が要るまさにその状況で `abandon` を使えない。**
// 利用者は `fatal: invalid gitfile format` の1行だけを渡されて終わっていた。
// **「失うものはありません」と偽ってはならない。**数えられなかったことは数えられなかったと出す。
// 与える情報: `.git` を空・でたらめ・不在の3通りに壊した issue 188 の worktree と `--dry-run`。
// 成功条件: 終了コードが 0、worktree のパスと branch が出ている、
// 調べられなかったことと git の理由が出ている、**「0 ファイル」と出していない**、
// worktree が残っている、herdr へ worktree.remove を送っていないこと。
func TestAbandon_gitファイルが壊れていてもdryRunで消えるものを見せる(t *testing.T) {
	for _, how := range []gitFileBreakage{gitFileEmpty, gitFileGarbage, gitFileMissing} {
		t.Run(string(how), func(t *testing.T) {
			fx := newFixture(t)
			prepared := fx.Prepare(t, 188)
			fx.BreakGitFile(t, prepared, how)

			code := fx.Run(t, 188, func(opts *abandon.Options) { opts.DryRun = true })

			assertExit(t, fx, code, abandon.ExitOK)
			assertContains(t, fx, i18n.T(i18n.KeyAbandonPlanWorktree, prepared.Path))
			assertContains(t, fx, i18n.T(i18n.KeyAbandonPlanBranch,
				prepared.Branch.String(), prepared.Base.String()))
			assertContains(t, fx, i18n.T(i18n.KeyAbandonPlanDirtyUnknown))
			assertContains(t, fx, i18n.T(i18n.KeyAbandonPlanUnpushedUnknown))
			// **git が何と言ったのかをそのまま見せる。**理由を隠すと、利用者は
			// 何を直せばよいのかを判断できない。
			assertContains(t, fx, ".git")
			assertContains(t, fx, i18n.T(i18n.KeyAbandonDryRunNote))
			if strings.Contains(fx.Output(), i18n.T(i18n.KeyAbandonPlanDirty, 0)) {
				t.Fatalf("調べられていないのに「0 ファイル」と出している\n出力:\n%s", fx.Output())
			}
			assertWorktreeExists(t, fx, prepared.Path)
			assertNoRemoval(t, fx)
		})
	}
}

// {"RUCM-PATH": "P485"}
//
// 目的: worktree の `.git` が壊れていて失うものを調べられないとき、`--force` が
// 無ければ何も消さずに終了コード 1 で止まることを確認する（設計 3-4 の段3。issue #23）。
// **中身が分からないものを黙って消してはならない。**
// **「失うものがある」とは別の文言で言う。**同じ文言だと、利用者は「何が残っているのか」を
// 探しに行ってしまうが、探す手立ては無い。
// 与える情報: `.git` を空にした issue 188 の worktree と、`--force` を付けない実行。
// 成功条件: 終了コードが 1、調べ切れなかったことを理由に止まった1行が出ている、
// worktree が残っている、branch も残っている、herdr へ worktree.remove を送っていないこと。
func TestAbandon_gitファイルが壊れていればforceなしでは消さない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)
	fx.BreakGitFile(t, prepared, gitFileEmpty)

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonErrUndeterminedWithoutForce))
	if strings.Contains(fx.Output(), i18n.T(i18n.KeyAbandonErrLossWithoutForce)) {
		t.Fatalf("調べ切れなかったことを「失うものがある」の文言で出している\n出力:\n%s", fx.Output())
	}
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
	if !branchExists(t, fx, prepared.Branch.String()) {
		t.Fatalf("何も消さないはずなのに branch %s が消えている", prepared.Branch.String())
	}
}

// {"RUCM-PATH": "P466"}
//
// 目的: worktree の `.git` が壊れていても、`--force` を付ければ
// **worktree のディレクトリと branch と herdr の workspace を消し切れる**ことを
// 確認する（設計 3-4 の段4。issue #23）。
// **`git worktree remove` はこの状態を必ず断る**（`validation failed, cannot remove
// working tree`。実測: 2026-08-25）。**断られたまま終わると、利用者には手が無くなる。**
// 与える情報: `.git` を空・でたらめ・不在の3通りに壊した issue 188 の worktree と `--force`。
// 成功条件: 終了コードが 0、worktree のディレクトリが消えている、branch が消えている、
// herdr の workspace が1つも残っていないこと。
func TestAbandon_gitファイルが壊れていてもforceで消し切る(t *testing.T) {
	for _, how := range []gitFileBreakage{gitFileEmpty, gitFileGarbage, gitFileMissing} {
		t.Run(string(how), func(t *testing.T) {
			fx := newFixture(t)
			prepared := fx.Prepare(t, 188)
			fx.BreakGitFile(t, prepared, how)

			code := fx.Run(t, 188, func(opts *abandon.Options) { opts.Force = true })

			assertExit(t, fx, code, abandon.ExitOK)
			assertWorktreeGone(t, fx, prepared.Path)
			if branchExists(t, fx, prepared.Branch.String()) {
				t.Fatalf("worktree を消したのに branch %s が残っている", prepared.Branch.String())
			}
			if ids := fx.Herdr.OpenWorkspaceIDs(); len(ids) != 0 {
				t.Fatalf("herdr の workspace が閉じていない: %v\n出力:\n%s", ids, fx.Output())
			}
		})
	}
}

// {"RUCM-PATH": "P485"}
//
// 目的: 未コミットの変更が残っているとき、`--force` が無ければ何も消さずに
// 終了コード 1 で止まることを確認する（設計 3-4 の段3）。
// 与える情報: worktree の中に置いた、commit も add もしていないファイル。
// 成功条件: 終了コードが 1、worktree が残っている、herdr へ worktree.remove を
// 送っていない、失われるファイル数と「--force を付けてください」が出ていること。
func TestAbandon_未コミットの変更があればforceなしでは消さない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)
	if err := os.WriteFile(filepath.Join(prepared.Path, "作りかけ.md"), []byte("途中\n"), 0o600); err != nil {
		t.Fatalf("未追跡のファイルを書けません: %v", err)
	}

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonPlanDirty, 1))
	assertContains(t, fx, i18n.T(i18n.KeyAbandonErrLossWithoutForce))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
}

// {"RUCM-PATH": "P466"}
//
// 目的: `--force` を付ければ、未コミットの変更があっても worktree と branch を
// 消すことを確認する（設計 3-4 の段4）。
// 与える情報: 未追跡のファイルが残った worktree と `--force`。
// 成功条件: 終了コードが 0、worktree が消えている、branch も消えている、
// 消したことを伝える1行が出ていること。
func TestAbandon_forceを付ければ未コミットの変更があっても消す(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)
	if err := os.WriteFile(filepath.Join(prepared.Path, "作りかけ.md"), []byte("途中\n"), 0o600); err != nil {
		t.Fatalf("未追跡のファイルを書けません: %v", err)
	}

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.Force = true })

	assertExit(t, fx, code, abandon.ExitOK)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonRemoved, prepared.Path, prepared.Branch.String()))
	assertWorktreeGone(t, fx, prepared.Path)
	if branchExists(t, fx, prepared.Branch.String()) {
		t.Fatalf("worktree を消したのに branch %s が残っている", prepared.Branch.String())
	}
}

// {"RUCM-PATH": "P466"}
//
// 目的: herdr へ pane の一覧を問い合わせられなくても、`--force` があれば
// 片付けを最後まで通すことを確認する（設計 3-4 の段4 の前。issue #23）。
// **herdr ごと落ちている状況で worktree を1つも片付けられないのでは、
// 壊れた状態を片付ける道具として成り立たない。**
// **確かめずに消したことは必ず言う。**黙って消すと、利用者は pane の生死が
// 確認済みだと思い込む。
// 与える情報: 誰も掴んでいないロックファイル、繋がらない herdr の socket、`--force`。
// 成功条件: 終了コードが 0、確かめずに消したことを伝える1行が出ている、
// worktree が消えていること。
func TestAbandon_herdrに繋げなくてもforceがあれば片付ける(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)
	// **worktree を用意したあとで socket を落とす。**用意の段階では herdr が要る。
	unreachable := fx.CloseHerdr(t)

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.Force = true })

	assertExit(t, fx, code, abandon.ExitOK)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonPaneCheckSkipped, unreachable))
	assertWorktreeGone(t, fx, prepared.Path)
}

// {"RUCM-PATH": "P473"}
//
// 目的: herdr へ pane の一覧を問い合わせられず、`--force` も無いときは、
// **これまでどおり何も消さずに止まる**ことを確認する（設計 3-4 の段4 の前）。
// **確かめられないまま消してよいと決めるのは人間である。**
// 与える情報: 誰も掴んでいないロックファイル、繋がらない herdr の socket、
// `--force` を付けない実行。
// 成功条件: 終了コードが 1、問い合わせられない理由が出ている、worktree が残っていること。
func TestAbandon_herdrに繋げずforceも無ければ消さない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)
	unreachable := fx.CloseHerdr(t)

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonErrPaneList, unreachable))
	assertWorktreeExists(t, fx, prepared.Path)
}

// {"RUCM-PATH": "P617"}
//
// 目的: 身元ファイルを読めない worktree を候補から外したことを、**人間に見せる**ことを
// 確認する（設計 3-4 の段2。issue #23）。
// **黙って飛ばすと「worktree はありません」としか見えない。**目の前にあるものが
// 無いことにされ、利用者は次に何をすればよいかを判断できない。
// 与える情報: 身元ファイルの JSON を壊した issue 188 の worktree。
// 成功条件: 終了コードが 0、飛ばした worktree のパスと身元ファイルのパスが出ている、
// worktree が残っている、herdr へ worktree.remove を送っていないこと。
func TestAbandon_身元ファイルを読めないworktreeを飛ばしたことを言う(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)
	identityPath := fx.Manager.IdentityPath(prepared.Path)
	if err := os.WriteFile(identityPath, []byte("{壊れた JSON"), 0o600); err != nil {
		t.Fatalf("身元ファイルを壊せません: %v", err)
	}

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitOK)
	assertContains(t, fx, prepared.Path)
	assertContains(t, fx, identityPath)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonNotFound, issueURL(188)))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
}

// {"RUCM-PATH": "P466"}
//
// 目的: `--to` を付けなければ Status を1文字も動かさないことを確認する
// （設計 3-4 の段5。片付けたあとの置き場所は、その issue をこれからどうするかで
// 決まるので、continuo が勝手に決めてはならない）。
// 与える情報: 失うものが無い issue 188 の worktree と、`--to` を付けない実行。
// 成功条件: 終了コードが 0、worktree が消えている、ボードへの書き込みが0件、
// 「Status は動かしていません」が出ていること。
func TestAbandon_toを付けなければStatusを動かさない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitOK)
	assertWorktreeGone(t, fx, prepared.Path)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonStatusLeftAlone))
	if len(fx.Tracker.Updates()) != 0 {
		t.Fatalf("--to が無いのに Status を動かしている: %v", fx.Tracker.Updates())
	}
}

// {"RUCM-PATH": "P309"}
//
// 目的: `--to` を付けたときだけ Status がその値へ動くことを確認する（設計 3-4 の段5）。
// 与える情報: 失うものが無い issue 188 の worktree と `--to Ice Box`。
// 成功条件: 終了コードが 0、worktree が消えている、ボードへの書き込みが1件で
// 値が `Ice Box`、宛先がボードから引いた project item の ID であること
// （**身元ファイルの project_item_id を宛先にしてはならない。**エージェントが
// 書き換えられる値なので、別の issue の Status を動かせてしまう）。
func TestAbandon_toを付ければStatusをその値へ動かす(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.ToState = "Ice Box" })

	assertExit(t, fx, code, abandon.ExitOK)
	assertWorktreeGone(t, fx, prepared.Path)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonStatusMoved, "Ice Box"))

	updates := fx.Tracker.Updates()
	if len(updates) != 1 {
		t.Fatalf("ボードへの書き込みが1件ではなく %d 件だった: %v", len(updates), updates)
	}
	if updates[0].State != "Ice Box" {
		t.Fatalf("Status を %q ではなく %q へ動かした", "Ice Box", updates[0].State)
	}
	if updates[0].ItemID != "PVTI_test" {
		t.Fatalf("書き込みの宛先が %q ではなく %q だった", "PVTI_test", updates[0].ItemID)
	}
	if fx.Tracker.Fetches() == 0 {
		t.Fatal("ボードから現在の Status を引かずに書き込んでいる")
	}
}

// {"RUCM-PATH": "P039"}
//
// 目的: continuo が動いているとき、`--park` の値へ Status を動かして手を離させ、
// その worktree の pane が消えるのを待ってから消すことを確認する（設計 3-4 の段1）。
// **待たずに消すと、消した worktree の中で Claude Code が動き続ける。**
// 与える情報: テストが先に掴んだロックファイル（＝continuo が動いている）、
// tracker.active_states に入る Status（In Progress）、2回目までは pane を返し
// 3回目から返さないテスト用herdr mock、`--park Ice Box`。
// 成功条件: 終了コードが 0、Status を `Ice Box` へ動かしている、
// pane が消えるまで pane の一覧を3回以上引いている、worktree が消えていること。
func TestAbandon_continuoが動いていればparkへ動かしてpaneが消えるのを待つ(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	held, err := lock.Acquire(fx.LockPath)
	if err != nil {
		t.Fatalf("テストがロックを掴めません: %v", err)
	}
	defer func() { _ = held.Release() }()

	fx.Herdr.SetPaneListScript(func(call int) []map[string]any {
		if call <= 2 {
			return panesAt(prepared.Path)
		}
		return nil
	})

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.ParkState = "Ice Box" })

	assertExit(t, fx, code, abandon.ExitOK)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonRunning, fx.LockPath))
	assertContains(t, fx, i18n.T(i18n.KeyAbandonPaneGone))

	updates := fx.Tracker.Updates()
	if len(updates) != 1 || updates[0].State != "Ice Box" {
		t.Fatalf("手を離させるための書き込みが `Ice Box` の1件ではなかった: %v", updates)
	}
	if fx.Herdr.PaneListCalls() < 3 {
		t.Fatalf("pane が消えるのを待っていない（pane の一覧を %d 回しか引いていない）",
			fx.Herdr.PaneListCalls())
	}
	assertWorktreeGone(t, fx, prepared.Path)
}

// {"RUCM-PATH": "P039"}
//
// 目的: `--park` を付けなかったとき、手を離させる先が tracker.failure_state に
// なることを確認する（設計 3-4 の段1。failure_state が active_states に入らないことは
// 設定の検証が保証しているので、そこへ動かせば必ず active から外れる）。
// 与える情報: テストが先に掴んだロックファイル、Status が In Progress のボード、
// pane が1つも無いテスト用herdr mock、`--park` を付けない実行。
// 成功条件: 終了コードが 0、ボードへの書き込みが既定の failure_state（Blocked）1件であること。
func TestAbandon_parkの指定が無ければfailureStateへ動かす(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	held, err := lock.Acquire(fx.LockPath)
	if err != nil {
		t.Fatalf("テストがロックを掴めません: %v", err)
	}
	defer func() { _ = held.Release() }()

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitOK)
	updates := fx.Tracker.Updates()
	if len(updates) != 1 {
		t.Fatalf("ボードへの書き込みが1件ではなく %d 件だった: %v", len(updates), updates)
	}
	if updates[0].State != fx.Config.Tracker.FailureState {
		t.Fatalf("手を離させる先が %q ではなく %q だった",
			fx.Config.Tracker.FailureState, updates[0].State)
	}
	assertWorktreeGone(t, fx, prepared.Path)
}

// {"RUCM-PATH": "P050"}
//
// 目的: 上限まで待っても pane が閉じないとき、**何も消さずに**終了コード 1 で
// 止まることを確認する（設計 3-4 の段1）。
// 与える情報: テストが先に掴んだロックファイル、いつまでも同じ pane を返し続ける
// テスト用herdr mock、上限3秒・間隔1秒（時計は Sleep のたびに進める）。
// 成功条件: 終了コードが 1、worktree が残っている、herdr へ worktree.remove を
// 送っていない、閉じなかったことを伝える1行が出ていること。
func TestAbandon_paneが閉じなければ何も消さずに止まる(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	held, err := lock.Acquire(fx.LockPath)
	if err != nil {
		t.Fatalf("テストがロックを掴めません: %v", err)
	}
	defer func() { _ = held.Release() }()

	fx.Herdr.SetPaneListScript(func(_ int) []map[string]any { return panesAt(prepared.Path) })

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonErrPaneRemains, 3*time.Second, "w1:p1"))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
}

// {"RUCM-PATH": "P622"}
//
// 目的: issue の URL として読めないものを渡されたとき、**置き場所を1度も走査せずに**
// 終了コード 1 で止まることを確認する（設計 3-4 の段2）。
// **読めない URL で照合を始めると、身元ファイルの issue_url と何とも一致しないまま
// 「消すものが無い」と答えてしまう。**言い分けなければ、利用者は打ち間違いに気づけない。
// 与える情報: issue 188 の worktree と、pull request の URL。
// 成功条件: 終了コードが 1、worktree が残っている、herdr へ worktree.remove を
// 送っていない、ボードのアダプタを1度も作っていない、読めない理由が出ていること。
func TestAbandon_issueのURLとして読めなければ何も触らない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	const bad = "https://github.com/octocat/hello-world/pull/188"
	_, parseErr := abandon.ParseIssueURL(bad)
	if parseErr == nil {
		t.Fatalf("%q を読めてしまった（テストの前提が崩れている）", bad)
	}

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.IssueURL = bad })

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, parseErr.Error())
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
	if fx.TrackerBuilds() != 0 {
		t.Fatalf("URL を読めていないのにボードのアダプタを %d 回作っている", fx.TrackerBuilds())
	}
}

// {"RUCM-PATH": "P621"}
//
// 目的: WORKFLOW.md を読めないとき、何も触らずに終了コード 1 で止まることを確認する
// （設計 3-4 の段1 より前）。**設定が読めなければ、worktree の置き場所も
// tracker.active_states も分からない。**分からないまま消しにいってはならない。
// 与える情報: issue 188 の worktree と、存在しない WORKFLOW.md のパス。
// 成功条件: 終了コードが 1、worktree が残っている、herdr へ worktree.remove を
// 送っていない、読めなかった理由が出ていること。
func TestAbandon_設定ファイルを読めなければ何も触らない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	missing := filepath.Join(fx.Root, "存在しない-WORKFLOW.md")
	_, loadErr := config.Load(missing)
	if loadErr == nil {
		t.Fatalf("%s を読めてしまった（テストの前提が崩れている）", missing)
	}

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.ConfigPath = missing })

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonErrConfigLoad, loadErr))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
}

// {"RUCM-PATH": "P620"}
//
// 目的: ロックファイルそのものを開けないとき、**「continuo が動いている」と
// 取り違えずに**終了コード 1 で止まることを確認する（設計 3-4 の段1）。
// **取り違えると被害が正反対になる。**開けないのは `runtime.lock_file` の
// 打ち間違いであり、そのまま「動いていない」と判定すれば、動いている continuo の
// 足元から worktree を消す。
// 与える情報: issue 188 の worktree と、無いディレクトリの下を指すロックファイルのパス。
// 成功条件: 終了コードが 1、worktree が残っている、herdr へ worktree.remove を
// 送っていない、ロックファイルのパスと理由が出ていること。
func TestAbandon_ロックファイルを開けなければ止まる(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	unopenable := filepath.Join(fx.Root, "無いディレクトリ", "continuo.lock")
	_, lockErr := lock.Acquire(unopenable)
	if lockErr == nil {
		t.Fatalf("%s を開けてしまった（テストの前提が崩れている）", unopenable)
	}

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.Deps.LockPath = unopenable })

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonErrLockFile, unopenable, lockErr))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
}

// {"RUCM-PATH": "P104"}
//
// 目的: continuo が動いているのに issue がボードに載っていないとき、**何も消さずに**
// 終了コード 1 で止まることを確認する（設計 3-4 の段1）。
// **Status を確かめられないまま消すと、走っているエージェントの足元から
// ディレクトリが消える。**手を離させたかどうかを確かめられないからである。
// 与える情報: テストが先に掴んだロックファイルと、その issue を載せていないボード。
// 成功条件: 終了コードが 1、worktree が残っている、herdr へ worktree.remove を
// 送っていない、ボードへの書き込みが0件、確かめられない理由が出ていること。
func TestAbandon_動いているのにボードから引けなければ何もしない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)
	fx.Tracker.SetNotListed()

	held, err := lock.Acquire(fx.LockPath)
	if err != nil {
		t.Fatalf("テストがロックを掴めません: %v", err)
	}
	defer func() { _ = held.Release() }()

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonErrParkStateUnknown,
		"octocat/hello-world#188", i18n.T(i18n.KeyAbandonBoardNotListed, "octocat/hello-world#188")))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
	if len(fx.Tracker.Updates()) != 0 {
		t.Fatalf("Status を確かめられないのに書き込んでいる: %v", fx.Tracker.Updates())
	}
}

// {"RUCM-PATH": "P052"}
//
// 目的: 手を離させるための書き込みがボードに入らなかったとき、**何も消さずに**
// 終了コード 1 で止まることを確認する（設計 3-4 の段1）。
// **書けなければ continuo はその issue を掴んだままである。**掴んだまま worktree を
// 消せば、走っているエージェントの足元からディレクトリが消える。
// 与える情報: テストが先に掴んだロックファイル、tracker.active_states に入る Status、
// 書き込みを受け付けないボード。
// 成功条件: 終了コードが 1、worktree が残っている、herdr へ worktree.remove を
// 送っていない、書けなかったことを伝える1行が出ていること。
func TestAbandon_手を離させる書き込みが入らなければ何も消さない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)
	fx.Tracker.SetWriteRejected()

	held, err := lock.Acquire(fx.LockPath)
	if err != nil {
		t.Fatalf("テストがロックを掴めません: %v", err)
	}
	defer func() { _ = held.Release() }()

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonParkNotWritten, fx.Config.Tracker.FailureState))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
}

// {"RUCM-PATH": "P091"}
//
// 目的: continuo が動いていても、Status が tracker.active_states に入っていなければ
// **ボードへ1文字も書かずに**片付けへ進むことを確認する（設計 3-4 の段1）。
// **その issue はもう continuo の手を離れている。**離れているものを動かせば、
// 人間が置いた値を continuo が勝手に上書きすることになる。
// 与える情報: テストが先に掴んだロックファイルと、Status が `Done` のボード。
// 成功条件: 終了コードが 0、ボードへの書き込みが0件、動かさない理由が出ている、
// worktree が消えていること。
func TestAbandon_作業中の状態でなければ手を離させる書き込みをしない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)
	fx.Tracker.SetState("Done")

	held, err := lock.Acquire(fx.LockPath)
	if err != nil {
		t.Fatalf("テストがロックを掴めません: %v", err)
	}
	defer func() { _ = held.Release() }()

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitOK)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonParkNotActive, "Done"))
	assertWorktreeGone(t, fx, prepared.Path)
	if len(fx.Tracker.Updates()) != 0 {
		t.Fatalf("作業中の状態ではないのに Status を動かしている: %v", fx.Tracker.Updates())
	}
}

// {"RUCM-PATH": "P051"}
//
// 目的: pane が閉じるのを待っているあいだに実行を中断されたら、**何も消さずに**
// 終了コード 1 で止まることを確認する（設計 3-4 の段1）。
// **中断は「待つのをやめてよい」であって「消してよい」ではない。**
// 与える情報: テストが先に掴んだロックファイル、いつまでも同じ pane を返し続ける
// テスト用herdr mock、待機を打ち切って偽を返す Sleep（＝SIGINT / SIGTERM を受けた状態）。
// 成功条件: 終了コードが 1、worktree が残っている、herdr へ worktree.remove を
// 送っていない、**中断されたことと残っている pane が出ている**（時間切れの文言では
// 出ない。上限が短すぎたのかと読み違えるため）、手を離させた Status を
// 元へ戻していないこと。
func TestAbandon_pane待ちを中断されたら何も消さない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	held, err := lock.Acquire(fx.LockPath)
	if err != nil {
		t.Fatalf("テストがロックを掴めません: %v", err)
	}
	defer func() { _ = held.Release() }()

	fx.Herdr.SetPaneListScript(func(_ int) []map[string]any { return panesAt(prepared.Path) })

	code := fx.Run(t, 188, func(opts *abandon.Options) {
		// **待たずに打ち切る。**`SIGINT` / `SIGTERM` で ctx が終わった状態と同じである。
		opts.Deps.Sleep = func(_ context.Context, _ time.Duration) bool { return false }
	})

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonErrPaneWaitInterrupted, "w1:p1"))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
	if strings.Contains(fx.Output(), i18n.T(i18n.KeyAbandonErrPaneRemains, 3*time.Second, "w1:p1")) {
		t.Fatalf("中断を時間切れの文言で出している\n出力:\n%s", fx.Output())
	}

	updates := fx.Tracker.Updates()
	if len(updates) != 1 || updates[0].State != fx.Config.Tracker.FailureState {
		t.Fatalf("手を離させた Status を元へ戻している（書き込み: %v）", updates)
	}
}

// {"RUCM-PATH": "P472"}
//
// 目的: 片付けそのものに失敗したとき、**Status を動かさずに**終了コード 1 で
// 止まることを確認する（設計 3-4 の段4 と段5）。
// **段5 は段4 が済んだときだけ走る。**worktree が残っているのに「片付けたあとの
// Status」へ動かすと、ボードの上では終わったことになって人間の目から消える。
//
// **herdr にエラーを返させるだけでは足りない。**herdr が断ったときは、continuo が
// worktree の実体を自分で消しにいく（issue #23）。**その手も塞いで初めて
// 「本当に片付けられなかった」状態になる。**ここでは worktree のディレクトリから
// 書き込みの権限を落として、実体を1つも消せなくする。
// 与える情報: issue 188 の worktree、`worktree.remove` にエラーを返す
// テスト用herdr mock、書き込みできない worktree のディレクトリ、`--to Ice Box`。
// 成功条件: 終了コードが 1、worktree が残っている、ボードへの書き込みが0件、
// 片付けに失敗した理由が出ていること。
func TestAbandon_片付けに失敗したらStatusを動かさない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)
	fx.Herdr.SetWorktreeRemoveError("internal_error", "worktree を消せません")
	freezeDir(t, prepared.Path)

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.ToState = "Ice Box" })

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, "worktree を消せません")
	assertWorktreeExists(t, fx, prepared.Path)
	if len(fx.Tracker.Updates()) != 0 {
		t.Fatalf("片付けに失敗したのに Status を動かしている: %v", fx.Tracker.Updates())
	}
}

// {"RUCM-PATH": "P470"}
//
// 目的: branch を消さなかったとき、**「消しました」と言わない**ことを確認する
// （設計 3-4 の段4）。
// **身元ファイルは worktree の中にあり、エージェントが書き換えられる。**現物と
// 食い違う branch 名は消さないのが正しく、そのとき消したと報告してはならない。
// 与える情報: issue 188 の worktree と、checkout している branch と違う名前を書いた
// 身元ファイル。
// 成功条件: 終了コードが 0、worktree が消えている、**現物の branch が残っている**、
// branch が残ったことと、その理由を伝える行が出ていること。
func TestAbandon_branchを消さなかったら消したと言わない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)
	// **接頭辞は continuo のままにする。**接頭辞で弾かれたのではなく、
	// 「現物と一致しないから消さなかった」経路を通すためである。
	stale := prepared.Branch.String() + "-old"
	fx.SetIdentityBranch(t, prepared, stale)

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitOK)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonRemovedWithLeftovers, prepared.Path))
	// **理由まで画面に出ること。**ログにだけ書くと、abandon は Logger を渡さないので
	// 誰にも届かない（issue #23）。
	assertContains(t, fx, i18n.T(i18n.KeyWorkspaceLeftoverBranchUndeletable, stale,
		i18n.T(i18n.KeyWorkspaceLeftoverBranchReasonHeadMismatch, prepared.Branch.String())))
	assertWorktreeGone(t, fx, prepared.Path)
	if !branchExists(t, fx, prepared.Branch.String()) {
		t.Fatalf("身元ファイルと食い違う branch %s を消している", prepared.Branch.String())
	}
}

// {"RUCM-PATH": "P311"}
//
// 目的: `--to` があるのに issue をボードから引けないとき、**片付けは済ませたうえで**
// 終了コード 1 で終わることを確認する（設計 3-4 の段5）。
// **片付けを巻き戻さない。**worktree はもう消えており、戻す手段は無い。
// 人間には「消えたが Status は動いていない」と伝えるほかない。
// 与える情報: issue 188 の worktree、その issue を載せていないボード、`--to Ice Box`。
// 成功条件: 終了コードが 1、worktree が消えている、ボードへの書き込みが0件、
// 引けなかった理由が出ていること。
func TestAbandon_片付けたあとにボードから引けなければ1で終わる(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)
	fx.Tracker.SetNotListed()

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.ToState = "Ice Box" })

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonErrStatusTargetUnknown,
		"octocat/hello-world#188", i18n.T(i18n.KeyAbandonBoardNotListed, "octocat/hello-world#188")))
	assertWorktreeGone(t, fx, prepared.Path)
	if len(fx.Tracker.Updates()) != 0 {
		t.Fatalf("ボードから引けないのに書き込んでいる: %v", fx.Tracker.Updates())
	}
}

// {"RUCM-PATH": "P310"}
//
// 目的: `--to` の書き込みがボードに入らなかったとき、**片付けは済ませたうえで**
// 終了コード 1 で終わることを確認する（設計 3-4 の段5）。
// **「消えたが Status は動いていない」を 0 で返してはならない。**利用者はボードを
// 見ずに次へ進むので、動いていないことに気づけない。
// 与える情報: issue 188 の worktree、書き込みを受け付けないボード、`--to Ice Box`。
// 成功条件: 終了コードが 1、worktree が消えている、書けなかったことを伝える1行が
// 出ていること。
func TestAbandon_片付けたあとの書き込みが入らなければ1で終わる(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)
	fx.Tracker.SetWriteRejected()

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.ToState = "Ice Box" })

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonStatusNotWritten, "Ice Box"))
	assertWorktreeGone(t, fx, prepared.Path)
}

// {"RUCM-PATH": "P036"}
//
// 目的: continuo が動いている状態で `--to` まで通したとき、ボードへの書き込みが
// **手を離させる1件と片付けたあとの1件の、この順の2件だけ**になることを確認する
// （設計 3-4 の段1 と段5）。
// **順番が入れ替わると、片付けたあとの値がすぐ park の値へ上書きされる。**
// 与える情報: テストが先に掴んだロックファイル、tracker.active_states に入る Status、
// 2回目までは pane を返すテスト用herdr mock、`--park Blocked` の既定と `--to Ice Box`。
// 成功条件: 終了コードが 0、worktree が消えている、書き込みが2件で
// 1件目が tracker.failure_state、2件目が `Ice Box` であること。
func TestAbandon_動いている状態でtoまで通せば書き込みは2件だけになる(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	held, err := lock.Acquire(fx.LockPath)
	if err != nil {
		t.Fatalf("テストがロックを掴めません: %v", err)
	}
	defer func() { _ = held.Release() }()

	fx.Herdr.SetPaneListScript(func(call int) []map[string]any {
		if call <= 2 {
			return panesAt(prepared.Path)
		}
		return nil
	})

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.ToState = "Ice Box" })

	assertExit(t, fx, code, abandon.ExitOK)
	assertWorktreeGone(t, fx, prepared.Path)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonStatusMoved, "Ice Box"))

	updates := fx.Tracker.Updates()
	if len(updates) != 2 {
		t.Fatalf("ボードへの書き込みが2件ではなく %d 件だった: %v", len(updates), updates)
	}
	if updates[0].State != fx.Config.Tracker.FailureState {
		t.Fatalf("1件目が手を離させる書き込み（%q）ではなく %q だった",
			fx.Config.Tracker.FailureState, updates[0].State)
	}
	if updates[1].State != "Ice Box" {
		t.Fatalf("2件目が %q ではなく %q だった", "Ice Box", updates[1].State)
	}
}

// {"RUCM-PATH": "P128"}
//
// 目的: 継続監視が動いている状態で `--dry-run` を叩いても、**ボードへ1文字も書かず、
// エージェントに手を離させない**ことを確認する（設計 3-4 の段1。`--dry-run` は
// 段1 の後半を通らない）。
// **README は「先に `--dry-run` を叩け」と勧めている。**勧めた手順が Status を書き換え、
// 動いているエージェントの手を離させてはならない。
// 与える情報: テストが先に掴んだロックファイル（＝継続監視が動いている）、
// tracker.active_states に入る Status（In Progress）、いつまでも pane を返し続ける
// テスト用herdr mock、`--dry-run`。
// 成功条件: 終了コードが 0、ボードへの書き込みが0件、worktree が残っている、
// herdr へ worktree.remove を送っていない、実行したときに動かす先の予告と
// 「何も消していません」が出ていること。
func TestAbandon_動いていてもdryRunならボードへ書かない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	held, err := lock.Acquire(fx.LockPath)
	if err != nil {
		t.Fatalf("テストがロックを掴めません: %v", err)
	}
	defer func() { _ = held.Release() }()

	// **手を離させていないので pane は閉じない。**通ってしまえば上限まで待って
	// 終了コード 1 になり、一覧を1行も出さずに終わる。
	fx.Herdr.SetPaneListScript(func(_ int) []map[string]any { return panesAt(prepared.Path) })

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.DryRun = true })

	assertExit(t, fx, code, abandon.ExitOK)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonPlanParkPending, fx.Config.Tracker.FailureState))
	assertContains(t, fx, i18n.T(i18n.KeyAbandonDryRunNote))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
	if len(fx.Tracker.Updates()) != 0 {
		t.Fatalf("--dry-run なのにボードへ書いている: %v", fx.Tracker.Updates())
	}
}

// {"RUCM-PATH": "P466"}
//
// 目的: 判定のために取ったロックを、**実行が終わるまで握り続ける**ことを確認する
// （設計 3-4 の段1）。
// **その場で手放すと、直後に継続監視が起動でき、その足元から worktree を消す。**
// abandon は git と herdr の RPC を何度も叩くので、窓は秒単位で開く。
// 与える情報: 誰も掴んでいないロックファイルと、pane の一覧を引かれた時点で
// ロックの獲得を試みるテスト用herdr mock（＝実行の途中で継続監視が起動しようとした状態）。
// 成功条件: 終了コードが 0、実行の途中でロックの獲得が試みられている、
// その獲得が「既に起動しています」で断られていること。
func TestAbandon_取れたロックを実行の最後まで握る(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	var mu sync.Mutex
	attempted, blocked := false, false
	fx.Herdr.SetPaneListScript(func(_ int) []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		if !attempted {
			attempted = true
			held, err := lock.Acquire(fx.LockPath)
			if err != nil {
				blocked = errors.Is(err, lock.ErrAlreadyRunning)
			} else {
				// **取れてしまった。**取れたものは握り続けずにすぐ返す。
				_ = held.Release()
			}
		}
		return nil
	})

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitOK)
	assertWorktreeGone(t, fx, prepared.Path)

	mu.Lock()
	defer mu.Unlock()
	if !attempted {
		t.Fatal("実行の途中でロックの獲得を1度も試みていない（検査が空振りしている）")
	}
	if !blocked {
		t.Fatal("abandon の実行中にロックを取れてしまった（判定の直後に手放している）")
	}
}

// {"RUCM-PATH": "P473"}
//
// 目的: 継続監視が動いていないと判定したときでも、**その worktree の pane が
// 生きていれば何も消さずに止まる**ことを確認する（設計 3-4 の段4 の前）。
// **ロックファイルの場所は環境変数で決まる。**launchd から起動した継続監視と
// 端末から叩いた abandon で食い違えば、生きた pane ごと worktree を消す。
// **herdr の socket は設定で決まるので、ロックより信用できる。**
// 与える情報: 誰も掴んでいないロックファイル（＝動いていないと判定される）と、
// その worktree を作業ディレクトリに持つ pane を返し続けるテスト用herdr mock。
// 成功条件: 終了コードが 1、worktree が残っている、herdr へ worktree.remove を
// 送っていない、残っている pane の ID とロックファイルのパスが出ていること。
func TestAbandon_動いていなくてもpaneが生きていれば消さない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	fx.Herdr.SetPaneListScript(func(_ int) []map[string]any { return panesAt(prepared.Path) })

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonNotRunning))
	assertContains(t, fx, i18n.T(i18n.KeyAbandonErrPaneAlive, "w1:p1", fx.LockPath))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
}

// {"RUCM-PATH": "P466"}
//
// 目的: **herdr の workspace が開いたままで `.git` が壊れている**worktree を、
// `--force` で片付け切れることを確認する（設計 3-4 の段4。issue #23）。
//
// **これが issue #23 の再報告そのものである。**continuo が worktree のために開いた
// herdr workspace には、その worktree を作業ディレクトリに持つ pane が必ず1枚ある
// （`worktree.open` が root pane を作る。実測: 2026-08-25）。
// **つまり workspace が開いているかぎり pane の検査は必ず引っかかり、
// `--force` を付けても何ひとつ消せなかった。****abandon が消すはずの workspace が
// abandon を止めていた。**
//
// 与える情報: `.git` を壊した issue 188 の worktree、その worktree を作業ディレクトリに
// 持つ pane を返し続けるテスト用herdr mock、`--force`。
// 成功条件: 終了コードが 0、worktree のディレクトリが消えている、branch が消えている、
// herdr の workspace が1つも残っていない、**pane ごと消すことを言う1行が出ている**こと。
func TestAbandon_herdrのworkspaceがあってgitが壊れていてもforceで消し切る(t *testing.T) {
	for _, how := range []gitFileBreakage{gitFileEmpty, gitFileGarbage, gitFileMissing} {
		t.Run(string(how), func(t *testing.T) {
			fx := newFixture(t)
			prepared := fx.Prepare(t, 188)
			fx.BreakGitFile(t, prepared, how)
			// **workspace が開いている状態を pane で表す。**herdr は workspace を開くと
			// root pane を1枚作り、その cwd は worktree である。
			fx.Herdr.SetPaneListScript(func(_ int) []map[string]any { return panesAt(prepared.Path) })

			code := fx.Run(t, 188, func(opts *abandon.Options) { opts.Force = true })

			assertExit(t, fx, code, abandon.ExitOK)
			assertContains(t, fx, i18n.T(i18n.KeyAbandonPaneAliveForced, "w1:p1"))
			assertWorktreeGone(t, fx, prepared.Path)
			if branchExists(t, fx, prepared.Branch.String()) {
				t.Fatalf("worktree を消したのに branch %s が残っている", prepared.Branch.String())
			}
			if ids := fx.Herdr.OpenWorkspaceIDs(); len(ids) != 0 {
				t.Fatalf("herdr の workspace が閉じていない: %v\n出力:\n%s", ids, fx.Output())
			}
		})
	}
}

// {"RUCM-PATH": "P473"}
//
// 目的: pane が生きていて `--force` が無いときの文言に、**越え方が書いてある**ことを
// 確認する（設計 3-4 の段4 の前。issue #23）。
// **止まったことだけを伝えて越え方を伝えないのは、詰まらせるのと同じである。**
// 与える情報: 誰も掴んでいないロックファイルと、その worktree を作業ディレクトリに持つ pane。
// 成功条件: 終了コードが 1、`--force` という語が同じ行に入っていること。
func TestAbandon_paneが生きて止まるときはforceの越え方を言う(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	fx.Herdr.SetPaneListScript(func(_ int) []map[string]any { return panesAt(prepared.Path) })

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitStopped)
	line := i18n.T(i18n.KeyAbandonErrPaneAlive, "w1:p1", fx.LockPath)
	if !strings.Contains(line, "--force") {
		t.Fatalf("pane が生きて止まる文言に越え方（--force）が書かれていない: %s", line)
	}
	assertContains(t, fx, line)
}

// {"RUCM-PATH": "P473"}
//
// 目的: pane の作業ディレクトリが worktree の**下の階層**であっても、その worktree の
// pane として拾うことを確認する（設計 3-4 の段1 と段4 の前）。
// **完全一致だけで照合すると、Claude Code が下の階層へ降りた瞬間に「pane はもう無い」と
// 判定して、生きている pane ごと worktree を消す。**継続監視の hook の判定も
// 「一致、または内側」である。
// 与える情報: 誰も掴んでいないロックファイルと、worktree の下の階層を作業ディレクトリに
// 持つ pane を返すテスト用herdr mock。
// 成功条件: 終了コードが 1、worktree が残っている、herdr へ worktree.remove を
// 送っていない、残っている pane の ID が出ていること。
func TestAbandon_paneの作業ディレクトリがworktreeの内側でも拾う(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	inner := filepath.Join(prepared.Path, "docs", "spec")
	fx.Herdr.SetPaneListScript(func(_ int) []map[string]any { return panesAt(inner) })

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonErrPaneAlive, "w1:p1", fx.LockPath))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
}

// {"RUCM-PATH": "P617"}
//
// 目的: 身元ファイルの issue_url が**置き場所のパスと食い違う** worktree を、
// 候補にしないことを確認する（設計 3-4 の段2）。
// **身元ファイルは worktree の直下にあり、そこでエージェントが
// `--permission-mode dontAsk` で動く。**検算しなければ、worktree A のエージェントが
// 自分の issue_url を issue B に書き換えるだけで、**人間が B を取り消したとき A が消える。**
// 与える情報: `octocat/another-repo` の下に用意した worktree に、
// `octocat/hello-world#188` の issue_url を書いた身元ファイル。
// 成功条件: 終了コードが 0、その worktree が残っている、herdr へ worktree.remove を
// 送っていない、食い違いの1行と「worktree はありません」が出ていること。
func TestAbandon_issueURLが置き場所と食い違えば候補にしない(t *testing.T) {
	fx := newFixture(t)
	// **パスは another-repo、身元ファイルは hello-world#188 を指している。**
	prepared := fx.PrepareIn(t, "octocat", "another-repo", 188, issueURL(188))

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitOK)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonOwnerRepoMismatch,
		prepared.Path, "octocat", "another-repo", issueURL(188)))
	assertContains(t, fx, i18n.T(i18n.KeyAbandonNotFound, issueURL(188)))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
}

// {"RUCM-PATH": "P462"}
//
// 目的: `--to` の値がボードの Status の選択肢に無いとき、**消す前に**止まることを
// 確認する（設計 3-4 の段2 の直後）。
// **確かめるのが段5 だと、綴り違いのときに worktree と branch を失ったうえに
// Status も動かない。**
// 与える情報: issue 188 の worktree と、選択肢に無い `--to Dnoe`。
// 成功条件: 終了コードが 1、worktree が残っている、branch も残っている、
// herdr へ worktree.remove を送っていない、ボードへの書き込みが0件であること。
func TestAbandon_toがボードの選択肢に無ければ消す前に止まる(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.ToState = "Dnoe" })

	assertExit(t, fx, code, abandon.ExitStopped)
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
	if !branchExists(t, fx, prepared.Branch.String()) {
		t.Fatalf("branch %s が消えている", prepared.Branch.String())
	}
	if len(fx.Tracker.Updates()) != 0 {
		t.Fatalf("止まったのにボードへ書いている: %v", fx.Tracker.Updates())
	}
}

// {"RUCM-PATH": "P308"}
//
// 目的: `--park` に作業中の状態（tracker.active_states の値）を渡したとき、
// **ボードへ1文字も書かずに**止まることを確認する（設計 3-4 の段2 の直後）。
// **そこへ動かしても継続監視は手を離さず、pane も閉じない。**pane の一覧を引いた
// 瞬間だけ空だった場合に、手を離していない issue の worktree を消してしまう。
// 与える情報: テストが先に掴んだロックファイル（＝継続監視が動いている）、
// issue 188 の worktree、`--park In Progress`（tracker.active_states の値）。
// 成功条件: 終了コードが 1、ボードへの書き込みが0件、worktree が残っている、
// herdr へ worktree.remove を送っていないこと。
func TestAbandon_parkが作業中の状態なら書く前に止まる(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	held, err := lock.Acquire(fx.LockPath)
	if err != nil {
		t.Fatalf("テストがロックを掴めません: %v", err)
	}
	defer func() { _ = held.Release() }()

	active := fx.Config.Tracker.ActiveStates[len(fx.Config.Tracker.ActiveStates)-1]
	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.ParkState = active })

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonErrParkActive, active))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
	if len(fx.Tracker.Updates()) != 0 {
		t.Fatalf("止まったのにボードへ書いている: %v", fx.Tracker.Updates())
	}
}

// {"RUCM-PATH": "P616"}
//
// 目的: 片付ける worktree が無いとき、`--to` の指定を黙って捨てないことを確認する
// （設計 3-4 の段2）。
// **黙って終わると、指定した人間は「動いた」と受け取るが、ボードには1文字も書かれていない。**
// **代わりに Status だけを動かすこともしない。**worktree が無い理由は URL の
// 打ち間違いでもあり、そのときは無関係な issue の Status を動かすことになる。
// 与える情報: issue 188 の worktree だけがある置き場所と、issue 999 の URL と `--to Ice Box`。
// 成功条件: 終了コードが 0、「動かしていません」の1行が出ている、
// ボードのアダプタを1度も作っていないこと。
func TestAbandon_worktreeが無ければtoを捨てたことを言う(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	code := fx.Run(t, 999, func(opts *abandon.Options) { opts.ToState = "Ice Box" })

	assertExit(t, fx, code, abandon.ExitOK)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonToSkipped, "Ice Box"))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
	if fx.TrackerBuilds() != 0 {
		t.Fatalf("消すものが無いのにボードのアダプタを %d 回作っている", fx.TrackerBuilds())
	}
}

// {"RUCM-PATH": "P050"}
//
// 目的: 手を離させる書き込みを済ませたあとに何も消さずに止まったとき、Status が
// その値のまま残ることを人間へ言うことを確認する（設計 3-4 の段1）。
// **「何も消していません」だけでは、ボードも元のままだと読まれる。**
// 与える情報: テストが先に掴んだロックファイルと、いつまでも閉じない pane。
// 成功条件: 終了コードが 1、Status が park の値のままであることを伝える1行が出ていること。
//
// **continuo は元へ戻さない。**戻す先は tracker.active_states の値なので、戻した瞬間に
// 動いている継続監視がその issue を拾い直しうる。戻すかどうかは人間が決める。
func TestAbandon_park後に止まったらStatusが残ることを言う(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	held, err := lock.Acquire(fx.LockPath)
	if err != nil {
		t.Fatalf("テストがロックを掴めません: %v", err)
	}
	defer func() { _ = held.Release() }()

	fx.Herdr.SetPaneListScript(func(_ int) []map[string]any { return panesAt(prepared.Path) })

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.ParkState = "Ice Box" })

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonParkLeftBehind, "Ice Box"))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)
}

// {"RUCM-PATH": "P039"}
//
// 目的: 手を離させたあとの計画表示に、park の**あと**の Status が出ることを確認する。
// **ボードは1回しか読まない**ので、書いた値を持ち回りへ反映しないと、これから消す
// worktree の issue が「まだ作業中」に見える。
// 与える情報: テストが先に掴んだロックファイルと、3回目の問い合わせで消える pane。
// 成功条件: 終了コードが 0、計画表示の Status が park の値（Ice Box）であり、
// park の前の値（In Progress）で出ていないこと。
func TestAbandon_計画表示のStatusはpark後の値になる(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)

	held, err := lock.Acquire(fx.LockPath)
	if err != nil {
		t.Fatalf("テストがロックを掴めません: %v", err)
	}
	defer func() { _ = held.Release() }()

	fx.Herdr.SetPaneListScript(func(call int) []map[string]any {
		if call <= 2 {
			return panesAt(prepared.Path)
		}
		return nil
	})

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.ParkState = "Ice Box" })

	assertExit(t, fx, code, abandon.ExitOK)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonPlanStatus, "Ice Box"))
	if strings.Contains(fx.Output(), i18n.T(i18n.KeyAbandonPlanStatus, "In Progress")) {
		t.Fatalf("計画表示に park の前の Status が出ている:\n%s", fx.Output())
	}
	assertWorktreeGone(t, fx, prepared.Path)
}
