// Package plugins は、continuo の実行ファイルに埋め込む Claude Code の plugin を持つ（issue #288）。
//
// **埋め込むのは `continuo-agent-procedures` だけである。**continuo が起動した Claude Code が、
// フローチャートの段ごとに読む手順（スキル）の置き場である。continuo は Claude Code を起動する前に毎回、
// この中身を実行時ディレクトリへ書き出し、`--plugin-dir` で渡す（`internal/agentplugin`）。
//
// **`continuo-issue-comments` は埋め込まない。**人間が自分で起動した Claude Code のための plugin で、
// 利用者が marketplace から入れるものである。continuo の run では使わない。
//
// **ここに置くファイルの中身は、書き出すときに2つの語を置き換える。**
//
//	CONTINUO_PLUGIN_HASH  … plugin の中身のハッシュ12桁（各 SKILL.md の終わりの印に使う）
//	CONTINUO_PLUGIN_DIR   … 書き出した plugin のディレクトリの絶対パス（scripts を叩くときに使う）
//
// **ハッシュは、置き換える前の中身から計算する。**印にハッシュを書いてから計算すると、値が定まらない。
package plugins

import "embed"

// AgentProceduresName は、continuo が起動した Claude Code に渡す plugin の名前である。
//
// **`.claude-plugin/plugin.json` の `name` と、埋め込むディレクトリの名前と同じである。**
// Skill ツールへ渡す完全な名前は `continuo-agent-procedures:<スキル名>` になる。
const AgentProceduresName = "continuo-agent-procedures"

// 書き出すときに置き換える語である。**システムプロンプトの雛形（internal/prompt）も同じ語を使う。**
const (
	// HashPlaceholder は plugin の中身のハッシュ12桁に置き換わる語である。
	HashPlaceholder = "CONTINUO_PLUGIN_HASH"
	// DirPlaceholder は書き出した plugin のディレクトリの絶対パスに置き換わる語である。
	DirPlaceholder = "CONTINUO_PLUGIN_DIR"
)

// AgentProcedures は `continuo-agent-procedures` の中身である。
//
// **`all:` を付ける。**付けないと `.claude-plugin/` のような `.` で始まるディレクトリが落ち、
// Claude Code が plugin として読めなくなる。
//
//go:embed all:continuo-agent-procedures
var AgentProcedures embed.FS
