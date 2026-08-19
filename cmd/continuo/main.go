// continuo は GitHub Projects v2 のボードを見張り、issue ごとに git worktree を用意して
// herdr の pane で Claude Code を起動し、完了までを面倒見る常駐プロセスである。
// 設計の正は docs/plans/continuo_design.md にある。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/daemon"
	"github.com/maimuzo/continuo/internal/doctor"
	"github.com/maimuzo/continuo/internal/hookclient"
	"github.com/maimuzo/continuo/internal/logging"
	"github.com/maimuzo/continuo/internal/scaffold"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run は continuo の CLI 全体のエントリポイントである。os.Exit を直接呼ばないので
// テストから終了コードを検証できる。
//
// args: os.Args[1:] に相当するコマンドライン引数。
// stdin: 標準入力。`continuo hook` が hook の JSON を読む先である。
// stdout / stderr: 出力先。テストでは bytes.Buffer を渡して出力内容を検証できる。
// 戻り値: プロセスの終了コード（0 は正常終了）。
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "hook":
			return runHook(args[1:], stdin, stderr)
		case "init":
			return runInit(args[1:], stdout, stderr)
		case "doctor":
			return runDoctor(args[1:], stdout, stderr)
		}
	}
	return runMain(args, stdout, stderr)
}

// runInit は `continuo init` サブコマンドである。WORKFLOW.md の雛形を1つだけ置く（設計 3-32）。
//
// 雛形を書き出す実体は internal/scaffold にある。ここが決めるのは、引数の受け取り方と、
// 失敗の種類ごとの文言・終了コードだけである。
//
// args: `continuo init` に続く引数。位置引数は書き出す先のディレクトリを0個か1個。
// 省略したら、いまいるディレクトリに書く。--force で既存の WORKFLOW.md を上書きする。
// stdout / stderr: 出力先。
// 戻り値: 終了コード。0 は書き出せた（--help / -h で使い方を出した場合も 0）、
// 1 は書き出せなかった（既にある・ディレクトリが無いなど）、2 は引数の指定が誤っている。
func runInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("continuo init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	forceFlag := fs.Bool("force", false, "既に WORKFLOW.md があっても上書きする")
	if err := fs.Parse(args); err != nil {
		return parseErrorExitCode(err)
	}

	// runMain と同じ理由で、位置引数のあとに書かれたフラグを黙って無視しない。
	// flag パッケージは最初の位置引数で解釈をやめるため、--force が効かないまま
	// 「既にあります」で止まる、という分かりにくい失敗になる。
	positional := fs.Args()
	for _, a := range positional {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintf(stderr, "エラー: 位置引数のあとにフラグらしき引数 %q があります。フラグは位置引数より前に書いてください\n", a)
			return 2
		}
	}
	if len(positional) > 1 {
		fmt.Fprintf(stderr, "エラー: continuo init の位置引数は、WORKFLOW.md を置くディレクトリを1つだけ受け付けます（%d 個指定されました: %v）\n", len(positional), positional)
		return 2
	}

	var dir string
	if len(positional) == 1 {
		dir = positional[0]
	}

	result, err := scaffold.WriteTemplate(dir, *forceFlag)
	switch {
	case err == nil:
		if result.Overwritten {
			fmt.Fprintf(stdout, "WORKFLOW.md を上書きしました: %s\n", result.Path)
		} else {
			fmt.Fprintf(stdout, "WORKFLOW.md を作成しました: %s\n", result.Path)
		}
		return 0
	case errors.Is(err, scaffold.ErrAlreadyExists):
		fmt.Fprintf(stderr, "%s は既にあります。上書きするなら --force を付けてください\n", result.Path)
		return 1
	case errors.Is(err, scaffold.ErrDirNotFound):
		// ディレクトリは作らない（--force でも作らない）。打ち間違えたパスに
		// WORKFLOW.md が生まれると、利用者は作ったはずのファイルを見失う。
		fmt.Fprintf(stderr, "エラー: %v。先にディレクトリを作ってください\n", err)
		return 1
	case errors.Is(err, scaffold.ErrNotADirectory):
		fmt.Fprintf(stderr, "エラー: %v。continuo init が受け取るのは WORKFLOW.md を置くディレクトリです\n", err)
		return 1
	case errors.Is(err, scaffold.ErrSymlink):
		// symlink は --force でも辿らない。辿ると指定されたディレクトリの外にある
		// リンク先を雛形で潰すため、--force を勧めてはならない。
		fmt.Fprintf(stderr, "エラー: %v。symlink を消すか、別のディレクトリを指定してください\n", err)
		return 1
	default:
		fmt.Fprintf(stderr, "エラー: WORKFLOW.md の雛形を書き出せません: %v\n", err)
		return 1
	}
}

