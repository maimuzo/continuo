package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// ===== 試用の一式（mock5つと本物の git） =====

// e2eEnv は docs/trying_it_out.md の段1から段9までを通すための一式である。
//
// **すべて一時ディレクトリの中で完結する。**実物のホームディレクトリ・実物の ghq の
// 置き場所・本番のボード・実 herdr・実物の Claude Code のいずれにも触らない。
type e2eEnv struct {
	// Root は一時ディレクトリの根である（socket のパスを短く保つため浅くする）。
	Root string
	// Home は子プロセスへ渡す HOME である（`.claude.json` と `.claude/` を置く）。
	Home string
	// BinDir はテスト用gh / ghq mock を置いたディレクトリである（PATH の先頭に入れる）。
	BinDir string
	// TryDir は `WORKFLOW.md` を置いて continuo を動かす作業ディレクトリである
	// （手順書の `~/continuo-try` にあたる）。
	TryDir string
	// WorkflowPath は `WORKFLOW.md` の絶対パスである。
	WorkflowPath string
	// RuntimeDir は実行時ディレクトリである（hook の socket・逃がし先・ロックの置き場所）。
	RuntimeDir string
	// WorktreeRoot は worktree の置き場所である（`workspace.root`）。
	WorktreeRoot string
	// OriginDir は本物の git の bare リポジトリ（push 先）である。
	OriginDir string
	// RepoDir は本物の git の clone である（テスト用ghq mock がこのパスを返す）。
	RepoDir string
	// Owner はボードの所有者名である。
	Owner string
	// Repo はリポジトリ名である（`<Owner>/<Repo>` で使う）。
	Repo string
	// Binary はビルドした continuo の絶対パスである。
	Binary string
	// Board は偽のボードの持ち手である。
	Board *board
	// GitHub はテスト用GitHub GraphQL mock サーバである。
	GitHub *fakeGitHub
	// Herdr はテスト用herdr mock の socket サーバである。
	Herdr *fakeHerdr
	// Claude はテスト用Claude Code mock である。
	Claude *fakeClaude
}

// FullName は `<owner>/<repo>` を返す。
func (e *e2eEnv) FullName() string {
	return e.Owner + "/" + e.Repo
}

// newE2EEnv はmock5つ（gh / GraphQL / herdr / ホームディレクトリ / Claude Code）と
// 本物の git を用意し、continuo をビルドする（手順書の段1 にあたる）。
//
// t: 呼び出し元のテスト。後始末を t.Cleanup に登録する。
// 戻り値: 段2 以降を叩くための一式。
func newE2EEnv(t *testing.T) *e2eEnv {
	t.Helper()

	// **socket のパスを短く保つ**（macOS の Unix domain socket の上限は103バイト）。
	root, err := os.MkdirTemp("", "ce2e")
	if err != nil {
		t.Fatalf("一時ディレクトリを作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("一時ディレクトリを解決できません: %v", err)
	}

	env := &e2eEnv{
		Root:         root,
		Home:         filepath.Join(root, "home"),
		BinDir:       filepath.Join(root, "bin"),
		TryDir:       filepath.Join(root, "try"),
		WorkflowPath: filepath.Join(root, "try", "WORKFLOW.md"),
		RuntimeDir:   filepath.Join(root, "rt"),
		WorktreeRoot: filepath.Join(root, "wt"),
		OriginDir:    filepath.Join(root, "origin.git"),
		RepoDir:      filepath.Join(root, "repo"),
		Owner:        "octofake",
		Repo:         "sandbox",
	}
	for _, dir := range []string{env.Home, env.BinDir, env.TryDir, env.RuntimeDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("%s を作れません: %v", dir, err)
		}
	}

	env.prepareHome(t)
	env.prepareGitRepos(t)

	// テスト用gh mock とテスト用ghq mock を PATH の先頭へ置く。
	buildFakeGH(t, filepath.Join(root, "ghsrc"), env.BinDir)
	writeFakeGhq(t, env.BinDir, env.FullName(), env.RepoDir)
	writeFakeClaudeBinary(t, env.BinDir)

	// 偽のボードと、それを読み書きするテスト用GraphQL mock。
	env.Board = newBoardFile(t, boardPathIn(root), env.Owner, env.Repo)
	env.GitHub = newFakeGitHub(t, env.Board.Path)

	// テスト用herdr mock と、その上で `agent.prompt` を受けたときに動くテスト用Claude Code mock。
	env.Herdr = newFakeHerdr(t, root)
	env.Herdr.SetRepoDir(env.RepoDir)
	env.Claude = newFakeClaude(t, env.Home, env.BinDir, env.Board.Path)
	env.Herdr.SetOnPrompt(env.Claude.Act)

	env.Binary = buildContinuo(t, root)
	return env
}

