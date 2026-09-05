// `continuo prompt --show --url` の検査である
// （issue #183（エージェントへ実際に送られる文面を、事前に確かめられない（変数が展開されない）））。
//
// **外部へ1回も接続しない。**issue を引く処理は `cli.Deps` で差し替える。
package cli_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/tracker"
)

// promptIssueURL は、検査で使う issue の URL である。
const promptIssueURL = "https://github.com/octocat/hello-world/issues/42"

// fakePromptIssue は、ボードから引けたことにする issue を1件返す。
//
// **`Identifier` は URL から作った識別子と一致させる。**内訳の1行に出るためである。
//
// 戻り値: 変数展開に使う issue。
func fakePromptIssue() tracker.Issue {
	url := promptIssueURL
	branch := "work/issue-42"
	return tracker.Issue{
		Identifier: "octocat/hello-world#42",
		Owner:      "octocat",
		Repo:       "hello-world",
		Number:     42,
		URL:        &url,
		Title:      "検査に使う issue",
		State:      "Ready",
		Labels:     []string{"bug"},
		BranchName: &branch,
	}
}

// promptFetchOK は、issue が1件引けたことにする差し替えである。
//
// 戻り値: 引けた issue と、組み立てられたことを表す true。
func promptFetchOK(
	_ context.Context, _ config.TrackerConfig, _, _ string,
) (tracker.Issue, bool, error) {
	return fakePromptIssue(), true, nil
}

