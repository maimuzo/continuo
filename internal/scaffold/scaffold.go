// Package scaffold は `continuo init` の実体である。WORKFLOW.md の雛形を書き出す。
//
// 置くのは WORKFLOW.md の1ファイルだけである（設計 3-32 / 5-1）。
// CLI（cmd/continuo）はこのパッケージを呼ぶだけにする。エラーの文言と終了コードの
// 対応は CLI 側で決めるため、区別が要る失敗は sentinel error で返す。
package scaffold

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/maimuzo/continuo/internal/config"
)

// fileName は init が書き出すファイルの名前である（設計 5-1 / SPEC.md 5.1）。
//
// 自前の定数を持たず config.DefaultFileName をそのまま使う。init が置いたファイル名と
// continuo 本体が探すファイル名は、必ず同じでなければならない（違うと init が置いたファイルを
// 本体が読まないのに、ビルドもテストも通ってしまう）。同じ値の定数を2箇所に持つと
// 片方だけ直したときにその状態になるので、定義を1つに寄せて、ずれようが無い形にする。
const fileName = config.DefaultFileName

// filePerm は書き出す WORKFLOW.md の権限である。
// 認証情報は書かない（トークンは環境変数か gh のログイン情報から取る）ので、
// 読み取りを所有者に限定する必要は無い。
const filePerm fs.FileMode = 0o644

// Result は WriteTemplate が何をしたかを返す。
type Result struct {
	// Path は書いたファイルの絶対パスである。symlink は辿った先（実体）のパスで返す。
	// 渡されたディレクトリが symlink のとき、書き込みはリンク先へ落ちるので、
	// リンク側のパスを報告すると「壊した場所」と食い違うためである。
	// エラーを返す場合も、どのパスで失敗したかを CLI がメッセージに出せるよう、決まっていれば埋める。
	Path string
	// Overwritten は既存のファイルを上書きしたかどうかである（force が真のときだけ真になりうる）。
	Overwritten bool
}

// 区別が要る失敗は sentinel error で返す。cmd/continuo はこれを見て終了コードと文言を決める。
var (
	// ErrAlreadyExists は、書き出す先に既に WORKFLOW.md があり、force が偽だったことを表す。
	ErrAlreadyExists = errors.New("WORKFLOW.md が既にあります")
	// ErrDirNotFound は、指定されたディレクトリが存在しないことを表す。
	// force が真でもディレクトリは作らない（打ち間違えたパスに一式が生まれるのを防ぐ）。
	ErrDirNotFound = errors.New("指定されたディレクトリがありません")
	// ErrNotADirectory は、指定されたパスが存在するがディレクトリではないことを表す。
	ErrNotADirectory = errors.New("指定されたパスはディレクトリではありません")
	// ErrSymlink は、書き出す先の WORKFLOW.md が symlink だったことを表す。
	// symlink を辿って書くと、指定されたディレクトリの外にあるリンク先を壊す。
	// --force であっても辿らずに止める。
	ErrSymlink = errors.New("WORKFLOW.md が symlink です")
)

// WriteTemplate は dir の直下に WORKFLOW.md の雛形を書く。
//
// dir: 書き出す先のディレクトリ。空文字なら、いまいるディレクトリ（os.Getwd）に書く。
// 相対パスも受け付け、いまいるディレクトリを基準に絶対パスへ直す。
// force: 既に WORKFLOW.md がある場合に上書きするかどうか。
//
// 戻り値: 書いたファイルの絶対パスと、上書きしたかどうか。Result.Path は成功時には必ず
// 絶対パスである（dir が相対でも空文字でも）。dir 自身やその親が symlink の場合は、
// 辿った先（実体）のパスで返す。実際に書き込まれる場所と報告が食い違わないようにするためである。
//
// 書き込む先が symlink だった場合は、force が真でも辿らずに ErrSymlink で止める。
// 辿ると dir の外にあるリンク先を雛形で潰すためである。
//
// エラー:
//   - ErrDirNotFound: dir が存在しない。force が真でもディレクトリは作らない
//   - ErrNotADirectory: dir が存在するがディレクトリではない
//   - ErrAlreadyExists: 書き出す先に既にファイルがあり、force が偽である
//   - ErrSymlink: 書き出す先が symlink である（force の有無によらない）
//   - 上記以外: いまいるディレクトリを取得できない、絶対パスへ直せない、書き込みに失敗した、など
//
// いずれの sentinel error も errors.Is で判定できる形で返す。
func WriteTemplate(dir string, force bool) (Result, error) {
	return WriteTemplateWithValues(dir, force, Values{})
}

