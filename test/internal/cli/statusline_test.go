// `continuo statusline` サブコマンドの入口の検査である（issue #284）。
//
// **Claude Code がステータスラインを描き直すたびに exec するコマンドである。**
// hook と同じく、新しい本体が書いた設定ファイルを別の版の実行ファイルが読むことがあるので、
// 引数が読めなくても終了コード 0 と固定の1行 `continuo` を守ることを確かめる。
package cli_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/daemon"
	"github.com/maimuzo/continuo/internal/statuslineclient"
	"github.com/maimuzo/continuo/internal/statuslineserver"
)

// statuslineInput は Claude Code がステータスラインのコマンドへ渡す入力の見本である。
//
// continuo が読む4つの欄（`session_id`・`cost.total_api_duration_ms`・
// `rate_limits.five_hour`・`rate_limits.seven_day`）のほかに、読まない欄（`model`）も混ぜる。
const statuslineInput = `{"session_id":"0f3c-sess","model":{"id":"claude-haiku"},` +
	`"cost":{"total_api_duration_ms":10728.6},` +
	`"rate_limits":{"five_hour":{"used_percentage":3.5,"resets_at":1790338200},` +
	`"seven_day":{"used_percentage":41,"resets_at":1790800000}}}`

// statuslineReceiveTimeout は、偽の受け口が1行を受け取るまで待つ上限である。
const statuslineReceiveTimeout = 5 * time.Second

// countingDaemonDeps は、常駐の本体が呼ばれた回数を数える Deps を返す。
//
// **`statusline` の分岐が `runMain` へ落ちると、常駐の本体を起動しようとする。**
// 本物を起動させないため、呼ばれたら数えるだけの偽物へ差し替える。
//
// calls: 呼ばれた回数を足す先。
// 戻り値: DaemonRun だけを差し替えた Deps。
func countingDaemonDeps(calls *int) cli.Deps {
	return cli.Deps{
		DaemonRun: func(_ context.Context, _ daemon.Options) error {
			*calls++
			return nil
		},
	}
}

// newStatuslineSink は、`sl.sock` の代わりに本物の Unix socket を1つ listen する。
//
// **socket のパスは短く保つ**（macOS の Unix domain socket のパス長の上限は103バイト）。
// t.TempDir() はテスト名を含んで長くなるので、os.MkdirTemp で作る。
//
// t: 呼び出し元のテスト。後始末を t.Cleanup に登録する。
// 戻り値の1つ目: socket の絶対パス。
// 戻り値の2つ目: 受け取った行を1行ずつ流すチャネル。
func newStatuslineSink(t *testing.T) (string, <-chan string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "slcli")
	if err != nil {
		t.Fatalf("一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socketPath := filepath.Join(dir, "sl.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("偽の受け口を listen できません（%s）: %v", socketPath, err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	received := make(chan string, 16)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_ = c.SetReadDeadline(time.Now().Add(statuslineReceiveTimeout))
				line, err := bufio.NewReader(c).ReadString('\n')
				if len(line) > 0 || err == nil {
					received <- line
				}
			}(conn)
		}
	}()
	return socketPath, received
}

