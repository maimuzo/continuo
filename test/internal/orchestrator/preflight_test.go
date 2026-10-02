// dispatch 直前の検査（段0）の検査である。
//
// **段0 を段2 より前に置くのは、Status を書いてから飛ばすと毎巡回で候補に上がり続け、
// 30秒ごとにコメントが積まれるためである**（設計 3-16）。
// ここで飛ばした issue は、Status も worktree も1バイトも動かないことを確かめる。
package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// TestPreflight_未信頼の通知は巡回のたびに繰り返さない は、コメントが積まれないことを確かめる。
//
// **`Ready` は active_states なので、飛ばした issue は毎巡回で候補に上がり続ける。**
// そのたびにコメントを書くと、30秒ごとに同じ内容が積まれる。
//
// 目的: 同じリポジトリについて、通知を1回だけにすること。
// 与える情報: 未信頼のまま巡回を3回。
// 成功条件: コメントが1件のままであること。
func TestPreflight_未信頼の通知は巡回のたびに繰り返さない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Untrusted: true})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())
	waitFor(t, 10*time.Second, "1件目のコメントが付く", func() bool {
		return len(fx.Tracker.CommentsOf("I_node188")) > 0
	})
	fx.Orc.Tick(context.Background())
	fx.Orc.Tick(context.Background())
	time.Sleep(2 * time.Second)

	if got := len(fx.Tracker.CommentsOf("I_node188")); got != 1 {
		t.Errorf("巡回のたびにコメントが積まれている: %d 件", got)
	}
}

// TestPreflight_信頼の検査を切れば未信頼でも着手する は、設定で外せることを確かめる。
//
// **`trust.require_repo_trusted: false` は「検査しない」という明示の選択である。**
// 使い捨ての環境で、いちいち承認したくない場合に使う。
//
// 目的: 検査を切ったとき、未信頼でも着手すること。
// 与える情報: `require_repo_trusted: false` と、信頼登録していないリポジトリ。
// 成功条件: turn が送られること。
func TestPreflight_信頼の検査を切れば未信頼でも着手する(t *testing.T) {
	fx := newFixture(t, fixtureOptions{
		Untrusted: true,
		Mutate:    func(cfg *config.Config) { cfg.Trust.RequireRepoTrusted = false },
	})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
}

// TestPreflight_目的のパス自身がbranchを使っているなら飛ばさない は、
// 段0 の branch の検査が**再利用の経路を巻き込まない**ことを確かめる。
//
// **除外を忘れると、continuo が自分で作った worktree が「別の worktree が使っている」と
// 判定され、2回目以降の着手が全部飛ぶ。**
//
// 目的: 目的のパスの worktree がその branch を出しているとき、そのまま着手すること。
// 与える情報: 目的のパスに、目的の branch を出す worktree を先に作っておく。
// 成功条件: turn が送られること（飛ばされていないこと）。
func TestPreflight_目的のパス自身がbranchを使っているなら飛ばさない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	target := filepath.Join(
		fx.WorktreeRoot, "github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatalf("置き場所の親を作れません: %v", err)
	}
	// **continuo が前の run で作った worktree と同じ形にする**（目的のパスが branch を出す）。
	runGit(t, fx.Repo.Dir, "worktree", "add", "-b", "continuo/octocat/hello-world/188",
		target, fx.Repo.Base)

	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	holdPrompt(fx)

	fx.Orc.Tick(context.Background())

	waitFor(t, 15*time.Second, "turn が送られる", func() bool {
		return fx.Herdr.CountMethod(herdr.MethodAgentPrompt) > 0
	})
}
