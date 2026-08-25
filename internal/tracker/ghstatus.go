package tracker

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
)

// ghBinary は実行する gh の名前である。PATH から解決する。
const ghBinary = "gh"

// ghAuthHost は認証を検査する対象のホストである（設計 3-6）。
//
// **`github.com` に固定する。設定から引かない。**このアダプタが対応するトラッカーは
// GitHub Projects v2 だけである。
const ghAuthHost = "github.com"

// requiredGHScope は起動時の検査で必須にする scope である（設計 3-6）。
//
// **`read:project` は不可である。**読めるだけでは Status を書けない。
const requiredGHScope = "project"

// ghWaitDelay は、ctx の期限で `gh` を殺したあと、その入出力の後始末を待つ上限である。
//
// **これが無いと、`gh` が孫プロセスへ標準出力を渡していた場合に Wait が返らない。**
// 殺したあとの後始末なので短くてよい（internal/ratelimit の keychainWaitDelay と同じ値）。
const ghWaitDelay = 2 * time.Second

// ghOutputMax はエラー文へ載せる `gh` の出力の長さ（文字数）である。
const ghOutputMax = 800

// GHAuthStatusFunc は `gh auth status` 相当の処理を行う関数の型である。
//
// 本番は RunGHAuthStatus を使う。テストではコマンドを実際に実行せずに済むよう、
// 別の関数を差し替えて渡す（GHAuthTokenFunc と同じ考え方）。
//
// ctx: 実行に適用するコンテキスト。
// 戻り値: `gh auth status` の出力（標準出力と標準エラーを連結したもの）と、
// gh を起動できなかった場合のエラー。**未ログインのときは gh が非 0 で終わるが、
// その場合も出力を返してエラーにはしない**（出力の中身で判定するため）。
type GHAuthStatusFunc func(ctx context.Context) (string, error)

// RunGHAuthStatus は実際に `gh auth status --hostname github.com` を実行する。
//
// **`--show-scopes` というフラグは存在しない**（gh 2.97.0 で確認）。既定の出力に scope が入る。
// **gh は情報を標準エラーへ出す版がある**ので、標準出力と標準エラーの両方を読む。
//
// ctx: 実行に適用するコンテキスト。
// 戻り値: 出力（標準出力 + 標準エラー）と、gh を起動できなかった場合のエラー。
// 終了コードが非 0 なだけならエラーにしない（未ログインの判定は出力で行う）。
func RunGHAuthStatus(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, ghBinary, "auth", "status", "--hostname", ghAuthHost)
	cmd.WaitDelay = ghWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String() + stderr.String()
	if err != nil {
		// **終了コードが非 0 なだけならエラーにしない。**未ログインでも gh は非 0 で終わるが、
		// その判定は出力の中身（Active account の有無）で行う。
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return out, i18n.Errorf(i18n.KeyTrackerGHAuthStatusStartFailed, ghAuthHost, err)
		}
	}
	return out, nil
}

// CheckGHAvailable は `gh` が使えるかを検査する（設計 3-6）。
//
// **エージェントが `gh issue comment` でコメントを書く**（設計 5-3）ので、
// これが無いと成果が issue に残らない。
//
// 戻り値: PATH に gh が無い場合のエラー。
func CheckGHAvailable() error {
	if _, err := exec.LookPath(ghBinary); err != nil {
		return i18n.Errorf(i18n.KeyTrackerGHAvailableNotInPath, ghBinary, err)
	}
	return nil
}

// CheckGHProjectScope は `gh auth status` の scope に `project` が含まれるかを検査する
// （設計 3-6 / 3-32 の「`gh auth status` の読み方」）。
//
// 読み方は次のとおりに1つへ決めてある。
//
//	対象のホスト     … github.com に固定する（設定から引かない）
//	どのブロックを読むか … `Active account: true` の行を持つブロックだけ
//	                    （gh は同じホストに複数のアカウントを持てる）
//	何を見るか       … そのブロックの `Token scopes:` の行。カンマで区切り、
//	                    各要素の前後の空白と引用符を落とす
//	合格の条件       … 落とした結果に `project` が1つの要素として在ること
//
// **落ちたときは gh 自身の出力を必ずエラー文へ添える。**添えないと、gh が書いた本当の理由
// （`The token in keyring is invalid.` など）と正しい直し方が1文字も画面に出ない。
//
// **「gh が非 0 で終わった」を「未ログイン」と同一視しない。**ログイン済みでも、
// 一時的に GitHub へ届かなければ gh は `Failed to log in to …` と書いて非 0 で終わる。
// そのときの直し方は `gh auth login` ではなく `gh auth refresh` であり、
// そもそもネットワークが戻れば何もしなくてよい。
//
// ctx: 実行に適用するコンテキスト。
// run: `gh auth status` を実行する関数。**nil なら RunGHAuthStatus を使う。**
// 戻り値: gh を起動できない場合、gh がトークンを検証できていない場合、
// 有効なアカウントが1つも無い場合、scope に `project` が無い場合のエラー。
func CheckGHProjectScope(ctx context.Context, run GHAuthStatusFunc) error {
	if run == nil {
		run = RunGHAuthStatus
	}
	out, err := run(ctx)
	if err != nil {
		return err
	}
	account := activeAccountScopes(out)
	if account.loginFailed {
		// gh は届いているが、そのトークンを検証できていない。**未ログインではない。**
		return i18n.Errorf(i18n.KeyTrackerGHScopeTokenUnverified,
			ghAuthHost, ghAuthHost, ghOutputForError(out))
	}
	if !account.found {
		return i18n.Errorf(i18n.KeyTrackerGHScopeNoActiveAccount,
			ghAuthHost, requiredGHScope, ghOutputForError(out))
	}
	for _, s := range account.scopes {
		if s == requiredGHScope {
			return nil
		}
	}
	return i18n.Errorf(
		i18n.KeyTrackerGHScopeMissingScope,
		requiredGHScope, account.scopes, ghAuthHost, requiredGHScope)
}

