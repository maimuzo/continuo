// continuo は GitHub Projects v2 のボードを見張り、issue ごとに git worktree を用意して
// herdr の pane で Claude Code を起動し、完了までを面倒見る常駐プロセスである。
// 設計の正は docs/plans/continuo_design.md にある。
// Package cli は continuo の CLI の実体である。
//
// **`cmd/continuo` に置かない。**`package main` の関数は `test/` から呼べないので、
// 実体をそこに置くと引数の受け取り方も終了コードも検査できない
// （2026-08-21 に、この理由で `cmd/continuo` のカバレッジが 27.9% まで落ちていた。設計 6-4）。
// **`cmd/continuo` は `cli.Run` を呼んで `os.Exit` するだけにする。**
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/maimuzo/continuo/internal/abandon"
	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/daemon"
	"github.com/maimuzo/continuo/internal/doctor"
	"github.com/maimuzo/continuo/internal/hookclient"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/logging"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/scaffold"
	"github.com/maimuzo/continuo/internal/setup"
	"github.com/maimuzo/continuo/internal/trust"
	"github.com/maimuzo/continuo/internal/workspace"
)

// version は、この実行ファイルがどの版から作られたかを表す。
//
// **ビルドのときに `-ldflags "-X …/internal/cli.version=v1.2.3"` で埋める。**
// 手元で `go build` しただけなら `dev` のままである。
//
// **左辺は変数の完全な位置でなければならない。**`-X main.version=…` と書いた版では、
// Go が何も言わないまま値が入らず、**入ったものが何版かを誰も確かめられなかった。**
var version = "dev"

// runVersion は `continuo version` サブコマンドである。
//
// **不具合の報告を受けたときに、どの配布物が動いているかを切り分けるために要る。**
// インストーラーが版を指定して取ってくる以上、入ったものが名乗れなければ意味がない。
//
// stdout: 出力先。
// 戻り値: 終了コード。常に 0。
func runVersion(stdout io.Writer) int {
	fmt.Fprintln(stdout, version)
	return 0
}

// Deps は CLI が外部へ繋ぐ処理をまとめたものである。
//
// **これを引数で渡せるようにしてあるのが、この package の作りの要点である。**
// GitHub・herdr・Keychain・ホームディレクトリへ実際に繋ぐ処理をここに集め、
// 検査ではそれぞれを偽物へ向ける。**この形にする前は `package main` に実体があり、
// 引数の受け取り方も終了コードも検査できなかった**（設計 6-4）。
//
// **ゼロ値のフィールドは既定（本物）に差し替わる**（withDefaults）。
// 検査は差し替えたいものだけを埋めればよい。
type Deps struct {
	// DoctorRun は前提の検査である。
	DoctorRun func(ctx context.Context, opts doctor.Options) doctor.Report
	// UserHomeDir はホームディレクトリを引く。`~/.claude.json` を書き換える先が決まるので、
	// **検査では必ず一時ディレクトリへ向ける。**
	UserHomeDir func() (string, error)
	// DaemonRun は常駐の本体である。
	DaemonRun func(ctx context.Context, opts daemon.Options) error
	// SetupFetchStatusField はボードの Status のフィールドを読む。
	SetupFetchStatusField func(ctx context.Context, opts setup.FetchOptions) (setup.StatusField, error)
	// TrustPlan は信頼登録の対象を調べる（`git` と `~/.claude.json` を読む）。
	TrustPlan func(ctx context.Context, opts trust.Options) (*trust.Report, error)
	// TrustApply は `~/.claude.json` を書き換える。**検査では必ず差し替える。**
	TrustApply func(ctx context.Context, opts trust.Options, report *trust.Report) (*trust.ApplyResult, error)
	// ProbeKeychain は macOS の Keychain を読めるかを確かめる。
	ProbeKeychain func(ctx context.Context, timeout time.Duration) (ratelimit.KeychainProbe, error)
	// ScaffoldDetect は owner とボードの番号を `gh` から引く。
	ScaffoldDetect func(ctx context.Context, opts scaffold.DetectOptions) scaffold.Detection
	// AbandonRun は着手した issue を着手する前の状態へ戻す。
	// **worktree と branch と pane を消すので、検査では必ず差し替える。**
	AbandonRun func(ctx context.Context, opts abandon.Options) int
	// GOOS は動いている OS である。`continuo allow-keychain-access` は macOS 専用なので、
	// **macOS 以外での応答を検査するために差し替えられるようにしてある。**空なら runtime.GOOS。
	GOOS string
}

// withDefaults は埋まっていないフィールドを本物で埋める。
//
// 戻り値: すべてのフィールドが埋まった Deps。
func (d Deps) withDefaults() Deps {
	if d.DoctorRun == nil {
		d.DoctorRun = doctor.Run
	}
	if d.UserHomeDir == nil {
		d.UserHomeDir = os.UserHomeDir
	}
	if d.DaemonRun == nil {
		d.DaemonRun = daemon.Run
	}
	if d.SetupFetchStatusField == nil {
		d.SetupFetchStatusField = setup.FetchStatusField
	}
	if d.TrustPlan == nil {
		d.TrustPlan = trust.Plan
	}
	if d.TrustApply == nil {
		d.TrustApply = trust.Apply
	}
	if d.ProbeKeychain == nil {
		d.ProbeKeychain = ratelimit.ProbeKeychain
	}
	if d.ScaffoldDetect == nil {
		d.ScaffoldDetect = scaffold.Detect
	}
	if d.AbandonRun == nil {
		d.AbandonRun = abandon.Run
	}
	if d.GOOS == "" {
		d.GOOS = runtime.GOOS
	}
	return d
}

// Run は continuo の CLI 全体のエントリポイントである。os.Exit を直接呼ばないので
// テストから終了コードを検証できる。
//
// args: os.Args[1:] に相当するコマンドライン引数。
// stdin: 標準入力。`continuo hook` が hook の JSON を読む先である。
// stdout / stderr: 出力先。テストでは bytes.Buffer を渡して出力内容を検証できる。
// 戻り値: プロセスの終了コード（0 は正常終了）。
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return RunWith(Deps{}, args, stdin, stdout, stderr)
}

