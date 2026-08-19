package tracker

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
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
			return out, fmt.Errorf("`gh auth status --hostname %s` を起動できません: %w", ghAuthHost, err)
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
		return fmt.Errorf("`%s` が PATH にありません（エージェントが issue へコメントを書けません）: %w", ghBinary, err)
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
// ctx: 実行に適用するコンテキスト。
// run: `gh auth status` を実行する関数。**nil なら RunGHAuthStatus を使う。**
// 戻り値: gh を起動できない場合、有効なアカウントが1つも無い場合、
// scope に `project` が無い場合のエラー。
func CheckGHProjectScope(ctx context.Context, run GHAuthStatusFunc) error {
	if run == nil {
		run = RunGHAuthStatus
	}
	out, err := run(ctx)
	if err != nil {
		return err
	}
	scopes, found := activeAccountScopes(out)
	if !found {
		return fmt.Errorf(
			"gh に %s の有効なアカウントがありません（`gh auth login -s %s` を実行してください）",
			ghAuthHost, requiredGHScope)
	}
	for _, s := range scopes {
		if s == requiredGHScope {
			return nil
		}
	}
	return fmt.Errorf(
		"gh の scope に %q がありません（あるのは %v。`read:project` では Status を書けません。"+
			"`gh auth refresh -h %s -s %s` を実行してください）",
		requiredGHScope, scopes, ghAuthHost, requiredGHScope)
}

// activeAccountScopes は `gh auth status` の出力から、有効なアカウントの scope を取り出す。
//
// **`Active account: true` の行を持つブロックだけを読む。**gh は同じホストに複数の
// アカウントを持てるので、最初に見つかった `Token scopes:` を読むと別のアカウントの
// scope を見てしまう。
//
// out: `gh auth status` の出力。
// 戻り値の1つ目: 有効なアカウントの scope（前後の空白と引用符を落としたもの）。
// 戻り値の2つ目: 有効なアカウントのブロックが見つかれば true。
func activeAccountScopes(out string) ([]string, bool) {
	type block struct {
		// active は `Active account:` の行が true だったかどうかである。
		active bool
		// hasActiveLine は `Active account:` の行がそのブロックに在ったかどうかである。
		// **「false と書いてあった」と「行そのものが無い」を区別するために持つ。**
		hasActiveLine bool
		// scopes は `Token scopes:` の行を分解したものである。
		scopes []string
	}
	var blocks []block
	cur := -1

	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// 記号（✓ / ✗ / -）を落としてから見る。
		trimmed := strings.TrimLeft(line, "-*✓✗x! \t")

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
			return b.scopes, true
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
		return blocks[0].scopes, true
	}
	return nil, false
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
