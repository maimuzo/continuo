package githubapp

import (
	"context"
	"log/slog"
	"time"
)

// AcquireToken は「ロックを取る → 資格情報を読む → 更新用のトークンを回す → 書き戻す →
// ロックを放す」を1回通し、アクセストークンを返す（3-82d の 1-3 の図）。
//
// **これが、continuo 本体・`continuo github-app token`・起動時の検査が共通で使う唯一の経路である。**
// 別の経路を作ると、ロックを取らない回転が混ざり、片方の資格情報が死ぬ。
//
// **返ったトークンは、次に誰かが回すまでしか生きていない。**メモリで使い回さず、
// 使ったらすぐ捨てること。使う段はロックの外で行う（投稿1件のあいだ他のプロセスを待たせない）。
//
// **権限が 0600 でなくても止めない。**WARN を1行出して読む（3-82b の表）。
// 止めると、権限を直す案内が出る場所（`continuo doctor`）へ到達する前に落ちる。
//
// ctx: 呼び出しに適用するコンテキスト。
// store: 資格情報の置き場所。
// client: GitHub との往復。
// lockTimeout: ロックを待つ上限。0 以下なら DefaultLockTimeout。
// now: いまの時刻を返す関数。nil なら time.Now。
// logger: 警告の出力先。nil なら slog.Default()。**トークンは1文字もログに出さない。**
// 戻り値: アクセストークンと、取れなかった場合のエラー（資格情報が無い場合は ErrNotFound を包む）。
func AcquireToken(
	ctx context.Context,
	store Store,
	client Client,
	lockTimeout time.Duration,
	now func() time.Time,
	logger *slog.Logger,
) (string, error) {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	l, err := store.Lock(lockTimeout)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := l.Release(); err != nil {
			logger.Warn("GitHub App の資格情報のロックを放せません", "lock_file", store.LockPath(), "error", err)
		}
	}()

	creds, perm, err := store.Read()
	if err != nil {
		return "", err
	}
	if perm != CredentialsPerm {
		logger.Warn("GitHub App の資格情報の権限が 0600 ではありません（そのまま読みます。chmod 600 で直してください）",
			"path", store.Path(), "perm", perm.String())
	}
	token, updated, err := client.Rotate(ctx, creds, now())
	if err != nil {
		return "", err
	}
	// **書き戻しは回転の直後に行う。**ここで落ちると、古い更新用のトークンは既に無効なので、
	// 認可のやり直しになる（3-82b）。減らす手段は無い（アクセストークンをファイルへ書く案は
	// 人間が退けている）。
	if err := store.Write(updated); err != nil {
		return "", err
	}
	return token, nil
}

// TokenSource は AcquireToken を `func(ctx) (string, error)` の形に包む。
//
// **`tracker.NewAdapter` へ渡す関数と、`continuo github-app token` が呼ぶ関数を、同じ1つにする。**
//
// store / client / lockTimeout / now / logger: AcquireToken と同じ。
// 戻り値: 呼ぶたびに更新用のトークンを1回転させてアクセストークンを返す関数。
func TokenSource(
	store Store,
	client Client,
	lockTimeout time.Duration,
	now func() time.Time,
	logger *slog.Logger,
) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		return AcquireToken(ctx, store, client, lockTimeout, now, logger)
	}
}
