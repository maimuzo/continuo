package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/maimuzo/continuo/internal/workspace"
)

// fakeClaude は「テスト用Claude Code mock」である。**実プロセスは1つも起動しない。**
//
// **枠を1トークンも使わずに、エージェントが1つの turn でやることを全部やる。**
// テスト用herdr mock が `agent.prompt` を受けたときに Act が呼ばれ、次の順で本物と同じ足跡を残す。
//
//	1  `gh issue view <URL> --comments` で issue を読む（テスト用gh mock が答える）
//	2  worktree の中でファイルを1つ作り、commit して push する
//	3  `gh issue comment` で作業の内容を書く（marker 付き。代筆の判定に使われる）
//	4  transcript の JSONL を書く（`CONTINUO-STATUS: review` の行を含む）
//	5  **`continuo hook` を叩いて Stop を送る**（transcript_path と session_id を渡す）
//
// **hook のコマンド行は設定ファイルから読む。**continuo が `--settings` で渡した
// settings.json の `hooks.Stop[].hooks[].command` をそのまま実行するので、
// 設定ファイルの組み立てまで含めて確かめられる。
type fakeClaude struct {
	// T は呼び出し元のテストである。**Act は接続ごとの goroutine から呼ばれるので
	// t.Errorf だけを使うこと。**
	T *testing.T
	// Home はテスト用ホームディレクトリである（transcript の置き場所の根になる）。
	Home string
	// BinDir はテスト用gh / ghq mock を置いたディレクトリである。
	BinDir string
	// BoardPath は偽のボードの JSON の絶対パスである（テスト用gh mock へ環境変数で渡す）。
	BoardPath string

	mu sync.Mutex
	// turns は agent 名から、その agent へ届いた turn の数を引く写像である。
	turns map[string]int
	// signal は transcript に書く表明の値である（既定は "review"）。
	signal string
	// transcripts は書いた transcript のパスを書いた順に並べたものである。
	transcripts []string
	// hookCommands は実行した hook のコマンド行を実行した順に並べたものである
	// （**`continuo hook` を本当に叩いたことの記録である**）。
	hookCommands []string
	// commits は作った commit の件数である。
	commits int
}

// newFakeClaude はテスト用Claude Code mock を1つ作る。
//
// t: 呼び出し元のテスト。
// home: テスト用ホームディレクトリ。
// binDir: テスト用gh / ghq mock を置いたディレクトリ。
// boardPath: 偽のボードの JSON の絶対パス。
// 戻り値: テスト用Claude Code mock。
func newFakeClaude(t *testing.T, home, binDir, boardPath string) *fakeClaude {
	t.Helper()
	return &fakeClaude{
		T: t, Home: home, BinDir: binDir, BoardPath: boardPath,
		turns: map[string]int{}, signal: "review",
	}
}

// Turns は agent へ届いた turn の数を返す。
//
// name: agent 名。
// 戻り値: turn の数。
func (fc *fakeClaude) Turns(name string) int {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.turns[name]
}

// Commits はテスト用Claude Code mock が作った commit の件数を返す。
func (fc *fakeClaude) Commits() int {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.commits
}

// HookCommands は実行した hook のコマンド行を実行した順に返す。
//
// **設定ファイルから読んだものをそのまま実行している**ので、
// `continuo hook --socket … --pending-dir …` が並ぶ。
func (fc *fakeClaude) HookCommands() []string {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	out := make([]string, len(fc.hookCommands))
	copy(out, fc.hookCommands)
	return out
}

// Transcripts は書いた transcript のパスを書いた順に返す。
func (fc *fakeClaude) Transcripts() []string {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	out := make([]string, len(fc.transcripts))
	copy(out, fc.transcripts)
	return out
}

// Act は1つの turn を演じる。
//
// **接続ごとの goroutine から呼ばれるので t.Fatalf を使ってはならない。**
//
// sess: テスト用herdr mock が覚えている agent（worktree・設定ファイル・セッション UUID）。
// text: 送られたプロンプトの本文。
func (fc *fakeClaude) Act(sess *agentSession, text string) {
	fc.mu.Lock()
	fc.turns[sess.Name]++
	turn := fc.turns[sess.Name]
	signal := fc.signal
	fc.mu.Unlock()

	identity, ok := fc.readIdentity(sess.WorktreePath)
	if !ok {
		return
	}

	// 1: issue を読む（プロンプトの本文が指示している操作をそのまま行う）。
	fc.runGH(sess.WorktreePath, "issue", "view", identity.IssueURL, "--comments")

	// 2: 作業して commit して push する。
	fc.work(sess, identity, turn)

	// 3: 作業の内容を issue に書く（**marker が要る。**無いと continuo が代筆に入る）。
	fc.runGH(sess.WorktreePath, "issue", "comment", identity.IssueURL, "--body",
		fmt.Sprintf("<!-- continuo:agent -->\n%d 回目の turn で作業して push しました。", turn))

	// 4: transcript を書く。
	transcriptPath, ok := fc.writeTranscript(sess, text, signal)
	if !ok {
		return
	}

	// 5: `continuo hook` を叩いて Stop を送る。
	fc.sendStop(sess, identity, transcriptPath)
}

