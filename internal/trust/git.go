package trust

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitOutputLimit は git の標準エラー出力をエラー文に載せる上限（バイト）である。
const gitStderrLimit = 8 * 1024

// RunGitToplevel は clone の絶対パスから、信頼を引く鍵を求める。
//
// **鍵は `git rev-parse --path-format=absolute --show-toplevel` の出力である**（3-6 の段2）。
// シンボリックリンクが解決された実体のパスが返る。
//
// **worktree のパスを渡してはならない。**信頼はリポジトリ単位で記録されるので、
// worktree のパスでは別の鍵になる（1-2 の実測）。
//
// **この鍵の作り方が巡回のループとずれていないことは、Apply が書き込みのあとに
// workspace.CheckTrustForClonePath で確かめる。**ずれていれば「書いたのに効かない」として報告する。
//
// ctx: 実行に適用するコンテキスト。
// clonePath: clone の絶対パス。
// 戻り値の1つ目: 信頼を引く鍵（作業ディレクトリの最上位の絶対パス）。
// 戻り値の2つ目: git を実行できない場合・非 0 で終わった場合のエラー。
func RunGitToplevel(ctx context.Context, clonePath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", clonePath,
		"rev-parse", "--path-format=absolute", "--show-toplevel")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > gitStderrLimit {
			msg = msg[:gitStderrLimit]
		}
		if msg == "" {
			return "", fmt.Errorf("`git -C %s rev-parse --show-toplevel` の実行に失敗しました: %w", clonePath, err)
		}
		return "", fmt.Errorf("`git -C %s rev-parse --show-toplevel` の実行に失敗しました: %w: %s", clonePath, err, msg)
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("`git -C %s rev-parse --show-toplevel` が空を返しました", clonePath)
	}
	return filepath.Clean(out), nil
}
