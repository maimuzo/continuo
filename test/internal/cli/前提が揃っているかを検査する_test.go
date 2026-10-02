// {"RUCM-CFG-SHA256": "4f2e2eaf480b50efd24ae739b618a524d1e218aff6f81c187a0171a24bd030bd", "SOURCE": "docs/spec/usecases/particular_case/前提が揃っているかを検査する.cfg.json"}
//
// **ユースケース記述「前提が揃っているかを検査する」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストと、そのテストだけが使う補助関数を置く。**
// 経路に対応しないテストと、ほかのファイルと共有する補助関数は、同じディレクトリの別のファイルに在る。
package cli_test

import (
	"context"
	"errors"
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

// brokenStdout は、書き込みが必ず失敗する出力先である。
//
// **標準出力を閉じられた実行を模す。**`continuo doctor --missing-keys-patch … | patch` の
// パイプの先が先に終わったときに起きる。
type brokenStdout struct{}

// Write は何も書かずに誤りを返す。
//
// p: 書こうとした中身。
// 戻り値: 書いたバイト数（常に 0）と、書けなかったことを表すエラー。
func (brokenStdout) Write(p []byte) (int, error) {
	return 0, errors.New("標準出力へ書けません")
}

// {"RUCM-PATH": "P018"}
//
// Test_前提が揃っているかを検査する_P018_いまいるディレクトリを引けなければ終了コード3で止まる は、
// `continuo doctor` が動けなかったことを、`✗` が在ったこと（1）と区別して返すことを確かめる。
//
// **1 は「検査で `✗` が在った」の意味である。**いまいるディレクトリを引けないときは検査を1件も
// 行っていないので、1 を返すと、スクリプトは「前提が足りない」と読み違える。
//
// 目的: いまいるディレクトリを引けないとき、終了コード 3 を返し、検査を始めないこと。
// 与える情報: いまいるディレクトリを、消したあとのディレクトリにする。
// 成功条件: 終了コードが 3、理由が標準エラーに出て、検査を1度も呼ばないこと。
func Test_前提が揃っているかを検査する_P018_いまいるディレクトリを引けなければ終了コード3で止まる(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatalf("ディレクトリを作れません: %v", err)
	}
	t.Chdir(gone)
	if err := os.Remove(gone); err != nil {
		t.Fatalf("ディレクトリを消せません: %v", err)
	}
	if _, err := os.Getwd(); err == nil {
		t.Skip("この環境では、消したディレクトリに居ても、いまいるディレクトリを引ける")
	}

	var ran bool
	deps := cli.Deps{DoctorRun: func(_ context.Context, _ doctor.Options) doctor.Report {
		ran = true
		return doctor.Report{}
	}}

	code, stdout, stderr := runCLIWith(deps, []string{"doctor"}, "")

	if code != 3 {
		t.Fatalf("終了コードが 3 でない: %d（stderr: %s）", code, stderr)
	}
	if ran {
		t.Error("いまいるディレクトリを引けないのに検査を始めている")
	}
	if stdout != "" {
		t.Errorf("標準出力に何か出ている: %s", stdout)
	}
	if strings.TrimSpace(stderr) == "" {
		t.Error("理由が標準エラーに出ていない")
	}
}

// {"RUCM-PATH": "P017"}
//
// Test_前提が揃っているかを検査する_P017_差分だけを求められて設定を読めなければ終了コード3で止まる は、
// `continuo doctor --missing-keys-patch` が差分を作れなかったことを、3 で返すことを確かめる。
//
// **`--missing-keys-patch` は検査をしないモードである。**1（検査で `✗` が在った）の意味を持たない。
//
// 目的: 設定ファイルを読めない場合と、front matter を切り出せない場合の両方で、終了コード 3 を返すこと。
// 与える情報: 存在しない設定ファイルのパスと、front matter を持たない設定ファイル。
// 成功条件: どちらも終了コードが 3、標準出力は空、理由が標準エラーに出ること。
func Test_前提が揃っているかを検査する_P017_差分だけを求められて設定を読めなければ終了コード3で止まる(t *testing.T) {
	dir := t.TempDir()
	noFrontMatter := filepath.Join(dir, "no-front-matter.md")
	if err := os.WriteFile(noFrontMatter, []byte("front matter の無い本文\n"), 0o600); err != nil {
		t.Fatalf("設定ファイルを書けません: %v", err)
	}
	cases := []struct {
		name string
		path string
	}{
		{"設定ファイルを読めない", filepath.Join(dir, "missing", "WORKFLOW.md")},
		{"front matter を切り出せない", noFrontMatter},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runCLIWith(cli.Deps{}, []string{"doctor", tc.path, "--missing-keys-patch"}, "")
			if code != 3 {
				t.Fatalf("終了コードが 3 でない: %d（stderr: %s）", code, stderr)
			}
			if stdout != "" {
				t.Errorf("標準出力に何か出ている: %s", stdout)
			}
			if strings.TrimSpace(stderr) == "" {
				t.Error("理由が標準エラーに出ていない")
			}
		})
	}
}

// {"RUCM-PATH": "P015"}
//
// Test_前提が揃っているかを検査する_P015_差分を書き出せなければ終了コード3で止まる は、
// 差分を標準出力へ書けなかったことを、3 で返すことを確かめる。
//
// 目的: 足す項目が在るのに差分を書き出せないとき、終了コード 3 を返すこと。
// 与える情報: `restart:` の行を落とした WORKFLOW.md と、書き込みが必ず失敗する標準出力。
// 成功条件: 終了コードが 3 で、理由が標準エラーに出ること。
func Test_前提が揃っているかを検査する_P015_差分を書き出せなければ終了コード3で止まる(t *testing.T) {
	dir := writeWorkflowFor(t)
	path := filepath.Join(dir, "WORKFLOW.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	// **足す項目を1つ作る。**`restart:` の節を、見出しの行ごと落とす。
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

	var errBuf strings.Builder
	code := cli.RunWith(cli.Deps{}, []string{"doctor", path, "--missing-keys-patch"}, strings.NewReader(""), brokenStdout{}, &errBuf)

	if code != 3 {
		t.Fatalf("終了コードが 3 でない: %d（stderr: %s）", code, errBuf.String())
	}
	if strings.TrimSpace(errBuf.String()) == "" {
		t.Error("理由が標準エラーに出ていない")
	}
}