// RunWith は外部へ繋ぐ処理を差し替えて CLI を動かす。
//
// **検査はこちらを呼ぶ。**埋めなかったフィールドは本物に差し替わるので、
// 差し替えたいものだけを渡せばよい。
//
// deps: 外部へ繋ぐ処理。ゼロ値なら全部が本物になる。
// args: os.Args[1:] に相当するコマンドライン引数。
// stdin: 標準入力。
// stdout / stderr: 出力先。
// 戻り値: プロセスの終了コード。
func RunWith(deps Deps, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	d := deps.withDefaults()
	// **まず環境変数 LANG で決める**（設計 3-35）。引数の誤りのように、設定ファイルを
	// 読むより前に出す文言があるためである。設定を読めたら useLanguageFromConfig が
	// 決め直す（**設定が主、環境変数が従**）。
	i18n.Use(i18n.FromEnv(os.Getenv))

	if len(args) > 0 {
		switch args[0] {
		case "hook":
			return runHook(args[1:], stdin, stderr)
		case "init":
			return runInit(d, args[1:], stdout, stderr)
		case "setup":
			return runSetup(d, args[1:], stdin, stdout, stderr)
		case "doctor":
			return runDoctor(d, args[1:], stdout, stderr)
		case "trust":
			return runTrust(d, args[1:], stdout, stderr)
		case "abandon":
			return runAbandon(d, args[1:], stdout, stderr)
		case "allow-keychain-access":
			return runAllowKeychainAccess(d, args[1:], stdout, stderr)
		case "version":
			return runVersion(stdout)
		}
	}
	return runMain(d, args, stdout, stderr)
}

// runInit は `continuo init` サブコマンドである。WORKFLOW.md の雛形を1つだけ置く（設計 3-32）。
//
// **利用者に手で埋めさせない。**tracker.provider.owner と tracker.provider.project_number は
// gh から引いて雛形に書き込む。引けなかったときはプレースホルダのまま残し、
// 何を埋めればよいかを出す。**引けなくても失敗させない**（雛形そのものは書けるため）。
//
// 雛形を書き出す実体と gh から引く実体は internal/scaffold にある。ここが決めるのは、
// 引数の受け取り方と、出力の文言・終了コードだけである。
//
// args: `continuo init` に続く引数。位置引数は書き出す先のディレクトリを0個か1個。
// 省略したら、いまいるディレクトリに書く。--force で既存の WORKFLOW.md を上書きする。
// --owner / --project を渡すと、その値を使って gh を叩かない。
// stdout / stderr: 出力先。
// 戻り値: 終了コード。0 は書き出せた（--help / -h で使い方を出した場合も 0）、
// 1 は書き出せなかった（既にある・ディレクトリが無いなど）、2 は引数の指定が誤っている。
func runInit(d Deps, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("continuo init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	forceFlag := fs.Bool("force", false, i18n.T(i18n.KeyCLIInitFlagForce))
	ownerFlag := fs.String("owner", "", i18n.T(i18n.KeyCLIInitFlagOwner))
	projectFlag := fs.Int("project", 0, i18n.T(i18n.KeyCLIInitFlagProject))
	if err := fs.Parse(args); err != nil {
		return parseErrorExitCode(err)
	}

	// gh から引いた値も同じ規則で弾く（internal/scaffold）。ここで弾くのは打ち間違いを
	// その場で知らせるためで、雛形へ書く前に止めれば YAML を壊した状態で残らない。
	if *ownerFlag != "" && !scaffold.ValidOwner(*ownerFlag) {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIInitErrOwnerInvalid, *ownerFlag))
		return 2
	}
	projectGiven := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "project" {
			projectGiven = true
		}
	})
	if projectGiven && *projectFlag <= 0 {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIInitErrProjectPositive, *projectFlag))
		return 2
	}

	// runMain と同じ理由で、位置引数のあとに書かれたフラグを黙って無視しない。
	// flag パッケージは最初の位置引数で解釈をやめるため、--force が効かないまま
	// 「既にあります」で止まる、という分かりにくい失敗になる。
	positional := fs.Args()
	for _, a := range positional {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIErrFlagAfterPositional, a))
			return 2
		}
	}
	if len(positional) > 1 {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIInitErrTooManyPositional, len(positional), positional))
		return 2
	}

	var dir string
	if len(positional) == 1 {
		dir = positional[0]
	}

	// gh を叩くのは、--owner / --project で渡されなかったぶんだけである（設計 3-32）。
	// 両方が渡されていれば Detect は1回も gh を起動しない。
	detection := d.ScaffoldDetect(context.Background(), scaffold.DetectOptions{
		Owner:         *ownerFlag,
		ProjectNumber: *projectFlag,
	})

	result, err := scaffold.WriteTemplateWithValues(dir, *forceFlag, detection.Values)
	switch {
	case err == nil:
		if result.Overwritten {
			fmt.Fprintln(stdout, i18n.T(i18n.KeyCLIInitOverwritten, result.Path))
		} else {
			fmt.Fprintln(stdout, i18n.T(i18n.KeyCLIInitCreated, result.Path))
		}
		printDetection(stdout, detection)
		return 0
	case errors.Is(err, scaffold.ErrAlreadyExists):
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIInitErrAlreadyExists, result.Path))
		return 1
	case errors.Is(err, scaffold.ErrDirNotFound):
		// ディレクトリは作らない（--force でも作らない）。打ち間違えたパスに
		// WORKFLOW.md が生まれると、利用者は作ったはずのファイルを見失う。
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIInitErrDirNotFound, err))
		return 1
	case errors.Is(err, scaffold.ErrNotADirectory):
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIInitErrNotADirectory, err))
		return 1
	case errors.Is(err, scaffold.ErrSymlink):
		// symlink は --force でも辿らない。辿ると指定されたディレクトリの外にある
		// リンク先を雛形で潰すため、--force を勧めてはならない。
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIInitErrSymlink, err))
		return 1
	default:
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIInitErrWriteFailed, err))
		return 1
	}
}

