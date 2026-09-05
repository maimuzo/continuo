package doctor

import (
	"context"
	"errors"
	"fmt"
	"github.com/maimuzo/continuo/internal/socketpath"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/fsprobe"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/prompt"
	"github.com/maimuzo/continuo/internal/ratelimit"
	"github.com/maimuzo/continuo/internal/tracker"
	"github.com/maimuzo/continuo/internal/workspace"
)

// checkConfig は設定ファイルを読んで検証する（見出し語 `設定ファイル`）。
//
// **`config.Load` をそのまま呼ぶ。**未知キーと不正値の検出はそこに入っている。
//
// **落ちた理由で直し方を変える。**理由を問わず `continuo init` を勧めていたとき、
// ファイルシステムが壊れて読めなくなった利用者を「設定を作り直す」方向へ誘導した
// （issue #11）。その案内に従うと `continuo init` は「既にあります」で止まり、
// **`--force` を足すと本物の設定を雛形で潰す。**
//
// path: 読み込む WORKFLOW.md の絶対パス。
// 戻り値の1つ目: 検査結果。
// 戻り値の2つ目: 読めた場合の設定（読めなければ OK が偽）。
func checkConfig(path string) (Result, loadedConfig) {
	loaded, err := config.Load(path)
	if err != nil {
		res := Result{
			Label:    LabelConfig,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorConfigUnreadable, path, err),
			Remedies: configRemedies(path, err),
		}
		if fsprobe.Classify(err) == fsprobe.FaultFilesystem {
			res.Notes = []string{i18n.T(i18n.KeyDoctorFilesystemFault)}
		}
		return res, loadedConfig{}
	}
	return Result{
		Label:  LabelConfig,
		Symbol: SymbolOK,
		Detail: i18n.T(i18n.KeyDoctorConfigOK, loaded.Path),
	}, loadedConfig{OK: true, Config: loaded.Config, Loaded: loaded}
}

// checkPromptVariables は、送るプロンプトの変数を検査する（見出し語 `プロンプトの変数`。設計 5-3c）。
//
// **`✗` にする。**この誤りがあると **issue が1件も着手できない。**
// `未記入の項目` と違い、既定値で代わりが利かない。
//
// **言い切らない。**検査は作り物の issue で2回変数展開するだけなので、
// `{{if eq .issue.state "Done"}}` のように値そのもので分かれる枝の中までは届かない。
// 文言も「検査に使った作り物の issue では」と範囲を書く。
//
// cfg: 設定を読んだ結果。
// configSymbol: 設定ファイルの検査の記号。
// 戻り値: 検査結果。
func checkPromptVariables(cfg loadedConfig, configSymbol Symbol) Result {
	if configSymbol != SymbolOK || !cfg.OK || cfg.Loaded == nil {
		return Result{
			Label:  LabelPromptVariables,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorPromptVariablesUnknown),
		}
	}
	loaded := cfg.Loaded

	frag := prompt.Build(loaded.PromptTemplate, loaded.Path)
	if err := frag.Validate(); err != nil {
		return Result{
			Label:    LabelPromptVariables,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorPromptVariablesInvalid, err),
			Remedies: []string{i18n.T(i18n.KeyDoctorPromptVariablesRemedy)},
		}
	}

	key := i18n.KeyDoctorPromptVariablesOKNoBody
	if frag.HasBody() {
		key = i18n.KeyDoctorPromptVariablesOK
	}
	return Result{
		Label:  LabelPromptVariables,
		Symbol: SymbolOK,
		Detail: i18n.T(key, loaded.Path),
	}
}

// configRemedies は設定ファイルを読めなかった理由ごとの直し方を返す（issue #11）。
//
// **`errors.Is` で読み分ける**（fsprobe.Classify がそれを行う）。文言の中身では判定しない。
//
//	無い（ENOENT）             … `continuo init` で雛形を置く
//	権限が足りない（EACCES）    … 所有者と権限を確かめる
//	ファイルシステムの異常      … マウント・カーネルログ・空き容量・WSL の再起動
//	それ以外                  … ファイルは読めているので front matter を直す
//
// path: 読もうとした WORKFLOW.md の絶対パス。
// err: config.Load が返したエラー。
// 戻り値: 画面に出す直し方の並び。
func configRemedies(path string, err error) []string {
	switch fsprobe.Classify(err) {
	case fsprobe.FaultNotExist:
		return []string{i18n.T(i18n.KeyDoctorConfigRemedyInit)}
	case fsprobe.FaultPermission:
		return []string{i18n.T(i18n.KeyDoctorConfigRemedyPermission, path)}
	case fsprobe.FaultFilesystem:
		return filesystemRemedies()
	default:
		return []string{i18n.T(i18n.KeyDoctorConfigRemedyFrontMatter)}
	}
}

// filesystemRemedies はファイルシステムが壊れているときの直し方を返す（issue #11）。
//
// **利用者の WSL で `EIO` と `EROFS` が同時に出た。**カーネルが ext4 を read-only へ
// 落とし、そのうえで I/O エラーも返していた。**OS を再起動して直った。**
//
// 戻り値: 画面に出す直し方の並び（確かめる順に並べる）。
func filesystemRemedies() []string {
	return []string{
		i18n.T(i18n.KeyDoctorFilesystemRemedyMount),
		i18n.T(i18n.KeyDoctorFilesystemRemedyDmesg),
		i18n.T(i18n.KeyDoctorFilesystemRemedyDisk),
		i18n.T(i18n.KeyDoctorFilesystemRemedyRestart),
	}
}

// writeRemedies は「そこへ書けなかった」ときの直し方を返す。
//
// dir: 書けなかった場所の絶対パス。
// err: 書き込みが返したエラー。
// 戻り値: 画面に出す直し方の並び。
func writeRemedies(dir string, err error) []string {
	if fsprobe.Classify(err) == fsprobe.FaultFilesystem {
		return filesystemRemedies()
	}
	return []string{i18n.T(i18n.KeyDoctorWriteRemedyPermission, dir)}
}

