// Package workspace は issue ごとの git worktree を用意・再利用・片付けし、
// その worktree が誰のものかを worktree の中の身元ファイルに書く
// （docs/plans/continuo_design.md 3-22 / 3-18 / 3-20 / 3-9）。
//
// このパッケージが守る境界は次の3つである。
//
//   - **トラッカーを知らない。**issue へのコメントは投稿せず、
//     「コメントすべきこと」を CleanupResult.ShouldComment で返して orchestrator に投げさせる。
//     トラッカーへの投稿口を注入すると、この層のテストのたびにテスト用トラッカー mockが要る
//   - **git は自分で叩き、herdr には「開く」と「消す」だけを任せる**（3-22）。
//     prune・worktree add・孤児 branch の始末・branch -D は herdr の API に無い
//   - **`~/.claude.json` は読むだけである。**信頼の登録は人間が Claude Code の画面で行う
package workspace

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/herdr"
	"github.com/maimuzo/continuo/internal/i18n"
)

// HerdrClient は internal/workspace が使う herdr の socket API の部分集合である。
//
// **worktree を作る worktree.create は含めない。**worktree の実体は git が作り終えて
// いるので、herdr には既にあるものを開かせる（3-22 の段7）。
//
// **workspace.list と workspace.close は含める。**`worktree.open` は cwd に渡した
// リポジトリの workspace（**リポジトリの親 workspace**）も一緒に開き、`worktree.remove` は
// そちらを閉じない。放っておくと issue 1件につき1つ溜まる（issue #19）。
// **閉じてよいかの判定に workspace.list が要る**（3-9 の段3b。closeRepoWorkspace を見よ）。
//
// *herdr.Client がこのインタフェースを満たす。
type HerdrClient interface {
	// WorktreeOpen は既にある worktree を herdr の workspace として開く。
	WorktreeOpen(ctx context.Context, params herdr.WorktreeOpenParams) (*herdr.WorktreeOpenResult, error)
	// WorktreeRemove は herdr workspace の ID を指定して worktree を消す。
	WorktreeRemove(ctx context.Context, params herdr.WorktreeRemoveParams) (*herdr.WorktreeRemoveResult, error)
	// WorkspaceRename は開いたあとの herdr workspace に label を書き直す。
	//
	// **worktree.open の label は、既に開かれている workspace には効かない。**
	// 開き直すたびにこれを呼ばないと、旧い label が残る（3-3）。
	WorkspaceRename(ctx context.Context, params herdr.WorkspaceRenameParams) (*herdr.WorkspaceRenameResult, error)
	// WorkspaceList は herdr が開いている workspace の一覧を取る。
	WorkspaceList(ctx context.Context) (*herdr.WorkspaceListResult, error)
	// WorkspaceClose は herdr workspace を閉じる（worktree の実体は消さない）。
	WorkspaceClose(ctx context.Context, params herdr.WorkspaceCloseParams) (*herdr.WorkspaceCloseResult, error)
}

// GhqListFunc は `ghq list -p -e <owner>/<repo>` 相当の処理を行う関数の型である。
//
// 本番は RunGhqList を使う。テストでは ghq を実際に実行せずに済むよう、別の関数を
// 差し替えて渡す（internal/tracker の GHAuthTokenFunc と同じ考え方で、
// グローバル変数ではなく Options で差し替える）。
//
// owner: リポジトリの所有者名。
// repo: リポジトリ名。
// 戻り値: clone の絶対パス。**clone が無ければ空文字を返し、エラーにはしない**
// （「clone が無い」と「ghq の実行そのものに失敗した」を呼び出し側で区別するため）。
type GhqListFunc func(ctx context.Context, owner, repo string) (string, error)

