// {"RUCM-CFG-SHA256": "7bfa29d39811b2e62cf8c0c5737d12af2cad7b39ac1a0d5c2792296c7159c2ab", "SOURCE": "docs/spec/usecases/particular_case/着手を取り消す.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**「着手を取り消す」の48本のパスのうち、
// **判断が分かれるところ**を通る20本に、それぞれ1本のテストがある。
// 残りは分岐の組み合わせ違い（例: 継続監視が動いている状態での `--dry-run`）であり、
// 組み合わせの片側を通しているテストで振る舞いが決まる。
package abandon_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/abandon"
	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/lock"
)

// {"RUCM-PATH": "P043"}
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

// {"RUCM-PATH": "P044"}
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

// {"RUCM-PATH": "P041"}
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

// {"RUCM-PATH": "P040"}
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

// {"RUCM-PATH": "P034"}
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

// {"RUCM-PATH": "P034"}
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

// {"RUCM-PATH": "P031"}
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

// {"RUCM-PATH": "P004"}
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

// {"RUCM-PATH": "P004"}
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

// {"RUCM-PATH": "P013"}
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

// {"RUCM-PATH": "P048"}
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

// {"RUCM-PATH": "P047"}
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

// {"RUCM-PATH": "P046"}
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

// {"RUCM-PATH": "P030"}
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

// {"RUCM-PATH": "P015"}
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

// {"RUCM-PATH": "P019"}
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

// {"RUCM-PATH": "P014"}
//
// 目的: pane が閉じるのを待っているあいだに実行を中断されたら、**何も消さずに**
// 終了コード 1 で止まることを確認する（設計 3-4 の段1）。
// **中断は「待つのをやめてよい」であって「消してよい」ではない。**
// 与える情報: テストが先に掴んだロックファイル、いつまでも同じ pane を返し続ける
// テスト用herdr mock、待機を打ち切って偽を返す Sleep（＝SIGINT / SIGTERM を受けた状態）。
// 成功条件: 終了コードが 1、worktree が残っている、herdr へ worktree.remove を
// 送っていない、残っている pane と待った上限が出ている、
// 手を離させた Status を元へ戻していないこと。
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
	assertContains(t, fx, i18n.T(i18n.KeyAbandonErrPaneRemains, 3*time.Second, "w1:p1"))
	assertWorktreeExists(t, fx, prepared.Path)
	assertNoRemoval(t, fx)

	updates := fx.Tracker.Updates()
	if len(updates) != 1 || updates[0].State != fx.Config.Tracker.FailureState {
		t.Fatalf("手を離させた Status を元へ戻している（書き込み: %v）", updates)
	}
}

// {"RUCM-PATH": "P039"}
//
// 目的: 片付けそのものに失敗したとき、**Status を動かさずに**終了コード 1 で
// 止まることを確認する（設計 3-4 の段4 と段5）。
// **段5 は段4 が済んだときだけ走る。**worktree が残っているのに「片付けたあとの
// Status」へ動かすと、ボードの上では終わったことになって人間の目から消える。
// 与える情報: issue 188 の worktree、`worktree.remove` にエラーを返す
// テスト用herdr mock、`--to Ice Box`。
// 成功条件: 終了コードが 1、worktree が残っている、ボードへの書き込みが0件、
// 片付けに失敗した理由が出ていること。
func TestAbandon_片付けに失敗したらStatusを動かさない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)
	fx.Herdr.SetWorktreeRemoveError("internal_error", "worktree を消せません")

	code := fx.Run(t, 188, func(opts *abandon.Options) { opts.ToState = "Ice Box" })

	assertExit(t, fx, code, abandon.ExitStopped)
	assertContains(t, fx, "worktree を消せません")
	assertWorktreeExists(t, fx, prepared.Path)
	if len(fx.Tracker.Updates()) != 0 {
		t.Fatalf("片付けに失敗したのに Status を動かしている: %v", fx.Tracker.Updates())
	}
}

// {"RUCM-PATH": "P038"}
//
// 目的: branch を消さなかったとき、**「消しました」と言わない**ことを確認する
// （設計 3-4 の段4）。
// **身元ファイルは worktree の中にあり、エージェントが書き換えられる。**現物と
// 食い違う branch 名は消さないのが正しく、そのとき消したと報告してはならない。
// 与える情報: issue 188 の worktree と、checkout している branch と違う名前を書いた
// 身元ファイル。
// 成功条件: 終了コードが 0、worktree が消えている、**現物の branch が残っている**、
// branch が残ったことを伝える1行が出ていること。
func TestAbandon_branchを消さなかったら消したと言わない(t *testing.T) {
	fx := newFixture(t)
	prepared := fx.Prepare(t, 188)
	// **接頭辞は continuo のままにする。**接頭辞で弾かれたのではなく、
	// 「現物と一致しないから消さなかった」経路を通すためである。
	stale := prepared.Branch.String() + "-old"
	fx.SetIdentityBranch(t, prepared, stale)

	code := fx.Run(t, 188, nil)

	assertExit(t, fx, code, abandon.ExitOK)
	assertContains(t, fx, i18n.T(i18n.KeyAbandonRemovedBranchKept, prepared.Path, stale))
	assertWorktreeGone(t, fx, prepared.Path)
	if !branchExists(t, fx, prepared.Branch.String()) {
		t.Fatalf("身元ファイルと食い違う branch %s を消している", prepared.Branch.String())
	}
}

// {"RUCM-PATH": "P033"}
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

// {"RUCM-PATH": "P032"}
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

// {"RUCM-PATH": "P001"}
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
