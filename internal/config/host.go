package config

import (
	"strings"

	"github.com/maimuzo/continuo/internal/i18n"
)

// DefaultHost は `tracker.provider.host` を書かなかったときの接続先ホストである（設計 3-86）。
const DefaultHost = "github.com"

// EnvGHHost は `gh` が「ホストを指定されていない呼び出しをどこへ送るか」を決める環境変数の名前である。
//
// continuo は、自分が起こす `gh` と、エージェントが叩く `gh` の両方へ、接続先ホストを
// この名前で渡す（設計 3-86）。
const EnvGHHost = "GH_HOST"

// NormalizeHost は接続先ホストの値を検査し、小文字に直して返す（設計 3-86）。
//
// **受け付けるのはホスト名だけである。**英字・数字・`.`・`-` だけを通す。
// `https://`・パス・ポート番号が付いた値は通さない。
// **ポート番号を通さないのは、worktree の置き場所のホスト（issue の URL から取る）が
// ポート番号を落とすためである。**通すと、設定の値と置き場所のホストが食い違う。
//
// **先頭か末尾が `-` か `.` の値も通さない。**この値は `gh` と `ghq` の引数へそのまま入る。
// `-` で始まる値は、フラグとして読まれる。
//
// raw: 設定か `--host` か環境変数 GH_HOST に書かれた値。前後の空白は落とす。
// 戻り値の1つ目: 小文字に直したホスト名。
// 戻り値の2つ目: 形が合わないときのエラー。
func NormalizeHost(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(raw))
	if host == "" {
		return "", i18n.Errorf(i18n.KeyConfigHostEmpty)
	}
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-':
		default:
			return "", i18n.Errorf(i18n.KeyConfigHostInvalidChar, raw)
		}
	}
	first, last := host[0], host[len(host)-1]
	if first == '-' || first == '.' || last == '-' || last == '.' {
		return "", i18n.Errorf(i18n.KeyConfigHostInvalidEdge, raw)
	}
	return host, nil
}

// validateHost は `tracker.provider.host` と `claude.env` の GH_HOST を検査する（設計 3-86）。
//
// **`tracker.provider.host` は小文字に直して書き戻す。**以後はこの値だけを使う。
//
// **`claude.env` に、接続先ホストと違う値の GH_HOST が書いてあれば誤りにする。**
// continuo は issue ごとの設定ファイルの `env` へ `GH_HOST=<接続先ホスト>` を書く。
// 黙ってどちらかを勝たせると、利用者が書いた値が効いていないことに気づけない。
//
// cfg: 検査する設定。
// 戻り値: 形が合わない・食い違っているときのエラー。
func validateHost(cfg *Config) error {
	host, err := NormalizeHost(cfg.Tracker.Provider.Host)
	if err != nil {
		return invalidValueError("tracker.provider.host", cfg.Tracker.Provider.Host, err.Error())
	}
	cfg.Tracker.Provider.Host = host
	if v, ok := cfg.Claude.Env[EnvGHHost]; ok && strings.ToLower(strings.TrimSpace(v)) != host {
		return i18n.Errorf(i18n.KeyConfigHostEnvMismatch, EnvGHHost, v, host)
	}
	return nil
}