// runSetup は `continuo setup` サブコマンドである（設計 3-32 / RUCM
// docs/spec/usecases/particular_case/既存のボードの Status を割り当てる.rucm.md）。
//
// **既にある WORKFLOW.md の Status の割り当てだけを書き換える。**ボードの Status の選択肢を
// continuo の5つの役割へ割り当て、`scaffold.StatusKeyNames` が返す7つのキーの行を差し替える。
// **他の行には触れない。**利用者が `continuo init` のあとに手で直した行
// （`workspace.root`、`trust.repositories` から消した行など）を消さないためである。
//
// **WORKFLOW.md が無ければ止める。**雛形を置くのは `continuo init` の仕事であり、
// 2つのコマンドが同じファイルを作れると、どちらが正かが決まらない。
//
// **`--force` は無い。**書き換えるのが7行だけになったので、上書きから守るものが無くなった。
// 何も守らないフラグを残すと、まだ何かを守っているように読める。
//
// **標準入力を握るのはこのサブコマンドだけである。**`continuo init` を対話にしないのは、
// 設定を作り直す自動化の経路を止めないためである。
//
// **ボードは読むだけである。**選択肢が足りなければ、GitHub の画面から足すよう案内して打ち切る。
// **API で足させない**（`updateProjectV2Field` は選択肢の指定を全件の置き換えとして扱うので、
// 設定済みの Status が全部消える）。
//
// **検証はファイルが先、ボードがあとである。**どうせ止まる実行で、先に gh を叩いて
// レートリミットを使う理由が無い。
//
// args: `continuo setup` に続く引数。位置引数は WORKFLOW.md があるディレクトリを0個か1個。
// --owner / --project は gh を叩かずにその値を使う（**どのボードを読むかの指定であり、
// WORKFLOW.md には書かない**）。
// --status-field は Status を読み書きする single-select フィールドの名前を渡す。
// stdin: 番号を読む先。
// stdout / stderr: 出力先。対話は stdout へ出す。
// 戻り値: 終了コード。0 は書き換えられた（--help / -h も 0）、
// 1 は書き換えずに終わった（WORKFLOW.md が無い・ボードを読めない・選択肢が足りない・中断した）、
// 2 は引数の指定が誤っている。
func runSetup(d Deps, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("continuo setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	ownerFlag := fs.String("owner", "", i18n.T(i18n.KeyCLISetupFlagOwner))
	projectFlag := fs.Int("project", 0, i18n.T(i18n.KeyCLISetupFlagProject))
	statusFieldFlag := fs.String("status-field", setup.DefaultStatusFieldName, i18n.T(i18n.KeyCLISetupFlagStatusField))
	if err := fs.Parse(args); err != nil {
		return parseErrorExitCode(err)
	}

	if *ownerFlag != "" && !scaffold.ValidOwner(*ownerFlag) {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLISetupErrOwnerInvalid, *ownerFlag))
		return 2
	}
	projectGiven := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "project" {
			projectGiven = true
		}
	})
	if projectGiven && *projectFlag <= 0 {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLISetupErrProjectPositive, *projectFlag))
		return 2
	}
	if strings.TrimSpace(*statusFieldFlag) == "" {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLISetupErrStatusFieldEmpty))
		return 2
	}

	// runInit と同じ理由で、位置引数のあとに書かれたフラグを黙って無視しない。
	positional := fs.Args()
	for _, a := range positional {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIErrFlagAfterPositional, a))
			return 2
		}
	}
	if len(positional) > 1 {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLISetupErrTooManyPositional, len(positional), positional))
		return 2
	}

	var dir string
	if len(positional) == 1 {
		dir = positional[0]
	}

	// **まず書き換える WORKFLOW.md があるかを確かめる**（RUCM の基本フロー2）。
	// ここで止まる実行では、役割の割り当てを1つも尋ねない。
	if check, err := scaffold.CheckUpdatable(dir); err != nil {
		return printScaffoldError(stderr, check, err)
	}

	// **owner とボードの番号は `continuo init` と同じ経路で引く**（internal/scaffold）。
	// 同じ検出を2箇所に持たない。**引いた値は WORKFLOW.md へ書かない。**
	// どのボードの Status の選択肢を読むかを決めるためだけに使う。
	detection := d.ScaffoldDetect(context.Background(), scaffold.DetectOptions{
		Owner:         *ownerFlag,
		ProjectNumber: *projectFlag,
	})
	if code := checkDetectionForSetup(stderr, detection); code != 0 {
		return code
	}

	field, err := d.SetupFetchStatusField(context.Background(), setup.FetchOptions{
		Owner:         detection.Values.Owner,
		ProjectNumber: detection.Values.ProjectNumber,
		FieldName:     *statusFieldFlag,
	})
	if err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLISetupBoardErr, err))
		switch {
		case errors.Is(err, setup.ErrScopeMissing):
			fmt.Fprintln(stderr, i18n.T(i18n.KeyCLISetupBoardRemedyScope))
		case errors.Is(err, setup.ErrStatusFieldNotFound):
			fmt.Fprintln(stderr, i18n.T(i18n.KeyCLISetupBoardRemedyStatusField))
		case errors.Is(err, setup.ErrRateLimited):
			fmt.Fprintln(stderr, i18n.T(i18n.KeyCLISetupBoardRemedyRateLimited))
		default:
			fmt.Fprintln(stderr, i18n.T(i18n.KeyCLISetupBoardRemedyGeneric))
		}
		return 1
	}

	// **Ctrl+C を「割り当てを保存せずに終わる」に繋ぐ。**既定の動作のままだと、
	// 中断したことを利用者に伝えないままプロセスが消える。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	assignment, err := setup.Assign(ctx, setup.AssignOptions{
		FieldName: field.Name,
		Options:   field.Options,
		In:        stdin,
		Out:       stdout,
	})
	if err != nil {
		// **なぜ止まったかは setup.Assign が stdout へ出し終えている。**
		// ここで同じことを繰り返さない（同じ理由が2回並ぶと、2つ起きたように読める）。
		return 1
	}

	// **書き換えるのは Status の7行だけである。**owner / project_number / trust.repositories は
	// `continuo init` が書いた値のまま残す。**Detect が引き直した値で上書きしない。**
	result, err := scaffold.UpdateStatuses(dir, assignment.Statuses())
	if err != nil {
		return printScaffoldError(stderr, result, err)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, i18n.T(i18n.KeyCLISetupUpdated, result.Path))
	fmt.Fprintln(stdout, i18n.T(i18n.KeyCLISetupUpdatedKeysNote))
	for _, k := range scaffold.StatusKeyNames() {
		fmt.Fprintln(stdout, i18n.T(i18n.KeyCLISetupUpdatedKey, k))
	}
	return 0
}

// checkDetectionForSetup は、対話に入れるだけの情報がボードから引けたかを確かめる。
//
// **`continuo setup` は owner とボードの番号が両方決まらないと1歩も進めない**
// （`continuo init` は決まらなくても雛形を書けるので、ここだけ扱いが違う）。
//
// w: 出力先。
// d: scaffold.Detect が返した結果。
// 戻り値: 進んでよければ 0、進めなければ終了コード 1。
func checkDetectionForSetup(w io.Writer, d scaffold.Detection) int {
	if d.Values.Owner == "" {
		fmt.Fprintln(w, i18n.T(i18n.KeyCLISetupBoardErrOwner, fieldReason(d, scaffold.OwnerKey)))
		fmt.Fprintln(w, i18n.T(i18n.KeyCLISetupBoardRemedyOwner))
		return 1
	}
	if d.Values.ProjectNumber <= 0 {
		fmt.Fprintln(w, i18n.T(i18n.KeyCLISetupBoardErrProject, fieldReason(d, scaffold.ProjectKey)))
		for _, c := range candidatesOf(d, scaffold.ProjectKey) {
			fmt.Fprintln(w, i18n.T(i18n.KeyCLISetupBoardCandidate, c.Owner, c.Number, c.Title, c.URL))
		}
		fmt.Fprintln(w, i18n.T(i18n.KeyCLISetupBoardRemedyProject))
		return 1
	}
	return 0
}

