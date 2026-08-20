package scaffold

// `continuo setup` が既にある WORKFLOW.md を書き換える経路である。
//
// **雛形を書き直さない。**`continuo setup` は `continuo init` が置いたあとの
// WORKFLOW.md に対して走るので、雛形で丸ごと上書きすると、利用者がその間に手で直した行
// （`workspace.root`、`agent.max_concurrent_agents`、`trust.repositories` から消した行など）が
// 全部消える。書き換えるのは StatusKeyNames が返す7つのキーの行だけで、
// **他の行・空行・並び順・インデント・行の右側のコメントは1文字も変えない。**

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// 区別が要る失敗は sentinel error で返す。cmd/continuo はこれを見て終了コードと文言を決める。
var (
	// ErrNotFound は、書き換える先に WORKFLOW.md が無いことを表す。
	//
	// **`continuo setup` は雛形を新規に作らない。**雛形を置くのは `continuo init` の仕事であり、
	// 2つのコマンドが同じファイルを作れると、どちらが正かが決まらない。
	ErrNotFound = errors.New("WORKFLOW.md がありません")

	// ErrKeysNotFound は、既にある WORKFLOW.md に書き換える対象のキーが無いことを表す。
	//
	// **黙って何もしないより落とす。**キーごと消された WORKFLOW.md に書き込んだつもりで
	// 進むと、巡回は無言で「対象0件」を返し続ける。
	ErrKeysNotFound = errors.New("WORKFLOW.md に書き換える対象のキーがありません")

	// ErrStatusesIncomplete は、5つの役割のうち1つでも空のまま渡されたことを表す。
	//
	// **一部だけ書き換えると、割り当てた Status と雛形の既定値が混ざる。**
	ErrStatusesIncomplete = errors.New("5つの役割すべてに選択肢が必要です")
)

// CheckUpdatable は、Status の割り当てを書き換えられるかだけを確かめる。
//
// **`continuo setup` が対話を始める前に呼ぶためにある**（RUCM の基本フロー2）。
// どうせ止まる実行で、先に gh を叩いてボードを読む理由が無い。
//
// **これは事前の見立てであり、保証ではない。**実際の書き換えは UpdateStatuses が行う。
//
// dir: WORKFLOW.md があるディレクトリ。空文字なら、いまいるディレクトリ。
// 戻り値: 書き換える WORKFLOW.md の絶対パス（Result.Overwritten は常に真）。
// エラー: ErrDirNotFound / ErrNotADirectory / ErrNotFound / ErrSymlink を
// errors.Is で判定できる形で返す。
func CheckUpdatable(dir string) (Result, error) {
	path, _, err := statTarget(dir)
	if err != nil {
		return Result{Path: path}, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return Result{Path: path}, fmt.Errorf("WORKFLOW.md を読み込めません: %s: %w", path, err)
	}

	// **尋ねる前にキーの有無を見る。**
	// 5問すべて答えさせたあとで「キーが無い」と落とすと、入力が全部捨てられる。
	// 置き換える値はここでは使わないので、Complete() を満たすだけのダミーを渡す。
	probe := Statuses{Dispatch: "x", Running: "x", Review: "x", Blocked: "x", Done: "x"}
	if _, missing := applyStatuses(string(raw), probe); len(missing) > 0 {
		return Result{Path: path}, fmt.Errorf("%w: %s: %s", ErrKeysNotFound, path, strings.Join(missing, " / "))
	}

	return Result{Path: path, Overwritten: true}, nil
}

