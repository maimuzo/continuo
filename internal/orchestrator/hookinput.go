package orchestrator

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/maimuzo/continuo/internal/hookserver"
)

// defaultTranscriptDirName は transcript の置き場所の既定の根である（ホームからの相対）。
//
// **Claude Code は `~/.claude/projects/<worktree ごとのディレクトリ>/<セッション UUID>.jsonl`
// に書く**（設計 3-15 の実測。3-4 の脚注も同じ場所を指している）。
var defaultTranscriptDirName = filepath.Join(".claude", "projects")

// sanitizeHookEvent は hook の JSON に入っている外部入力を検査する（設計 3-2 / 3-23）。
//
// **hook の中身はエージェントが書き換えられる外部入力である。**socket にも逃がし先にも
// 送り主を確かめる手立ては無く、run の特定は `session_id` だけで行っている。
// **そのまま信じてはならない。**
//
//	cwd             … その run の worktree の内側でなければ、**その hook ごと捨てる**
//	transcript_path … 絶対パス・許可された根の内側・通常のファイル、の3つを満たさなければ
//	                  **その項目だけを落とす**（stall の時計は進めたいので hook は捨てない）
//
// **`transcript_path` の検査がとくに重い。**FIFO のパスを渡されると `os.Open` が
// 書き手の現れるまで永久に返らず、turn ループの goroutine ごと固まる。その goroutine は
// `o.wg` に載っているので `Close()` も返らなくなり、**無人の常駐プロセスが SIGTERM でも
// 終われなくなる。**
//
// ev: 届いた hook。
// rs: `session_id` で引いた run。
// 戻り値の1つ目: 検査を通した hook（`transcript_path` が落ちていることがある）。
// 戻り値の2つ目: この hook を使ってよければ true。偽なら捨てる。
func (o *Orchestrator) sanitizeHookEvent(ev hookserver.HookEvent, rs *runState) (hookserver.HookEvent, bool) {
	if !o.acceptHookCwd(rs, ev.Cwd) {
		o.logger.Warn("hook の cwd がその run の worktree の外なので捨てました（session_id を騙った hook かもしれません）",
			"identifier", rs.issue().Identifier, "cwd", ev.Cwd, "session_id", ev.SessionID)
		return ev, false
	}
	if ev.TranscriptPath != "" && !o.acceptTranscriptPath(rs, ev.TranscriptPath) {
		ev.TranscriptPath = ""
	}
	return ev, true
}

// acceptHookCwd は hook の `cwd` がその run の worktree の内側かを判定する。
//
// **判定できない場合は通す。**`cwd` が空の hook（逃がし先から読み戻したものなど）や、
// worktree のパスをまだ知らない run で hook を落とすと、turn の終わりを検知できなくなる。
//
// rs: `session_id` で引いた run。
// cwd: hook の `cwd`。
// 戻り値: 受け入れてよければ true。
func (o *Orchestrator) acceptHookCwd(rs *runState, cwd string) bool {
	if cwd == "" {
		return true
	}
	root := rs.snapshot().WorktreePath
	if root == "" {
		return true
	}
	resolvedRoot, ok := resolvePath(root)
	if !ok {
		return true
	}
	resolved, ok := resolvePath(cwd)
	if !ok {
		return false
	}
	return resolved == resolvedRoot || isUnder(resolvedRoot, resolved)
}

// acceptTranscriptPath は hook が渡した `transcript_path` を受け入れてよいかを判定する。
//
// 3つを確かめる。
//
//	絶対パスであること           … 相対パスは continuo の作業ディレクトリ次第で別物を指す
//	許可された根の内側であること  … 既定は `~/.claude/projects`（`Options.TranscriptRoot` で変えられる）
//	通常のファイルであること      … **FIFO・デバイス・ディレクトリを弾く。**FIFO は開いた
//	                              まま永久に返らない
//
// **根が決まっていなければ根の検査だけを飛ばす**（`os.UserHomeDir` に失敗した場合）。
// 通常のファイルであることの検査は必ず行う。
//
// **「まだ無い」と「不正」を分ける。**Claude Code は transcript を非同期に書くので、
// `SessionStart` と `UserPromptSubmit` が発火する時点ではファイルが1バイトも
// 書かれていないことがある（設計 3-25）。**これは正常な並びであり、警告ではない。**
// **まだ無いパスは Debug に落として、その項目だけを捨てる。**
//
// **検査の順番。**シンボリックリンクの解決（`filepath.EvalSymlinks`）は実在するときにしか
// 通らないので、まず解決を試し、次に `os.Lstat` で「有るのか無いのか」を決め、
// **有ると分かってから根の内側かを比べる。**無いパスは1バイトも読まないので、
// 根の検査を飛ばしても抜け道にはならない。
//
// rs: `session_id` で引いた run。
// path: hook の `transcript_path`。
// 戻り値: 受け入れてよければ true。
func (o *Orchestrator) acceptTranscriptPath(rs *runState, path string) bool {
	identifier := rs.issue().Identifier
	if !filepath.IsAbs(path) {
		o.logger.Warn("hook の transcript_path が絶対パスではないので捨てました",
			"identifier", identifier, "transcript_path", path)
		return false
	}
	// **実在するなら解決してから比べる。**実在しなければ字句のままにしておく。
	resolved := filepath.Clean(path)
	if r, ok := resolvePath(resolved); ok {
		resolved = r
	}
	info, err := os.Lstat(resolved)
	if errors.Is(err, os.ErrNotExist) {
		// **まだ書かれていないだけである。**この項目だけ落とし、hook そのものは使う。
		o.logger.Debug("hook の transcript_path はまだ作られていないので、この項目だけ落とします",
			"identifier", identifier, "transcript_path", path)
		return false
	}
	if err != nil {
		o.logger.Warn("hook の transcript_path を読めないので捨てました",
			"identifier", identifier, "transcript_path", path, "error", err)
		return false
	}
	if o.transcriptRoot != "" && !isUnder(o.transcriptRoot, resolved) {
		o.logger.Warn("hook の transcript_path が許可された置き場所の外なので捨てました",
			"identifier", identifier, "transcript_path", path, "根", o.transcriptRoot)
		return false
	}
	if !info.Mode().IsRegular() {
		o.logger.Warn("hook の transcript_path が通常のファイルではないので捨てました（FIFO を開くと永久に返らない）",
			"identifier", identifier, "transcript_path", path, "mode", info.Mode().String())
		return false
	}
	return true
}

// resolveTranscriptRoot は transcript の置き場所の根を決める。
//
// configured: `Options.TranscriptRoot` の値。空ならホームから既定を組み立てる。
// 戻り値の1つ目: 解決した根（シンボリックリンクを解いた絶対パス）。決められなければ空文字。
// 戻り値の2つ目: 決められなかった理由。決められた場合は nil。
func resolveTranscriptRoot(configured string) (string, error) {
	root := configured
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, defaultTranscriptDirName)
	}
	if resolved, ok := resolvePath(root); ok {
		return resolved, nil
	}
	// まだ作られていないことがある（1回も Claude Code を起動していないホーム）。
	// **字句だけで持っておく。**実在するようになれば、そのまま突き合わせに使える。
	return filepath.Clean(root), nil
}
