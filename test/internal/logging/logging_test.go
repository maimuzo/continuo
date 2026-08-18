// Package logging_test は internal/logging の振る舞いを公開 API を通して検証する
// （テストファイルは test/ 配下に internal/ と同じ構造で置く）。
package logging_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/logging"
)

// 目的: ログレベルの綴りの大文字小文字を区別しないこと（GoDoc の宣言どおりの挙動）を確認する。
// 列挙した綴りだけを照合していると "dEBUG" や "Info" が既定の info へ黙って落ちるため、
// 混ざった綴りも含めて検証する。
// 与える情報: 同じレベルを指す複数の綴り（前後に空白を付けたものを含む）。
// 成功条件: どの綴りでも同じ slog.Level が返り、認識できたことを表す第2返り値が true であること。
func TestParseLevel_大文字小文字が混ざった綴りでも同じレベルになる(t *testing.T) {
	cases := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"Debug", slog.LevelDebug},
		{"dEBUG", slog.LevelDebug},
		{" debug ", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"Info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"Warn", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"Warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"Error", slog.LevelError},
		{"ERROR", slog.LevelError},
	}
	for _, c := range cases {
		got, ok := logging.ParseLevel(c.input)
		if !ok {
			t.Errorf("%q を認識できなかった", c.input)
			continue
		}
		if got != c.want {
			t.Errorf("%q のレベルが一致しない: got %v, want %v", c.input, got, c.want)
		}
	}
}

// 目的: 認識できない綴りを渡したとき、既定の info を返しつつ「認識できなかった」ことを
// 呼び出し側へ伝えることを確認する。無人運用では、指定が効いていないことに気づく手段が
// この返り値しかない。
// 与える情報: レベル名として不正な文字列（空文字・綴り誤り・別の語）。
// 成功条件: 第1返り値が slog.LevelInfo、第2返り値が false であること。
func TestParseLevel_認識できない綴りはinfoに落ちたことを伝える(t *testing.T) {
	for _, input := range []string{"", "warnning", "verbose", "trace", "12"} {
		got, ok := logging.ParseLevel(input)
		if ok {
			t.Errorf("%q は認識できないはずなのに認識できたと返った", input)
		}
		if got != slog.LevelInfo {
			t.Errorf("%q の既定値が info でない: got %v", input, got)
		}
	}
}

// 目的: New が返す logger が、指定したレベル未満のログを出さず、key=value 形式で
// 出力することを確認する（設計 2-5 / SPEC.md 13.1）。
// 与える情報: レベルを warn にした logger と、info / warn の2件のログ。
// 成功条件: info の行は出力されず、warn の行だけが key=value 形式で出ること。
func TestNew_指定したレベル未満のログを出さない(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(&buf, slog.LevelWarn)

	logger.Info("これは出ない", "issue", 1)
	logger.Warn("これは出る", "issue", 2)

	out := buf.String()
	if strings.Contains(out, "これは出ない") {
		t.Errorf("レベル未満のログが出力された: %q", out)
	}
	if !strings.Contains(out, "これは出る") {
		t.Errorf("出力されるべきログが出ていない: %q", out)
	}
	if !strings.Contains(out, "issue=2") {
		t.Errorf("key=value 形式になっていない: %q", out)
	}
}
