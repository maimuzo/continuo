// {"RUCM-CFG-SHA256": "bebb24af1222bffaebe633b64bdcd42d011883d0e32157325376627ac7393cd5", "SOURCE": "docs/spec/usecases/particular_case/issue を1件処理する.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**
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
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
)

// {"RUCM-PATH": "P016"}
//
// TestPreflight_未信頼なら着手せず承認を促すコメントを1件書く は、段0 の信頼の検査を確かめる。
//
// 目的: 信頼登録されていないリポジトリの issue に着手しないこと。
// **そのまま黙って飛ばすと、人間は「なぜ動かないのか」を知る手がかりを持たない**ので、
// issue へ直し方を1件だけ書く。
// 与える情報: 信頼登録していないリポジトリ（`Untrusted`）。
// 成功条件: Status が動かず、worktree も開かず、issue にコメントが1件だけ付くこと。
func TestPreflight_未信頼なら着手せず承認を促すコメントを1件書く(t *testing.T) {
	fx := newFixture(t, fixtureOptions{Untrusted: true})
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))

	fx.Orc.Tick(context.Background())

	waitFor(t, 10*time.Second, "承認を促すコメントが付く", func() bool {
		return len(fx.Tracker.CommentsOf("I_node188")) > 0
	})
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Ready" {
		t.Errorf("着手していないのに Status を動かしている: %s", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("未信頼なのに worktree を開いている: %d 回", got)
	}
	var body strings.Builder
	for _, c := range fx.Tracker.CommentsOf("I_node188") {
		body.WriteString(c.Body)
	}
	if !strings.Contains(body.String(), "continuo trust") {
		t.Errorf("直し方（continuo trust）を書いていない: %s", body.String())
	}
}

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

// {"RUCM-PATH": "P015"}
//
// TestPreflight_登録の無い実体があるならStatusを1バイトも書かずに飛ばす は、
// 段0 の worktree の検査を確かめる。
//
// **この検査が段3（worktree の用意）にあると、必ず失敗する着手でも先に running_state を
// 書いてしまう。**running_state は active_states なので次の巡回でまた候補に上がり、
// running_state と failure_state の往復が永久に続く。
//
// 目的: 目的のパスに実体があるのに git の worktree として登録されていないとき、
// **Status を1バイトも書かずに** その issue を飛ばすこと。
// 与える情報: 目的のパスに、git に登録されていないディレクトリを先に置く。
// 成功条件: UpdateStatus が1回も呼ばれず、Status が Ready のままで、
// worktree も開かれず、印も残らないこと。
func TestPreflight_登録の無い実体があるならStatusを1バイトも書かずに飛ばす(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	// **git の登録を持たない実体を、目的のパスへ先に置く。**
	// 人間が手で作ったディレクトリや、消し損ねた残骸がこの形になる。
	unregistered := filepath.Join(
		fx.WorktreeRoot, "github.com", "octocat", "hello-world", "continuo-octocat-hello-world-188")
	if err := os.MkdirAll(unregistered, 0o700); err != nil {
		t.Fatalf("登録の無い実体を置けません: %v", err)
	}
	fx.Tracker.AddIssue(sampleIssue(188, "Ready"))
	fx.Tracker.ResetCalls()

	fx.Orc.Tick(context.Background())

	if got := fx.Tracker.CountCall("UpdateStatus"); got != 0 {
		t.Errorf("着手できないと分かっているのに Status を書いている: UpdateStatus を %d 回呼んだ", got)
	}
	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Ready" {
		t.Errorf("Status が動いている: got %q, want Ready", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("着手できないのに worktree を開いている: %d 回", got)
	}
	if got := len(fx.Orc.RunningIdentifiers()); got != 0 {
		t.Errorf("印が残っている: %d 件", got)
	}
}
