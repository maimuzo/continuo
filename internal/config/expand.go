package config

import (
	"os"
	"strings"

	"github.com/maimuzo/continuo/internal/i18n"
)

// expandKeys は 5-5 の表が「適用するキー」として挙げている4つのキーの一覧である。
// パスと接続先を表すものだけに展開を適用し、テンプレート文字列（branch_template）や
// Claude Code へ渡す環境変数（claude.env）、workspace_hooks の各コマンドには適用しない。
const (
	keyHerdrSocket      = "herdr.socket"
	keyWorkspaceRoot    = "workspace.root"
	keyClaudeHookListen = "claude.hook_bridge.listen"
	keyRuntimeLockFile  = "runtime.lock_file"
)

// expandConfig は Config のうち、5-5 が展開対象と定めた4つのキーだけへ
// 環境変数展開・チルダ展開を適用する。他のキーは一切変更しない。
//
// cfg: front matter をパースした直後の Config（展開前）。呼び出し後、対象キーの値が
// 展開済みの値へ書き換わる。
// 戻り値: 展開に失敗した場合のエラー。設定キーの名前と元の文字列を必ず含む（5-5）。
func expandConfig(cfg *Config) error {
	expanded, err := expandValue(cfg.Herdr.Socket, keyHerdrSocket)
	if err != nil {
		return err
	}
	cfg.Herdr.Socket = expanded

	expanded, err = expandValue(cfg.Workspace.Root, keyWorkspaceRoot)
	if err != nil {
		return err
	}
	cfg.Workspace.Root = expanded

	if cfg.Claude.HookBridge.Listen != nil {
		expanded, err = expandValue(*cfg.Claude.HookBridge.Listen, keyClaudeHookListen)
		if err != nil {
			return err
		}
		cfg.Claude.HookBridge.Listen = &expanded
	}

	if cfg.Runtime.LockFile != nil {
		expanded, err = expandValue(*cfg.Runtime.LockFile, keyRuntimeLockFile)
		if err != nil {
			return err
		}
		cfg.Runtime.LockFile = &expanded
	}

	return nil
}

// expandValue は1つの設定値へ、環境変数展開（expandDollar）とチルダ展開（expandTilde）を
// この順に適用する。
//
// raw: 展開前の生の文字列。
// key: エラーメッセージに含める設定キーの名前（例: "workspace.root"）。
// 戻り値: 展開後の文字列。どちらかの展開が失敗したらエラーを返す。
func expandValue(raw, key string) (string, error) {
	dollarExpanded, err := expandDollar(raw, key)
	if err != nil {
		return "", err
	}
	return expandTilde(dollarExpanded, key)
}

// expandDollar は `$NAME` / `${NAME}` / `$$`（リテラルのドル記号）の3つの書き方だけを
// 受け付けて展開する自前の小さなパーサである。
//
// os.ExpandEnv / os.Expand を使わない理由（設計 5-5 で実測済み）:
//   - 未定義の変数を黙って空文字に変換してしまい、設定の誤りに気づけない
//   - `price is $100` を `price is 00` に変えてしまう（`$1`, `$0`, `$0` を展開しようとするため）
//   - `${UNCLOSED`（閉じ忘れ）を検出できない
//
// raw: 展開前の文字列。
// key: エラーメッセージに含める設定キーの名前。
// 戻り値: 展開後の文字列。次のいずれかに該当する場合はエラーを返す。
//   - `$NAME` / `${NAME}` / `$$` のいずれの形式でもない `$` が現れた場合
//   - `${` が `}` で閉じられていない場合
//   - 環境変数名が空である場合（`${}`）
//   - 参照した環境変数が定義されていない、または空文字である場合
func expandDollar(raw, key string) (string, error) {
	var b strings.Builder
	i := 0
	for i < len(raw) {
		c := raw[i]
		if c != '$' {
			b.WriteByte(c)
			i++
			continue
		}

		if i+1 >= len(raw) {
			return "", i18n.Errorf(i18n.KeyConfigExpandTrailingDollar, key, raw)
		}

		next := raw[i+1]
		switch {
		case next == '$':
			// $$ はリテラルのドル記号1文字になる。
			b.WriteByte('$')
			i += 2

		case next == '{':
			closeIdx := strings.IndexByte(raw[i+2:], '}')
			if closeIdx < 0 {
				return "", i18n.Errorf(i18n.KeyConfigExpandUnclosedBrace, key, raw)
			}
			name := raw[i+2 : i+2+closeIdx]
			if name == "" {
				return "", i18n.Errorf(i18n.KeyConfigExpandEmptyEnvName, key, raw)
			}
			val, err := lookupEnv(name, key, raw)
			if err != nil {
				return "", err
			}
			b.WriteString(val)
			i = i + 2 + closeIdx + 1

		case isEnvNameStart(next):
			j := i + 1
			for j < len(raw) && isEnvNameChar(raw[j]) {
				j++
			}
			name := raw[i+1 : j]
			val, err := lookupEnv(name, key, raw)
			if err != nil {
				return "", err
			}
			b.WriteString(val)
			i = j

		default:
			return "", i18n.Errorf(i18n.KeyConfigExpandInvalidDollarForm, key, raw, i+1)
		}
	}
	return b.String(), nil
}

// isEnvNameStart は環境変数名の1文字目として許される文字かどうかを判定する。
// シェルの変数名の慣習（英字とアンダースコアで始まる）に合わせる。
func isEnvNameStart(c byte) bool {
	return c == '_' || ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z')
}

// isEnvNameChar は環境変数名の2文字目以降として許される文字かどうかを判定する。
func isEnvNameChar(c byte) bool {
	return isEnvNameStart(c) || ('0' <= c && c <= '9')
}

// lookupEnv は環境変数を読み、未定義または空文字ならエラーにする（5-5）。
// name: 環境変数名。
// key: エラーメッセージに含める設定キーの名前。
// original: エラーメッセージに含める、展開前の元の文字列。
func lookupEnv(name, key, original string) (string, error) {
	val, ok := os.LookupEnv(name)
	if !ok {
		return "", i18n.Errorf(i18n.KeyConfigExpandEnvUndefined, key, original, name)
	}
	if val == "" {
		return "", i18n.Errorf(i18n.KeyConfigExpandEnvEmpty, key, original, name)
	}
	return val, nil
}

// expandTilde は先頭の `~` または `~/` だけをホームディレクトリへ展開する。
// `~user` のような他ユーザーのホームを指す形式は、continuo が動くマシンの外の
// ユーザー情報に依存してしまうためサポートせず、エラーにする（5-5）。
//
// raw: 展開前の文字列（expandDollar を通した後の文字列を渡す想定）。
// key: エラーメッセージに含める設定キーの名前。
// 戻り値: 展開後の文字列。`~` で始まらない場合はそのまま返す。
func expandTilde(raw, key string) (string, error) {
	if raw == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", i18n.Errorf(i18n.KeyConfigExpandHomeDirFailed, key, raw, err)
		}
		return home, nil
	}

	if strings.HasPrefix(raw, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", i18n.Errorf(i18n.KeyConfigExpandHomeDirFailed, key, raw, err)
		}
		// パス結合は文字列連結で行う。filepath.Join は "~/" の直後にスラッシュが
		// 連続する場合などを正規化してしまい、意図が分かりにくくなるため使わない。
		return home + raw[1:], nil
	}

	if strings.HasPrefix(raw, "~") {
		return "", i18n.Errorf(i18n.KeyConfigExpandTildeUserUnsupported, key, raw)
	}

	return raw, nil
}
