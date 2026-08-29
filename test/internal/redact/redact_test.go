// **公開の issue に、手元の絶対パスをそのまま書かないための検査である**（issue #75 / 設計 3-73）。
//
// **利用者名は個人情報である。**`/home/<利用者名>/…` を書いてしまうと、issue のコメントは
// 編集履歴が残るので取り消せない。**home で始まるパスは `~` に縮める。**
//
// **綴り方は4つあり、4つとも縮める。**home をそのまま書いた形、symlink を解いた形、
// **`/` を `-` に置き換えた形**、**`/` と `.` と `_` を `-` に置き換えた形**
// （どちらも Claude Code の会話の記録の置き場所の名前になりうる）。
//
// **縮めすぎも落とす。**`/home/alice` を縮める場面で `/home/alice2` や `/mnt/home/alice`
// まで縮めると、人間は存在しない場所を見に行くことになる。
package redact_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/redact"
)

// shortened は PathsWithHome を呼び、縮められなかったらそこでテストを落とす。
//
// **エラーを握り潰さない。**縮める対象として使えない home が渡ると
// `redact.ErrUnusableHome` が返る。**それを無視すると、何も縮めていない結果を
// 「期待どおり」と読んでしまう。**
//
// t: 呼び出し元のテスト。
// body: 縮める本文。
// home: 縮める対象の home。
// 戻り値: 縮めたあとの本文。
func shortened(t *testing.T, body, home string) string {
	t.Helper()
	got, err := redact.PathsWithHome(body, home)
	if err != nil {
		t.Fatalf("縮められませんでした（home=%q）: %v", home, err)
	}
	return got
}

// TestPathsWithHome_homeで始まるパスを縮める は、
// 公開の issue に出しては困る形が、どれも `~` に縮むことを確かめる。
//
// 目的: issue #75 の「利用者名を含むパスが公開される」を塞いだことを示す。
// 与える情報: continuo が実際に組み立てる文面と同じ囲み方をしたパス。
// 成功条件: 縮めた結果が期待どおりで、home の綴りが1文字も残らないこと。
func TestPathsWithHome_homeで始まるパスを縮める(t *testing.T) {
	const home = "/home/alice"

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "home そのもの",
			in:   "場所は /home/alice です",
			want: "場所は ~ です",
		},
		{
			name: "backtick で囲んだ会話の記録",
			in:   "- Claude Code の会話の記録: `/home/alice/.claude/projects/x/y.jsonl`",
			want: "- Claude Code の会話の記録: `~/.claude/projects/x/y.jsonl`",
		},
		{
			name: "全角の括弧で囲んだ worktree",
			in:   "worktree を片付けずに残しました（/home/alice/worktrees/issue-1）。",
			want: "worktree を片付けずに残しました（~/worktrees/issue-1）。",
		},
		{
			name: "1つの本文に何度出ても全部縮む",
			in:   "`/home/alice/a` と `/home/alice/b`",
			want: "`~/a` と `~/b`",
		},
		{
			name: "行の先頭",
			in:   "/home/alice/x\n次の行",
			want: "~/x\n次の行",
		},
		{
			name: "末尾がスラッシュ",
			in:   "`/home/alice/`",
			want: "`~/`",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shortened(t, c.in, home)
			if got != c.want {
				t.Fatalf("縮め方が違う:\n 入力: %s\n 結果: %s\n 期待: %s", c.in, got, c.want)
			}
			if strings.Contains(got, home) {
				t.Fatalf("home がまだ残っている: %s", got)
			}
		})
	}
}