// checkCleanupStates は、片付けを始める Status が「終わったとみなす Status」に
// 収まっているかを検査する（見出し語 `片付けの状態`。設計 3-9e。issue #35）。
//
// **判定は書き直さない。**internal/config の `CleanupStatesOutsideTerminal` をそのまま呼ぶ。
// 起動時の警告（internal/daemon の `WarnCleanupStates`）も同じ関数を呼んでいる。
// **違うのは出し方だけである。**
//
// **記号は `✗` ではなく `!` にする。**噛み合っていなくても continuo は起動し、走る。
// **起動を止めると、いま動いている人の continuo が版を上げた瞬間に起動しなくなる**
// （報告された `WORKFLOW.md` が、まさにこの検査に引っかかる形だった）。
//
// **なぜ既にある検査では捕まらないのか。**`config.Validate` が見ているのは
// 「`cleanup.on_states` が `tracker.active_states` と重ならないこと」だけである。
// あちらは**走っている worktree を消す**ので起動を止める価値があるが、
// **`tracker.terminal_states` との関係は誰も見ていなかった。**
//
// **どのキーのどの値かを必ず出す。**見出し語 `Status の名前` と同じ流儀である。
//
// cfg: 読めた場合の設定。
// configSymbol: 上流（設定ファイル）の記号。
// 戻り値: 検査結果。
func checkCleanupStates(cfg loadedConfig, configSymbol Symbol) Result {
	if configSymbol != SymbolOK {
		return Result{
			Label:  LabelCleanupStates,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorCleanupStatesConfigUnreadable),
		}
	}
	// **片付けそのものを行わない設定なら、噛み合っていなくても何も起きない。**
	// ここで注意を出すと、`cleanup.enabled: false` にした人が毎回読み飛ばす注意を1件抱える。
	if !cfg.Config.Cleanup.Enabled {
		return Result{
			Label:  LabelCleanupStates,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorCleanupStatesDisabled),
		}
	}

	outside := config.CleanupStatesOutsideTerminal(cfg.Config)
	if len(outside) == 0 {
		return Result{
			Label:  LabelCleanupStates,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorCleanupStatesOK, len(cfg.Config.Cleanup.OnStates)),
		}
	}

	notes := make([]string, 0, len(outside))
	remedies := make([]string, 0, len(outside))
	for _, state := range outside {
		quoted := strconv.Quote(state)
		notes = append(notes, i18n.T(i18n.KeyDoctorCleanupStatesNote, quoted))
		remedies = append(remedies, i18n.T(i18n.KeyDoctorCleanupStatesRemedy, quoted, quoted))
	}
	return Result{
		Label:    LabelCleanupStates,
		Symbol:   SymbolUnknown,
		Detail:   i18n.T(i18n.KeyDoctorCleanupStatesMismatch, len(outside)),
		Notes:    notes,
		Remedies: remedies,
	}
}

// checkClaudeHome は Claude Code の設定ディレクトリに本当に書けるかを検査する
// （見出し語 `Claude の設定`。issue #11）。
//
// **文字列を組み立てるだけでは足りない。**`~/.claude/session-env/<使い捨ての名前>` を
// 実際に作って消す（hook の置き場所の検査と同じ流儀）。
//
// **設定ファイルが読めなくても走らせる。**この検査は設定を1バイトも読まないので、
// 設定が `✗` でも成立する。**今回の `EROFS` を先に捕まえるのはこの検査である。**
//
// opts: `HomeDir` を使う（テストは一時ディレクトリを渡すこと）。
// 戻り値: 検査の結果。
func checkClaudeHome(opts Options) Result {
	dir, err := fsprobe.ProbeClaudeSessionEnv(opts.HomeDir)
	if err != nil {
		res := Result{
			Label:    LabelClaudeHome,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorClaudeHomeFailed, dir, err),
			Notes:    []string{i18n.T(i18n.KeyDoctorClaudeHomeReason)},
			Remedies: writeRemedies(dir, err),
		}
		if fsprobe.Classify(err) == fsprobe.FaultFilesystem {
			res.Notes = append(res.Notes, i18n.T(i18n.KeyDoctorFilesystemFault))
		}
		return res
	}
	return Result{
		Label:  LabelClaudeHome,
		Symbol: SymbolOK,
		Detail: i18n.T(i18n.KeyDoctorClaudeHomeOK, dir),
	}
}

// checkWorkspaceRoot は worktree の置き場所に本当に書けるかを検査する
// （見出し語 `worktree の場所`。issue #11）。
//
// **設定が読めているときだけ走る。**置き場所は `workspace.root` にしか書いていない。
//
// **ここが書けないと、着手は段3（ws.Prepare）で必ず落ちる。**doctor がそれを
// 事前に何も言わなかったので、利用者は起動してから初めて気づくことになっていた。
//
// **書けたあとに、壊れた worktree が残っていないかも見る**（設計 3-49）。
// `workspace.on_broken_worktree` が既定の `stop` なら、それが1件でもあると continuo は
// 起動しない。**doctor が黙っていると、利用者は起動してから初めてそれを知る。**
//
// opts: doctor の入力（ホームディレクトリの差し替えに使う）。
// cfg: 読めた場合の設定。
// configSymbol: 設定ファイルの検査の結果。
// 戻り値: 検査の結果。
func checkWorkspaceRoot(opts Options, cfg loadedConfig, configSymbol Symbol) Result {
	if configSymbol != SymbolOK {
		return Result{
			Label:  LabelWorkspaceRoot,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorWorkspaceRootConfigUnreadable),
		}
	}
	root := cfg.Config.Workspace.Root
	if err := fsprobe.ProbeWritable(root); err != nil {
		res := Result{
			Label:    LabelWorkspaceRoot,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorWorkspaceRootFailed, root, err),
			Notes:    []string{i18n.T(i18n.KeyDoctorWorkspaceRootReason)},
			Remedies: writeRemedies(root, err),
		}
		if fsprobe.Classify(err) == fsprobe.FaultFilesystem {
			res.Notes = append(res.Notes, i18n.T(i18n.KeyDoctorFilesystemFault))
		}
		return res
	}
	return brokenWorktreeResult(opts, cfg, root)
}

