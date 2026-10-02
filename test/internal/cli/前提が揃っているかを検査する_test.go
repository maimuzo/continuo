// {"RUCM-CFG-SHA256": "8d34b59560432f5f5be879b54890bcbc29a015a9624e41ceff56182d8aa15c44", "SOURCE": "docs/spec/usecases/particular_case/前提が揃っているかを検査する.cfg.json"}
//
// **ユースケース記述「前提が揃っているかを検査する」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/doctor"
)

// {"RUCM-PATH": "P020"}
//
// Test_前提が揃っているかを検査する_P020_後ろに書いたhelpが効く は、`continuo doctor` でも並べ替えが効くことを確かめる。
//
// **`--help` が後ろで効くかを見れば、並べ替えを通していることを確かめられる。**
//
// 目的: `doctor <ディレクトリ> --help` が使い方を出して 0 で終わり、検査を始めないこと。
// 与える情報: 位置引数のあとに置いた `--help`。
// 成功条件: 終了コードが 0 で、検査を1度も呼ばないこと。
func Test_前提が揃っているかを検査する_P020_後ろに書いたhelpが効く(t *testing.T) {
	var ran bool
	deps := cli.Deps{DoctorRun: func(_ context.Context, _ doctor.Options) doctor.Report {
		ran = true
		return doctor.Report{}
	}}

	code, _, stderr := runCLIWith(deps, []string{"doctor", t.TempDir(), "--help"}, "")

	if code != 0 {
		t.Fatalf("--help の終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if ran {
		t.Error("--help なのに検査を始めている")
	}
}

// {"RUCM-PATH": "P014"}
//
// Test_前提が揃っているかを検査する_P014_差分だけを求められたら検査を1つも行わない は、
// `continuo doctor --missing-keys-patch` を確かめる（設計 3-75。issue #85）。
//
// **この口があるから、検査結果に出した差分を利用者が組み立て直さずに当てられる。**
// 検査結果の中の差分は見出し語の桁に揃えて字下げされるので、そのままでは `patch` に渡せない。
//
// 目的: 差分だけを標準出力へ出し、**検査を1つも呼ばない**こと（外部へ1回も出ない）。
// 与える情報: `continuo init` が置いたままの WORKFLOW.md から `restart:` の節を落としたもの。
// 成功条件: 終了コードが 0、標準出力が unified diff で、検査が呼ばれていないこと。
func Test_前提が揃っているかを検査する_P014_差分だけを求められたら検査を1つも行わない(t *testing.T) {
	dir := writeWorkflowFor(t)
	path := filepath.Join(dir, "WORKFLOW.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	var kept []string
	dropping := false
	for _, line := range strings.Split(string(raw), "\n") {
		if dropping {
			if strings.HasPrefix(line, "  ") {
				continue
			}
			dropping = false
		}
		if strings.HasPrefix(line, "restart:") {
			dropping = true
			continue
		}
		kept = append(kept, line)
	}
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	var ran bool
	deps := cli.Deps{DoctorRun: func(_ context.Context, _ doctor.Options) doctor.Report {
		ran = true
		return doctor.Report{}
	}}

	code, stdout, stderr := runCLIWith(deps, []string{"doctor", path, "--missing-keys-patch"}, "")

	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if ran {
		t.Error("差分だけを求められたのに検査を始めている")
	}
	if !strings.HasPrefix(stdout, "--- "+path+"\n+++ "+path+"\n@@ ") {
		t.Fatalf("unified diff の形になっていない:\n%s", stdout)
	}
	if !strings.Contains(stdout, "+restart:") {
		t.Fatalf("落とした節を足す差分になっていない:\n%s", stdout)
	}
}
