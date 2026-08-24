// {"RUCM-CFG-SHA256": "1bf01241889b9e6e9759b0792ea55c34364edc6c75fbd389fc68bffe349f4adb", "SOURCE": "docs/spec/usecases/particular_case/既存のボードの Status を割り当てる.cfg.json"}
//
// **RUCM のテストパスに対応づけたテストである。**
// CLI の入口（`cli.Run`）の検査である。
//
// **外部へ1回も接続しない。**GitHub も herdr も Keychain も叩かないところまでで判定する。
// 外部へ繋ぐ処理は `internal/cli` の差し替え点から偽物へ向ける。
package cli_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maimuzo/continuo/internal/abandon"
	"github.com/maimuzo/continuo/internal/cli"
	"github.com/maimuzo/continuo/internal/daemon"
	"github.com/maimuzo/continuo/internal/doctor"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/scaffold"
	"github.com/maimuzo/continuo/internal/setup"
	"github.com/maimuzo/continuo/internal/trust"
)

// runCLI は run を呼び、終了コードと出力を返す。
//
// args: `continuo` に続く引数。
// stdin: 標準入力の中身。
// 戻り値の1つ目: 終了コード。
// 戻り値の2つ目: stdout の中身。
// 戻り値の3つ目: stderr の中身。
func runCLI(args []string, stdin string) (int, string, string) {
	return runCLIWith(cli.Deps{}, args, stdin)
}

// runCLIWith は外部へ繋ぐ処理を差し替えて CLI を呼ぶ。
//
// deps: 差し替えたい処理だけを埋めた Deps。
// args: `continuo` に続く引数。
// stdin: 標準入力の中身。
// 戻り値: 終了コードと stdout / stderr。
func runCLIWith(deps cli.Deps, args []string, stdin string) (int, string, string) {
	var out, errBuf bytes.Buffer
	code := cli.RunWith(deps, args, strings.NewReader(stdin), &out, &errBuf)
	return code, out.String(), errBuf.String()
}

// writeWorkflowFor は、CLI を通すための WORKFLOW.md を1つ置く。
//
// **雛形をそのまま置くと `owner` と `project_number` がプレースホルダのままで
// 検証に落ちる。**CLI がどこまで進むかを見たいので、そこだけ埋める。
//
// t: 呼び出し元のテスト。
// 戻り値: 置いたディレクトリ。
func writeWorkflowFor(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := scaffold.TemplateWithValues(scaffold.Values{Owner: "octocat", ProjectNumber: 3})
	if err := os.WriteFile(filepath.Join(dir, "WORKFLOW.md"), []byte(out), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}
	return dir
}

// TestRun_ヘルプは終了コード0で返す は、`--help` を誤りとして扱わないことを確かめる。
//
// 目的: `flag.ErrHelp` を受けたときに 0 を返すこと（2 ではない）。
// 与える情報: 各サブコマンドへの `--help`。
// 成功条件: すべて終了コードが 0 であること。
func TestRun_ヘルプは終了コード0で返す(t *testing.T) {
	for _, sub := range []string{"init", "setup", "trust", "doctor"} {
		t.Run(sub, func(t *testing.T) {
			code, _, _ := runCLI([]string{sub, "--help"}, "")
			if code != 0 {
				t.Errorf("`continuo %s --help` の終了コードが 0 でない: %d", sub, code)
			}
		})
	}
}

// TestRun_知らないフラグは終了コード2で返す は、引数の誤りを黙って進めないことを確かめる。
//
// 目的: 解釈できないフラグを渡したとき、外部へ接続する前に 2 で止まること。
// 与える情報: 各サブコマンドへの `--no-such-flag`。
// 成功条件: すべて終了コードが 2 で、stderr に何か出ていること。
func TestRun_知らないフラグは終了コード2で返す(t *testing.T) {
	for _, sub := range []string{"init", "setup", "trust", "doctor"} {
		t.Run(sub, func(t *testing.T) {
			code, _, stderr := runCLI([]string{sub, "--no-such-flag"}, "")
			if code != 2 {
				t.Errorf("`continuo %s --no-such-flag` の終了コードが 2 でない: %d", sub, code)
			}
			if stderr == "" {
				t.Errorf("何が誤りかを stderr へ出していない")
			}
		})
	}
}

// TestRun_位置引数のあとに書いたフラグを黙って無視しない は、設計 3-32 の約束を確かめる。
//
// **`continuo init ./dir --force` のような並びは、Go の flag では `--force` が
// 位置引数として扱われ、黙って効かない。**気づかないまま「上書きされなかった」と
// 悩むことになるので、誤りとして落とす。
//
// 目的: 位置引数のあとにフラグを書いたら 2 で止まること。
// 与える情報: `init <ディレクトリ> --force`。
// 成功条件: 終了コードが 2 で、そのフラグの名前が stderr に出ていること。
func TestRun_位置引数のあとに書いたフラグを黙って無視しない(t *testing.T) {
	dir := t.TempDir()
	code, _, stderr := runCLI([]string{"init", dir, "--force"}, "")
	if code != 2 {
		t.Fatalf("終了コードが 2 でない: %d（stderr: %s）", code, stderr)
	}
	if !strings.Contains(stderr, "--force") {
		t.Errorf("どのフラグが問題かを示していない: %s", stderr)
	}
}

// TestRun_位置引数が多すぎたら落とす は、引数の数の検査を確かめる。
//
// 目的: WORKFLOW.md のパスは0個か1個であり、2個以上なら 2 で止まること。
// 与える情報: `init a b`。
// 成功条件: 終了コードが 2。
func TestRun_位置引数が多すぎたら落とす(t *testing.T) {
	for _, sub := range []string{"init", "setup", "trust", "doctor"} {
		t.Run(sub, func(t *testing.T) {
			code, _, _ := runCLI([]string{sub, "a", "b"}, "")
			if code != 2 {
				t.Errorf("`continuo %s a b` の終了コードが 2 でない: %d", sub, code)
			}
		})
	}
}