// readIdentity は worktree の身元ファイルを読む。
//
// **エージェントは自分がどの issue の worktree に居るかをここで知る。**
//
// worktreePath: worktree の絶対パス。
// 戻り値の1つ目: 読み取った身元。
// 戻り値の2つ目: 読めれば true。
func (fc *fakeClaude) readIdentity(worktreePath string) (workspace.Identity, bool) {
	var identity workspace.Identity
	raw, err := os.ReadFile(filepath.Join(worktreePath, ".continuo.json"))
	if err != nil {
		fc.T.Errorf("テスト用Claude Code mock が身元ファイルを読めません（%s）: %v", worktreePath, err)
		return identity, false
	}
	if err := json.Unmarshal(raw, &identity); err != nil {
		fc.T.Errorf("テスト用Claude Code mock が身元ファイルを解釈できません: %v", err)
		return identity, false
	}
	return identity, true
}

// work は worktree の中でファイルを1つ作り、commit して push する。
//
// **push まで行う。**cleanup.require_pushed が真のままで片付けを通したいので、
// upstream のある branch にしておく必要がある（設計 3-9 の手順2b）。
//
// sess: 対象の agent。
// identity: worktree の身元。
// turn: 何回目の turn か。
func (fc *fakeClaude) work(sess *agentSession, identity workspace.Identity, turn int) {
	note := filepath.Join(sess.WorktreePath, "AGENT_NOTE.md")
	body := fmt.Sprintf("# %s\n\n%d 回目の turn で書いた。\n", identity.IssueIdentifier, turn)
	if err := os.WriteFile(note, []byte(body), 0o600); err != nil {
		fc.T.Errorf("テスト用Claude Code mock が作業のファイルを書けません: %v", err)
		return
	}
	fc.runGit(sess.WorktreePath, "add", "-A")
	fc.runGit(sess.WorktreePath, "commit", "--quiet", "-m",
		fmt.Sprintf("%s の作業（%d 回目の turn）", identity.IssueIdentifier, turn))
	fc.runGit(sess.WorktreePath, "push", "--quiet", "-u", "origin", identity.Branch)

	fc.mu.Lock()
	fc.commits++
	fc.mu.Unlock()
}

// writeTranscript は transcript の JSONL を書く。
//
// **形は既存のテスト（test/internal/orchestrator）と同じである**
// （`promptSource` / `isSidechain` / `message.content[].text` / `requestId` /
// `message.usage`）。**置き場所は `<HOME>/.claude/projects/<worktree のスラグ>/` である**
// （continuo は許可された根の外の transcript_path を捨てる）。
//
// sess: 対象の agent。
// text: 送られたプロンプトの本文（turn の頭の user 行に書く）。
// signal: 表明の値（`CONTINUO-STATUS: <値>`）。
// 戻り値の1つ目: 書いた transcript の絶対パス。
// 戻り値の2つ目: 書けたら true。
func (fc *fakeClaude) writeTranscript(sess *agentSession, text, signal string) (string, bool) {
	slug := strings.ReplaceAll(strings.TrimPrefix(sess.WorktreePath, "/"), "/", "-")
	dir := filepath.Join(fc.Home, ".claude", "projects", slug)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fc.T.Errorf("テスト用Claude Code mock が transcript の置き場所を作れません: %v", err)
		return "", false
	}
	path := filepath.Join(dir, sess.SessionUUID+".jsonl")

	fc.mu.Lock()
	turn := fc.turns[sess.Name]
	fc.mu.Unlock()

	lines := []any{
		map[string]any{
			"type": "user", "promptSource": "typed",
			"promptId": fmt.Sprintf("p%d", turn), "isSidechain": false,
			"message": map[string]any{"content": text},
		},
		map[string]any{
			"type": "assistant", "isSidechain": false,
			"requestId": fmt.Sprintf("req%d", turn),
			"message": map[string]any{
				"content": []any{map[string]any{
					"type": "text",
					"text": "実装して commit と push をしました。\n\nCONTINUO-STATUS: " + signal,
				}},
				"usage": map[string]any{
					"input_tokens": 10, "cache_creation_input_tokens": 20,
					"cache_read_input_tokens": 30, "output_tokens": 40,
				},
			},
		},
	}
	var b strings.Builder
	for _, line := range lines {
		encoded, err := json.Marshal(line)
		if err != nil {
			fc.T.Errorf("テスト用Claude Code mock が transcript の行を JSON 化できません: %v", err)
			return "", false
		}
		b.Write(encoded)
		b.WriteByte('\n')
	}
	// **追記する。**同じセッションで2回目の turn が来ても、前の turn の行を消さない。
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fc.T.Errorf("テスト用Claude Code mock が transcript を開けません: %v", err)
		return "", false
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(b.String()); err != nil {
		fc.T.Errorf("テスト用Claude Code mock が transcript を書けません: %v", err)
		return "", false
	}

	fc.mu.Lock()
	fc.transcripts = append(fc.transcripts, path)
	fc.mu.Unlock()
	return path, true
}

