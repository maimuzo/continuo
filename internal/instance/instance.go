// Package instance は「1台で continuo を何本動かすか」を1箇所で決める
// （docs/plans/continuo_design.md 3-17 / 3-17b）。
//
// **既定では1本だけ動く。**ロックは `~/.continuo/continuo.lock` に機械で固定してあり、
// **socket の置き場所からは導かない。**socket の場所は `CONTINUO_RUNTIME_DIR` /
// `XDG_RUNTIME_DIR` / `TMPDIR` で動く（3-23）ので、そこから導くと、
// **同じ機械の同じ利用者が、誰も頼んでいないのに別のロックを握る**（3-17）。
//
// **`--id <名前>` を付けたときだけ、その名前ごとに別の1本として動く。**
// **分かれるのはロックだけである**（`~/.continuo/id/<名前>/continuo.lock`）。
//
// **worktree の置き場所も socket も branch 名も、ここでは導かない**（3-17b）。
// **開発時に、本番を止めずにテスト版を動かすための機能である。**
// 置き場所はテスト用の `WORKFLOW.md` で書き換える前提であり、
// **導いてしまうと、利用者が書いた設定を黙って上書きすることになる。**
package instance

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/maimuzo/continuo/internal/i18n"
)

const (
	// DirName は continuo が機械ごとに1つ持つディレクトリの名前である（`~/.continuo`）。
	DirName = ".continuo"

	// IDDirName は `--id` ごとの置き場所を並べるディレクトリの名前である
	// （`~/.continuo/id/<名前>/`）。
	IDDirName = "id"

	// MaxIDLen は `--id` に書ける名前の長さの上限である。
	//
	// **この文字列はロックファイルのパスに入る。**上限を置かないと、
	// ファイル名の長さの上限で初めて落ちる。
	MaxIDLen = 32

	// LockFileName は二重起動防止のロックファイル名である（internal/lock が使う）。
	//
	// **置き場所を決めるのはこの package である**（設計 3-17）。
	// **socket の場所から導いてはならない。**socket の場所は環境変数で動くので、
	// そこから導くと、同じ機械の同じ利用者が別のロックを握る。
	LockFileName = "continuo.lock"

	// lockDirPerm は `~/.continuo` と `~/.continuo/id/<名前>` を**新しく作るときに**付ける権限である。
	//
	// **本人だけが読み書きできる形にする。**ロックファイルの隣に何も置かないとはいえ、
	// 置き場所そのものが「どの `--id` で動かしているか」を晒す。
	//
	// **既にあるディレクトリの権限は変えない**（`os.MkdirAll` の振る舞いであり、
	// 意図してそのままにしている）。**continuo は、自分が作っていないディレクトリの
	// 権限を書き換えない。**`continuo doctor` が hook の socket の置き場所について
	// 出す文言と、同じ立場である。
	lockDirPerm = 0o700
)

// InvalidIDError は `--id` に渡された名前そのものが使えないことを表す。
//
// **名前の誤りと、それ以外の誤りを呼ぶ側が言い分けるためにある。**
// `Resolve` は名前を先に検査し、そのあとでホームディレクトリを引く。
// **`HOME` を引けなかっただけの失敗を「--id に渡した名前が使えません」と報告すると、
// `--id` を1文字も渡していない人にその文言が出る。**
//
// **文言は包んだエラーのものをそのまま出す。**この型は目印であって、文言を足さない。
type InvalidIDError struct {
	// Err は使えない理由である（長すぎる・使えない文字がある）。
	Err error
}

// Error は error インターフェースを満たす。**包んだ理由をそのまま返す。**
func (e *InvalidIDError) Error() string { return e.Err.Error() }

// Unwrap は包んだ理由を返す（errors.Is / errors.As のため）。
func (e *InvalidIDError) Unwrap() error { return e.Err }

