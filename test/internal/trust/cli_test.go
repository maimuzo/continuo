// Package trust_test のうち、このファイルは `continuo trust` を実際に起動して、
// 端から端まで通ることを確かめる（設計 3-33）。
//
// **テスト用ホームディレクトリとテスト用ghq mock の置き場所を環境変数で渡す。**
// 実物の `~/.claude.json` には認証情報を含む全設定が入っており、テストが触ってはならない。
package trust_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildBinary は `continuo` をビルドする。
//
// **リポジトリの中には出力しない**（生成物を残さないため、テストの一時ディレクトリへ出す）。
//
// t: 呼び出し元のテスト。
// outDir: 出力先のディレクトリ。
// 戻り値: ビルドしたバイナリの絶対パス。
func buildBinary(t *testing.T, outDir string) string {
	t.Helper()

	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("リポジトリの場所を決められません: %v", err)
	}

	bin := filepath.Join(outDir, "continuo")
	cmd := exec.Command(goBin, "build", "-o", bin, "./cmd/continuo")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("continuo をビルドできません: %v\n%s", err, out)
	}
	return bin
}

// 目的: `continuo trust --dry-run` が要求内容を出すだけで、`~/.claude.json` を
// 1バイトも書き換えないことを、実際にコマンドを起動して確認する。
//
// **`--dry-run` は信頼のダイアログの代わりである**（設計 3-33）。
// ここが書き換えてしまうと、確かめてから決めるという手順そのものが成立しない。
//
// 与える情報: テスト用ホームディレクトリ・テスト用ghq mock の置き場所・
// trust.repositories に2件を書いた WORKFLOW.md。
// 成功条件: 出力に要求内容が出て、終了コードが 1（登録の対象が残っている）で、
// `~/.claude.json` が変わらず、バックアップも作られないこと。
func TestCLI_dryrunは要求内容を出すだけで書き換えない(t *testing.T) {
	requireCommands(t, "ghq", "git")

	env := setUpCLI(t)
	stdout, code := runContinuo(t, env, "trust", "--dry-run")

	if code != 1 {
		t.Errorf("登録の対象が残っているのに終了コードが 1 でない: %d\n%s", code, stdout)
	}
	for _, want := range []string{
		"maimuzo/demo-a", "Bash(rm -rf:*)", "/etc", "payments", "docs",
		"--dry-run なので何も書き換えていません",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("出力に %q が無い:\n%s", want, stdout)
		}
	}
	if got := readFile(t, env.configPath); got != env.before {
		t.Errorf("--dry-run なのにファイルが変わっている\n期待:\n%s\n実際:\n%s", env.before, got)
	}
	if names := backupNames(t, env.home); len(names) != 0 {
		t.Errorf("--dry-run なのにバックアップを作っている: %v", names)
	}
}

// 目的: `continuo trust` が列挙された2件だけを登録し、列挙していないものに触らないことを、
// 実際にコマンドを起動して確認する。あわせて、2回目の実行が何も書かないことを見る。
//
// 与える情報: dry-run と同じ環境。ghq には列挙していないリポジトリも置いてある。
// 成功条件: 終了コードが 0、列挙した2件だけが登録され、
// 列挙していないリポジトリの記述が作られないこと。2回目は何も書かないこと。
func TestCLI_列挙した2件だけを登録し2回目は何も書かない(t *testing.T) {
	requireCommands(t, "ghq", "git")

	env := setUpCLI(t)
	stdout, code := runContinuo(t, env, "trust")

	if code != 0 {
		t.Fatalf("登録できたのに終了コードが 0 でない: %d\n%s", code, stdout)
	}
	after := readFile(t, env.configPath)
	for _, repo := range []string{"demo-a", "demo-b"} {
		key := trustKeyOf(t, filepath.Join(env.ghqRoot, "github.com", "maimuzo", repo))
		entry, ok := projectEntry(t, after, key)
		if !ok || entry["hasTrustDialogAccepted"] != true {
			t.Errorf("%s が登録されていない:\n%s", repo, after)
		}
	}
	if strings.Contains(after, "demo-unlisted") {
		t.Errorf("列挙していないリポジトリまで登録されている:\n%s", after)
	}
	if names := backupNames(t, env.home); len(names) != 1 {
		t.Errorf("バックアップが1つ残っていない: %v", names)
	}

	// 2回目。既に true なので何も書かない。
	stdout2, code2 := runContinuo(t, env, "trust")
	if code2 != 0 {
		t.Errorf("2回目の終了コードが 0 でない: %d\n%s", code2, stdout2)
	}
	if got := readFile(t, env.configPath); got != after {
		t.Errorf("2回目でファイルが変わっている\n1回目:\n%s\n2回目:\n%s", after, got)
	}
	if names := backupNames(t, env.home); len(names) != 1 {
		t.Errorf("2回目でバックアップが増えている: %v", names)
	}
}

