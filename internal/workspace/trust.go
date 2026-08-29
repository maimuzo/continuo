package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/maimuzo/continuo/internal/i18n"
)

// ClaudeConfigFileName は Claude Code が信頼済みフォルダを記録しているファイルの名前である。
// ホームディレクトリの直下に置かれる。**読むだけで、絶対に書き換えない**（4-3）。
const ClaudeConfigFileName = ".claude.json"

// trustCheckTimeout は信頼の判定が外部コマンド（ghq / git）を待つ上限である。
//
// **上限が無いと、ghq か git が固まったときに巡回のループごと止まる。**この判定は
// dispatch の直前に issue ごとに呼ばれる（3-6）ので、1件でも返らなければ無人運用が
// そこで終わる（3-11 の「人間の入力を待つ箇所を全部潰す」と同じ理由）。
const trustCheckTimeout = 10 * time.Second

// trustCacheTTL は信頼の判定の結果を覚えておく時間である。
//
// **判定1回につき ghq と git を1本ずつ起動し、`~/.claude.json` を読み直す。**
// ボードの項目ごとに呼ばれるので、覚えないと1巡回で数百回のプロセス起動になる。
// **人間が信頼を承認してから、最大でこの時間だけ「未信頼」のままになる**
// （巡回の間隔と同じ桁なので、次の巡回では効く）。
const trustCacheTTL = 30 * time.Second

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
// 呼ぶのは同じ機械のローカルなコマンド（ghq / git）と1つのファイルの読み取りだけである。
// **ただし上限は必ず置く**（trustCheckTimeout）。ローカルのコマンドでも、
// NFS 等で固まれば巡回全体が止まる。
func (m *Manager) CheckTrust(owner, repo string) (bool, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), trustCheckTimeout)
	defer cancel()

	clonePath, err := m.clonePath(ctx, owner, repo)
	if err != nil {
		return false, "", err
	}
	if clonePath == "" {
		return false, i18n.T(i18n.KeyWorkspaceCheckTrustCloneMissing, owner, repo, owner, repo), nil
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
	// **上限を置く。**CheckTrust と同じ理由である（固まると呼び出し側ごと止まる）。
	ctx, cancel := context.WithTimeout(context.Background(), trustCheckTimeout)
	defer cancel()

	key, err := gitToplevel(ctx, clonePath)
	if err != nil {
		return false, "", i18n.Errorf(
			i18n.KeyWorkspaceCheckTrustForClonePathToplevelFailed, clonePath, err)
	}

	configPath := filepath.Join(homeDir, ClaudeConfigFileName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, i18n.T(i18n.KeyWorkspaceCheckTrustForClonePathConfigMissing, configPath), nil
		}
		return false, "", i18n.Errorf(i18n.KeyWorkspaceCheckTrustForClonePathConfigUnreadable, configPath, err)
	}
	var parsed claudeConfigFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return false, "", i18n.Errorf(i18n.KeyWorkspaceCheckTrustForClonePathConfigUnparsable, configPath, err)
	}

	entry, ok := parsed.Projects[key]
	if !ok {
		return false, i18n.T(i18n.KeyWorkspaceCheckTrustForClonePathKeyMissing, key, configPath), nil
	}
	if !entry.HasTrustDialogAccepted {
		return false, i18n.T(i18n.KeyWorkspaceCheckTrustForClonePathNotAccepted, key, configPath), nil
	}
	return true, i18n.T(i18n.KeyWorkspaceCheckTrustForClonePathTrusted, key), nil
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
// **結果は trustCacheTTL のあいだ覚える。**この関数はボードの項目ごとに呼ばれるので、
// 覚えないと1巡回で ghq と git を数百回起動する（判定できなかった場合は覚えない）。
//
// 戻り値: owner と repo を受け取り、信頼済みなら true を返す関数。
func (m *Manager) TrustFunc() func(owner, repo string) bool {
	return func(owner, repo string) bool {
		key := owner + "/" + repo
		if cached, ok := m.trustResults.get(key); ok {
			return cached
		}
		trusted, reason, err := m.CheckTrust(owner, repo)
		if err != nil {
			m.logger.Warn("リポジトリの信頼を判定できませんでした",
				"owner", owner, "repo", repo, "error", err)
			return false
		}
		m.trustResults.put(key, trusted)
		if !trusted {
			m.logger.Info("リポジトリが Claude Code に信頼登録されていません",
				"owner", owner, "repo", repo, "reason", reason)
		}
		return trusted
	}
}