// brokenWorktreeResult は、置き場所に**身元を確かめられない worktree**が残っていないかを
// 調べて、見出し語 `worktree の場所` の結果に足す（設計 3-49）。
//
// **起動する前に気づけるほうがよい。**`workspace.on_broken_worktree` が既定の `stop` なら、
// この worktree が1件でもあるだけで continuo は起動しない。doctor が黙っていると、
// 利用者は起動してから初めてそれを知る。
//
// **判定は書き直さない。**internal/workspace の ScanBroken をそのまま呼ぶ
// （doctor は判定の実体を持たない、という 3-32 の規則どおりである）。
//
// **1件も消さない。**doctor は読むだけである。
//
// opts: doctor の入力（ホームディレクトリの差し替えに使う）。
// cfg: 読めた設定。
// root: 検査した置き場所。
// 戻り値: 検査の結果。
func brokenWorktreeResult(opts Options, cfg loadedConfig, root string) Result {
	ok := Result{
		Label:  LabelWorkspaceRoot,
		Symbol: SymbolOK,
		Detail: i18n.T(i18n.KeyDoctorWorkspaceRootOK, root),
	}

	manager, err := workspace.New(workspace.Options{Config: cfg.Config, HomeDir: opts.HomeDir})
	if err != nil {
		ok.Notes = append(ok.Notes, i18n.T(i18n.KeyDoctorWorkspaceRootBrokenScanFailed, root, err))
		return ok
	}
	broken, err := manager.ScanBroken()
	if err != nil {
		ok.Notes = append(ok.Notes, i18n.T(i18n.KeyDoctorWorkspaceRootBrokenScanFailed, root, err))
		return ok
	}
	if len(broken) == 0 {
		return ok
	}

	// **何が起きているかと、次に何をすべきかを、必ず両方出す。**
	identityFile := manager.IdentityFileName()
	for _, b := range broken {
		ok.Notes = append(ok.Notes, b.What(identityFile))
		ok.Remedies = append(ok.Remedies, b.NextSteps()...)
	}
	if cfg.Config.Workspace.OnBrokenWorktree == config.OnBrokenWorktreeSkip {
		// **飛ばす設定でも黙らない。**起動はできるが、その worktree は誰も面倒を見ない。
		ok.Detail = i18n.T(i18n.KeyDoctorWorkspaceRootBrokenSkip, root, len(broken))
		return ok
	}
	ok.Symbol = SymbolMissing
	ok.Detail = i18n.T(i18n.KeyDoctorWorkspaceRootBrokenStop, root, len(broken))
	return ok
}

// daemonEnvRuntimeDir は実行時ディレクトリを差し替える環境変数である。
//
// **`internal/daemon` の定数と同じ値でなければならない。**
// doctor が daemon を import すると循環するので、ここに置く。
// **食い違うと、doctor が見る場所と起動が使う場所がずれる。**
const daemonEnvRuntimeDir = "CONTINUO_RUNTIME_DIR"

// runtimeDirDialTimeout は、置き場所に在るものへ繋いでみるときの待ち時間である。
//
// **既に continuo が待ち受けているなら、繋がるのは同じマシンの unix socket なので即座である。**
// 待つのは、相手が backlog を捌けずに詰まっている場合だけなので、短くてよい。
const runtimeDirDialTimeout = 2 * time.Second

// checkRuntimeDir は、hook を受ける socket を実際に置けるかを検査する。
//
// **文字列を組み立てるだけでは足りない。**決めた場所にディレクトリを作り、
// unix socket を listen して、すぐ閉じるところまで通す。
//
// **これが無かったとき、当時の項目すべてが ✓ で「足りないものはありません」と出たのに、
// 起動だけが落ちた**（issue #9）。
//
//	mkdir /run/user/1000: permission denied
//
// `XDG_RUNTIME_DIR` が設定されているのにディレクトリが無い環境（systemd が
// 動いていない WSL など）で起きる。**doctor は「起動できるか」を答えるべきである。**
//
// **設定が読めなくても走る**（issue #11）。`claude.hook_bridge.listen` が無指定の
// ときと同じ探索順で置き場所が決まるので、既定値だけで「書けるか」は分かる。
//
// cfg: 設定。読めていれば `claude.hook_bridge.listen` を使う。
// configSymbol: 設定ファイルの検査の結果。`✓` でなければ既定値で確かめる。
// 戻り値: 検査の結果。
func checkRuntimeDir(cfg loadedConfig, configSymbol Symbol) Result {
	// **設定が読めなくても、既定値で成立するところまでは確かめる**（issue #11）。
	// `claude.hook_bridge.listen` が無指定のときと同じ探索順で置き場所が決まるので、
	// 設定を読めていなくても「書けるかどうか」は分かる。**設定が読めないという理由だけで
	// すべての見出し語を `!` にしてしまうと、本当の原因を1つも指摘できない。**
	var listen *string
	var notes []string
	if configSymbol == SymbolOK {
		listen = cfg.Config.Claude.HookBridge.Listen
	} else {
		notes = []string{i18n.T(i18n.KeyDoctorDefaultUsed)}
	}

	sock, err := socketpath.Prepare(os.Getenv(daemonEnvRuntimeDir), listen)
	if err != nil {
		return Result{
			Label:    LabelRuntimeDir,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorRuntimeDirFailed, err),
			Notes:    runtimeDirNotes(notes, err),
			Remedies: []string{i18n.T(i18n.KeyDoctorRuntimeDirRemedy)},
		}
	}

	// **本当に listen できるかまで確かめる。**
	// パス長の上限、権限、既に使われている、のどれでもここで分かる。
	//
	// **EADDRINUSE は「既に continuo が動いている」を意味しない。**そのパスに何かが在れば
	// 必ず返る（通常ファイル・ディレクトリ・listen していない残骸の socket のどれでも
	// errno 48 が返ることを darwin で実測した）。**繋がるかどうかまで見ないと、
	// continuo が起動できない状態を `✓` と報告する。**その形は issue #9 と同じで、
	// **doctor が全項目 ✓ なのに起動だけが落ちる。**
	ln, lerr := net.Listen("unix", sock)
	if lerr != nil {
		if errors.Is(lerr, syscall.EADDRINUSE) {
			return runtimeDirInUse(sock, notes)
		}
		return Result{
			Label:    LabelRuntimeDir,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorRuntimeDirFailed, lerr),
			Notes:    runtimeDirNotes(notes, lerr),
			Remedies: []string{i18n.T(i18n.KeyDoctorRuntimeDirRemedy)},
		}
	}
	_ = ln.Close()
	// **作った socket は消す。**残すと、次に起動する continuo が
	// 「既に動いている」と誤解しかねない。
	_ = os.Remove(sock)

	return Result{
		Label:  LabelRuntimeDir,
		Symbol: SymbolOK,
		Detail: i18n.T(i18n.KeyDoctorRuntimeDirOK, sock),
		Notes:  notes,
	}
}

// runtimeDirInUse は、socket の置き場所が既に使われていたときの結果を組み立てる。
//
// **繋がるかどうかで分ける。**hookserver が起動時に行う判定（internal/hookserver の
// removeStaleSocketFile）と同じやり方にする。繋がれば既に continuo が待ち受けており、
// そのまま起動できる。繋がらなければ、hookserver は残骸を消してから作ろうとするので、
// **消せない残骸（root 所有のファイル、ディレクトリなど）は起動を止める。**
//
// sock: 決まった socket のパス。
// notes: 記号の下に添える内訳（設定を読めずに既定値で確かめたこと、など）。
// 戻り値: 検査の結果。
func runtimeDirInUse(sock string, notes []string) Result {
	conn, derr := net.DialTimeout("unix", sock, runtimeDirDialTimeout)
	if derr == nil {
		_ = conn.Close()
		return Result{
			Label:  LabelRuntimeDir,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorRuntimeDirInUse, sock),
			Notes:  notes,
		}
	}
	return Result{
		Label:  LabelRuntimeDir,
		Symbol: SymbolMissing,
		Detail: i18n.T(i18n.KeyDoctorRuntimeDirStale, sock),
		Notes:  notes,
		Remedies: []string{
			i18n.T(i18n.KeyDoctorRuntimeDirRemedyStale, sock, sock),
			i18n.T(i18n.KeyDoctorRuntimeDirRemedy),
		},
	}
}

