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
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/scaffold"
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

// TestRunAbandon_フラグは位置引数の前でも後ろでも同じに効く は、フラグの置き場所を問わないことを確かめる。
//
// **`git` も `docker` も `gh` もフラグを後ろに書ける。**利用者はそちらに慣れているので、
// `continuo abandon <URL> --dry-run` を弾かない。**弾いていた版では、この道具を作った本人が
// 実際に間違えた。**
//
// 目的: `--dry-run` を issue の URL の前に書いても後ろに書いても、同じ値が渡ること。
// 与える情報: フラグが前の並びと、フラグが後ろの並び。
// 成功条件: どちらも DryRun が真で、issue の URL が位置引数として渡ること。
func TestRunAbandon_フラグは位置引数の前でも後ろでも同じに効く(t *testing.T) {
	url := "https://github.com/octocat/hello-world/issues/42"
	cases := map[string][]string{
		"フラグが前":  {"abandon", "--dry-run", url},
		"フラグが後ろ": {"abandon", url, "--dry-run"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var got abandon.Options
			deps := cli.Deps{AbandonRun: func(_ context.Context, opts abandon.Options) int {
				got = opts
				return 0
			}}

			code, _, stderr := runCLIWith(deps, args, "")

			if code != 0 {
				t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
			}
			if !got.DryRun {
				t.Error("--dry-run が DryRun へ渡っていない")
			}
			if got.IssueURL != url {
				t.Errorf("issue の URL が %q ではなく %q で渡っている", url, got.IssueURL)
			}
		})
	}
}

// TestRunAbandon_後ろに書いた値を取るフラグは次の引数を巻き込まない は、並べ替えの要点を確かめる。
//
// **`--to "Ice Box"` の `Ice Box` はフラグの値であって位置引数ではない。**
// 取り違えると、位置引数が3つあるとして落ちるか、WORKFLOW.md の場所として扱われる。
//
// 目的: `abandon <URL> <ディレクトリ> --to "Ice Box"` で、ToState と設定ファイルの場所が
// どちらも正しく渡ること。
// 与える情報: 値を取るフラグを末尾に書いた並び。
// 成功条件: ToState が "Ice Box"、ConfigPath が渡したディレクトリの WORKFLOW.md であること。
func TestRunAbandon_後ろに書いた値を取るフラグは次の引数を巻き込まない(t *testing.T) {
	dir := writeWorkflowFor(t)
	url := "https://github.com/octocat/hello-world/issues/42"
	var got abandon.Options
	deps := cli.Deps{AbandonRun: func(_ context.Context, opts abandon.Options) int {
		got = opts
		return 0
	}}

	code, _, stderr := runCLIWith(deps, []string{"abandon", url, dir, "--to", "Ice Box"}, "")

	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if got.ToState != "Ice Box" {
		t.Errorf("--to が ToState へ %q ではなく %q で渡っている", "Ice Box", got.ToState)
	}
	if want := filepath.Join(dir, "WORKFLOW.md"); got.ConfigPath != want {
		t.Errorf("設定ファイルのパスが %q ではなく %q で渡っている", want, got.ConfigPath)
	}
	if got.IssueURL != url {
		t.Errorf("issue の URL が %q ではなく %q で渡っている", url, got.IssueURL)
	}
}

// TestRunAbandon_二重ダッシュのあとは位置引数として扱う は、`--` の作法を確かめる。
//
// **`--` より後ろは、`-` で始まっていてもフラグではない。**この作法が無いと、
// `-` で始まる文字列を位置引数として渡す手段が消える。
//
// 目的: `abandon -- --dry-run` の `--dry-run` が issue の URL として渡り、
// DryRun が立たないこと。
// 与える情報: `--` のあとにフラグらしき文字列を1つ置いた並び。
// 成功条件: IssueURL が "--dry-run"、DryRun が偽であること。
func TestRunAbandon_二重ダッシュのあとは位置引数として扱う(t *testing.T) {
	var got abandon.Options
	deps := cli.Deps{AbandonRun: func(_ context.Context, opts abandon.Options) int {
		got = opts
		return 0
	}}

	code, _, stderr := runCLIWith(deps, []string{"abandon", "--", "--dry-run"}, "")

	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if got.IssueURL != "--dry-run" {
		t.Errorf("`--` のあとが位置引数になっていない: IssueURL=%q", got.IssueURL)
	}
	if got.DryRun {
		t.Error("`--` のあとの文字列がフラグとして解釈されている")
	}
}