// prepareHome は隔離したホームディレクトリを作る。
//
// **実物の `~/.claude.json` は読みも書きもしない。**`projects` が空の JSON を置き、
// `continuo trust` がここへ書き込む形にする。**`.claude/` も作る**
// （transcript の置き場所の根であり、`continuo doctor` が資格情報を探す場所でもある）。
//
// t: 呼び出し元のテスト。
func (e *e2eEnv) prepareHome(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(e.Home, ".claude", "projects"), 0o700); err != nil {
		t.Fatalf("テスト用ホームディレクトリの .claude を作れません: %v", err)
	}
	doc := map[string]any{"projects": map[string]any{}}
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("偽の ~/.claude.json を JSON 化できません: %v", err)
	}
	if err := os.WriteFile(filepath.Join(e.Home, ".claude.json"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatalf("偽の ~/.claude.json を書けません: %v", err)
	}
}

// prepareGitRepos は本物の git の bare リポジトリと clone を作る。
//
// **git だけは本物を使う。**worktree の作成・削除・push の有無の判定はmockでは確かめられない。
//
// t: 呼び出し元のテスト。
func (e *e2eEnv) prepareGitRepos(t *testing.T) {
	t.Helper()
	e.RunGit(t, "", "init", "--quiet", "--bare", "--initial-branch=main", e.OriginDir)
	e.RunGit(t, "", "clone", "--quiet", e.OriginDir, e.RepoDir)
	e.RunGit(t, e.RepoDir, "config", "user.email", "continuo@example.test")
	e.RunGit(t, e.RepoDir, "config", "user.name", "continuo e2e")
	e.RunGit(t, e.RepoDir, "config", "commit.gpgsign", "false")
	readme := filepath.Join(e.RepoDir, "README.md")
	if err := os.WriteFile(readme, []byte("# sandbox\n\n最初の中身。\n"), 0o600); err != nil {
		t.Fatalf("初期ファイルを書けません: %v", err)
	}
	e.RunGit(t, e.RepoDir, "add", ".")
	e.RunGit(t, e.RepoDir, "commit", "--quiet", "-m", "初期コミット")
	e.RunGit(t, e.RepoDir, "push", "--quiet", "-u", "origin", "main")
}

