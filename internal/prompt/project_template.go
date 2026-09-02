package prompt

import _ "embed"

//go:embed project_specific.md
var projectTemplateRaw string

// ProjectTemplate は `continuo init` が書き出す PROJECT_SPECIFIC_PROMPT.md の雛形を返す。
//
// **そのまま送っても害が無く、そのままでも役に立つ形にしてある。**
// 穴埋めの案内は HTML のコメントで書いてあるので、節ごと消せる。
//
// **雛形をこのパッケージに置くのは、組み込みのプロンプトと同じ検査に掛けるためである。**
// 雛形は送る文面の一部になるので、変数の名前を間違えていれば起動が止まる。
// **配る前にテストで変数展開して確かめる**（test/internal/prompt）。
//
// 戻り値: PROJECT_SPECIFIC_PROMPT.md の雛形の全文。
func ProjectTemplate() string { return projectTemplateRaw }