// runtimeDirNotes は hook の置き場所の検査で、記号の下に添える内訳を組み立てる。
//
// notes: 既に決まっている内訳（既定値で確かめたことなど）。
// err: 検査が返したエラー。
// 戻り値: ファイルシステムの異常なら、その旨を足した内訳。
func runtimeDirNotes(notes []string, err error) []string {
	if fsprobe.Classify(err) != fsprobe.FaultFilesystem {
		return notes
	}
	return append(append([]string{}, notes...), i18n.T(i18n.KeyDoctorFilesystemFault))
}

// checkHerdr は herdr の socket の ping を呼び、protocol が設定と一致するかを検査する
// （見出し語 `herdr`）。
//
// **`herdr status` の CLI は使わない**（設計 2-1 / 3-32）。internal/herdr の CheckProtocol を呼ぶ。
//
// ctx: 呼び出しに適用するコンテキスト。
// cfg: 読めた場合の設定。
// configSymbol: 上流（設定ファイル）の記号。
// 戻り値: 検査結果。設定が読めていなければ `!` を返す（照合する protocol が決まらない）。
func checkHerdr(ctx context.Context, cfg loadedConfig, configSymbol Symbol) Result {
	if configSymbol != SymbolOK {
		return Result{
			Label:  LabelHerdr,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorHerdrConfigUnreadable),
		}
	}

	socketPath, err := herdr.ResolveSocketPath(cfg.Config.Herdr.Socket)
	if err != nil {
		return Result{
			Label:    LabelHerdr,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorHerdrSocketUnresolved, err),
			Remedies: []string{i18n.T(i18n.KeyDoctorHerdrRemedySocketAbs)},
		}
	}

	client := herdr.New(socketPath, herdr.Timeouts{
		Read: time.Duration(cfg.Config.Herdr.ReadTimeoutMs) * time.Millisecond,
	})
	ping, err := client.CheckProtocol(ctx, cfg.Config.Herdr.Protocol)
	if err != nil {
		if timedOut(ctx, err) {
			// **期限切れは「足りない」ではない。**herdr が遅いだけかもしれないので、
			// 終了コードを 1 にせず「確かめられなかった」として残りの検査へ進む。
			return Result{
				Label:    LabelHerdr,
				Symbol:   SymbolUnknown,
				Detail:   i18n.T(i18n.KeyDoctorHerdrTimeout, socketPath, err),
				Remedies: []string{i18n.T(i18n.KeyDoctorHerdrRemedyTimeout)},
			}
		}
		detail := fmt.Sprintf("%v", err)
		remedy := i18n.T(i18n.KeyDoctorHerdrRemedyNotListening, socketPath)
		if ping != nil {
			// **protocol が食い違っただけの場合は、socket には届いている。**
			// 直すのは herdr の起動ではなく、continuo と herdr の版の組み合わせである。
			remedy = i18n.T(i18n.KeyDoctorHerdrRemedyProtocol, ping.Protocol)
		}
		return Result{
			Label:    LabelHerdr,
			Symbol:   SymbolMissing,
			Detail:   detail,
			Remedies: []string{remedy},
		}
	}
	return Result{
		Label:  LabelHerdr,
		Symbol: SymbolOK,
		Detail: i18n.T(i18n.KeyDoctorHerdrOK, ping.Protocol, ping.Version, socketPath),
	}
}

// checkGHAuth は `gh` の有無と `gh auth status` の scope を検査する（見出し語 `gh の認証`）。
//
// 読み方は internal/tracker の CheckGHProjectScope が1つに決めてある（設計 3-32）。
// **対象のホストは github.com に固定**し、**`Active account: true` の行を持つブロックだけ**を読み、
// **`Token scopes:` をカンマで区切って前後の空白と引用符を落とし**、**`project` が1つの要素として
// 在ること**を合格とする。`read:project` は不可である。
//
// **設定ファイルの下流である**（設計 3-32 の依存の図）。上流が `✗` か `!` なら
// 検査せずに `!` にして理由を出す。読む値そのものは設定に無い（ホストは github.com に固定）が、
// **依存の図と「上流が `✗` か `!` なら下流を `!` にする」の規則を実装で曲げない。**
//
// ctx: 呼び出しに適用するコンテキスト。
// opts: `gh auth status` の差し替え口を含む入力。
// configSymbol: 上流（設定ファイル）の記号。
// 戻り値: 検査結果。gh が無い場合・未ログインの場合・scope が足りない場合は `✗`。
// 設定ファイルが `✓` でなければ `!`。
func checkGHAuth(ctx context.Context, opts Options, configSymbol Symbol) Result {
	if configSymbol != SymbolOK {
		return Result{
			Label:  LabelGHAuth,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorGHAuthConfigUnreadable),
			Remedies: []string{
				i18n.T(i18n.KeyDoctorGHAuthRemedyFixConfig),
			},
		}
	}
	if err := tracker.CheckGHAvailable(); err != nil {
		return Result{
			Label:    LabelGHAuth,
			Symbol:   SymbolMissing,
			Detail:   fmt.Sprintf("%v", err),
			Remedies: []string{i18n.T(i18n.KeyDoctorGHAuthRemedyInstall)},
		}
	}
	if err := tracker.CheckGHProjectScope(ctx, opts.GHAuthStatus); err != nil {
		if timedOut(ctx, err) {
			return Result{
				Label:    LabelGHAuth,
				Symbol:   SymbolUnknown,
				Detail:   i18n.T(i18n.KeyDoctorGHAuthTimeout, err),
				Remedies: []string{i18n.T(i18n.KeyDoctorGHAuthRemedyTimeout)},
			}
		}
		return Result{
			Label:  LabelGHAuth,
			Symbol: SymbolMissing,
			Detail: fmt.Sprintf("%v", err),
			Remedies: []string{
				i18n.T(i18n.KeyDoctorGHAuthRemedyLogin),
			},
		}
	}
	return Result{
		Label:  LabelGHAuth,
		Symbol: SymbolOK,
		Detail: i18n.T(i18n.KeyDoctorGHAuthOK),
	}
}

