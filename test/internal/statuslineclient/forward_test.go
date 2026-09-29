// `continuo statusline` の本体が、使用率を送ったあとで利用者のステータスラインへ転送することを
// 確かめる（設計 3-84b。issue #295）。
package statuslineclient_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/statuslineclient"
)

// writeScript は一時ディレクトリに sh のスクリプトを置き、そのパスを返す。
func writeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(shortDir(t), "sl.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatalf("スクリプトを書けません: %v", err)
	}
	return path
}

// 目的: 転送先のコマンドが、受け取ったのと同じ標準入力で起動され、その標準出力がそのまま
// ステータスラインへ出ることを確かめる。使用率の1行も、転送の前にいままでどおり送ること。
// 与える情報: 標準入力を標準出力へ写すだけのコマンドと、listen している sl.sock。
// 成功条件: 標準出力が標準入力と同じバイト列で、固定の1行を足していないこと。sl.sock へ1行が届くこと。
// 終了コードが 0 であること。
func TestRun_転送先へ同じ入力を渡しその出力をそのまま出す(t *testing.T) {
	path, ch := listenOnce(t)
	var stdout bytes.Buffer

	code := statuslineclient.Run(strings.NewReader(sampleInput), &stdout, path, "cat")

	if code != 0 {
		t.Fatalf("終了コード = %d, want 0", code)
	}
	if got := stdout.String(); got != sampleInput {
		t.Fatalf("標準出力 = %q, want 標準入力と同じ %q", got, sampleInput)
	}
	recvLine(t, ch)
}

// 目的: sl.sock へ送れないとき・入力が JSON として読めないときも、転送は続けることを確かめる
// （使用率の送信は転送の前提ではない）。
// 与える情報: socket の無いパスと、固定の文字列を出すコマンド。JSON として読めない入力。
// 成功条件: どちらも標準出力がコマンドの出力で、終了コードが 0 であること。
func TestRun_送れなくても転送する(t *testing.T) {
	for name, input := range map[string]string{
		"socketが無い": sampleInput,
		"読めないJSON":  "{壊れた",
	} {
		t.Run(name, func(t *testing.T) {
			missing := filepath.Join(shortDir(t), "sl.sock")
			var stdout bytes.Buffer

			code := statuslineclient.Run(strings.NewReader(input), &stdout, missing, "printf 'my line'")

			if code != 0 {
				t.Fatalf("終了コード = %d, want 0", code)
			}
			if got := stdout.String(); got != "my line" {
				t.Fatalf("標準出力 = %q, want %q", got, "my line")
			}
		})
	}
}

// 目的: 転送先が非 0 で終わったとき・何も出さなかったときは、固定の1行を出すことを確かめる
// （Claude Code は非 0 のコマンドの出力を表示しない。空にせず転送先が無いときと同じ見た目にする）。
// 与える情報: 出力してから 1 で終わるコマンドと、何も出さずに 0 で終わるコマンド。
// 成功条件: どちらも標準出力が固定の1行で、終了コードが 0 であること。
func TestRun_転送先が非0か空なら固定の1行を出す(t *testing.T) {
	for name, command := range map[string]string{
		"非0": "echo partial; exit 1",
		"空":  "true",
	} {
		t.Run(name, func(t *testing.T) {
			var stdout bytes.Buffer
			missing := filepath.Join(shortDir(t), "sl.sock")

			code := statuslineclient.Run(strings.NewReader(sampleInput), &stdout, missing, command)

			assertFixedOutput(t, code, &stdout)
		})
	}
}

// 目的: 標準入力が上限（1 MiB）を超えたときは、転送先を起動せずに固定の1行を出すことを確かめる。
// 与える情報: 上限を超える入力と、起動されたら印のファイルを作るコマンド。
// 成功条件: 標準出力が固定の1行で、印のファイルが作られていないこと。
func TestRun_入力が上限を超えたら転送しない(t *testing.T) {
	mark := filepath.Join(shortDir(t), "started")
	big := `{"session_id":"s1","pad":"` + strings.Repeat("a", statuslineclient.MaxInputBytes) + `"}`
	var stdout bytes.Buffer

	code := statuslineclient.Run(strings.NewReader(big), &stdout, filepath.Join(shortDir(t), "sl.sock"),
		"touch '"+mark+"'; echo started")

	assertFixedOutput(t, code, &stdout)
	if _, err := os.Stat(mark); err == nil {
		t.Fatal("上限を超えた入力なのに転送先を起動した")
	}
}

