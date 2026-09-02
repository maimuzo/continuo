// Package scaffold は WORKFLOW.md を置くコマンドの実体である。
//
// **`continuo init` は雛形を書き出し、`continuo setup` は既にある WORKFLOW.md の
// Status の割り当てだけを書き換える**（update.go）。**setup は雛形を書き直さない。**
//
// **置くのは2枚である**（設計 3-32 / 5-1 / 5-3c / 5-3g）。
// `WORKFLOW.md` が設定で、`PROJECT_SPECIFIC_PROMPT.md` がエージェントへ送る指示書のうち
// 利用者が書く部分である。**1枚ずつ独立に扱い、片方が既にあっても、もう片方は書く。**
// **送る指示書の残りは internal/prompt が持っている**（実行ファイルの中にある）。
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

	"github.com/maimuzo/continuo/internal/atomicfile"
	"github.com/maimuzo/continuo/internal/config"
	"github.com/maimuzo/continuo/internal/i18n"
	"github.com/maimuzo/continuo/internal/prompt"
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

// Result は WriteTemplate / UpdateStatuses が何をしたかを返す。
type Result struct {
	// Path は書いたファイルの絶対パスである。symlink は辿った先（実体）のパスで返す。
	// 渡されたディレクトリが symlink のとき、書き込みはリンク先へ落ちるので、
	// リンク側のパスを報告すると「壊した場所」と食い違うためである。
	// エラーを返す場合も、どのパスで失敗したかを CLI がメッセージに出せるよう、決まっていれば埋める。
	Path string
	// Overwritten は既にあったファイルを書き換えたかどうかである。
	// WriteTemplateWithValues では force が真のときだけ真になりうる。
	// UpdateStatuses は既にあるファイルしか対象にしないので常に真である。
	Overwritten bool
	// Owner は、既にある WORKFLOW.md に書かれていた `tracker.provider.owner` である。
	//
	// **CheckUpdatable だけが埋める。**`continuo setup` が「どのボードを読むか」を
	// 決めるために使う。**プレースホルダのままなら空文字である。**
	Owner string
	// ProjectNumber は、既にある WORKFLOW.md に書かれていた
	// `tracker.provider.project_number` である。
	//
	// **CheckUpdatable だけが埋める。**プレースホルダ（0）のままなら 0 である。
	ProjectNumber int
}

// 区別が要る失敗は sentinel error で返す。cmd/continuo はこれを見て終了コードと文言を決める。
var (
	// ErrAlreadyExists は、書き出す先に既に WORKFLOW.md があり、force が偽だったことを表す。
	ErrAlreadyExists = i18n.Sentinel(i18n.KeyScaffoldErrAlreadyExists)
	// ErrDirNotFound は、指定されたディレクトリが存在しないことを表す。
	// force が真でもディレクトリは作らない（打ち間違えたパスに一式が生まれるのを防ぐ）。
	ErrDirNotFound = i18n.Sentinel(i18n.KeyScaffoldErrDirNotFound)
	// ErrNotADirectory は、指定されたパスが存在するがディレクトリではないことを表す。
	ErrNotADirectory = i18n.Sentinel(i18n.KeyScaffoldErrNotADirectory)
	// ErrSymlink は、書き出す先の WORKFLOW.md が symlink だったことを表す。
	// symlink を辿って書くと、指定されたディレクトリの外にあるリンク先を壊す。
	// --force であっても辿らずに止める。
	//
	// **--force の経路には隙間がある。**os.Lstat で symlink を見てから os.Rename で
	// 差し替えるまでの間に symlink へ置き換えられると、これを返さずに差し替える（設計 3-60）。
	// 新しく作る経路には隙間が無い（O_EXCL と O_NOFOLLOW が kernel の open の時点で見る）。
	ErrSymlink = i18n.Sentinel(i18n.KeyScaffoldErrSymlink)
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
// **ただし force の経路には隙間がある。**os.Lstat で見てから os.Rename で差し替えるまでの間に
// symlink へ置き換えられると、ErrSymlink を返さずに差し替える（設計 3-60）。
//
// **force で既にある WORKFLOW.md を置き換えるときは、その場で空にしてから書かない。**
// 同じディレクトリの一時ファイルへ書き切ってから差し替える（設計 3-59）。途中で落ちても、
// 利用者が手で直した WORKFLOW.md は元のまま残る。**元のファイルの権限もそのまま残る。**
// **読み取り専用（0444 など）の WORKFLOW.md も force なら置き換わる。**差し替えに要るのは
// 親ディレクトリへの書き込み権限であって、ファイル自身の権限ではないためである（設計 3-60）。
// **新しく作るときは差し替えない。**失うものが無いうえ、差し替えにすると umask が効かなくなる。
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
	path, err := resolveTarget(dir, fileName)
	if err != nil {
		return Result{}, err
	}
	return writeOne(path, TemplateWithValues(values), force)
}