// fieldReason は、あるキーを決められなかった理由の1行を取り出す。
//
// d: scaffold.Detect が返した結果。
// key: scaffold.OwnerKey などのキーのパス。
// 戻り値: 理由の1行。該当するキーが無ければ空文字。
func fieldReason(d scaffold.Detection, key string) string {
	for _, f := range d.Fields {
		if f.Key == key {
			return f.Reason
		}
	}
	return ""
}

// candidatesOf は、あるキーについて並んだボードの候補を取り出す。
//
// d: scaffold.Detect が返した結果。
// key: scaffold.ProjectKey などのキーのパス。
// 戻り値: 候補の一覧。無ければ nil。
func candidatesOf(d scaffold.Detection, key string) []scaffold.Project {
	for _, f := range d.Fields {
		if f.Key == key {
			return f.Candidates
		}
	}
	return nil
}

// printScaffoldError は WORKFLOW.md を書き換えられない理由を出し、終了コードを決める。
//
// **`continuo setup` 専用の文言を使う。**`continuo init` の文言をそのまま出すと、
// 叩いていないコマンドの名前が案内に出る。
//
// w: 出力先。
// result: scaffold が返した結果（パスだけ埋まっていることがある）。
// err: scaffold が返した非 nil のエラー。
// 戻り値: 終了コード 1。
func printScaffoldError(w io.Writer, result scaffold.Result, err error) int {
	switch {
	case errors.Is(err, scaffold.ErrNotFound):
		// **雛形を作るのは `continuo init` の仕事である。**setup は作らずに案内して止まる。
		fmt.Fprintln(w, i18n.T(i18n.KeyCLISetupErrNotFound, pathOf(result, err)))
		fmt.Fprintln(w, i18n.T(i18n.KeyCLISetupErrNotFoundRemedy))
	case errors.Is(err, scaffold.ErrKeysNotFound):
		fmt.Fprintln(w, i18n.T(i18n.KeyCLISetupErrKeysNotFound, err))
	case errors.Is(err, scaffold.ErrDirNotFound):
		fmt.Fprintln(w, i18n.T(i18n.KeyCLISetupErrDirNotFound, err))
	case errors.Is(err, scaffold.ErrNotADirectory):
		fmt.Fprintln(w, i18n.T(i18n.KeyCLISetupErrNotADirectory, err))
	case errors.Is(err, scaffold.ErrSymlink):
		// symlink は辿らない。辿ると指定されたディレクトリの外にあるリンク先を書き換える。
		fmt.Fprintln(w, i18n.T(i18n.KeyCLISetupErrSymlink, err))
	default:
		fmt.Fprintln(w, i18n.T(i18n.KeyCLISetupErrWriteFailed, err))
	}
	return 1
}

// pathOf は「WORKFLOW.md がありません」の文言に出すパスを決める。
//
// result: scaffold が返した結果。
// err: scaffold が返したエラー（パスを含む文言を持つ）。
// 戻り値: Result.Path が埋まっていればそれ、無ければエラーの文言。
func pathOf(result scaffold.Result, err error) string {
	if result.Path != "" {
		return result.Path
	}
	return err.Error()
}

// useLanguageFromConfig は WORKFLOW.md を読んで、画面に出す文言の言語を決め直す（設計 3-35）。
//
// **設定が主、環境変数 LANG が従である。**run が起動直後に環境変数から決めた言語を、
// 設定に書かれた `language` で上書きする。
//
// **設定を読めなくても止めない。**読めないこと自体は、それぞれのサブコマンドが自分の
// 文言で報告する（`continuo doctor` は検査結果の1件目として出す）。ここで読むのは
// 言語を決めるためだけであり、読めなければ環境変数から決めた言語のまま進む。
//
// path: 読む WORKFLOW.md の絶対パス。
func useLanguageFromConfig(path string) {
	loaded, err := config.Load(path)
	if err != nil {
		return
	}
	useLanguage(loaded.Config)
}

// useLanguage は検証済みの設定から、画面に出す文言の言語を決める（設計 3-35）。
//
// **`config.Load` が対応していない言語を先に弾いている**ので、ここへ来る値は
// 空文字・`auto`・資源のある言語のいずれかである。i18n.Resolve はそれ以外の値に対しても
// 既定の言語（日本語）を返すので、決められないまま進むことはない。
//
// cfg: config.Load を通した設定。
func useLanguage(cfg config.Config) {
	lang, _ := i18n.Resolve(cfg.Language, os.Getenv)
	i18n.Use(lang)
}

// printDetection は、雛形の値をどう決めたかを人が読める形で出す（設計 3-32）。
//
// 記号は `continuo doctor` と同じものを使う。埋まったものは ✓、埋まらなかったものは !
// （雛形そのものは書けているので、✗ ではない）。
//
// w: 出力先。
// d: Detect が返した結果。
func printDetection(w io.Writer, d scaffold.Detection) {
	for _, f := range d.Fields {
		if f.Filled {
			fmt.Fprintln(w, i18n.T(i18n.KeyCLIInitDetectFilled, f.Key, f.Value, f.Reason))
		} else {
			fmt.Fprintln(w, i18n.T(i18n.KeyCLIInitDetectUnfilled, f.Key, f.Reason))
		}
		for _, c := range f.Candidates {
			fmt.Fprintln(w, i18n.T(i18n.KeyCLIInitDetectCandidate, c.Owner, c.Number, c.Title, c.URL))
		}
		// **埋まったときも案内を出す。**trust.repositories は埋めて終わりではなく、
		// **人間が要らない行を消して初めて意味を持つ**（設計 3-33）。
		// 埋まった場合に黙ると、その手順が誰にも伝わらない。
		for _, a := range f.Advice {
			fmt.Fprintln(w, i18n.T(i18n.KeyCLIInitDetectAdvice, a))
		}
	}
	if !d.AllFilled() {
		fmt.Fprintln(w, i18n.T(i18n.KeyCLIInitDetectPlaceholderNote))
	}
}

// trustInternalErrorExitCode は `continuo trust` が調査も登録も行えなかったときの終了コードである。
//
// **登録の対象が残っていること（1）と区別する。**スクリプトから「まだ承認していないものがある」と
// 「trust 自体が動けなかった」を言い分けられるようにする。
const trustInternalErrorExitCode = 3