// TestPathsWithHome_home以外は縮めない は、
// 前置きが同じだけの別のパスを巻き込まないことを確かめる。
//
// 目的: 【調べるところ】が「存在しない場所」を指さないようにする。
// **縮めすぎると、人間は case を潰しに行けなくなる。**
// 与える情報: home を部分文字列として含むが、home ではないパス。
// 成功条件: 入力がそのまま返ること。
func TestPathsWithHome_home以外は縮めない(t *testing.T) {
	const home = "/home/alice"

	cases := []struct {
		name string
		in   string
	}{
		{name: "前置きが同じだけの別の利用者", in: "`/home/alice2/x`"},
		{name: "ハイフンで続く別のディレクトリ", in: "`/home/alice-old/x`"},
		{name: "別の親の下にある同じ並び", in: "`/mnt/home/alice/x`"},
		{name: "home の外", in: "`/opt/continuo/bin/continuo`"},
		{name: "パスが1つも無い", in: "Status を **Ready → Blocked** へ動かしました。"},
		{name: "綴り直した形で続く別の利用者", in: "`-home-alice2-worktrees-issue-1`"},
		{name: "綴り直した形が名前の途中で終わる", in: "`-home-alice_old-wt`"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shortened(t, c.in, home); got != c.in {
				t.Fatalf("縮めてはいけないものを縮めた:\n 入力: %s\n 結果: %s", c.in, got)
			}
		})
	}
}

// TestPathsWithHome_2つ目以降の一致でも境界を見る は、
// **同じ本文に一致が2つ以上あるときも、直前の1文字を読めていること**を確かめる。
//
// 目的: 走査のたびに文字列を切り詰めると、**2つ目の一致が行頭にあるものとして通る。**
// `/mnt/home/alice/home/alice/x` の2つ目がそれに当たり、
// **縮めてはいけない側が `/mnt/home/alice~/x` に縮む**（利用者名を残したまま、
// 案内としては壊れたパスになる）。
//
// 与える情報: 縮めない一致のすぐ後ろに、もう1つ一致が続く本文。
// 成功条件: どちらも縮まないこと。**`~` が1文字も出ないこと。**
func TestPathsWithHome_2つ目以降の一致でも境界を見る(t *testing.T) {
	const home = "/home/alice"

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "縮めない一致の直後にもう1つ続く",
			in:   "`/mnt/home/alice/home/alice/x`",
			want: "`/mnt/home/alice/home/alice/x`",
		},
		{
			name: "縮める一致の直後に同じ並びが続く",
			in:   "`/home/alice/home/alice/x`",
			want: "`~/home/alice/x`",
		},
		{
			name: "綴り直した形でも直前の1文字を見る",
			in:   "`-mnt-home-alice-home-alice-x`",
			want: "`-mnt-home-alice-home-alice-x`",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shortened(t, c.in, home); got != c.want {
				t.Fatalf("境界の判定が2つ目以降で効いていない:\n 入力: %s\n 結果: %s\n 期待: %s",
					c.in, got, c.want)
			}
		})
	}
}

// TestPathsWithHome_使えないhomeは何もせずエラーを返す は、
// home が使えない値のときに本文を壊さず、**黙って通しもしない**ことを確かめる。
//
// 目的: **すべての絶対パスが `~` に化ける事故を防ぐ。**`/` を home として渡されると、
// `/opt/continuo` まで `~opt/continuo` になり、コメントが読めなくなる。
// **同時に、何も縮めなかったことを呼び出し側へ伝える。**伝えないと、
// 絶対パスが警告1行も無いまま公開の issue へ出る。
//
// 与える情報: 空・`/`・相対パスの home。
// 成功条件: どれも入力がそのまま返り、`redact.ErrUnusableHome` が返ること。
func TestPathsWithHome_使えないhomeは何もせずエラーを返す(t *testing.T) {
	const body = "`/home/alice/x` と `/opt/continuo/bin/continuo`"

	for _, home := range []string{"", "/", "//", "home/alice", "relative/path"} {
		t.Run("home="+home, func(t *testing.T) {
			got, err := redact.PathsWithHome(body, home)
			if got != body {
				t.Fatalf("本文を書き換えた（home=%q）:\n 結果: %s", home, got)
			}
			if !errors.Is(err, redact.ErrUnusableHome) {
				t.Fatalf("縮められなかったことが伝わっていない（home=%q）: err=%v", home, err)
			}
		})
	}
}

