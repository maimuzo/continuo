// {"RUCM-CFG-SHA256": "43566f3b603f55073410b6583b08c94b06550123df19e7fcbed2d97885a6270a", "SOURCE": "docs/spec/usecases/particular_case/着手を取り消す.cfg.json"}
//
// **ユースケース記述「着手を取り消す」の経路に対応づけたテストである。**
// 関数名の `P008` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **同じ経路を別の観点で確かめるテストは、同じ番号を持つ。**この記述は、同じ入力で決まる条件を経路へ割らずに
// 1本の経路の中へ畳んでいるので、畳んだ条件の真の側と偽の側が同じ番号に並ぶ（設計 6-20）。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package cli_test

import (
	"context"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/abandon"
	"github.com/maimuzo/continuo/internal/cli"
)

// {"RUCM-PATH": "P039"}
//
// Test_着手を取り消す_P039_引数の誤りは本体を呼ばずに2で止まる は、消す処理へ進ませないことを確かめる。
//
// **引数を取り違えたまま進むと、消す相手を間違える。**打ち間違えたフラグを位置引数として
// 飲み込むと、`--dry-run` のつもりで本当に消すことになる。
//
// 目的: issue の URL が無い・位置引数が3つ以上・知らないフラグを書いた場合に、
// 終了コード 2 で止まり、abandon の本体を1度も呼ばないこと。
// 与える情報: 誤った並びの3通り。
// 成功条件: すべて終了コードが 2、本体の呼び出しが0回、stderr に理由が出ていること。
func Test_着手を取り消す_P039_引数の誤りは本体を呼ばずに2で止まる(t *testing.T) {
	url := "https://github.com/octocat/hello-world/issues/42"
	cases := map[string][]string{
		"URLが無い":  {"abandon"},
		"位置引数が3つ": {"abandon", url, "a", "b"},
		"知らないフラグ": {"abandon", url, "--dryrun"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			calls := 0
			deps := cli.Deps{AbandonRun: func(_ context.Context, _ abandon.Options) int {
				calls++
				return 0
			}}
			code, _, stderr := runCLIWith(deps, args, "")
			if code != 2 {
				t.Errorf("終了コードが 2 でない: %d（stderr: %s）", code, stderr)
			}
			if calls != 0 {
				t.Errorf("引数が誤っているのに abandon の本体を %d 回呼んでいる", calls)
			}
			if stderr == "" {
				t.Error("何が誤りかを stderr へ出していない")
			}
		})
	}
}

// {"RUCM-PATH": "P040"}
//
// Test_着手を取り消す_P040_helpは0で返して本体を呼ばない は、使い方の表示を確かめる。
//
// 目的: `continuo abandon --help` が 0 で返り、消す処理へ進まないこと。
// 与える情報: `abandon --help`。
// 成功条件: 終了コードが 0、本体の呼び出しが0回、使い方が stderr に出ていること。
func Test_着手を取り消す_P040_helpは0で返して本体を呼ばない(t *testing.T) {
	calls := 0
	deps := cli.Deps{AbandonRun: func(_ context.Context, _ abandon.Options) int {
		calls++
		return 0
	}}

	code, _, stderr := runCLIWith(deps, []string{"abandon", "--help"}, "")

	if code != 0 {
		t.Fatalf("--help の終了コードが 0 でない: %d", code)
	}
	if calls != 0 {
		t.Errorf("--help なのに abandon の本体を %d 回呼んでいる", calls)
	}
	if !strings.Contains(stderr, "-dry-run") {
		t.Errorf("使い方にフラグの説明が出ていない: %s", stderr)
	}
}