// runTrust は `continuo trust` サブコマンドである（設計 3-33）。
//
// **`WORKFLOW.md` の `trust.repositories` に人間が列挙したリポジトリだけを対象に、
// `~/.claude.json` の `hasTrustDialogAccepted` を `true` にする。**
// **ボードから自動で集めない。**ボードは他人が編集できるので、そこから集めると
// issue を足せる人が信頼させるリポジトリを増やせてしまう。
//
// **`--dry-run` は信頼のダイアログの代わりである。**対象の `.claude/settings.json` の
// `permissions.allow` と `permissions.additionalDirectories`、`.mcp.json` の MCP サーバーを出す。
// **これが無いと、人間が中身を確かめる機会が消える。**登録するときも同じ一覧を先に出す。
//
// **人間に問い返さない。**対話するコマンドは `continuo setup` の1つに寄せてある。
//
// args: `continuo trust` に続く引数（--dry-run と、WORKFLOW.md のパスを0個か1個）。
// stdout / stderr: 出力先。要求内容と結果は stdout へ出す。
// 戻り値: 終了コード。0 は「登録した」または「登録するものが無かった」、
// **1 は登録の対象が残っている**（--dry-run のとき、または調べられなかったものがあるとき）、
// 2 は引数の指定が誤っている（--help / -h なら 0）、
// 3 は設定を読めない・`~/.claude.json` を読み書きできない。
func runTrust(d Deps, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("continuo trust", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRunFlag := fs.Bool("dry-run", false, i18n.T(i18n.KeyCLITrustFlagDryRun))
	if err := fs.Parse(args); err != nil {
		return parseErrorExitCode(err)
	}

	// runMain と同じ理由で、位置引数のあとに書かれたフラグを黙って無視しない。
	// ここで見逃すと、--dry-run のつもりで本当に書き込むことになる。
	positional := fs.Args()
	for _, a := range positional {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIErrFlagAfterPositional, a))
			return 2
		}
	}
	if len(positional) > 1 {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLITrustErrTooManyPositional, len(positional), positional))
		return 2
	}

	var argPath string
	if len(positional) == 1 {
		argPath = positional[0]
	}

	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIErrGetwd, err))
		return trustInternalErrorExitCode
	}
	path, err := config.ResolvePath(argPath, workDir)
	if err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIErrResolveConfigPath, err))
		return trustInternalErrorExitCode
	}
	loaded, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIErrLoadConfig, path, err))
		return trustInternalErrorExitCode
	}
	// **設定を読めたので、ここから先は設定の言語で出す**（設計 3-35）。
	useLanguage(loaded.Config)

	// **ホームディレクトリは os.UserHomeDir で引く。**`~/.claude.json` を書き換えるので、
	// 引けないまま既定値へ落とさない（別の場所を書き換えることになる）。
	homeDir, err := d.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLITrustErrHomeDir, err))
		return trustInternalErrorExitCode
	}

	opts := trust.Options{Repositories: loaded.Config.Trust.Repositories, HomeDir: homeDir}
	// **`--dry-run` では clone を取りに行かない。**読むだけのつもりで叩いた人の
	// ディスクを無断で使わないため（設計 3-22 / 3-33）。
	if !*dryRunFlag {
		opts.FetchClone = workspace.RunGhqGet
		opts.OnFetch = func(repository string) {
			fmt.Fprintln(stdout, i18n.T(i18n.KeyCLITrustFetchingClone, repository))
		}
		opts.OnFetched = func(clonePath string) {
			fmt.Fprintln(stdout, i18n.T(i18n.KeyCLITrustFetchedClone, clonePath))
		}
	}
	report, err := d.TrustPlan(context.Background(), opts)
	if err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLITrustErrPlan, err))
		return trustInternalErrorExitCode
	}
	// **登録するときも、まず要求内容を出す。**`--dry-run` を叩かずに実行した人にも、
	// 何を許すことになったのかが同じ画面に残る。
	if err := trust.WriteRequirements(stdout, report); err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLITrustErrWriteRequirements, err))
		return trustInternalErrorExitCode
	}

	if *dryRunFlag {
		fmt.Fprintf(stdout, "\n%s\n", i18n.T(i18n.KeyCLITrustDryRunNote))
		if len(report.Pending()) > 0 || len(report.Problems()) > 0 {
			return 1
		}
		return 0
	}

	// **書き込むものがあるときだけ警告を出す。**毎回出すと、何も起きない実行でも
	// 警告が並び、本当に書き換えるときの一行が埋もれる。
	if len(report.Pending()) > 0 {
		fmt.Fprintf(stdout, "\n%s\n", i18n.T(i18n.KeyCLITrustWarnConcurrent, report.ClaudeConfigPath))
		fmt.Fprintln(stdout, i18n.T(i18n.KeyCLITrustWarnCloseClaude))
	}
	fmt.Fprintf(stdout, "\n")

	result, err := d.TrustApply(context.Background(), opts, report)
	if err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLITrustErrApply, err))
		return trustInternalErrorExitCode
	}
	if err := trust.WriteApplyResult(stdout, result); err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLITrustErrWriteResult, err))
		return trustInternalErrorExitCode
	}
	// 調べられなかったものと、書いたのに確認できなかったものは、まだ人間の手が要る。
	if len(report.Problems()) > 0 || len(result.VerifyProblems) > 0 {
		return 1
	}
	return 0
}

