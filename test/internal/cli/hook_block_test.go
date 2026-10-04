package cli_test

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/statussignal"
)

// hookFixture は `continuo hook` を、偽の受け口と issue ごとのディレクトリ付きで叩くための状態である。
type hookFixture struct {
	// SocketPath は偽の受け口の socket のパスである。
	SocketPath string
	// PendingDir は `--pending-dir` に渡すパスである。
	PendingDir string
}

// newHookFixture は偽の hook の受け口を1つ立て、逃がし先のパスを用意する。
//
// **受け口は受け取った行を読み捨てるだけである**（本物も応答を返さない。設計 3-2）。
//
// socket のパスは短く保つ（macOS の Unix domain socket のパス長の上限は 103 バイト）。
// `t.TempDir()` はテスト名を含む長いパスになるので使わない。
//
// t: 呼び出し元のテスト。
// listen: 偽なら受け口を立てない（continuo 本体が動いていない状態を作る）。
// 戻り値: 用意した状態。
func newHookFixture(t *testing.T, listen bool) hookFixture {
	t.Helper()
	dir, err := os.MkdirTemp("", "hookblk")
	if err != nil {
		t.Fatalf("一時ディレクトリを作れない: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	fx := hookFixture{
		SocketPath: filepath.Join(dir, "h.sock"),
		PendingDir: filepath.Join(dir, "issues", "octocat-hello-world-188", "pending"),
	}
	if !listen {
		return fx
	}
	ln, err := net.Listen("unix", fx.SocketPath)
	if err != nil {
		t.Fatalf("偽の受け口を立てられない: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _ = io.Copy(io.Discard, conn)
				_ = conn.Close()
			}()
		}
	}()
	return fx
}

// writeSignalFile は、取り得る値のファイルを issue ごとのディレクトリへ置く（既定の対応表）。
func (fx hookFixture) writeSignalFile(t *testing.T) {
	t.Helper()
	inReview, blocked := "In Review", "Blocked"
	data, err := statussignal.Encode(statussignal.File{
		Identifier: "octocat/hello-world#188",
		Prefix:     "CONTINUO-STATUS:",
		Values:     map[string]*string{"review": &inReview, "blocked": &blocked, "working": nil},
	})
	if err != nil {
		t.Fatalf("取り得る値のファイルを組み立てられない: %v", err)
	}
	path := statussignal.PathFromPendingDir(fx.PendingDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("issue ごとのディレクトリを作れない: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("取り得る値のファイルを書けない: %v", err)
	}
}

// run は `continuo hook` を叩く。
//
// input: 標準入力へ渡す hook の JSON。
// 戻り値: 終了コードと stdout / stderr。
func (fx hookFixture) run(input string) (int, string, string) {
	return runCLI([]string{"hook", "--socket", fx.SocketPath, "--pending-dir", fx.PendingDir}, input)
}

// stopInput は `Stop` hook の入力を作る。
//
// message: `last_assistant_message` に入れる本文。
// stopHookActive: `stop_hook_active` の値。
// 戻り値: JSON の文字列。
func stopInput(t *testing.T, message string, stopHookActive bool) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"hook_event_name":        "Stop",
		"session_id":             "session-1",
		"stop_hook_active":       stopHookActive,
		"background_tasks":       []any{},
		"last_assistant_message": message,
	})
	if err != nil {
		t.Fatalf("hook の入力を組み立てられない: %v", err)
	}
	return string(data)
}

// TestRunHook_決まり以外の表明なら差し戻しのJSONを1行返す は、issue #274 の経路1 を
// `continuo hook` の入口から出口まで通して確かめる。
//
// **ここが Claude Code との約束である。**標準出力へ `{"decision":"block","reason":"…"}` を
// 1行返すと、Claude Code は turn を終わらせずに `reason` をエージェントへ渡す。
// **終了コードは 0 のままである。**2 を返すと「その操作を止めろ」と読まれる。
//
// 目的: 転送できた `Stop` の表明の値が決まり以外なら、差し戻しの JSON を1行だけ返すこと。
// 与える情報: 偽の受け口と、既定の対応表のファイルと、`CONTINUO-STATUS: done` で終わる応答。
// 成功条件: 終了コード 0。標準出力が JSON の1行で、`decision` が `block`、
// `reason` に書いた値と取り得る値が載っている。
func TestRunHook_決まり以外の表明なら差し戻しのJSONを1行返す(t *testing.T) {
	fx := newHookFixture(t, true)
	fx.writeSignalFile(t)

	code, stdout, stderr := fx.run(stopInput(t, "終わりました。\n\nCONTINUO-STATUS: done", false))

	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if strings.Count(stdout, "\n") != 1 || !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("標準出力が1行でない: %q", stdout)
	}
	var out struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("標準出力が JSON でない: %v（%q）", err, stdout)
	}
	if out.Decision != "block" {
		t.Errorf("decision が block でない: %q", out.Decision)
	}
	for _, want := range []string{"「done」", "`CONTINUO-STATUS: review`", "`CONTINUO-STATUS: blocked`", "`CONTINUO-STATUS: working`"} {
		if !strings.Contains(out.Reason, want) {
			t.Errorf("reason に %q が無い:\n%s", want, out.Reason)
		}
	}
}