// TestPathsWithHome_末尾のスラッシュは無視する は、
// home の綴り方の揺れで結果が変わらないことを確かめる。
//
// 目的: 設定や環境変数から来る home は `/home/alice` とも `/home/alice/` とも書かれる。
// 与える情報: 末尾にスラッシュを付けた home。
// 成功条件: 付けない場合と同じ結果になること。
func TestPathsWithHome_末尾のスラッシュは無視する(t *testing.T) {
	const body = "`/home/alice/.claude/projects/x.jsonl`"
	const want = "`~/.claude/projects/x.jsonl`"

	if got := shortened(t, body, "/home/alice/"); got != want {
		t.Fatalf("結果: %s\n期待: %s", got, want)
	}
}

// TestPathsWithHome_綴り直したhomeも縮める は、
// **issue #75 が挙げた当の例**が縮むことを確かめる。
//
// 目的: Claude Code の会話の記録は
// `~/.claude/projects/<cwd を綴り直したもの>/<セッション UUID>.jsonl` にある。
// **その真ん中のディレクトリ名に、利用者名が丸ごと入る。**前半を `~` にしただけでは、
// 同じ行の後半で利用者名が公開されたままになる。
// 与える情報: issue #75 の本文が挙げているのと同じ形のパス。
// 成功条件: `alice` が1文字も残らないこと。
func TestPathsWithHome_綴り直したhomeも縮める(t *testing.T) {
	const home = "/home/alice"

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "会話の記録",
			in:   "- Claude Code の会話の記録: `/home/alice/.claude/projects/-home-alice-worktrees-issue-1/1f2e.jsonl`",
			want: "- Claude Code の会話の記録: `~/.claude/projects/~-worktrees-issue-1/1f2e.jsonl`",
		},
		{
			name: "綴り直した形の前がハイフン",
			in:   "`~/.claude/projects/-tmp-x--home-alice-wt/1f2e.jsonl`",
			want: "`~/.claude/projects/-tmp-x-~-wt/1f2e.jsonl`",
		},
		{
			name: "綴り直した形が名前の終わり",
			in:   "`-home-alice`",
			want: "`~`",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shortened(t, c.in, home)
			if got != c.want {
				t.Fatalf("縮め方が違う:\n 入力: %s\n 結果: %s\n 期待: %s", c.in, got, c.want)
			}
			if strings.Contains(got, "alice") {
				t.Fatalf("利用者名が残っている: %s", got)
			}
		})
	}
}