// checkBoard はボードを読む（見出し語 `ボード`）。
//
// 3つのことを行う。**GraphQL のリクエストも3本送る。**
//
//	1 Bootstrap … project と Status フィールドを解決し、active_states・terminal_states 等の
//	              選択肢名がボード側に全部あるかを照合する。**不一致は `✗`**（巡回が無言で
//	              0件を返す原因になる。設計 3-6 / 3-32）
//	2 候補の取得 … active_states の issue を読み、対象リポジトリを集める
//	3 自動化の取得 … カンバンの組み込みの自動化を読む（見出し語 `自動化`）。
//	              **要る2本より後ろに置く。**読めなくてもこの見出し語は落とさない
//	              （自動化は起動の前提ではないので、期限が足りなければこの1本だけを諦める）
//
// **記号は落ち方で分ける**（設計 3-32）。レートリミットだけ `!`（一時的である）、
// project が見つからない・トークンの取り出しに失敗・選択肢名の不一致は `✗` である。
//
// **信頼の判定関数はアダプタに渡さない。**渡すと候補の取得のたびに issue ごとの ghq と git が
// 走る。doctor はリポジトリ単位で1回ずつ検査する（段6）ので、ここでは要らない。
//
// ctx: 呼び出しに適用するコンテキスト。
// cfg: 読めた場合の設定。
// opts: GraphQL の接続先・HTTP クライアント・`gh auth token` の差し替え口。
// configSymbol: 上流（設定ファイル）の記号。
// ghSymbol: 上流（gh の認証）の記号。
// 戻り値の1つ目: 検査結果。
// 戻り値の2つ目: ボードから集めた対象リポジトリ（読めなければ nil）。
// 戻り値の3つ目: ボード側の Status の選択肢名（Bootstrap を通っていなければ nil）。
// **見出し語 `Status の名前` がこれを使う。**同じ応答から取るので、追加のリクエストは要らない。
// 戻り値の4つ目: ボードの自動化の一覧（読めなければ nil）。
// **見出し語 `自動化` がこれを使う。**
// **これだけは別のリクエストで取る**（`FetchProjectWorkflows`）。
// **起動時の検査のクエリへ混ぜてはならない。**あちらは GraphQL が `errors` を1件でも
// 返した時点で落ちるので、`workflows` を読めない環境
// （権限の足りないトークン・この field を持たない GitHub Enterprise Server）では
// **常駐プロセスが起動しなくなる。**
// **nil は「読めなかった」である。**長さ0の「1件も無い」と取り違えてはならない。
func checkBoard(
	ctx context.Context,
	cfg loadedConfig,
	opts Options,
	configSymbol, ghSymbol Symbol,
) (Result, []Repo, []string, []tracker.ProjectWorkflow) {
	if configSymbol != SymbolOK {
		return Result{
			Label:  LabelBoard,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorBoardConfigUnreadable),
		}, nil, nil, nil
	}
	if ghSymbol != SymbolOK {
		// **上流の記号によって文言を分ける。**`✗`（足りない）を「確かめられなかった」と
		// 書くと、人間が直す先を取り違える。
		reason := i18n.T(i18n.KeyDoctorBoardGHMissing)
		if ghSymbol == SymbolUnknown {
			reason = i18n.T(i18n.KeyDoctorBoardGHUnknown)
		}
		return Result{Label: LabelBoard, Symbol: SymbolUnknown, Detail: reason}, nil, nil, nil
	}

	token, err := tracker.ResolveToken(ctx, cfg.Config.Tracker.Provider, opts.GHAuthToken)
	if err != nil {
		return Result{
			Label:    LabelBoard,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorBoardTokenUnresolved, err),
			Remedies: []string{i18n.T(i18n.KeyDoctorBoardRemedyTokenSource)},
		}, nil, nil, nil
	}

	adapter, err := tracker.NewAdapter(
		cfg.Config.Tracker, opts.GraphQLEndpoint, token, opts.HTTPClient, opts.Logger, nil)
	if err != nil {
		return Result{
			Label:    LabelBoard,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorBoardAdapterFailed, err),
			Remedies: []string{i18n.T(i18n.KeyDoctorBoardRemedyTracker)},
		}, nil, nil, nil
	}

	if err := adapter.Bootstrap(ctx, cfg.Config.Tracker); err != nil {
		return boardFailure(ctx, i18n.T(i18n.KeyDoctorBoardWhatBootstrap), err, opts.GraphQLEndpoint), nil, nil, nil
	}
	boardStates := adapter.StatusOptionNames()

	issues, err := adapter.FetchIssuesByStates(ctx, cfg.Config.Tracker.ActiveStates)
	if err != nil {
		return boardFailure(ctx, i18n.T(i18n.KeyDoctorBoardWhatFetchIssues), err, opts.GraphQLEndpoint), nil, nil, nil
	}

	// **自動化はいちばん最後に読む**（見出し語 `自動化`。issue #209）。
	//
	// **起動時の検査のクエリへ混ぜてはならない。**あちらは GraphQL が `errors` を
	// 1件でも返した時点で落ちるので、`workflows` を読めない環境
	// （権限の足りないトークン・この field を持たない GitHub Enterprise Server）では
	// **常駐プロセスが起動しなくなる。**
	//
	// **要る2本（Bootstrap と候補の取得）より後ろに置く。**この見出し語の期限は
	// 2本ぶんしかないので、**先に置くと、止まったこの1本が候補の取得の残り時間を食い、
	// 見出し語 `カンバン` が `!` になって clone も信頼登録も巻き添えで `!` になる。**
	// **自動化は起動の前提ではない。**後ろに置けば、足りなくなるのはこの1本だけで済む。
	//
	// **読めなくても、ここでは何もしない。**戻り値は nil のままにして、
	// 見出し語 `自動化` を `!`（確かめられなかった）にする。
	workflows, err := adapter.FetchProjectWorkflows(ctx)
	if err != nil {
		opts.Logger.Debug("カンバンの自動化を読めませんでした（見出し語 `自動化` は確かめられなかったになります）",
			"error", err)
		workflows = nil
	}

	repos := collectRepos(issues)
	return Result{
		Label:  LabelBoard,
		Symbol: SymbolOK,
		Detail: i18n.T(i18n.KeyDoctorBoardOK,
			cfg.Config.Tracker.Provider.Owner, cfg.Config.Tracker.Provider.ProjectNumber,
			len(issues), len(repos), endpointNote(opts.GraphQLEndpoint)),
	}, repos, boardStates, workflows
}

// endpointNote は接続先を差し替えているときに添える1行を作る。
//
// **どこへ繋いだのかを必ず出す。**接続先は環境変数1つで差し替わり、そこへ GitHub の
// トークンが送られる。出力に出さないと、本物の GitHub でない宛先に繋いだことに
// 人間が気づけない。
//
// endpoint: 差し替えた接続先（空なら本番の GitHub）。
// 戻り値: 添える文字列（差し替えていなければ空）。
func endpointNote(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	return i18n.T(i18n.KeyDoctorBoardEndpointNote, endpoint)
}

