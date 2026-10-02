// Package statuslineclient_test は、`continuo statusline` の本体が、ステータスラインの入力から
// 4つの欄を1行にして sl.sock へ送り、どんな失敗でも固定の1行を出して終了コード 0 で終えることを
// 確かめる（issue #284）。
package statuslineclient_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/statuslineclient"
	"github.com/maimuzo/continuo/internal/statuslineserver"
)

// waitTimeout は行が届くのを待つ上限である。
const waitTimeout = 5 * time.Second

// sampleInput は Claude Code がステータスラインへ渡す JSON の形の例である
// （continuo が読まない欄も含める。https://code.claude.com/docs/en/statusline ）。
const sampleInput = `{
  "hook_event_name": "Status",
  "session_id": "0f3c9a1b-2d4e-4f60-8a7b-1c2d3e4f5a6b",
  "model": {"id": "claude-haiku", "display_name": "Haiku"},
  "workspace": {"current_dir": "/tmp/continuo-test/clone"},
  "cost": {"total_cost_usd": 0.01, "total_duration_ms": 45000, "total_api_duration_ms": 10728.9},
  "rate_limits": {
    "five_hour": {"used_percentage": 3.7, "resets_at": 1790338200},
    "seven_day": {"used_percentage": 34, "resets_at": 1790622000}
  }
}`

// shortDir は、macOS の Unix socket の長さの上限に収まる短い一時ディレクトリを作る。
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "slcli")
	if err != nil {
		t.Fatalf("一時ディレクトリを作成できません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// listenOnce は sl.sock を1本 listen し、最初の接続から読んだ1行を返す channel を返す。
func listenOnce(t *testing.T) (string, <-chan []byte) {
	t.Helper()
	path := filepath.Join(shortDir(t), "sl.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("socket を listen できない: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	ch := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetReadDeadline(time.Now().Add(waitTimeout))
		line, _ := bufio.NewReader(conn).ReadBytes('\n')
		ch <- line
	}()
	return path, ch
}

// recvLine は1行を受ける。届かなければテストを落とす。
func recvLine(t *testing.T, ch <-chan []byte) []byte {
	t.Helper()
	select {
	case line := <-ch:
		return line
	case <-time.After(waitTimeout):
		t.Fatal("sl.sock へ1行が届きませんでした")
		return nil
	}
}

// assertFixedOutput は、標準出力が固定の1行 `continuo` だけで、終了コードが 0 であることを確かめる。
func assertFixedOutput(t *testing.T, code int, stdout *bytes.Buffer) {
	t.Helper()
	if code != 0 {
		t.Fatalf("終了コード = %d, want 0（ステータスラインのコマンドは失敗を返さない）", code)
	}
	if got := stdout.String(); got != statuslineclient.Output+"\n" {
		t.Fatalf("標準出力 = %q, want %q（描き直しで画面の版が動かないよう固定にする）", got, statuslineclient.Output+"\n")
	}
}

// 目的: ステータスラインの入力から4つの欄（session_id・cost.total_api_duration_ms・
// rate_limits.five_hour・rate_limits.seven_day）だけを抜き出し、1行にして sl.sock へ送ることを確かめる。
// 与える情報: continuo が読まない欄も含む、Claude Code と同じ形の入力。
// 成功条件: 届いた行が改行で終わる1行で、キーが session_id・api_ms・five_hour・seven_day の4つだけ、
// api_ms は小数を切り捨てた整数、used_percentage は小数のまま運ばれること。標準出力は固定の1行、
// 終了コードは 0 であること。
func TestRun_4つの欄を1行にしてsocketへ送る(t *testing.T) {
	path, ch := listenOnce(t)
	var stdout bytes.Buffer

	code := statuslineclient.Run(strings.NewReader(sampleInput), &stdout, path, "")

	assertFixedOutput(t, code, &stdout)
	line := recvLine(t, ch)
	if !bytes.HasSuffix(line, []byte("\n")) || bytes.Count(line, []byte("\n")) != 1 {
		t.Fatalf("1行で届いていない: %q", line)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		t.Fatalf("届いた行を JSON として読めない: %v（%q）", err, line)
	}
	for _, key := range []string{"session_id", "api_ms", "five_hour", "seven_day"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("欄 %q が無い: %s", key, line)
		}
	}
	if len(raw) != 4 {
		t.Fatalf("4つの欄のほかに余計な欄を送っている: %s", line)
	}
	var got statuslineserver.Line
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("届いた行を Line として読めない: %v", err)
	}
	if got.SessionID != "0f3c9a1b-2d4e-4f60-8a7b-1c2d3e4f5a6b" {
		t.Fatalf("session_id = %q", got.SessionID)
	}
	if got.APIMs != 10728 {
		t.Fatalf("api_ms = %d, want 10728", got.APIMs)
	}
	if got.FiveHour == nil || got.FiveHour.UsedPercentage != 3.7 || got.FiveHour.ResetsAt != 1790338200 {
		t.Fatalf("five_hour = %+v", got.FiveHour)
	}
	if got.SevenDay == nil || got.SevenDay.UsedPercentage != 34 || got.SevenDay.ResetsAt != 1790622000 {
		t.Fatalf("seven_day = %+v", got.SevenDay)
	}
}

