package redact_test

import (
	"testing"

	"github.com/maimuzo/continuo/internal/redact"
)

// 目的: HomeRanges が、PathsWithHome が `~` に置き換える範囲を、置き換えずに返すことを固定する（issue #347）。
//
// **確認の画面の文は pane の幅で割れて届くので、行をつないだ文の上で範囲を探す。**
// 綴りと境界が PathsWithHome と食い違うと、割れた home が公開の issue に残るか、縮めすぎる。
//
// 与える情報: home を `/home/alice` とし、そのままの綴り・`-` で綴った形・別の利用者・別のパスの一部を含む文。
// 成功条件: 範囲を `~` に置き換えた結果が PathsWithHome の結果と一致し、`/home/alice2` と `/mnt/home/alice` を返さないこと。
func TestHomeRanges_PathsWithHomeと同じ範囲を返す(t *testing.T) {
	const home = "/home/alice"
	cases := []string{
		"cat /home/alice/notes.txt",
		"see ~/.claude/projects/-home-alice-worktrees-issue-1/x.jsonl",
		"/home/alice2/x と /mnt/home/alice/y は別のもの",
		"/home/alice",
		"two /home/alice/a and /home/alice/b",
	}
	for _, body := range cases {
		ranges := redact.HomeRanges(body, home)
		got := ""
		last := 0
		for _, r := range sortRanges(ranges) {
			got += body[last:r[0]] + "~"
			last = r[1]
		}
		got += body[last:]
		want, err := redact.PathsWithHome(body, home)
		if err != nil {
			t.Fatalf("PathsWithHome が失敗: %v", err)
		}
		if got != want {
			t.Errorf("範囲で置き換えた結果が PathsWithHome と違う:\n body: %q\n got:  %q\n want: %q", body, got, want)
		}
	}
	if r := redact.HomeRanges("x", "/"); r != nil {
		t.Errorf("使えない home なのに範囲を返した: %v", r)
	}
}

// sortRanges は範囲を開始の位置の順に並べる（挿入ソート。数件しか無い）。
func sortRanges(in [][2]int) [][2]int {
	out := append([][2]int{}, in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j][0] < out[j-1][0]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
