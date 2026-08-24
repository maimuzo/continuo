package abandon_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/abandon"
)

// 目的: issue の URL から owner・リポジトリ名・番号を取り出せることと、
// issue の URL になっていないものを受け付けないことを確認する（設計 3-4 の段2）。
// **pull request の URL は受け付けない。**continuo が worktree を用意した単位は issue で
// あり、pull request の番号では身元ファイルの issue_url と照合できない。
// 与える情報: 受け付けるべき URL と、受け付けてはならない URL。
// 成功条件: 受け付ける URL では `<owner>/<repo>#<番号>` の形の名前が取れること、
// 受け付けない URL ではエラーになること。
func TestParseIssueURL_受け付ける形と受け付けない形(t *testing.T) {
	accepted := map[string]string{
		"https://github.com/octocat/hello-world/issues/42":     "octocat/hello-world#42",
		"https://github.com/octocat/hello-world/issues/42/":    "octocat/hello-world#42",
		"http://ghe.example.test/octocat/hello-world/issues/7": "octocat/hello-world#7",
		"https://github.com/octocat/hello-world/issues/42?x=1": "octocat/hello-world#42",
	}
	for raw, want := range accepted {
		ref, err := abandon.ParseIssueURL(raw)
		if err != nil {
			t.Fatalf("%q を読めなかった: %v", raw, err)
		}
		if ref.Identifier() != want {
			t.Fatalf("%q から取れた名前が %q ではなく %q だった", raw, want, ref.Identifier())
		}
	}

	rejected := []string{
		"",
		"github.com/octocat/hello-world/issues/42",
		"ftp://github.com/octocat/hello-world/issues/42",
		"https://github.com/octocat/hello-world/pull/42",
		"https://github.com/octocat/hello-world/issues/0",
		"https://github.com/octocat/hello-world/issues/abc",
		"https://github.com/octocat/issues/42",
	}
	for _, raw := range rejected {
		if ref, err := abandon.ParseIssueURL(raw); err == nil {
			t.Fatalf("%q を受け付けてしまった（取れた名前: %s）", raw, ref.Identifier())
		}
	}
}

// 目的: 身元ファイルの issue_url との照合が、大文字小文字と末尾のスラッシュの
// 揺れを吸収し、**別の issue には一致しない**ことを確認する（設計 3-4 の段2）。
// **消す対象を決める照合なので、緩すぎれば別の issue の worktree を消す。**
// 与える情報: 同じ issue を指す書き方の揺れと、番号・リポジトリ・ホストが違う URL。
// 成功条件: 同じ issue を指すものだけ true、違うものは false になること。
func TestSameIssue_揺れは吸収し別のissueには一致しない(t *testing.T) {
	ref, err := abandon.ParseIssueURL("https://github.com/octocat/hello-world/issues/42")
	if err != nil {
		t.Fatalf("issue の URL を読めなかった: %v", err)
	}

	same := []string{
		"https://github.com/octocat/hello-world/issues/42",
		"https://github.com/OctoCat/Hello-World/issues/42",
		"https://GitHub.com/octocat/hello-world/issues/42/",
		" https://github.com/octocat/hello-world/issues/42 ",
	}
	for _, other := range same {
		if !ref.SameIssue(other) {
			t.Fatalf("同じ issue のはずの %q が一致しなかった", other)
		}
	}

	different := []string{
		"",
		"https://github.com/octocat/hello-world/issues/420",
		"https://github.com/octocat/another-repo/issues/42",
		"https://ghe.example.test/octocat/hello-world/issues/42",
		"これは URL ではない",
	}
	for _, other := range different {
		if ref.SameIssue(other) {
			t.Fatalf("別の issue のはずの %q が一致した", other)
		}
	}
}