// runAllowKeychainAccess は `continuo allow-keychain-access` サブコマンドである。
//
// **Keychain へのアクセスを人間に1回だけ許可させるためにある。**macOS の Keychain は、
// 初めて読む実行ファイルに対して確認のダイアログを出す。**無人で走る continuo が
// そのダイアログに当たると、答える人がいないまま枠の判定の期限が切れる。**
// 人間が端末にいるうちに1回読んでおき、「常に許可」を選ばせるのがこのコマンドの仕事である。
//
// **読むのは項目の名前だけである。**トークンの値は画面にもログにも出さない
// （internal/ratelimit の ProbeKeychain）。
//
// **設定ファイルは読まない。**読む先は `rate_limit.token_source` の値によらず Keychain の
// 1項目に決まっており、WORKFLOW.md がまだ無い段階でも叩けたほうがよい。
//
// args: `continuo allow-keychain-access` に続く引数（**位置引数は受け付けない**）。
// stdout / stderr: 出力先。案内と結果は stdout へ、引数の誤りは stderr へ出す。
// 戻り値: 終了コード。**macOS 以外は 0**（何もしない）、読めたら 0、
// 読めなかった・期限内に返らなかったら 1、引数の指定が誤っていれば 2（--help / -h なら 0）。
func runAllowKeychainAccess(d Deps, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("continuo allow-keychain-access", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return parseErrorExitCode(err)
	}

	// runMain と同じ理由で、位置引数のあとに書かれたフラグを黙って無視しない。
	positional := fs.Args()
	for _, a := range positional {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIErrFlagAfterPositional, a))
			return 2
		}
	}
	if len(positional) > 0 {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIAllowKeychainAccessErrTooManyPositional, len(positional), positional))
		return 2
	}

	// **macOS 以外では何もしない。**`security` はほかの OS に無く、
	// 「失敗した」と出すのは誤った案内になる（前提が違うだけである）。
	if d.GOOS != "darwin" {
		fmt.Fprintln(stdout, i18n.T(i18n.KeyCLIAllowKeychainAccessNotDarwin, d.GOOS))
		return 0
	}

	// **読みに行く前に案内を出す。**ダイアログが出てから何を選べばよいかを探させない。
	fmt.Fprintln(stdout, i18n.T(i18n.KeyCLIAllowKeychainAccessBefore, ratelimit.KeychainService))
	fmt.Fprintln(stdout, i18n.T(i18n.KeyCLIAllowKeychainAccessBeforeDialog))

	// **人間がダイアログに答えるのを待つので、無人の経路より長い上限を使う**
	// （ratelimit.AllowAccessTimeout）。
	probe, err := d.ProbeKeychain(context.Background(), ratelimit.AllowAccessTimeout)
	switch {
	case errors.Is(err, ratelimit.ErrKeychainTimeout):
		fmt.Fprintln(stdout, i18n.T(i18n.KeyCLIAllowKeychainAccessTimeoutHeadline, ratelimit.AllowAccessTimeout))
		fmt.Fprintln(stdout, i18n.T(i18n.KeyCLIAllowKeychainAccessTimeoutHowTo))
		fmt.Fprintln(stdout, i18n.T(i18n.KeyCLIAllowKeychainAccessTimeoutCauses))
		fmt.Fprintln(stdout, i18n.T(i18n.KeyCLIAllowKeychainAccessTimeoutRemedy))
		return 1
	case err != nil:
		printKeychainFailure(stdout, i18n.T(i18n.KeyCLIAllowKeychainAccessErrHeadline, ratelimit.KeychainService, err))
		return 1
	case !probe.HasAccessToken:
		// **読めた項目は出す。**何が入っていたのかが分かると、人間は次に何を疑えばよいか判断できる。
		fmt.Fprintln(stdout, i18n.T(i18n.KeyCLIAllowKeychainAccessFields, strings.Join(probe.Fields, ", ")))
		printKeychainFailure(stdout, i18n.T(i18n.KeyCLIAllowKeychainAccessNoAccessToken, ratelimit.KeychainService))
		return 1
	default:
		fmt.Fprintln(stdout, i18n.T(i18n.KeyCLIAllowKeychainAccessOK, ratelimit.KeychainService))
		// **出すのは名前だけである。**値（トークン）は1つも出さない。
		fmt.Fprintln(stdout, i18n.T(i18n.KeyCLIAllowKeychainAccessFields, strings.Join(probe.Fields, ", ")))
		return 0
	}
}

// printKeychainFailure は Keychain を読めなかったときの案内を、原因と対処つきで出す（設計 3-34b）。
//
// **1行だけ出して終わらない。**読んだ人が次に何をすればよいかを、確かめ方・よくある原因・
// 対処の3つで書く。
//
// w: 出力先。
// headline: 1行目（何が起きたか）。
func printKeychainFailure(w io.Writer, headline string) {
	fmt.Fprintln(w, headline)
	fmt.Fprintln(w, i18n.T(i18n.KeyCLIAllowKeychainAccessErrHowTo, ratelimit.KeychainService))
	fmt.Fprintln(w, i18n.T(i18n.KeyCLIAllowKeychainAccessErrCauses))
	fmt.Fprintln(w, i18n.T(i18n.KeyCLIAllowKeychainAccessErrRemedy))
}

// runDoctor は `continuo doctor` サブコマンドである（設計 3-32）。
//
// **前提を見出し語ごとに検査して、足りないものと直し方を出す。**検査の実体は internal/doctor に
// あり、ここが決めるのは引数の受け取り方・出力先・終了コードだけである。
//
// **1つ失敗しても残りを全部検査する。**そのため、この関数は途中で戻らない。
//
// **検査そのものに期限を付ける**（`doctor.DefaultTimeout`）。gh / ghq / herdr / GitHub を
// 叩くので、期限が無いと「前提を機械的に検査する」道具が固まって人間の手が止まる。
//
// args: `continuo doctor` に続く引数（WORKFLOW.md のパスを0個か1個）。
// stdout / stderr: 出力先。検査結果は stdout へ出す。
// 戻り値: 終了コード。**`✗` が1つでもあれば 1、`!` だけなら 0**（設計 3-32）。
// 引数の指定が誤っていれば 2（--help / -h なら 0）。
// **検査結果を書き出せなかった場合と、接続先の環境変数が不正な場合は 3**
// （`✗` があった場合の 1 と区別できるようにする）。
func runDoctor(d Deps, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("continuo doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return parseErrorExitCode(err)
	}

	// runMain と同じ理由で、位置引数のあとに書かれたフラグを黙って無視しない。
	positional := fs.Args()
	for _, a := range positional {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIErrFlagAfterPositional, a))
			return 2
		}
	}
	if len(positional) > 1 {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIDoctorErrTooManyPositional, len(positional), positional))
		return 2
	}

	var argPath string
	if len(positional) == 1 {
		argPath = positional[0]
	}

	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIErrGetwd, err))
		return 1
	}
	// **設定ファイルの場所が決まらなくても検査は続ける。**場所が決まらないことは
	// 「設定ファイルを読めない」の一種であり、doctor はそれも記号で報告する対象である。
	path, err := config.ResolvePath(argPath, workDir)
	if err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIDoctorWarnPathUnresolved, err))
	}
	// **設定を読めたら、その言語で検査結果を出す**（設計 3-35）。読めなくても検査は続ける
	// （読めなかったこと自体が検査結果の1件目になる）。
	useLanguageFromConfig(path)

	// **接続先の差し替えは常駐プロセスと同じ環境変数で行う**（daemon.EnvGraphQLEndpoint）。
	// 空なら本番の GitHub GraphQL API を読む（読み取りだけである）。
	// **常駐プロセスと同じ検査を通す。**ここへ `gh auth token` のトークンが送られるので、
	// 宛先を確かめずに使わない。
	endpoint := os.Getenv(daemon.EnvGraphQLEndpoint)
	if err := daemon.ValidateGraphQLEndpoint(endpoint); err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIErrGeneric, err))
		return doctorInternalErrorExitCode
	}

	// **検査が返らないまま人間を待たせない。**全体にも1項目にも上限を置く（doctor 側の既定）。
	ctx, cancel := context.WithTimeout(context.Background(), doctor.DefaultTimeout)
	defer cancel()

	report := d.DoctorRun(ctx, doctor.Options{
		ConfigPath:      path,
		GraphQLEndpoint: endpoint,
	})
	if err := report.Write(stdout); err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIDoctorErrWriteReport, err))
		return doctorInternalErrorExitCode
	}
	return report.ExitCode()
}