// IsInvalidID は、エラーが「`--id` に渡された名前そのものが使えない」ものかを返す。
//
// err: 判定するエラー。
// 戻り値: 名前の誤りなら true。
func IsInvalidID(err error) bool {
	var invalid *InvalidIDError
	return errors.As(err, &invalid)
}

// Layout は continuo 1本ぶんのロックの置き場所である。
//
// **Resolve でしか作れない。**フィールドを外から埋められると、
// ロックの場所が別の規則で決まる経路ができる。
type Layout struct {
	id       string
	lockPath string
}

// ID は `--id` に渡された名前を返す。**既定（`--id` 無し）なら空文字である。**
func (l Layout) ID() string { return l.id }

// LockPath は二重起動防止のロックファイルの絶対パスを返す。
//
// **socket の場所から導かない**（3-17）。既定なら `~/.continuo/continuo.lock`、
// `--id <名前>` なら `~/.continuo/id/<名前>/continuo.lock` である。
func (l Layout) LockPath() string { return l.lockPath }

// Resolve は `--id` に渡された名前から Layout を1つ作る（3-17 / 3-17b）。
//
// **名前の検査もここで行う。**呼ぶ側（internal/cli）は、フラグを読んだ直後にこれを呼び、
// エラーが返ったら起動しない。
//
// id: `--id` に渡された名前。**空文字なら既定の1本である。**
// 戻り値: 決まった Layout と、次のいずれかの場合のエラー。
// **上の2つは `*InvalidIDError` である**（IsInvalidID で見分けられる。渡した名前が悪い）。
// **最後の1つは違う**（`--id` を1文字も渡していなくても起きる）。
//   - 名前に使えない文字がある（`[a-z0-9]` で始まり、以降は `[a-z0-9-]` だけ）
//   - 名前が MaxIDLen 文字を超える
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
		return Layout{lockPath: filepath.Join(root, LockFileName)}, nil
	}

	return Layout{
		id:       id,
		lockPath: filepath.Join(root, IDDirName, id, LockFileName),
	}, nil
}

// ValidateID は `--id` に渡された名前が使える形かを検査する。
//
// **この文字列はロックファイルのパスに入る。**絞らないと
// `--id ../../etc` が `~/.continuo/id/../../etc/continuo.lock` を指し、
// **`~/.continuo` の外へ出る。**
//
// **返すエラーは必ず *InvalidIDError である。**呼ぶ側は IsInvalidID でそれを見て、
// 名前の誤りとそれ以外を言い分ける（同じ検査を2度走らせない）。
//
// id: 検査する名前。**空文字は「既定の1本」を表すので、ここへ渡してはならない**
// （渡すとエラーになる）。
// 戻り値: 使える形でない場合のエラー（*InvalidIDError）。
func ValidateID(id string) error {
	if len(id) > MaxIDLen {
		return &InvalidIDError{Err: i18n.Errorf(i18n.KeyInstanceValidateIDTooLong, id, len(id), MaxIDLen)}
	}
	if !validIDShape(id) {
		return &InvalidIDError{Err: i18n.Errorf(i18n.KeyInstanceValidateIDInvalidShape, id)}
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
// **Resolve を通していない Layout を渡してはならない。**`Layout{}` のゼロ値は
// `lockPath` が空文字なので、`filepath.Dir("")` が `"."` になり、
// **カレントディレクトリを `os.MkdirAll` して成功してしまう。**
// 型は Resolve でしか埋められないが、**フィールドを1つも書かない複合リテラル
// （`instance.Layout{}`）は package の外からでも書ける。**だからここで弾く。
//
// 戻り値: ゼロ値を渡された場合と、作成に失敗した場合のエラー。
func (l Layout) EnsureLockDir() error {
	if l.lockPath == "" {
		return i18n.Errorf(i18n.KeyInstanceEnsureLockDirUnresolved)
	}
	dir := filepath.Dir(l.lockPath)
	if err := os.MkdirAll(dir, lockDirPerm); err != nil {
		return i18n.Errorf(i18n.KeyInstanceEnsureLockDirFailed, dir, err)
	}
	return nil
}