// TestPathsWithHome_利用者名にドットや下線があっても綴り直した形を縮める は、
// **`/Users/first.last` の機械で会話の記録の置き場所が縮むこと**を確かめる。
//
// 目的: **Claude Code は `/` だけでなく `.` と `_` も `-` に変える。**
// `/` だけを見ていると `-Users-john-doe-…` に1つも当たらず、
// **`-Users-john-doe-` の部分がそのまま公開される。**
// **`/Users/first.last` は、会社で使う Mac の既定の形である。**
//
// **`.` と `_` が混ざる利用者名では、置き換えの組み合わせが4通りある。**
// `/Users/ann_b.c` は `-Users-ann-b-c` にも `-Users-ann_b.c` にも
// `-Users-ann_b-c` にも `-Users-ann-b.c` にもなりうる。
// **綴り直す規則を持っているのは Claude Code であり、こちらは版を選べない。**
//
// 与える情報: 利用者名に `.` を持つ home、`_` を持つ home、その両方を持つ home、
// および Claude Code が作りうる置き場所の名前の綴り。
// 成功条件: 利用者名が1文字も残らないこと。
func TestPathsWithHome_利用者名にドットや下線があっても綴り直した形を縮める(t *testing.T) {
	cases := []struct {
		name    string
		home    string
		in      string
		want    string
		leaking string
	}{
		{
			name:    "ドットを含む利用者名",
			home:    "/Users/john.doe",
			in:      "- 会話の記録: `/Users/john.doe/.claude/projects/-Users-john-doe-worktrees-issue-1/1f2e.jsonl`",
			want:    "- 会話の記録: `~/.claude/projects/~-worktrees-issue-1/1f2e.jsonl`",
			leaking: "john",
		},
		{
			name:    "下線を含む利用者名",
			home:    "/Users/john_doe",
			in:      "- 会話の記録: `/Users/john_doe/.claude/projects/-Users-john-doe-worktrees-issue-1/1f2e.jsonl`",
			want:    "- 会話の記録: `~/.claude/projects/~-worktrees-issue-1/1f2e.jsonl`",
			leaking: "john",
		},
		{
			name:    "ドットのまま綴られた置き場所も縮める",
			home:    "/Users/john.doe",
			in:      "`-Users-john.doe-worktrees-issue-1`",
			want:    "`~-worktrees-issue-1`",
			leaking: "john",
		},
		{
			name:    "ドットと下線が混ざり、両方が置き換わった形",
			home:    "/Users/ann_b.c",
			in:      "`-Users-ann-b-c-worktrees-issue-1`",
			want:    "`~-worktrees-issue-1`",
			leaking: "ann",
		},
		{
			name:    "ドットと下線が混ざり、どちらも置き換わらなかった形",
			home:    "/Users/ann_b.c",
			in:      "`-Users-ann_b.c-worktrees-issue-1`",
			want:    "`~-worktrees-issue-1`",
			leaking: "ann",
		},
		{
			name:    "ドットと下線が混ざり、ドットだけが置き換わった形",
			home:    "/Users/ann_b.c",
			in:      "`-Users-ann_b-c-worktrees-issue-1`",
			want:    "`~-worktrees-issue-1`",
			leaking: "ann",
		},
		{
			name:    "ドットと下線が混ざり、下線だけが置き換わった形",
			home:    "/Users/ann_b.c",
			in:      "`-Users-ann-b.c-worktrees-issue-1`",
			want:    "`~-worktrees-issue-1`",
			leaking: "ann",
		},
		{
			name:    "スラッシュを残したまま下線だけが置き換わった形",
			home:    "/Users/ann_b.c",
			in:      "- worktree: `/Users/ann-b.c/worktrees/issue-1`",
			want:    "- worktree: `~/worktrees/issue-1`",
			leaking: "ann",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shortened(t, c.in, c.home)
			if got != c.want {
				t.Fatalf("縮め方が違う:\n 入力: %s\n 結果: %s\n 期待: %s", c.in, got, c.want)
			}
			if strings.Contains(got, c.leaking) {
				t.Fatalf("利用者名が残っている: %s", got)
			}
		})
	}
}

// TestPathsWithHome_symlink越しのhomeも縮める は、
// **解決済みの絶対パスが縮むこと**を確かめる。
//
// 目的: 引き渡しの通知に載る subagent の記録のパスは、`filepath.EvalSymlinks` で
// 解決済みの値である（internal/orchestrator/transcript.go の `subagentDirOf` /
// `SubagentTranscriptsFor`）。**home が symlink 越しに指されている機械では、
// 解決後のパスが `$HOME` で始まらない。**縮まらないまま公開されてはならない。
// 与える情報: 実体のディレクトリを指す symlink を home として渡し、本文には解決後のパスを置く。
// 成功条件: 解決後のパスが `~` に縮むこと。
func TestPathsWithHome_symlink越しのhomeも縮める(t *testing.T) {
	real := t.TempDir()
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("実体のパスを解決できません: %v", err)
	}
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink を張れません: %v", err)
	}

	in := "- 作業していた場所: `" + filepath.Join(resolved, "worktrees", "issue-1") + "`"
	const want = "- 作業していた場所: `~/worktrees/issue-1`"

	if got := shortened(t, in, link); got != want {
		t.Fatalf("解決後のパスが縮んでいない:\n 入力: %s\n 結果: %s\n 期待: %s", in, got, want)
	}
}

