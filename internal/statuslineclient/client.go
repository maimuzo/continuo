// Package statuslineclient は `continuo statusline` の本体である（issue #284）。
//
// Claude Code は、continuo が渡した設定ファイル（`--settings`）の `statusLine` に書いたコマンドを、
// ステータスラインを描き直すたびに実行し、標準入力へ JSON を渡す。このコマンドは、そこから
// 4つの欄（`session_id`・`cost.total_api_duration_ms`・`rate_limits.five_hour`・
// `rate_limits.seven_day`）を抜き出して1行にし、本体の `sl.sock` へ送る。
//
// **どんな失敗でも、何も書かずに固定の1行 `continuo` を出して終了コード 0 で終える。**
// socket が無い・接続を断られた・期限切れ・標準入力が大きすぎる・JSON として読めない、の
// どれでも同じである。ステータスラインのコマンドが失敗しても Claude Code の作業は止まらないが、
// 出力が変わると画面の版（`revision`）が動き、continuo の stall の判定を狂わせうる。
// **逃がし先は持たない。**使用率は statusline取得でまた取り直せる。
package statuslineclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/maimuzo/continuo/internal/statuslineserver"
)

// MaxInputBytes は標準入力の上限である。
const MaxInputBytes = 1 << 20

// Output はステータスラインに出す固定の1行である。
//
// **固定にする。**描き直しの中身で画面の版が動かないようにするためである。
const Output = "continuo"

// dialTimeout は socket への接続を待つ上限である。
const dialTimeout = 200 * time.Millisecond

// writeTimeout は1行を書き切るまでの上限である。
const writeTimeout = 500 * time.Millisecond

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

// Run は標準入力を読んで1行を socket へ送り、固定の1行を出す。
//
// stdin: ステータスラインの入力。
// stdout: ステータスラインの出力先。
// socketPath: `sl.sock` の絶対パス。
// 戻り値: 終了コード。**いつも 0 である。**
func Run(stdin io.Reader, stdout io.Writer, socketPath string) int {
	defer fmt.Fprintln(stdout, Output)
	data, err := io.ReadAll(io.LimitReader(stdin, MaxInputBytes+1))
	if err != nil || len(data) > MaxInputBytes {
		return 0
	}
	var in input
	if err := json.Unmarshal(data, &in); err != nil || in.SessionID == "" {
		return 0
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
		return 0
	}
	out = append(out, '\n')
	conn, err := net.DialTimeout("unix", socketPath, dialTimeout)
	if err != nil {
		return 0
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, _ = conn.Write(out)
	return 0
}