// runAbandon は `continuo abandon` サブコマンドである。
//
// **間違えて着手した issue を、着手する前の状態へ戻す。**worktree・pane・
// herdr の workspace・branch を消し、`--to` があれば Status をその値へ動かす。
// 段の中身は internal/abandon にあり、ここが決めるのは引数の受け取り方と終了コードだけである。
//
// **位置引数は `init` / `doctor` / `trust` と揃える。**1つ目が issue の URL、
// 2つ目が WORKFLOW.md のあるディレクトリ（省略すると、いまいるディレクトリ）である。
//
// **`--dry-run` を先に出せるようにしてある。**worktree と branch を消すコマンドで、
// 何が消えるかを見る手段が無いのは危ない。
//
// **`SIGINT` / `SIGTERM` で止まれるようにする。**段1 は pane が閉じるのを最大で
// `herdr.read_timeout_ms` の10倍だけ待つので、その間に人間が止められなければならない。
//
// args: `continuo abandon` に続く引数（--dry-run / --force / --to / --park と、
// issue の URL、WORKFLOW.md のあるディレクトリ）。
// stdout / stderr: 出力先。消すものの一覧は stdout へ出す。
// 戻り値: 終了コード。**0 は消した・消すものが無かった・`--dry-run` で見せ終わった**、
// **1 は何も消さずに止まったか、消す途中で失敗した**、
// 2 は引数の指定が誤っている（--help / -h なら 0）。
func runAbandon(d Deps, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("continuo abandon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// **使い方を自分で出す。**flag は自分が知っているフラグしか出さないので、
	// 既定のままだと `continuo abandon --help` に位置引数が1つも載らない。
	fs.Usage = func() {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIAbandonUsage))
		fs.PrintDefaults()
	}
	dryRunFlag := fs.Bool("dry-run", false, i18n.T(i18n.KeyCLIAbandonFlagDryRun))
	forceFlag := fs.Bool("force", false, i18n.T(i18n.KeyCLIAbandonFlagForce))
	toFlag := fs.String("to", "", i18n.T(i18n.KeyCLIAbandonFlagTo))
	parkFlag := fs.String("park", "", i18n.T(i18n.KeyCLIAbandonFlagPark))
	if err := fs.Parse(args); err != nil {
		return parseErrorExitCode(err)
	}

	// runMain と同じ理由で、位置引数のあとに書かれたフラグを黙って無視しない。
	// ここで見逃すと、--dry-run のつもりで本当に消すことになる。
	positional := fs.Args()
	for _, a := range positional {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIErrFlagAfterPositional, a))
			return 2
		}
	}
	if len(positional) == 0 {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIAbandonErrIssueURLRequired))
		return 2
	}
	if len(positional) > 2 {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIAbandonErrTooManyPositional, len(positional), positional))
		return 2
	}

	issueURL := positional[0]
	var argPath string
	if len(positional) == 2 {
		argPath = positional[1]
	}

	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIErrGetwd, err))
		return 1
	}
	path, err := config.ResolvePath(argPath, workDir)
	if err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIErrResolveConfigPath, err))
		return 1
	}
	// **設定を読めたら、その言語で出す**（設計 3-35）。読めなければ abandon 自身が
	// 「WORKFLOW.md を読めません」を出すので、ここでは環境変数から決めた言語のまま進む。
	useLanguageFromConfig(path)

	// **トークンを載せる前に接続先を確かめる。**abandon はボードの Status を読み書きするので、
	// 常駐プロセスと同じ検査を通す。
	//
	// **この検査が拒むのは平文の http だけである**（ループバック宛は通す。
	// daemon.ValidateGraphQLEndpoint を見よ）。**https ならどのホストでも通る**ので、
	// 「宛先が GitHub であること」の保証ではない。**環境変数を書き換えられる人は、
	// この検査を通る別の宛先へトークンを送れる。**それを防ぐのは環境変数の側の管理である。
	endpoint := os.Getenv(daemon.EnvGraphQLEndpoint)
	if err := daemon.ValidateGraphQLEndpoint(endpoint); err != nil {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIErrGeneric, err))
		return 1
	}

	// **人間が止められるようにする。**pane が閉じるのを待つあいだ、Ctrl+C で抜けられる。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return d.AbandonRun(ctx, abandon.Options{
		ConfigPath:      path,
		IssueURL:        issueURL,
		DryRun:          *dryRunFlag,
		Force:           *forceFlag,
		ToState:         *toFlag,
		ParkState:       *parkFlag,
		GraphQLEndpoint: endpoint,
		Out:             stdout,
		Err:             stderr,
	})
}

// doctorInternalErrorExitCode は `continuo doctor` が検査そのものを実施・報告できなかった
// ときの終了コードである。
//
// **`✗` があったこと（1）と、引数の誤り（2）のどちらとも別の値にする。**
// スクリプトから「前提が足りない」と「doctor 自体が動けなかった」を区別できるようにする。
const doctorInternalErrorExitCode = 3

// parseErrorExitCode は flag.FlagSet.Parse が返したエラーを終了コードに直す。
//
// flag パッケージは `--help` / `-h` を受け取ると、使い方を出したうえで flag.ErrHelp を返す。
// これは利用者が意図して求めたものなので、引数の指定の誤り（終了コード 2）と同じ扱いにしない。
//
// err: fs.Parse が返した非 nil のエラー。
// 戻り値: flag.ErrHelp なら 0、それ以外（未知のフラグ、値の形式違いなど）は 2。
func parseErrorExitCode(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}