// 目的: rate_limits が null の入力（起動の直後の1回など）でも、session_id と api_ms だけの行を
// 送ることを確かめる（本体が「応答はあったが値が無い」を見分けるため）。
// 与える情報: rate_limits が null の入力と、rate_limits の欄そのものが無い入力。
// 成功条件: どちらも行が届き、five_hour と seven_day の欄を持たず、session_id と api_ms を持つこと。
func TestRun_rate_limitsがnullでもsession_idとapi_msを送る(t *testing.T) {
	for name, input := range map[string]string{
		"null": `{"session_id":"s1","cost":{"total_api_duration_ms":0},"rate_limits":null}`,
		"無い":   `{"session_id":"s1","cost":{"total_api_duration_ms":0}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path, ch := listenOnce(t)
			var stdout bytes.Buffer

			code := statuslineclient.Run(strings.NewReader(input), &stdout, path, "")

			assertFixedOutput(t, code, &stdout)
			line := recvLine(t, ch)
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(line, &raw); err != nil {
				t.Fatalf("届いた行を JSON として読めない: %v（%q）", err, line)
			}
			if _, ok := raw["five_hour"]; ok {
				t.Fatalf("値の無い行に five_hour を載せている: %s", line)
			}
			if _, ok := raw["seven_day"]; ok {
				t.Fatalf("値の無い行に seven_day を載せている: %s", line)
			}
			if string(raw["session_id"]) != `"s1"` || string(raw["api_ms"]) != "0" {
				t.Fatalf("session_id と api_ms が届いていない: %s", line)
			}
		})
	}
}

// 目的: 期間が片方だけ（seven_day だけ）の入力で、その期間だけを送ることを確かめる
// （期間は独立に欠けうる。公式文書）。
// 与える情報: rate_limits に seven_day だけを持つ入力。
// 成功条件: 行に seven_day があり、five_hour が無いこと。
func TestRun_期間が片方だけならその期間だけを送る(t *testing.T) {
	path, ch := listenOnce(t)
	var stdout bytes.Buffer

	code := statuslineclient.Run(strings.NewReader(
		`{"session_id":"s1","cost":{"total_api_duration_ms":12},"rate_limits":{"seven_day":{"used_percentage":90,"resets_at":1790622000}}}`),
		&stdout, path, "")

	assertFixedOutput(t, code, &stdout)
	var got statuslineserver.Line
	if err := json.Unmarshal(recvLine(t, ch), &got); err != nil {
		t.Fatalf("届いた行を読めない: %v", err)
	}
	if got.FiveHour != nil {
		t.Fatalf("入力に無い five_hour を送っている: %+v", got.FiveHour)
	}
	if got.SevenDay == nil || got.SevenDay.UsedPercentage != 90 {
		t.Fatalf("seven_day = %+v", got.SevenDay)
	}
}

// 目的: 送れないどの場合も、何も書かずに固定の1行を出して終了コード 0 で終えることを確かめる
// （ステータスラインのコマンドの出力が変わると画面の版が動き、stall の判定を狂わせうる）。
// 与える情報: socket が無いパス・JSON として読めない入力・session_id の無い入力・空の入力・
// 上限（1 MiB）を超える入力。読めない入力では、listen している socket を渡して何も届かないことも見る。
// 成功条件: どれも標準出力が固定の1行、終了コードが 0 で、socket へは何も届かないこと。
func TestRun_送れなかったときも固定の1行を出して0で終える(t *testing.T) {
	t.Run("socketが無い", func(t *testing.T) {
		var stdout bytes.Buffer
		missing := filepath.Join(shortDir(t), "sl.sock")
		code := statuslineclient.Run(strings.NewReader(sampleInput), &stdout, missing, "")
		assertFixedOutput(t, code, &stdout)
	})

	big := `{"session_id":"s1","pad":"` + strings.Repeat("a", statuslineclient.MaxInputBytes) + `"}`
	for name, input := range map[string]string{
		"読めないJSON":      "{壊れた",
		"session_idが無い": `{"cost":{"total_api_duration_ms":1}}`,
		"空":             "",
		"上限を超える":        big,
	} {
		t.Run(name, func(t *testing.T) {
			path, ch := listenOnce(t)
			var stdout bytes.Buffer

			code := statuslineclient.Run(strings.NewReader(input), &stdout, path, "")

			assertFixedOutput(t, code, &stdout)
			select {
			case line := <-ch:
				t.Fatalf("送ってはならない入力なのに行が届いた: %q", line)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}

// 目的: 送り手と本物の受け口（statuslineserver）をつないだとき、行が本体へ届くことを確かめる
// （送り手と受け手の欄の名前がずれていないこと）。知らない欄を持つ入力でも落ちないこと。
// 与える情報: 本物の受け口と、Claude Code と同じ形の入力。
// 成功条件: sink が受けた行の4つの欄が入力のとおりであること。
func TestRun_本物の受け口へ届く(t *testing.T) {
	path := filepath.Join(shortDir(t), "sl.sock")
	got := make(chan statuslineserver.Line, 1)
	srv := statuslineserver.New(path, func(line statuslineserver.Line) {
		select {
		case got <- line:
		default:
		}
	}, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("受け口を立てられない: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	var stdout bytes.Buffer
	code := statuslineclient.Run(strings.NewReader(sampleInput), &stdout, path, "")
	assertFixedOutput(t, code, &stdout)

	select {
	case line := <-got:
		if line.SessionID != "0f3c9a1b-2d4e-4f60-8a7b-1c2d3e4f5a6b" || line.APIMs != 10728 {
			t.Fatalf("届いた行 = %+v", line)
		}
		if line.FiveHour == nil || line.FiveHour.ResetsAt != 1790338200 || line.SevenDay == nil {
			t.Fatalf("届いた期間 = five_hour %+v, seven_day %+v", line.FiveHour, line.SevenDay)
		}
	case <-time.After(waitTimeout):
		t.Fatal("本物の受け口へ行が届かなかった")
	}
}
