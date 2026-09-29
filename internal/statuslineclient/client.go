// Package statuslineclient は `continuo statusline` の本体である（issue #284 / issue #295）。
//
// Claude Code は、continuo が渡した設定ファイル（`--settings`）の `statusLine` に書いたコマンドを、
// ステータスラインを描き直すたびに実行し、標準入力へ JSON を渡す。このコマンドは、そこから
// 4つの欄（`session_id`・`cost.total_api_duration_ms`・`rate_limits.five_hour`・
// `rate_limits.seven_day`）を抜き出して1行にし、本体の `sl.sock` へ送る。
//
// **送ったあと（送れなくても）、利用者のステータスラインへ転送する**（設計 3-84）。
// 転送先は、continuo が着手のときに決めて issue ごとの設定ファイルの `env` の
// `CONTINUO_STATUSLINE_COMMAND` に書いたコマンドである。同じ標準入力で起動し、その出力を返す。
//
// **転送先が無いとき・転送のどの失敗でも、固定の1行 `continuo` を出して終了コード 0 で終える。**
// socket が無い・接続を断られた・期限切れ・JSON として読めない、は送信を諦めるだけで転送は続ける。
// ステータスラインのコマンドが失敗しても Claude Code の作業は止まらないが、
// 出力が変わると画面の版（`revision`）が動き、continuo の stall の判定を狂わせうる（設計 3-84c）。
// **逃がし先は持たない。**使用率は statusline取得でまた取り直せる。
package statuslineclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/maimuzo/continuo/internal/statuslineserver"
)

// EnvForwardCommand は転送先のコマンドを運ぶ環境変数の名前である（設計 3-84a）。
//
// **continuo が issue ごとの設定ファイルの `env` に書き、Claude Code がこのコマンドへ渡す。**
// 転送した子のプロセスからは外す（自分を呼び合わないため。設計 3-84b の段3）。
const EnvForwardCommand = "CONTINUO_STATUSLINE_COMMAND"

// MaxInputBytes は標準入力の上限である。
const MaxInputBytes = 1 << 20

// MaxForwardOutputBytes は転送先の標準出力のうち、そのまま出す上限である（設計 3-84b の段5）。
const MaxForwardOutputBytes = 64 << 10

// ForwardTimeout は転送先のコマンドを待つ上限である（設計 3-84b の段4）。
const ForwardTimeout = 5 * time.Second

// Output は、転送先が無いときと転送に失敗したときにステータスラインへ出す固定の1行である。
//
// **固定にする。**描き直しの中身で画面の版が動かないようにするためである。
const Output = "continuo"

// dialTimeout は socket への接続を待つ上限である。
const dialTimeout = 200 * time.Millisecond

// writeTimeout は1行を書き切るまでの上限である。
const writeTimeout = 500 * time.Millisecond

// forwardWaitDelay は、転送先が終わった（または止めた）あと、出力の pipe が閉じるのを待つ上限である。
//
// **子が背景へ回した孫が pipe を握っていると、これが無いと Cmd.Wait が返らない。**
const forwardWaitDelay = 500 * time.Millisecond

// shellPath は転送先を解釈するシェルである（Claude Code と同じ `/bin/sh -c`。設計 3-84 の実測）。
const shellPath = "/bin/sh"

// input はステータスラインの入力のうち、continuo が読む欄である。
type input struct {
	SessionID string `json:"session_id"`
	Cost      struct {
		TotalAPIDurationMs float64 `json:"total_api_duration_ms"`
	} `json:"cost"`
	RateLimits *struct {
		FiveHour *statuslineserver.Window `json:"five_hour"`
		SevenDay *statuslineserver.Window `json:"seven_day"`
	} `json:"rate_limits"`
}

// Run は標準入力を読んで1行を socket へ送り、転送先があればその出力を、無ければ固定の1行を出す
// （設計 3-84b）。
//
// stdin: ステータスラインの入力。
// stdout: ステータスラインの出力先。
// socketPath: `sl.sock` の絶対パス。
// forwardCommand: 転送先のコマンド（`CONTINUO_STATUSLINE_COMMAND` の値）。空なら転送しない。
// **環境変数はここで読まない。**呼び出し側（internal/cli の runStatusline）が読んで渡す。
// 戻り値: 終了コード。**いつも 0 である。**
func Run(stdin io.Reader, stdout io.Writer, socketPath, forwardCommand string) int {
	// 段1: 標準入力を読む（上限 1MiB）。
	data, err := io.ReadAll(io.LimitReader(stdin, MaxInputBytes+1))
	readable := err == nil && len(data) <= MaxInputBytes
	if readable {
		// **送れても送れなくても段2 へ進む。**
		send(data, socketPath)
	}
	// 段2: 転送先が空か、標準入力が読めない・上限を超えたら、固定の1行で終える。
	if forwardCommand == "" || !readable {
		fmt.Fprintln(stdout, Output)
		return 0
	}
	// 段3〜段5。
	out, ok := forward(data, forwardCommand)
	if !ok {
		fmt.Fprintln(stdout, Output)
		return 0
	}
	_, _ = stdout.Write(out)
	return 0
}