// WriteProjectPrompt は dir の直下に PROJECT_SPECIFIC_PROMPT.md の雛形を書く（設計 5-3c）。
//
// **中身は internal/prompt が持っている。**送る文面の一部になるので、
// 組み込みのプロンプトと同じ場所に置き、同じ検査に掛ける。
//
// **WORKFLOW.md と扱いを揃える。**既にあれば ErrAlreadyExists、symlink なら ErrSymlink で止め、
// --force のときだけ同じディレクトリの一時ファイルへ書き切ってから差し替える。
// **読むときは symlink を辿るのに、書くときは辿らない**のは WORKFLOW.md と同じである
// （辿ると、指定されたディレクトリの外にあるリンク先を雛形で潰す）。
//
// dir: 書き出す先のディレクトリ。空文字なら、いまいるディレクトリに書く。
// force: 既に PROJECT_SPECIFIC_PROMPT.md がある場合に上書きするかどうか。
// 戻り値: 書いたファイルの絶対パスと、上書きしたかどうか。エラーは WriteTemplate と同じ種類である。
func WriteProjectPrompt(dir string, force bool) (Result, error) {
	path, err := resolveTarget(dir, prompt.ProjectFileName)
	if err != nil {
		return Result{}, err
	}
	return writeOne(path, prompt.ProjectTemplate(), force)
}