// 目的: 転送先の出力は先頭 64KiB だけを出すことを確かめる。
// 与える情報: 64KiB を超えて書くコマンド。
// 成功条件: 標準出力がちょうど 64KiB であること。
func TestRun_転送先の出力は先頭64KiBまで(t *testing.T) {
	var stdout bytes.Buffer
	n := statuslineclient.MaxForwardOutputBytes + 1000

	code := statuslineclient.Run(strings.NewReader(sampleInput), &stdout, filepath.Join(shortDir(t), "sl.sock"),
		"head -c "+strconv.Itoa(n)+" /dev/zero | tr '\\0' 'a'")

	if code != 0 {
		t.Fatalf("終了コード = %d, want 0", code)
	}
	if got := stdout.Len(); got != statuslineclient.MaxForwardOutputBytes {
		t.Fatalf("標準出力の長さ = %d, want %d", got, statuslineclient.MaxForwardOutputBytes)
	}
}

// 目的: 転送先の子の環境変数から CONTINUO_STATUSLINE_COMMAND を外すことを確かめる
// （利用者の statusLine が `continuo statusline` でも、呼ばれた側は転送先を持たず1段で止まる）。
// ほかの環境変数は受け継ぐこと。
// 与える情報: 環境に CONTINUO_STATUSLINE_COMMAND と別の変数を置いたうえで、2つの値を出すコマンド。
// 成功条件: 子から見て CONTINUO_STATUSLINE_COMMAND が未設定で、別の変数は見えること。
func TestRun_転送先の子にはCONTINUO_STATUSLINE_COMMANDを渡さない(t *testing.T) {
	t.Setenv(statuslineclient.EnvForwardCommand, "continuo statusline --socket /nonexistent/sl.sock")
	t.Setenv("CONTINUO_TEST_INHERITED", "kept")
	var stdout bytes.Buffer

	code := statuslineclient.Run(strings.NewReader(sampleInput), &stdout, filepath.Join(shortDir(t), "sl.sock"),
		`printf '%s|%s' "${CONTINUO_STATUSLINE_COMMAND-unset}" "$CONTINUO_TEST_INHERITED"`)

	if code != 0 {
		t.Fatalf("終了コード = %d, want 0", code)
	}
	if got := stdout.String(); got != "unset|kept" {
		t.Fatalf("子から見えた値 = %q, want %q", got, "unset|kept")
	}
}

// 目的: 転送先が5秒で終わらなければ打ち切り、子のプロセスグループ（背景へ回した孫も含む）ごと
// 止めて固定の1行を出すことを確かめる。
// 与える情報: 背景に sleep を回してその PID を書き、自分も眠り続けるスクリプト。
// 成功条件: 5秒ほどで戻り、標準出力が固定の1行で、背景の sleep も止まっていること。
func TestRun_転送先が5秒で終わらなければグループごと止める(t *testing.T) {
	pidFile := filepath.Join(shortDir(t), "pid")
	script := writeScript(t, "sleep 60 &\necho $! > '"+pidFile+"'\nsleep 60\n")
	var stdout bytes.Buffer

	start := time.Now()
	code := statuslineclient.Run(strings.NewReader(sampleInput), &stdout, filepath.Join(shortDir(t), "sl.sock"), script)
	elapsed := time.Since(start)

	assertFixedOutput(t, code, &stdout)
	if elapsed < statuslineclient.ForwardTimeout || elapsed > statuslineclient.ForwardTimeout+3*time.Second {
		t.Fatalf("戻るまで %v（want 約 %v）", elapsed, statuslineclient.ForwardTimeout)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("背景の PID を読めません: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("背景の PID が数字ではない: %q", raw)
	}
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("背景の sleep（PID %d）が止まっていない", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
