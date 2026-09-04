package scaffold

import (
	"errors"
)

// ciFileName は `continuo init` が置く2枚目のファイルの名前である（設計 5-3o）。
//
// **拡張子は .yaml である。**GitHub Actions は .yml も .yaml も読むが、
// **利用者はこのファイルを .github/workflows/ へ自分で移す。**移した先で名前を選べるので、
// ここでは片方に決め打つ。**このリポジトリの workflow は .yml なので、
// 移したときに既にあるものと名前がぶつからないほうを選んである。**
const ciFileName = "continuo-ci.yaml"

// InitResult は `continuo init` が2枚のファイルに対して何をしたかである（設計 5-3o）。
//
// **1枚ずつ独立に扱う。**片方が既にあっても、もう片方は書く。
// **版を上げた利用者が `continuo init` を叩くと、足りない continuo-ci.yaml だけが増える。**
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
}

// Wrote は、1枚でも新しく書いた（または上書きした）かどうかを返す。
//
// 戻り値: 1枚でも書いていれば真。
func (r InitResult) Wrote() bool {
	return r.WorkflowErr == nil || r.CIErr == nil
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

// BothExisted は、2枚とも既にあって1枚も書かなかったかどうかを返す。
//
// **このときだけ `--force` を勧めて終了コード 1 で終える。**
// 片方でも置けたなら、置けたことを報告して 0 で終える。
//
// 戻り値: 2枚とも `ErrAlreadyExists` なら真。
func (r InitResult) BothExisted() bool {
	return errors.Is(r.WorkflowErr, ErrAlreadyExists) && errors.Is(r.CIErr, ErrAlreadyExists)
}

// hardFailure は「既にある」以外の失敗かどうかを判定する。
//
// **既にあることは失敗ではない。**片方だけ在る状態から、足りないほうを足せることを
// 移行の手順にしているためである。
//
// err: 判定するエラー。
// 戻り値: nil でも ErrAlreadyExists でもないなら真。
func hardFailure(err error) bool {
	return err != nil && !errors.Is(err, ErrAlreadyExists)
}

// WriteAll は `continuo init` が置く2枚を書く（設計 5-3o）。
//
// **WORKFLOW.md を先に書く。**ディレクトリが無い・ディレクトリでない、といった
// 両方に効く失敗を、先に1回だけ報告するためである。
// **片方が既にあっても、もう片方は書く。**そのとき返るのは、在ったほうだけの
// `ErrAlreadyExists` である。
//
// dir: 書き出す先のディレクトリ。空文字なら、いまいるディレクトリ。
// force: 既にある場合に上書きするかどうか。**2枚とも同じ扱いになる。**
// values: WORKFLOW.md の front matter に埋める値。
// 戻り値: 2枚それぞれの結果。**エラーは戻り値の中に入れる**（片方だけ落ちる状態を
// 1つのエラーで表せないため）。
func WriteAll(dir string, force bool, values Values) InitResult {
	var out InitResult
	out.Workflow, out.WorkflowErr = WriteTemplateWithValues(dir, force, values)
	out.CI, out.CIErr = WriteCIWorkflowWithValues(dir, force, values)
	return out
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
