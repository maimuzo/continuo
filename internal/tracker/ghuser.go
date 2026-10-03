package tracker

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	"github.com/maimuzo/continuo/internal/i18n"
)

// GHLoginFunc は「continuo が使う gh の持ち主」を取る関数の型である（設計 3-65）。
//
// 本番は GHAPIUserLoginForHost が返す関数を使う。テストではコマンドを実際に実行せずに済むよう、
// 別の関数を差し替えて渡す（GHAuthTokenFunc / GHAuthStatusFunc と同じ考え方）。
//
// ctx: 実行に適用するコンテキスト。
// 戻り値: gh のログイン名（`octocat` のような文字列）と、取れなかった場合のエラー。
type GHLoginFunc func(ctx context.Context) (string, error)

// GHAPIUserLoginForHost は、接続先ホストに対して `gh api --hostname <ホスト> user --jq .login` を
// 実行し、continuo が使う gh の持ち主のログイン名を返す関数を返す（設計 3-65 / 3-86）。
//
// **ホストを必ず渡す。**渡さないと `gh` は自分の既定ホストの持ち主を返すので、
// GraphQL の `viewer`（接続先ホストの持ち主）と突き合わせたときに食い違う。
//
// **エージェントが `gh issue comment` で書くコメントも、continuo 自身が書くコメントも、
// この持ち主の名前で投稿される。**印（`tracker.comments.marker` /
// `tracker.comments.self_marker`）は誰でも本文の先頭に書ける文字列なので、
// **印と投稿者を併せて見ないと、外部の第三者のコメントをエージェントのものと読み違える。**
//
// **`gh auth status` の出力から名前を拾わない。**あちらは版によって書式が変わるうえ、
// 同じホストに複数のアカウントがあるときの読み分けが要る。**`gh api user` は
// いま有効なアカウントを1つだけ返す。**
//
// host: 接続先ホスト。空なら github.com として扱う。
// 戻り値: ログイン名を取る関数。その関数は、前後の空白を落としたログイン名を返す。
// コマンドの実行に失敗した場合、または出力が空文字だった場合はエラーを返す。
// **呼び出し側はこのエラーで起動を止めない**
// （設計 3-65。取れなければ印だけで判定する形に落ちる）。
// **その関数へは、期限を持たせた ctx を渡すこと。**
func GHAPIUserLoginForHost(host string) GHLoginFunc {
	h := NormalizedHost(host)
	return func(ctx context.Context) (string, error) {
		cmd := exec.CommandContext(ctx, ghBinary, "api", "--hostname", h, "user", "--jq", ".login")
		// **ctx の期限で殺したあとの後始末にも上限を置く**（GHAuthTokenForHost と同じ理由）。
		cmd.WaitDelay = ghWaitDelay
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			// **gh 自身の出力を必ず添える。**添えないと、未ログインなのか API へ届かないのかが
			// 1文字も画面に出ない。
			return "", i18n.Errorf(i18n.KeyTrackerGHAPIUserRunFailed, h, ghOutputForError(stderr.String()), err)
		}
		login := strings.TrimSpace(stdout.String())
		if login == "" {
			return "", i18n.Errorf(i18n.KeyTrackerGHAPIUserEmptyOutput, h)
		}
		return login, nil
	}
}