// TestRunStatusline_本体へ落ちずに固定の1行を出して0で終わる は、分岐があることを確かめる。
//
// **`statusline` の分岐が無いと、引数は `runMain` へ落ち、`--socket` が知らないフラグとして
// 終了コード 2 になる。**ステータスラインの出力も変わり、画面の版が動いて stall の判定を狂わせうる。
//
// 目的: `continuo statusline --socket <絶対パス>` が常駐の本体を起動せず、固定の1行
// `continuo` だけを出して終了コード 0 で終わること。受け口が居なくても同じであること。
// 与える情報: 存在しない socket の絶対パスと、使用率を含む標準入力。
// 成功条件: 終了コードが 0、標準出力が `continuo\n` ちょうど、標準エラーが空、
// 常駐の本体が1回も呼ばれないこと。
func TestRunStatusline_本体へ落ちずに固定の1行を出して0で終わる(t *testing.T) {
	dir, err := os.MkdirTemp("", "slnone")
	if err != nil {
		t.Fatalf("一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "sl.sock")

	var calls int
	code, stdout, stderr := runCLIWith(countingDaemonDeps(&calls),
		[]string{"statusline", "--socket", socketPath}, statuslineInput)

	if code != 0 {
		t.Errorf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if stdout != statuslineclient.Output+"\n" {
		t.Errorf("固定の1行を出していない: %q", stdout)
	}
	if stderr != "" {
		t.Errorf("標準エラーへ何か出している: %q", stderr)
	}
	if calls != 0 {
		t.Errorf("常駐の本体を %d 回呼んでいる（runMain へ落ちている）", calls)
	}
}

// TestRunStatusline_引数が読めなくても固定の1行を出して0で終わる は、引数の誤りの扱いを確かめる。
//
// **hook と違い、引数の誤りでも 0 で終える。**ステータスラインのコマンドは描き直すたびに
// exec されるので、版の混在で引数が読めなくなっても、画面の出力を変えず、Claude Code を止めない。
//
// 目的: `--socket` が無い・値が無い・相対パス・知らないフラグ、のどれでも、何も送らずに
// 固定の1行を出して 0 で終わり、常駐の本体へ落ちないこと。
// 与える情報: 4通りの引数と、使用率を含む標準入力。
// 成功条件: どれも終了コードが 0、標準出力が `continuo\n` ちょうど、標準エラーが空、
// 常駐の本体が1回も呼ばれないこと。
func TestRunStatusline_引数が読めなくても固定の1行を出して0で終わる(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "socketの指定が無い", args: []string{"statusline"}},
		{name: "socketの値が無い", args: []string{"statusline", "--socket"}},
		{name: "相対パスのsocket", args: []string{"statusline", "--socket", "./sl.sock"}},
		{name: "知らないフラグ", args: []string{"statusline", "--no-such-flag", "--socket", "/nonexistent/sl.sock"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			code, stdout, stderr := runCLIWith(countingDaemonDeps(&calls), tc.args, statuslineInput)
			if code != 0 {
				t.Errorf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
			}
			if stdout != statuslineclient.Output+"\n" {
				t.Errorf("固定の1行を出していない: %q", stdout)
			}
			if stderr != "" {
				t.Errorf("標準エラーへ何か出している: %q", stderr)
			}
			if calls != 0 {
				t.Errorf("常駐の本体を %d 回呼んでいる（runMain へ落ちている）", calls)
			}
		})
	}
}

// TestRunStatusline_相対パスのsocketには何も送らない は、相対パスを使わないことを確かめる。
//
// **ステータスラインのコマンドは Claude Code が起動するので、いまいるディレクトリを当てにできない。**
// 相対パスを受け付けると、worktree の中の別の socket へ書きうる。
//
// 目的: `--socket` に相対パスを渡したとき、そのパスに listen している受け口へ1行も送らないこと。
// 与える情報: 本物の Unix socket を listen したディレクトリへ移ったうえでの `--socket sl.sock`。
// 成功条件: 終了コードが 0 で固定の1行を出し、受け口が1行も受け取らないこと。
func TestRunStatusline_相対パスのsocketには何も送らない(t *testing.T) {
	socketPath, received := newStatuslineSink(t)
	t.Chdir(filepath.Dir(socketPath))

	code, stdout, _ := runCLI([]string{"statusline", "--socket", filepath.Base(socketPath)}, statuslineInput)
	if code != 0 {
		t.Errorf("終了コードが 0 でない: %d", code)
	}
	if stdout != statuslineclient.Output+"\n" {
		t.Errorf("固定の1行を出していない: %q", stdout)
	}
	select {
	case line := <-received:
		t.Errorf("相対パスの socket へ送っている: %q", line)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestRunStatusline_標準入力の使用率を1行で受け口へ送る は、本物の socket へ届くことを確かめる。
//
// **本体はこの1行でしか使用率を知らない。**欄の名前か値の写し方がずれると、入札が
// 古い値か0のまま進む。
//
// 目的: 標準入力の JSON から4つの欄を抜き出し、改行で終わる1行として `--socket` の
// Unix socket へ送り、固定の1行を出して 0 で終わること。
// 与える情報: 本物の Unix socket を listen した絶対パスと、使用率を含む標準入力。
// 成功条件: 受け口が1行を受け取り、その行が改行で終わり、`statuslineserver.Line` として
// 読むと session_id・api_ms（小数は切り捨て）・five_hour・seven_day が標準入力と一致すること。
// 終了コードが 0、標準出力が `continuo\n` ちょうどであること。
func TestRunStatusline_標準入力の使用率を1行で受け口へ送る(t *testing.T) {
	socketPath, received := newStatuslineSink(t)

	code, stdout, stderr := runCLI([]string{"statusline", "--socket", socketPath}, statuslineInput)
	if code != 0 {
		t.Errorf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if stdout != statuslineclient.Output+"\n" {
		t.Errorf("固定の1行を出していない: %q", stdout)
	}

	var line string
	select {
	case line = <-received:
	case <-time.After(statuslineReceiveTimeout):
		t.Fatal("受け口が1行も受け取らなかった")
	}
	if !strings.HasSuffix(line, "\n") {
		t.Errorf("改行で終わっていない: %q", line)
	}
	var got statuslineserver.Line
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("送った行を JSON として読めない: %v（%q）", err, line)
	}
	if got.SessionID != "0f3c-sess" {
		t.Errorf("session_id が違う: %q", got.SessionID)
	}
	if got.APIMs != 10728 {
		t.Errorf("api_ms が違う: %d", got.APIMs)
	}
	if got.FiveHour == nil || got.FiveHour.UsedPercentage != 3.5 || got.FiveHour.ResetsAt != 1790338200 {
		t.Errorf("five_hour が違う: %+v", got.FiveHour)
	}
	if got.SevenDay == nil || got.SevenDay.UsedPercentage != 41 || got.SevenDay.ResetsAt != 1790800000 {
		t.Errorf("seven_day が違う: %+v", got.SevenDay)
	}
}

// TestRun_ヘルプの一覧にstatuslineがありallowKeychainAccessが無い は、サブコマンドの一覧を確かめる。
//
// **利用者は、何が使えるかを `continuo --help` の一覧からしか知れない。**
// 消した `allow-keychain-access` が残っていると、叩いて `runMain` へ落ちる。
//
// 目的: `continuo --help` の一覧に `statusline` が載り、`allow-keychain-access` が載らないこと。
// 与える情報: `--help` だけ。
// 成功条件: 終了コードが 0 で、出力に `statusline` を含み、`allow-keychain-access` を含まないこと。
func TestRun_ヘルプの一覧にstatuslineがありallowKeychainAccessが無い(t *testing.T) {
	var calls int
	code, stdout, stderr := runCLIWith(countingDaemonDeps(&calls), []string{"--help"}, "")
	if code != 0 {
		t.Errorf("終了コードが 0 でない: %d", code)
	}
	if calls != 0 {
		t.Errorf("--help なのに常駐の本体を %d 回呼んでいる", calls)
	}
	usage := stdout + stderr
	if !strings.Contains(usage, "statusline") {
		t.Errorf("一覧に statusline が無い:\n%s", usage)
	}
	if strings.Contains(usage, "allow-keychain-access") {
		t.Errorf("消した allow-keychain-access が一覧に残っている:\n%s", usage)
	}
}
