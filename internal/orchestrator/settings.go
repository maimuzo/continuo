package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// settingsFileName は issue ごとの Claude Code の設定ファイルの名前である（設計 3-12）。
//
//	<実行時ディレクトリ>/issues/<issue のスラグ>/settings.json
//
// **worktree の中には置かない。**置くと `--settings` を選んだ意味が無くなる。
const settingsFileName = "settings.json"

// settingsFilePerm は設定ファイルのパーミッションである。
// hook のコマンド行に socket の絶対パスが入るので、他人に読ませない 0600 にする。
const settingsFilePerm os.FileMode = 0o600

// settingsDirPerm は issue ごとのディレクトリのパーミッションである。
const settingsDirPerm os.FileMode = 0o700

// hookEntry は Claude Code の設定ファイルの hooks の1件である。
type hookEntry struct {
	// Type は hook の種別である。continuo が使うのは "command" だけである。
	Type string `json:"type"`
	// Command は実行するコマンド行である（`continuo hook --socket <S> --pending-dir <P>`）。
	Command string `json:"command"`
}

// hookMatcher は hooks の1つの matcher の塊である。
type hookMatcher struct {
	// Matcher は対象を絞る文字列である。空なら送らない。
	// **`PreToolUse` / `PostToolUse` は `*`（全ツール）にする**（設計 3-2）。
	// `Agent|Task` に絞るとメインが叩いた Bash の記録が落ちる。
	Matcher string `json:"matcher,omitempty"`
	// Hooks は実行する hook の並びである。
	Hooks []hookEntry `json:"hooks"`
}

// claudeSettings は `--settings` で渡す設定ファイルの中身である（設計 3-2 / 3-12）。
//
// **hook と permissions.allow と env を1ファイルに書ける**（実測で確認済み）。
type claudeSettings struct {
	// Hooks はイベント名から matcher の並びへの対応である。
	Hooks map[string][]hookMatcher `json:"hooks"`
	// Permissions は許可・拒否リストである（`dontAsk` のとき許可リストの外は全部拒否される）。
	Permissions claudeSettingsPermissions `json:"permissions"`
	// Env は Claude Code のプロセスへ渡す環境変数である。
	//
	// **環境変数はここにしか書けない。**`worktree.open` にも `agent.start` にも env の
	// 引数が無い（設計 3-2 / 3-12。2026-08-19 に実測で確認済み）。
	Env map[string]string `json:"env,omitempty"`
}

// claudeSettingsPermissions は設定ファイルの permissions である。
type claudeSettingsPermissions struct {
	// Allow は許可するツール・コマンドのパターンである。
	Allow []string `json:"allow"`
	// Deny は明示的に拒否するパターンである。
	Deny []string `json:"deny,omitempty"`
}

// hookEventNames は設定ファイルへ張る hook の一覧である（設計 3-2 の「7つ張る」の表）。
//
// **この一覧が正である。**どれが欠けても turn の終わりの判定が成立しない。
// `PreToolUse` と `PostToolUse` は matcher を `*` にする（絞ってはならない）。
var hookEventNames = []struct {
	// Name は hook のイベント名である。
	Name string
	// Matcher は絞り込みの文字列である。空なら matcher を書かない。
	Matcher string
}{
	{Name: hookStop},
	{Name: hookUserPromptSubmit},
	{Name: hookSubagentStop},
	{Name: hookSubagentStart},
	{Name: hookNotification},
	{Name: hookSessionStart},
	{Name: hookPreToolUse, Matcher: "*"},
	{Name: hookPostToolUse, Matcher: "*"},
}

