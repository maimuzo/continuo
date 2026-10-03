package doctor_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/doctor"
	"github.com/maimuzo/continuo/test/testlang"
)

// buildBinary は `continuo` をビルドする。
//
// **リポジトリの中には出力しない**（生成物を残さないため、テストの一時ディレクトリへ出す）。
//
// t: 呼び出し元のテスト。
// outDir: 出力先のディレクトリ。
// 戻り値: ビルドしたバイナリの絶対パス。
func buildBinary(t *testing.T, outDir string) string {
	t.Helper()

	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("リポジトリの場所を決められません: %v", err)
	}

	bin := filepath.Join(outDir, "continuo")
	cmd := exec.Command(goBin, "build", "-o", bin, "./cmd/continuo")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("continuo をビルドできません: %v\n%s", err, out)
	}
	return bin
}

// runDoctorBinary は `continuo doctor` をビルドしたバイナリで実行する。
//
// **環境変数は明示的に組み立てる。**本物の `HERDR_SOCKET_PATH` や `GH_TOKEN`、
// 本物のホームディレクトリを継承させないためである。
//
// t: 呼び出し元のテスト。
// fx: 偽のサーバと一時ディレクトリの一式。
// bin: ビルドしたバイナリの絶対パス。
// 戻り値の1つ目: 標準出力と標準エラーを連結した出力。
// 戻り値の2つ目: 終了コード。
func runDoctorBinary(t *testing.T, fx *fixture, bin string) (string, int) {
	t.Helper()
	return runDoctorBinaryWithEndpoint(t, fx, bin, fx.GitHub.URL)
}

// runDoctorBinaryWithEndpoint は接続先を指定して `continuo doctor` を実行する。
//
// t: 呼び出し元のテスト。
// fx: 偽のサーバと一時ディレクトリの一式。
// bin: ビルドしたバイナリの絶対パス。
// endpoint: `CONTINUO_GITHUB_GRAPHQL_ENDPOINT` に入れる値。
// 戻り値の1つ目: 標準出力と標準エラーを連結した出力。
// 戻り値の2つ目: 終了コード。
func runDoctorBinaryWithEndpoint(t *testing.T, fx *fixture, bin, endpoint string) (string, int) {
	t.Helper()

	cmd := exec.Command(bin, "doctor", fx.WorkflowPath)
	cmd.Dir = fx.Root
	cmd.Env = []string{
		"PATH=" + fx.BinDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + fx.Home,
		"CONTINUO_GITHUB_GRAPHQL_ENDPOINT=" + endpoint,
		"CONTINUO_TEST_TOKEN=dummy-token-for-the-fake-server",
		testlang.EnvEntry(),
	}
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		// **`errors.As` で判定する。**`os/exec` が将来エラーを包んでも、
		// 「起動できません」に化けさせない。
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("continuo doctor を実行できません: %v\n%s", err, out)
		}
		code = exitErr.ExitCode()
	}
	return string(out), code
}

// TestDoctorCLI_前提が揃っていれば全項目を出して終了コードは0 は、
// **ビルドしたバイナリを実際に起動して**出力と終了コードを確かめる。
//
// 目的: `continuo doctor` が固定の見出し語を並べて出し、すべて通れば 0 で終わること。
// 与える情報: テスト用herdr mock・偽カンバン・テスト用gh / ghq mock・一時ディレクトリのホーム
// （**本番のカンバンにも実 herdr にも繋がない**）。
// 成功条件: 出力に見出し語と `✓` が並び、終了コードが 0 であること。
func TestDoctorCLI_前提が揃っていれば全項目を出して終了コードは0(t *testing.T) {
	fx := newFixture(t)
	// **信頼の検査は PATH のテスト用ghq mock が返すパスで行う**（注入は使わない経路を通す）。
	fx.GhqPaths = nil
	bin := buildBinary(t, fx.Root)

	out, code := runDoctorBinary(t, fx, bin)

	for _, label := range wantLabels {
		// **キーではなく、実際に画面へ出る語を探す**（設計 3-35）。
		text := doctor.LabelText(label)
		if !strings.Contains(out, text) {
			t.Fatalf("見出し語 %q が出力に無い:\n%s", text, out)
		}
	}
	if strings.Contains(out, "✗") || strings.Contains(out, "!") {
		t.Fatalf("すべて通るはずなのに問題が出ている:\n%s", out)
	}
	if !strings.Contains(out, "前提はすべて揃っています") {
		t.Fatalf("集計の行が出ていない:\n%s", out)
	}
	if code != 0 {
		t.Fatalf("終了コードが %d だった:\n%s", code, out)
	}
}

// TestDoctorCLI_足りないものがあれば直し方を出して終了コードは1 は、
// **ビルドしたバイナリで**失敗の出力を確かめる。
//
// 目的: `✗` が1つでもあれば終了コードが 1 になり、直し方が `→ ` 付きで出ること。
// 与える情報: clone が見つからない偽の `ghq`（出力が空）。ほかは揃っている。
// 成功条件: 出力に `✗ clone` と `→ ghq get octocat/hello-world` が出て、
// 信頼登録が `!` になり、終了コードが 1 であること。
func TestDoctorCLI_足りないものがあれば直し方を出して終了コードは1(t *testing.T) {
	fx := newFixture(t)
	fx.GhqPaths = nil
	// **出力が空 = clone が無い**（`ghq` の exit code は存在の有無にかかわらず 0 である）。
	writeFakeGhq(t, fx.BinDir, "", fx.GhqArgsFile)
	bin := buildBinary(t, fx.Root)

	out, code := runDoctorBinary(t, fx, bin)

	if !strings.Contains(out, "✗ clone") {
		t.Fatalf("clone が ✗ になっていない:\n%s", out)
	}
	if !strings.Contains(out, "→ ghq get --vcs git https://github.com/octocat/hello-world を実行してください") {
		t.Fatalf("直し方が出ていない:\n%s", out)
	}
	if !strings.Contains(out, "! 信頼登録") {
		t.Fatalf("信頼登録が ! になっていない:\n%s", out)
	}
	if !strings.Contains(out, "件に問題があります") {
		t.Fatalf("集計の行が出ていない:\n%s", out)
	}
	if code != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった:\n%s", code, out)
	}
}

// failingWriter は書き込みを必ず失敗させる出力先である。
//
// **検査結果の書き出しが失敗する状態は、外から作れない。**リダイレクト先の
// ディスクが一杯になった場合などがそれに当たるが、テストで再現できないので、
// 出力先そのものを失敗させて `continuo doctor` の応答を確かめる。
type failingWriter struct {
	// err は Write が必ず返すエラーである。
	err error
}

// Write は書き込まずに err を返す。
//
// p: 書き込もうとした内容（使わない）。
// 戻り値: 書けた byte 数（常に 0）と、失敗の理由。
func (w failingWriter) Write(p []byte) (int, error) { return 0, w.err }