// sendStop は設定ファイルに書かれた hook のコマンド行を実行して Stop を送る。
//
// **`continuo hook` を叩く。**socket へ直接書かないのは、
// 設定ファイル（`--settings` で渡したもの）の hook の組み立てまで確かめるためである。
//
// sess: 対象の agent。
// identity: worktree の身元（cwd の検査に通る値を送るために使う）。
// transcriptPath: 書いた transcript の絶対パス。
func (fc *fakeClaude) sendStop(sess *agentSession, identity workspace.Identity, transcriptPath string) {
	command, ok := fc.stopHookCommand(sess.SettingsPath)
	if !ok {
		return
	}
	event := map[string]any{
		"session_id":       sess.SessionUUID,
		"transcript_path":  transcriptPath,
		"cwd":              sess.WorktreePath,
		"hook_event_name":  "Stop",
		"background_tasks": []any{},
		"stop_hook_active": false,
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		fc.T.Errorf("テスト用Claude Code mock が hook を JSON 化できません: %v", err)
		return
	}

	fc.mu.Lock()
	fc.hookCommands = append(fc.hookCommands, command)
	fc.mu.Unlock()

	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Dir = sess.WorktreePath
	cmd.Stdin = strings.NewReader(string(encoded) + "\n")
	cmd.Env = fc.childEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		fc.T.Errorf("テスト用Claude Code mock が hook を実行できません（%s）: %v\n%s", command, err, out)
		return
	}
	_ = identity
}

// stopHookCommand は設定ファイルから Stop hook のコマンド行を読む。
//
// settingsPath: `--settings` で渡された設定ファイルの絶対パス。
// 戻り値の1つ目: コマンド行。
// 戻り値の2つ目: 読めたら true。
func (fc *fakeClaude) stopHookCommand(settingsPath string) (string, bool) {
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		fc.T.Errorf("テスト用Claude Code mock が設定ファイルを読めません（%s）: %v", settingsPath, err)
		return "", false
	}
	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		fc.T.Errorf("テスト用Claude Code mock が設定ファイルを解釈できません: %v", err)
		return "", false
	}
	for _, matcher := range settings.Hooks["Stop"] {
		for _, h := range matcher.Hooks {
			if h.Type == "command" && h.Command != "" {
				return h.Command, true
			}
		}
	}
	fc.T.Errorf("設定ファイルに Stop hook のコマンドがありません: %s", settingsPath)
	return "", false
}

// runGit は worktree の中で git を実行する。
//
// **HOME はテスト用ホームディレクトリにする。**実物の `~/.gitconfig` を読ませない
// （署名の設定などが混ざると commit が失敗する）。
//
// dir: 実行する作業ディレクトリ。
// args: git の引数。
func (fc *fakeClaude) runGit(dir string, args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = fc.childEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		fc.T.Errorf("テスト用Claude Code mock が `git %s` に失敗しました: %v\n%s",
			strings.Join(args, " "), err, out)
	}
}

// runGH はテスト用gh mock を実行する。
//
// dir: 実行する作業ディレクトリ。
// args: gh の引数。
func (fc *fakeClaude) runGH(dir string, args ...string) {
	cmd := exec.Command(filepath.Join(fc.BinDir, "gh"), args...)
	cmd.Dir = dir
	cmd.Env = fc.childEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		fc.T.Errorf("テスト用Claude Code mock が `gh %s` に失敗しました: %v\n%s",
			strings.Join(args, " "), err, out)
	}
}

// childEnv はテスト用Claude Code mock が起動する子プロセスへ渡す環境変数を組み立てる。
//
// **実物のホームディレクトリと実物の gh を見せない。**
//
// 戻り値: 環境変数の並び。
func (fc *fakeClaude) childEnv() []string {
	return []string{
		"PATH=" + fc.BinDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + fc.Home,
		"CONTINUO_E2E_BOARD=" + fc.BoardPath,
		"GIT_TERMINAL_PROMPT=0",
	}
}
