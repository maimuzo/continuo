package orchestrator_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/tracker"
)

// pushBranchTemplate は `.push_branch` だけを見るテンプレートである。
//
// **`{{if}}` を使う。**リンクが無いときに空文字が渡ることを、変数展開の結果で見分けるためである。
const pushBranchTemplate = `{{.issue.identifier}} を実装してください。
{{if .push_branch}}リンクされた branch は {{.push_branch}} です。{{else}}リンクされた branch はありません。{{end}}`

// renderPushBranchPrompt は1件の issue を dispatch させ、送られた1回目の本文を返す。
//
// **issue.BranchName が入っているときは、その branch を origin に用意する。**
// 用意しないと base に据えられず、着手そのものが失敗する（設計 3-22d）。
//
// t: 呼び出し元のテスト。
// issue: dispatch させる issue。
// 戻り値: 送られた本文。
func renderPushBranchPrompt(t *testing.T, issue tracker.Issue) string {
	t.Helper()
	fx := newFixture(t, fixtureOptions{
		PromptTemplate: pushBranchTemplate,
		Mutate: func(cfg *config.Config) {
			cfg.Tracker.VerifyStatesEvery = 0
		},
	})
	if issue.BranchName != nil {
		runGit(t, fx.Repo.Dir, "branch", *issue.BranchName, fx.Repo.Base)
		runGit(t, fx.Repo.Dir, "push", "--quiet", "origin", *issue.BranchName)
	}
	prompts := recordPrompts(fx)
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "1回目の本文が送られる", func() bool {
		return len(prompts()) > 0
	})
	return prompts()[0]
}

// 目的: リンクされた branch の名前を、プロンプトの `.push_branch` で渡すことを確認する
// （設計 3-22d・5-3）。**エージェントは「どこへ push するか」をプロンプトからしか知れない。**
//
// **`origin/` は付けない。**base は `origin/work/issue-42` だが、`.push_branch` は
// 生の名前である。付けて渡すと、エージェントが `origin/origin/work/issue-42` へ
// push しようとする。
//
// 与える情報: BranchName が `work/issue-42` の issue と、`.push_branch` を使うテンプレート。
// 成功条件: 変数展開された本文に `work/issue-42` が入り、`origin/` が付いていないこと。
func TestPrompt_リンクされたbranchの名前をpush_branchで渡す(t *testing.T) {
	issue := sampleIssue(188, "Ready")
	branch := "work/issue-42"
	issue.BranchName = &branch

	got := renderPushBranchPrompt(t, issue)

	if !strings.Contains(got, "リンクされた branch は work/issue-42 です。") {
		t.Fatalf(".push_branch がリンクされた branch の名前になっていない:\n%s", got)
	}
	if strings.Contains(got, "origin/work/issue-42") {
		t.Fatalf(".push_branch に origin/ が付いている（base とは形が違う）:\n%s", got)
	}
}

