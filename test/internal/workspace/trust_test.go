package workspace_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// writeClaudeConfig はテスト用の `~/.claude.json` を書く。
//
// t: 呼び出し元のテスト。
// home: ホームディレクトリ。
// projects: projects に入れる「鍵 → hasTrustDialogAccepted」の対応。
func writeClaudeConfig(t *testing.T, home string, projects map[string]bool) {
	t.Helper()

	entries := map[string]any{}
	for key, accepted := range projects {
		entries[key] = map[string]any{
			"hasTrustDialogAccepted": accepted,
			// continuo が読まないキーも混ぜておく（読み飛ばせることの確認を兼ねる）。
			"allowedTools": []string{},
		}
	}
	data, err := json.MarshalIndent(map[string]any{"projects": entries}, "", "  ")
	if err != nil {
		t.Fatalf("~/.claude.json を JSON 化できない: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, workspace.ClaudeConfigFileName), append(data, '\n'), 0o600); err != nil {
		t.Fatalf("~/.claude.json を書けない: %v", err)
	}
}

// 目的: 信頼を引く鍵が、ghq の clone のパスを `git rev-parse --show-toplevel` で解決した
// ものであることを確認する（設計 3-6 の3段）。**worktree のパスで引いてはならない。**
// 与える情報: clone のパスを鍵にした `~/.claude.json` と、worktree を1つ持つリポジトリ。
// 成功条件: CheckTrust が true を返し、理由に clone のパスが入っていること。
func TestCheckTrust_鍵はcloneのtoplevelである(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	toplevel := runGit(t, fx.Repo.Dir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	writeClaudeConfig(t, fx.Home, map[string]bool{toplevel: true})

	trusted, reason, err := fx.Manager.CheckTrust("maimuzo", "koetsumugi")
	if err != nil {
		t.Fatalf("CheckTrust に失敗した: %v", err)
	}
	if !trusted {
		t.Fatalf("clone のパスで登録されているのに未信頼と判定された: %s", reason)
	}
	if !strings.Contains(reason, toplevel) {
		t.Fatalf("理由に鍵のパスが入っていない: %q", reason)
	}
	if strings.Contains(reason, prepared.Path) {
		t.Fatalf("worktree のパスを鍵にしている: %q", reason)
	}
}

// 目的: worktree のパスだけが登録されていても「信頼済み」にならないことを確認する
// （設計 1-2 の実測。信頼はリポジトリ単位で記録される）。
// 与える情報: worktree のパスだけを鍵にした `~/.claude.json`。
// 成功条件: CheckTrust が false を返すこと。
func TestCheckTrust_worktreeのパスでは信頼済みにならない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	prepared := prepareWorktree(t, fx, sampleIssue(188))

	writeClaudeConfig(t, fx.Home, map[string]bool{prepared.Path: true})

	trusted, reason, err := fx.Manager.CheckTrust("maimuzo", "koetsumugi")
	if err != nil {
		t.Fatalf("CheckTrust に失敗した: %v", err)
	}
	if trusted {
		t.Fatalf("worktree のパスで信頼済みと判定された: %s", reason)
	}
}

// 目的: 「clone が無い」と「未信頼」を理由で区別できることを確認する
// （設計 05_workspace.md の受け入れ基準。doctor が理由を出す）。
// 与える情報: clone が引けない ghq と、clone はあるが未承認の `~/.claude.json`。
// 成功条件: どちらも trusted が偽で、理由の文言が異なること（clone が無い側には
// clone のことが書かれていること）。
func TestCheckTrust_cloneが無いことと未信頼を理由で区別する(t *testing.T) {
	noClone := newFixture(t, fixtureOptions{
		GhqList: func(_ context.Context, _, _ string) (string, error) { return "", nil },
	})
	trusted, cloneReason, err := noClone.Manager.CheckTrust("maimuzo", "koetsumugi")
	if err != nil {
		t.Fatalf("clone が無い場合の CheckTrust がエラーになった: %v", err)
	}
	if trusted {
		t.Fatal("clone が無いのに信頼済みと判定された")
	}
	if !strings.Contains(cloneReason, "clone") {
		t.Fatalf("clone が無いことが理由に書かれていない: %q", cloneReason)
	}

	fx := newFixture(t, fixtureOptions{})
	toplevel := runGit(t, fx.Repo.Dir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	writeClaudeConfig(t, fx.Home, map[string]bool{toplevel: false})

	trusted, untrustedReason, err := fx.Manager.CheckTrust("maimuzo", "koetsumugi")
	if err != nil {
		t.Fatalf("未信頼の場合の CheckTrust がエラーになった: %v", err)
	}
	if trusted {
		t.Fatal("hasTrustDialogAccepted が false なのに信頼済みと判定された")
	}
	if untrustedReason == cloneReason {
		t.Fatalf("clone が無い場合と未信頼の場合で理由が同じになっている: %q", untrustedReason)
	}
}

// 目的: `~/.claude.json` を書き換えないことを確認する（絶対に守る制約）。
// 与える情報: 検査の前後で比べるための `~/.claude.json` の中身。
// 成功条件: CheckTrust と TrustFunc を呼んだあとも、ファイルの中身が1バイトも変わらないこと。
func TestCheckTrust_claude_jsonを書き換えない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	toplevel := runGit(t, fx.Repo.Dir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	writeClaudeConfig(t, fx.Home, map[string]bool{toplevel: true})

	path := filepath.Join(fx.Home, workspace.ClaudeConfigFileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("検査前の ~/.claude.json を読めない: %v", err)
	}

	if _, _, err := fx.Manager.CheckTrust("maimuzo", "koetsumugi"); err != nil {
		t.Fatalf("CheckTrust に失敗した: %v", err)
	}
	if !fx.Manager.TrustFunc()("maimuzo", "koetsumugi") {
		t.Fatal("TrustFunc が偽を返した")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("検査後の ~/.claude.json を読めない: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("~/.claude.json が書き換えられている:\n前: %s\n後: %s", before, after)
	}
}

// 目的: tracker.RepoTrustFunc に渡す薄い包みが、「clone が無い」も「未信頼」も同じく
// false にすることを確認する（設計 05_workspace.md の受け入れ基準）。
// 与える情報: clone が無い場合と、clone はあるが未承認の場合。
// 成功条件: どちらも false になること。
func TestTrustFunc_cloneが無くても未信頼でも偽になる(t *testing.T) {
	noClone := newFixture(t, fixtureOptions{
		GhqList: func(_ context.Context, _, _ string) (string, error) { return "", nil },
	})
	if noClone.Manager.TrustFunc()("maimuzo", "koetsumugi") {
		t.Fatal("clone が無いのに真を返した")
	}

	fx := newFixture(t, fixtureOptions{})
	toplevel := runGit(t, fx.Repo.Dir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	writeClaudeConfig(t, fx.Home, map[string]bool{toplevel: false})
	if fx.Manager.TrustFunc()("maimuzo", "koetsumugi") {
		t.Fatal("未信頼なのに真を返した")
	}
}

// 目的: `~/.claude.json` が無くても、判定できないエラーではなく「未信頼」として扱うことを
// 確認する（Claude Code をまだ一度も使っていない機械でも起動を止めない）。
// 与える情報: `~/.claude.json` を置いていないホームディレクトリ。
// 成功条件: trusted が偽、err が nil、理由にファイルが無いことが書かれていること。
func TestCheckTrust_claude_jsonが無ければ未信頼として扱う(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})

	trusted, reason, err := fx.Manager.CheckTrust("maimuzo", "koetsumugi")
	if err != nil {
		t.Fatalf("~/.claude.json が無いだけでエラーになった: %v", err)
	}
	if trusted {
		t.Fatal("~/.claude.json が無いのに信頼済みと判定された")
	}
	if !strings.Contains(reason, workspace.ClaudeConfigFileName) {
		t.Fatalf("理由にファイル名が入っていない: %q", reason)
	}
}

// 目的: TrustFunc の戻り値が tracker.RepoTrustFunc へそのまま渡せることを確認する
// （05_workspace.md の受け入れ基準は「tracker.RepoTrustFunc に**合う**
// `func(owner, repo string) bool`」であり、**internal/workspace は
// internal/tracker を import しない**。この検査は tracker 側から行う）。
// 与える情報: 信頼済みのリポジトリを登録した `~/.claude.json`。
// 成功条件: 代入がコンパイルでき、tracker.RepoTrustFunc として呼んで真になること。
func TestTrustFunc_tracker_RepoTrustFuncへ代入できる(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	toplevel := runGit(t, fx.Repo.Dir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	writeClaudeConfig(t, fx.Home, map[string]bool{toplevel: true})

	var trusted tracker.RepoTrustFunc = fx.Manager.TrustFunc()
	if !trusted("maimuzo", "koetsumugi") {
		t.Fatal("tracker.RepoTrustFunc として呼ぶと偽になった")
	}
}

// 目的: 信頼の判定が外部コマンドに時間の上限を渡すことを確認する（設計 3-6 / 3-11）。
//
// **上限が無いと、ghq か git が固まったときに巡回のループごと止まる。**この判定は
// dispatch の直前に issue ごとに呼ばれるので、1件でも返らなければ無人運用がそこで終わる。
//
// 与える情報: 受け取ったコンテキストに期限があるかを記録する ghq の差し替え。
// 成功条件: 期限つきのコンテキストが渡っていること。
func TestCheckTrust_外部コマンドに時間の上限を渡す(t *testing.T) {
	var hasDeadline bool
	fx := newFixture(t, fixtureOptions{
		GhqList: func(ctx context.Context, _, _ string) (string, error) {
			_, hasDeadline = ctx.Deadline()
			return "", nil
		},
	})

	if _, _, err := fx.Manager.CheckTrust("maimuzo", "koetsumugi"); err != nil {
		t.Fatalf("CheckTrust に失敗した: %v", err)
	}
	if !hasDeadline {
		t.Fatal("外部コマンドに期限の無いコンテキストを渡している（固まると巡回ごと止まる）")
	}
}

// 目的: 信頼の判定の結果を短い間だけ覚え、ボードの項目ごとに ghq と git を起動し直さない
// ことを確認する（設計 3-6。判定1回につき外部プロセス2本と `~/.claude.json` の読み直しが要る）。
// 与える情報: 1回目の判定のあとに `~/.claude.json` を消してから行う2回目の判定。
// 成功条件: 2回目も真のままであること（覚えていなければ、ファイルが無いので偽になる）。
func TestTrustFunc_判定を覚えて外部コマンドを繰り返し起動しない(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	toplevel := runGit(t, fx.Repo.Dir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	writeClaudeConfig(t, fx.Home, map[string]bool{toplevel: true})

	trusted := fx.Manager.TrustFunc()
	if !trusted("maimuzo", "koetsumugi") {
		t.Fatal("1回目の判定が偽になった")
	}

	if err := os.Remove(filepath.Join(fx.Home, workspace.ClaudeConfigFileName)); err != nil {
		t.Fatalf("~/.claude.json を消せない: %v", err)
	}
	if !trusted("maimuzo", "koetsumugi") {
		t.Fatal("2回目の判定で `~/.claude.json` を読み直している（1件ごとに外部プロセスが立つ）")
	}
}
