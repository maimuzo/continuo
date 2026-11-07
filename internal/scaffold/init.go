package scaffold

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/maimuzo/continuo/internal/config"
)

// ErrLocalInUse は、WORKFLOW.md の隣に WORKFLOW.local.md が既にあるので、
// 雛形の WORKFLOW.local.md.template を置かなかったことを表す（issue #325 の実装レビュー1周目）。
//
// **失敗ではない。**「既にある」と同じに数える（AllExisted・hardFailure）。
// **--force でも置かない。**置くと、利用者が雛形を写して、使っている WORKFLOW.local.md を上書きしかねない。
// 使い始めた人に雛形は要らず、置くたびに「3枚とも在る」に二度と当たらなくなる。
//
// **この番兵の文言は画面に出さない。**出す文言は CLI が `cli.init.local_in_use` で組み立てる。
var ErrLocalInUse = errors.New("WORKFLOW.local.md already exists")

// ciFileName は `continuo init` が置く2枚目のファイルの名前である（設計 5-3o）。
//
// **拡張子は .yaml である。**GitHub Actions は .yml も .yaml も読むが、
// **利用者はこのファイルを .github/workflows/ へ自分で移す。**移した先で名前を選べるので、
// ここでは片方に決め打つ。**このリポジトリの workflow は .yml なので、
// 移したときに既にあるものと名前がぶつからないほうを選んである。**
const ciFileName = "continuo-ci.yaml"

// InitResult は `continuo init` が3枚のファイルに対して何をしたかである（設計 5-3o）。
//
// **1枚ずつ独立に扱う。**どれかが既にあっても、残りは書く。
// **版を上げた利用者が `continuo init` を叩くと、足りないもの（continuo-ci.yaml・WORKFLOW.local.md.template）だけが増える。**
// **これが移行の唯一の手順であり、`--force` を要求してはならない**
// （要求すると、手で直した WORKFLOW.md の本文を潰す `--force` を打たせることになる。設計 5-3g）。
type InitResult struct {
	// Workflow は WORKFLOW.md の結果である。
	Workflow Result
	// WorkflowErr は WORKFLOW.md を書けなかった理由である。書けたなら nil。
	WorkflowErr error
	// CI は continuo-ci.yaml の結果である。
	CI Result
	// CIErr は continuo-ci.yaml を書けなかった理由である。書けたなら nil。
	CIErr error
	// Local は WORKFLOW.local.md.template の結果である。
	Local Result
	// LocalErr は WORKFLOW.local.md.template を書けなかった理由である。書けたなら nil。
	LocalErr error
}

// Wrote は、1枚でも新しく書いた（または上書きした）かどうかを返す。
//
// 戻り値: 1枚でも書いていれば真。
func (r InitResult) Wrote() bool {
	return r.WorkflowErr == nil || r.CIErr == nil || r.LocalErr == nil
}

// WorkflowFailed は、WORKFLOW.md が「既にある」以外の理由で落ちたかどうかを返す。
//
// **WORKFLOW.md は continuo が動くのに要る。**書けなかったら `continuo init` は失敗である。
//
// 戻り値: `ErrAlreadyExists` 以外の失敗なら真。
func (r InitResult) WorkflowFailed() bool {
	return hardFailure(r.WorkflowErr)
}

// CIFailed は、continuo-ci.yaml が「既にある」以外の理由で落ちたかどうかを返す。
//
// **continuo-ci.yaml は continuo が動くのに要らない。**CI へ移すための見本である。
// **書けなくても `continuo init` は成功で終える**（設計 5-3o）。失敗にすると、
// CI を持たない利用者や、書き込みを絞ったディレクトリで `continuo init` を通せなくなる。
// **ただし黙って落とさない。**呼び出し側が標準エラーへ理由を出す。
//
// 戻り値: `ErrAlreadyExists` 以外の失敗なら真。
func (r InitResult) CIFailed() bool {
	return hardFailure(r.CIErr)
}

// LocalFailed は、WORKFLOW.local.md.template が「既にある」以外の理由で落ちたかどうかを返す。
//
// **WORKFLOW.local.md.template は continuo が動くのに要らない。**continuo はこの名前のままでは読まない見本である。
// **書けなくても `continuo init` は成功で終える**（CIFailed と同じ理由。設計 5-3o）。
// **ただし黙って落とさない。**呼び出し側が標準エラーへ理由を出す。
//
// 戻り値: `ErrAlreadyExists` 以外の失敗なら真。
func (r InitResult) LocalFailed() bool {
	return hardFailure(r.LocalErr)
}

// AllExisted は、3枚とも既にあって1枚も書かなかったかどうかを返す。
//
// **このときだけ `--force` を勧めて終了コード 1 で終える。**
// 1枚でも置けたなら、置けたことを報告して 0 で終える。
//
// **WORKFLOW.local.md が在るので雛形を置かなかった（`ErrLocalInUse`）ときも、3枚目は在るものと数える。**
// 数えないと、雛形を使い始めた人の init は毎回「書いた」扱いになり、1 に二度と届かない。
//
// 戻り値: 3枚とも `ErrAlreadyExists`（3枚目は `ErrLocalInUse` でもよい）なら真。
func (r InitResult) AllExisted() bool {
	return errors.Is(r.WorkflowErr, ErrAlreadyExists) &&
		errors.Is(r.CIErr, ErrAlreadyExists) &&
		(errors.Is(r.LocalErr, ErrAlreadyExists) || errors.Is(r.LocalErr, ErrLocalInUse))
}