// writeOne は1つのファイルを書く。force と symlink の扱いは WriteTemplate の説明のとおりである。
//
// path: 書き出す絶対パス。
// content: 書き出す中身。
// force: 既にある場合に上書きするかどうか。
// 戻り値: 書いた場所と、上書きしたかどうか。
func writeOne(path, content string, force bool) (Result, error) {
	if force {
		// force のときだけ、既にあるかどうかを先に見る。上書きしたかどうかの報告に使うのと、
		// 既にあるなら「その場で空にしてから書く」のではなく差し替えるためである。
		// symlink そのものの有無を見たいので os.Stat ではなく os.Lstat を使う。
		info, statErr := os.Lstat(path)
		if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return Result{Path: path}, i18n.Errorf(i18n.KeyScaffoldFileStatFailed, path, statErr)
		}
		if statErr == nil {
			// --force であっても symlink は辿らない。辿ると dir の外にあるリンク先を潰す。
			// os.Rename は symlink を辿らずリンクそのものを置き換えるので、ここで止めないと
			// 「リンクを雛形で置き換えてしまった」ことになる。
			if info.Mode()&fs.ModeSymlink != 0 {
				return Result{Path: path}, i18n.Errorf(i18n.KeyScaffoldWriteSymlinkNotFollowed, ErrSymlink, path)
			}
			// **WORKFLOW.md という名前のディレクトリは、ここで名指しして止める。**
			// そのまま差し替えに進むと os.Rename が失敗し、利用者には
			// 「一時ファイルの名前と rename の失敗」だけが並んだ読めない文言が出る。
			// EISDIR を添えて「作成できません: … is a directory」に揃える
			// （その場で開いて書いていた頃と同じ文言である）。
			if info.IsDir() {
				return Result{Path: path}, i18n.Errorf(i18n.KeyScaffoldFileCreateFailed, path, syscall.EISDIR)
			}
			// **既にある WORKFLOW.md は、その場で空にしてから書かない**（CLAUDE.md の
			// 「絶対に守る制約」4 / 設計 3-59）。O_TRUNC で開くと、書いている途中で落ちたときに
			// 利用者が手で直した WORKFLOW.md が失われる。
			// **元のファイルの権限をそのまま渡す。**差し替えは書き込みであって、権限を変える
			// 操作ではない。
			if err := atomicfile.Write(path, []byte(content), info.Mode().Perm()); err != nil {
				return Result{Path: path}, err
			}
			return Result{Path: path, Overwritten: true}, nil
		}
		// force でも、まだ無いなら下の「新しく作る」経路へ落ちる。
	}

	// **新しく作る経路は変えない。**まだ無いファイルには失うものが無いので、差し替えにする
	// 理由が無い。ここを差し替えにすると、umask ではなく chmod で権限が決まるようになり、
	// 出来上がるファイルの権限が変わってしまう。
	//
	// syscall.O_NOFOLLOW は「最後の要素が symlink なら開かずに ELOOP で失敗する」フラグである。
	// これが無いと os.OpenFile は symlink を辿るため、<dir>/WORKFLOW.md が symlink のとき、
	// dir の外にあるリンク先を雛形で上書きしてしまう。
	// syscall は標準ライブラリなので、外部依存は増えない。
	//
	// O_EXCL で「無いときだけ作る」を1回の操作にする。先に os.Stat で存在を見てから
	// 書くと、その隙間に別のプロセスが作ったファイルを黙って壊しうる。
	// O_EXCL は既存の symlink があれば（リンク先の有無によらず）失敗するので
	// O_NOFOLLOW は無くても辿らないが、「辿らない」という意図を明示するために付ける。
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL | syscall.O_NOFOLLOW

	f, err := os.OpenFile(path, flags, filePerm)
	if err != nil {
		return Result{Path: path}, openError(path, err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return Result{Path: path}, i18n.Errorf(i18n.KeyScaffoldFileWriteFailed, path, err)
	}
	if err := f.Close(); err != nil {
		return Result{Path: path}, i18n.Errorf(i18n.KeyScaffoldFileCloseFailed, path, err)
	}
	return Result{Path: path, Overwritten: false}, nil
}

// resolveTarget は受け取ったディレクトリから、書き出すファイルの絶対パスを決める。
//
// dir: 書き出す先のディレクトリ。空文字なら、いまいるディレクトリ。
// name: 書き出すファイルの名前（WORKFLOW.md か PROJECT_SPECIFIC_PROMPT.md）。
// 戻り値: 書き出すファイルの絶対パス（ディレクトリの symlink は辿った先で組み立てる）。
// エラー: ErrDirNotFound / ErrNotADirectory を包んだエラー、または実体を辿れなかった理由。
func resolveTarget(dir, name string) (string, error) {
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
		return "", i18n.Errorf(i18n.KeyScaffoldDirStatFailed, absDir, err)
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
		return "", i18n.Errorf(i18n.KeyScaffoldDirEvalSymlinksFailed, absDir, err)
	}
	return filepath.Join(realDir, name), nil
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
		return i18n.Errorf(i18n.KeyScaffoldWriteSymlinkNotFollowed, ErrSymlink, path)
	}
	if errors.Is(err, fs.ErrExist) {
		// O_EXCL は既存の symlink でも EEXIST を返す。--force を勧めても
		// そちらは ErrSymlink で止まるので、symlink のときは symlink だと言う。
		if info, lerr := os.Lstat(path); lerr == nil && info.Mode()&fs.ModeSymlink != 0 {
			return i18n.Errorf(i18n.KeyScaffoldWriteSymlinkNotFollowed, ErrSymlink, path)
		}
		return fmt.Errorf("%w: %s", ErrAlreadyExists, path)
	}
	return i18n.Errorf(i18n.KeyScaffoldFileCreateFailed, path, err)
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
			return "", i18n.Errorf(i18n.KeyScaffoldDirGetwdFailed, err)
		}
		return wd, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", i18n.Errorf(i18n.KeyScaffoldDirAbsFailed, dir, err)
	}
	return abs, nil
}