// TestRunInit_ownerの形が不正なら外部へ接続する前に落とす は、段0 の検査を確かめる。
//
// 目的: `--owner` に GitHub のアカウント名として成り立たない文字列を渡したとき、
// `gh` を1回も起動せずに 2 で止まること。
// 与える情報: 空白や記号を含む owner。
// 成功条件: すべて終了コードが 2。
func TestRunInit_ownerの形が不正なら外部へ接続する前に落とす(t *testing.T) {
	for _, owner := range []string{"has space", "-leading", "trailing-", "a/b", strings.Repeat("x", 40)} {
		t.Run(owner, func(t *testing.T) {
			code, _, stderr := runCLI([]string{"init", "--owner", owner, t.TempDir()}, "")
			if code != 2 {
				t.Errorf("owner=%q の終了コードが 2 でない: %d（stderr: %s）", owner, code, stderr)
			}
		})
	}
}

// TestRunInit_projectが0以下なら落とす は、ボードの番号の検査を確かめる。
//
// 目的: `--project 0` や負の数を、ボードを引きに行く前に弾くこと。
// 与える情報: 0 と -1。
// 成功条件: 終了コードが 2。
func TestRunInit_projectが0以下なら落とす(t *testing.T) {
	for _, n := range []string{"0", "-1"} {
		t.Run(n, func(t *testing.T) {
			code, _, _ := runCLI([]string{"init", "--project", n, t.TempDir()}, "")
			if code != 2 {
				t.Errorf("--project %s の終了コードが 2 でない: %d", n, code)
			}
		})
	}
}

// TestRunSetup_statusFieldが空なら落とす は、尋ねる前の検査を確かめる。
//
// **5問すべて答えさせたあとで落とすと、入力が全部捨てられる**（設計 3-32）。
//
// 目的: `--status-field ""` を、ボードを読みに行く前に弾くこと。
// 与える情報: 空文字と空白だけの文字列。
// 成功条件: 終了コードが 2。
func TestRunSetup_statusFieldが空なら落とす(t *testing.T) {
	for _, v := range []string{"", "   "} {
		t.Run("["+v+"]", func(t *testing.T) {
			code, _, _ := runCLI([]string{"setup", "--status-field", v}, "")
			if code != 2 {
				t.Errorf("--status-field %q の終了コードが 2 でない: %d", v, code)
			}
		})
	}
}

// {"RUCM-PATH": "P010"}
//
// TestRunSetup_WORKFLOWmdが無ければ尋ねずに落とす は、RUCM の基本フロー2 を確かめる。
//
// 目的: 書き換える WORKFLOW.md が無いとき、**役割の割り当てを1つも尋ねずに**落ちること。
// 与える情報: 空のディレクトリ。
// 成功条件: 終了コードが 1 で、stdout に質問（`[1/5]`）が出ていないこと。
func TestRunSetup_WORKFLOWmdが無ければ尋ねずに落とす(t *testing.T) {
	code, stdout, stderr := runCLI([]string{"setup", t.TempDir()}, "")
	if code != 1 {
		t.Fatalf("終了コードが 1 でない: %d（stderr: %s）", code, stderr)
	}
	if strings.Contains(stdout, "[1/5]") {
		t.Errorf("WORKFLOW.md が無いのに役割を尋ねている: %s", stdout)
	}
	if stderr == "" {
		t.Errorf("なぜ止まったかを stderr へ出していない")
	}
}

// TestRunHook_ソケットの指定が無ければ落とす は、`continuo hook` の引数の検査を確かめる。
//
// 目的: `--socket` は必須であり、無ければ 1 で止まって理由を出すこと。
//
// **ほかのサブコマンドの引数の誤りは 2 だが、hook だけ 1 である。**
// hook は Claude Code が起動するので、終了コードを人間が読むことはない。
// 与える情報: 引数なしの `hook`。
// 成功条件: 終了コードが 1 で、`--socket` が要ることを stderr に出すこと。
func TestRunHook_ソケットの指定が無ければ落とす(t *testing.T) {
	code, _, stderr := runCLI([]string{"hook"}, `{"session_id":"x"}`)
	if code != 1 {
		t.Errorf("終了コードが 1 でない: %d（stderr: %s）", code, stderr)
	}
	if !strings.Contains(stderr, "--socket") {
		t.Errorf("何が足りないかを示していない: %s", stderr)
	}
}

// TestRunHook_相対パスのソケットは受け付けない は、設計の約束を確かめる。
//
// **hook は Claude Code が起動するので、いまいるディレクトリが何かを当てにできない。**
// 相対パスを許すと、worktree の中を指しているつもりで別の場所を指す。
//
// 目的: `--socket` に相対パスを渡したら止まること（終了コード 1）。
// 与える情報: `./hooks.sock`。
// 成功条件: 終了コードが 1 で、stderr に理由が出ること。
func TestRunHook_相対パスのソケットは受け付けない(t *testing.T) {
	code, _, stderr := runCLI([]string{"hook", "--socket", "./hooks.sock"}, `{"session_id":"x"}`)
	if code != 1 {
		t.Errorf("相対パスを受け付けている: 終了コード %d", code)
	}
	if stderr == "" {
		t.Errorf("なぜ受け付けないかを出していない")
	}
}

// TestRunTrust_dryRunなら1バイトも書き換えない は、`--dry-run` の約束を確かめる。
//
// **`--dry-run` は信頼のダイアログの代わりである。**何を許すことになるかを見せるだけで、
// `~/.claude.json` を書き換えてはならない。
//
// 目的: `--dry-run` を付けたとき、対象の一覧を出して終わること。
// 与える情報: owner とボードの番号を埋めた WORKFLOW.md。
// 成功条件: 落ちずに終わり、stdout か stderr に何か出ること
// （clone が無い環境でも「調べられなかった」まで進む）。
func TestRunTrust_dryRunなら1バイトも書き換えない(t *testing.T) {
	dir := writeWorkflowFor(t)
	code, stdout, stderr := runCLI([]string{"trust", "--dry-run", dir}, "")
	// **終了コードは環境で変わる**（clone の有無で 0 か 1）。ここで見るのは
	// 「引数を解釈して設定を読み、報告まで進んだ」ことである。
	if code == 2 {
		t.Fatalf("引数の誤りとして落ちている: stderr=%s", stderr)
	}
	if stdout == "" && stderr == "" {
		t.Error("何も報告していない")
	}
}