// runMain は継続監視の本体である（設計 3-4 の「起動から復元までの順序」）。
//
// **結線の実体は internal/daemon にある。**ここが決めるのは、引数の受け取り方・
// ログの出力先・`SIGINT` / `SIGTERM` を受けるコンテキストの作り方・終了コードだけである
// （`package main` の非公開関数は test/ から呼べないため、実体を internal へ置く）。
//
// args: `continuo` に続く引数（--log-level / --port と、WORKFLOW.md のパスを0個か1個）。
// stdout / stderr: 出力先。
// 戻り値: 終了コード。0 は正常終了（SIGINT / SIGTERM での停止を含む）、
// **1 は起動できなかったか、巡回中に落ちた**（ログの文言で言い分ける）、
// 2 は引数の指定が誤っている。
func runMain(d Deps, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("continuo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// **サブコマンドの一覧を出す。**
	//
	// flag は自分が知っているフラグしか出さないので、既定のままだと
	// `continuo --help` に `init` も `doctor` も載らない。**利用者は、何が使えるかを
	// 引数の一覧からしか知れない。**
	fs.Usage = func() {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIMainUsage))
		fs.PrintDefaults()
	}
	logLevelFlag := fs.String("log-level", "info", i18n.T(i18n.KeyCLIMainFlagLogLevel))
	// **`SPEC.md` 13.7 の「CLI `--port` overrides `server.port`」である。**
	// 既定値では区別が付かないので、渡されたかどうかは fs.Visit で見る
	// （`--port=0` は「OS に空きポートを選ばせる」という意味を持つ指定であり、
	// 「指定しなかった」と同じ扱いにしてはならない）。
	portFlag := fs.Int("port", 0, i18n.T(i18n.KeyCLIMainFlagPort))
	if err := fs.Parse(args); err != nil {
		return parseErrorExitCode(err)
	}
	var port *int
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			port = portFlag
		}
	})
	// **`--port` は `server.port` を上書きするので、同じ範囲で検査する。**
	//
	// **設定ファイル側だけを検査しても意味が無い**（`--port` が後から上書きする）。
	// ここで弾かないと、範囲外の値のまま常駐を始めて、待ち受けの段で初めて落ちる
	// （2026-08-21 にテストで発見。設計 6-2）。
	if port != nil && (*port < 0 || *port > 65535) {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIMainErrPortRange, *port))
		return 2
	}

	// flag パッケージは「位置引数のあとに書かれたフラグ」を黙って無視する
	// （例: `continuo WORKFLOW.md --log-level=debug` を渡すと、--log-level=debug は
	// フラグとして解釈されず、そのまま2つ目の位置引数として fs.Args() に残る）。
	// 気づかずに無視されるとオペレータが設定したつもりのフラグが効かないので、
	// 残った位置引数の中に "-" で始まるものが無いかを自前で検査して起動を止める。
	positional := fs.Args()
	for _, a := range positional {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIErrFlagAfterPositional, a))
			return 2
		}
	}
	if len(positional) > 1 {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIMainErrTooManyPositional, len(positional), positional))
		return 2
	}

	var argPath string
	if len(positional) == 1 {
		argPath = positional[0]
	}

	// 認識できないログレベルを黙って info に落とすと、無人運用では
	// 「指定したつもりのレベルが効いていない」ことに誰も気づけない。必ず警告を出す。
	level, levelOK := logging.ParseLevel(*logLevelFlag)
	logger := logging.New(stderr, level)
	if !levelOK {
		logger.Warn("ログレベルの指定を認識できないため info として扱います",
			"log_level", *logLevelFlag,
			"accepted", "debug|info|warn|error")
	}

	workDir, err := os.Getwd()
	if err != nil {
		logger.Error("作業ディレクトリの取得に失敗しました", "error", err)
		return 1
	}

	path, err := config.ResolvePath(argPath, workDir)
	if err != nil {
		logger.Error("設定ファイルの場所を決められません", "error", err)
		return 1
	}

	// **SIGINT / SIGTERM で巡回を止める。**受けたあとの作法（巡回を止め・hook の受け口を
	// 閉じ・turn ループの終了を待つ・**pane は閉じない**）は internal/daemon が持つ。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// **2回目の signal を効かせる。**1回目を受けたら登録を外し、既定の動作
	// （プロセスの終了）へ戻す。理由は daemon.RestoreDefaultSignalsOnShutdown にある。
	daemon.RestoreDefaultSignalsOnShutdown(ctx, stop)

	// **設定を読めたら、その言語で出す**（設計 3-35）。読めなければ daemon.Run が
	// 起動できない理由をログに出すので、ここでは環境変数から決めた言語のまま進む。
	useLanguageFromConfig(path)

	fmt.Fprintln(stdout, i18n.T(i18n.KeyCLIMainStarting, path))
	if err := d.DaemonRun(ctx, daemon.Options{ConfigPath: path, Logger: logger, Port: port}); err != nil {
		// **起動できなかったのか、動いていたものが落ちたのかを言い分ける。**
		// 無人運用のログを後から読む人間が、起動失敗と実行中の異常終了を取り違えないようにする。
		if errors.Is(err, daemon.ErrStartup) {
			logger.Error("continuo を起動できません", "error", err)
		} else {
			logger.Error("continuo が異常終了しました（起動は済んでいました）", "error", err)
		}
		return 1
	}
	logger.Info("continuo を終了しました")
	return 0
}

// runHook は `continuo hook` サブコマンドである（設計 3-2）。
//
// 標準入力の hook の JSON を hook 受け口の socket へ1行で転送し、応答を待たずに終わる。
// socket へ繋がらなければ --pending-dir の下へ逃がす（設計 3-19）。実体は
// internal/hookclient にあり、ここが決めるのは引数の受け取り方と、標準エラーへ出す文言だけである。
//
// **どの経路でも終了コードは 0 にする。**continuo が落ちていても、逃がし先へ書けなくても、
// エージェントを止めないためである。引数の指定が誤っている場合だけ 1 を返す。
// **2 を返してはならない。**Claude Code は hook の終了コード 2 を「その操作を止めろ」の
// 合図として扱うため、Stop hook で 2 を返すとエージェントが止まれなくなる。
//
// args: `continuo hook` に続く引数（--socket / --pending-dir）。
// stdin: hook の JSON の入力元。
// stderr: 出力先。転送できなかった理由をここへ出す。
// **`--socket` と `--pending-dir` は絶対パスでなければ受け付けない。**hook の cwd は
// worktree なので（設計 1-5）、相対パスを受けると逃がし先が worktree の中に掘られ、
// continuo は実行時ディレクトリの下しか走査しないので**永久に読まれない。**
// 受け口の側（`hookserver.New`）も同じ理由で絶対パスを要求している。
//
// 戻り値: 終了コード。--help / -h なら 0、引数の指定が誤っていれば 1、それ以外は常に 0。
func runHook(args []string, stdin io.Reader, stderr io.Writer) int {
	fs := flag.NewFlagSet("continuo hook", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socketFlag := fs.String("socket", "", i18n.T(i18n.KeyCLIHookFlagSocket))
	pendingDirFlag := fs.String("pending-dir", "", i18n.T(i18n.KeyCLIHookFlagPendingDir))
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIHookErrPositional, fs.Args()))
		return 1
	}
	if *socketFlag == "" {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIHookErrSocketRequired))
		return 1
	}
	// --pending-dir も必須にする。指定が落ちていると continuo が落ちている間の hook が
	// socket にも逃がし先にも残らず、設計 3-19 の逃がし先が丸ごと無効になる。
	// 設定ファイルのテンプレートの書き間違いを、hook 側で黙って見逃さない。
	if *pendingDirFlag == "" {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIHookErrPendingDirRequired))
		return 1
	}
	if !filepath.IsAbs(*socketFlag) {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIHookErrSocketAbs, *socketFlag))
		return 1
	}
	if !filepath.IsAbs(*pendingDirFlag) {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIHookErrPendingDirAbs, *pendingDirFlag))
		return 1
	}

	result := hookclient.Forward(hookclient.Config{
		SocketPath: *socketFlag,
		PendingDir: *pendingDirFlag,
		Stdin:      stdin,
	})

	if result.Truncated {
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIHookTruncated))
	}
	switch result.Outcome {
	case hookclient.OutcomeSpilled:
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIHookSpilled, result.PendingPath, result.Err))
	case hookclient.OutcomeDropped:
		fmt.Fprintln(stderr, i18n.T(i18n.KeyCLIHookDropped, result.Err))
	}
	return 0
}