// TestPaths_利用者のhomeを引いて縮める は、
// 引数なしの入り口が、動いている機械の home を見ることを確かめる。
//
// 目的: continuo が issue へ書くときに呼ぶのはこちらである。
// 与える情報: `HOME` を差し替えた状態で組み立てた、会話の記録の形の本文。
// 成功条件: home が `/` の綴りでも `-` の綴りでも `~` に縮み、エラーが返らないこと。
func TestPaths_利用者のhomeを引いて縮める(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dashed := strings.ReplaceAll(home+"/worktrees/issue-1", "/", "-")
	in := "- 会話の記録: `" + home + "/.claude/projects/" + dashed + "/1f2e.jsonl`"
	const want = "- 会話の記録: `~/.claude/projects/~-worktrees-issue-1/1f2e.jsonl`"

	got, err := redact.Paths(in)
	if err != nil {
		t.Fatalf("home を引けませんでした: %v", err)
	}
	if got != want {
		t.Fatalf("結果: %s\n期待: %s", got, want)
	}
	if strings.Contains(got, home) {
		t.Fatalf("home の綴りが残っている: %s", got)
	}
}

// TestPaths_homeを引けなければエラーを返す は、
// **縮められなかったことが呼び出し側に伝わる**ことを確かめる。
//
// 目的: Unix の `os.UserHomeDir` は `$HOME` しか見ない。環境を絞って起こされると
// home が引けず、**警告が無ければ絶対パスが黙って公開の issue へ出る。**
// **issue のコメントは編集履歴が残るので取り消せない。**
// 与える情報: `HOME` を空にした状態。
// 成功条件: エラーが返り、本文はそのまま返ること（投稿そのものは止めない）。
func TestPaths_homeを引けなければエラーを返す(t *testing.T) {
	t.Setenv("HOME", "")

	const in = "- 作業していた場所: `/home/alice/worktrees/issue-1`"
	got, err := redact.Paths(in)
	if err == nil {
		t.Fatal("home を引けないのにエラーが返っていません")
	}
	if got != in {
		t.Fatalf("本文を書き換えた:\n 結果: %s\n 期待: %s", got, in)
	}
}

// TestPaths_引けたhomeが使えなければエラーを返す は、
// **`os.UserHomeDir` が値を返しても、それが使えなければ警告が出る**ことを確かめる。
//
// 目的: `HOME=/` は `os.UserHomeDir` を素通りする。**そこで何も縮めずに
// エラーも返さないと、絶対パスが警告1行も無いまま公開の issue へ出る。**
// 引けなかった場合と、扱いを変える理由が無い。
//
// 与える情報: `HOME` を `/` にした状態。
// 成功条件: `redact.ErrUnusableHome` が返り、本文はそのまま返ること。
func TestPaths_引けたhomeが使えなければエラーを返す(t *testing.T) {
	// **`/` は実在する。**continuo が作るディレクトリでもない。
	// **確かめたいのは「この値を home として使えないこと」そのもの**なので、
	// t.TempDir() では置き換えられない（置き換えると使える home になってしまう）。
	// test-design:allow-fake-path
	t.Setenv("HOME", "/")

	const in = "- 作業していた場所: `/home/alice/worktrees/issue-1`"
	got, err := redact.Paths(in)
	if !errors.Is(err, redact.ErrUnusableHome) {
		t.Fatalf("使えない home が黙って通っている: err=%v", err)
	}
	if got != in {
		t.Fatalf("本文を書き換えた:\n 結果: %s\n 期待: %s", got, in)
	}
}
