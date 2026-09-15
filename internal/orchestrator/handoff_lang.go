package orchestrator

import "github.com/maimuzo/continuo/internal/i18n"

// handoffReasonT は、引き渡しのコメントの「理由」の段を、日本語の資源から引く（設計 3-11。issue #259）。
//
// **なぜ利用者の言語ではなく日本語か。**理由の段を包む枠（prompt.go の buildHandoffComment の
// 書き出しと【調べるところ】）は、日本語の直書きである。**同じ関数のコメントが
// 「この関数の文言はまとめて移すまで日本語のままにし」と決めている。**
// **理由の段だけを利用者の言語で引くと、英語の既定では1つのコメントに日本語と英語が混ざる。**
//
// **それでも資源から引くのは、文面を1箇所に置くためである。**Go のソースへ日本語を
// 書き戻すと、資源と2箇所に同じ文面ができ、片方だけ直す事故が起きる。
// 枠ごと資源へ移すときは、この関数を i18n.T に置き換えればよい。
//
// key: 引くキー。
// args: 書式へ差し込む値。
// 戻り値: 日本語の資源から組み立てた文字列。日本語の資源が無ければ利用者の言語で引く。
func handoffReasonT(key i18n.Key, args ...any) string {
	if c, ok := i18n.CatalogOf(i18n.LangJA); ok {
		return c.T(key, args...)
	}
	return i18n.T(key, args...)
}