// boardFailure はボードを読めなかったときの結果を組み立てる（設計 3-32 の「落ち方で分ける」）。
//
// **レートリミットだけ `!` にする。**時間をおけば通るので、直すものが無い。
// それ以外（project が見つからない・Status の選択肢名の不一致・通信の失敗）は `✗` である。
//
// **認証の失効も分ける。**トークンが無効・失効しているのに
// 「owner / project_number / status_field を確認してください」と案内すると、
// 人間が直す先を取り違える。
//
// **期限切れも `!` にする。**時間内に応答が無かっただけで、前提が欠けているとは限らない。
//
// ctx: 検査に渡したコンテキスト（期限切れの判定に使う）。
// what: 何をしようとして落ちたかの説明。
// err: 落ちた原因。
// endpoint: 差し替えた接続先（空なら本番の GitHub）。
// 戻り値: 検査結果。
func boardFailure(ctx context.Context, what string, err error, endpoint string) Result {
	if timedOut(ctx, err) {
		return Result{
			Label:    LabelBoard,
			Symbol:   SymbolUnknown,
			Detail:   i18n.T(i18n.KeyDoctorBoardTimeout, what, err, endpointNote(endpoint)),
			Remedies: []string{i18n.T(i18n.KeyDoctorBoardRemedyRetryConnection)},
		}
	}
	if tracker.IsCategory(err, tracker.CategoryRateLimited) {
		return Result{
			Label:    LabelBoard,
			Symbol:   SymbolUnknown,
			Detail:   i18n.T(i18n.KeyDoctorBoardRateLimited, what, err),
			Remedies: []string{i18n.T(i18n.KeyDoctorBoardRemedyWait)},
		}
	}
	remedy := i18n.T(i18n.KeyDoctorBoardRemedyProvider)
	switch {
	case tracker.IsCategory(err, tracker.CategoryInvalidConfig):
		remedy = i18n.T(i18n.KeyDoctorBoardRemedyStatusOptions)
	case tracker.IsCategory(err, tracker.CategoryMissingSecret):
		remedy = i18n.T(i18n.KeyDoctorBoardRemedyTokenInvalid)
	}
	return Result{
		Label:    LabelBoard,
		Symbol:   SymbolMissing,
		Detail:   i18n.T(i18n.KeyDoctorBoardFailed, what, err, endpointNote(endpoint)),
		Remedies: []string{remedy},
	}
}

// checkClone は対象リポジトリの clone が手元にあるかを検査する（見出し語 `clone`）。
//
// **`ghq list -p -e <owner>/<repo>` の出力が空でないかで判定する**（設計 3-6 の3段と同じ呼び方）。
// **exit code は存在の有無にかかわらず 0 を返す**（実測）ので、出力の有無だけを見る。
//
// ctx: 呼び出しに適用するコンテキスト。
// opts: `ghq list` の差し替え口を含む入力。
// repos: ボードから集めた対象リポジトリ。
// boardSymbol: 上流（ボード）の記号。
// 戻り値の1つ目: 検査結果。
// 戻り値の2つ目: リポジトリごとの clone の絶対パス（見つからなかったものは載らない）。
func checkClone(
	ctx context.Context,
	opts Options,
	repos []Repo,
	boardSymbol Symbol,
) (Result, map[string]string) {
	if boardSymbol != SymbolOK {
		return Result{
			Label:  LabelClone,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorCloneBoardUnreadable),
		}, nil
	}
	// **ghq と git が PATH に無ければ、この先を調べても意味が無い。**
	// continuo は worktree を用意するときにこの2つを起動するので、
	// 無いまま段8 へ進むと必ず落ちる。**対象が0件でも先に見る。**段6 の時点ではボードに issue が無いので、
	// ここを後回しにすると段7 まで気づけない。
	for _, bin := range []string{"ghq", "git"} {
		if _, err := exec.LookPath(bin); err != nil {
			return Result{
				Label:    LabelClone,
				Symbol:   SymbolMissing,
				Detail:   i18n.T(i18n.KeyDoctorCloneBinNotFound, bin),
				Remedies: []string{i18n.T(i18n.KeyDoctorCloneRemedyInstallBin, bin)},
			}, nil
		}
	}

	if len(repos) == 0 {
		// **ボードが空なのは設定の誤りではない**（設計 3-32）。終了コードに影響させない。
		return Result{
			Label:  LabelClone,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorCloneNoTargets),
		}, nil
	}

	paths := make(map[string]string, len(repos))
	symbol := SymbolOK
	var notes, remedies []string
	missing, unknown := 0, 0

	for _, repo := range repos {
		path, err := opts.GhqList(ctx, repo.Owner, repo.Name)
		switch {
		case err != nil:
			symbol = worse(symbol, SymbolUnknown)
			unknown++
			notes = append(notes, i18n.T(i18n.KeyDoctorCloneNoteGhqFailed, repo, err))
		case path == "":
			symbol = worse(symbol, SymbolMissing)
			missing++
			notes = append(notes, i18n.T(i18n.KeyDoctorCloneNoteMissing, repo))
			remedies = append(remedies, i18n.T(i18n.KeyDoctorCloneRemedyGhqGet, repo))
		default:
			paths[repo.String()] = path
			notes = append(notes, i18n.T(i18n.KeyDoctorCloneNoteFound, repo, path))
		}
	}

	return Result{
		Label:  LabelClone,
		Symbol: symbol,
		Detail: countDetail(symbol,
			i18n.T(i18n.KeyDoctorCloneDetailOK, len(repos)),
			i18n.T(i18n.KeyDoctorCloneDetailMissing, len(repos), missing),
			i18n.T(i18n.KeyDoctorCloneDetailUnknown, len(repos), unknown),
			unknown),
		Notes:    notes,
		Remedies: remedies,
	}, paths
}

// countDetail は対象が複数ある検査（clone / 信頼登録）の説明を、記号と食い違わない文言で選ぶ。
//
// **記号が `!` のときに「0件が未承認です」のような件数の見出しを出さない。**
// 見出しの行だけを読むと「問題なし」に見えてしまうためである。
// `✗` と `!` が混ざったときは、`✗` の文言に「確かめられなかった件数」を添える。
//
// symbol: その検査の記号（重いほうを採ったあとの値）。
// ok: `✓` のときの説明。
// missing: `✗` のときの説明。
// unknown: `!` のときの説明。
// unknownCount: 確かめられなかった対象の件数（`✗` の説明に添えるかどうかの判定に使う）。
// 戻り値: 説明の1行。
func countDetail(symbol Symbol, ok, missing, unknown string, unknownCount int) string {
	switch symbol {
	case SymbolMissing:
		if unknownCount > 0 {
			return missing + i18n.T(i18n.KeyDoctorCountUnknownSuffix, unknownCount)
		}
		return missing
	case SymbolUnknown:
		return unknown
	default:
		return ok
	}
}

