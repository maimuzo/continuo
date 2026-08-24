package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// setupAnswers は `continuo setup` の対話へ流す番号である。
//
// 偽のボードの Status の選択肢は `Ice Box, Ready, In Progress, Blocked, In Review, Done`
// の6つなので、手順書の段4 と同じ答えになる。
//
//	[1/5] 着手待ち    → 2（Ready）
//	[2/5] 作業中      → 3（In Progress）
//	[3/5] レビュー待ち → 5（In Review）
//	[4/5] 保留        → 4（Blocked）
//	[5/5] 完了        → 6（Done）
const setupAnswers = "2\n3\n5\n4\n6\n"

// TestE2E_手順書の段1から段9までをmockだけで通す は、docs/trying_it_out.md の全段を
// **被害ゼロで**最初から最後まで通す。
//
// 目的:
//   - **段7〜段9（issue を実際に処理する部分）を初めて通す。**
//     ここは実物を使うと枠を消費し、リポジトリが変更され、ボードが書き換わるため、
//     これまで一度も動かせていなかった
//   - mockどうし（テスト用gh mock・テスト用GraphQL mock・テスト用herdr mock・隔離したホームディレクトリ・
//     テスト用Claude Code mock）が繋がって、1件の issue が `Ready` から `Done` まで通ること
//
// 与える情報:
//   - 既に issue が1件（`Ice Box`）載っている偽のボード
//   - 本物の git の bare リポジトリと clone（worktree の作成・削除・push の判定に要る）
//   - `agent.prompt` を受けたら、テスト用Claude Code mock が commit して push し、
//     issue にコメントを書き、transcript に `CONTINUO-STATUS: review` を書いて、
//     **`continuo hook` で Stop を送る**
//
// 成功条件:
//   - 段1〜段6 が手順書どおりの終了コードと出力になる
//   - 段8 で Status が `Ready` → `In Progress` → `In Review` と動く
//   - worktree が本物の git で作られ、テスト用Claude Code mock の commit が push される
//   - 人間が `Done` へ動かすと worktree と branch が消える
//   - 段9 の `SIGINT`（`Ctrl+C` 相当）で終了コード 0 で終わる
func TestE2E_手順書の段1から段9までをmockだけで通す(t *testing.T) {
	env := newE2EEnv(t)

	// ===== 段1. ビルドする =====
	// newE2EEnv がビルド済みである。動くことだけを確かめる。
	if res := env.Run(t, env.Root, "", "init", "--help"); res.Code != 0 {
		t.Fatalf("段1: `continuo init --help` の終了コードが 0 ではありません: %d\n%s", res.Code, res.Out)
	}

	// ===== 段2. 使うボードを確かめる（作らない） =====
	stage2ReadBoard(t, env)

	// ===== 段3. 設定を置く =====
	stage3Init(t, env)

	// ===== 段4. Status の割り当てを合わせる =====
	stage4Setup(t, env)

	// **試用の一式（テスト用herdr mock と一時ディレクトリ）を向くように書き換える。**
	// 手順書の「手で書き換えることもできる」にあたる。
	env.TestSettings(t)

	// ===== 段5. clone して信頼を登録する =====
	stage5Trust(t, env)

	// ===== 段6. 前提が揃っているかを検査する =====
	stage6Doctor(t, env)

	// ===== 段7. issue を1件用意する =====
	issueURL := stage7PrepareIssue(t, env)

	// ===== 段8. 動かす =====
	cmd, logs := env.Start(t)
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("continuo の出力:\n%s", logs.String())
		}
	})
	worktreePath := stage8Run(t, env, issueURL, logs)

	// ===== 段9. 止める・片付ける =====
	stage9Stop(t, env, cmd, logs, worktreePath)
}

// stage2ReadBoard は段2（使うボードを確かめる）を叩く。
//
// **テスト用gh mock を直接叩く。**手順書がここで案内しているのは continuo ではなく gh である。
//
// t: 呼び出し元のテスト。
// env: 試用の一式。
func stage2ReadBoard(t *testing.T, env *e2eEnv) {
	t.Helper()
	gh := filepath.Join(env.BinDir, "gh")

	out := runTool(t, env, gh, "project", "list", "--owner", env.Owner)
	mustContain(t, "段2 の `gh project list`", out, "continuo 試用ボード", "open")

	out = runTool(t, env, gh, "project", "field-list", "7", "--owner", env.Owner, "--format", "json")
	mustContain(t, "段2 の `gh project field-list`", out,
		"ProjectV2SingleSelectField", "Ready", "In Progress", "In Review", "Blocked", "Done")
}

