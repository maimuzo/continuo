package orchestrator_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/agentplugin"
	"github.com/maimuzo/continuo/internal/orchestrator"
)

// agentView は、エージェントが受け取る指示の全体を、この issue の値で埋めた形で返す（issue #288）。
//
// **手順の本文は、送る文面（最初のプロンプト）ではなく、plugin のスキルにある。**エージェントは
// システムプロンプト（`--append-system-prompt-file`）の地図と「この issue の値」の表を見て、
// スキルの山括弧の語（`<issue のリポジトリ>` など）をその値で埋めて使う。
// **ここでは、その埋める作業をテストの側で行い、エージェントが読むものを1枚にする。**
//
// **読むのは、continuo が実際に書いたファイルである。**system-prompt.md と、書き出した plugin の SKILL.md。
// 書き出しが落ちていれば、ここで落ちる。
//
// t: テストコンテキスト。
// fx: 着手を済ませた fixture。
// identifier: issue の識別子。
// first: 送った最初のプロンプト。
// 戻り値: 最初のプロンプト・システムプロンプト・スキル11本を、この issue の値で埋めて連結したもの。
func agentView(t *testing.T, fx *fixture, identifier, first string) string {
	t.Helper()

	runtimeDir := filepath.Dir(fx.SocketPath)
	sp := filepath.Join(runtimeDir, "issues", orchestrator.IssueSlug(identifier), agentplugin.SystemPromptFileName)
	data, err := os.ReadFile(sp)
	if err != nil {
		t.Fatalf("システムプロンプトを読めません: %v", err)
	}
	system := string(data)

	// **「この issue の値」の表の行を読む。**`| <語> | 値 |` の形である。
	values := map[string]string{}
	pluginDir := ""
	for _, line := range strings.Split(system, "\n") {
		cells := strings.Split(strings.Trim(line, "|"), " | ")
		if len(cells) != 2 {
			continue
		}
		key := strings.Trim(strings.TrimSpace(cells[0]), "`")
		value := strings.TrimSpace(cells[1])
		switch {
		case key == "plugin のディレクトリ":
			pluginDir = value
		case strings.HasPrefix(key, "<") && strings.HasSuffix(key, ">"):
			values[key] = value
		}
	}
	if pluginDir == "" || !filepath.IsAbs(pluginDir) {
		t.Fatalf("システムプロンプトに plugin のディレクトリの絶対パスがありません: %q", pluginDir)
	}
	if len(values) == 0 {
		t.Fatal("システムプロンプトに「この issue の値」の表がありません")
	}

	paths, err := filepath.Glob(filepath.Join(pluginDir, "skills", "*", "SKILL.md"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("書き出した plugin に SKILL.md がありません（%s）: %v", pluginDir, err)
	}
	sort.Strings(paths)
	var b strings.Builder
	b.WriteString(first + "\n" + system)
	for _, p := range paths {
		skill, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s を読めません: %v", p, err)
		}
		b.WriteString("\n" + string(skill))
	}
	out := b.String()
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = strings.ReplaceAll(out, k, values[k])
	}
	return out
}

// assertLaunchArgs は、Claude Code の起動の引数に、システムプロンプトと plugin が渡っていることを確かめる（issue #288）。
//
// **Claude Code を起動する経路は、着手と再着手・書かせ直しの復元・direct chat の3つである**（`claudeStartArgs` の呼び出し元）。
// どれかで渡し忘れると、その経路で起動した Claude Code はフローチャートもスキルも持たない。
//
// 見るもの:
//
//	--append-system-prompt-file <実行時ディレクトリ>/issues/<スラグ>/system-prompt.md （そのファイルが在る）
//	--system-prompt-snapshot off                                                      （毎回付ける）
//	--plugin-dir <実行時ディレクトリ>/plugins/<12桁>/continuo-agent-procedures         （SKILL.md が在る）
//
// t: テストコンテキスト。
// fx: fixture。
// identifier: issue の識別子。
// args: `agent.start` に渡した args。
func assertLaunchArgs(t *testing.T, fx *fixture, identifier string, args []any) {
	t.Helper()
	runtimeDir := filepath.Dir(fx.SocketPath)
	wantPrompt := filepath.Join(runtimeDir, "issues", orchestrator.IssueSlug(identifier), agentplugin.SystemPromptFileName)
	wantPlugin := agentplugin.PluginDir(runtimeDir)
	pairs := map[string]string{}
	for i := 0; i+1 < len(args); i++ {
		k, _ := args[i].(string)
		v, _ := args[i+1].(string)
		if strings.HasPrefix(k, "--") {
			pairs[k] = v
		}
	}
	if pairs["--append-system-prompt-file"] != wantPrompt {
		t.Errorf("--append-system-prompt-file が %q です（%q のはず）: %v", pairs["--append-system-prompt-file"], wantPrompt, args)
	}
	if pairs["--system-prompt-snapshot"] != "off" {
		t.Errorf("--system-prompt-snapshot off を付けていません: %v", args)
	}
	if pairs["--plugin-dir"] != wantPlugin {
		t.Errorf("--plugin-dir が %q です（%q のはず）: %v", pairs["--plugin-dir"], wantPlugin, args)
	}
	if _, err := os.Stat(wantPrompt); err != nil {
		t.Errorf("渡したシステムプロンプトのファイルがありません: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wantPlugin, "skills", "starting-run", "SKILL.md")); err != nil {
		t.Errorf("渡した plugin のディレクトリにスキルがありません: %v", err)
	}
}