// checkTrust は対象リポジトリが Claude Code に信頼登録されているかを検査する
// （見出し語 `信頼登録`）。
//
// **判定は internal/workspace の CheckTrustForClonePath を呼ぶ。**二重に実装しない。
// **鍵にするのは `ghq list -p -e` が返した clone の絶対パスである**（設計 3-32）。
// worktree のパスでは必ず「未承認」になる。**`~/.claude.json` は読むだけである。**
//
// opts: ホームディレクトリを含む入力。
// repos: ボードから集めた対象リポジトリ。
// clonePaths: リポジトリごとの clone の絶対パス（checkClone の戻り値）。
// boardSymbol: 上流（ボード）の記号。
// 戻り値: 検査結果。
func checkTrust(opts Options, repos []Repo, clonePaths map[string]string, boardSymbol Symbol) Result {
	if boardSymbol != SymbolOK {
		return Result{
			Label:  LabelTrust,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorTrustBoardUnreadable),
		}
	}
	if len(repos) == 0 {
		return Result{
			Label:  LabelTrust,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorTrustNoTargets),
		}
	}

	home, err := resolveHomeDir(opts.HomeDir)
	if err != nil {
		return Result{
			Label:  LabelTrust,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorTrustHomeUnresolved,
				workspace.ClaudeConfigFileName, err),
		}
	}

	symbol := SymbolOK
	var notes, remedies []string
	untrusted, unknown := 0, 0

	for _, repo := range repos {
		path, ok := clonePaths[repo.String()]
		if !ok {
			symbol = worse(symbol, SymbolUnknown)
			unknown++
			notes = append(notes, i18n.T(i18n.KeyDoctorTrustNoteNoClone, repo))
			continue
		}
		trusted, reason, err := workspace.CheckTrustForClonePath(path, home)
		switch {
		case err != nil:
			symbol = worse(symbol, SymbolUnknown)
			unknown++
			notes = append(notes, i18n.T(i18n.KeyDoctorTrustNoteUndecidable, repo, err))
		case !trusted:
			symbol = worse(symbol, SymbolMissing)
			untrusted++
			notes = append(notes, i18n.T(i18n.KeyDoctorTrustNoteReason, repo, reason))
			remedies = append(remedies, i18n.T(i18n.KeyDoctorTrustRemedyRunTrust, path))
		default:
			notes = append(notes, i18n.T(i18n.KeyDoctorTrustNoteReason, repo, reason))
		}
	}

	return Result{
		Label:  LabelTrust,
		Symbol: symbol,
		Detail: countDetail(symbol,
			i18n.T(i18n.KeyDoctorTrustDetailOK, len(repos)),
			i18n.T(i18n.KeyDoctorTrustDetailMissing, len(repos), untrusted),
			i18n.T(i18n.KeyDoctorTrustDetailUnknown, len(repos), unknown),
			unknown),
		Notes:    notes,
		Remedies: remedies,
	}
}

// checkCredentials は枠の判定に使う資格情報を検査する（見出し語 `資格情報`）。
//
// **記号は設定が読めたかどうかで分ける**（設計 3-32 の表）。
//
//	設定が読めない                              … `!`（何を見るべきか決まらない）
//	rate_limit.source が none                   … `✓`（token_source は見ない）
//	token_source が env で環境変数がある         … `✓`
//	token_source が env で環境変数が無い         … `✗`
//	token_source が claude_credentials でファイルがある … `✓`
//	token_source が claude_credentials でファイルが無い … `!`
//	token_source が keychain で読めた            … `✓`
//	token_source が keychain で読めない          … `✗`
//	token_source が keychain で期限内に返らない  … `!`
//
// **`token_source: keychain` のときは Keychain を実際に読む**（読めた項目の名前だけを取る。
// **値は受け取らない**）。読まずに `!` を出すと、macOS の利用者はこの検査から何も得られない。
// **doctor は人間が端末で叩く道具である**ので、確認のダイアログが出ても人間がその場で答えられる。
// **固まらないことは仕組みで保証してある。**この検査には doctor の1項目あたりの期限が掛かり、
// `security` は期限が来た時点で殺される（internal/ratelimit の runSecurity）。
// 期限内に返らなければ `!` にして「ダイアログが出たままかもしれない」と案内する。
// **無人の常駐プロセスでダイアログを出さないための手当ては `continuo allow-keychain-access` である。**
//
// ctx: 呼び出しに適用するコンテキスト（`security` の実行に渡す）。
// opts: 環境変数を引く関数とホームディレクトリを含む入力。
// cfg: 読めた場合の設定。
// configSymbol: 上流（設定ファイル）の記号。
// 戻り値: 検査結果。
func checkCredentials(ctx context.Context, opts Options, cfg loadedConfig, configSymbol Symbol) Result {
	if configSymbol != SymbolOK {
		return Result{
			Label:  LabelCredentials,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorCredentialsConfigUnreadable),
			Remedies: []string{
				i18n.T(i18n.KeyDoctorCredentialsRemedyFixConfig),
			},
		}
	}

	rl := cfg.Config.RateLimit
	if rl.Source == ratelimit.SourceNone {
		return Result{
			Label:  LabelCredentials,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorCredentialsNone),
		}
	}

	if rl.TokenSource == ratelimit.TokenSourceKeychain {
		return checkKeychainCredentials(ctx)
	}

	if rl.TokenSource == ratelimit.TokenSourceEnv {
		// **`config.Load` が先に弾く経路だが、判定はここにも残す。**この検査は
		// 「設定を読めたあとに、資格情報が実際に取れるか」を見るものであり、
		// 空の環境変数名で先へ進むと、下の LookupEnv が空文字を引いて意味の違う文言になる。
		if rl.TokenEnv == "" {
			return Result{
				Label:    LabelCredentials,
				Symbol:   SymbolMissing,
				Detail:   i18n.T(i18n.KeyDoctorCredentialsTokenEnvEmpty),
				Remedies: []string{i18n.T(i18n.KeyDoctorCredentialsRemedyTokenEnv)},
			}
		}
		if value, ok := opts.LookupEnv(rl.TokenEnv); ok && value != "" {
			return Result{
				Label:  LabelCredentials,
				Symbol: SymbolOK,
				Detail: i18n.T(i18n.KeyDoctorCredentialsEnvOK, rl.TokenEnv),
			}
		}
		return Result{
			Label:    LabelCredentials,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorCredentialsEnvMissing, rl.TokenEnv),
			Remedies: []string{i18n.T(i18n.KeyDoctorCredentialsRemedySetEnv, rl.TokenEnv)},
		}
	}

	// **ここへ来るのは `claude_credentials` のときだけである。**
	// この関数が cfg を見るのは configSymbol が `✓` のとき、つまり config.Load の検証を
	// 通ったときだけで、その検証は rate_limit.token_source を `claude_credentials` /
	// `keychain` / `env` に限っている（internal/config/validate.go）。
	// **不正値の分岐は到達しないので置かない。**
	home, err := resolveHomeDir(opts.HomeDir)
	if err != nil {
		return Result{
			Label:  LabelCredentials,
			Symbol: SymbolUnknown,
			Detail: i18n.T(i18n.KeyDoctorCredentialsHomeUnresolved,
				ratelimit.CredentialsRelPath, err),
		}
	}
	path := filepath.Join(home, ratelimit.CredentialsRelPath)
	// **中身は読まない。**在るかどうかだけで判定する（設計 3-32）。
	if _, err := os.Stat(path); err == nil {
		return Result{
			Label:  LabelCredentials,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorCredentialsFileFound, path),
		}
	}
	// **macOS では、このファイルが無いのが普通である。**資格情報は Keychain に入っているので、
	// 「飛ばした」で終わらせず、読める設定へ移る道を出す。
	remedies := []string{i18n.T(i18n.KeyDoctorCredentialsRemedySkipped)}
	if runtime.GOOS == "darwin" {
		remedies = append(remedies, i18n.T(i18n.KeyDoctorCredentialsRemedyUseKeychain))
	}
	return Result{
		Label:    LabelCredentials,
		Symbol:   SymbolUnknown,
		Detail:   i18n.T(i18n.KeyDoctorCredentialsFileMissing, path),
		Remedies: remedies,
	}
}

