package abandon_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/abandon"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/lock"
)

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