// RunGit はテストの中で git を実行し、標準出力を返す。
//
// **HOME はテスト用ホームディレクトリにする。**実物の `~/.gitconfig` を読ませない。
//
// t: 呼び出し元のテスト。失敗したらテストを止める。
// dir: `-C` に渡す作業ディレクトリ。空文字なら付けない。
// args: git の引数。
// 戻り値: 標準出力（前後の空白を落としたもの）。
func (e *e2eEnv) RunGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", full...)
	cmd.Env = e.ChildEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("`git %s` に失敗しました: %v\n%s", strings.Join(full, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// writeFakeClaudeBinary は、PATH に置くだけの偽の `claude` を作る。
//
// **`continuo doctor` は `claude` が PATH にあるかを調べる**（設計 6-2）。
// **mock だけで通すはずの E2E が、本物の PATH を見ていた。**
// 開発者の手元には claude があるので通り、**CI には無いので落ちた**（2026-08-23 に実測）。
//
// **中身は何でもよい。**doctor は `exec.LookPath` で在ることだけを見て、実行はしない。
//
// binDir: PATH の先頭に入るディレクトリ。
func writeFakeClaudeBinary(t *testing.T, binDir string) {
	t.Helper()
	p := filepath.Join(binDir, "claude")
	body := "#!/bin/sh\n# doctor は在ることだけを見る。実行はされない。\necho 'fake claude'\n"
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("テスト用の claude を置けません: %v", err)
	}
}

// ChildEnv は continuo とテスト用gh mock へ渡す環境変数を組み立てる。
//
// **環境変数は明示的に組み立てる。**実物の `HERDR_SOCKET_PATH` や `GH_TOKEN` を
// 継承させないためである。
//
// **偽のボードとテスト用GraphQL mockがまだ無い時期にも呼べる**
// （newE2EEnv が git のリポジトリを作るときに使う）。その場合は空文字を渡す。
//
// 戻り値: 環境変数の並び。
func (e *e2eEnv) ChildEnv() []string {
	graphQL := ""
	if e.GitHub != nil {
		graphQL = e.GitHub.URL
	}
	boardPath := ""
	if e.Board != nil {
		boardPath = e.Board.Path
	}
	return []string{
		"PATH=" + e.BinDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + e.Home,
		"LANG=ja_JP.UTF-8",
		"CONTINUO_RUNTIME_DIR=" + e.RuntimeDir,
		"CONTINUO_GITHUB_GRAPHQL_ENDPOINT=" + graphQL,
		"CONTINUO_E2E_BOARD=" + boardPath,
		"GIT_TERMINAL_PROMPT=0",
	}
}

// ===== continuo の起動 =====

// runResult は continuo を1回起動した結果である。
type runResult struct {
	// Code は終了コードである。
	Code int
	// Out は標準出力と標準エラーを混ぜたものである。
	Out string
}

// Run は continuo のサブコマンドを1回実行して終わるまで待つ。
//
// t: 呼び出し元のテスト。
// dir: 実行する作業ディレクトリ（手順書の「実行する場所」）。
// stdin: 標準入力へ流す文字列（`continuo setup` の番号など）。
// args: continuo に渡す引数。
// 戻り値: 終了コードと出力。
func (e *e2eEnv) Run(t *testing.T, dir, stdin string, args ...string) runResult {
	t.Helper()
	cmd := exec.Command(e.Binary, args...)
	cmd.Dir = dir
	cmd.Env = e.ChildEnv()
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	code := 0
	var exitErr *exec.ExitError
	if err != nil {
		if ok := asExitError(err, &exitErr); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("`continuo %s` を実行できません: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return runResult{Code: code, Out: string(out)}
}

// Start は continuo を常駐プロセスとして起動する（手順書の段8）。
//
// t: 呼び出し元のテスト。
// args: continuo に渡す引数（`--log-level=debug` と WORKFLOW.md のパスは自動で足す）。
// 戻り値の1つ目: 起動したプロセス。
// 戻り値の2つ目: 標準出力と標準エラーを溜める先（失敗したときに中身を出す）。
func (e *e2eEnv) Start(t *testing.T, args ...string) (*exec.Cmd, *syncBuffer) {
	t.Helper()
	logs := &syncBuffer{}
	full := append(append([]string{}, args...), "--log-level=debug", e.WorkflowPath)
	cmd := exec.Command(e.Binary, full...)
	cmd.Dir = e.TryDir
	cmd.Env = e.ChildEnv()
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("continuo を起動できません: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	return cmd, logs
}

// ===== WORKFLOW.md の書き換え =====

// PatchWorkflow は `WORKFLOW.md` の front matter の行を差し替える。
//
// **手順書の「手で書き換えることもできる」にあたる。**試用の一式はテスト用herdr mock と
// 一時ディレクトリを向いている必要があるので、雛形の既定値のままでは動かせない。
//
// **1行も見つからない差し替えは失敗にする。**雛形が変わったのに気づかずに、
// 効いていない設定でテストが通り続けるのを防ぐ。
//
// t: 呼び出し元のテスト。
// replacements: 行の先頭一致（キー）と、置き換える行の全体（値）。
func (e *e2eEnv) PatchWorkflow(t *testing.T, replacements [][2]string) {
	t.Helper()
	raw, err := os.ReadFile(e.WorkflowPath)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	for _, rep := range replacements {
		prefix, replacement := rep[0], rep[1]
		hit := 0
		for i, line := range lines {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			hit++
			lines[i] = replacement
		}
		if hit != 1 {
			t.Fatalf("WORKFLOW.md の %q で始まる行が %d 行あります（1行であるべきです）", prefix, hit)
		}
	}
	if err := os.WriteFile(e.WorkflowPath, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
}

// TestSettings は試用の一式を向くように `WORKFLOW.md` を書き換える。
//
// **待ち時間だけを短くしてある**（判定の意味は変えない）。
//
// t: 呼び出し元のテスト。
func (e *e2eEnv) TestSettings(t *testing.T) {
	t.Helper()
	e.PatchWorkflow(t, [][2]string{
		{"  socket: ", "  socket: " + e.Herdr.SocketPath},
		{"  root: ", "  root: " + e.WorktreeRoot},
		{"  interval_ms: ", "  interval_ms: 500"},
		{"  poll_wait_ms: ", "  poll_wait_ms: 300"},
		{"  settle_ms: ", "  settle_ms: 200"},
		{"  turn_timeout_ms: ", "  turn_timeout_ms: 120000"},
		{"  read_timeout_ms: ", "  read_timeout_ms: 5000"},
		{"  startup_timeout_ms: ", "  startup_timeout_ms: 10000"},
		// 枠は読まない（実物の資格情報を読ませない。判定の対象でもない）。
		{"  source: ", "  source: none"},
	})
}

// ===== 補助 =====

// goBinary は go コマンドの絶対パスを返す。
//
// t: 呼び出し元のテスト。
// 戻り値: go の絶対パス。
func goBinary(t *testing.T) string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	return goBin
}

// buildContinuo は continuo をビルドする（手順書の段1）。
//
// **リポジトリの中には出力しない**（生成物を残さないため、テストの一時ディレクトリへ出す）。
//
// t: 呼び出し元のテスト。
// outDir: 出力先のディレクトリ。
// 戻り値: ビルドしたバイナリの絶対パス。
func buildContinuo(t *testing.T, outDir string) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("リポジトリの場所を決められません: %v", err)
	}
	bin := filepath.Join(outDir, "continuo")
	cmd := exec.Command(goBinary(t), "build", "-o", bin, "./cmd/continuo")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("continuo をビルドできません: %v\n%s", err, out)
	}
	return bin
}

// waitFor は cond が真になるまで最大 d だけ待つ。
//
// t: 呼び出し元のテスト。**テスト本体の goroutine から呼ぶこと。**
// d: 待つ長さの上限。
// message: 失敗したときに出す説明。
// cond: 判定する関数。
func waitFor(t *testing.T, d time.Duration, message string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s（%s 以内に条件を満たしませんでした）", message, d)
}

// waitProcess はプロセスの終了を最大 d だけ待つ。
//
// ctx: 待ちに適用するコンテキスト。
// cmd: 待つプロセス。
// d: 待つ長さの上限。
// 戻り値の1つ目: 終了コード。
// 戻り値の2つ目: 期限までに終わったかどうか。
func waitProcess(ctx context.Context, cmd *exec.Cmd, d time.Duration) (int, bool) {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return 0, true
		}
		var exitErr *exec.ExitError
		if ok := asExitError(err, &exitErr); ok {
			return exitErr.ExitCode(), true
		}
		return -1, true
	case <-time.After(d):
		return -1, false
	case <-ctx.Done():
		return -1, false
	}
}

