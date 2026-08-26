// {"RUCM-CFG-SHA256": "05dde3d6b6d1fff7cc317912d27113c4890cf461623b277e4c4c53852fe9b5c3", "SOURCE": "docs/spec/usecases/particular_case/前提が揃っているかを検査する.cfg.json"}
//
// **hook の置き場所の検査だけを集めたファイルである。**
//
// **この検査は「doctor が全項目 ✓ なのに起動だけが落ちた」（issue #9）のために足された。**
// にもかかわらず、失敗する場合のテストが1本も無かった。**そのうえ、置き場所を差し替えて
// いなかったので、開発機に continuo が動いていれば「既に使われている」の近道で ✓ になり、
// listen も後始末も1行も実行されなかった。**ここでは置き場所を必ず一時ディレクトリへ閉じ、
// 実機を触っていないことを毎回確かめる（newFixture と assertSocketUnderRoot）。
package doctor_test

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/doctor"
)

// {"RUCM-PATH": "P001"}
//
// TestDoctorRuntimeDir_何も無ければ作って消す は、検査の本体が実際に走ることを確かめる。
//
// 目的: 置き場所に何も無いとき、socket を作れることを確かめ、**作った socket を残さない**こと。
// 与える情報: 一時ディレクトリへ閉じた置き場所（CONTINUO_RUNTIME_DIR）。
// 成功条件: `✓` で、説明が一時ディレクトリの下の socket を指し、
// 検査のあとにその socket が残っていないこと。
func TestDoctorRuntimeDir_何も無ければ作って消す(t *testing.T) {
	fx := newFixture(t)

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelRuntimeDir, doctor.SymbolOK)
	assertSocketUnderRoot(t, fx, res.Detail)
	if !strings.Contains(res.Detail, "作れます") {
		t.Fatalf("socket を作れたことが出ていない: %q", res.Detail)
	}
	if _, err := os.Lstat(fx.SocketPath()); !os.IsNotExist(err) {
		t.Fatalf("検査が作った socket が残っている: %s（%v）", fx.SocketPath(), err)
	}
}

// {"RUCM-PATH": "P001"}
//
// TestDoctorRuntimeDir_既にcontinuoが待ち受けていれば通る は、動いている continuo を
// 「用意できない」と言わないことを確かめる。
//
// 目的: 置き場所で誰かが待ち受けているとき、`✓` にし、**「作れます」とは言わない**こと。
// 与える情報: 一時ディレクトリの置き場所に、テストが自分で listen した socket。
// 成功条件: `✓` で、説明が「既に continuo が待ち受けています」であること。
func TestDoctorRuntimeDir_既にcontinuoが待ち受けていれば通る(t *testing.T) {
	fx := newFixture(t)
	if err := os.MkdirAll(fx.RunDir, 0o700); err != nil {
		t.Fatalf("置き場所を作れません: %v", err)
	}
	ln, err := net.Listen("unix", fx.SocketPath())
	if err != nil {
		t.Fatalf("テストが socket を listen できません: %v", err)
	}
	defer ln.Close()

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelRuntimeDir, doctor.SymbolOK)
	assertSocketUnderRoot(t, fx, res.Detail)
	if !strings.Contains(res.Detail, "待ち受けています") {
		t.Fatalf("既に使われていることが出ていない: %q", res.Detail)
	}
	if strings.Contains(res.Detail, "作れます") {
		t.Fatalf("使われているのに「作れます」と出ている: %q", res.Detail)
	}
}

// {"RUCM-PATH": "P020"}
//
// TestDoctorRuntimeDir_残骸があれば足りないと出す は、**「作れます」の嘘**を落とす。
//
// **置き場所に何かが在れば、AF_UNIX の bind は必ず EADDRINUSE を返す**
// （通常ファイル・ディレクトリ・listen していない socket のすべてで、darwin で実測した）。
// それを「既に continuo が動いている」と読むと、continuo が起動できない状態を ✓ と報告する。
//
// 目的: 待ち受けていない残骸があるとき、`✗` にし、確かめて消す手順を出すこと。
// 与える情報: 置き場所に置いた、listen していない通常ファイル。
// 成功条件: `✗` で、直し方に `ls -l` と `rm` が出ること。
func TestDoctorRuntimeDir_残骸があれば足りないと出す(t *testing.T) {
	fx := newFixture(t)
	if err := os.MkdirAll(fx.RunDir, 0o700); err != nil {
		t.Fatalf("置き場所を作れません: %v", err)
	}
	if err := os.WriteFile(fx.SocketPath(), []byte("これは socket ではない\n"), 0o600); err != nil {
		t.Fatalf("残骸を置けません: %v", err)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelRuntimeDir, doctor.SymbolMissing)
	assertSocketUnderRoot(t, fx, res.Detail)
	remedies := strings.Join(res.Remedies, "\n")
	if !strings.Contains(remedies, "ls -l") || !strings.Contains(remedies, "rm ") {
		t.Fatalf("残骸を確かめて消す手順が出ていない: %v", res.Remedies)
	}
}

// {"RUCM-PATH": "P020"}
//
// TestDoctorRuntimeDir_置き場所がディレクトリなら足りないと出す は、
// 残骸が socket とは限らないことを確かめる。
//
// 目的: socket のパスがディレクトリのとき、`✗` にすること。
// 与える情報: 置き場所に作った、`hooks.sock` という名前のディレクトリ。
// 成功条件: `✗` になること。
func TestDoctorRuntimeDir_置き場所がディレクトリなら足りないと出す(t *testing.T) {
	fx := newFixture(t)
	if err := os.MkdirAll(fx.SocketPath(), 0o700); err != nil {
		t.Fatalf("ディレクトリを作れません: %v", err)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelRuntimeDir, doctor.SymbolMissing)
	assertSocketUnderRoot(t, fx, res.Detail)
}

// {"RUCM-PATH": "P020"}
//
// TestDoctorRuntimeDir_置き場所を決められなければ足りないと出す は、
// 置き場所そのものを用意できない場合を確かめる。
//
// 目的: `CONTINUO_RUNTIME_DIR` が絶対パスでないとき、`✗` にして直し方を出すこと。
// 与える情報: 相対パスを入れた `CONTINUO_RUNTIME_DIR`。
// 成功条件: `✗` で、直し方に `CONTINUO_RUNTIME_DIR` が出ること。
func TestDoctorRuntimeDir_置き場所を決められなければ足りないと出す(t *testing.T) {
	fx := newFixture(t)
	t.Setenv(envRuntimeDir, filepath.Join("relative", "run"))

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelRuntimeDir, doctor.SymbolMissing)
	if !strings.Contains(strings.Join(res.Remedies, "\n"), envRuntimeDir) {
		t.Fatalf("置き場所の直し方が出ていない: %v", res.Remedies)
	}
}
