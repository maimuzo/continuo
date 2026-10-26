package claudeversion_test

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/maimuzo/continuo/internal/claudeversion"
)

// このファイルは、continuo が起動する Claude Code の版の確かめを固定する（issue #288）。
//
// **v2.1.304 より古い Claude Code では、continuo は着手できない。**`--system-prompt-snapshot` を知らない版は
// 起動のエラーで落ち、v2.1.257〜303 は起動できても compaction のあとにスキルの一覧を作り直さない。

// lookOK は、実行ファイルが在ることにする。
func lookOK(string) (string, error) { return "/opt/bin/claude", nil }

// runWith は、`--version` の出力を固定する。
func runWith(out string, err error) claudeversion.Runner {
	return func(context.Context, string) (string, error) { return out, err }
}

// 目的: 版を読めて下限以上なら通し、下限より古ければ止め、読めなければ止めないことを固定する。
//
// 与える情報: いろいろな `--version` の出力と失敗。
// 成功条件: 表のとおりの結果になること。
func TestCheck_版で止めるかを決める(t *testing.T) {
	for _, c := range []struct {
		name    string
		look    claudeversion.LookPath
		run     claudeversion.Runner
		want    claudeversion.Status
		version string
	}{
		{"下限ちょうど", lookOK, runWith("2.1.304 (Claude Code)\n", nil), claudeversion.StatusOK, "2.1.304"},
		{"新しい", lookOK, runWith("2.1.310 (Claude Code)\n", nil), claudeversion.StatusOK, "2.1.310"},
		{"メジャーが上", lookOK, runWith("3.0.0 (Claude Code)\n", nil), claudeversion.StatusOK, "3.0.0"},
		{"snapshot を知らない版", lookOK, runWith("2.1.250 (Claude Code)\n", nil), claudeversion.StatusTooOld, "2.1.250"},
		{"スキルの一覧を作り直さない版", lookOK, runWith("2.1.303 (Claude Code)\n", nil), claudeversion.StatusTooOld, "2.1.303"},
		{"PATH に無い", func(string) (string, error) { return "", exec.ErrNotFound }, runWith("", nil), claudeversion.StatusUnknown, ""},
		{"叩けない", lookOK, runWith("", errors.New("exit status 1")), claudeversion.StatusUnknown, ""},
		{"版が出ない", lookOK, runWith("fake claude\n", nil), claudeversion.StatusUnknown, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := claudeversion.Check(context.Background(), "claude", c.look, c.run)
			if got.Status != c.want || got.Version != c.version {
				t.Errorf("結果が %+v です（状態 %v・版 %q のはず）", got, c.want, c.version)
			}
			if c.want == claudeversion.StatusUnknown && got.Err == nil {
				t.Error("読めなかった理由が入っていません")
			}
		})
	}
}

// 目的: 版を数として比べることを固定する（文字列で比べると 2.1.40 が 2.1.304 より新しく見える）。
//
// 与える情報: 桁数の違う版の組。
// 成功条件: 数として古いほうだけが Less で真になること。
func TestLess_版を数として比べる(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"2.1.40", "2.1.304", true},
		{"2.1.304", "2.1.40", false},
		{"2.1.304", "2.1.304", false},
		{"1.9.999", "2.1.304", true},
		{"2.2.0", "2.1.304", false},
	} {
		if got := claudeversion.Less(c.a, c.b); got != c.want {
			t.Errorf("Less(%q, %q) = %v（%v のはず）", c.a, c.b, got, c.want)
		}
	}
}