// Options は Manager を組み立てるための入力である。
type Options struct {
	// Config は WORKFLOW.md から読んだ設定である。
	// workspace / workspace_hooks / herdr.worktree / cleanup の各節を読む。
	Config config.Config
	// Herdr は herdr の socket API のクライアントである。
	// nil でも Manager は作れる（herdr.worktree.create_via_herdr が false のときは使わない）。
	Herdr HerdrClient
	// Logger は構造化ログの出力先である。nil なら何も出力しないロガーを使う。
	Logger *slog.Logger
	// Now は現在時刻を返す関数である。nil なら time.Now を使う。
	Now func() time.Time
	// HomeDir は `~/.claude.json` を探すホームディレクトリである。
	// 空なら os.UserHomeDir() の結果を使う。テストが一時ディレクトリを渡せるようにしてある。
	HomeDir string
	// GhqList は clone のパスを引く関数である。nil なら RunGhqList（本物の ghq 実行）を使う。
	GhqList GhqListFunc
	// SettingsRoot は issue ごとの Claude Code の設定ファイルを置くディレクトリである
	// （3-12 の `<実行時ディレクトリ>/issues`）。
	//
	// **片付けが身元ファイルの settings_path を消す前に、このディレクトリの内側かを
	// 確かめるために持つ。**身元ファイルは worktree の直下にあり、その worktree では
	// エージェントが `--permission-mode dontAsk` で動く（3-16 の段9）ので、
	// **settings_path はエージェントが書き換えられる値である。**検査せずに os.Remove へ
	// 渡すと、任意の1ファイルを消させられる。
	//
	// **空なら settings_path を消さない**（内側かどうかを確かめられないため。
	// 消さなかったことは警告としてログに残す）。絶対パスでなければ New がエラーを返す。
	SettingsRoot string
}

// Manager は worktree の用意・再利用・身元ファイルの読み書き・封じ込め検査・後始末を行う。
//
// **New が workspace.root を作る**（3-20 の段1）。封じ込め検査は解決済みの root と
// 比較するので、root が存在しない状態では検査そのものが成立しない。
//
// **複数の goroutine から同時に呼んでよい。**turn ループは run ごとに goroutine を1つ持ち
// （3-8）、agent.max_concurrent_agents の既定は 2 なので、**1つの Manager を複数の run が
// 共有する。**守り方は次の3つである。
//
//   - after_run の実行済みの印は afterRunMu で守る
//   - **身元ファイルの読み書きは worktree ごとに直列化する**（identityMu）。
//     SetAgentName / IncrementTakeover / MarkCleanupDeferred / WriteIdentity は
//     読んで書き戻すので、守らないと、あとの書き込みが前の書き込みを消す
//   - **`info/exclude` の更新は共通ディレクトリごとに直列化する**（identityMu の別の鍵）。
//     1つのリポジトリの1本のファイルを、worktree ごとに触るためである
type Manager struct {
	cfg          config.Config
	herdr        HerdrClient
	logger       *slog.Logger
	now          func() time.Time
	homeDir      string
	ghqList      GhqListFunc
	settingsRoot string

	// clonePaths は `ghq list -p -e <owner>/<repo>` の答えを短い間だけ覚える
	// （clonePathCacheTTL）。信頼の判定と、破壊的な git コマンドの宛先の検算が
	// issue ごとに ghq を起動するので、そのままではボードの件数ぶんプロセスが立つ。
	clonePaths *ttlCache[string]
	// trustResults は信頼の判定の結果を短い間だけ覚える（trustCacheTTL）。
	// **判定1回につき ghq と git を1本ずつ起動し `~/.claude.json` を読み直す**ので、
	// ボードの項目ごとに呼ばれると費用が跳ね上がる（3-6）。
	trustResults *ttlCache[bool]
	// identityMu は身元ファイルと `info/exclude` の読んで書き戻す処理を鍵ごとに直列化する。
	// 鍵は identityLockKey / excludeLockKey が作る。
	identityMu *keyedMutex

	// resolvedRoot は workspace.root をシンボリックリンク解決した絶対パスである（3-20 の段2）。
	// この機械の ~/ghq の下は全部シンボリックリンクなので、素朴な文字列比較では
	// 封じ込め検査が必ず落ちる（3-22）。
	resolvedRoot string

	// afterRunMu は afterRunDone を守る排他である。
	// **2つの run が同時に終わると RunAfterRunOnce が同じ map へ同時に書く**ので、
	// これが無いと Go ランタイムが concurrent map write で落ちる。
	afterRunMu sync.Mutex
	// afterRunDone は、**いま進行中の run について** after_run を実行済みの worktree の
	// パスの集合である。「run が終わったときに1回だけ」（3-9 の段0）をこの層で担保する。
	//
	// **印は run 単位である。**worktree を再利用するということは、その issue が再び
	// dispatch されたということであり、**そこから先は別の run である**（3-18）。
	// そこで Prepare が worktree ごとの印を消す（BeginRun）。消さないと、2回目以降の
	// run で after_run が二度と実行されない。
	//
	// **プロセスを再起動すると消えるが、それでよい。**再起動をまたいで同じ run は続かない。
	afterRunDone map[string]bool
}