// TestRunDoctor_設定を読めなくても検査を続ける は、doctor の約束を確かめる。
//
// **1つ失敗しても残りを全部検査する**（設計 3-32）。設定ファイルが無いことは
// 「設定ファイルを読めない」という検査結果の1つであって、打ち切る理由ではない。
//
// 目的: WORKFLOW.md が無いディレクトリでも、検査の一覧を出して終わること。
// 与える情報: 空のディレクトリ。
// 成功条件: 終了コードが 2 ではなく（引数の誤りではない）、報告が出ること。
func TestRunDoctor_設定を読めなくても検査を続ける(t *testing.T) {
	code, stdout, stderr := runCLI([]string{"doctor", t.TempDir()}, "")
	if code == 2 {
		t.Fatalf("引数の誤りとして落ちている: stderr=%s", stderr)
	}
	if stdout == "" {
		t.Error("検査の結果を1件も出していない")
	}
}

// TestRunInit_雛形を置いてから2度目は上書きしない は、`--force` の要否を確かめる。
//
// **`continuo init` が既にある WORKFLOW.md を黙って上書きすると、
// 利用者が手で直した行（`trust.repositories` から消した行など）が全部消える。**
//
// 目的: 2度目の `init` が `--force` 無しでは上書きを拒むこと。
// 与える情報: 既に WORKFLOW.md があるディレクトリ。
// 成功条件: 終了コードが 0 でなく、ファイルの中身が変わらないこと。
func TestRunInit_雛形を置いてから2度目は上書きしない(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	const mark = "# 人間が手で足した行\n"
	if err := os.WriteFile(path, []byte(scaffold.Template()+mark), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	code, _, _ := runCLI([]string{"init", dir}, "")
	if code == 0 {
		t.Error("既にあるのに上書きを許している")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	if !strings.HasSuffix(string(got), mark) {
		t.Error("人間が足した行が消えている")
	}
}

// useFakeDoctor は doctor の検査を差し替え、終わったら元へ戻す。
//
// t: 呼び出し元のテスト。
// report: 返させる検査結果。
func fakeDoctor(report doctor.Report) cli.Deps {
	return cli.Deps{
		DoctorRun: func(_ context.Context, _ doctor.Options) doctor.Report { return report },
	}
}

// useFakeHome はホームディレクトリを一時ディレクトリへ向け、終わったら元へ戻す。
//
// **`~/.claude.json` を書き換える処理を検査するので、本物のホームへ向けてはならない。**
//
// t: 呼び出し元のテスト。
// 戻り値: 向けた先のディレクトリ。
func fakeHome(t *testing.T) (cli.Deps, string) {
	t.Helper()
	dir := t.TempDir()
	return cli.Deps{UserHomeDir: func() (string, error) { return dir, nil }}, dir
}

// TestRunDoctor_検査結果をそのまま出して終了コードに変える は、doctor の出力経路を確かめる。
//
// 目的: 検査結果の見出し語と直し方を stdout へ出し、終了コードを検査結果から決めること。
// 与える情報: 1件が `✗` の検査結果。
// 成功条件: その見出し語と直し方が出て、終了コードが 1 になること。
func TestRunDoctor_検査結果をそのまま出して終了コードに変える(t *testing.T) {
	deps := fakeDoctor(doctor.Report{Results: []doctor.Result{
		{Label: doctor.LabelClaude, Symbol: doctor.SymbolMissing,
			Detail: "claude が PATH にありません", Remedies: []string{"Claude Code を入れてください"}},
	}})

	code, stdout, _ := runCLIWith(deps, []string{"doctor", writeWorkflowFor(t)}, "")
	if code != 1 {
		t.Errorf("`✗` があるのに終了コードが 1 でない: %d", code)
	}
	for _, want := range []string{"claude が PATH にありません", "Claude Code を入れてください"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("%q を出していない: %s", want, stdout)
		}
	}
}

// TestRunDoctor_すべて通れば終了コード0 は、正常時の経路を確かめる。
//
// 目的: 検査がすべて `✓` なら 0 を返すこと。
// 与える情報: `✓` だけの検査結果。
// 成功条件: 終了コードが 0 で、報告が出ること。
func TestRunDoctor_すべて通れば終了コード0(t *testing.T) {
	deps := fakeDoctor(doctor.Report{Results: []doctor.Result{
		{Label: doctor.LabelConfig, Symbol: doctor.SymbolOK, Detail: "読めました"},
		{Label: doctor.LabelClaude, Symbol: doctor.SymbolOK, Detail: "/usr/local/bin/claude"},
	}})

	code, stdout, stderr := runCLIWith(deps, []string{"doctor", writeWorkflowFor(t)}, "")
	if code != 0 {
		t.Errorf("すべて通ったのに終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if !strings.Contains(stdout, "/usr/local/bin/claude") {
		t.Errorf("検査結果を出していない: %s", stdout)
	}
}

// TestRunTrust_dryRunは書き込まずに対象を並べる は、`--dry-run` が書き換えないことを確かめる。
//
// **`--dry-run` は「何を許すことになるか」を見せるためのものである。**
// 1バイトでも書き換えたら、読むだけのつもりで叩いた人を裏切る。
//
// 目的: `--dry-run` のとき `~/.claude.json` を作らないこと。
// 与える情報: ホームディレクトリを空の一時ディレクトリへ向けた状態。
// 成功条件: `.claude.json` が作られないこと。
func TestRunTrust_dryRunは書き込まずに対象を並べる(t *testing.T) {
	deps, home := fakeHome(t)
	_, stdout, stderr := runCLIWith(deps, []string{"trust", "--dry-run", filepath.Join(writeWorkflowFor(t), "WORKFLOW.md")}, "")

	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Errorf("--dry-run なのに ~/.claude.json を作っている: %v", err)
	}
	if stdout == "" && stderr == "" {
		t.Error("何を許すことになるかを報告していない")
	}
}

// TestRunTrust_ホームディレクトリを引けなければ書き込まない は、書き込み先の確定を確かめる。
//
// **引けないまま既定値へ落とすと、別の場所を書き換えることになる。**
//
// 目的: ホームディレクトリを引けないとき、何も書き換えずに落ちること。
// 与える情報: 常に失敗する userHomeDir。
// 成功条件: 終了コードが 0 でなく、stderr に理由が出ること。
func TestRunTrust_ホームディレクトリを引けなければ書き込まない(t *testing.T) {
	deps := cli.Deps{
		UserHomeDir: func() (string, error) { return "", errors.New("ホームを引けません") },
	}

	code, _, stderr := runCLIWith(deps, []string{"trust", filepath.Join(writeWorkflowFor(t), "WORKFLOW.md")}, "")
	if code == 0 {
		t.Error("ホームを引けないのに成功として終わっている")
	}
	if stderr == "" {
		t.Error("なぜ止まったかを出していない")
	}
}

// TestRunDoctor_設定の言語で検査結果を出す は、設計 3-35 を確かめる。
//
// **設定が主、環境変数 LANG が従である。**
//
// 目的: WORKFLOW.md の `language` に従って見出し語の言語が決まること。
// 与える情報: `language: en` を書いた WORKFLOW.md。
// 成功条件: 呼んだあとの言語が英語になっていること。
func TestRunDoctor_設定の言語で検査結果を出す(t *testing.T) {
	t.Cleanup(func() { i18n.Use(i18n.FromEnv(os.Getenv)) })
	deps := fakeDoctor(doctor.Report{Results: []doctor.Result{
		{Label: doctor.LabelConfig, Symbol: doctor.SymbolOK, Detail: "ok"},
	}})

	dir := writeWorkflowFor(t)
	path := filepath.Join(dir, "WORKFLOW.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	out := strings.Replace(string(body), "language: auto", "language: en", 1)
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatalf("WORKFLOW.md を書けません: %v", err)
	}

	i18n.Use(i18n.LangJA)
	runCLIWith(deps, []string{"doctor", dir}, "")
	if got := i18n.Current(); got != i18n.LangEN {
		t.Errorf("設定の language が効いていない: %v", got)
	}
}

// TestRunMain_起動で落ちたか巡回で落ちたかを言い分ける は、終了コードの決め方を確かめる。
//
// **どちらも終了コードは 1 だが、人間が次にやることが違う。**
// 起動で落ちたなら設定か前提を直す。巡回で落ちたなら、動いていた run の後始末を見る。
// **ログの文言で言い分ける**ので、その文言が出ることを確かめる。
//
// 目的: `daemon.ErrStartup` を包んだエラーと、そうでないエラーを言い分けること。
// 与える情報: それぞれのエラーを返す daemon。
// 成功条件: どちらも終了コードが 1 で、stderr の文言が違うこと。
func TestRunMain_起動で落ちたか巡回で落ちたかを言い分ける(t *testing.T) {
	dir := writeWorkflowFor(t)

	startupCode, _, startupErr := runCLIWith(cli.Deps{
		DaemonRun: func(_ context.Context, _ daemon.Options) error {
			return fmt.Errorf("%w: herdr へ繋げません", daemon.ErrStartup)
		},
	}, []string{dir}, "")

	runningCode, _, runningErr := runCLIWith(cli.Deps{
		DaemonRun: func(_ context.Context, _ daemon.Options) error {
			return errors.New("巡回の途中で落ちました")
		},
	}, []string{dir}, "")

	if startupCode != 1 || runningCode != 1 {
		t.Errorf("終了コードが 1 でない: 起動=%d 巡回=%d", startupCode, runningCode)
	}
	if startupErr == runningErr {
		t.Errorf("起動と巡回を言い分けていない: %q", startupErr)
	}
	if !strings.Contains(startupErr, "herdr へ繋げません") {
		t.Errorf("起動の失敗の理由を出していない: %s", startupErr)
	}
	if !strings.Contains(runningErr, "巡回の途中で落ちました") {
		t.Errorf("巡回の失敗の理由を出していない: %s", runningErr)
	}
}

// TestRunMain_正常に終われば0を返す は、`Ctrl+C` での停止を確かめる。
//
// **`SIGINT` / `SIGTERM` での停止は失敗ではない。**1 を返すと、
// 監視の仕組みが「落ちた」と誤検知する。
//
// 目的: daemon が nil を返したら 0 を返すこと。
// 与える情報: nil を返す daemon。
// 成功条件: 終了コードが 0。
func TestRunMain_正常に終われば0を返す(t *testing.T) {
	deps := cli.Deps{DaemonRun: func(_ context.Context, _ daemon.Options) error { return nil }}

	code, _, stderr := runCLIWith(deps, []string{writeWorkflowFor(t)}, "")
	if code != 0 {
		t.Errorf("正常終了なのに %d を返している（stderr: %s）", code, stderr)
	}
}

// TestRunMain_portの指定が範囲外なら落とす は、引数の検査を確かめる。
//
// 目的: `--port` に使えない値を渡したら、常駐を始める前に落とすこと。
// 与える情報: 範囲外のポート番号。
// 成功条件: 終了コードが 2 で、daemon を1回も呼ばないこと。
func TestRunMain_portの指定が範囲外なら落とす(t *testing.T) {
	var called bool
	deps := cli.Deps{
		DaemonRun: func(_ context.Context, _ daemon.Options) error { called = true; return nil },
	}

	for _, port := range []string{"-1", "65536", "999999"} {
		t.Run(port, func(t *testing.T) {
			code, _, _ := runCLIWith(deps, []string{"--port", port, writeWorkflowFor(t)}, "")
			if code != 2 {
				t.Errorf("--port %s の終了コードが 2 でない: %d", port, code)
			}
		})
	}
	if called {
		t.Error("引数が誤っているのに常駐を始めている")
	}
}

// TestRunSetup_ボードを読めなければ尋ねずに落とす は、RUCM の基本フロー3 を確かめる。
//
// **5問すべて答えさせたあとで「ボードを読めません」と落とすと、入力が全部捨てられる。**
//
// 目的: ボードの Status を読めないとき、役割の割り当てを1つも尋ねないこと。
// 与える情報: 常に失敗する setupFetchStatusField。
// 成功条件: 終了コードが 1 で、stdout に質問（`[1/5]`）が出ないこと。
func TestRunSetup_ボードを読めなければ尋ねずに落とす(t *testing.T) {
	deps := cli.Deps{ScaffoldDetect: fixedDetection, SetupFetchStatusField: func(_ context.Context, _ setup.FetchOptions) (setup.StatusField, error) {
		return setup.StatusField{}, errors.New("ボードを読めません")
	}}

	code, stdout, stderr := runCLIWith(deps, []string{"setup", writeWorkflowFor(t)}, "")
	if code != 1 {
		t.Errorf("終了コードが 1 でない: %d", code)
	}
	if strings.Contains(stdout, "[1/5]") {
		t.Error("ボードを読めないのに役割を尋ねている")
	}
	if !strings.Contains(stderr, "ボードを読めません") {
		t.Errorf("なぜ止まったかを出していない: %s", stderr)
	}
}

// TestRunSetup_選択肢が5つ未満なら尋ねずに落とす は、尋ねる前の検査を確かめる。
//
// **1つの選択肢を2つの役割へ割り当てないので、選択肢が5つ未満のボードでは
// 対話が必ず途中で行き止まる。**尋ねる前に落として、GitHub の画面で足すよう案内する。
//
// 目的: 選択肢が4つ以下のとき、質問を1つも出さないこと。
// 与える情報: 選択肢を3つだけ返す setupFetchStatusField。
// 成功条件: 終了コードが 0 でなく、質問が出ないこと。
func TestRunSetup_選択肢が5つ未満なら尋ねずに落とす(t *testing.T) {
	deps := cli.Deps{ScaffoldDetect: fixedDetection, SetupFetchStatusField: func(_ context.Context, _ setup.FetchOptions) (setup.StatusField, error) {
		return setup.StatusField{Name: "Status", Options: []string{"Todo", "In Progress", "Done"}}, nil
	}}

	code, stdout, _ := runCLIWith(deps, []string{"setup", writeWorkflowFor(t)}, "")
	if code == 0 {
		t.Error("選択肢が足りないのに成功として終わっている")
	}
	if strings.Contains(stdout, "[1/5]") {
		t.Error("選択肢が足りないのに役割を尋ねている")
	}
}

// TestRunSetup_5つ答えれば WORKFLOW.md へ書き込む は、`continuo setup` の本筋を確かめる。
//
// 目的: 選択肢が5つあるボードで、5問に答えたら7つのキーを書き換えること。
// 与える情報: 選択肢を5つ返す setupFetchStatusField と、番号の入力。
// 成功条件: 終了コードが 0 で、WORKFLOW.md に割り当てた選択肢名が入ること。
func TestRunSetup_5つ答えればWORKFLOWmdへ書き込む(t *testing.T) {
	deps := cli.Deps{ScaffoldDetect: fixedDetection, SetupFetchStatusField: func(_ context.Context, _ setup.FetchOptions) (setup.StatusField, error) {
		return setup.StatusField{
			Name:    "Status",
			Options: []string{"着手待ち", "作業中", "レビュー待ち", "保留", "完了"},
		}, nil
	}}

	dir := writeWorkflowFor(t)
	code, stdout, stderr := runCLIWith(deps, []string{"setup", dir}, "1\n2\n3\n4\n5\n")
	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stdout: %s / stderr: %s）", code, stdout, stderr)
	}
	got, err := os.ReadFile(filepath.Join(dir, "WORKFLOW.md"))
	if err != nil {
		t.Fatalf("WORKFLOW.md を読めません: %v", err)
	}
	for _, want := range []string{"着手待ち", "作業中", "レビュー待ち", "保留", "完了"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("%q が書き込まれていない", want)
		}
	}
}