// writeSettingsFile は issue ごとの Claude Code の設定ファイルを worktree の外に作る
// （着手の段5。設計 3-12）。
//
// 書く場所と中身は次のとおりである。
//
//	<実行時ディレクトリ>/issues/<issue のスラグ>/settings.json
//
//	{
//	  "hooks": { "Stop": [{"hooks":[{"type":"command",
//	              "command":"'/usr/local/bin/continuo' hook --socket '/…/hooks.sock' --pending-dir '/…/pending'"}]}], … },
//	  "permissions": { "allow": ["Bash","Read","Glob","Grep","Edit","Write"], "deny": [] },
//	  "env": { "CLAUDE_CODE_RETRY_WATCHDOG": "1" }
//	}
//
// **再 dispatch のたびに作り直す**（socket のパスが変わっているかもしれない。設計 3-25）。
// **逃がし先のディレクトリも同時に作る。**`continuo hook` は自分でディレクトリを掘るが、
// 先に作っておくと hookserver の走査が最初の巡回から効く。
//
// identifier: issue の識別子（置き場所のスラグを作るのに使う）。
// 戻り値の1つ目: 書いた設定ファイルの絶対パス。
// 戻り値の2つ目: ディレクトリを作れない・JSON 化できない・書けない場合のエラー。
func (o *Orchestrator) writeSettingsFile(identifier string) (string, error) {
	dir := o.issueDir(identifier)
	if err := os.MkdirAll(dir, settingsDirPerm); err != nil {
		return "", fmt.Errorf("issue ごとの設定ファイルの置き場所を作れません: %s: %w", dir, err)
	}
	pending := o.pendingDir(identifier)
	if err := os.MkdirAll(pending, settingsDirPerm); err != nil {
		return "", fmt.Errorf("hook の逃がし先を作れません: %s: %w", pending, err)
	}

	// **shell の引用を通す。**この文字列は Claude Code が shell で実行する。
	// 引用せずに繋ぐと、パスに空白が1つ入るだけでコマンド行が別の引数へ割れ、
	// **7種の hook が1つも届かなくなる**（turn の終わりを永久に検知できない）。
	command := fmt.Sprintf("%s hook --socket %s --pending-dir %s",
		shellQuote(o.continuoPath), shellQuote(o.socketPath), shellQuote(pending))
	hooks := make(map[string][]hookMatcher, len(hookEventNames))
	for _, ev := range hookEventNames {
		hooks[ev.Name] = []hookMatcher{{
			Matcher: ev.Matcher,
			Hooks:   []hookEntry{{Type: "command", Command: command}},
		}}
	}

	settings := claudeSettings{
		Hooks: hooks,
		Permissions: claudeSettingsPermissions{
			Allow: o.cfg.Claude.Permissions.Allow,
			Deny:  o.cfg.Claude.Permissions.Deny,
		},
		Env: o.cfg.Claude.Env,
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", fmt.Errorf("Claude Code の設定ファイルを JSON 化できません: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(dir, settingsFileName)
	if err := os.WriteFile(path, data, settingsFilePerm); err != nil {
		return "", fmt.Errorf("Claude Code の設定ファイルを書けません: %s: %w", path, err)
	}
	return path, nil
}

// claudeStartArgs は agent.start の args に載せる Claude Code の起動フラグを組み立てる
// （着手の段9。設計 3-16）。
//
// **環境変数はここに載せない。**`agent.start` に env の引数は無いので、設定ファイルの
// `env` へ書く（設計 3-12）。
//
// settingsPath: 設定ファイルの絶対パス。
// sessionUUID: 採番したセッション UUID。空なら `--session-id` を付けない。
// resumeUUID: 復帰するセッションの UUID。空なら `--resume` を付けない
// （設計 3-25 の9段でだけ使う）。
// 戻り値: 起動フラグの並び。
func (o *Orchestrator) claudeStartArgs(settingsPath, sessionUUID, resumeUUID string) []string {
	args := []string{"--settings", settingsPath}
	if resumeUUID != "" {
		args = append(args, "--resume", resumeUUID)
	} else if sessionUUID != "" {
		args = append(args, "--session-id", sessionUUID)
	}
	if mode := o.cfg.Claude.PermissionMode; mode != "" {
		args = append(args, "--permission-mode", mode)
	}
	return args
}

// shellQuote は shell のコマンド行へ埋め込む1語を単一引用符で包む。
//
// **単一引用符の中では展開が一切起きない**ので、空白・`$`・バッククォート・`;` を
// そのまま渡せる。語の中に単一引用符があれば、「引用を閉じる・逃がした単一引用符を置く・
// 引用を開き直す」の3つを並べた形へ置き換える。
//
// s: 埋め込む1語。
// 戻り値: 引用した文字列。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