// WriteTemplateWithValues は、雛形のプレースホルダを values で埋めてから dir の直下に書く。
//
// dir / force / 戻り値・エラーの扱いは WriteTemplate と同じである。違うのは中身だけで、
// tracker.provider.owner と tracker.provider.project_number に values の値を書き込む。
// values のゼロ値（Owner が空文字、ProjectNumber が 0）はプレースホルダのまま残す。
//
// dir: 書き出す先のディレクトリ。空文字なら、いまいるディレクトリに書く。
// force: 既に WORKFLOW.md がある場合に上書きするかどうか。
// values: 埋める値。
func WriteTemplateWithValues(dir string, force bool, values Values) (Result, error) {
	path, err := resolveTarget(dir)
	if err != nil {
		return Result{}, err
	}

	// syscall.O_NOFOLLOW は「最後の要素が symlink なら開かずに ELOOP で失敗する」フラグである。
	// これが無いと os.WriteFile / os.OpenFile は symlink を辿るため、<dir>/WORKFLOW.md が
	// symlink のとき、dir の外にあるリンク先を雛形で上書きしてしまう。
	// syscall は標準ライブラリなので、外部依存は増えない。
	flags := os.O_WRONLY | os.O_CREATE | syscall.O_NOFOLLOW

	overwritten := false
	if force {
		// force のときだけ、上書きしたかどうかを報告するために事前の存在を見る。
		// symlink そのものの有無を見たいので os.Stat ではなく os.Lstat を使う。
		// ここで見た結果と実際の書き込みがずれても、報告する文言が変わるだけで害は無い。
		_, statErr := os.Lstat(path)
		if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return Result{Path: path}, fmt.Errorf("WORKFLOW.md を確認できません: %s: %w", path, statErr)
		}
		overwritten = statErr == nil
		flags |= os.O_TRUNC
	} else {
		// O_EXCL で「無いときだけ作る」を1回の操作にする。先に os.Stat で存在を見てから
		// 書くと、その隙間に別のプロセスが作ったファイルを黙って壊しうる。
		// O_EXCL は既存の symlink があれば（リンク先の有無によらず）失敗するので
		// O_NOFOLLOW は無くても辿らないが、「辿らない」という意図を明示するために付ける。
		flags |= os.O_EXCL
	}

	f, err := os.OpenFile(path, flags, filePerm)
	if err != nil {
		return Result{Path: path}, openError(path, err)
	}
	if _, err := f.WriteString(TemplateWithValues(values)); err != nil {
		f.Close()
		return Result{Path: path}, fmt.Errorf("WORKFLOW.md を書き込めません: %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return Result{Path: path}, fmt.Errorf("WORKFLOW.md を閉じられません: %s: %w", path, err)
	}
	return Result{Path: path, Overwritten: overwritten}, nil
}

// CheckWritable は、雛形を書き出す前に「書き出せるか」だけを確かめる。
//
// **`continuo setup` が対話を始める前に呼ぶためにある**（RUCM の基本フロー2）。
// 上書きできずにどうせ止まる実行で、先に gh を叩いてボードを読む理由が無い。
//
// **これは事前の見立てであり、保証ではない。**実際の書き込みは
// WriteTemplateWithValues が `O_EXCL` / `O_NOFOLLOW` で1回の操作として行う
// （ここで見てから書くまでの隙間に別のプロセスが作ったファイルを壊さないため）。
//
// dir: 書き出す先のディレクトリ。空文字なら、いまいるディレクトリ。
// force: 既に WORKFLOW.md がある場合に上書きしてよいかどうか。
// 戻り値: 書き出す先のパスと、上書きになるかどうか。
// エラー: WriteTemplate と同じ sentinel error（ErrDirNotFound / ErrNotADirectory /
// ErrSymlink / ErrAlreadyExists）を errors.Is で判定できる形で返す。
func CheckWritable(dir string, force bool) (Result, error) {
	path, err := resolveTarget(dir)
	if err != nil {
		return Result{}, err
	}

	// symlink そのものの有無を見たいので os.Stat ではなく os.Lstat を使う。
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Result{Path: path}, nil
		}
		return Result{Path: path}, fmt.Errorf("WORKFLOW.md を確認できません: %s: %w", path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		// --force でも辿らない（書き込み側と同じ判断）。
		return Result{Path: path}, fmt.Errorf("%w: %s: 辿るとこのディレクトリの外にあるリンク先を壊すため書き込みません", ErrSymlink, path)
	}
	if !force {
		return Result{Path: path}, fmt.Errorf("%w: %s", ErrAlreadyExists, path)
	}
	return Result{Path: path, Overwritten: true}, nil
}