// ghOutputForError は `gh auth status` の出力を、エラー文へ載せられる形にする。
//
// **前後の空白を落としてから切り詰める。**gh の出力は末尾に空行を持つことがあり、
// そのままだとエラー文が改行で終わって読みにくい。切り詰めは graphql.go の truncate
// （文字で切る）に寄せる。
//
// out: `gh auth status` の出力。
// 戻り値: エラー文へ載せる文字列。出力が空なら「出力なし」と分かる文字列。
func ghOutputForError(out string) string {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return "(出力なし)"
	}
	return truncate([]byte(trimmed), ghOutputMax)
}

// ghAccount は `gh auth status` の出力から読み取った、有効なアカウントの状態である。
type ghAccount struct {
	// scopes は有効なアカウントの scope（前後の空白と引用符を落としたもの）である。
	scopes []string
	// found は有効なアカウントのブロックが見つかったかどうかである。
	found bool
	// loginFailed は、その有効なアカウントについて gh が
	// `Failed to log in to …` と書いたかどうかである。
	// **「gh は届いているがトークンを検証できていない」ことを表す。未ログインではない。**
	loginFailed bool
}

// activeAccountScopes は `gh auth status` の出力から、有効なアカウントの状態を取り出す。
//
// **`Active account: true` の行を持つブロックだけを読む。**gh は同じホストに複数の
// アカウントを持てるので、最初に見つかった `Token scopes:` を読むと別のアカウントの
// scope を見てしまう。
//
// **ブロックの始まりは `Logged in to ` だけではない。**gh はトークンを検証できなかった
// アカウントを `Failed to log in to <host> account <name> (<出所>)` と書き出す
// （実測: gh 2.97.0、ネットワークを塞いだ状態、終了コード 1）。
// **これをブロックの始まりとして扱わないと、その下の `Active account: true` が
// どのブロックにも属さず、出力全体が「未ログイン」に見える。**
//
// out: `gh auth status` の出力。
// 戻り値: 有効なアカウントの状態。
func activeAccountScopes(out string) ghAccount {
	type block struct {
		// active は `Active account:` の行が true だったかどうかである。
		active bool
		// hasActiveLine は `Active account:` の行がそのブロックに在ったかどうかである。
		// **「false と書いてあった」と「行そのものが無い」を区別するために持つ。**
		hasActiveLine bool
		// loginFailed は `Failed to log in to ` で始まったブロックかどうかである。
		loginFailed bool
		// scopes は `Token scopes:` の行を分解したものである。
		scopes []string
	}
	var blocks []block
	cur := -1

	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// 記号（✓ / ✗ / X / -）を落としてから見る。
		// **大文字の `X` も落とす。**gh は記号を出せない端末でこちらを使う。
		trimmed := strings.TrimLeft(line, "-*✓✗xX! \t")

		// **`Failed to log in to ` を先に見る。**`Logged in to ` はこの文字列の
		// 部分列ではないが、順序を固定しておくほうが読み違えにくい。
		if strings.Contains(trimmed, "Failed to log in to ") {
			blocks = append(blocks, block{loginFailed: true})
			cur = len(blocks) - 1
			continue
		}
		if strings.Contains(trimmed, "Logged in to ") {
			blocks = append(blocks, block{})
			cur = len(blocks) - 1
			continue
		}
		if cur < 0 {
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, "Active account:"); ok {
			blocks[cur].hasActiveLine = true
			blocks[cur].active = strings.EqualFold(strings.TrimSpace(rest), "true")
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, "Token scopes:"); ok {
			blocks[cur].scopes = parseScopes(rest)
		}
	}

	for _, b := range blocks {
		if b.active {
			return ghAccount{scopes: b.scopes, found: true, loginFailed: b.loginFailed}
		}
	}
	// **`Active account:` の行が1つも無い版の gh もありうる。**その場合に限り、
	// ブロックが1つだけなら、それを有効なアカウントとして扱う。
	//
	// **`Active account: false` と書いてあるブロックは、1つだけでも受理しない**
	// （設計 3-32 の「読むのは `Active account: true` の行を持つブロックだけ」
	// 「該当ブロックが1つも無ければ `✗`（未ログイン）」）。有効なアカウントが別のホストに
	// ある場合、`gh auth status --hostname github.com` は false のブロックを1つだけ出しうる。
	if len(blocks) == 1 && !blocks[0].hasActiveLine {
		return ghAccount{
			scopes:      blocks[0].scopes,
			found:       true,
			loginFailed: blocks[0].loginFailed,
		}
	}
	return ghAccount{}
}

// parseScopes は `Token scopes:` の行の値を scope の並びへ分解する。
//
// raw: `'gist', 'project', 'repo'` のような値。
// 戻り値: 前後の空白と引用符を落とした scope の並び。空の要素は捨てる。
func parseScopes(raw string) []string {
	var scopes []string
	for _, part := range strings.Split(raw, ",") {
		s := strings.TrimSpace(part)
		s = strings.Trim(s, "'\"")
		s = strings.TrimSpace(s)
		if s != "" {
			scopes = append(scopes, s)
		}
	}
	return scopes
}