// TestRunTrust_登録の対象があれば書き込んで結果を出す は、`continuo trust` の本筋を確かめる。
//
// 目的: 未登録の項目があるとき、`Apply` を呼んで結果を報告すること。
// 与える情報: 未登録の項目を1件返す Plan と、書き込んだことにする Apply。
// 成功条件: 終了コードが 0 で、Apply が呼ばれ、登録した項目が stdout に出ること。
func TestRunTrust_登録の対象があれば書き込んで結果を出す(t *testing.T) {
	deps, _ := fakeHome(t)
	var applied bool
	deps.TrustPlan = func(_ context.Context, _ trust.Options) (*trust.Report, error) {
		return &trust.Report{
			ClaudeConfigPath: "/tmp/.claude.json",
			Entries: []trust.Entry{
				{Repository: "octocat/hello-world", ClonePath: "/repos/hello-world",
					TrustKey: "/repos/hello-world", Trusted: false},
			},
		}, nil
	}
	deps.TrustApply = func(_ context.Context, _ trust.Options, _ *trust.Report) (*trust.ApplyResult, error) {
		applied = true
		return &trust.ApplyResult{
			ClaudeConfigPath: "/tmp/.claude.json",
			BackupPath:       "/tmp/.claude.json.backup",
			Changed: []trust.Change{
				{Repository: "octocat/hello-world", TrustKey: "/repos/hello-world"},
			},
		}, nil
	}

	code, stdout, stderr := runCLIWith(deps, []string{"trust", filepath.Join(writeWorkflowFor(t), "WORKFLOW.md")}, "")
	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if !applied {
		t.Error("登録の対象があるのに Apply を呼んでいない")
	}
	if !strings.Contains(stdout, "octocat/hello-world") {
		t.Errorf("登録した項目を報告していない: %s", stdout)
	}
}

