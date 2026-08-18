// continuo は GitHub Projects v2 のボードを見張り、issue ごとに git worktree を用意して
// herdr の pane で Claude Code を起動し、完了までを面倒見る常駐プロセスである。
// 設計の正は docs/plans/continuo_design.md にある。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/lock"
	"github.com/maimuzo/continuo/internal/logging"
	"github.com/maimuzo/continuo/internal/socketpath"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run は continuo の CLI 全体のエントリポイントである。os.Exit を直接呼ばないので
// テストから終了コードを検証できる。
//
// args: os.Args[1:] に相当するコマンドライン引数。
// stdout / stderr: 出力先。テストでは bytes.Buffer を渡して出力内容を検証できる。
// 戻り値: プロセスの終了コード（0 は正常終了）。
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "hook" {
		return runHook(args[1:], stdout, stderr)
	}
	return runMain(args, stdout, stderr)
}

// runMain は継続監視の本体である。第1段階では「設定を読み込み、検証し、
// 二重起動でないことを確認して終了する」ところまでを実装する。巡回・dispatch・
// turn ループなどは後続の段階（docs/plans/continuo_design.md 7節）で実装する。
func runMain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("continuo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	logLevelFlag := fs.String("log-level", "info", "ログレベル（debug|info|warn|error）")
	if err := fs.Parse(args); err != nil {
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

	loaded, err := config.Load(path)
	if err != nil {
		logger.Error("設定ファイルの読み込みに失敗しました", "path", path, "error", err)
		return 1
	}
	logger.Info("設定ファイルを読み込みました", "path", loaded.Path)

	sockPath, err := socketpath.ResolveHookSocketPath(loaded.Config.Claude.HookBridge.Listen, os.Getenv("CONTINUO_RUNTIME_DIR"))
	if err != nil {
		logger.Error("hook を受ける socket の場所を決められません", "error", err)
		return 1
	}
	if err := socketpath.EnsureDir(filepath.Dir(sockPath)); err != nil {
		logger.Error("hook を受ける socket のディレクトリを準備できません", "error", err)
		return 1
	}
	logger.Info("hook を受ける socket の場所を決めました", "socket", sockPath)

	lockPath := resolveLockFilePath(loaded.Config, sockPath)
	l, err := lock.Acquire(lockPath)
	if err != nil {
		logger.Error("二重起動を検出しました", "lock_file", lockPath, "error", err)
		return 1
	}
	defer func() {
		if err := l.Release(); err != nil {
			logger.Warn("ロックの解放に失敗しました", "error", err)
		}
	}()
	logger.Info("二重起動防止のロックを獲得しました", "lock_file", lockPath)

	// 巡回・dispatch・turn ループ以降は後続の段階（docs/plans/continuo_design.md 7節の
	// 段階6以降）で実装する。第1段階ではここまでで正常終了とする。
	logger.Info("continuo の第1段階（設定読み込み・二重起動防止）の起動処理が完了しました")
	return 0
}

// resolveLockFilePath は二重起動防止のロックファイルの絶対パスを決める。
// cfg.Runtime.LockFile が明示されていればそれを使い、無ければ hook socket と
// 同じディレクトリに置く（設計 5-2 の runtime.lock_file の既定値の説明）。
//
// cfg: 読み込み済みの設定（5-4 の展開を通した後のもの）。
// sockPath: 解決済みの hook socket の絶対パス。
// 戻り値: ロックファイルの絶対パス。
func resolveLockFilePath(cfg config.Config, sockPath string) string {
	if cfg.Runtime.LockFile != nil && *cfg.Runtime.LockFile != "" {
		return *cfg.Runtime.LockFile
	}
	return filepath.Join(filepath.Dir(sockPath), socketpath.LockFileName)
}

// runHook は `continuo hook` サブコマンドの骨組みである。
//
// 設計 3-2 のとおり、最終的には「標準入力の JSON をそのまま hook 受け口の socket へ
// 転送して終了するだけ」の薄い処理になる。socket 経由の通信は後続の段階
// （docs/plans/continuo_design.md 7節の段階4）で実装するため、第1段階では
// サブコマンドとして受理できることと、未実装であることを明示するところまでを行う。
//
// args: `continuo hook` に続く引数（--socket など）。
// stdout / stderr: 出力先。
// 戻り値: 終了コード。第1段階では常に1（未実装）を返す。
func runHook(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("continuo hook", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socketFlag := fs.String("socket", "", "hook を受ける socket のパス")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintf(stderr, "エラー: continuo hook は位置引数を受け付けません（%v が指定されました）\n", fs.Args())
		return 2
	}

	fmt.Fprintf(stderr, "continuo hook は未実装です（--socket=%q）。標準入力を socket へ転送する処理は後続の段階で実装します\n", *socketFlag)
	return 1
}
