package daemon

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/tracker"
)

// runStartupChecks は起動時の検査を全部通す（設計 3-6）。
//
// **1つでも失敗したら起動を止める。**無言で止まる経路が多いので、ここで全部潰す。
//
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
// ctx: 呼び出しに適用するコンテキスト。
// cfg: 検証済みの設定。
// d: 組み立て済みの依存。
// logger: ログの出力先。
// 戻り値: いずれかの検査に落ちた場合のエラー。
func runStartupChecks(ctx context.Context, cfg config.Config, d *deps, logger *slog.Logger) error {
	if err := tracker.CheckGHAvailable(); err != nil {
		return err
	}
	if err := tracker.CheckGHProjectScope(ctx, nil); err != nil {
		return err
	}
	logger.Info("gh の認証と scope を確かめました", "scope", "project")

	ping, err := d.Herdr.CheckProtocol(ctx, cfg.Herdr.Protocol)
	if err != nil {
		return fmt.Errorf("herdr の socket に到達できないか protocol が想定外です: %w", err)
	}
	logger.Info("herdr の socket に到達しました", "protocol", ping.Protocol)

	if err := d.Tracker.Bootstrap(ctx, cfg.Tracker); err != nil {
		return fmt.Errorf("ボードの Status の選択肢名が設定と一致しません（対象0件が無言で続くのを防ぎます）: %w", err)
	}
	return nil
}