// TestRunTrust_dryRunならApplyを呼ばない は、`--dry-run` の約束を確かめる。
//
// 目的: `--dry-run` のとき、対象があっても `Apply` を呼ばないこと。
// 与える情報: 未登録の項目を1件返す Plan。
// 成功条件: Apply が呼ばれないこと。
func TestRunTrust_dryRunならApplyを呼ばない(t *testing.T) {
	deps, _ := fakeHome(t)
	var applied bool
	deps.TrustPlan = func(_ context.Context, _ trust.Options) (*trust.Report, error) {
		return &trust.Report{
			ClaudeConfigPath: "/tmp/.claude.json",
			Entries: []trust.Entry{
				{Repository: "octocat/hello-world", ClonePath: "/repos/hello-world",
					TrustKey: "/repos/hello-world", Trusted: false},
			},
		}, nil
	}
	deps.TrustApply = func(_ context.Context, _ trust.Options, _ *trust.Report) (*trust.ApplyResult, error) {
		applied = true
		return &trust.ApplyResult{}, nil
	}

	runCLIWith(deps, []string{"trust", "--dry-run", filepath.Join(writeWorkflowFor(t), "WORKFLOW.md")}, "")
	if applied {
		t.Error("--dry-run なのに Apply を呼んでいる")
	}
}

