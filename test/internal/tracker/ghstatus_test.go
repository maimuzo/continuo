package tracker_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/tracker"
)

// ghAuthStatusOutput は `gh auth status --hostname github.com` の実際の出力の形である
// （設計 3-32 の「`gh auth status` の読み方」。**`--show-scopes` というフラグは存在しない**）。
const ghAuthStatusOutput = `github.com
  ✓ Logged in to github.com account maimuzo (keyring)
  - Active account: true
  - Git operations protocol: https
  - Token: gho_************************************
  - Token scopes: 'gist', 'project', 'read:org', 'repo', 'workflow'
`

// TestCheckGHProjectScope_有効なアカウントにprojectがあれば通る は、
// 起動時の検査の合格の条件を確かめる。
//
// 目的: 設計 3-6。scope に `project` が無いとボードを読めない。
//
// 与える情報: `Active account: true` のブロックに `project` を含む出力。
//
// 成功条件: エラーを返さない。
func TestCheckGHProjectScope_有効なアカウントにprojectがあれば通る(t *testing.T) {
	err := tracker.CheckGHProjectScope(context.Background(), func(context.Context) (string, error) {
		return ghAuthStatusOutput, nil
	})
	if err != nil {
		t.Fatalf("合格すべき出力で落ちた: %v", err)
	}
}

// TestCheckGHProjectScope_read_projectだけでは落ちる は、読めるだけでは足りないことを確かめる。
//
// 目的: 設計 3-32。**`read:project` は不可である。**読めるだけでは Status を書けない。
//
// 与える情報: scope が `read:project` だけの出力。
//
// 成功条件: エラーを返し、その文面に `project` が要ることが書いてある。
func TestCheckGHProjectScope_read_projectだけでは落ちる(t *testing.T) {
	out := strings.Replace(ghAuthStatusOutput,
		"'gist', 'project', 'read:org', 'repo', 'workflow'",
		"'gist', 'read:project', 'repo'", 1)

	err := tracker.CheckGHProjectScope(context.Background(), func(context.Context) (string, error) {
		return out, nil
	})
	if err == nil {
		t.Fatalf("read:project だけなのに合格した")
	}
	if !strings.Contains(err.Error(), "project") {
		t.Fatalf("何が足りないのか分からない文面である: %v", err)
	}
}

// TestCheckGHProjectScope_有効でないアカウントのscopeは読まない は、
// 複数アカウントのときに別のアカウントの scope を見ないことを確かめる。
//
// 目的: 設計 3-32。**`gh` は同じホストに複数のアカウントを持てる。**最初に見つかった
// `Token scopes:` を読むと、有効でないアカウントの scope で合格してしまう。
//
// 与える情報: 有効でないアカウントが `project` を持ち、有効なアカウントが持たない出力。
//
// 成功条件: エラーを返す。
func TestCheckGHProjectScope_有効でないアカウントのscopeは読まない(t *testing.T) {
	out := `github.com
  ✓ Logged in to github.com account other (keyring)
  - Active account: false
  - Token scopes: 'project', 'repo'

  ✓ Logged in to github.com account maimuzo (keyring)
  - Active account: true
  - Token scopes: 'repo'
`
	err := tracker.CheckGHProjectScope(context.Background(), func(context.Context) (string, error) {
		return out, nil
	})
	if err == nil {
		t.Fatalf("有効でないアカウントの scope で合格してしまった")
	}
}

// TestCheckGHProjectScope_未ログインならログインの案内を出す は、
// アカウントが1つも無いときの扱いを確かめる。
//
// 目的: 設計 3-32。該当ブロックが1つも無いときは落とし、`gh auth login -s project` を案内する。
//
// 与える情報: 未ログインの出力。
//
// 成功条件: エラーを返し、文面に `gh auth login -s project` が入っている。
func TestCheckGHProjectScope_未ログインならログインの案内を出す(t *testing.T) {
	err := tracker.CheckGHProjectScope(context.Background(), func(context.Context) (string, error) {
		return "You are not logged into any GitHub hosts. To log in, run: gh auth login\n", nil
	})
	if err == nil {
		t.Fatalf("未ログインなのに合格した")
	}
	if !strings.Contains(err.Error(), "gh auth login -s project") {
		t.Fatalf("ログインの案内が出ていない: %v", err)
	}
}

// TestCheckGHProjectScope_ghを起動できなければそのまま落ちる は、
// 実行できないときにその理由を隠さないことを確かめる。
//
// 目的: 起動時の検査は落ちた理由が分からないと直せない。
//
// 与える情報: 実行そのものが失敗する関数。
//
// 成功条件: 返したエラーがそのまま伝わる。
func TestCheckGHProjectScope_ghを起動できなければそのまま落ちる(t *testing.T) {
	want := errors.New("gh を起動できません")
	err := tracker.CheckGHProjectScope(context.Background(), func(context.Context) (string, error) {
		return "", want
	})
	if !errors.Is(err, want) {
		t.Fatalf("実行の失敗が伝わっていない: %v", err)
	}
}
