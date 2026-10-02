// {"RUCM-CFG-SHA256": "4f2e2eaf480b50efd24ae739b618a524d1e218aff6f81c187a0171a24bd030bd", "SOURCE": "docs/spec/usecases/particular_case/前提が揃っているかを検査する.cfg.json"}
//
// **ユースケース記述「前提が揃っているかを検査する」の経路に対応づけたテストである。**
// 関数名の `P001` などは、その記述の経路の番号である。経路の中身は 1行目の SOURCE の CFG に在る。
// **経路の全部には書かない。**同じ経路を別の観点で確かめるテストは、同じ番号を持つ。
//
// **このファイルには、経路に対応するテストだけを置く。**経路に対応しないテストと補助関数は、
// 同じディレクトリの別のファイルに在る。
package doctor_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/doctor"
	"github.com/maimuzo/continuo/test/testlang"
)

// {"RUCM-PATH": "P019"}
//
// Test_前提が揃っているかを検査する_P019_位置引数を2つ以上渡したら使い方の誤りとして止まる は、引数の受け取り方を固定する。
//
// 目的: WORKFLOW.md のパスは1つだけ受け付け、2つ以上なら終了コード 2 で止まること
// （`continuo` 本体・`continuo init` と同じ扱い）。
// 与える情報: 位置引数を2つ渡した起動。
// 成功条件: 終了コードが 2 で、標準エラーに理由が出ること。
func Test_前提が揃っているかを検査する_P019_位置引数を2つ以上渡したら使い方の誤りとして止まる(t *testing.T) {
	fx := newFixture(t)
	bin := buildBinary(t, fx.Root)

	cmd := exec.Command(bin, "doctor", fx.WorkflowPath, "もう1つ")
	cmd.Dir = fx.Root
	cmd.Env = []string{"PATH=" + fx.BinDir, "HOME=" + fx.Home, testlang.EnvEntry()}
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

// {"RUCM-PATH": "P013"}
//
// Test_前提が揃っているかを検査する_P013_接続先がループバック以外のhttpなら検査せずに止まる は、
// トークンの送り先の検査が `continuo doctor` にも入っていることを確かめる。
//
// 目的: doctor もカンバンを読むために `gh auth token` のトークンを送る。**常駐プロセスと
// 同じ検査を通していないと、doctor だけが平文で外部へトークンを送る経路になる。**
// 与える情報: `CONTINUO_GITHUB_GRAPHQL_ENDPOINT` に `http://example.com/graphql`。
// 成功条件: 検査結果を出さずに止まり、終了コードが `✗` の 1 とも引数の誤りの 2 とも
// 違う 3 になること。文言が https を求めていること。
func Test_前提が揃っているかを検査する_P013_接続先がループバック以外のhttpなら検査せずに止まる(t *testing.T) {
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

// {"RUCM-PATH": "P004"}
//
// Test_前提が揃っているかを検査する_P004_検査結果を書き出せなければ終了コード3で止まる は、
// **検査そのものは動いたが、結果を届けられなかった場合**の応答を固定する。
//
// 目的: 検査結果を書き出せないとき、理由を標準エラーへ出し、終了コードを 3 にすること。
// **`✗` があったことの 1 とも、引数の誤りの 2 とも別の値にする**（設計 3-32）。
// 書き出せなかったことを 0 で返すと、検査結果を読めていないのに「前提は揃っている」
// と受け取られる。
// 与える情報: 検査は全項目 `✓` を返し、標準出力への書き込みだけが必ず失敗する状態。
// 成功条件: 終了コードが 3 で、標準エラーに書き出せない理由が出ること。
func Test_前提が揃っているかを検査する_P004_検査結果を書き出せなければ終了コード3で止まる(t *testing.T) {
	// **接続先の検査より先に進ませる。**空なら本番の GitHub を指すが、
	// 検査そのものは差し替えてあるので、どこへも繋がない。
	t.Setenv("CONTINUO_GITHUB_GRAPHQL_ENDPOINT", "")

	called := false
	deps := cli.Deps{
		DoctorRun: func(_ context.Context, _ doctor.Options) doctor.Report {
			called = true
			return doctor.Report{Results: []doctor.Result{
				{Label: doctor.LabelConfig, Symbol: doctor.SymbolOK, Detail: "読めました"},
			}}
		},
	}
	writeErr := errors.New("書き出し先が閉じています")
	var stderr bytes.Buffer

	code := cli.RunWith(deps, []string{"doctor", t.TempDir()},
		strings.NewReader(""), failingWriter{err: writeErr}, &stderr)

	if !called {
		t.Fatalf("検査そのものが走っていない（書き出しより前で止まっている）:\n%s", stderr.String())
	}
	if code != 3 {
		t.Fatalf("終了コードが 3 ではなく %d だった:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), writeErr.Error()) {
		t.Fatalf("書き出せない理由が出ていない:\n%s", stderr.String())
	}
}

// {"RUCM-PATH": "P005"}
//
// Test_前提が揃っているかを検査する_P005_設定ファイルが無ければ雛形の作成を勧める は、直し方を理由で分けたことを確かめる。
//
// 目的: 設定ファイルが**無い**（ENOENT）ときだけ `continuo init` を勧めること。
// 与える情報: WORKFLOW.md を消した状態。
// 成功条件: 直し方に `continuo init` が入り、ファイルシステムの案内が混ざらず、
// 終了コードが 1 になること。
func Test_前提が揃っているかを検査する_P005_設定ファイルが無ければ雛形の作成を勧める(t *testing.T) {
	fx := newFixture(t)
	if err := os.Remove(fx.WorkflowPath); err != nil {
		t.Fatalf("WORKFLOW.md を消せません: %v", err)
	}

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelConfig, doctor.SymbolMissing)
	remedies := strings.Join(res.Remedies, "\n")
	if !strings.Contains(remedies, "continuo init") {
		t.Fatalf("設定ファイルが無いのに雛形の作成を勧めていない: %v", res.Remedies)
	}
	if strings.Contains(remedies, "wsl --shutdown") {
		t.Fatalf("ただ無いだけなのにファイルシステムの案内を出している: %v", res.Remedies)
	}
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
}

// {"RUCM-PATH": "P007"}
//
// Test_前提が揃っているかを検査する_P007_設定ファイルを読めないだけなら雛形の作成を勧めない は、
// **`continuo init` の案内で本物の設定を潰させないこと**を確かめる（issue #11）。
//
// 目的: ファイルは在るが読めない（EACCES）とき、`continuo init` を勧めないこと。
// その案内に従うと `continuo init` は「既にあります」で止まり、
// **`--force` を足すと本物の設定を雛形で潰す。**
// 与える情報: WORKFLOW.md の権限を 0 にした状態。
// 成功条件: 直し方が所有者と権限の確認になり、`continuo init` を含まず、
// 終了コードが 1 になること。
func Test_前提が揃っているかを検査する_P007_設定ファイルを読めないだけなら雛形の作成を勧めない(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root は権限に関係なく読めるので、この検査は成立しない")
	}
	fx := newFixture(t)
	if err := os.Chmod(fx.WorkflowPath, 0o000); err != nil {
		t.Fatalf("WORKFLOW.md の権限を変えられません: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(fx.WorkflowPath, 0o600) })

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelConfig, doctor.SymbolMissing)
	remedies := strings.Join(res.Remedies, "\n")
	if strings.Contains(remedies, "continuo init") {
		t.Fatalf("読めないだけなのに雛形の作成を勧めている: %v", res.Remedies)
	}
	if !strings.Contains(remedies, "所有者と権限") {
		t.Fatalf("所有者と権限の確認を勧めていない: %v", res.Remedies)
	}
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった", report.ExitCode())
	}
}

// {"RUCM-PATH": "P011"}
//
// Test_前提が揃っているかを検査する_P011_token_envの書き漏らしは設定ファイルの検査で足りないと出る は、
// 設定の書き漏らしがどこで捕まるかを固定する。
//
// 目的: `rate_limit.token_source` が `env` なのに `token_env` が空なら、continuo は
// 起動できない。**その書き漏らしを doctor が見逃さないこと**を確かめる
// （判定は `config.Load` が持ち、doctor はその結果を記号にする。設計 3-32）。
// 与える情報: `token_env: ""` を明示した設定。
// 成功条件: 設定ファイルが `✗` になり、説明が `rate_limit.token_env` を指すこと。
// 下流の資格情報は `!`（設定を読めていないので確かめられない）で、終了コードは 1 であること。
func Test_前提が揃っているかを検査する_P011_token_envの書き漏らしは設定ファイルの検査で足りないと出る(t *testing.T) {
	fx := newFixture(t)
	fx.WriteWorkflow(t, "rate_limit:\n  source: oauth_usage_api\n  token_source: env\n  token_env: \"\"\n")

	report := fx.Run(t)

	res := assertSymbol(t, report, doctor.LabelConfig, doctor.SymbolMissing)
	if !strings.Contains(res.Detail, "token_env") {
		t.Fatalf("説明が token_env の未設定を指していない: %q", res.Detail)
	}
	assertSymbol(t, report, doctor.LabelCredentials, doctor.SymbolUnknown)
	if report.ExitCode() != 1 {
		t.Fatalf("✗ があるのに終了コードが %d だった\n%s", report.ExitCode(), renderReport(t, report))
	}
}
