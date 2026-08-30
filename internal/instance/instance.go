// Package instance は「1台で continuo を何本動かすか」を1箇所で決める
// （docs/plans/continuo_design.md 3-17 / 3-17b / 3-17c / 3-17d）。
//
// **既定では1本だけ動く。**ロックは `~/.continuo/continuo.lock` に機械で固定してあり、
// **socket の置き場所からは導かない。**socket の場所は `CONTINUO_RUNTIME_DIR` /
// `XDG_RUNTIME_DIR` / `TMPDIR` で動く（3-23）ので、そこから導くと、
// **同じ機械の同じ利用者が、誰も頼んでいないのに別のロックを握る**（3-17）。
//
// **`--id <名前>` を付けたときだけ、その名前ごとに別の1本として動く。**
// **分けるべきもの5つを、この package の Layout 1つから導く**（3-17b）。
//
//	ロック                  ~/.continuo/id/<名前>/continuo.lock
//	socket と実行時ディレクトリ  ~/.continuo/id/<名前>/run/
//	worktree の置き場所      <workspace.root>/<名前>
//	branch 名               <名前>/<herdr.worktree.branch_template>
//	herdr の agent 名        continuo-<名前>-<repo>-<番号>
//
// **agent 名だけは、この package が組み立てない**（herdr の 32 文字の上限に
// 収める規則が internal/orchestrator にあるため）。**名前を渡すだけである。**
//
// **別々に導いてはならない。**片方だけを直すと食い違い、常駐している側と
// `continuo abandon` が別の場所を見る。**そのとき abandon は、動いている continuo を
// 「動いていない」と判定して worktree を消しにいく**（3-17c）。
//
// **6つ目（ボード）は名前から導けない。**ボードごとのロックで断る（3-17e。board.go）。
package instance

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/socketpath"
)

const (
	// DirName は continuo が機械ごとに1つ持つディレクトリの名前である（`~/.continuo`）。
	DirName = ".continuo"

	// IDDirName は `--id` ごとの置き場所を並べるディレクトリの名前である
	// （`~/.continuo/id/<名前>/`）。
	IDDirName = "id"

	// RunDirName は `--id` を付けたときの実行時ディレクトリの名前である
	// （`~/.continuo/id/<名前>/run/`）。
	RunDirName = "run"

	// MaxIDLen は `--id` に書ける名前の長さの上限である（3-17d）。
	//
	// **socket のパスの上限（103 バイト）に収めるための値である。**
	// 利用者の home のパスが長いと、それだけで上限に近づく。
	MaxIDLen = 32
)

// Layout は continuo 1本ぶんの置き場所である。
//
// **Resolve でしか作れない。**フィールドを外から埋められると、4つのうち1つだけが
// 別の規則で決まる経路ができる。
type Layout struct {
	id         string
	lockPath   string
	runtimeDir string
}

// ID は `--id` に渡された名前を返す。**既定（`--id` 無し）なら空文字である。**
func (l Layout) ID() string { return l.id }

// LockPath は二重起動防止のロックファイルの絶対パスを返す。
//
// **socket の場所から導かない**（3-17）。既定なら `~/.continuo/continuo.lock`、
// `--id <名前>` なら `~/.continuo/id/<名前>/continuo.lock` である。
func (l Layout) LockPath() string { return l.lockPath }

// RuntimeDir は `--id` から導いた実行時ディレクトリの絶対パスを返す。
//
// **既定（`--id` 無し）なら空文字を返す。**その場合は 3-23 の探索順に従う。
func (l Layout) RuntimeDir() string { return l.runtimeDir }