// runDoctor は `continuo doctor` サブコマンドである（設計 3-32）。
//
// **前提の7項目を検査して、足りないものと直し方を出す。**検査の実体は internal/doctor に
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
func runDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("continuo doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return parseErrorExitCode(err)
	}

	// runMain と同じ理由で、位置引数のあとに書かれたフラグを黙って無視しない。
	positional := fs.Args()
	for _, a := range positional {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintf(stderr, "エラー: 位置引数のあとにフラグらしき引数 %q があります。フラグは位置引数より前に書いてください\n", a)
			return 2
		}
	}
	if len(positional) > 1 {
		fmt.Fprintf(stderr, "エラー: continuo doctor の位置引数は WORKFLOW.md のパスを1つだけ受け付けます（%d 個指定されました: %v）\n",
			len(positional), positional)
		return 2
	}

	var argPath string
	if len(positional) == 1 {
		argPath = positional[0]
	}

	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "エラー: 作業ディレクトリを取得できません: %v\n", err)
		return 1
	}
	// **設定ファイルの場所が決まらなくても検査は続ける。**場所が決まらないことは
	// 「設定ファイルを読めない」の一種であり、doctor はそれも記号で報告する対象である。
	path, err := config.ResolvePath(argPath, workDir)
	if err != nil {
		fmt.Fprintf(stderr, "設定ファイルの場所を決められません（残りの検査は続けます）: %v\n", err)
	}

	// **接続先の差し替えは常駐プロセスと同じ環境変数で行う**（daemon.EnvGraphQLEndpoint）。
	// 空なら本番の GitHub GraphQL API を読む（読み取りだけである）。
	// **常駐プロセスと同じ検査を通す。**ここへ `gh auth token` のトークンが送られるので、
	// 宛先を確かめずに使わない。
	endpoint := os.Getenv(daemon.EnvGraphQLEndpoint)
	if err := daemon.ValidateGraphQLEndpoint(endpoint); err != nil {
		fmt.Fprintf(stderr, "エラー: %v\n", err)
		return doctorInternalErrorExitCode
	}

	// **検査が返らないまま人間を待たせない。**全体にも1項目にも上限を置く（doctor 側の既定）。
	ctx, cancel := context.WithTimeout(context.Background(), doctor.DefaultTimeout)
	defer cancel()

	report := doctor.Run(ctx, doctor.Options{
		ConfigPath:      path,
		GraphQLEndpoint: endpoint,
	})
	if err := report.Write(stdout); err != nil {
		fmt.Fprintf(stderr, "エラー: 検査結果を書き出せません: %v\n", err)
		return doctorInternalErrorExitCode
	}
	return report.ExitCode()
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
func runMain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("continuo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	logLevelFlag := fs.String("log-level", "info", "ログレベル（debug|info|warn|error）")
	// **`SPEC.md` 13.7 の「CLI `--port` overrides `server.port`」である。**
	// 既定値では区別が付かないので、渡されたかどうかは fs.Visit で見る
	// （`--port=0` は「OS に空きポートを選ばせる」という意味を持つ指定であり、
	// 「指定しなかった」と同じ扱いにしてはならない）。
	portFlag := fs.Int("port", 0, "ダッシュボードのポート番号（server.port を上書きする。0 は空きポートを OS に選ばせる）")
	if err := fs.Parse(args); err != nil {
		return parseErrorExitCode(err)
	}
	var port *int
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			port = portFlag
		}
	})

	// flag パッケージは「位置引数のあとに書かれたフラグ」を黙って無視する
	// （例: `continuo WORKFLOW.md --log-level=debug` を渡すと、--log-level=debug は
	// フラグとして解釈されず、そのまま2つ目の位置引数として fs.Args() に残る）。
	// 気づかずに無視されるとオペレータが設定したつもりのフラグが効かないので、
	// 残った位置引数の中に "-" で始まるものが無いかを自前で検査して起動を止める。
	positional := fs.Args()
	for _, a := range positional {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintf(stderr, "エラー: 位置引数のあとにフラグらしき引数 %q があります。フラグは位置引数より前に書いてください\n", a)
			return 2
		}
	}
	if len(positional) > 1 {
		fmt.Fprintf(stderr, "エラー: 位置引数は WORKFLOW.md のパスを1つだけ受け付けます（%d 個指定されました: %v）\n", len(positional), positional)
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

	fmt.Fprintf(stdout, "continuo を起動します（設定ファイル: %s）\n", path)
	if err := daemon.Run(ctx, daemon.Options{ConfigPath: path, Logger: logger, Port: port}); err != nil {
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
	socketFlag := fs.String("socket", "", "hook を受ける socket の絶対パス")
	pendingDirFlag := fs.String("pending-dir", "", "socket へ繋がらなかったときの逃がし先の絶対パス")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintf(stderr, "エラー: continuo hook は位置引数を受け付けません（%v が指定されました）\n", fs.Args())
		return 1
	}
	if *socketFlag == "" {
		fmt.Fprintf(stderr, "エラー: continuo hook には --socket が必要です（continuo が issue ごとの設定ファイルへ絶対パスを埋め込みます）\n")
		return 1
	}
	// --pending-dir も必須にする。指定が落ちていると continuo が落ちている間の hook が
	// socket にも逃がし先にも残らず、設計 3-19 の逃がし先が丸ごと無効になる。
	// 設定ファイルのテンプレートの書き間違いを、hook 側で黙って見逃さない。
	if *pendingDirFlag == "" {
		fmt.Fprintf(stderr, "エラー: continuo hook には --pending-dir が必要です（continuo が落ちている間の hook の逃がし先です。continuo が issue ごとの設定ファイルへ絶対パスを埋め込みます）\n")
		return 1
	}
	if !filepath.IsAbs(*socketFlag) {
		fmt.Fprintf(stderr, "エラー: --socket は絶対パスで指定してください（hook の cwd は worktree なので、相対パスでは繋ぎ先が定まりません）: %q\n", *socketFlag)
		return 1
	}
	if !filepath.IsAbs(*pendingDirFlag) {
		fmt.Fprintf(stderr, "エラー: --pending-dir は絶対パスで指定してください（hook の cwd は worktree なので、相対パスでは逃がし先が worktree の中に掘られ、continuo に読まれません）: %q\n", *pendingDirFlag)
		return 1
	}

	result := hookclient.Forward(hookclient.Config{
		SocketPath: *socketFlag,
		PendingDir: *pendingDirFlag,
		Stdin:      stdin,
	})

	if result.Truncated {
		fmt.Fprintf(stderr, "continuo hook: 標準入力が上限を超えたので、判定に要る項目だけを拾って転送しました（tool_input / tool_response は落ちています）\n")
	}
	switch result.Outcome {
	case hookclient.OutcomeSpilled:
		fmt.Fprintf(stderr, "continuo hook: socket へ転送できなかったので逃がし先へ書きました（%s）: %v\n",
			result.PendingPath, result.Err)
	case hookclient.OutcomeDropped:
		fmt.Fprintf(stderr, "continuo hook: この hook はどこにも記録できませんでした: %v\n", result.Err)
	}
	return 0
}
