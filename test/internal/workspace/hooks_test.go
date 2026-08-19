package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/workspace"
)

// 目的: workspace_hooks.after_run が、run が終わったときに1回だけ実行されることを確認する
// （設計 3-9 の段0。turn ごとではない）。
// 与える情報: 呼ばれるたびに1行追記する after_run と、同じ worktree に対する2回の呼び出し。
// 成功条件: 1回目だけ実行され、2回目は実行されず、追記された行が1行であること。
func TestRunAfterRunOnce_runが終わったときに1回だけ実行する(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "count.txt")
	command := "echo 呼ばれた >> " + marker
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.WorkspaceHooks.AfterRun = &command },
	})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	ran, err := fx.Manager.RunAfterRunOnce(context.Background(), prepared.Path)
	if err != nil {
		t.Fatalf("1回目の RunAfterRunOnce に失敗した: %v", err)
	}
	if !ran {
		t.Fatal("1回目なのに実行されていない")
	}

	ran, err = fx.Manager.RunAfterRunOnce(context.Background(), prepared.Path)
	if err != nil {
		t.Fatalf("2回目の RunAfterRunOnce がエラーになった: %v", err)
	}
	if ran {
		t.Fatal("2回目も実行されている（after_run は run 1回につき1度である）")
	}

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("after_run が実行されていない（%s を読めない）: %v", marker, err)
	}
	lines := strings.Count(strings.TrimSpace(string(data)), "\n") + 1
	if lines != 1 {
		t.Fatalf("after_run の実行回数が1でない: got %d 行\n%s", lines, data)
	}
}

// 目的: after_run が失敗しても、その失敗をエラーとして呼び出し側へ返すことを確認する
// （設計 3-9 の段0。呼び出し側は記録して続ける）。
// 与える情報: 必ず失敗する after_run。
// 成功条件: 実行したことを示す真と、失敗のエラーが返ること。
func TestRunAfterRunOnce_失敗は呼び出し側へ返す(t *testing.T) {
	command := "exit 3"
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.WorkspaceHooks.AfterRun = &command },
	})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	ran, err := fx.Manager.RunAfterRunOnce(context.Background(), prepared.Path)
	if !ran {
		t.Fatal("実行されていない")
	}
	if err == nil {
		t.Fatal("失敗する after_run なのにエラーが返らなかった")
	}
}

// 目的: workspace_hooks が未設定（null）なら何も実行せずに成功することを確認する。
// 与える情報: 既定の設定（すべての workspace_hooks が null）。
// 成功条件: RunHook がエラーを返さないこと。
func TestRunHook_未設定なら何もしない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	for _, phase := range []workspace.HookPhase{
		workspace.HookAfterCreate,
		workspace.HookBeforeRun,
		workspace.HookAfterRun,
		workspace.HookBeforeRemove,
	} {
		if err := fx.Manager.RunHook(context.Background(), phase, prepared.Path); err != nil {
			t.Fatalf("未設定の %s でエラーになった: %v", phase, err)
		}
	}
}

// 目的: workspace_hooks が worktree を作業ディレクトリにして実行されることを確認する
// （設計 3-9 / 3-16。cwd は worktree である）。
// 与える情報: 作業ディレクトリをファイルへ書き出す after_create。
// 成功条件: 書き出されたパスが worktree のパスと一致すること。
func TestRunHook_worktreeをcwdにして実行する(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "cwd.txt")
	command := "pwd > " + marker
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.WorkspaceHooks.AfterCreate = &command },
	})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	if err := fx.Manager.RunHook(context.Background(), workspace.HookAfterCreate, prepared.Path); err != nil {
		t.Fatalf("RunHook に失敗した: %v", err)
	}

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("after_create が実行されていない: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != prepared.Path {
		t.Fatalf("cwd が worktree でない: got %q, want %q", got, prepared.Path)
	}
}

// 目的: worktree を再利用して2回目の run が始まったら after_run がもう一度実行されることを
// 確認する（設計 3-18「再利用するということは、その issue が再び dispatch されたということで
// あり、そこから先は別の run である」。設計 3-9 の段0 の「1回だけ」は run 単位である）。
// 与える情報: 呼ばれるたびに1行追記する after_run と、同じ issue に対する2回の Prepare。
// 成功条件: どちらの run でも after_run が実行され、追記された行が2行であること。
func TestRunAfterRunOnce_再利用したら次のrunでも実行する(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "count.txt")
	command := "echo 呼ばれた >> " + marker
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.WorkspaceHooks.AfterRun = &command },
	})

	prepared := prepareWorktree(t, fx, sampleIssue(188))
	if ran, err := fx.Manager.RunAfterRunOnce(context.Background(), prepared.Path); err != nil || !ran {
		t.Fatalf("1回目の run の after_run が実行されていない: ran=%v err=%v", ran, err)
	}

	// 同じ worktree を再利用する（＝その issue が再び dispatch された）。
	reused := prepareWorktree(t, fx, sampleIssue(188))
	if reused.Created {
		t.Fatal("再利用されていない（worktree を作り直している）")
	}
	ran, err := fx.Manager.RunAfterRunOnce(context.Background(), reused.Path)
	if err != nil {
		t.Fatalf("2回目の run の after_run に失敗した: %v", err)
	}
	if !ran {
		t.Fatal("2回目の run なのに after_run が実行されない（印が worktree 単位のままである）")
	}

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("after_run が実行されていない（%s を読めない）: %v", marker, err)
	}
	lines := strings.Count(strings.TrimSpace(string(data)), "\n") + 1
	if lines != 2 {
		t.Fatalf("after_run の実行回数が2でない: got %d 行\n%s", lines, data)
	}
}

// 目的: 複数の run から同時に呼んでも after_run がちょうど1回だけ実行されることを確認する
// （設計 3-8。turn ループは run ごとの goroutine で動き、Manager は共有される）。
// 与える情報: 同じ Manager に対する、8つの goroutine からの同時呼び出し。
// 成功条件: 実行したと答えたのがちょうど1つで、`go test -race` でも競合が検出されないこと。
func TestRunAfterRunOnce_複数のrunから同時に呼んでも1回だけ実行する(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "count.txt")
	command := "echo 呼ばれた >> " + marker
	fx := newFixture(t, fixtureOptions{
		Mutate: func(cfg *config.Config) { cfg.WorkspaceHooks.AfterRun = &command },
	})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	const goroutines = 8
	var wg sync.WaitGroup
	results := make([]bool, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ran, err := fx.Manager.RunAfterRunOnce(context.Background(), prepared.Path)
			if err != nil {
				t.Errorf("RunAfterRunOnce に失敗した: %v", err)
			}
			results[i] = ran
		}()
	}
	wg.Wait()

	executed := 0
	for _, ran := range results {
		if ran {
			executed++
		}
	}
	if executed != 1 {
		t.Fatalf("同時に呼んだときの実行回数が1でない: got %d", executed)
	}
}