// 目的: `--url` が、変数をその issue の値で展開した文面を標準出力へ出すことを固定する
// （issue #183）。
//
// **なぜ要るか。**`continuo prompt --show` は変数を展開しないので、
// `{{.issue.identifier}}` や `{{if .attempt}}` がそのまま出る。
// **`WORKFLOW.md` の本文にテンプレートを書いた人は、それが実際にどう展開されるかを事前に確かめられない。**
// **間違いに気づくのは、エージェントが動き出したあとになる。**
//
// 与える情報: `{{.issue.identifier}}` と `{{.issue.title}}` を使う本文と、`--url`。
// 成功条件: 終了コードが 0 で、標準出力に展開後の値が入り、`{{` が1つも残っていないこと。
func TestPromptURL_変数をその_issue_の値で展開する(t *testing.T) {
	dir := writeWorkflowFor(t)
	setBody(t, dir, "## 固有\n\n{{.issue.identifier}} / {{.issue.title}} / {{.push_branch}}\n")

	deps := cli.Deps{PromptFetchIssue: promptFetchOK}
	code, stdout, stderr := runCLIWith(deps,
		[]string{"prompt", "--show", "--url", promptIssueURL, dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	for _, want := range []string{"octocat/hello-world#42", "検査に使う issue", "work/issue-42"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("展開した値 %q が標準出力にありません", want)
		}
	}
	// **`{{` が1つでも残っていたら、確かめたい当のものが確かめられていない。**
	if strings.Contains(stdout, "{{") {
		i := strings.Index(stdout, "{{")
		t.Errorf("変数が展開されずに残っています: %q", stdout[i:min(i+60, len(stdout))])
	}
	// **内訳は標準エラーへ出す。**標準出力は送る文面と1バイトも違わないままにする。
	if !strings.Contains(stderr, "octocat/hello-world#42") {
		t.Errorf("どの issue の値で展開したかが内訳に出ていません: %q", stderr)
	}
}

// 目的: 何回目の試行として展開したかを、内訳へ必ず出すことを固定する（issue #183）。
//
// **なぜ要るか。**`--attempt` を省くと1回目として展開するので、
// 組み込みの `## 7-5. これは {{.attempt}} 回目の試行です` は出ない。
// **出さないと、利用者はそれを「文面から消えた」と読み違える。**
//
// 与える情報: `--url` だけを渡した場合と、`--attempt 3` を足した場合。
// 成功条件: どちらも内訳に回数が出て、`--attempt 3` では 7-5 の節が本文に現れること。
func TestPromptURL_何回目として展開したかを内訳に出す(t *testing.T) {
	dir := writeWorkflowFor(t)
	setBody(t, dir, "")
	deps := cli.Deps{PromptFetchIssue: promptFetchOK}

	code, stdout, stderr := runCLIWith(deps,
		[]string{"prompt", "--show", "--url", promptIssueURL, dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	if !strings.Contains(stderr, "1") || !strings.Contains(stderr, "試行") {
		t.Errorf("1回目として展開したことが内訳に出ていません: %q", stderr)
	}
	if strings.Contains(stdout, "回目の試行です") {
		t.Error("1回目なのに 7-5 の節が出ています（{{if .attempt}} が偽になっていません）")
	}

	code, stdout, stderr = runCLIWith(deps,
		[]string{"prompt", "--show", "--url", promptIssueURL, "--attempt", "3", dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	if !strings.Contains(stdout, "3 回目の試行です") {
		t.Error("--attempt 3 なのに 7-5 の節が出ていません")
	}
}

// 目的: `--attempt 1` が、本番に存在しない文面を見せないことを固定する（issue #183）。
//
// **なぜ要るか。**本番で `attempt` に値が入るのは、やり直しのときだけである。
// `internal/orchestrator/turn.go` は `snap.RetryCount > 0` のときに `RetryCount + 1` を渡すので、
// **入りうる最小値は 2 である。****1 が入ることは無い。**
//
// **1 をそのまま渡すと `{{if .attempt}}` が真になり、`## 7-5. これは 1 回目の試行です` が出る。**
// **「本当に送られる文面」を名乗るコマンドが、送られない文面を見せてはならない。**
//
// 与える情報: `--attempt 1` と、`--attempt` を省いた場合。
// 成功条件: どちらも 7-5 の節が出ず、同じ標準出力になること。
func TestPromptURL_attempt1は試行回数を渡さない(t *testing.T) {
	dir := writeWorkflowFor(t)
	setBody(t, dir, "")
	deps := cli.Deps{PromptFetchIssue: promptFetchOK}

	code, withOne, stderr := runCLIWith(deps,
		[]string{"prompt", "--show", "--url", promptIssueURL, "--attempt", "1", dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	if strings.Contains(withOne, "回目の試行です") {
		t.Error("--attempt 1 で 7-5 の節が出ています。本番でその文面が送られることはありません")
	}

	_, without, _ := runCLIWith(deps,
		[]string{"prompt", "--show", "--url", promptIssueURL, dir}, "")
	if withOne != without {
		t.Error("--attempt 1 と、--attempt を省いた場合で標準出力が違います。" +
			"どちらも「1回目」なので、同じ文面になるはずです")
	}

	// **内訳では、7-5 の節が出ない理由を言い切る。**
	// 出ないことを「文面から消えた」と読み違えさせない。
	if !strings.Contains(stderr, "1回目") {
		t.Errorf("1回目として展開したことが内訳に出ていません: %q", stderr)
	}
}

// 目的: 引数の誤りを、終了コード 2 で断ることを固定する（issue #183）。
//
// **`--builtin` と `--url` を同時に許さない。**`--builtin` の売りは
// 「`WORKFLOW.md` を1バイトも読まない」ことなのに、`--url` は front matter の
// `tracker.provider` を読まないと issue を引けない。
// **同時に許すと、その売りが消えたまま `--builtin` を名乗ることになる。**
//
// **pull request の URL は受け付けない。**pull request と issue は番号を共有するので、
// **受け付けると「pull request の URL を貼ったのに issue の文面が出る」ことになる。**
//
// 与える情報: 誤った組み合わせと誤った URL。
// 成功条件: どれも終了コードが 2 で、標準出力が空であること。
func TestPromptURL_引数の誤りは終了コード2で断る(t *testing.T) {
	dir := writeWorkflowFor(t)
	setBody(t, dir, "")
	// **GitHub を叩く前に断ることも確かめる。**叩いたら検査が落ちるようにしておく。
	called := 0
	deps := cli.Deps{PromptFetchIssue: func(
		_ context.Context, _ config.TrackerConfig, _, _ string,
	) (tracker.Issue, bool, error) {
		called++
		return fakePromptIssue(), true, nil
	}}

	for _, c := range []struct {
		name string
		args []string
	}{
		{"--builtin と同時", []string{"prompt", "--show", "--builtin", "--url", promptIssueURL}},
		{"pull request の URL", []string{"prompt", "--show", "--url",
			"https://github.com/octocat/hello-world/pull/42", dir}},
		{"issue の URL ではない", []string{"prompt", "--show", "--url", "https://example.com/", dir}},
		{"番号に前置ゼロ", []string{"prompt", "--show", "--url",
			"https://github.com/octocat/hello-world/issues/042", dir}},
		{"--attempt が0", []string{"prompt", "--show", "--url", promptIssueURL, "--attempt", "0", dir}},
		// **`--attempt` は変数を展開するときにしか効かない。**
		// **黙って捨てると、利用者は「効いた」と思ったまま違う文面を読む。**
		// このコマンド自身が「気づけない出力が、いちばん悪い落ち方である」を理由に、
		// 展開できなかったら断ると決めている。**同じ理由がそのまま当たる。**
		{"--attempt を --url 無しで渡す", []string{"prompt", "--show", "--attempt", "3", dir}},
		// **空の `--url` を「指定されていない」と同じ扱いにしてはならない。**
		// **変数を展開しないまま終了コード 0 で出すことになる。**
		// 環境変数が空のままスクリプトが叩くと、**成功したのと見分けが付かない。**
		{"--url が空", []string{"prompt", "--show", "--url", "", dir}},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, stdout, stderr := runCLIWith(deps, c.args, "")
			if code != 2 {
				t.Errorf("終了コードが %d です（2 を期待。stderr: %s）", code, stderr)
			}
			if stdout != "" {
				t.Errorf("断ったのに標準出力へ出しています: %q", stdout)
			}
		})
	}
	if called != 0 {
		t.Errorf("引数を断る前に GitHub を叩いています（%d 回）", called)
	}
}

// 目的: 引けなかったときに、展開せずに出さないことを固定する（issue #183）。
//
// **なぜ要るか。**このコマンドの目的は「本当に送られる文面を確かめる」ことである。
// **展開に失敗したものを出すと、`--url` を付けたのに付けなかったときと同じものが出て、
// 利用者はそれに気づけない。**気づけない出力が、いちばん悪い落ち方である。
//
// **「載っていません」とだけ言わない。**`FetchIssueByIdentifier` が偽を返す理由は5通りあり、
// Status 未設定は本番のボードでも104件中4件ある通常の状態である。
// **`Bootstrap` を通していないので、`status_field` の綴りがずれていると全件がそう見える。**
// **唯一の検出手段が `continuo doctor` なので、そこまで案内する。**
//
// 与える情報: 引けなかった場合と、ボードから組み立てられなかった場合。
// 成功条件: どちらも終了コードが 1 で、標準出力が空であること。
func TestPromptURL_引けなかったら何も出さずに断る(t *testing.T) {
	dir := writeWorkflowFor(t)
	setBody(t, dir, "")

	t.Run("ボードを読めない", func(t *testing.T) {
		deps := cli.Deps{PromptFetchIssue: func(
			_ context.Context, _ config.TrackerConfig, _, _ string,
		) (tracker.Issue, bool, error) {
			return tracker.Issue{}, false, errors.New("接続できません")
		}}
		code, stdout, stderr := runCLIWith(deps,
			[]string{"prompt", "--show", "--url", promptIssueURL, dir}, "")
		if code != 1 {
			t.Errorf("終了コードが %d です（1 を期待）", code)
		}
		if stdout != "" {
			t.Errorf("断ったのに標準出力へ出しています: %q", stdout)
		}
		if !strings.Contains(stderr, "接続できません") {
			t.Errorf("読めなかった理由が出ていません: %q", stderr)
		}
	})

	t.Run("ボードから組み立てられない", func(t *testing.T) {
		deps := cli.Deps{PromptFetchIssue: func(
			_ context.Context, _ config.TrackerConfig, _, _ string,
		) (tracker.Issue, bool, error) {
			return tracker.Issue{}, false, nil
		}}
		code, stdout, stderr := runCLIWith(deps,
			[]string{"prompt", "--show", "--url", promptIssueURL, dir}, "")
		if code != 1 {
			t.Errorf("終了コードが %d です（1 を期待）", code)
		}
		if stdout != "" {
			t.Errorf("断ったのに標準出力へ出しています: %q", stdout)
		}
		// **理由を1つに決めつけない。**Status 未設定も archive 済みもここへ来る。
		for _, want := range []string{"Status", "doctor"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("断りの文言に %q がありません（原因を絞り込めません）: %q", want, stderr)
			}
		}
	})
}

// 目的: 内訳の行数が、実際に標準出力へ出した行数と一致することを固定する（issue #183）。
//
// **なぜ要るか。**内訳の見出しは「送る文面の内訳」である。
// **`--url` のとき、標準出力へ出るのは変数展開したあとの文面である。**
// **展開する前の断片を数えると、その行数は嘘になる。**
//
// **`{{if .attempt}}` が実際に落ちる。**`--attempt` を付けないと
// `## 7-5. これは N 回目の試行です` の節が消えるので、**組み込みの後半の行数が変わる。**
// **直す前は6行ずれていた**（内訳は220行と出し、実際に出たのは214行だった）。
//
// 与える情報: `--attempt` を付けない `--url` の出力と、その内訳。
// 成功条件: 内訳が出した「組み込みのプロンプト（後半）」の行数が、
// 標準出力の後半の実際の行数と一致すること。
func TestPromptURL_内訳の行数が出した文面と一致する(t *testing.T) {
	dir := writeWorkflowFor(t)
	setBody(t, dir, "")

	deps := cli.Deps{PromptFetchIssue: promptFetchOK}
	code, stdout, stderr := runCLIWith(deps,
		[]string{"prompt", "--show", "--url", promptIssueURL, dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}

	want := breakdownTailLines(t, stderr)
	// **組み込みの後半は `# 5. 共通ルール` から始まる。**そこから最後までを数える。
	at := strings.Index(stdout, "# 5. 共通ルール")
	if at < 0 {
		t.Fatalf("標準出力に組み込みの後半が見つかりません")
	}
	got := len(strings.Split(strings.Trim(stdout[at:], "\n"), "\n"))
	if got != want {
		t.Errorf("内訳の行数が、出した文面と合っていません: 内訳=%d 行 / 実際=%d 行\n"+
			"見出しは「送る文面の内訳」なので、展開したあとを数えなければ嘘になります", want, got)
	}
}

// breakdownTailLines は、内訳から「組み込みのプロンプト（後半）」の行数を取り出す。
//
// t: テストコンテキスト。
// stderr: 内訳が出た標準エラーの中身。
// 戻り値: 内訳が主張している行数。
func breakdownTailLines(t *testing.T, stderr string) int {
	t.Helper()
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.Contains(line, "後半") && !strings.Contains(line, "second half") {
			continue
		}
		for _, f := range strings.Fields(line) {
			if n, err := strconv.Atoi(f); err == nil {
				return n
			}
		}
	}
	t.Fatalf("内訳に組み込みの後半の行がありません: %q", stderr)
	return 0
}

// 目的: `--url` を付けないときは、いままでどおり変数を展開しないことを固定する（issue #183）。
//
// **足した道が、既にある道を変えていないことを見る。**
//
// 与える情報: `--url` を付けない `continuo prompt --show`。
// 成功条件: 終了コードが 0 で、`{{.issue.identifier}}` がそのまま出ること。GitHub を叩かないこと。
func TestPromptURL_urlを付けなければ展開しない(t *testing.T) {
	dir := writeWorkflowFor(t)
	setBody(t, dir, "## 固有\n\n{{.issue.identifier}}\n")
	called := 0
	deps := cli.Deps{PromptFetchIssue: func(
		_ context.Context, _ config.TrackerConfig, _, _ string,
	) (tracker.Issue, bool, error) {
		called++
		return fakePromptIssue(), true, nil
	}}

	code, stdout, stderr := runCLIWith(deps, []string{"prompt", "--show", dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	if !strings.Contains(stdout, "{{.issue.identifier}}") {
		t.Error("--url を付けていないのに変数が展開されています")
	}
	if called != 0 {
		t.Errorf("--url を付けていないのに GitHub を叩いています（%d 回）", called)
	}
}

// 目的: 本文が丸ごと `{{if .attempt}}` の中にあるとき、内訳が
// 「本文 0 行」と「本文はありません」を同時に出さないことを固定する（issue #183）。
//
// **なぜ要るか。**行数は展開後、有無も展開後で決めているのに、
// **展開後に空になった本文の断片が並びに残るので、行数の行だけが 0 行で出てしまう。**
// **読む人は「本文が 0 行ある」と「本文が無い」を同時に見せられ、どちらが本当か決められない。**
//
// 与える情報: 本文を丸ごと `{{if .attempt}}` で囲った WORKFLOW.md と、`--attempt` を付けない `--url`。
// 成功条件: 標準エラーに「本文はありません」だけが出て、「本文  0 行」が出ないこと。
func TestPromptURL_展開して空になった本文は無いとだけ言う(t *testing.T) {
	dir := writeWorkflowFor(t)
	setBody(t, dir, "{{if .attempt}}## やり直しのときだけ読む\n\nここは 2 回目から出ます。\n{{end}}")

	deps := cli.Deps{PromptFetchIssue: promptFetchOK}
	code, _, stderr := runCLIWith(deps,
		[]string{"prompt", "--show", "--url", promptIssueURL, dir}, "")
	if code != 0 {
		t.Fatalf("終了コードが %d です（stderr: %s）", code, stderr)
	}
	if !strings.Contains(stderr, "本文はありません") {
		t.Errorf("本文が空なのに「本文はありません」が出ていません（stderr: %s）", stderr)
	}
	if strings.Contains(stderr, "本文  0 行") {
		t.Errorf("「本文はありません」と「本文  0 行」が同時に出ています（stderr: %s）", stderr)
	}
}

// 目的: URL の形の誤りを、`WORKFLOW.md` を読む前に断ることを固定する（issue #183）。
//
// **なぜ要るか。**設定を先に読むと、**設定が壊れている場所で URL を打ち間違えた人は
// 終了コード 1（設定を読めない）を受け取る。**
// [docs/FAQ.md](docs/FAQ.md) の表は「URL の形が違う → 2」と書いているので、
// **文書と振る舞いが食い違う。**引数の形の誤りは、いちばん安く判定できる。
//
// 与える情報: `WORKFLOW.md` が1つも無いディレクトリと、URL として読めない `--url`。
// 成功条件: 終了コードが 2 で、標準出力が空であること。
func TestPromptURL_URLの形は設定を読む前に断る(t *testing.T) {
	dir := t.TempDir() // WORKFLOW.md を置かない。config.Load は必ず落ちる

	called := 0
	deps := cli.Deps{PromptFetchIssue: func(
		_ context.Context, _ config.TrackerConfig, _, _ string,
	) (tracker.Issue, bool, error) {
		called++
		return tracker.Issue{}, false, nil
	}}

	code, stdout, stderr := runCLIWith(deps,
		[]string{"prompt", "--show", "--url", "not-a-url", dir}, "")
	if code != 2 {
		t.Errorf("終了コードが %d です（2 のはずです）。"+
			"設定を先に読むと、URL の打ち間違いが「設定を読めない」に化けます（stderr: %s）", code, stderr)
	}
	if stdout != "" {
		t.Errorf("標準出力に何か出ています: %q", stdout)
	}
	if called != 0 {
		t.Errorf("URL の形が違うのに GitHub を叩いています（%d 回）", called)
	}
}
