package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ClaudeConfigFileName は Claude Code が信頼済みフォルダを記録しているファイルの名前である。
// ホームディレクトリの直下に置かれる。**読むだけで、絶対に書き換えない**（4-3）。
const ClaudeConfigFileName = ".claude.json"

// claudeConfigFile は `~/.claude.json` のうち、信頼の判定に使う部分だけを写した型である。
// **書き戻す用途には使わない**（書き戻すと、写していないキーが全部消える）。
type claudeConfigFile struct {
	// Projects のキーは作業ディレクトリの絶対パスである（実機で確認済み。3-6）。
	Projects map[string]claudeProjectEntry `json:"projects"`
}

// claudeProjectEntry は `~/.claude.json` の projects の値のうち、信頼の判定に使う項目である。
type claudeProjectEntry struct {
	// HasTrustDialogAccepted は、そのフォルダの信頼ダイアログを人間が承認済みかどうかである。
	HasTrustDialogAccepted bool `json:"hasTrustDialogAccepted"`
}

// CheckTrust は、リポジトリが Claude Code に信頼登録されているかを、理由つきで返す
// （3-6 の「信頼を引く鍵の作り方」の3段）。
//
//  1. ghq list -p -e <owner>/<repo> で clone のパスを引く（空なら「clone が無い」）
//  2. git -C <そのパス> rev-parse --path-format=absolute --show-toplevel で解決する
//  3. その出力を鍵にして ~/.claude.json の projects[<鍵>].hasTrustDialogAccepted を読む
//
// **worktree のパスで引いてはならない。**信頼はリポジトリ単位で記録されるので、
// worktree のパスでは必ず「未承認」になる（1-2 の実測）。
// **`~/.claude.json` は読むだけである。**
//
// owner: リポジトリの所有者名。
// repo: リポジトリ名。
// 戻り値の1つ目: 信頼登録されていれば true。
// 戻り値の2つ目: 人間に見せる理由。**「clone が無い」と「未信頼」をここで区別する**
// （doctor が使う。08）。信頼済みのときは鍵に使ったパスを書く。
// 戻り値の3つ目: ghq や git を実行できない・`~/.claude.json` を読めない・
// JSON として解析できない場合のエラー（判定できなかったことを表す）。
//
// **コンテキストを引数に取らない。**このシグネチャは 05_workspace.md が指定したもので、
// 内部では context.Background() を使う。呼ぶのは同じ機械のローカルなコマンド
// （ghq / git）と1つのファイルの読み取りだけである。
func (m *Manager) CheckTrust(owner, repo string) (bool, string, error) {
	ctx := context.Background()

	clonePath, err := m.ghqList(ctx, owner, repo)
	if err != nil {
		return false, "", err
	}
	if clonePath == "" {
		return false, fmt.Sprintf(
			"%s/%s の clone がありません（`ghq list -p -e %s/%s` の出力が空。continuo は勝手に clone しません）",
			owner, repo, owner, repo), nil
	}

	return CheckTrustForClonePath(clonePath, m.homeDir)
}

// CheckTrustForClonePath は clone の絶対パスを鍵にして、信頼登録を理由つきで判定する
// （3-6 の「信頼を引く鍵の作り方」の2段と3段）。
//
//  1. git -C <clonePath> rev-parse --path-format=absolute --show-toplevel で鍵を解決する
//  2. その出力を鍵にして ~/.claude.json の projects[<鍵>].hasTrustDialogAccepted を読む
//
// **clone のパスを引く1段（ghq）は含まない。**呼び出し側が既にパスを持っている場合に
// ghq を2回起動しないためである（doctor は clone の検査で先に引いている。3-32）。
// **`~/.claude.json` は読むだけである。**
//
// **worktree のパスを渡してはならない。**信頼はリポジトリ単位で記録されるので、
// worktree のパスでは必ず「未承認」になる（1-2 の実測）。
//
// clonePath: clone の絶対パス（`ghq list -p -e` が返した値）。
// homeDir: `~/.claude.json` を探すホームディレクトリ。
// 戻り値の1つ目: 信頼登録されていれば true。
// 戻り値の2つ目: 人間に見せる理由。信頼済みのときは鍵に使ったパスを書く。
// 戻り値の3つ目: git を実行できない・`~/.claude.json` を読めない・JSON として解析できない
// 場合のエラー（判定できなかったことを表す）。
func CheckTrustForClonePath(clonePath, homeDir string) (bool, string, error) {
	ctx := context.Background()

	key, err := gitToplevel(ctx, clonePath)
	if err != nil {
		return false, "", fmt.Errorf(
			"clone のパス %s を `git rev-parse --show-toplevel` で解決できません: %w", clonePath, err)
	}

	configPath := filepath.Join(homeDir, ClaudeConfigFileName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, fmt.Sprintf(
				"%s がありません（Claude Code をまだ一度も使っていない可能性がある）", configPath), nil
		}
		return false, "", fmt.Errorf("%s を読めません: %w", configPath, err)
	}
	var parsed claudeConfigFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return false, "", fmt.Errorf("%s を JSON として解析できません: %w", configPath, err)
	}

	entry, ok := parsed.Projects[key]
	if !ok {
		return false, fmt.Sprintf(
			"%s が Claude Code に登録されていません（%s の projects に鍵がない）", key, configPath), nil
	}
	if !entry.HasTrustDialogAccepted {
		return false, fmt.Sprintf(
			"%s の信頼ダイアログが承認されていません（%s の hasTrustDialogAccepted が false）", key, configPath), nil
	}
	return true, fmt.Sprintf("%s は信頼済みです", key), nil
}

// TrustFunc は tracker.RepoTrustFunc に合う薄い包みを返す（既存の呼び出し口に渡すため）。
//
// **戻り値の型は素の関数型である。**`tracker.RepoTrustFunc` は
// `func(owner, repo string) bool` を基底型に持つ名前付き型なので、この戻り値はそのまま
// 代入できる。**名前付き型で返すと internal/workspace が internal/tracker に依存し、
// このパッケージの境界（「トラッカーを知らない」）が崩れる。**
//
// **「clone が無い」と「未信頼」と「判定できなかった」を区別しない。**
// どれも false になる。理由が要る場合（doctor）は CheckTrust を直接使うこと。
// 判定できなかった場合は警告をログに出す（無言で false にしない）。
//
// 戻り値: owner と repo を受け取り、信頼済みなら true を返す関数。
func (m *Manager) TrustFunc() func(owner, repo string) bool {
	return func(owner, repo string) bool {
		trusted, reason, err := m.CheckTrust(owner, repo)
		if err != nil {
			m.logger.Warn("リポジトリの信頼を判定できませんでした",
				"owner", owner, "repo", repo, "error", err)
			return false
		}
		if !trusted {
			m.logger.Info("リポジトリが Claude Code に信頼登録されていません",
				"owner", owner, "repo", repo, "reason", reason)
		}
		return trusted
	}
}
