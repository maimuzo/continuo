package scaffold

import (
	"errors"

	"github.com/maimuzo/continuo/internal/prompt"
)

// InitResult は `continuo init` が2枚のファイルに対して何をしたかである（設計 5-3c）。
//
// **1枚ずつ独立に扱う。**片方が既にあっても、もう片方は書く。
// **版を上げた利用者が `continuo init` を叩くと、足りない PROJECT_SPECIFIC_PROMPT.md
// だけが増える。**これが移行の唯一の手順であり、`--force` を要求してはならない
// （要求すると、手で直した WORKFLOW.md を潰す `--force` を打たせることになる）。
type InitResult struct {
	// Workflow は WORKFLOW.md の結果である。
	Workflow Result
	// WorkflowErr は WORKFLOW.md を書けなかった理由である。書けたなら nil。
	WorkflowErr error
	// ProjectPrompt は PROJECT_SPECIFIC_PROMPT.md の結果である。
	ProjectPrompt Result
	// ProjectPromptErr は PROJECT_SPECIFIC_PROMPT.md を書けなかった理由である。書けたなら nil。
	ProjectPromptErr error
}

// Wrote は、1枚でも新しく書いた（または上書きした）かどうかを返す。
//
// 戻り値: 1枚でも書いていれば真。
func (r InitResult) Wrote() bool {
	return r.WorkflowErr == nil || r.ProjectPromptErr == nil
}

// Failed は、`ErrAlreadyExists` 以外の理由で落ちた失敗があるかどうかを返す。
//
// **既にあることは失敗ではない。**片方だけ在る状態から、足りないほうを足せることを
// 移行の手順にしているためである。**それ以外の失敗**（ディレクトリが無い・symlink・
// 書き込めない）**は、1枚でもあれば終了コードを 1 にする。**
//
// 戻り値: `ErrAlreadyExists` 以外の失敗が1つでもあれば真。
func (r InitResult) Failed() bool {
	return hardFailure(r.WorkflowErr) || hardFailure(r.ProjectPromptErr)
}

// hardFailure は「既にある」以外の失敗かどうかを判定する。
//
// err: 判定するエラー。
// 戻り値: nil でも ErrAlreadyExists でもないなら真。
func hardFailure(err error) bool {
	return err != nil && !errors.Is(err, ErrAlreadyExists)
}

// WriteAll は `continuo init` が置く2枚を書く（設計 5-3c）。
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
	out.ProjectPrompt, out.ProjectPromptErr = WriteProjectPrompt(dir, force)
	return out
}

// ProjectPromptFileName は `continuo init` が置く固有のプロンプトのファイル名である。
//
// **internal/prompt の値をそのまま返す。**書き出す側と読む側で名前がずれると、
// 書いたはずの指示が届かないまま無人で回り続ける。
//
// 戻り値: PROJECT_SPECIFIC_PROMPT.md。
func ProjectPromptFileName() string { return prompt.ProjectFileName }