// TestRunHook_差し戻すとき以外は標準出力へ1バイトも書かない は、いままでの hook と同じ出力の
// ままであることを確かめる。
//
// **`continuo hook` は7種の hook 全部で呼ばれる。**標準出力へ余計なものを書くと、
// Claude Code がそれを hook の応答として読む。
//
// 目的: 差し戻す条件を1つでも外したら、標準出力が空で、終了コードが 0 であること。
// 与える情報: 条件を1つずつ外した入力（どれも応答は `CONTINUO-STATUS: done` で終わる）と、
// 決まりどおりの応答。
// 成功条件: どれも標準出力が0バイトで、終了コードが 0。
func TestRunHook_差し戻すとき以外は標準出力へ1バイトも書かない(t *testing.T) {
	invalid := "終わりました。\n\nCONTINUO-STATUS: done"
	cases := []struct {
		name string
		// listen は偽の受け口を立てるかどうかである。
		listen bool
		// file は取り得る値のファイルを置くかどうかである。
		file bool
		// input は標準入力である。
		input string
	}{
		{name: "決まりどおりの値", listen: true, file: true,
			input: stopInput(t, "終わりました。\n\nCONTINUO-STATUS: review", false)},
		{name: "表明が無い", listen: true, file: true, input: stopInput(t, "作業を進めています。", false)},
		{name: "差し戻されたあとの Stop", listen: true, file: true, input: stopInput(t, invalid, true)},
		{name: "取り得る値のファイルが無い（古い continuo 本体が着手した run）", listen: true, file: false,
			input: stopInput(t, invalid, false)},
		{name: "continuo 本体が動いていない（逃がし先へ書いた）", listen: false, file: true,
			input: stopInput(t, invalid, false)},
		{name: "Stop 以外の hook", listen: true, file: true,
			input: `{"hook_event_name":"PostToolUse","session_id":"session-1","last_assistant_message":"CONTINUO-STATUS: done","stop_hook_active":false}`},
		{name: "SubagentStop", listen: true, file: true,
			input: `{"hook_event_name":"SubagentStop","session_id":"session-1","last_assistant_message":"CONTINUO-STATUS: done","stop_hook_active":false}`},
		{name: "JSON でない入力", listen: true, file: true, input: "これは JSON ではない"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newHookFixture(t, tc.listen)
			if tc.file {
				fx.writeSignalFile(t)
			}

			code, stdout, stderr := fx.run(tc.input)

			if code != 0 {
				t.Errorf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
			}
			if stdout != "" {
				t.Errorf("標準出力へ書いている: %q", stdout)
			}
		})
	}
}

// TestRunHook_reasonに引用符やbacktickが入ってもJSONとして読める は、標準出力の形を確かめる。
//
// **エージェントが書いた値が `reason` に入る。**引用符やバックスラッシュを含む値でも、
// 標準出力は1行の正しい JSON でなければならない（壊れた JSON は差し戻しとして読まれない）。
// **`reason` の本文そのものは複数行である**（一覧を載せるため）。改行は JSON の中で `\n` に
// 逃がされるので、標準出力は1行のままである。値そのものには改行は入らない（値は空白で区切った1語）。
//
// 目的: 値に引用符・backtick・バックスラッシュが入っていても、1行の JSON として読めること。
// 与える情報: `CONTINUO-STATUS: "do`ne\` という表明。
// 成功条件: 標準出力が1行で、JSON として読め、`reason` に値がそのまま載っている。
func TestRunHook_reasonに引用符やbacktickが入ってもJSONとして読める(t *testing.T) {
	fx := newHookFixture(t, true)
	fx.writeSignalFile(t)

	code, stdout, _ := fx.run(stopInput(t, "CONTINUO-STATUS: \"do`ne\\", false))

	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d", code)
	}
	if strings.Count(stdout, "\n") != 1 {
		t.Fatalf("標準出力が1行でない: %q", stdout)
	}
	var out struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("標準出力が JSON でない: %v（%q）", err, stdout)
	}
	if out.Decision != "block" || !strings.Contains(out.Reason, "「\"do`ne\\」") {
		t.Errorf("値がそのまま載っていない: %+v", out)
	}
	if !strings.Contains(out.Reason, "\n- `CONTINUO-STATUS: review`") {
		t.Errorf("reason の本文が複数行のまま読み戻せていない: %q", out.Reason)
	}
}
