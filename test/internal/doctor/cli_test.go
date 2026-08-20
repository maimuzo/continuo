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

// TestDoctorCLI_前提が揃っていれば7項目を出して終了コードは0 は、
// **ビルドしたバイナリを実際に起動して**出力と終了コードを確かめる。
//
// 目的: `continuo doctor` が7項目を固定の見出し語で出し、すべて通れば 0 で終わること。
// 与える情報: テスト用herdr mock・偽ボード・テスト用gh / ghq mock・一時ディレクトリのホーム
// （**本番のボードにも実 herdr にも繋がない**）。
// 成功条件: 出力に7つの見出し語と `✓` が並び、終了コードが 0 であること。
func TestDoctorCLI_前提が揃っていれば7項目を出して終了コードは0(t *testing.T) {
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
// 成功条件: 出力に `✗ clone` と `→ ghq get maimuzo/koetsumugi` が出て、
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
	if !strings.Contains(out, "→ ghq get maimuzo/koetsumugi を実行してください") {
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

// TestDoctorCLI_位置引数を2つ以上渡したら使い方の誤りとして止まる は、引数の受け取り方を固定する。
//
// 目的: WORKFLOW.md のパスは1つだけ受け付け、2つ以上なら終了コード 2 で止まること
// （`continuo` 本体・`continuo init` と同じ扱い）。
// 与える情報: 位置引数を2つ渡した起動。
// 成功条件: 終了コードが 2 で、標準エラーに理由が出ること。
func TestDoctorCLI_位置引数を2つ以上渡したら使い方の誤りとして止まる(t *testing.T) {
	fx := newFixture(t)
	bin := buildBinary(t, fx.Root)

	cmd := exec.Command(bin, "doctor", fx.WorkflowPath, "もう1つ")
	cmd.Dir = fx.Root
	cmd.Env = []string{"PATH=" + fx.BinDir, "HOME=" + fx.Home}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("位置引数が2つあるのに正常終了した:\n%s", out)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("continuo doctor を実行できません: %v\n%s", err, out)
	}
	if exitErr.ExitCode() != 2 {
		t.Fatalf("終了コードが 2 ではなく %d だった:\n%s", exitErr.ExitCode(), out)
	}
	if !strings.Contains(string(out), "1つだけ受け付けます") {
		t.Fatalf("理由が出ていない:\n%s", out)
	}
}

// TestDoctorCLI_接続先がループバック以外のhttpなら検査せずに止まる は、
// トークンの送り先の検査が `continuo doctor` にも入っていることを確かめる。
//
// 目的: doctor もボードを読むために `gh auth token` のトークンを送る。**常駐プロセスと
// 同じ検査を通していないと、doctor だけが平文で外部へトークンを送る経路になる。**
// 与える情報: `CONTINUO_GITHUB_GRAPHQL_ENDPOINT` に `http://example.com/graphql`。
// 成功条件: 検査結果を出さずに止まり、終了コードが `✗` の 1 とも引数の誤りの 2 とも
// 違う 3 になること。文言が https を求めていること。
func TestDoctorCLI_接続先がループバック以外のhttpなら検査せずに止まる(t *testing.T) {
	fx := newFixture(t)
	bin := buildBinary(t, fx.Root)

	out, code := runDoctorBinaryWithEndpoint(t, fx, bin, "http://example.com/graphql")

	if code != 3 {
		t.Fatalf("終了コードが 3 ではない: got %d\n%s", code, out)
	}
	if !strings.Contains(out, "https") {
		t.Fatalf("https を求める文言が出ていない:\n%s", out)
	}
	if strings.Contains(out, doctor.LabelText(doctor.LabelBoard)) {
		t.Fatalf("接続先が不正なのに検査を始めている:\n%s", out)
	}
}
