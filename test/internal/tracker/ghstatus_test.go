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
  ✓ Logged in to github.com account octocat (keyring)
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

  ✓ Logged in to github.com account octocat (keyring)
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

// ghAuthStatusTokenInvalidOutput は、gh はログイン済みだが GitHub へ届かず、
// トークンを検証できなかったときの実際の出力である
// （実測: gh 2.97.0 / `HTTPS_PROXY=http://127.0.0.1:1 gh auth status --hostname github.com`。
// 終了コードは 1 だった）。**`Logged in to ` という文字列を1つも含まない。**
const ghAuthStatusTokenInvalidOutput = `github.com
  X Failed to log in to github.com account octocat (keyring)
  - Active account: true
  - The token in keyring is invalid.
  - To re-authenticate, run: gh auth refresh -h github.com
  - To forget about this account, run: gh auth logout -h github.com -u octocat
`

// TestCheckGHProjectScope_トークンを検証できないだけなら未ログインと言わない は、
// 落ちた理由の言い分けを固定する。
//
// 目的: ログイン済みでも、一時的に GitHub へ届かなければ gh は非 0 で終わる。
// **それを「有効なアカウントがありません」と報告すると、運用者は言われたとおり
// `gh auth login` をやり直すが、原因はネットワーク側なので直らない。**
// ネットワークが戻れば何もしなくても起動できたはずの状態で、無駄な再認証をさせることになる。
//
// 与える情報: `Failed to log in to …` と `Active account: true` を含む実測の出力。
//
// 成功条件: エラーになり、文面が「トークンを検証できていません」と `gh auth refresh` を
// 指すこと。**`gh auth login` を案内しないこと。**gh 自身が書いた理由
// （`The token in keyring is invalid.`）が文面に残っていること。
func TestCheckGHProjectScope_トークンを検証できないだけなら未ログインと言わない(t *testing.T) {
	err := tracker.CheckGHProjectScope(context.Background(), func(context.Context) (string, error) {
		return ghAuthStatusTokenInvalidOutput, nil
	})
	if err == nil {
		t.Fatal("トークンを検証できていないのに合格した")
	}
	if strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("未ログインだと言い切って再認証をやり直させている: %v", err)
	}
	if !strings.Contains(err.Error(), "gh auth refresh -h github.com") {
		t.Fatalf("正しい直し方（gh auth refresh）を案内していない: %v", err)
	}
	if !strings.Contains(err.Error(), "The token in keyring is invalid.") {
		t.Fatalf("gh 自身が書いた理由が文面に残っていない: %v", err)
	}
}

// TestCheckGHProjectScope_未ログインのときもghの出力を隠さない は、
// 落ちた理由を運用者が読めることを固定する。
//
// 目的: 固定の文言だけを出すと、gh が書いた本当の理由が1文字も画面に出ない。
// 起動時の検査は落ちた理由が分からないと直せない。
//
// 与える情報: 未ログインの出力。
//
// 成功条件: エラー文に `gh auth status` の出力がそのまま含まれること。
func TestCheckGHProjectScope_未ログインのときもghの出力を隠さない(t *testing.T) {
	const out = "You are not logged into any GitHub hosts. To log in, run: gh auth login\n"
	err := tracker.CheckGHProjectScope(context.Background(), func(context.Context) (string, error) {
		return out, nil
	})
	if err == nil {
		t.Fatal("未ログインなのに合格した")
	}
	if !strings.Contains(err.Error(), "You are not logged into any GitHub hosts.") {
		t.Fatalf("gh の出力が文面に残っていない: %v", err)
	}
}