// send は入力から4つの欄を1行にして socket へ送る。**どの失敗でも何もせずに戻る。**
//
// data: ステータスラインの入力（上限以内）。
// socketPath: `sl.sock` の絶対パス。
func send(data []byte, socketPath string) {
	var in input
	if err := json.Unmarshal(data, &in); err != nil || in.SessionID == "" {
		return
	}
	line := statuslineserver.Line{
		SessionID: in.SessionID,
		APIMs:     int64(in.Cost.TotalAPIDurationMs),
	}
	if in.RateLimits != nil {
		line.FiveHour = in.RateLimits.FiveHour
		line.SevenDay = in.RateLimits.SevenDay
	}
	out, err := json.Marshal(line)
	if err != nil {
		return
	}
	out = append(out, '\n')
	conn, err := net.DialTimeout("unix", socketPath, dialTimeout)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, _ = conn.Write(out)
}

// forward は転送先のコマンドを `/bin/sh -c` で起動し、同じ入力を渡して標準出力を受ける（設計 3-84b の段3〜段5）。
//
// **新しいプロセスグループで起動する。**5秒で打ち切るときと、このプロセスが SIGTERM・SIGINT・SIGHUP を
// 受けたときは、子のグループごと SIGKILL で止める（Claude Code は実行中のスクリプトを次の更新で打ち切る）。
// 別のグループに置いた子は、このプロセスへ送られたシグナルでは止まらないためである。
// cwd は受け継ぐ。環境変数は EnvForwardCommand だけを外して受け継ぐ。標準エラーは捨てる。
//
// data: 子の標準入力へ渡すバイト列。
// command: 転送先のコマンド行。
// 戻り値の1つ目: 子の標準出力の先頭 MaxForwardOutputBytes バイト。
// 戻り値の2つ目: 子が時間内に終了コード 0 で終わり、標準出力が空でなければ true。
func forward(data []byte, command string) ([]byte, bool) {
	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer stopSignals()
	ctx, cancel := context.WithTimeout(ctx, ForwardTimeout)
	defer cancel()

	out := &cappedBuffer{limit: MaxForwardOutputBytes}
	cmd := exec.CommandContext(ctx, shellPath, "-c", command)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = out
	cmd.Stderr = nil // /dev/null へ捨てる
	cmd.Env = environWithout(os.Environ(), EnvForwardCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// 負の PID はプロセスグループ全体を指す。落とせないときは、せめて本人を落とす。
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = forwardWaitDelay

	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, false
	}
	if err != nil {
		// **終了コード 0 で終わったのに、背景へ回した孫が pipe を握っていただけなら成功として扱う。**
		if !errors.Is(err, exec.ErrWaitDelay) || cmd.ProcessState == nil || !cmd.ProcessState.Success() {
			return nil, false
		}
	}
	if len(out.buf) == 0 {
		return nil, false
	}
	return out.buf, true
}

// environWithout は環境変数の並びから name を外した写しを返す。
//
// env: `KEY=VALUE` の並び。
// name: 外す変数の名前。
// 戻り値: name を含まない新しい並び。
func environWithout(env []string, name string) []string {
	prefix := name + "="
	kept := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		kept = append(kept, kv)
	}
	return kept
}

// cappedBuffer は先頭の limit バイトだけを覚える io.Writer である。
//
// **上限を超えた分は捨てるが、エラーにはしない。**エラーにすると子の書き込みが失敗し、
// 子が非 0 で終わりうる（出力の先頭は出せるのに `continuo` になる）。
type cappedBuffer struct {
	limit int
	buf   []byte
}

// Write は io.Writer を満たす。
//
// p: 書き込まれた内容。
// 戻り値の1つ目: 受け取ったバイト数（常に len(p)）。
// 戻り値の2つ目: 常に nil。
func (c *cappedBuffer) Write(p []byte) (int, error) {
	if remaining := c.limit - len(c.buf); remaining > 0 {
		if len(p) > remaining {
			c.buf = append(c.buf, p[:remaining]...)
		} else {
			c.buf = append(c.buf, p...)
		}
	}
	return len(p), nil
}