// 目的: リンクされた branch が、実際に worktree の起点になることを確認する
// （設計 3-22d）。**トラッカーが読んだ名前が workspace まで運ばれていなければ、
// 起点は既定 branch のままになる。**着手を1件通して、その経路が繋がっていることを見る。
//
// 与える情報: origin にだけ在る `work/issue-188`（main には無い commit を1つ持つ）を
// リンクした issue。**手元の clone はローカル branch もリモート追跡 ref も持たない。**
// 成功条件: 着手で作られた branch の先頭が、リンクされた branch の commit と一致すること。
func TestDispatch_リンクされたbranchを起点に着手する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Tracker.VerifyStatesEvery = 0 },
	})

	// origin にだけ在る branch を作る。**main とは別の commit を持たせる。**
	// 同じ commit だと、起点が main でも一致してしまい、検査にならない。
	runGit(t, fx.Repo.Dir, "checkout", "--quiet", "-b", "work/issue-188")
	if err := os.WriteFile(filepath.Join(fx.Repo.Dir, "LINKED.md"), []byte("リンクされた branch の中身\n"), 0o644); err != nil {
		t.Fatalf("リンクされた branch のファイルを書けません: %v", err)
	}
	runGit(t, fx.Repo.Dir, "add", ".")
	runGit(t, fx.Repo.Dir, "commit", "--quiet", "-m", "リンクされた branch の commit")
	runGit(t, fx.Repo.Dir, "push", "--quiet", "origin", "work/issue-188")
	linkedSHA := runGit(t, fx.Repo.Dir, "rev-parse", "HEAD")
	runGit(t, fx.Repo.Dir, "checkout", "--quiet", fx.Repo.Base)
	runGit(t, fx.Repo.Dir, "branch", "--quiet", "-D", "work/issue-188")
	runGit(t, fx.Repo.Dir, "update-ref", "-d", "refs/remotes/origin/work/issue-188")

	issue := sampleIssue(188, "Ready")
	branch := "work/issue-188"
	issue.BranchName = &branch
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "着手で branch が作られる", func() bool {
		out, err := exec.Command("git", "-C", fx.Repo.Dir,
			"rev-parse", "--verify", "--quiet", "continuo/octocat/hello-world/188").Output()
		return err == nil && strings.TrimSpace(string(out)) != ""
	})

	got := runGit(t, fx.Repo.Dir, "rev-parse", "continuo/octocat/hello-world/188")
	if got != linkedSHA {
		mainSHA := runGit(t, fx.Repo.Dir, "rev-parse", "main")
		t.Fatalf("着手の起点がリンクされた branch でない: got %q, want %q（main は %q）",
			got, linkedSHA, mainSHA)
	}
}

// 目的: リンクされた branch を取ってこられなかった issue を、**人間へ渡さずに
// 試し直す**ことを確認する（設計 3-22d / 3-16 の段10）。
//
// **fetch の失敗は回線で起きる。**30秒×2＋1秒＝61秒だけ回線が切れた issue を
// `failure_state` へ置くと、そこは `tracker.active_states` に入っていないので、
// **人間がカンバンで戻すまで continuo は二度と拾わない。**
// 同じ形の事故が 2026-08-21 に起きている（dispatch.go の ErrStartupRetryable の説明）。
//
// 与える情報: origin に存在しない `work/does-not-exist` をリンクした issue。
// 成功条件: 着手の失敗が「待って試し直します」として記録され、
// **Status が failure_state（Blocked）へ落ちないこと。**
func TestDispatch_リンクされたbranchを取ってこられなくても人間へ渡さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.Tracker.VerifyStatesEvery = 0 },
	})
	// **どちらも、このテストがわざと起こしている失敗である。**
	fx.AllowLog("リンクされた branch を取ってこられなかったのでやり直します")
	fx.AllowLog("着手に失敗しました（待って試し直します）")

	issue := sampleIssue(188, "Ready")
	branch := "work/does-not-exist"
	issue.BranchName = &branch
	fx.Tracker.AddIssue(issue)

	fx.Orc.Tick(context.Background())

	waitFor(t, 30*time.Second, "着手の失敗が記録される", func() bool {
		return strings.Contains(fx.Logs.String(), "着手に失敗しました（待って試し直します）")
	})
	if got := fx.Tracker.StateOf("PVTI_item188"); got == "Blocked" {
		t.Fatalf("fetch に失敗しただけの issue を人間へ渡している"+
			"（failure_state は active_states に無いので、二度と拾われない）: state=%s", got)
	}
}

// 目的: リンクが無い issue では `.push_branch` が空文字になることを確認する
// （設計 3-22d・5-3）。**`{{if .push_branch}}` が偽になる形でなければ、
// テンプレートを書く側が「リンクが無いとき」を書き分けられない。**
// 与える情報: BranchName が nil の issue と、`.push_branch` を使うテンプレート。
// 成功条件: 変数展開された本文が `{{else}}` の側になること。
func TestPrompt_リンクが無ければpush_branchは空になる(t *testing.T) {
	got := renderPushBranchPrompt(t, sampleIssue(188, "Ready"))

	if !strings.Contains(got, "リンクされた branch はありません。") {
		t.Fatalf("リンクが無いのに .push_branch が真になっている:\n%s", got)
	}
}