// TestRunTrust_調べられなければ落とす は、Plan の失敗を確かめる。
//
// 目的: 対象を調べられないとき、書き込まずに落ちること。
// 与える情報: 常に失敗する Plan。
// 成功条件: 終了コードが 0 でなく、Apply を呼ばないこと。
func TestRunTrust_調べられなければ落とす(t *testing.T) {
	deps, _ := fakeHome(t)
	var applied bool
	deps.TrustPlan = func(_ context.Context, _ trust.Options) (*trust.Report, error) {
		return nil, errors.New("git を実行できません")
	}
	deps.TrustApply = func(_ context.Context, _ trust.Options, _ *trust.Report) (*trust.ApplyResult, error) {
		applied = true
		return &trust.ApplyResult{}, nil
	}

	code, _, stderr := runCLIWith(deps, []string{"trust", filepath.Join(writeWorkflowFor(t), "WORKFLOW.md")}, "")
	if code == 0 {
		t.Error("調べられないのに成功として終わっている")
	}
	if applied {
		t.Error("調べられないのに書き込んでいる")
	}
	if !strings.Contains(stderr, "git を実行できません") {
		t.Errorf("なぜ止まったかを出していない: %s", stderr)
	}
}

// TestRunAllowKeychainAccess_macOS以外では何もしない は、OS の判定を確かめる。
//
// **`security` は macOS の標準コマンドであり、ほかの OS には無い。**
// **黙って失敗させると、Linux の利用者は「なぜ動かないのか」を知る手がかりを持たない。**
//
// 目的: macOS 以外では Keychain を叩かず、その旨を出して終わること。
// 与える情報: `goos` を linux にした状態。
// 成功条件: Keychain を1回も叩かず、OS 名を含む案内が出ること。
func TestRunAllowKeychainAccess_macOS以外では何もしない(t *testing.T) {
	deps := cli.Deps{GOOS: "linux"}
	var probed bool
	deps.ProbeKeychain = func(_ context.Context, _ time.Duration) (ratelimit.KeychainProbe, error) {
		probed = true
		return ratelimit.KeychainProbe{}, nil
	}

	_, stdout, _ := runCLIWith(deps, []string{"allow-keychain-access"}, "")
	if probed {
		t.Error("macOS 以外なのに Keychain を叩いている")
	}
	if !strings.Contains(stdout, "linux") {
		t.Errorf("どの OS で動いているかを示していない: %s", stdout)
	}
}

// TestRunAllowKeychainAccess_読めたら項目の名前だけを出す は、値を漏らさないことを確かめる。
//
// **この出力は端末とスクロールバッファに残る。**
// **トークンの値が1文字でも混ざってはならない。**
//
// 目的: 読めたとき、項目の名前だけを出すこと。
// 与える情報: 項目の名前を返す probeKeychain。
// 成功条件: 終了コードが 0 で、項目の名前が出ること。
func TestRunAllowKeychainAccess_読めたら項目の名前だけを出す(t *testing.T) {
	deps := cli.Deps{GOOS: "darwin"}
	deps.ProbeKeychain = func(_ context.Context, _ time.Duration) (ratelimit.KeychainProbe, error) {
		return ratelimit.KeychainProbe{
			Fields:         []string{"accessToken", "expiresAt", "refreshToken"},
			HasAccessToken: true,
		}, nil
	}

	code, stdout, stderr := runCLIWith(deps, []string{"allow-keychain-access"}, "")
	if code != 0 {
		t.Errorf("読めたのに終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if !strings.Contains(stdout, "accessToken") {
		t.Errorf("項目の名前を出していない: %s", stdout)
	}
}

// TestRunAllowKeychainAccess_期限内に返らなければ直し方を出す は、ダイアログで止まった場合を確かめる。
//
// **確認のダイアログが出たまま誰も答えないと、`security` は返らない。**
// **黙って待ち続けると、人間は何が起きているか分からない。**
//
// 目的: 期限切れのとき、何が起きているかと直し方を出すこと。
// 与える情報: `ErrKeychainTimeout` を返す probeKeychain。
// 成功条件: 終了コードが 0 でなく、案内が出ること。
func TestRunAllowKeychainAccess_期限内に返らなければ直し方を出す(t *testing.T) {
	deps := cli.Deps{GOOS: "darwin"}
	deps.ProbeKeychain = func(_ context.Context, _ time.Duration) (ratelimit.KeychainProbe, error) {
		return ratelimit.KeychainProbe{}, ratelimit.ErrKeychainTimeout
	}

	code, stdout, _ := runCLIWith(deps, []string{"allow-keychain-access"}, "")
	if code == 0 {
		t.Error("期限切れなのに成功として終わっている")
	}
	if stdout == "" {
		t.Error("何が起きたかを出していない")
	}
}

// fixedDetection は `gh` を叩かずに owner とボードの番号を返す。
//
// **`continuo setup` は本物の `gh` からボードの一覧を引く。**検査で差し替えないと、
// 実行した人のアカウントにあるボードの数で結果が変わる（2026-08-21 に実際に起きた）。
//
// 戻り値: owner と番号が埋まった検出結果。
func fixedDetection(_ context.Context, _ scaffold.DetectOptions) scaffold.Detection {
	return scaffold.Detection{
		Values: scaffold.Values{Owner: "octocat", ProjectNumber: 3},
		Fields: []scaffold.Field{
			{Key: scaffold.OwnerKey, Filled: true, Reason: "検査用に固定した値です"},
			{Key: scaffold.ProjectKey, Filled: true, Reason: "検査用に固定した値です"},
		},
	}
}

// TestRunInit_引けた値と引けなかった理由を両方出す は、`continuo init` の報告を確かめる。
//
// **`continuo init` は値を埋めるだけでなく、「なぜその値になったか」を出す。**
// **埋まらなかったキーは、何をすればよいかを出す。**出さないと、人間はプレースホルダの
// ままのファイルを渡されて途方に暮れる。
//
// 目的: 埋まったキーの値と理由、埋まらなかったキーの理由と直し方を出すこと。
// 与える情報: owner は埋まり、ボードの番号は候補が複数で埋まらない検出結果。
// 成功条件: 両方の理由と、候補の一覧と、プレースホルダが残っている旨が出ること。
func TestRunInit_引けた値と引けなかった理由を両方出す(t *testing.T) {
	deps := cli.Deps{ScaffoldDetect: func(_ context.Context, _ scaffold.DetectOptions) scaffold.Detection {
		return scaffold.Detection{
			Values: scaffold.Values{Owner: "octocat"},
			Fields: []scaffold.Field{
				{Key: scaffold.OwnerKey, Filled: true, Value: "octocat", Reason: "gh api user が返しました"},
				{
					Key:    scaffold.ProjectKey,
					Reason: "ボードの候補が2件あります",
					Candidates: []scaffold.Project{
						{Number: 3, Title: "開発ボード", URL: "https://github.com/users/octocat/projects/3"},
						{Number: 9, Title: "検証用", URL: "https://github.com/users/octocat/projects/9"},
					},
					Advice: []string{"`continuo init --project <番号>` で指定してください"},
				},
			},
		}
	}}

	code, stdout, stderr := runCLIWith(deps, []string{"init", t.TempDir()}, "")
	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	for _, want := range []string{
		"octocat",       // 埋まった値
		"gh api user",   // 埋まった理由
		"ボードの候補が2件あります", // 埋まらなかった理由
		"開発ボード",         // 候補の一覧
		"検証用",
		"--project", // 直し方
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("%q を出していない:\n%s", want, stdout)
		}
	}
}