// stage3Init は段3（設定を置く）を叩く。
//
// t: 呼び出し元のテスト。
// env: 試用の一式。
func stage3Init(t *testing.T, env *e2eEnv) {
	t.Helper()
	res := env.Run(t, env.TryDir, "", "init")
	if res.Code != 0 {
		t.Fatalf("段3: `continuo init` の終了コードが 0 ではありません: %d\n%s", res.Code, res.Out)
	}
	mustContain(t, "段3 の `continuo init`", res.Out,
		"tracker.provider.owner: "+env.Owner,
		"tracker.provider.project_number: 7",
		"trust.repositories: "+env.FullName())

	// **`gh` から引いた値が雛形へ入っている。**手で埋めさせない（設計 3-32）。
	raw, err := os.ReadFile(env.WorkflowPath)
	if err != nil {
		t.Fatalf("段3: WORKFLOW.md を読めません: %v", err)
	}
	mustContain(t, "段3 の WORKFLOW.md", string(raw),
		"owner: "+env.Owner, "project_number: 7", env.FullName())
	if strings.Contains(string(raw), "__FILL_ME__") {
		t.Fatalf("段3: WORKFLOW.md にプレースホルダが残っています:\n%s", raw)
	}
}

// stage4Setup は段4（Status の割り当てを合わせる）を叩く。
//
// **`continuo setup` は段3 で作った WORKFLOW.md の Status の行だけを書き換える。**
// 雛形は作らないので、段3 より先に叩くことはできない。
//
// t: 呼び出し元のテスト。
// env: 試用の一式。
func stage4Setup(t *testing.T, env *e2eEnv) {
	t.Helper()
	res := env.Run(t, env.TryDir, setupAnswers, "setup")
	if res.Code != 0 {
		t.Fatalf("段4: `continuo setup` の終了コードが 0 ではありません: %d\n%s", res.Code, res.Out)
	}
	mustContain(t, "段4 の `continuo setup`", res.Out,
		"Ice Box", "Ready", "In Progress", "Blocked", "In Review", "Done")

	raw, err := os.ReadFile(env.WorkflowPath)
	if err != nil {
		t.Fatalf("段4: WORKFLOW.md を読めません: %v", err)
	}
	for _, want := range []string{
		`dispatch_state: "Ready"`,
		`running_state: "In Progress"`,
		`failure_state: "Blocked"`,
		`terminal_states: ["Done"]`,
		`review: "In Review"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("段4: WORKFLOW.md に %q がありません:\n%s", want, raw)
		}
	}
}

// stage5Trust は段5（clone して信頼を登録する）を叩く。
//
// **書き込む先は隔離したホームディレクトリの `~/.claude.json` である。**
// 実物のホームディレクトリは読みも書きもしない。
//
// t: 呼び出し元のテスト。
// env: 試用の一式。
func stage5Trust(t *testing.T, env *e2eEnv) {
	t.Helper()
	// 何を許すことになるかを先に見る。**未信頼が残っているので終了コードは 1 である。**
	res := env.Run(t, env.TryDir, "", "trust", "--dry-run")
	if res.Code != 1 {
		t.Fatalf("段5: 未信頼のリポジトリがあるのに `continuo trust --dry-run` が %d で終わりました\n%s",
			res.Code, res.Out)
	}
	mustContain(t, "段5 の `continuo trust --dry-run`", res.Out, env.FullName(), "--dry-run")

	before := readFileString(t, filepath.Join(env.Home, ".claude.json"))
	if strings.Contains(before, env.RepoDir) {
		t.Fatalf("段5: --dry-run の前から信頼が登録されています:\n%s", before)
	}

	// 登録する。
	res = env.Run(t, env.TryDir, "", "trust")
	if res.Code != 0 {
		t.Fatalf("段5: `continuo trust` の終了コードが 0 ではありません: %d\n%s", res.Code, res.Out)
	}
	after := readFileString(t, filepath.Join(env.Home, ".claude.json"))
	toplevel := env.RunGit(t, env.RepoDir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	if !strings.Contains(after, toplevel) {
		t.Fatalf("段5: ~/.claude.json に clone のパス（%s）が入っていません:\n%s", toplevel, after)
	}

	// もう一度叩いても何も起きない。
	res = env.Run(t, env.TryDir, "", "trust", "--dry-run")
	if res.Code != 0 {
		t.Fatalf("段5: 信頼済みなのに `continuo trust --dry-run` が %d で終わりました\n%s", res.Code, res.Out)
	}
}

// stage6Doctor は段6（前提が揃っているかを検査する）を叩く。
//
// **`✗` が1つも無いので終了コードは 0 である。**まだ `Ready` の issue が無いので、
// `clone` と `信頼登録` は「確かめられなかった（`!`）」になる。
//
// t: 呼び出し元のテスト。
// env: 試用の一式。
func stage6Doctor(t *testing.T, env *e2eEnv) {
	t.Helper()
	res := env.Run(t, env.TryDir, "", "doctor")
	if res.Code != 0 {
		t.Fatalf("段6: `continuo doctor` の終了コードが 0 ではありません: %d\n%s", res.Code, res.Out)
	}
	mustContain(t, "段6 の `continuo doctor`", res.Out,
		"✓ 設定ファイル", "✓ herdr", "✓ gh の認証", "✓ ボード")
	mustHaveNoFailure(t, "段6 の `continuo doctor`", res.Out)
}

// mustHaveNoFailure は doctor の出力に `✗` の項目が1つも無いことを確かめる。
//
// **末尾の集計行（`✗ 0件 / ! 2件`）を数えない。**行の先頭が `✗` の項目だけを見る。
//
// t: 呼び出し元のテスト。
// label: 失敗したときに出す見出し。
// out: doctor の出力。
func mustHaveNoFailure(t *testing.T, label, out string) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "✗") {
			t.Fatalf("%s に ✗ の項目があります:\n%s", label, out)
		}
	}
}

// stage7PrepareIssue は段7（issue を1件用意する）を叩く。
//
// **`Ready` にするのは人間の画面の作業なので、ここではボードを直接書き換える。**
// continuo は GraphQL 経由でしか書かないため、画面の操作を再現できるのはここだけである。
//
// t: 呼び出し元のテスト。
// env: 試用の一式。
// 戻り値: 用意した issue の URL。
func stage7PrepareIssue(t *testing.T, env *e2eEnv) string {
	t.Helper()
	gh := filepath.Join(env.BinDir, "gh")

	out := runTool(t, env, gh, "issue", "create", "--repo", env.FullName(),
		"--title", "README に使い方を1行足す",
		"--body", "README.md の先頭に、このリポジトリが何かを1行で書いてください。")
	issueURL := strings.TrimSpace(out)
	if !strings.HasPrefix(issueURL, "https://github.com/"+env.FullName()+"/issues/") {
		t.Fatalf("段7: `gh issue create` が issue の URL を返しませんでした: %q", out)
	}

	runTool(t, env, gh, "project", "item-add", "7", "--owner", env.Owner, "--url", issueURL)

	// **足した item はまだ Status が空である。**人間が画面で `Ready` にする。
	if got := env.Board.StateOfURL(t, issueURL); got != "" {
		t.Fatalf("段7: ボードへ足した直後の Status が空ではありません: %q", got)
	}
	env.Board.SetStateByURL(t, issueURL, "Ready")

	// 足した issue が `gh project item-list` にも出る（**テスト用gh mock が状態を持っている**）。
	list := runTool(t, env, gh, "project", "item-list", "7", "--owner", env.Owner, "--format", "json")
	mustContain(t, "段7 の `gh project item-list`", list, issueURL, `"status": "Ready"`)

	// ここでもう一度 doctor を叩くと、`clone` と `信頼登録` が `✓` になる。
	res := env.Run(t, env.TryDir, "", "doctor")
	if res.Code != 0 {
		t.Fatalf("段7: `continuo doctor` の終了コードが 0 ではありません: %d\n%s", res.Code, res.Out)
	}
	mustContain(t, "段7 の `continuo doctor`", res.Out, "✓ clone", "✓ 信頼登録")
	return issueURL
}

// stage8Run は段8（動かす）を確かめる。
//
// t: 呼び出し元のテスト。
// env: 試用の一式。
// issueURL: 段7 で用意した issue の URL。
// logs: continuo の出力。
// 戻り値: 作られた worktree の絶対パス。
func stage8Run(t *testing.T, env *e2eEnv, issueURL string, logs *syncBuffer) string {
	t.Helper()
	number := env.Board.Read(t).findIssueByURL(issueURL).Number
	branch := branchName(env.Owner, env.Repo, number)
	worktreePath := filepath.Join(env.WorktreeRoot, "github.com", env.Owner, env.Repo,
		strings.ReplaceAll(branch, "/", "-"))

	// 3: `Ready` の issue を取り、Status を `In Progress` へ書く。
	waitFor(t, 60*time.Second, "段8: Status が In Progress になる", func() bool {
		return env.Board.StateOfURL(t, issueURL) == "In Progress"
	})

	// 4: worktree を作り、pane で Claude Code を起動する。
	waitFor(t, 60*time.Second, "段8: worktree が作られる", func() bool {
		_, err := os.Stat(filepath.Join(worktreePath, ".continuo.json"))
		return err == nil
	})

	// **身元ファイルに base が入っていること。**空のまま書くと、continuo を再起動した
	// あとの片付けが「base が分からないので判定できない」で永久に見送る（設計 3-9）。
	// 同じプロセスの中では run の状態が base を持っているので、この検査でしか気づけない。
	assertIdentityHasBase(t, worktreePath)

	// 5〜6: エージェントが表明を出し、continuo が Status を `In Review` へ動かす。
	waitFor(t, 60*time.Second, "段8: Status が In Review になる", func() bool {
		return env.Board.StateOfURL(t, issueURL) == "In Review"
	})

	// **continuo が GraphQL で書いた Status が、テスト用gh mock からも見える。**
	// 別の端末から `gh project item-list` で様子を見る、という手順書の案内にあたる。
	list := runTool(t, env, filepath.Join(env.BinDir, "gh"),
		"project", "item-list", "7", "--owner", env.Owner, "--format", "json")
	mustContain(t, "段8 の `gh project item-list`", list, `"status": "In Review"`)

	// **turn の終わりは `continuo hook` が届けている。**設定ファイルに書かれた
	// コマンド行をそのまま実行した記録を見る（socket へ直接書いてはいない）。
	hooks := env.Claude.HookCommands()
	if len(hooks) == 0 {
		t.Fatalf("段8: テスト用Claude Code mock が hook を1回も実行していません")
	}
	mustContain(t, "段8 の hook のコマンド行", hooks[0], "hook --socket", "--pending-dir")

	// **テスト用Claude Code mock の成果が本物の git に残っている。**
	if n := env.Claude.Commits(); n == 0 {
		t.Fatalf("段8: テスト用Claude Code mock が commit を1つも作っていません")
	}
	pushed := env.RunGit(t, env.RepoDir, "ls-remote", "--heads", "origin", branch)
	if !strings.Contains(pushed, branch) {
		t.Fatalf("段8: branch %s が push されていません: %q", branch, pushed)
	}

	// **エージェントのコメントが issue に残っている**（marker 付き。代筆に入っていない）。
	nodeID := env.Board.Read(t).findIssueByURL(issueURL).NodeID
	bodies := env.Board.CommentBodies(t, nodeID)
	found := false
	for _, b := range bodies {
		if strings.Contains(b, "<!-- continuo:agent -->") {
			found = true
		}
	}
	if !found {
		t.Fatalf("段8: エージェントのコメントが issue にありません: %v", bodies)
	}

	// **mockどうしが繋がっている証拠を確かめる。**
	if env.Herdr.CountMethod("worktree.open") == 0 {
		t.Fatalf("段8: テスト用herdr mock が worktree.open を受けていません: %v", env.Herdr.Methods())
	}
	if env.Herdr.CountMethod("agent.start") == 0 {
		t.Fatalf("段8: テスト用herdr mock が agent.start を受けていません: %v", env.Herdr.Methods())
	}
	// **起動を待つ経路を、実際に通ったことを確かめる。**
	//
	// テスト用herdr mock は最初の `agent.get` に `unknown` / `interactive_ready: false` を返す
	// （本物と同じ並び。設計 6-8）。**continuo が待たずに進む実装だと、ここで
	// `agent.prompt` が `agent_not_ready` で弾かれて上の検査が落ちる。**
	// それとは別に、**2回以上尋ねたこと自体**をここで確かめる。
	// 1回で通っていたら、待つ経路が消えている。
	if n := env.Herdr.CountMethod("agent.get"); n < 2 {
		t.Fatalf("段8: agent.get が %d 回しか呼ばれていません。起動を待つ経路を通っていません", n)
	}
	if prompts := env.Herdr.Prompts(); len(prompts) == 0 {
		t.Fatalf("段8: テスト用herdr mock が agent.prompt を受けていません")
	} else if !strings.Contains(prompts[0], fmt.Sprintf("%s#%d", env.FullName(), number)) {
		t.Fatalf("段8: 1回目の本文に issue の識別子がありません: %q", prompts[0])
	}
	if n := env.GitHub.Queries.Count("update_status"); n == 0 {
		t.Fatalf("段8: テスト用GraphQL mockが Status の書き込みを受けていません: %v",
			env.GitHub.Queries.Entries())
	}

	// 7: 人間が `Done` へ動かす。8: continuo が worktree と branch を片付ける。
	env.Board.SetStateByURL(t, issueURL, "Done")
	waitFor(t, 60*time.Second, "段8: worktree が片付く", func() bool {
		_, err := os.Stat(worktreePath)
		return os.IsNotExist(err)
	})
	waitFor(t, 30*time.Second, "段8: branch が消える", func() bool {
		refs := env.RunGit(t, env.RepoDir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
		return !strings.Contains(refs, branch)
	})

	// **push した branch は origin に残る。**消えるのは手元の branch だけである
	// （`cleanup.require_pushed` が push を確かめてから消しているので、
	// 成果は GitHub 側に残る）。手順書の段9 の片付けの表がこれを載せている。
	remote := env.RunGit(t, env.RepoDir, "ls-remote", "--heads", "origin", branch)
	if !strings.Contains(remote, branch) {
		t.Fatalf("段8: 片付けで origin の branch まで消えています: %q", remote)
	}

	_ = logs
	return worktreePath
}

// stage9Stop は段9（止める・片付ける）を確かめる。
//
// **手順書が案内しているのは `Ctrl+C` なので、`SIGINT` を送る。**
// `SIGTERM` も同じ経路で受ける（cmd/continuo/main.go の signal.NotifyContext）。
//
// t: 呼び出し元のテスト。
// env: 試用の一式。
// cmd: 走っている continuo。
// logs: continuo の出力。
// worktreePath: 段8 で片付いた worktree のパス。
func stage9Stop(t *testing.T, env *e2eEnv, cmd *exec.Cmd, logs *syncBuffer, worktreePath string) {
	t.Helper()
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("段9: SIGINT を送れません: %v", err)
	}
	code, finished := waitProcess(context.Background(), cmd, 30*time.Second)
	if !finished {
		t.Fatalf("段9: SIGINT を受けても 30 秒以内に終了しませんでした\n%s", logs.String())
	}
	if code != 0 {
		t.Fatalf("段9: 終了コードが 0 ではありません: %d\n%s", code, logs.String())
	}
	mustContain(t, "段9 の終了", logs.String(), "巡回を止めました")

	// 片付いたものが戻ってきていないことを確かめる。
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("段9: 片付けたはずの worktree が残っています: %s", worktreePath)
	}
	if panes := env.Herdr.LivePanes(); len(panes) != 0 {
		t.Fatalf("段9: pane が残っています: %v", panes)
	}

	// **テスト用gh mock が手順書のとおりに叩かれている。**
	calls := strings.Join(env.Board.GHCalls(t), "\n")
	mustContain(t, "段9 のテスト用gh mock の呼び出し記録", calls,
		"api user", "project list", "project field-list", "project item-list",
		"project item-add", "issue create", "issue view", "issue comment",
		"auth status", "auth token")
}

// runTool はテスト用gh mock のような外部コマンドを1回実行して標準出力を返す。
//
// t: 呼び出し元のテスト。
// env: 試用の一式（環境変数を借りる）。
// bin: 実行ファイルの絶対パス。
// args: 引数。
// 戻り値: 標準出力と標準エラーを混ぜたもの。
func runTool(t *testing.T, env *e2eEnv, bin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = env.Root
	cmd.Env = env.ChildEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("`%s %s` に失敗しました: %v\n%s", filepath.Base(bin), strings.Join(args, " "), err, out)
	}
	return string(out)
}

// readFileString はファイルの中身を文字列で読む。
//
// t: 呼び出し元のテスト。
// path: 読むファイルの絶対パス。
// 戻り値: 中身。
func readFileString(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s を読めません: %v", path, err)
	}
	return string(raw)
}

// TestE2E_status_fieldの綴りが違うとボードを読めない は、手順書の段2 と段6 が載せている
// **`✗ ボード` の出力**を、テスト用GitHub GraphQL mock サーバに対して実際に出す。
//
// 目的: 「綴りが1文字でも違うとボードを読めない」という手順書の記述が、
// **文面まで含めて本当かどうか**を確かめる。以前は偽のサーバが `statusField` を
// 見ていなかったため、綴りを間違えても `✓ ボード` になっていた。
//
// 与える情報: 段3〜段5 まで通した `WORKFLOW.md` の `status_field` を、
// ボードに存在しない `continuo Status` へ書き換えた写し。
//
// 成功条件: `continuo doctor` の終了コードが 1 で、`✗ ボード` の行に
// GitHub が返す `NOT_FOUND` の文面が出ること。
func TestE2E_status_fieldの綴りが違うとボードを読めない(t *testing.T) {
	env := newE2EEnv(t)

	if res := env.Run(t, env.TryDir, "", "init"); res.Code != 0 {
		t.Fatalf("`continuo init` の終了コードが 0 ではありません: %d\n%s", res.Code, res.Out)
	}
	if res := env.Run(t, env.TryDir, setupAnswers, "setup"); res.Code != 0 {
		t.Fatalf("`continuo setup` の終了コードが 0 ではありません: %d\n%s", res.Code, res.Out)
	}
	env.TestSettings(t)

	// **書き換えた写しを別のディレクトリに置く。**本流の WORKFLOW.md は壊さない。
	dir := filepath.Join(env.Root, "badfield")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("badfield を作れません: %v", err)
	}
	raw, err := os.ReadFile(env.WorkflowPath)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	bad := strings.Replace(string(raw), "status_field: Status", `status_field: "continuo Status"`, 1)
	if bad == string(raw) {
		t.Fatalf("status_field の行を差し替えられませんでした:\n%s", raw)
	}
	if err := os.WriteFile(filepath.Join(dir, "WORKFLOW.md"), []byte(bad), 0o600); err != nil {
		t.Fatalf("badfield の WORKFLOW.md を書けません: %v", err)
	}

	res := env.Run(t, dir, "", "doctor")
	if res.Code != 1 {
		t.Fatalf("綴りが違うのに `continuo doctor` が %d で終わりました\n%s", res.Code, res.Out)
	}
	mustContain(t, "status_field の綴りが違うときの `continuo doctor`", res.Out,
		"✗ ボード",
		"Could not resolve to a Unions::ProjectV2FieldConfiguration with the name continuo Status",
		"→ WORKFLOW.md の tracker.provider（owner / project_number / status_field）を確認してください")
}

// assertIdentityHasBase は worktree の身元ファイルに base が書かれていることを確かめる。
//
// t: テスト。
// worktreePath: worktree の絶対パス。
func assertIdentityHasBase(t *testing.T, worktreePath string) {
	t.Helper()

	path := filepath.Join(worktreePath, ".continuo.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("段8: 身元ファイルを読めません: %s: %v", path, err)
	}

	var identity struct {
		Base string `json:"base"`
	}
	if err := json.Unmarshal(raw, &identity); err != nil {
		t.Fatalf("段8: 身元ファイルを解釈できません: %s: %v\n%s", path, err, raw)
	}
	if identity.Base == "" {
		t.Fatalf("段8: 身元ファイルの base が空です。再起動をまたぐと片付けが見送られます:\n%s", raw)
	}
}

// TestE2E_逃げ道の環境変数を渡さなくても起動して1件通る は、
// 本番の探索順（設計 3-23）で hook の socket の場所を決めさせる。
//
// **目的。**他の E2E テストは全部 `CONTINUO_RUNTIME_DIR` を渡している。
// **それは運用者の逃げ道であって、本番の経路ではない。**渡している限り、
// 探索順の2番目以降（XDG_RUNTIME_DIR / $TMPDIR / ~/.continuo/run）は**1度も走らない。**
// 実際、その隙間を実在しない `XDG_RUNTIME_DIR` が通り抜けて利用者に届いた（issue #9）。
//
// **与える情報。**`CONTINUO_RUNTIME_DIR` を渡さず、`TMPDIR` をテストの一時ディレクトリへ向ける
// （macOS の枝がここを使う。Linux は `$HOME/.continuo/run` に落ち、HOME もテスト用である）。
//
// **成功条件。**issue が1件 `In Review` まで進むこと。
// あわせて、決まった socket の場所がテストの一時ディレクトリの中にあること
// （**実機の `/run/user/<uid>` などを触っていないこと**）。
func TestE2E_逃げ道の環境変数を渡さなくても起動して1件通る(t *testing.T) {
	env := newE2EEnv(t)
	// **これが本題である。**逃げ道を渡さない。
	env.OmitRuntimeDirEnv = true

	stage2ReadBoard(t, env)
	stage3Init(t, env)
	stage4Setup(t, env)
	env.TestSettings(t)
	stage5Trust(t, env)

	issueURL := stage7PrepareIssue(t, env)

	cmd, logs := env.Start(t)
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("continuo の出力:\n%s", logs.String())
		}
	})

	// **本番の探索順で決めた場所に、実際に socket を立てられたこと。**
	waitFor(t, 30*time.Second, "hook の socket を listen する", func() bool {
		return strings.Contains(logs.String(), "hook を受ける socket の listen を始めました")
	})
	sock := socketFromLogs(t, logs.String())
	if !strings.HasPrefix(sock, env.Root) {
		t.Fatalf("テストの一時ディレクトリの外に socket を作りました（実機を触っています）: %s\n  一時ディレクトリ: %s",
			sock, env.Root)
	}
	if strings.Contains(sock, env.RuntimeDir) {
		t.Fatalf("逃げ道を渡していないのに、逃げ道の場所を使っています: %s", sock)
	}

	// **issue が1件進むこと。**探索順で決めた socket で hook を受け取れなければ、
	// turn の終わりが分からず `In Review` まで進まない。
	number := env.Board.Read(t).findIssueByURL(issueURL).Number
	waitFor(t, 90*time.Second, "issue が In Review になる", func() bool {
		return env.Board.Read(t).findIssueByURL(issueURL).State == "In Review"
	})
	if n := env.Herdr.CountMethod("agent.start") - 0; n == 0 {
		t.Fatalf("テスト用herdr mock が agent.start を受けていません（issue %d）", number)
	}

	// 止める。**探索順で決めた socket を、終了時に片付けられること。**
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("SIGINT を送れません: %v", err)
	}
	code, finished := waitProcess(context.Background(), cmd, 30*time.Second)
	if !finished {
		t.Fatalf("SIGINT を受けても 30 秒以内に終了しませんでした\n%s", logs.String())
	}
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, logs.String())
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("終了しても socket のファイルが残っています: %s", sock)
	}
}

// socketFromLogs は continuo のログから、listen した socket の絶対パスを取り出す。
//
// t: 呼び出し元のテスト。
// out: continuo の標準出力と標準エラーを混ぜたもの。
// 戻り値: socket の絶対パス。見つからなければテストを落とす。
func socketFromLogs(t *testing.T, out string) string {
	t.Helper()
	// **行を探してから `socket=` を読む。**ログは
	// `msg="hook を受ける socket の listen を始めました" socket=/…` の形なので、
	// 文言のうしろに引用符が挟まる。文言と `socket=` を続けて探すと必ず外れる。
	const marker = "hook を受ける socket の listen を始めました"
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, marker) {
			continue
		}
		idx := strings.Index(line, "socket=")
		if idx < 0 {
			continue
		}
		rest := line[idx+len("socket="):]
		if end := strings.IndexAny(rest, " \t"); end >= 0 {
			rest = rest[:end]
		}
		return strings.TrimSpace(rest)
	}
	t.Fatalf("ログに socket の場所がありません:\n%s", out)
	return ""
}