// UpdateStatuses は、既にある WORKFLOW.md の Status に関する7行だけを書き換える。
//
// **書き換えるのは StatusKeyNames が返すキーの行だけである。**行の右側のコメントは
// 原文のまま残し、他の行には触れない。YAML を組み立て直さないので、並び順・空行・
// インデントも変わらない。
//
// **書き込みは不可分にする。**同じディレクトリの一時ファイルへ全文を書いてから
// os.Rename で置き換える。途中で落ちても、WORKFLOW.md が半分書かれた状態にはならない。
//
// dir: WORKFLOW.md があるディレクトリ。空文字なら、いまいるディレクトリ。
// st: 割り当てた選択肢名。5つとも埋まっていること。
// 戻り値: 書き換えたファイルの絶対パス（Result.Overwritten は常に真）。
//
// エラー:
//   - ErrStatusesIncomplete: 5つの役割のうち1つでも空である
//   - ErrDirNotFound: dir が存在しない
//   - ErrNotADirectory: dir が存在するがディレクトリではない
//   - ErrNotFound: dir の直下に WORKFLOW.md が無い
//   - ErrSymlink: WORKFLOW.md が symlink である（辿ると dir の外を書き換えるため）
//   - ErrKeysNotFound: 書き換える対象のキーが WORKFLOW.md に無い
//   - 上記以外: 読み込み・書き込みに失敗した
//
// いずれの sentinel error も errors.Is で判定できる形で返す。
func UpdateStatuses(dir string, st Statuses) (Result, error) {
	if !st.Complete() {
		return Result{}, ErrStatusesIncomplete
	}

	path, info, err := statTarget(dir)
	if err != nil {
		return Result{Path: path}, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return Result{Path: path}, fmt.Errorf("WORKFLOW.md を読み込めません: %s: %w", path, err)
	}

	updated, missing := applyStatuses(string(raw), st)
	if len(missing) > 0 {
		return Result{Path: path}, fmt.Errorf("%w: %s: %s", ErrKeysNotFound, path, strings.Join(missing, " / "))
	}

	if err := writeAtomically(path, updated, info.Mode().Perm()); err != nil {
		return Result{Path: path}, err
	}
	return Result{Path: path, Overwritten: true}, nil
}

// statTarget は書き換える WORKFLOW.md のパスを決め、それが書き換えてよいものかを確かめる。
//
// dir: WORKFLOW.md があるディレクトリ。空文字なら、いまいるディレクトリ。
// 戻り値: WORKFLOW.md の絶対パスと、その os.Lstat の結果。
// エラー: ErrDirNotFound / ErrNotADirectory / ErrNotFound / ErrSymlink、
// または os.Lstat が失敗した理由。
func statTarget(dir string) (string, fs.FileInfo, error) {
	path, err := resolveTarget(dir)
	if err != nil {
		return "", nil, err
	}

	// symlink そのものの有無を見たいので os.Stat ではなく os.Lstat を使う。
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return path, nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return path, nil, fmt.Errorf("WORKFLOW.md を確認できません: %s: %w", path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		// WriteTemplateWithValues と同じ判断で、辿らずに止める。
		return path, nil, fmt.Errorf("%w: %s: 辿るとこのディレクトリの外にあるリンク先を壊すため書き換えません", ErrSymlink, path)
	}
	if !info.Mode().IsRegular() {
		return path, nil, fmt.Errorf("%w: %s: 通常のファイルではありません", ErrNotFound, path)
	}
	return path, info, nil
}

// writeAtomically は content を path へ不可分に書き込む。
//
// **同じディレクトリに一時ファイルを作ってから os.Rename する。**別のファイルシステムを
// またぐと os.Rename が使えないので、置き場所は必ず書き込む先と同じディレクトリにする。
// 途中で失敗したときは一時ファイルを消す（消せなかった場合は、書き込みの失敗の理由を優先して返す）。
//
// path: 書き込む先の絶対パス。
// content: 書き込む全文。
// perm: 書き込んだあとに設定する権限（元のファイルの権限をそのまま渡すこと）。
// 戻り値: 書き込みに失敗した理由。成功したら nil。
func writeAtomically(path, content string, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("WORKFLOW.md を書き換える一時ファイルを作れません: %s: %w", dir, err)
	}
	tmp := f.Name()

	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("WORKFLOW.md を書き込めません: %s: %w", tmp, err)
	}
	// os.CreateTemp は 0600 で作るので、元のファイルの権限に戻す。
	if err := f.Chmod(perm); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("WORKFLOW.md の権限を設定できません: %s: %w", tmp, err)
	}
	// **rename の前に fsync する。**書き込んだ内容がディスクに届く前に rename が
	// 先に届くと、電源が落ちたときに中身の無いファイルが残りうる。
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("WORKFLOW.md を書き出せません: %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("WORKFLOW.md を閉じられません: %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("WORKFLOW.md を置き換えられません: %s: %w", path, err)
	}
	return nil
}