// TestRunSetup_ボードの番号が決まらなければ候補を出して落とす は、setup の案内を確かめる。
//
// **`continuo setup` はどのボードの Status を読むかを決められないと進めない。**
// **候補を出さずに落とすと、人間は何を指定すればよいか分からない。**
//
// 目的: ボードの番号が決まらないとき、候補の一覧と直し方を出して落ちること。
// 与える情報: 候補が3件あって決まらない検出結果。
// 成功条件: 終了コードが 0 でなく、候補の名前と `--project` の案内が出ること。
func TestRunSetup_ボードの番号が決まらなければ候補を出して落とす(t *testing.T) {
	deps := cli.Deps{ScaffoldDetect: func(_ context.Context, _ scaffold.DetectOptions) scaffold.Detection {
		return scaffold.Detection{
			Values: scaffold.Values{Owner: "octocat"},
			Fields: []scaffold.Field{
				{Key: scaffold.OwnerKey, Filled: true, Value: "octocat", Reason: "gh api user が返しました"},
				{
					Key:    scaffold.ProjectKey,
					Reason: "ボードの候補が3件あります",
					Candidates: []scaffold.Project{
						{Number: 3, Title: "開発ボード", URL: "https://github.com/users/octocat/projects/3"},
						{Number: 8, Title: "検証用", URL: "https://github.com/users/octocat/projects/8"},
						{Number: 9, Title: "使い捨て", URL: "https://github.com/users/octocat/projects/9"},
					},
				},
			},
		}
	}}

	code, stdout, stderr := runCLIWith(deps, []string{"setup", writeWorkflowFor(t)}, "")
	if code == 0 {
		t.Error("ボードが決まらないのに成功として終わっている")
	}
	out := stdout + stderr
	for _, want := range []string{"開発ボード", "検証用", "使い捨て", "--project"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q を出していない:\n%s", want, out)
		}
	}
	if strings.Contains(stdout, "[1/5]") {
		t.Error("ボードが決まらないのに役割を尋ねている")
	}
}

// TestRunAllowKeychainAccess_読めなければ直し方を出す は、Keychain の失敗の案内を確かめる。
//
// **トークンの値をエラー文へ混ぜてはならない。**この出力は端末とスクロールバッファに残る。
//
// 目的: Keychain を読めないとき、何が起きたかと直し方を出すこと。
// 与える情報: エラーを返す probeKeychain。
// 成功条件: 終了コードが 0 でなく、案内が出ること。
func TestRunAllowKeychainAccess_読めなければ直し方を出す(t *testing.T) {
	deps := cli.Deps{
		GOOS: "darwin",
		ProbeKeychain: func(_ context.Context, _ time.Duration) (ratelimit.KeychainProbe, error) {
			return ratelimit.KeychainProbe{}, errors.New("security コマンドが失敗しました")
		},
	}

	code, stdout, _ := runCLIWith(deps, []string{"allow-keychain-access"}, "")
	if code == 0 {
		t.Error("読めないのに成功として終わっている")
	}
	if !strings.Contains(stdout, "security コマンドが失敗しました") {
		t.Errorf("何が起きたかを出していない:\n%s", stdout)
	}
}

// TestRunAllowKeychainAccess_accessTokenが無ければ落とす は、中身の検査を確かめる。
//
// **Keychain の項目は読めても、`accessToken` が空のことがある。**
// **そのまま「読めました」と出すと、人間は枠を読めると思い込む。**
//
// 目的: `accessToken` が無いとき、成功として終わらないこと。
// 与える情報: 項目はあるが `HasAccessToken` が偽の probe。
// 成功条件: 終了コードが 0 でないこと。
func TestRunAllowKeychainAccess_accessTokenが無ければ落とす(t *testing.T) {
	deps := cli.Deps{
		GOOS: "darwin",
		ProbeKeychain: func(_ context.Context, _ time.Duration) (ratelimit.KeychainProbe, error) {
			return ratelimit.KeychainProbe{Fields: []string{"expiresAt"}, HasAccessToken: false}, nil
		},
	}

	code, _, _ := runCLIWith(deps, []string{"allow-keychain-access"}, "")
	if code == 0 {
		t.Error("accessToken が無いのに成功として終わっている")
	}
}