// checkKeychainCredentials は macOS の Keychain から資格情報を読めるかを検査する。
//
// **読むのは項目の名前だけである。**値（トークン）は受け取らないし、画面にも出さない
// （internal/ratelimit の ProbeKeychain）。
//
// **記号の分け方。**読めたら `✓`、読めたのに accessToken が無い・読めないなら `✗`、
// 期限内に返らなかったら `!` である。`✗` にするのは、利用者が `keychain` を明示して選んだのに
// 取れていない状態だからで、`token_source: env` の環境変数が無いときと同じ扱いにそろえてある。
// **期限切れだけ `!` にする。**返らなかっただけで、資格情報が無いとは限らない。
//
// ctx: 呼び出しに適用するコンテキスト（doctor の1項目あたりの期限が掛かっている）。
// 戻り値: 検査結果。
func checkKeychainCredentials(ctx context.Context) Result {
	probe, err := ratelimit.ProbeKeychain(ctx, 0)
	switch {
	case errors.Is(err, ratelimit.ErrKeychainTimeout):
		return Result{
			Label:    LabelCredentials,
			Symbol:   SymbolUnknown,
			Detail:   i18n.T(i18n.KeyDoctorCredentialsKeychainTimeout, ratelimit.KeychainService, err),
			Remedies: []string{i18n.T(i18n.KeyDoctorCredentialsRemedyKeychainTimeout)},
		}
	case err != nil:
		return Result{
			Label:    LabelCredentials,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorCredentialsKeychainFailed, ratelimit.KeychainService, err),
			Remedies: []string{i18n.T(i18n.KeyDoctorCredentialsRemedyKeychain)},
		}
	case !probe.HasAccessToken:
		return Result{
			Label:    LabelCredentials,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorCredentialsKeychainNoAccessToken, ratelimit.KeychainService),
			Remedies: []string{i18n.T(i18n.KeyDoctorCredentialsRemedyKeychain)},
		}
	default:
		return Result{
			Label:  LabelCredentials,
			Symbol: SymbolOK,
			Detail: i18n.T(i18n.KeyDoctorCredentialsKeychainOK, ratelimit.KeychainService),
		}
	}
}

// resolveHomeDir はホームディレクトリを決める。
//
// configured: Options.HomeDir の値（テストが一時ディレクトリを渡せるようにしてある）。
// 戻り値: ホームディレクトリの絶対パスと、特定できなかった場合のエラー。
func resolveHomeDir(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	return os.UserHomeDir()
}

// checkClaude は `claude.kind` に対応する実行ファイルが PATH にあるかを調べる（設計 3-32）。
//
// **この検査が無かったために、実運用で issue が1件無駄に止まった**（2026-08-21）。
// claude が PATH に無くても herdr は pane を作れてしまうので、着手は段9 まで進み、
// 段10 で `agent_status: unknown` になって初めて分かる。**そこまで行くと worktree も
// pane も作ったあとであり、人間には「Claude Code が起動しませんでした」としか見えない。**
//
// **調べるのは PATH にあるかどうかだけである。**バージョンは見ない（continuo は
// どの版でも動く前提で作ってあり、下限を実測していない）。
//
// opts: `LookPath` を使う（テストは差し替えること）。
// cfg: 読み込んだ設定（`claude.kind` を使う）。
// configSymbol: 設定ファイルの検査の結果。読めていなければ判定しない。
// 戻り値: 検査の結果。
func checkClaude(opts Options, cfg loadedConfig, configSymbol Symbol) Result {
	// **`claude.kind` は herdr に渡す agent の種別であり、実行ファイル名でもある**
	// （herdr の `--kind` の説明が「Supported agent kind and canonical executable」）。
	//
	// **設定が読めなくても既定値で探す**（issue #11）。設定が読めないという理由だけで
	// この検査を `!` にしていたため、すべての見出し語が `!` か `✗` になり、
	// **本当の原因を1つも指摘しないまま利用者を突き放していた。**
	kind := "claude"
	var notes []string
	if configSymbol == SymbolOK {
		if cfg.Config.Claude.Kind != "" {
			kind = cfg.Config.Claude.Kind
		}
	} else {
		notes = []string{i18n.T(i18n.KeyDoctorDefaultUsed)}
	}
	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	path, err := lookPath(kind)
	if err != nil {
		return Result{
			Label:    LabelClaude,
			Symbol:   SymbolMissing,
			Detail:   i18n.T(i18n.KeyDoctorClaudeNotFound, kind),
			Notes:    notes,
			Remedies: []string{i18n.T(i18n.KeyDoctorClaudeRemedyInstall, kind)},
		}
	}
	return Result{
		Label:  LabelClaude,
		Symbol: SymbolOK,
		Detail: i18n.T(i18n.KeyDoctorClaudeFound, path),
		Notes:  notes,
	}
}
