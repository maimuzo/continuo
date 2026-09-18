package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/fsprobe"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/tracker"
)

// runStartupChecks は起動時の検査を全部通す（設計 3-6）。
//
// **1つでも失敗したら起動を止める。**無言で止まる経路が多いので、ここで全部潰す。
//
//	書ける場所があるか              … **ホームが read-only なら着手のたびに落ちる**（issue #11）
//	gh が使えるか                  … エージェントが `gh issue comment` でコメントを書く（5-3）
//	gh auth status の scope        … `project` が無いとカンバンを読めない
//	herdr の socket と protocol     … 通信できない
//	Status の選択肢名が設定と一致するか … **合わないと GraphQL がエラーを出さずに 0 件を返し、
//	                                 キューが永久に止まる**
//	GitHub App のトークンが取れるか   … `github_app_attribution` が真のときだけ。**取れなければ
//	                                 起動しない**（3-82c）。認可した人が gh の持ち主と違っても止める（3-82f）
//
// **設定ファイルの未知キーと不正値は `config.Load` が既に見ている。**
//
// **ここで落ちても pane を閉じてはならない**（呼び出し側の責任。設計 3-4）。
// この関数は pane を1つも触らない。
//
// **リポジトリの信頼登録はここでは検査しない**（設計 3-6）。対象リポジトリの集合は
// カンバンを読むまで確定しないので、dispatch の直前に issue ごとに検査する。
//
// **外向きの呼び出しには必ず期限を与える。**`gh` の起動・herdr の socket・GitHub の
// GraphQL はどれも応答が返らないことがあり、期限が無いと**起動が無言で止まる**
// （復元にも巡回にも進まない）。
//
// ctx: 呼び出しに適用するコンテキスト。
// cfg: 検証済みの設定。
// d: 組み立て済みの依存。
// ga: GitHub App の資格情報の置き場所と `gh api user` の口（3-82c / 3-82f。最後の検査が使う）。
// timeout: この関数全体の上限。0 以下なら DefaultStartupCheckTimeout を使う。
// logger: ログの出力先。
// 戻り値: いずれかの検査に落ちた場合のエラー。
func runStartupChecks(
	ctx context.Context,
	cfg config.Config,
	d *deps,
	ga githubAppWiring,
	timeout time.Duration,
	logger *slog.Logger,
) error {
	if timeout <= 0 {
		// **GitHub App の検査を回すときは、直列の待ちを足したぶんを上乗せする**（3-82c）。
		//
		// `DefaultStartupCheckTimeout`（60秒）は6本の検査で分け合う予算である。
		// **最後に置く GitHub App の検査は、資格情報のロックの待ち（既定60秒）と
		// GitHub の往復（既定30秒）を直列で持つので、分け合った残りでは足りない。**
		// `continuo github-app token` が同じ足し算をしている
		// （`internal/cli` の `githubAppTokenTimeout`）。
		//
		// **足さないと、資格情報が1バイトも壊れていないのに continuo が起動を拒む。**
		// そのとき出る文面（回転を断られたときの2通り目）は原因を3つしか挙げていないので、
		// **人間は健全な資格情報を消して GitHub App を作り直す段へ進む。**
		//
		// **呼び出し側が明示した上限には足さない。**あれは期限を短く与えるための口であり
		// （`Options.StartupCheckTimeout`）、足すと短く与えられなくなる。
		timeout = StartupCheckBudget(cfg.Tracker.Comments.GitHubAppAttribution)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// **書けなければならない場所に、実際に書いて確かめる**（issue #11）。
	//
	// **doctor と同じ関数（fsprobe）を呼び、落ち方だけを変える。**doctor は記号で並べ、
	// 起動はここで止める。**外へ出る検査より先に置く。**ホームが read-only なら、
	// gh も herdr もカンバンも全部通ったうえで、着手のたびに落ち続けることになる。
	if err := fsprobe.CheckWritablePlaces("", cfg.Workspace.Root); err != nil {
		return i18n.Errorf(i18n.KeyDaemonRunStartupChecksNotWritable, err)
	}
	logger.Info("書けなければならない場所に書けることを確かめました", "workspace_root", cfg.Workspace.Root)

	if err := tracker.CheckGHAvailable(); err != nil {
		return err
	}
	if err := tracker.CheckGHProjectScope(ctx, nil); err != nil {
		return err
	}
	logger.Info("gh の認証と scope を確かめました", "scope", "project")

	ping, err := d.Herdr.CheckProtocol(ctx, cfg.Herdr.Protocol)
	if err != nil {
		return i18n.Errorf(i18n.KeyDaemonRunStartupChecksHerdrUnreachable, err)
	}
	logger.Info("herdr の socket に到達しました", "protocol", ping.Protocol)

	if err := d.Tracker.Bootstrap(ctx, cfg.Tracker); err != nil {
		return i18n.Errorf(i18n.KeyDaemonRunStartupChecksStatusOptionMismatch, err)
	}

	// **GitHub App のトークンが取れるか**（`github_app_attribution` が真のときだけ。3-82c）。
	// **最後に置く。**取ると更新用のトークンが1回転し、書き戻しの直前で落ちる窓が開くので、
	// 他の検査で落ちる起動では1回も回さない。文面はそのまま返す（Run が `ErrStartup` で包む）。
	return checkGitHubAppStartup(ctx, cfg, d, ga, time.Now(), logger)
}
