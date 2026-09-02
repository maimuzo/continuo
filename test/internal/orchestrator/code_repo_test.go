package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/tracker"
)

// {"RUCM-CFG-SHA256": "5ba01b4a174c146c45e05e581754e126e476934e66380f1e243886859c4b3419", "SOURCE": "docs/spec/usecases/particular_case/issue にリンクされた branch を起点にして着手する.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**
// 「リンクが別々のリポジトリを指しているので着手しない」経路を確かめる。

// {"RUCM-PATH": "P010"}
//
// TestPreflight_リンクが別々のリポジトリなら何も書き換えずに飛ばす は、
// 着手の段0 の新しい関門を確かめる。
//
// 目的: どちらのリポジトリで作業すべきかを決められない issue に着手しないこと。
// **勝手にどちらかを選ぶと、別のリポジトリで作業を始めてしまう。**
//
// **1巡回では issue へ書かない。**カンバンの候補一覧は GitHub のサーバ側の検索結果であり、
// 索引の反映が遅れて1巡回だけ答えが揺れることがある。
// **揺れただけの issue に誤った案内が1件残る。消す手段は無い。**
//
// 与える情報: 別々のリポジトリの branch が2本リンクされた issue（1巡回だけ）。
// 成功条件: Status が動かず、worktree も開かず、issue にコメントが1件も付かないこと。
func TestPreflight_リンクが別々のリポジトリなら何も書き換えずに飛ばす(t *testing.T) {
	fx := newFixture(t, fixtureOptions{})
	fx.Tracker.AddIssue(undecidedIssue(188, "Ready"))
	fx.AllowLog("リンクされた branch が別々のリポジトリを指すので着手しません")

	fx.Orc.Tick(context.Background())

	if got := fx.Tracker.StateOf("PVTI_item188"); got != "Ready" {
		t.Errorf("着手していないのに Status を動かしている: %s", got)
	}
	if got := fx.Herdr.CountMethod(herdr.MethodWorktreeOpen); got != 0 {
		t.Errorf("決められないのに worktree を開いている: %d 回", got)
	}
	if got := len(fx.Tracker.CommentsOf("I_node188")); got != 0 {
		t.Errorf("1巡回目でコメントを書いている（索引の揺れで誤った案内が残る）: %d 件", got)
	}
}

// {"RUCM-PATH": "P009"}
//
// TestPreflight_リンクが別々のリポジトリなら3巡回目に1回だけ書く は、
// 案内が「3回続けて止め、60秒たったとき」に1回だけ出ることを確かめる。
//
// 目的: **Status が動かない経路なので、書かないと誰にも届かない。**
// だが毎巡回書くと、30秒ごとに同じ案内が積まれる。**消す手段は無い。**
//
// 与える情報: 別々のリポジトリの branch が2本リンクされた issue と、
// 60秒を越える時計の進み。
// 成功条件: コメントが1件だけ付き、そこにリンクの一覧と確かめ方が入っていること。
// **4巡回目でも増えない**こと。
func TestPreflight_リンクが別々のリポジトリなら3巡回目に1回だけ書く(t *testing.T) {
	clock := newTestClock()
	fx := newFixture(t, fixtureOptions{Now: clock.Now})
	fx.Tracker.AddIssue(undecidedIssue(188, "Ready"))
	fx.AllowLog("リンクされた branch が別々のリポジトリを指すので着手しません")

	for range 4 {
		fx.Orc.Tick(context.Background())
		clock.Advance(30 * time.Second)
	}

	comments := fx.Tracker.CommentsOf("I_node188")
	if len(comments) != 1 {
		t.Fatalf("案内が1件でない: %d 件", len(comments))
	}
	body := comments[0].Body
	for _, want := range []string{
		"myorg/project", "other-org/project", "gh issue develop --list 188",
		"1バイトも書き換えていません",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("案内に %q が入っていない:\n%s", want, body)
		}
	}
}

// undecidedIssue は、別々のリポジトリの branch が2本リンクされた issue を作る。
//
// **`CodeRepoUndecided` を真にするのはトラッカーのアダプタである。**
// ここでは、その結果を受け取った orchestrator の振る舞いだけを確かめる。
//
// number: issue の番号。
// state: カンバンの Status。
// 戻り値: 着手しない判定になっている issue。
func undecidedIssue(number int, state string) tracker.Issue {
	issue := sampleIssue(number, state)
	issue.CodeRepoUndecided = true
	issue.LinkedBranches = []tracker.LinkedBranchRef{
		{NameWithOwner: "myorg/project", Branch: "work/issue-42"},
		{NameWithOwner: "other-org/project", Branch: "hotfix/issue-42"},
	}
	return issue
}