// cliEnv は CLI のテストで使う偽の環境である。
type cliEnv struct {
	// bin はビルドした continuo の絶対パス。
	bin string
	// home はテスト用ホームディレクトリ。
	home string
	// ghqRoot はテスト用ghq mock の置き場所。
	ghqRoot string
	// workDir は WORKFLOW.md を置いた作業ディレクトリ。
	workDir string
	// configPath は偽の `~/.claude.json` の絶対パス。
	configPath string
	// before は書き換える前の `~/.claude.json` の中身。
	before string
}

// setUpCLI は CLI のテスト用に、偽のホーム・テスト用ghq mock・WORKFLOW.md を組み立てる。
//
// t: テストコンテキスト。
// 戻り値: 組み立てた環境。
func setUpCLI(t *testing.T) cliEnv {
	t.Helper()

	root := t.TempDir()
	env := cliEnv{
		bin:     buildBinary(t, root),
		home:    filepath.Join(root, "home"),
		ghqRoot: filepath.Join(root, "ghq"),
		workDir: filepath.Join(root, "work"),
	}
	for _, d := range []string{env.home, env.workDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("%s を作れなかった: %v", d, err)
		}
	}

	// 対象になる2つと、列挙しない1つ。
	for _, name := range []string{"demo-a", "demo-b", "demo-unlisted"} {
		dir := filepath.Join(env.ghqRoot, "github.com", "maimuzo", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("%s を作れなかった: %v", dir, err)
		}
		cmd := exec.Command("git", "init", "--quiet")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init に失敗した: %v: %s", err, out)
		}
	}
	writeJSON(t, filepath.Join(env.ghqRoot, "github.com", "maimuzo", "demo-a", ".claude", "settings.json"),
		`{"permissions":{"allow":["Bash(rm -rf:*)","Read"],"additionalDirectories":["/etc"]}}`)
	writeJSON(t, filepath.Join(env.ghqRoot, "github.com", "maimuzo", "demo-a", ".mcp.json"),
		`{"mcpServers":{"payments":{"command":"node","args":["server.js"]},"docs":{"url":"https://example.invalid/mcp"}}}`)

	env.before = `{
  "numStartups": 41,
  "projects": {
    "/somewhere/else": {"hasTrustDialogAccepted": true}
  }
}
`
	env.configPath = filepath.Join(env.home, claudeConfigName)
	if err := os.WriteFile(env.configPath, []byte(env.before), 0o600); err != nil {
		t.Fatalf("偽の %s を置けなかった: %v", claudeConfigName, err)
	}

	writeWorkflow(t, filepath.Join(env.workDir, "WORKFLOW.md"))
	return env
}

// runContinuo は組み立てた環境で continuo を1回起動する。
//
// **HOME と GHQ_ROOT だけを差し替える。**実物のホームを見に行かせない。
//
// t: テストコンテキスト。
// env: 組み立てた環境。
// args: continuo に渡す引数。
// 戻り値の1つ目: 標準出力と標準エラー出力を合わせたもの。
// 戻り値の2つ目: 終了コード。
func runContinuo(t *testing.T, env cliEnv, args ...string) (string, int) {
	t.Helper()

	cmd := exec.Command(env.bin, args...)
	cmd.Dir = env.workDir
	cmd.Env = append(os.Environ(), "HOME="+env.home, "GHQ_ROOT="+env.ghqRoot)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !asExitError(err, &exitErr) {
			t.Fatalf("continuo を起動できなかった: %v\n%s", err, out)
		}
		code = exitErr.ExitCode()
	}
	return string(out), code
}
