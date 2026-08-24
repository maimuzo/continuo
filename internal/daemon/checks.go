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
//	gh auth status の scope        … `project` が無いとボードを読めない
//	herdr の socket と protocol     … 通信できない
//	Status の選択肢名が設定と一致するか … **合わないと GraphQL がエラーを出さずに 0 件を返し、
//	                                 キューが永久に止まる**
//
// **設定ファイルの未知キーと不正値は `config.Load` が既に見ている。**
//
// **ここで落ちても pane を閉じてはならない**（呼び出し側の責任。設計 3-4）。
// この関数は pane を1つも触らない。
//
// **リポジトリの信頼登録はここでは検査しない**（設計 3-6）。対象リポジトリの集合は
// ボードを読むまで確定しないので、dispatch の直前に issue ごとに検査する。
//
// **外向きの呼び出しには必ず期限を与える。**`gh` の起動・herdr の socket・GitHub の
// GraphQL はどれも応答が返らないことがあり、期限が無いと**起動が無言で止まる**
// （復元にも巡回にも進まない）。
//
// ctx: 呼び出しに適用するコンテキスト。
// cfg: 検証済みの設定。
// d: 組み立て済みの依存。
// timeout: この関数全体の上限。0 以下なら DefaultStartupCheckTimeout を使う。
// logger: ログの出力先。
// 戻り値: いずれかの検査に落ちた場合のエラー。
func runStartupChecks(
	ctx context.Context,
	cfg config.Config,
	d *deps,
	timeout time.Duration,
	logger *slog.Logger,
) error {
	if timeout <= 0 {
		timeout = DefaultStartupCheckTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// **書けなければならない場所に、実際に書いて確かめる**（issue #11）。
	//
	// **doctor と同じ関数（fsprobe）を呼び、落ち方だけを変える。**doctor は記号で並べ、
	// 起動はここで止める。**外へ出る検査より先に置く。**ホームが read-only なら、
	// gh も herdr もボードも全部通ったうえで、着手のたびに落ち続けることになる。
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
	return nil
}