// New は Manager を組み立て、そのときに workspace.root を 0700 で作る（3-20 の段1）。
//
// **root を作るのは封じ込め検査より前でなければならない。**検査は root を
// filepath.EvalSymlinks で解決してから比較するので、root が無いと解決に失敗する。
//
// **workspace.identity_file がファイルの名前かどうかもここで確かめる**（3-18）。
// パスの区切りや `..` を含む値だと、身元ファイルが worktree の外側へ書かれる。
//
// opts: 設定・herdr クライアント・ログ・時刻・ホームディレクトリ・ghq の差し替え・
// issue ごとの設定ファイルの置き場所。
// 戻り値: 組み立てた Manager。workspace.root が空・相対パス・作成に失敗・解決に失敗した
// 場合、workspace.identity_file がファイルの名前でない場合、SettingsRoot が相対パスの
// 場合はエラーを返す。
func New(opts Options) (*Manager, error) {
	if err := ValidateIdentityFileName(opts.Config.Workspace.IdentityFile); err != nil {
		return nil, err
	}
	if opts.SettingsRoot != "" && !filepath.IsAbs(opts.SettingsRoot) {
		return nil, i18n.Errorf(i18n.KeyWorkspaceNewSettingsRootNotAbsolute, opts.SettingsRoot)
	}

	resolvedRoot, err := EnsureRoot(opts.Config.Workspace.Root)
	if err != nil {
		return nil, err
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	nowFunc := opts.Now
	if nowFunc == nil {
		nowFunc = time.Now
	}
	ghqList := opts.GhqList
	if ghqList == nil {
		ghqList = RunGhqList
	}
	homeDir := opts.HomeDir
	if homeDir == "" {
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return nil, i18n.Errorf(i18n.KeyWorkspaceNewHomeDirUnknown, err)
		}
	}

	settingsRoot := opts.SettingsRoot
	if settingsRoot != "" {
		settingsRoot = filepath.Clean(settingsRoot)
	}

	// **どのホームディレクトリの `~/.claude.json` で信頼を判定するかを1回だけ残す**
	// （3-6）。HOME を差し替えた環境で起動すると判定の根拠ごと変わるので、
	// あとから「どのファイルを見ていたのか」を追えるようにしておく。**中身は出さない。**
	logger.Info("リポジトリの信頼はこのファイルで判定します",
		"claude_config", filepath.Join(homeDir, ClaudeConfigFileName))

	return &Manager{
		cfg:          opts.Config,
		herdr:        opts.Herdr,
		logger:       logger,
		now:          nowFunc,
		homeDir:      homeDir,
		ghqList:      ghqList,
		settingsRoot: settingsRoot,
		clonePaths:   newTTLCache[string](clonePathCacheTTL, nowFunc),
		trustResults: newTTLCache[bool](trustCacheTTL, nowFunc),
		identityMu:   newKeyedMutex(),
		resolvedRoot: resolvedRoot,
		afterRunDone: map[string]bool{},
	}, nil
}

// ResolvedRoot はシンボリックリンクを解決した workspace.root の絶対パスを返す。
// 封じ込め検査（CheckContainment）に渡す基準のパスである。
func (m *Manager) ResolvedRoot() string { return m.resolvedRoot }

// IssueRef は worktree を用意するために必要な issue の情報である。
//
// **tracker.Issue そのものは受け取らない。**このパッケージはトラッカーを知らないため、
// 必要な項目だけを写した値で受け取る。
type IssueRef struct {
	// URL は issue の URL である。置き場所の <host> をここから取る（3-22）。
	// **空なら github.com を使う。**
	URL string
	// Identifier は `<owner>/<repo>#<番号>` の形の人間可読な名前である（身元ファイルに書く）。
	Identifier string
	// ProjectItemID は project item の ID である（身元ファイルに書く）。
	ProjectItemID string
	// Owner はリポジトリの所有者名である。branch 名と置き場所の組み立てに使う。
	Owner string
	// Repo はリポジトリ名である。
	Repo string
	// Number は GitHub issue の番号である。
	Number int
	// NativeRef はトラッカーのアダプタが入れた provider 固有の値である。
	// **このパッケージが読むのは "default_branch" の1キーだけである**
	// （herdr.worktree.base が null のときの base。3-22 の段4。
	// 「orchestrator は NativeRef の中身を解釈しない」の唯一の例外がここである）。
	NativeRef map[string]any
}