// TestRunVersion_版を答える は、`continuo version` を確かめる。
//
// **ビルドのときに `-ldflags "-X …/internal/cli.version=v1.2.3"` で埋める。**
// **左辺は変数の完全な位置でなければならない。**`-X main.version=…` と書いていた版では、
// Go が何も言わないまま値が入らず、**入ったものが何版かを誰も確かめられなかった。**
//
// 目的: `version` サブコマンドが版を1行で答えること。
// 与える情報: `version` だけ。
// 成功条件: 終了コードが 0 で、何かしらの版が出ること。
func TestRunVersion_版を答える(t *testing.T) {
	code, stdout, stderr := runCLI([]string{"version"}, "")
	if code != 0 {
		t.Fatalf("終了コードが 0 ではありません: %d\n%s", code, stderr)
	}
	got := strings.TrimSpace(stdout)
	if got == "" {
		t.Error("版を1文字も答えていません")
	}
	// **設定ファイルを読みにいってはならない。**`version` を設定ファイルのパスとして
	// 解釈していた版では、`open …/version: no such file or directory` で落ちていた。
	if strings.Contains(stderr, "continuo を起動できません") {
		t.Errorf("version を設定ファイルのパスとして扱っています:\n%s", stderr)
	}
}

// TestRunAbandon_フラグを取り違えずに渡す は、引数の結線を確かめる。
//
// **`runAbandon` はフラグを `abandon.Options` へ結線する唯一の場所である。**
// `DryRun: *forceFlag` のような取り違えは、ここを通す検査が無ければ誰も気づかない。
// **本物の `abandon.Run` は worktree と branch と pane を消す**ので、差し替えて
// 渡ってきた値だけを見る。
//
// 目的: `--dry-run` / `--force` / `--to` / `--park` と issue の URL と
// WORKFLOW.md の場所が、それぞれ対応するフィールドへ入ること。
// 与える情報: すべてのフラグを立てた `continuo abandon <URL> <ディレクトリ>`。
// 成功条件: 終了コードが差し替えた戻り値のまま、Options の6つの値が渡した通りであること。
func TestRunAbandon_フラグを取り違えずに渡す(t *testing.T) {
	dir := writeWorkflowFor(t)
	var got abandon.Options
	deps := cli.Deps{AbandonRun: func(_ context.Context, opts abandon.Options) int {
		got = opts
		return 1
	}}

	url := "https://github.com/octocat/hello-world/issues/42"
	code, _, stderr := runCLIWith(deps,
		[]string{"abandon", "--dry-run", "--force", "--to", "Ice Box", "--park", "Blocked", url, dir}, "")

	if code != 1 {
		t.Fatalf("差し替えた戻り値がそのまま返っていない: %d（stderr: %s）", code, stderr)
	}
	if got.IssueURL != url {
		t.Errorf("issue の URL が %q ではなく %q で渡っている", url, got.IssueURL)
	}
	if want := filepath.Join(dir, "WORKFLOW.md"); got.ConfigPath != want {
		t.Errorf("設定ファイルのパスが %q ではなく %q で渡っている", want, got.ConfigPath)
	}
	if !got.DryRun {
		t.Error("--dry-run が DryRun へ渡っていない")
	}
	if !got.Force {
		t.Error("--force が Force へ渡っていない")
	}
	if got.ToState != "Ice Box" {
		t.Errorf("--to が ToState へ %q ではなく %q で渡っている", "Ice Box", got.ToState)
	}
	if got.ParkState != "Blocked" {
		t.Errorf("--park が ParkState へ %q ではなく %q で渡っている", "Blocked", got.ParkState)
	}
}

// TestRunAbandon_フラグを立てなければ偽と空で渡る は、既定値の結線を確かめる。
//
// **立てていないフラグが真で渡ると、`--force` を付けていないのに失うものごと消す。**
//
// 目的: フラグを1つも書かないとき、DryRun と Force が偽、ToState と ParkState が空で渡ること。
// 与える情報: `continuo abandon <URL> <ディレクトリ>` だけ。
// 成功条件: 4つとも既定値のまま渡ること。
func TestRunAbandon_フラグを立てなければ偽と空で渡る(t *testing.T) {
	dir := writeWorkflowFor(t)
	var got abandon.Options
	deps := cli.Deps{AbandonRun: func(_ context.Context, opts abandon.Options) int {
		got = opts
		return 0
	}}

	code, _, stderr := runCLIWith(deps,
		[]string{"abandon", "https://github.com/octocat/hello-world/issues/42", dir}, "")

	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if got.DryRun || got.Force {
		t.Errorf("立てていないフラグが真で渡っている（DryRun=%v / Force=%v）", got.DryRun, got.Force)
	}
	if got.ToState != "" || got.ParkState != "" {
		t.Errorf("指定していない値が空で渡っていない（ToState=%q / ParkState=%q）", got.ToState, got.ParkState)
	}
}

// TestRunAbandon_引数の誤りは本体を呼ばずに2で止まる は、消す処理へ進ませないことを確かめる。
//
// **引数を取り違えたまま進むと、消す相手を間違える。**位置引数のあとのフラグは
// Go の flag では黙って無視されるので、`--dry-run` のつもりで本当に消すことになる。
//
// 目的: issue の URL が無い・位置引数が3つ以上・位置引数のあとにフラグを書いた場合に、
// 終了コード 2 で止まり、abandon の本体を1度も呼ばないこと。
// 与える情報: 誤った並びの3通り。
// 成功条件: すべて終了コードが 2、本体の呼び出しが0回、stderr に理由が出ていること。
func TestRunAbandon_引数の誤りは本体を呼ばずに2で止まる(t *testing.T) {
	url := "https://github.com/octocat/hello-world/issues/42"
	cases := map[string][]string{
		"URLが無い":      {"abandon"},
		"位置引数が3つ":     {"abandon", url, "a", "b"},
		"位置引数のあとのフラグ": {"abandon", url, "--dry-run"},
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

// TestRunAbandon_helpは0で返して本体を呼ばない は、使い方の表示を確かめる。
//
// 目的: `continuo abandon --help` が 0 で返り、消す処理へ進まないこと。
// 与える情報: `abandon --help`。
// 成功条件: 終了コードが 0、本体の呼び出しが0回、使い方が stderr に出ていること。
func TestRunAbandon_helpは0で返して本体を呼ばない(t *testing.T) {
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