// TestRun_知らないフラグは後ろに書いてもエラーのまま は、打ち間違いを通さないことを確かめる。
//
// **後ろのフラグを受け付けるようにしても、知らないフラグまで通してはならない。**
// `--dryrun` のような打ち間違いを黙って位置引数として飲み込むと、
// `--dry-run` のつもりで本当に消すことになる。
//
// 目的: 位置引数のあとに知らないフラグを書いたら 2 で止まること。
// 与える情報: `init <ディレクトリ> --dryrun` と `abandon <URL> --dryrun`。
// 成功条件: どちらも終了コードが 2 で、stderr に理由が出ていること。
func TestRun_知らないフラグは後ろに書いてもエラーのまま(t *testing.T) {
	url := "https://github.com/octocat/hello-world/issues/42"
	cases := map[string][]string{
		"init":    {"init", t.TempDir(), "--dryrun"},
		"abandon": {"abandon", url, "--dryrun"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			deps := cli.Deps{
				ScaffoldDetect: fixedDetection,
				AbandonRun: func(_ context.Context, _ abandon.Options) int {
					t.Error("知らないフラグなのに本体を呼んでいる")
					return 0
				},
			}

			code, _, stderr := runCLIWith(deps, args, "")

			if code != 2 {
				t.Errorf("終了コードが 2 でない: %d（stderr: %s）", code, stderr)
			}
			if stderr == "" {
				t.Error("何が誤りかを stderr へ出していない")
			}
		})
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
// 与える情報: owner とカンバンの番号を埋めた WORKFLOW.md。
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

// fixedDetection は `gh` を叩かずに owner とカンバンの番号を返す。
//
// **`continuo setup` は本物の `gh` からカンバンの一覧を引く。**検査で差し替えないと、
// 実行した人のアカウントにあるカンバンの数で結果が変わる（2026-08-21 に実際に起きた）。
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

// tempCLIHome は、ホームディレクトリの代わりに使う一時ディレクトリを作り、
// `HOME` をそこへ向ける。
//
// **本物の `~/.continuo` を触らせないためである。**`--id` の解決は
// ホームディレクトリを起点にする。
//
// t: 呼び出し元のテスト。
func tempCLIHome(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("", "cc")
	if err != nil {
		t.Fatalf("一時ディレクトリを作れません: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if resolved, rerr := filepath.EvalSymlinks(dir); rerr == nil {
		dir = resolved
	}
	t.Setenv("HOME", dir)
}

// TestRunMain_idの名前が使えなければ常駐を始めない は、フラグを読んだ直後の検査を確かめる。
//
// 目的: 設計 3-17b。**この文字列はロックファイルのパスに入る。**
// **あとで検査すると、検査より先に `~/.continuo` の外を指すパスが組み上がる。**
// 与える情報: 大文字・`..`・空白・33文字の名前。
// 成功条件: 終了コードが 2 で、daemon を1回も呼ばないこと。
func TestRunMain_idの名前が使えなければ常駐を始めない(t *testing.T) {
	tempCLIHome(t)

	var called bool
	deps := cli.Deps{
		DaemonRun: func(_ context.Context, _ daemon.Options) error { called = true; return nil },
	}

	for _, id := range []string{"E2E", "..", "../../etc", "my id", strings.Repeat("a", 33)} {
		t.Run(id, func(t *testing.T) {
			code, _, stderr := runCLIWith(deps, []string{"--id", id, writeWorkflowFor(t)}, "")
			if code != 2 {
				t.Errorf("--id %q の終了コードが 2 でない: %d（stderr: %s）", id, code, stderr)
			}
			if !strings.Contains(stderr, "--id") {
				t.Errorf("--id が悪いことを言っていない: %s", stderr)
			}
		})
	}
	if called {
		t.Error("使えない名前なのに常駐を始めている")
	}
}

// TestRunMain_idをそのまま常駐へ渡す は、フラグの受け渡しを確かめる。
//
// 目的: 設計 3-17b。`--id` は常駐の側でロックの置き場所へ展開される。
// **CLI で握り潰すと、名前を付けたのにロックが分かれない。**
// 与える情報: `--id e2e`。
// 成功条件: daemon.Options.Instance に `e2e` で解決した置き場所が渡り、
// **そのロックが `--id` を付けない場合と違う場所を指すこと。**
func TestRunMain_idをそのまま常駐へ渡す(t *testing.T) {
	tempCLIHome(t)

	var got, gotLock string
	deps := cli.Deps{
		DaemonRun: func(_ context.Context, opts daemon.Options) error {
			if opts.Instance == nil {
				return nil
			}
			got = opts.Instance.ID()
			gotLock = opts.Instance.LockPath()
			return nil
		},
	}

	code, _, stderr := runCLIWith(deps, []string{"--id", "e2e", writeWorkflowFor(t)}, "")
	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if got != "e2e" {
		t.Errorf("--id が常駐へ渡っていない: got %q, want %q", got, "e2e")
	}

	var defaultLock string
	deps.DaemonRun = func(_ context.Context, opts daemon.Options) error {
		if opts.Instance != nil {
			defaultLock = opts.Instance.LockPath()
		}
		return nil
	}
	if code, _, stderr := runCLIWith(deps, []string{writeWorkflowFor(t)}, ""); code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if gotLock == defaultLock {
		t.Errorf("--id を付けたのに既定と同じロックを指している: %q", gotLock)
	}
}

// TestRunMain_ホームを引けない失敗をidのせいにしない は、文言の切り分けを固定する。
//
// 目的: 設計 3-17b。`instance.Resolve` は名前を先に検査し、そのあとで
// ホームディレクトリを引く。**名前の検査を通ったあとの失敗を
// 「--id に渡した名前が使えません」と報告してはならない。**
// **`--id` を1文字も渡していない人にも、その文言が出る。**
// 与える情報: `HOME` が空の環境と、`--id` を渡さない起動。
// 成功条件: 起動できず、**stderr に `--id` が出ないこと。**
func TestRunMain_ホームを引けない失敗をidのせいにしない(t *testing.T) {
	t.Setenv("HOME", "")

	code, _, stderr := runCLIWith(cli.Deps{}, []string{writeWorkflowFor(t)}, "")
	if code == 0 {
		t.Fatalf("ホームディレクトリを引けないのに起動できてしまった（stderr: %s）", stderr)
	}
	if strings.Contains(stderr, "--id") {
		t.Fatalf("--id を渡していないのに --id のせいにしている: %s", stderr)
	}
}

// TestRunAbandon_idをそのまま片付けへ渡す は、フラグの受け渡しを確かめる。
//
// 目的: 設計 3-17b。**常駐している側と同じ名前を渡さないと、abandon は
// 空いている既定のロックを見て「動いていない」と判定し、生きた worktree を消す。**
// 与える情報: `--id e2e` と issue の URL。
// 成功条件: abandon.Options.Instance に `e2e` で解決した置き場所が渡ること。
func TestRunAbandon_idをそのまま片付けへ渡す(t *testing.T) {
	tempCLIHome(t)

	var got string
	deps := cli.Deps{AbandonRun: func(_ context.Context, opts abandon.Options) int {
		if opts.Instance != nil {
			got = opts.Instance.ID()
		}
		return 0
	}}

	dir := writeWorkflowFor(t)
	code, _, stderr := runCLIWith(deps,
		[]string{"abandon", "https://github.com/octocat/hello-world/issues/42", dir, "--id", "e2e"}, "")
	if code != 0 {
		t.Fatalf("終了コードが 0 でない: %d（stderr: %s）", code, stderr)
	}
	if got != "e2e" {
		t.Errorf("--id が片付けへ渡っていない: got %q, want %q", got, "e2e")
	}
}
