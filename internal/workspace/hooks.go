package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// hookShell は workspace_hooks のコマンド文字列を実行するシェルである。
// 設定にはコマンド行が1本の文字列で書かれるので、シェルに解釈させる。
const hookShell = "sh"

// HookPhase は workspace_hooks のどの段のコマンドかを表す。
type HookPhase string

// HookPhase の取りうる値である（設定の workspace_hooks の各キーと1対1）。
const (
	// HookAfterCreate は worktree を新しく作った直後である（3-16 の段4）。
	// **再利用したときは実行しない**（SPEC.md 5.3.4）。失敗したら致命として扱う。
	HookAfterCreate HookPhase = "after_create"
	// HookBeforeRun は Claude Code を起動する直前である（3-16 の段7）。失敗したら致命。
	HookBeforeRun HookPhase = "before_run"
	// HookAfterRun は run が終わったときである（3-9 の段0）。
	// **turn ごとではなく、worker を止める直前に1回だけ。**失敗しても記録して続ける。
	HookAfterRun HookPhase = "after_run"
	// HookBeforeRemove は worktree を消す直前である（3-9 の段2d）。
	// 失敗しても記録して続ける（片付けを止めない）。
	HookBeforeRemove HookPhase = "before_remove"
)

// command は phase に対応する設定のコマンド文字列を返す。
//
// phase: 対象の段。
// 戻り値の1つ目: 設定されたコマンド文字列。
// 戻り値の2つ目: 設定されていれば true（null や空文字なら false）。
func (m *Manager) command(phase HookPhase) (string, bool) {
	var raw *string
	switch phase {
	case HookAfterCreate:
		raw = m.cfg.WorkspaceHooks.AfterCreate
	case HookBeforeRun:
		raw = m.cfg.WorkspaceHooks.BeforeRun
	case HookAfterRun:
		raw = m.cfg.WorkspaceHooks.AfterRun
	case HookBeforeRemove:
		raw = m.cfg.WorkspaceHooks.BeforeRemove
	}
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return "", false
	}
	return *raw, true
}

// RunHook は workspace_hooks のコマンドを1本実行する。
//
// cwd は dir である（after_run と before_remove では worktree、
// after_create と before_run でも worktree。3-9 / 3-16）。
// 実行時間の上限は workspace_hooks.timeout_ms である。
// **未設定（null や空文字）のときは何もせず nil を返す。**
//
// ctx: 実行に適用するコンテキスト。
// phase: 実行する段。
// dir: コマンドの作業ディレクトリ。
// 戻り値: コマンドが非 0 で終わった場合・起動できなかった場合・時間切れの場合のエラー。
// 標準出力と標準エラー出力の内容をエラー文に含める。
// **失敗を致命として扱うかどうかは呼び出し側が決める**（段によって違う）。
func (m *Manager) RunHook(ctx context.Context, phase HookPhase, dir string) error {
	command, ok := m.command(phase)
	if !ok {
		return nil
	}

	runCtx := ctx
	if timeout := time.Duration(m.cfg.WorkspaceHooks.TimeoutMs) * time.Millisecond; timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, hookShell, "-c", command)
	cmd.Dir = dir
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	start := m.now()
	err := cmd.Run()
	m.logger.Info("workspace_hooks を実行しました",
		"phase", string(phase), "dir", dir, "duration_ms", m.now().Sub(start).Milliseconds())
	if err != nil {
		return fmt.Errorf(
			"workspace_hooks.%s の実行に失敗しました（cwd=%s、出力: %s）: %w",
			phase, dir, strings.TrimSpace(output.String()), err)
	}
	return nil
}

// RunAfterRunOnce は workspace_hooks.after_run を、**いま進行中の run について1回だけ**
// 実行する（3-9 の段0）。
//
// **run が終わったとき（worker を止める直前）に呼ぶ。turn ごとではない**（SPEC.md 5.3.4）。
// 同じ run の中での2回目以降の呼び出しは何もしない。
//
// **印は run 単位であって worktree 単位ではない。**同じ worktree を再利用するということは、
// その issue が再び dispatch されたということであり、**そこから先は別の run である**（3-18）。
// 印は BeginRun（Prepare が呼ぶ）で消えるので、2回目の run でも after_run は実行される。
//
// **この記録はメモリにしか無い**が、プロセスを再起動すると同じ run は続かないので足りる。
// **複数の run から同時に呼んでよい**（afterRunMu で守る）。
//
// ctx: 実行に適用するコンテキスト。
// worktreePath: cwd にする worktree の絶対パス。
// 戻り値の1つ目: 実際に実行したかどうか（同じ run の2回目以降は false）。
// 戻り値の2つ目: 実行に失敗した場合のエラー。**呼び出し側は記録して続けること**
// （after_run の失敗で run の後片付けを止めない）。
func (m *Manager) RunAfterRunOnce(ctx context.Context, worktreePath string) (bool, error) {
	if !m.markAfterRun(worktreePath) {
		return false, nil
	}
	return true, m.RunHook(ctx, HookAfterRun, worktreePath)
}

// BeginRun は worktree に対する after_run の印を消し、**そこから新しい run が始まる**
// ことを宣言する（3-18 の「再利用するということは、再び dispatch されたということである」）。
//
// **Prepare が用意を終えたときに呼ぶ。**引き継ぎ（3-4）のように Prepare を通らずに
// run を始める経路ができたときは、そこでも呼ぶこと。呼ばないと、2回目以降の run で
// after_run が二度と実行されない。
//
// worktreePath: 新しい run を始める worktree の絶対パス。
func (m *Manager) BeginRun(worktreePath string) {
	key := afterRunKey(worktreePath)
	m.afterRunMu.Lock()
	defer m.afterRunMu.Unlock()
	delete(m.afterRunDone, key)
}

// markAfterRun は after_run の実行済みの印を立て、立てられたかどうかを返す。
//
// worktreePath: 対象の worktree の絶対パス。
// 戻り値: 印を新しく立てたら true（＝これから実行する）。既に立っていたら false。
func (m *Manager) markAfterRun(worktreePath string) bool {
	key := afterRunKey(worktreePath)
	m.afterRunMu.Lock()
	defer m.afterRunMu.Unlock()
	if m.afterRunDone[key] {
		return false
	}
	m.afterRunDone[key] = true
	return true
}

// afterRunKey は after_run の印の鍵を作る。
//
// **シンボリックリンクを解決してから鍵にする。**Prepare は解決済みのパスを返すが、
// 呼び出し側が解決前のパスを渡してくることもありうるので、両者を同じ鍵に寄せる。
// 解決できない（既に消えている等）ときは Clean しただけの値を使う。
//
// worktreePath: worktree のパス。
// 戻り値: 印の鍵に使う文字列。
func afterRunKey(worktreePath string) string {
	if resolved, err := filepath.EvalSymlinks(worktreePath); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(worktreePath)
}
