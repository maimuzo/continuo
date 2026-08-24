package herdr_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/herdr"
)

// 目的: pane と herdr workspace に貼る label が `owner/repo/issues/N` の形になることを
// 確認する（設計 3-3。issue #12）。
// 与える情報: owner / repo / issue 番号。
// 成功条件: `octocat/hello-world/issues/188` が返ること。
// **issue の URL にしないのは、herdr の一覧で先頭が全部 `https://github.com/` になり、
// 見分けたい部分が右へ押し出されるためである。**
func TestIssueLabel_ownerとrepoとissue番号を並べる(t *testing.T) {
	got := herdr.IssueLabel("octocat", "hello-world", 188)
	const want = "octocat/hello-world/issues/188"
	if got != want {
		t.Fatalf("label の形が違う: got %q, want %q", got, want)
	}
}

// 目的: 材料が欠けているときに壊れた label を組み立てないことを確認する。
// 与える情報: owner が空・repo が空・番号が0・番号が負、の4通り。
// 成功条件: どれも空文字が返ること（`//issues/0` のような label を書かない）。
// **draft issue は owner も repo も issue 番号も持たない。**着手の経路では
// Dispatchable で弾いているが、弾く条件が将来変わったときの保険としてここでも止める。
func TestIssueLabel_材料が欠けていたら空文字を返す(t *testing.T) {
	cases := []struct {
		name   string
		owner  string
		repo   string
		number int
	}{
		{name: "owner が空", owner: "", repo: "hello-world", number: 188},
		{name: "repo が空", owner: "octocat", repo: "", number: 188},
		{name: "番号が0", owner: "octocat", repo: "hello-world", number: 0},
		{name: "番号が負", owner: "octocat", repo: "hello-world", number: -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := herdr.IssueLabel(c.owner, c.repo, c.number); got != "" {
				t.Fatalf("空文字にならない: got %q", got)
			}
		})
	}
}