// hardFailure は「既にある」以外の失敗かどうかを判定する。
//
// **既にあることは失敗ではない。**一部だけ在る状態から、足りないものを足せることを
// 移行の手順にしているためである。**`ErrLocalInUse` も「既にある」と同じに扱う。**
//
// err: 判定するエラー。
// 戻り値: nil でも ErrAlreadyExists でも ErrLocalInUse でもないなら真。
func hardFailure(err error) bool {
	return err != nil && !errors.Is(err, ErrAlreadyExists) && !errors.Is(err, ErrLocalInUse)
}

// WriteAll は `continuo init` が置く3枚を書く（設計 5-3o）。
//
// **WORKFLOW.md を先に書く。**ディレクトリが無い・ディレクトリでない、といった
// 全部に効く失敗を、先に1回だけ報告するためである。
// **どれかが既にあっても、残りは書く。**そのとき返るのは、在ったものだけの
// `ErrAlreadyExists` である。
// **continuo-ci.yaml が落ちても、WORKFLOW.local.md.template は書く。**2枚とも continuo が読まない見本で、
// 互いに関係が無い。片方の失敗（symlink など）でもう片方まで置けなくなると、利用者には理由が読めない。
//
// dir: 書き出す先のディレクトリ。空文字なら、いまいるディレクトリ。
// force: 既にある場合に上書きするかどうか。**3枚とも同じ扱いになる。**
// values: WORKFLOW.md の front matter に埋める値。
// 戻り値: 3枚それぞれの結果。**エラーは戻り値の中に入れる**（一部だけ落ちる状態を
// 1つのエラーで表せないため）。
func WriteAll(dir string, force bool, values Values) InitResult {
	var out InitResult
	out.Workflow, out.WorkflowErr = WriteTemplateWithValues(dir, force, values)
	// **WORKFLOW.md が「既にある」以外で落ちたら、2枚目も3枚目も書かない。**
	// **2枚目と3枚目も同じ理由で落ちるとは限らない。**
	// **実測: WORKFLOW.md を symlink にすると、`continuo init` は終了コード 1 で
	// symlink のエラーだけを出しながら、15KB の continuo-ci.yaml を黙って置いていた。**
	// **落ちたと言いながらファイルが増えるのは、利用者に説明できない。**
	//
	// **同じエラーを CIErr と LocalErr にも入れる。**呼ぶ側は WorkflowFailed() で先に終えるので
	// この値は画面に出ないが、WriteAll を直に使う側が「書けたのか」を取り違えないようにする
	// （入れないと Wrote() が、1枚も書いていないのに真を返す）。
	if hardFailure(out.WorkflowErr) {
		out.CIErr = out.WorkflowErr
		out.LocalErr = out.WorkflowErr
		return out
	}
	out.CI, out.CIErr = WriteCIWorkflowWithValues(dir, force, values)
	out.Local, out.LocalErr = WriteLocalTemplate(dir, force)
	return out
}

// WriteLocalTemplate は dir の直下に WORKFLOW.local.md.template を書く（設計 5-1a / 5-3o）。
//
// dir / force / 戻り値・エラーの扱いは WriteTemplateWithValues と同じである。
// **書く中身だけが違い、値を1つも埋めない**（localTemplate の説明）。
//
// **同じディレクトリに WORKFLOW.local.md が既にあれば、書かずに `ErrLocalInUse` を返す。**
// `--force` でも書かない（ErrLocalInUse の説明）。そのとき Result.Path は WORKFLOW.local.md のパスである。
// **名前が在るかを os.Lstat で見る。**リンク先の無い symlink も「在る」と数える（config.Load がそれを読もうとして止まるのと揃える）。
//
// dir: 書き出す先のディレクトリ。空文字なら、いまいるディレクトリ。
// force: 既に WORKFLOW.local.md.template がある場合に上書きするかどうか。
// 戻り値: 書いたファイルの絶対パスと、上書きしたかどうか。
func WriteLocalTemplate(dir string, force bool) (Result, error) {
	path, err := resolveTarget(dir, localTemplateFileName)
	if err != nil {
		return Result{}, err
	}
	local := config.LocalPathFor(filepath.Join(filepath.Dir(path), fileName))
	if _, err := os.Lstat(local); err == nil {
		return Result{Path: local}, ErrLocalInUse
	}
	return writeOne(path, localTemplate, force)
}

// WriteCIWorkflowWithValues は dir の直下に continuo-ci.yaml を書く。
//
// dir / force / 戻り値・エラーの扱いは WriteTemplateWithValues と同じである。
// **書く中身だけが違う。**
//
// dir: 書き出す先のディレクトリ。空文字なら、いまいるディレクトリ。
// force: 既に continuo-ci.yaml がある場合に上書きするかどうか。
// values: 埋める値。**いまは1つも使わない**（CITemplateWithValues の説明）。
// 戻り値: 書いたファイルの絶対パスと、上書きしたかどうか。
func WriteCIWorkflowWithValues(dir string, force bool, values Values) (Result, error) {
	path, err := resolveTarget(dir, ciFileName)
	if err != nil {
		return Result{}, err
	}
	return writeOne(path, CITemplateWithValues(values), force)
}

// CIFileName は `continuo init` が置く2枚目のファイル名である。
//
// **CLI が案内と失敗の文言で名乗るために要る。**
//
// 戻り値: continuo-ci.yaml。
func CIFileName() string { return ciFileName }