// Resolve は `--id` に渡された名前から Layout を1つ作る（3-17 / 3-17b / 3-17d）。
//
// **名前の検査もここで行う。**呼ぶ側（internal/cli）は、フラグを読んだ直後にこれを呼び、
// エラーが返ったら起動しない。
//
// id: `--id` に渡された名前。**空文字なら既定の1本である。**
// 戻り値: 決まった Layout と、次のいずれかの場合のエラー。
//   - 名前に使えない文字がある（`[a-z0-9]` で始まり、以降は `[a-z0-9-]` だけ）
//   - 名前が MaxIDLen 文字を超える
//   - 名前を足した socket のパスが socketpath.MaxPathLen バイトを超える
//   - ホームディレクトリを引けない
func Resolve(id string) (Layout, error) {
	// **名前の検査を先に通す。**`ValidateID` は外へ1回も出ない純粋な関数であり、
	// **ホームディレクトリを引くより先に答えが出る。**順序を逆にすると、
	// `HOME` を引けない環境で `--id ../../etc` が「ホームディレクトリを取得できません」
	// として報告され、**本当の誤り（名前が使えない）が人間に届かない。**
	if id != "" {
		if err := ValidateID(id); err != nil {
			return Layout{}, err
		}
	}

	root, err := Root()
	if err != nil {
		return Layout{}, err
	}

	if id == "" {
		return Layout{lockPath: filepath.Join(root, socketpath.LockFileName)}, nil
	}

	base := filepath.Join(root, IDDirName, id)
	runtimeDir := filepath.Join(base, RunDirName)

	// **socket のパスの長さを、名前の検査と同じところで見る**（3-17d）。
	// **ここで見ないと、起動して bind する段で初めて落ちる。**
	if _, err := socketpath.Resolve(runtimeDir); err != nil {
		return Layout{}, i18n.Errorf(i18n.KeyInstanceResolveSocketPathTooLong, id, err)
	}

	return Layout{
		id:         id,
		lockPath:   filepath.Join(base, socketpath.LockFileName),
		runtimeDir: runtimeDir,
	}, nil
}

// ValidateID は `--id` に渡された名前が使える形かを検査する（3-17d）。
//
// **この文字列はパスにも branch 名にも socket のパスにも入る。**絞らないと
// `--id ../../etc` が `~/.continuo/id/../../etc/continuo.lock` を指し、
// **`~/.continuo` の外へ出る。**空白や `..` は git の ref としても不正になる。
//
// id: 検査する名前。**空文字は「既定の1本」を表すので、ここへ渡してはならない**
// （渡すとエラーになる）。
// 戻り値: 使える形でない場合のエラー。
func ValidateID(id string) error {
	if len(id) > MaxIDLen {
		return i18n.Errorf(i18n.KeyInstanceValidateIDTooLong, id, len(id), MaxIDLen)
	}
	if !validIDShape(id) {
		return i18n.Errorf(i18n.KeyInstanceValidateIDInvalidShape, id)
	}
	return nil
}

// validIDShape は id が `[a-z0-9]` で始まり、以降が `[a-z0-9-]` だけかを見る。
//
// **正規表現を使わない。**`regexp` を1つ足すために package を引き込むより、
// 許す文字を数え上げたほうが、何を許しているのかが読んで分かる。
//
// id: 検査する名前。
// 戻り値: 形が合っていれば true。空文字は false。
func validIDShape(id string) bool {
	if id == "" {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' && i > 0:
		default:
			return false
		}
	}
	return true
}

// Root は continuo が機械ごとに1つ持つディレクトリ（`~/.continuo`）の絶対パスを返す。
//
// 戻り値: 絶対パスと、ホームディレクトリを引けなかった場合のエラー。
func Root() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", i18n.Errorf(i18n.KeyInstanceRootHomeDirFailed, err)
	}
	return filepath.Join(home, DirName), nil
}

// EnsureLockDir はロックファイルを置くディレクトリを用意する。
//
// **`lock.Acquire` を呼ぶ前に必ず通すこと。**親ディレクトリが無いと、
// 二重起動でもないのに「ロックファイルを開けません」で止まる。
//
// **`socketpath.EnsureDir` は使わない。**あちらは socket を置く場所として
// 権限まで検査するが、ここは `~/.continuo` そのものを作ることがあり、
// **利用者が自分で作った `~/.continuo` の権限を continuo が拒む理由は無い。**
//
// 戻り値: 作成に失敗した場合のエラー。
func (l Layout) EnsureLockDir() error {
	dir := filepath.Dir(l.lockPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return i18n.Errorf(i18n.KeyInstanceEnsureLockDirFailed, dir, err)
	}
	return nil
}

