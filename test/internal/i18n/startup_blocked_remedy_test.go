// 起動直後に確認の画面で止まったときの文言が、許可の出し方を1文字も持たないことの検査である
// （設計 3-11。issue #259）。
//
// **なぜ持たせないか。**起動直後は1回目の指示を送る前なので、エージェントは道具を1つも使っていない。
// **この画面は権限の確認ではなく、フォルダの信頼などの確認である。**判定役へ許可を伝える【対処】は効かない。
// **しかもこの文面は issue のコメントとして投稿される。**公開リポジトリなら、許可の出し方を第三者に教えることになる。
//
// **比べる言い回しは permission_remedy_lang_test.go の remedyWordings から取る。**
// 同じ package の中に同じ文字列を2組持つと、片方だけ文面に合わせて直したときに、
// もう片方が「存在しない文字列が入っていない」を確かめ続け、**必ず通る検査になる。**
package i18n_test

import (
	"strings"
	"testing"

	"github.com/maimuzo/continuo/internal/i18n"
)

// 目的: 起動直後に止まったときの文言が、許可の出し方を持たず、信頼の案内を持つことを、両方の言語で固定する。
//
// **許可の出し方が入っていると、効かない手を人間に打たせ、公開リポジトリでは第三者にも教える。**
// **信頼の案内が消えると、いちばん多い原因（フォルダの信頼）の直し方が届かない。**
//
// 与える情報: 両方の言語の `orchestrator.confirm_startup.blocked` の文言。
// 成功条件: 許可の文・「あなたの言葉で許可を出す」案内・公開の断りのどれも入っておらず、
// `continuo trust` が入っていること。
func TestStartupBlocked_許可の出し方を持たず信頼の案内を持つ(t *testing.T) {
	t.Cleanup(func() { i18n.Use(i18n.DefaultLang) })

	for _, lang := range []i18n.Lang{i18n.LangJA, i18n.LangEN} {
		i18n.Use(lang)

		got := i18n.T(i18n.KeyOrchestratorConfirmStartupBlocked, "agent-1", "agent-1")
		for wlang, w := range remedyWordings {
			for _, ng := range []string{w.grant, w.ownWords, w.publicRefusal} {
				if strings.Contains(got, ng) {
					t.Errorf("%v: 起動直後の文言に %v の %q が入っています。"+
						"起動直後は判定役の出番が無く、公開リポジトリでは第三者に許可の出し方を教えます:\n%s",
						lang, wlang, ng, got)
				}
			}
		}
		if !strings.Contains(got, "continuo trust") {
			t.Errorf("%v: 起動直後の文言に `continuo trust` の案内がありません:\n%s", lang, got)
		}
	}
}