// asExitError は err が *exec.ExitError かを判定する。
//
// err: 判定するエラー。
// target: 一致したときに代入する先。
// 戻り値: 一致すれば true。
func asExitError(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

// syncBuffer は子プロセスの出力を溜める。
//
// **`exec.Cmd` は標準出力と標準エラーを別々の goroutine から書く**ので、
// 排他を持たない strings.Builder をそのまま渡すと競合する。
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

// Write は io.Writer の実装である。
//
// p: 書き込むバイト列。
// 戻り値: 書き込んだバイト数と、常に nil のエラー。
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String は溜まった出力を返す。
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// mustContain は出力に語が含まれていることを確かめる。
//
// t: 呼び出し元のテスト。
// label: 失敗したときに出す見出し（どの段の何か）。
// out: 調べる出力。
// want: 含まれているべき語。
func mustContain(t *testing.T, label, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Fatalf("%s の出力に %q がありません:\n%s", label, w, out)
		}
	}
}

// branchName は issue 番号に対応する branch 名を返す（`branch_template` の既定に合わせる）。
//
// owner: リポジトリの所有者名。
// repo: リポジトリ名。
// number: issue 番号。
// 戻り値: branch 名。
func branchName(owner, repo string, number int) string {
	return fmt.Sprintf("continuo/%s/%s/%d", owner, repo, number)
}