// HookSocketPath は hook を受ける socket の絶対パスを決め、置き場所を用意する。
//
// **`--id` を付けたときは `~/.continuo/id/<名前>/run/hooks.sock` に固定する**（3-17b）。
// **3-23 の探索順（`CONTINUO_RUNTIME_DIR` / `XDG_RUNTIME_DIR` / `TMPDIR`）も、
// `claude.hook_bridge.listen` も、そのときは使わない。**どちらかが効くと、
// 同じ `WORKFLOW.md` から2本立てたときに **issue ごとの設定と hook の逃がし先を共有し、
// 片方がもう片方の hook を食べて捨てる**（3-17b）。
//
// **`--id` を付けていなければ、いままでどおり socketpath.Prepare へ委ねる。**
//
// envRuntimeDir: 環境変数 `CONTINUO_RUNTIME_DIR` の値（`--id` 無しのときだけ使う）。
// explicitListen: `claude.hook_bridge.listen`（`--id` 無しのときだけ使う）。
// 戻り値: socket の絶対パスと、置き場所を用意できなかった場合のエラー。
// **ディレクトリは作られている**（socket そのものはまだ作らない）。
func (l Layout) HookSocketPath(envRuntimeDir string, explicitListen *string) (string, error) {
	if l.runtimeDir == "" {
		return socketpath.Prepare(envRuntimeDir, explicitListen)
	}
	if err := socketpath.EnsureDir(l.runtimeDir); err != nil {
		return "", err
	}
	return socketpath.Resolve(l.runtimeDir)
}

// OverridesListen は、`--id` が `claude.hook_bridge.listen` の指定を使わずに済ませたかを返す。
//
// **黙って握り潰さないためだけにある。**呼ぶ側は真なら1行ログに残すこと。
//
// cfg: 読み込み済みの設定。
// 戻り値: `--id` があり、かつ `claude.hook_bridge.listen` が書かれていれば true。
func (l Layout) OverridesListen(cfg config.Config) bool {
	if l.runtimeDir == "" {
		return false
	}
	return cfg.Claude.HookBridge.Listen != nil && *cfg.Claude.HookBridge.Listen != ""
}

// OverridesRuntimeDirEnv は、`--id` が環境変数 `CONTINUO_RUNTIME_DIR` の指定を
// 使わずに済ませたかを返す。
//
// **黙って握り潰さないためだけにある。**呼ぶ側は真なら1行ログに残すこと。
// **`OverridesListen` と同じ形にしてある。**片方だけ黙ると、
// 「socket が思った場所にできない」理由を、無人運用のログから引けない。
//
// envRuntimeDir: 環境変数 `CONTINUO_RUNTIME_DIR` の値。
// 戻り値: `--id` があり、かつ環境変数に値が入っていれば true。
func (l Layout) OverridesRuntimeDirEnv(envRuntimeDir string) bool {
	return l.runtimeDir != "" && envRuntimeDir != ""
}

// Apply は `--id` から導く残り2つ（worktree の置き場所と branch 名）を設定へ写す（3-17b）。
//
// **`--id` を付けていなければ何も変えない。**
//
//	workspace.root                  <元の値>/<名前>
//	herdr.worktree.branch_template  <名前>/<元のテンプレート>
//
// **branch 名は、既定のテンプレートかどうかを問わず先頭へ足す**（3-17b）。
// 足さないと、同じ branch を2つの worktree が出せないので2本目が永久に着手できず、
// **そのとき出る案内は「`continuo abandon` を実行してください」で、
// 従うと1本目の作業を消す。**
//
// cfg: 読み込み・検証済みの設定。**書き換えずに写しを返す。**
// 戻り値: 置き場所と branch 名を差し替えた設定。
func (l Layout) Apply(cfg config.Config) config.Config {
	if l.id == "" {
		return cfg
	}
	cfg.Workspace.Root = filepath.Join(cfg.Workspace.Root, l.id)
	cfg.Herdr.Worktree.BranchTemplate = l.id + "/" +
		strings.TrimPrefix(cfg.Herdr.Worktree.BranchTemplate, "/")
	return cfg
}