// resolveTarget は受け取ったディレクトリから、書き出す WORKFLOW.md の絶対パスを決める。
//
// dir: 書き出す先のディレクトリ。空文字なら、いまいるディレクトリ。
// 戻り値: 書き出す WORKFLOW.md の絶対パス（ディレクトリの symlink は辿った先で組み立てる）。
// エラー: ErrDirNotFound / ErrNotADirectory を包んだエラー、または実体を辿れなかった理由。
func resolveTarget(dir string) (string, error) {
	absDir, err := resolveDir(dir)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(absDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// ディレクトリは作らない。打ち間違えたパスに WORKFLOW.md が生まれると、
			// 利用者は「作ったはずのファイルが見つからない」状態になる。
			return "", fmt.Errorf("%w: %s", ErrDirNotFound, absDir)
		}
		return "", fmt.Errorf("ディレクトリを確認できません: %s: %w", absDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: %s", ErrNotADirectory, absDir)
	}

	// dir 自身（またはその親）が symlink だと、os.Stat も os.OpenFile も辿るので、
	// 書き込みはリンク先へ落ちるのに Result.Path はリンク側のパスを返す。
	// 「壊した場所と、報告した場所が食い違う」ことになるため、実体のパスに直してから
	// Result.Path を組み立てる。filepath.EvalSymlinks は対象が存在しないとエラーになるので、
	// 上の存在の検査より後に呼ぶ。
	realDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return "", fmt.Errorf("ディレクトリの実体を辿れません: %s: %w", absDir, err)
	}
	return filepath.Join(realDir, fileName), nil
}

// openError は書き出し先を開けなかったときのエラーを、CLI が文言と終了コードを決められる形に直す。
//
// path: 開こうとした WORKFLOW.md の絶対パス。
// err: os.OpenFile が返したエラー。
// 戻り値: ErrSymlink / ErrAlreadyExists を包んだエラー、またはそれ以外の理由を説明するエラー。
func openError(path string, err error) error {
	// O_NOFOLLOW を付けて symlink を開こうとすると ELOOP が返る（macOS / Linux とも）。
	// 「シンボリックリンクが多すぎます」という OS の文言のままでは何が起きたか分からないので、
	// symlink であることを名指しした文言に直す。
	if errors.Is(err, syscall.ELOOP) {
		return fmt.Errorf("%w: %s: 辿るとこのディレクトリの外にあるリンク先を壊すため書き込みません", ErrSymlink, path)
	}
	if errors.Is(err, fs.ErrExist) {
		// O_EXCL は既存の symlink でも EEXIST を返す。--force を勧めても
		// そちらは ErrSymlink で止まるので、symlink のときは symlink だと言う。
		if info, lerr := os.Lstat(path); lerr == nil && info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s: 辿るとこのディレクトリの外にあるリンク先を壊すため書き込みません", ErrSymlink, path)
		}
		return fmt.Errorf("%w: %s", ErrAlreadyExists, path)
	}
	return fmt.Errorf("WORKFLOW.md を作成できません: %s: %w", path, err)
}

// Template は書き出す雛形の中身を、プレースホルダを埋めずにそのまま返す。
// 埋めた全文が要る場合は TemplateWithValues を使う。
//
// 戻り値: WORKFLOW.md の雛形の全文（front matter と本文）。
// テストが「プレースホルダが入っているか」「そのまま config.Load に通るか」を、
// ファイルを書かずに確かめられるようにするために公開している。
func Template() string {
	return workflowTemplate
}

// resolveDir は WriteTemplate が受け取った dir を絶対パスへ直す。
//
// dir: 書き出す先のディレクトリ。空文字なら、いまいるディレクトリを使う。
// 戻り値: 絶対パスへ直したディレクトリのパス。
// エラー: いまいるディレクトリを取得できない場合、または絶対パスへ直せない場合。
func resolveDir(dir string) (string, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("いまいるディレクトリを取得できません: %w", err)
		}
		return wd, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("ディレクトリのパスを絶対パスに直せません: %s: %w", dir, err)
	}
	return abs, nil
}
